# IServ → reMarkable RC27 für Windows – portables Paket

Dieses ZIP enthält denselben grafischen Setup- und Wartungsassistenten wie die Mac-App RC27, einschließlich des grafischen Ordnereditors. Es benötigt keine zusätzliche lokale Installation von Go, Node, Fly CLI oder Wrangler. Die benötigten Laufzeitwerkzeuge lädt der Assistent mit Prüfsummenprüfung.

## Paket wählen und starten

Für einen üblichen Windows-PC mit Intel- oder AMD-Prozessor verwende **amd64**. Für Windows auf ARM verwende **arm64**. Beide Pakete benötigen ein aktuelles 64-Bit-Windows mit Browser und Internetzugang.

1. Beende einen eventuell noch laufenden alten Setup-Assistenten.
2. Prüfe bei Bedarf die ZIP-Prüfsumme mit PowerShell: `Get-FileHash .\DATEINAME.zip -Algorithm SHA256`. Vergleiche den Wert mit der gleichnamigen `.sha256`-Datei.
3. Wähle beim ZIP **Alle extrahieren**. Starte die Anwendung nicht direkt aus der ZIP-Vorschau.
4. Behalte den vollständig entpackten Ordner an einem festen Ort. Die beiden EXE-Dateien müssen nebeneinander liegen.
5. Doppelklicke **iserv-remarkable-setup.exe**. Der Browser öffnet den lokalen Assistenten auf `127.0.0.1`. Oben muss **0.9.0-rc27** stehen.

Es werden kein Startmenüeintrag und kein Windows-Uninstaller eingerichtet. Ein geöffnetes Konsolenfenster gehört zum Assistenten; lasse es während der Arbeit geöffnet. Zum Beenden verwende **Setup beenden**. Für den nächsten Start öffne wieder dieselbe EXE-Datei.

Das Paket ist nicht mit einem Windows-Herausgeberzertifikat signiert. Falls Windows eine Warnung zeigt, prüfe Herkunft und Prüfsumme. Schalte keinen Virenschutz aus und ändere keine systemweiten Sicherheitseinstellungen.

## Bestehende Einrichtung behalten

Das portable Programm verwendet denselben Einrichtungsstand wie die installierte Windows-App unter `%APPDATA%\IServ-reMarkable-Setup`. Lösche dieses Verzeichnis nicht. Die alte App muss nicht deinstalliert werden; starte für RC27 ausdrücklich die neue EXE aus diesem Ordner.

Ist bereits RC26 in der Cloud installiert, ist für den Ordnereditor kein Cloud-Update nötig. Der erfolgreiche Echtlauf bleibt erhalten. Ältere Cloud-Komponenten brauchen weiterhin ein ausdrückliches Update über die Verwaltung der vorhandenen Einrichtung.

## Ordnerverknüpfungen ändern

1. Öffne **Ordnerverknüpfungen bearbeiten** in der lokalen Verwaltung der vorhandenen Installation.
2. Gib dein Dashboard-Passwort ein und klicke **Gespeicherte Zuordnungen laden**.
3. Verbinde bei Bedarf unter **Ordnerzugang für diese Sitzung verbinden** IServ und reMarkable. Für reMarkable wird ein neuer Einmalcode verwendet; diese Sitzung dient nur dem Ordnerbrowser.
4. Wähle links die IServ-Quelle und rechts das reMarkable-Ziel, bestätige jeweils die Auswahl und klicke **Ausgewählte Ordner verknüpfen**. Entferne alte Testzuordnungen über **Entfernen**.
5. Klicke **Zuordnungen speichern**. Zeitplan und Filter bleiben erhalten. Speichern startet keinen Lauf und aktiviert keine Automatik.
6. Prüfe die Auswahl mit dem **Testlauf ohne Upload**. Aktiviere danach die Automatik bewusst über **Sichere Freigabe abschließen**.

Eine bereits aktive Automatik vorher im Dashboard pausieren und laufende Jobs abwarten.

## Neue Einrichtung

Folge den Schritten im Assistenten: Systemprüfung und Kostenhinweis, eigene Fly.io-/Cloudflare-Konten, IServ-/reMarkable-Verbindungen, Ordnerzuordnung, Zeitplan, Installation. Neue Cloudflare-Konten müssen die E-Mail-Adresse bestätigen und gegebenenfalls eine workers.dev-Kontoadresse anlegen. Die Einrichtung startet mit einem Testlauf ohne Upload und ausgeschalteter Automatik. Erst danach bestätigst du einen begrenzten echten Lauf und anschließend die Automatik getrennt.

Bei einer angehaltenen Installation die Hauptmeldung beachten und **Installation fortsetzen** verwenden. Nicht den Einrichtungsstand oder Cloud-Ressourcen löschen. Zur Diagnose den redigierten Bericht exportieren und vor Weitergabe prüfen.

## Kosten, Datenschutz und Teststand

Fly.io und Cloudflare sind externe Dienste in eigenen Konten. Fly kann Zahlungsinformationen verlangen und Kosten berechnen. Die reMarkable-Cloud-Anbindung nutzt eine inoffizielle Schnittstelle. Synchronisiere nur Dateien, die du dafür verwenden darfst. Passwörter, Tokens und lokale Zustandsdateien nicht weitergeben.

Diese EXE-Dateien wurden auf macOS aus dem RC27-Quellstand für Windows übersetzt. Dateiformat, CPU-Ziel und Archivinhalt wurden geprüft; ein nativer Windows-Test dieses Pakets steht aus. Der gemeinsame grafische Ablauf wurde mit der Mac-App geprüft. Das ist keine Zusage vollständiger Fehlerfreiheit auf Windows.
