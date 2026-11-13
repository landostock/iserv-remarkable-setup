#!/usr/bin/env bash
# Cross-build the same companion without requiring Inno Setup or a Windows host.
set -euo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd -P)"
cd "$ROOT"
VERSION="$(python3 packaging/version.py app)"
SOURCE_COMMIT="$(git rev-parse HEAD)"
BUILD="$ROOT/build/release/windows-portable"
DIST="$ROOT/dist/windows-portable"
RMAPI="$BUILD/rmapi"
HELPER="$BUILD/helper"
rm -rf "$BUILD" "$DIST"
mkdir -p "$HELPER" "$DIST"
git clone --quiet --filter=blob:none https://github.com/FNStudios-NI/rmapi.git "$RMAPI"
git -C "$RMAPI" checkout --quiet f295d5466978954edabaae4c730d50d03740cfac
cp cmd/setup/payload/rmapi_safe_api.go.txt "$RMAPI/api/api.go"
(cd "$RMAPI" && go mod edit -droprequire=github.com/golang-jwt/jwt)
cp helper/rmfolder/main.go helper/rmfolder/go.mod helper/rmfolder/go.sum "$HELPER/"
(cd "$HELPER" && go mod edit -replace=github.com/juruen/rmapi=../rmapi && go mod tidy)
for arch in amd64 arm64; do
  NAME="IServ-reMarkable-windows-$arch-$VERSION-portable"
  PACKAGE="$BUILD/$NAME"
  mkdir -p "$PACKAGE"
  CGO_ENABLED=0 GOOS=windows GOARCH="$arch" go build -trimpath -ldflags="-s -w" -o "$PACKAGE/iserv-remarkable-setup.exe" ./cmd/setup
  (cd "$HELPER" && CGO_ENABLED=0 GOOS=windows GOARCH="$arch" go build -trimpath -ldflags="-s -w" -o "$PACKAGE/iserv-rm-folder-helper.exe" .)
  cp packaging/windows/README-portable.md "$PACKAGE/LIESMICH.md"
  cp README.md LICENSE THIRD_PARTY_NOTICES.md SECURITY.md PRIVACY.md "$PACKAGE/"
  printf '%s\n' "$VERSION" > "$PACKAGE/VERSION"
  printf 'source_commit=%s\nversion=%s\nbuild_kind=cross-compiled-windows-%s-portable\n' "$SOURCE_COMMIT" "$VERSION" "$arch" > "$PACKAGE/RC_BUILD.txt"
  python3 - "$PACKAGE" "$DIST/$NAME.zip" "$arch" <<'PY'
from pathlib import Path
import hashlib, struct, sys, zipfile
package, target = map(Path, sys.argv[1:3])
machine = {'amd64': 0x8664, 'arm64': 0xaa64}[sys.argv[3]]
for name in ('iserv-remarkable-setup.exe', 'iserv-rm-folder-helper.exe'):
    raw = (package / name).read_bytes()
    assert raw[:2] == b'MZ', name
    offset = struct.unpack_from('<I', raw, 0x3c)[0]
    assert raw[offset:offset+4] == b'PE\0\0', name
    assert struct.unpack_from('<H', raw, offset+4)[0] == machine, name
    assert struct.unpack_from('<H', raw, offset+24)[0] == 0x20b, name
    assert len(raw) > 100_000, name
files = sorted(p for p in package.iterdir() if p.is_file())
(package / 'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest() + '  ' + p.name + '\n' for p in files))
with zipfile.ZipFile(target, 'w', zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
    for p in sorted(package.iterdir()):
        archive.write(p, package.name + '/' + p.name)
with zipfile.ZipFile(target) as archive:
    assert archive.testzip() is None
    for p in package.iterdir():
        assert archive.read(package.name + '/' + p.name) == p.read_bytes()
Path(str(target) + '.sha256').write_text(hashlib.sha256(target.read_bytes()).hexdigest() + '  ' + target.name + '\n')
print(target.name + ': PE architecture, checksums and archive contents OK')
PY
done
printf 'Built Windows portable packages in %s\n' "$DIST"
