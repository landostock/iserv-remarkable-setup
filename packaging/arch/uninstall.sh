#!/usr/bin/env bash
set -euo pipefail

[[ -n "${HOME:-}" && "$HOME" = /* ]] || { echo "HOME ist nicht als absoluter Pfad gesetzt." >&2; exit 1; }

APP_ROOT="$HOME/.local/lib/iserv-remarkable-setup"
LAUNCHER="$HOME/.local/bin/iserv-remarkable-setup"
UNINSTALL_LAUNCHER="$HOME/.local/bin/iserv-remarkable-uninstall"
DATA_HOME="${XDG_DATA_HOME:-$HOME/.local/share}"
DESKTOP_DIR="$DATA_HOME/applications"
DESKTOP_FILE="$DESKTOP_DIR/iserv-remarkable-setup.desktop"
ICON_FILE="$DATA_HOME/icons/hicolor/scalable/apps/iserv-remarkable-setup.svg"
CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"
STATE_DIR="$CONFIG_HOME/IServ-reMarkable-Setup"

for managed_link in "$LAUNCHER" "$UNINSTALL_LAUNCHER"; do
  if [[ -L "$managed_link" ]]; then
    TARGET="$(readlink "$managed_link")"
    case "$TARGET" in
      "$APP_ROOT"/*) rm -f -- "$managed_link" ;;
      *) printf 'Fremder Launcher bleibt erhalten: %s\n' "$managed_link" ;;
    esac
  fi
done

rm -f -- "$DESKTOP_FILE" "$ICON_FILE"
rm -rf -- "$APP_ROOT"
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$DESKTOP_DIR" >/dev/null 2>&1 || true
fi

printf 'Die Anwendung wurde entfernt.\n'
printf 'Einrichtungsstand und Wiederaufnahmejournal bleiben absichtlich erhalten unter:\n  %s\n' "$STATE_DIR"
