#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() {
  printf 'Installation abgebrochen: %s\n' "$*" >&2
  exit 1
}

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)"
case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) fail "Diese CPU-Architektur wird nicht unterstützt: $(uname -m)" ;;
esac

[[ -n "${HOME:-}" && "$HOME" = /* ]] || fail "HOME ist nicht als absoluter Pfad gesetzt."
[[ "$HOME" != *$'\n'* && "$HOME" != *$'\r'* ]] || fail "HOME enthält unzulässige Zeichen."

command -v install >/dev/null 2>&1 || fail "Benötigter Systembefehl fehlt: install"
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$SCRIPT_DIR" && sha256sum --strict --check SHA256SUMS) \
    || fail "Die Paketprüfung ist fehlgeschlagen. Dateien nicht verwenden und Absender informieren."
elif command -v shasum >/dev/null 2>&1; then
  (cd "$SCRIPT_DIR" && shasum -a 256 -c SHA256SUMS) \
    || fail "Die Paketprüfung ist fehlgeschlagen. Dateien nicht verwenden und Absender informieren."
else
  fail "Weder sha256sum noch shasum ist verfügbar."
fi

VERSION="$(tr -d '\r\n' < "$SCRIPT_DIR/VERSION")"
[[ "$VERSION" = "0.9.0-rc27" ]] || fail "Unerwartete Paketversion: $VERSION"

PAYLOAD="$SCRIPT_DIR/bin/linux-$ARCH"
SETUP_SOURCE="$PAYLOAD/iserv-remarkable-setup"
HELPER_SOURCE="$PAYLOAD/iserv-rm-folder-helper"
[[ -f "$SETUP_SOURCE" && -f "$HELPER_SOURCE" ]] || fail "Das Paket enthält keine vollständigen $ARCH-Binärdateien."

APP_ROOT="$HOME/.local/lib/iserv-remarkable-setup"
APP_DIR="$APP_ROOT/$VERSION/linux-$ARCH"
LAUNCHER_DIR="$HOME/.local/bin"
LAUNCHER="$LAUNCHER_DIR/iserv-remarkable-setup"
UNINSTALL_LAUNCHER="$LAUNCHER_DIR/iserv-remarkable-uninstall"
DATA_HOME="${XDG_DATA_HOME:-$HOME/.local/share}"
DESKTOP_DIR="$DATA_HOME/applications"
DESKTOP_FILE="$DESKTOP_DIR/iserv-remarkable-setup.desktop"
ICON_DIR="$DATA_HOME/icons/hicolor/scalable/apps"
ICON_FILE="$ICON_DIR/iserv-remarkable-setup.svg"

install -d -m 0700 "$APP_DIR" "$LAUNCHER_DIR"
install -d -m 0755 "$DESKTOP_DIR" "$ICON_DIR"
install -m 0700 "$SETUP_SOURCE" "$APP_DIR/iserv-remarkable-setup"
install -m 0700 "$HELPER_SOURCE" "$APP_DIR/iserv-rm-folder-helper"
install -m 0700 "$SCRIPT_DIR/uninstall.sh" "$APP_DIR/uninstall.sh"
install -m 0600 "$SCRIPT_DIR/README.md" "$SCRIPT_DIR/PROJECT_README.md" "$SCRIPT_DIR/THIRD_PARTY_NOTICES.md" \
  "$SCRIPT_DIR/SECURITY.md" "$SCRIPT_DIR/PRIVACY.md" "$SCRIPT_DIR/RC_BUILD.txt" "$APP_DIR/"
install -m 0600 "$SCRIPT_DIR/LICENSE" "$APP_DIR/"
install -m 0644 "$SCRIPT_DIR/assets/app-icon.svg" "$ICON_FILE"

HELPER_RESULT="$("$APP_DIR/iserv-rm-folder-helper" --self-test)"
[[ "$HELPER_RESULT" = "iserv-rm-folder-helper: ok" ]] || fail "Der installierte reMarkable-Helfer hat seinen Selbsttest nicht bestanden."

for managed_link in "$LAUNCHER" "$UNINSTALL_LAUNCHER"; do
  if [[ -e "$managed_link" && ! -L "$managed_link" ]]; then
    fail "$managed_link existiert bereits und gehört nicht eindeutig zu diesem Paket."
  fi
done
TEMP_LINK="$LAUNCHER.tmp.$$"
TEMP_UNINSTALL_LINK="$UNINSTALL_LAUNCHER.tmp.$$"
trap 'rm -f -- "$TEMP_LINK" "$TEMP_UNINSTALL_LINK" "${TEMP_DESKTOP:-}"' EXIT
ln -s "$APP_DIR/iserv-remarkable-setup" "$TEMP_LINK"
ln -s "$APP_DIR/uninstall.sh" "$TEMP_UNINSTALL_LINK"
mv -f -- "$TEMP_LINK" "$LAUNCHER"
mv -f -- "$TEMP_UNINSTALL_LINK" "$UNINSTALL_LAUNCHER"

DESKTOP_EXEC="${LAUNCHER//\\/\\\\}"
DESKTOP_EXEC="${DESKTOP_EXEC//\"/\\\"}"
TEMP_DESKTOP="$DESKTOP_FILE.tmp.$$"
cat > "$TEMP_DESKTOP" <<EOF
[Desktop Entry]
Type=Application
Name=IServ → reMarkable Setup
Comment=IServ-Ordner sicher mit reMarkable verbinden
Exec="$DESKTOP_EXEC"
Icon=iserv-remarkable-setup
Terminal=false
Categories=Utility;Education;
StartupNotify=true
EOF
chmod 0644 "$TEMP_DESKTOP"
mv -f -- "$TEMP_DESKTOP" "$DESKTOP_FILE"
trap - EXIT

if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$DESKTOP_DIR" >/dev/null 2>&1 || true
fi

printf '\nIServ → reMarkable %s wurde für Linux %s installiert.\n' "$VERSION" "$ARCH"
printf 'Starte es im Anwendungsmenü oder mit:\n\n  %s\n\n' "$LAUNCHER"
if ! command -v xdg-open >/dev/null 2>&1; then
  printf 'Hinweis: Für das automatische Öffnen des Browsers fehlt xdg-open.\n'
  printf 'Installiere bei Bedarf: sudo pacman -Syu --needed xdg-utils ca-certificates\n'
fi
printf 'Bestehende Einrichtungsdaten wurden nicht gelöscht oder verändert.\n'
