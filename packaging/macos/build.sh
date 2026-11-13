#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
if [[ "$(go env GOOS)" != "darwin" ]]; then
  echo "macOS packaging must run on macOS" >&2
  exit 1
fi

VERSION="$(python3 packaging/version.py app)"
BUILD_NUMBER="$(python3 packaging/version.py macbuild)"
RMAPI_COMMIT="f295d5466978954edabaae4c730d50d03740cfac"
BUILD="$ROOT/build/release/macos"
DIST="$ROOT/dist/macos"
RMAPI="$BUILD/rmapi"
HELPER="$BUILD/helper"
APP="$BUILD/IServ reMarkable Setup.app"
CONTENTS="$APP/Contents"
rm -rf "$BUILD" "$DIST"
mkdir -p "$BUILD/bin/amd64" "$BUILD/bin/arm64" "$DIST" "$HELPER"

git clone --quiet --filter=blob:none https://github.com/FNStudios-NI/rmapi.git "$RMAPI"
git -C "$RMAPI" checkout --quiet "$RMAPI_COMMIT"
cp cmd/setup/payload/rmapi_safe_api.go.txt "$RMAPI/api/api.go"
(cd "$RMAPI" && go mod edit -droprequire=github.com/golang-jwt/jwt)
cp helper/rmfolder/main.go helper/rmfolder/go.mod helper/rmfolder/go.sum "$HELPER/"
pushd "$HELPER" >/dev/null
go mod edit -replace=github.com/juruen/rmapi=../rmapi
go mod tidy
popd >/dev/null

for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath -ldflags="-s -w" -o "$BUILD/bin/$arch/iserv-remarkable-setup" ./cmd/setup
  pushd "$HELPER" >/dev/null
  CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath -ldflags="-s -w" -o "$BUILD/bin/$arch/iserv-rm-folder-helper" .
  popd >/dev/null
done

mkdir -p "$CONTENTS/MacOS" "$CONTENTS/Resources"
lipo -create "$BUILD/bin/amd64/iserv-remarkable-setup" "$BUILD/bin/arm64/iserv-remarkable-setup" -output "$CONTENTS/MacOS/iserv-remarkable-setup"
lipo -create "$BUILD/bin/amd64/iserv-rm-folder-helper" "$BUILD/bin/arm64/iserv-rm-folder-helper" -output "$CONTENTS/MacOS/iserv-rm-folder-helper"
chmod 0755 "$CONTENTS/MacOS/iserv-remarkable-setup" "$CONTENTS/MacOS/iserv-rm-folder-helper"
lipo "$CONTENTS/MacOS/iserv-remarkable-setup" -verify_arch x86_64
lipo "$CONTENTS/MacOS/iserv-remarkable-setup" -verify_arch arm64
lipo "$CONTENTS/MacOS/iserv-rm-folder-helper" -verify_arch x86_64
lipo "$CONTENTS/MacOS/iserv-rm-folder-helper" -verify_arch arm64

python3 - "$VERSION" "$BUILD_NUMBER" packaging/macos/Info.plist.in "$CONTENTS/Info.plist" <<'PY'
from pathlib import Path
import sys
version, build, source, target = sys.argv[1:]
text = Path(source).read_text(encoding="utf-8")
text = text.replace("__VERSION__", version).replace("__BUILD__", build)
if "__VERSION__" in text or "__BUILD__" in text:
    raise SystemExit("unresolved Info.plist placeholder")
Path(target).write_text(text, encoding="utf-8")
PY
plutil -lint "$CONTENTS/Info.plist"

# Render the committed vector artwork using macOS Quick Look, then build a
# proper multi-resolution .icns with Apple-provided tools only.
PREVIEW="$BUILD/icon-preview"
ICONSET="$BUILD/AppIcon.iconset"
mkdir -p "$PREVIEW" "$ICONSET"
qlmanage -t -s 1024 -o "$PREVIEW" assets/app-icon.svg >/dev/null 2>&1
MASTER="$(find "$PREVIEW" -type f -name '*.png' -print -quit)"
test -n "$MASTER" -a -s "$MASTER"
make_icon() { sips -z "$1" "$1" "$MASTER" --out "$ICONSET/$2" >/dev/null; }
make_icon 16 icon_16x16.png
make_icon 32 icon_16x16@2x.png
make_icon 32 icon_32x32.png
make_icon 64 icon_32x32@2x.png
make_icon 128 icon_128x128.png
make_icon 256 icon_128x128@2x.png
make_icon 256 icon_256x256.png
make_icon 512 icon_256x256@2x.png
make_icon 512 icon_512x512.png
make_icon 1024 icon_512x512@2x.png
iconutil -c icns "$ICONSET" -o "$CONTENTS/Resources/AppIcon.icns"
test -s "$CONTENTS/Resources/AppIcon.icns"

install -m 0644 README.md LICENSE THIRD_PARTY_NOTICES.md SECURITY.md PRIVACY.md "$CONTENTS/Resources/"
install -m 0644 packaging/macos/README.md "$CONTENTS/Resources/INSTALLATION.md"
# Ad-hoc signing gives the controlled RC a coherent local code signature while
# remaining explicitly different from the Developer ID + notarized v1 gate.
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP"

# Present the conventional drag-to-Applications layout rather than exposing
# build-oriented files directly in the disk image.
DMGROOT="$BUILD/dmg-root"
mkdir -p "$DMGROOT"
ditto "$APP" "$DMGROOT/IServ reMarkable Setup.app"
ln -s /Applications "$DMGROOT/Applications"
DMG="$DIST/IServ-reMarkable-macos.dmg"
hdiutil create -volname "IServ reMarkable" -srcfolder "$DMGROOT" -ov -format UDZO "$DMG" >/dev/null
test -s "$DMG"
MOUNT="$BUILD/mount"
mkdir -p "$MOUNT"
detach_dmg() {
  local attempt
  for attempt in 1 2 3 4 5; do
    if hdiutil detach "$MOUNT" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  hdiutil detach -force "$MOUNT" >/dev/null
}
hdiutil attach "$DMG" -readonly -nobrowse -mountpoint "$MOUNT" >/dev/null
trap 'detach_dmg >/dev/null 2>&1 || true' EXIT
test -x "$MOUNT/IServ reMarkable Setup.app/Contents/MacOS/iserv-remarkable-setup"
test -x "$MOUNT/IServ reMarkable Setup.app/Contents/MacOS/iserv-rm-folder-helper"
test -s "$MOUNT/IServ reMarkable Setup.app/Contents/Resources/AppIcon.icns"
test -s "$MOUNT/IServ reMarkable Setup.app/Contents/Resources/INSTALLATION.md"
test -L "$MOUNT/Applications"
plutil -lint "$MOUNT/IServ reMarkable Setup.app/Contents/Info.plist" >/dev/null
codesign --verify --deep --strict "$MOUNT/IServ reMarkable Setup.app"
test "$("$MOUNT/IServ reMarkable Setup.app/Contents/MacOS/iserv-rm-folder-helper" --self-test)" = "iserv-rm-folder-helper: ok"
detach_dmg
trap - EXIT

pushd "$DIST" >/dev/null
shasum -a 256 "$(basename "$DMG")" > "$(basename "$DMG").sha256"
shasum -a 256 -c "$(basename "$DMG").sha256"
printf '%s\n' "$VERSION" > IServ-reMarkable-macos.version.txt
popd >/dev/null

printf 'Built universal macOS RC package in %s\n' "$DIST"
