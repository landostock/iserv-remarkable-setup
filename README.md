# IServ → reMarkable

Ein grafischer Einrichtungsassistent, der ausgewählte PDF- und EPUB-Dateien aus IServ in die reMarkable Cloud synchronisiert. Dashboard und Zeitplan laufen in deinen eigenen Cloudflare- und Fly.io-Konten. Der Computer muss nach der Einrichtung nicht eingeschaltet bleiben.

**Status: RC27 / öffentliche Beta.** Die reMarkable-Schnittstelle ist inoffiziell. Die Mac-Version wurde praktisch getestet; die aktuellen Windows- und Linux-Pakete wurden auf macOS übersetzt und strukturell geprüft, aber noch nicht auf diesen Betriebssystemen vollständig getestet. Eine allgemeine Fehlerfreiheit wird nicht zugesichert.

## Download

Alle Pakete, SHA-256-Prüfsummen, Quelltexte und Hinweise findest du im [Release RC27](https://github.com/landostock/iserv-remarkable-setup/releases/tag/v0.9.0-rc27).

| System | Download | Anleitung |
| --- | --- | --- |
| macOS 13.5+, Apple Silicon und Intel | [Universelle DMG](https://github.com/landostock/iserv-remarkable-setup/releases/download/v0.9.0-rc27/IServ-reMarkable-macos.dmg) | [macOS](packaging/macos/README.md) |
| Windows, Intel/AMD 64 Bit | [Portables ZIP x64](https://github.com/landostock/iserv-remarkable-setup/releases/download/v0.9.0-rc27/IServ-reMarkable-windows-amd64-0.9.0-rc27-portable.zip) | [Windows](packaging/windows/README-portable.md) |
| Windows auf ARM64 | [Portables ZIP ARM64](https://github.com/landostock/iserv-remarkable-setup/releases/download/v0.9.0-rc27/IServ-reMarkable-windows-arm64-0.9.0-rc27-portable.zip) | [Windows](packaging/windows/README-portable.md) |
| Arch Linux / Arch Linux ARM | [Linux-Paket für x86-64 und ARM64](https://github.com/landostock/iserv-remarkable-setup/releases/download/v0.9.0-rc27/IServ-reMarkable-Arch-Linux-0.9.0-rc27.tar.gz) | [Arch Linux](packaging/arch/README.md) |

Für andere Linux-Distributionen sind die Build-Skripte vorhanden; ein natives Debian-Paket wird in diesem Release nicht angeboten. Windows wird hier als portabler Assistent ausgeliefert, ohne Startmenü-Installation. Die DMG ist ad-hoc signiert, nicht notarisiert; die Windows-Dateien haben keine Herausgebersignatur.

## Einrichtung

Du brauchst einen aktuellen Browser, Internetzugang, ein reMarkable-Konto, deine IServ-WebDAV-Zugangsdaten und eigene Konten bei Fly.io und Cloudflare. Fly.io kann Zahlungsinformationen verlangen und nutzungsabhängige Kosten berechnen. Prüfe die aktuellen [Fly.io-Preise](https://fly.io/docs/about/pricing/) und [Cloudflare-Bedingungen](https://developers.cloudflare.com/workers/platform/pricing/).

1. Lade das passende Paket herunter und prüfe seine SHA-256-Prüfsumme anhand der beigefügten Datei.
2. macOS: App aus der DMG nach **Programme** ziehen. Windows: ZIP vollständig entpacken und **iserv-remarkable-setup.exe** starten; die Helfer-EXE muss im selben Ordner bleiben. Arch Linux: Archiv entpacken und `bash install.sh` ausführen, ohne `sudo`.
3. Folge dem lokalen Assistenten: Systemprüfung, Kostenhinweis, Fly.io-/Cloudflare-Anmeldung und IServ-/reMarkable-Verbindung.
4. Wähle links die IServ-Quellen und rechts die reMarkable-Ziele. Lege Zeitplan und Dateifilter fest.
5. Die Installation führt einen **Testlauf ohne Upload** aus. Die Automatik bleibt ausgeschaltet.
6. Bestätige anschließend bewusst einen begrenzten echten Lauf mit höchstens fünf Uploadversuchen. Prüfe das Ergebnis auf dem reMarkable.
7. Aktiviere die Automatik erst nach erfolgreichem Echtlauf und einer weiteren bewussten Bestätigung.

Bei neuen Cloudflare-Konten zuerst die E-Mail-Adresse bestätigen. Falls eine workers.dev-Kontoadresse verlangt wird, diese im Cloudflare-Konto einrichten und danach dieselbe Installation fortsetzen. Bei Fehlern den angezeigten Hinweis beachten; keine Ressourcen oder lokalen Zustandsdateien auf Verdacht löschen.

## Spätere Bedienung

Das Cloud-Dashboard bietet Zeitplan, Dateifilter, Status und manuelle Läufe. Die lokale App öffnet nach abgeschlossener Einrichtung die Wartungsansicht: Verbindungen reparieren, Cloud-Komponenten aktualisieren, Diagnose erstellen und die grafischen **Ordnerverknüpfungen bearbeiten**.

Zum Ändern der Zuordnungen das Dashboard-Passwort eingeben, gespeicherte Zuordnungen laden und bei Bedarf den lokalen Ordnerzugang verbinden. Links Quelle und rechts Ziel wählen, verknüpfen, alte Testzuordnungen entfernen und speichern. Speichern startet keinen Sync. Eine bereits aktive Automatik vorher pausieren und laufende Jobs abwarten. Danach die neuen Zuordnungen mit einem Testlauf ohne Upload prüfen.

Vorhandene RC26-Cloud-Komponenten bleiben kompatibel, wenn ihr eingebetteter Deployment-Code dem erwarteten Fingerabdruck entspricht. Ein bereits bestätigter Echtlauf bleibt beim lokalen Update auf RC27 erhalten. Ältere Cloud-Versionen benötigen das ausdrückliche Cloud-Update.

## Daten und Sicherheit

- Es gibt keinen zentralen Projektserver mit Benutzerkonten oder gesammelten Zugangsdaten. Jede Einrichtung verwendet eigene Providerkonten.
- Der lokale Assistent lauscht nur auf `127.0.0.1` und schützt seine API mit einem zufälligen Sitzungstoken und Kontextprüfungen.
- Das Cloud-Dashboard ist passwortgeschützt. Die Fly-Machine erhält keinen öffentlichen Dienstport.
- Dokumente werden nicht automatisch aus reMarkable gelöscht. Geänderte Quellen werden als versionierte Kopien übertragen, um Annotationen zu schützen.
- Teile keine Zugangsdaten, Gerätecodes, privaten URLs, Schuldateien oder ungeprüften Diagnoseberichte in Issues.

Siehe [Datenschutz](PRIVACY.md), [Sicherheit und vertrauliche Meldungen](SECURITY.md), [Teststand](RC_VERIFICATION.md) und [Fremdsoftware](THIRD_PARTY_NOTICES.md). Neue Ausfälle oder Änderungen externer Anbieter sind möglich.

## Entwicklung und Build

Der Quelltext umfasst die lokale Go-App, den reMarkable-Helfer, die Fly-Sync-Anwendung und den Cloudflare Worker. Es wird Go 1.26.11 benötigt. Entwicklerwerkzeuge sind für die Installation fertiger Pakete nicht erforderlich.

```bash
go test -race ./...
go vet ./...
python3 -m unittest discover -s cmd/setup/payload/tests
python3 -m unittest discover -s packaging -p 'test_*.py'
node --test cmd/setup/payload/worker/src/*.test.js
python3 packaging/check_release_definition.py
```

Build-Skripte:

```bash
bash packaging/macos/build.sh                    # auf macOS
bash packaging/windows/build-portable.sh         # ZIPs für Windows x64/ARM64
bash packaging/arch/build.sh                     # Arch x64/ARM64
bash packaging/linux/build.sh amd64              # nativ auf Linux amd64: .deb + Archiv
```

Für Windows-Setup-EXE-Dateien gibt es außerdem `packaging/windows/build.ps1`; es benötigt Windows, PowerShell und Inno Setup und führt dort einen Installations-/Deinstallationstest durch. Hinweise zum Mitwirken stehen in [CONTRIBUTING.md](CONTRIBUTING.md).

Die Go-Modulnamen behalten aus Kompatibilitätsgründen ihre bisherigen Bezeichner. Der Quelltext und die öffentlichen Downloads dieses Projekts liegen ausschließlich in diesem Repository. Der automatische Installer-Updatekanal ist für RC27 noch nicht konfiguriert; lade Updates über die Releases herunter.

## Lizenz

Der projektspezifische Quelltext wird unter [GNU AGPL-3.0](LICENSE) veröffentlicht. Abhängigkeiten behalten ihre eigenen Lizenzen, siehe [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). Die Releases enthalten den entsprechenden Quelltext der gebündelten reMarkable-Integration.

Dieses Projekt ist unabhängig und wird weder von IServ noch von reMarkable unterstützt oder autorisiert. Produktnamen und Marken gehören ihren jeweiligen Inhabern.
