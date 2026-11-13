#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd -P)"
cd "$ROOT"
VERSION="$(python3 packaging/version.py app)"
[[ "$VERSION" = "0.9.0-rc27" ]] || { echo "unexpected version: $VERSION" >&2; exit 1; }
SOURCE_COMMIT="$(git rev-parse HEAD)"
RMAPI_COMMIT="f295d5466978954edabaae4c730d50d03740cfac"
BUILD="$ROOT/build/release/arch"
DIST="$ROOT/dist/arch"
PACKAGE_NAME="IServ-reMarkable-Arch-Linux-$VERSION"
PACKAGE="$BUILD/$PACKAGE_NAME"
RMAPI="$BUILD/rmapi"
HELPER="$BUILD/helper"
rm -rf "$BUILD" "$DIST"
mkdir -p "$PACKAGE/bin/linux-amd64" "$PACKAGE/bin/linux-arm64" "$PACKAGE/assets" "$HELPER" "$DIST"

git clone --quiet --filter=blob:none https://github.com/FNStudios-NI/rmapi.git "$RMAPI"
git -C "$RMAPI" checkout --quiet "$RMAPI_COMMIT"
cp cmd/setup/payload/rmapi_safe_api.go.txt "$RMAPI/api/api.go"
(
  cd "$RMAPI"
  go mod edit -droprequire=github.com/golang-jwt/jwt
)
cp helper/rmfolder/main.go helper/rmfolder/go.mod helper/rmfolder/go.sum "$HELPER/"
(
  cd "$HELPER"
  go mod edit -replace=github.com/juruen/rmapi=../rmapi
  go mod tidy
)

for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags="-s -w" -o "$PACKAGE/bin/linux-$arch/iserv-remarkable-setup" ./cmd/setup
  (
    cd "$HELPER"
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags="-s -w" -o "$PACKAGE/bin/linux-$arch/iserv-rm-folder-helper" .
  )
  chmod 0755 "$PACKAGE/bin/linux-$arch/iserv-remarkable-setup" "$PACKAGE/bin/linux-$arch/iserv-rm-folder-helper"
  file "$PACKAGE/bin/linux-$arch/iserv-remarkable-setup" | grep -q 'ELF 64-bit'
  file "$PACKAGE/bin/linux-$arch/iserv-rm-folder-helper" | grep -q 'ELF 64-bit'
  python3 - "$ROOT" "$PACKAGE/bin/linux-$arch" <<'PY'
from pathlib import Path
import sys
build_root = sys.argv[1].encode()
for binary in Path(sys.argv[2]).iterdir():
    if build_root in binary.read_bytes():
        raise SystemExit('absolute workspace path embedded in ' + binary.name)
PY
done

cp packaging/arch/install.sh packaging/arch/uninstall.sh packaging/arch/README.md "$PACKAGE/"
cp README.md "$PACKAGE/PROJECT_README.md"
cp LICENSE THIRD_PARTY_NOTICES.md SECURITY.md PRIVACY.md "$PACKAGE/"
cp assets/app-icon.svg "$PACKAGE/assets/app-icon.svg"
chmod 0755 "$PACKAGE/install.sh" "$PACKAGE/uninstall.sh"
printf '%s\n' "$VERSION" > "$PACKAGE/VERSION"
printf 'source_commit=%s\n' "$SOURCE_COMMIT" > "$PACKAGE/RC_BUILD.txt"

(
  cd "$PACKAGE"
  find . -type f ! -name SHA256SUMS -print | LC_ALL=C sort | xargs shasum -a 256 > SHA256SUMS
  shasum -a 256 -c SHA256SUMS
)

bash packaging/arch/test_installer.sh
tar -C "$BUILD" -czf "$DIST/$PACKAGE_NAME.tar.gz" "$PACKAGE_NAME"
(
  cd "$DIST"
  shasum -a 256 "$PACKAGE_NAME.tar.gz" > "$PACKAGE_NAME.tar.gz.sha256"
  shasum -a 256 -c "$PACKAGE_NAME.tar.gz.sha256"
)

printf 'Built Arch Linux RC package in %s\n' "$DIST"
