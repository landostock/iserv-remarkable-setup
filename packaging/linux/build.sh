#!/usr/bin/env bash
set -euo pipefail

ARCH="${1:-}"
case "$ARCH" in
  amd64|arm64) ;;
  *) echo "usage: $0 amd64|arm64" >&2; exit 2 ;;
esac

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
VERSION="$(python3 packaging/version.py app)"
DEB_VERSION="$(python3 packaging/version.py deb)"
RMAPI_COMMIT="f295d5466978954edabaae4c730d50d03740cfac"
BUILD="$ROOT/build/release/linux-$ARCH"
DIST="$ROOT/dist/linux-$ARCH"
RMAPI="$BUILD/rmapi"
HELPER="$BUILD/helper"
rm -rf "$BUILD" "$DIST"
mkdir -p "$BUILD/native" "$DIST" "$HELPER"

if [[ "$(go env GOOS)/$(go env GOARCH)" != "linux/$ARCH" ]]; then
  echo "Linux packaging must run natively on linux/$ARCH" >&2
  exit 1
fi

git clone --quiet --filter=blob:none https://github.com/FNStudios-NI/rmapi.git "$RMAPI"
git -C "$RMAPI" checkout --quiet "$RMAPI_COMMIT"
cp cmd/setup/payload/rmapi_safe_api.go.txt "$RMAPI/api/api.go"
(cd "$RMAPI" && go mod edit -droprequire=github.com/golang-jwt/jwt)
cp helper/rmfolder/main.go helper/rmfolder/go.mod helper/rmfolder/go.sum "$HELPER/"
pushd "$HELPER" >/dev/null
go mod edit -replace=github.com/juruen/rmapi=../rmapi
go mod tidy
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$BUILD/native/iserv-rm-folder-helper" .
popd >/dev/null
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$BUILD/native/iserv-remarkable-setup" ./cmd/setup

test -x "$BUILD/native/iserv-remarkable-setup"
test -x "$BUILD/native/iserv-rm-folder-helper"
test "$("$BUILD/native/iserv-rm-folder-helper" --self-test)" = "iserv-rm-folder-helper: ok"
! strings "$BUILD/native/iserv-remarkable-setup" | grep -F "$ROOT"
! strings "$BUILD/native/iserv-rm-folder-helper" | grep -F "$ROOT"

PORTABLE="IServ-reMarkable-linux-$ARCH"
mkdir -p "$BUILD/$PORTABLE"
install -m 0755 "$BUILD/native/iserv-remarkable-setup" "$BUILD/$PORTABLE/"
install -m 0755 "$BUILD/native/iserv-rm-folder-helper" "$BUILD/$PORTABLE/"
install -m 0644 README.md LICENSE THIRD_PARTY_NOTICES.md SECURITY.md PRIVACY.md "$BUILD/$PORTABLE/"
tar -C "$BUILD" -czf "$DIST/$PORTABLE.tar.gz" "$PORTABLE"

DEBROOT="$BUILD/deb"
install -Dm755 "$BUILD/native/iserv-remarkable-setup" "$DEBROOT/usr/lib/iserv-remarkable-setup/iserv-remarkable-setup"
install -Dm755 "$BUILD/native/iserv-rm-folder-helper" "$DEBROOT/usr/lib/iserv-remarkable-setup/iserv-rm-folder-helper"
install -Dm644 packaging/linux/iserv-remarkable.desktop "$DEBROOT/usr/share/applications/iserv-remarkable.desktop"
install -Dm644 packaging/linux/io.github.landostock.IServRemarkableSetup.metainfo.xml "$DEBROOT/usr/share/metainfo/io.github.landostock.IServRemarkableSetup.metainfo.xml"
install -Dm644 assets/app-icon.svg "$DEBROOT/usr/share/icons/hicolor/scalable/apps/iserv-remarkable-setup.svg"
install -Dm644 THIRD_PARTY_NOTICES.md "$DEBROOT/usr/share/doc/iserv-remarkable-setup/THIRD_PARTY_NOTICES.md"
install -Dm644 README.md "$DEBROOT/usr/share/doc/iserv-remarkable-setup/README.md"
install -Dm644 SECURITY.md "$DEBROOT/usr/share/doc/iserv-remarkable-setup/SECURITY.md"
install -Dm644 PRIVACY.md "$DEBROOT/usr/share/doc/iserv-remarkable-setup/PRIVACY.md"
install -Dm644 LICENSE "$DEBROOT/usr/share/doc/iserv-remarkable-setup/copyright"
mkdir -p "$DEBROOT/DEBIAN"
DEB_ARCH="$ARCH"
cat > "$DEBROOT/DEBIAN/control" <<EOF
Package: iserv-remarkable-setup
Version: $DEB_VERSION
Section: utils
Priority: optional
Architecture: $DEB_ARCH
Maintainer: IServ reMarkable Project <noreply@github.com>
Depends: ca-certificates
Recommends: xdg-utils
Description: Guided IServ to reMarkable synchronization setup
 Cross-platform setup and maintenance companion for a user-owned
 IServ to reMarkable synchronization deployment.
EOF

dpkg-deb --root-owner-group --build "$DEBROOT" "$DIST/IServ-reMarkable-linux-$ARCH.deb"
dpkg-deb --info "$DIST/IServ-reMarkable-linux-$ARCH.deb" >/dev/null
rm -rf "$BUILD/deb-check"
mkdir -p "$BUILD/deb-check"
dpkg-deb -x "$DIST/IServ-reMarkable-linux-$ARCH.deb" "$BUILD/deb-check"
test -x "$BUILD/deb-check/usr/lib/iserv-remarkable-setup/iserv-remarkable-setup"
test -x "$BUILD/deb-check/usr/lib/iserv-remarkable-setup/iserv-rm-folder-helper"
test -s "$BUILD/deb-check/usr/share/icons/hicolor/scalable/apps/iserv-remarkable-setup.svg"

pushd "$DIST" >/dev/null
for file in "$PORTABLE.tar.gz" "IServ-reMarkable-linux-$ARCH.deb"; do
  sha256sum "$file" > "$file.sha256"
  sha256sum -c "$file.sha256"
done
printf '%s\n' "$VERSION" > "IServ-reMarkable-linux-$ARCH.version.txt"
popd >/dev/null

printf 'Built Linux %s RC packages in %s\n' "$ARCH" "$DIST"
