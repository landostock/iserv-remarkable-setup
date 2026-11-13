#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd -P)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
PACKAGE="$WORK/package"
HOME_DIR="$WORK/home"
MOCK_BIN="$WORK/mock-bin"
mkdir -p "$PACKAGE/bin/linux-amd64" "$PACKAGE/bin/linux-arm64" "$PACKAGE/assets" "$HOME_DIR/.config/IServ-reMarkable-Setup" "$MOCK_BIN"

cp "$ROOT/packaging/arch/install.sh" "$ROOT/packaging/arch/uninstall.sh" "$PACKAGE/"
cp "$ROOT/assets/app-icon.svg" "$PACKAGE/assets/app-icon.svg"
for file in README.md PROJECT_README.md LICENSE THIRD_PARTY_NOTICES.md SECURITY.md PRIVACY.md; do
  printf 'test %s\n' "$file" > "$PACKAGE/$file"
done
printf '0.9.0-rc27\n' > "$PACKAGE/VERSION"
printf 'source_commit=test\n' > "$PACKAGE/RC_BUILD.txt"

for arch in amd64 arm64; do
  cat > "$PACKAGE/bin/linux-$arch/iserv-remarkable-setup" <<'EOF'
#!/usr/bin/env sh
echo setup-test
EOF
  cat > "$PACKAGE/bin/linux-$arch/iserv-rm-folder-helper" <<'EOF'
#!/usr/bin/env sh
test "$1" = "--self-test" && echo "iserv-rm-folder-helper: ok"
EOF
  chmod 0755 "$PACKAGE/bin/linux-$arch/iserv-remarkable-setup" "$PACKAGE/bin/linux-$arch/iserv-rm-folder-helper"
done

cat > "$MOCK_BIN/uname" <<'EOF'
#!/usr/bin/env sh
echo x86_64
EOF
chmod 0755 "$MOCK_BIN/uname"
printf 'keep\n' > "$HOME_DIR/.config/IServ-reMarkable-Setup/state-marker"

(
  cd "$PACKAGE"
  find . -type f ! -name SHA256SUMS -print | LC_ALL=C sort | xargs shasum -a 256 > SHA256SUMS
)

HOME="$HOME_DIR" PATH="$MOCK_BIN:$PATH" "$PACKAGE/install.sh" >/dev/null
APP_DIR="$HOME_DIR/.local/lib/iserv-remarkable-setup/0.9.0-rc27/linux-amd64"
test -x "$APP_DIR/iserv-remarkable-setup"
test "$("$APP_DIR/iserv-rm-folder-helper" --self-test)" = "iserv-rm-folder-helper: ok"
test -L "$HOME_DIR/.local/bin/iserv-remarkable-setup"
test -L "$HOME_DIR/.local/bin/iserv-remarkable-uninstall"
test -s "$HOME_DIR/.local/share/applications/iserv-remarkable-setup.desktop"
test -s "$HOME_DIR/.local/share/icons/hicolor/scalable/apps/iserv-remarkable-setup.svg"

HOME="$HOME_DIR" "$PACKAGE/uninstall.sh" >/dev/null
test ! -e "$HOME_DIR/.local/lib/iserv-remarkable-setup"
test ! -e "$HOME_DIR/.local/bin/iserv-remarkable-setup"
test ! -e "$HOME_DIR/.local/bin/iserv-remarkable-uninstall"
test -s "$HOME_DIR/.config/IServ-reMarkable-Setup/state-marker"

printf 'tampered\n' >> "$PACKAGE/bin/linux-amd64/iserv-remarkable-setup"
if HOME="$HOME_DIR" PATH="$MOCK_BIN:$PATH" "$PACKAGE/install.sh" >/dev/null 2>&1; then
  echo "tampered payload was accepted" >&2
  exit 1
fi
test ! -e "$HOME_DIR/.local/lib/iserv-remarkable-setup"

echo "Arch installer tests passed"
