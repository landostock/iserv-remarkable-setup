# Prüfstand RC27

Stand: 14. November 2026.

## Nachweise

- Go-Tests mit Race Detector und Go vet auf macOS.
- Python-Sync-Tests, Worker-/Dashboard-Tests und Packaging-Tests.
- Release-Definitionsprüfung sowie Cross-Builds für Windows und Linux, jeweils x64 und ARM64.
- Mac-DMG: Prüfsumme, schreibgeschütztes Mounten, universelle Binärdateien, Ad-hoc-Signaturprüfung, Helper-Selbsttest und isolierter Start der lokalen App. Intel-Binary unter Rosetta gestartet.
- Browserprüfung der tatsächlichen Mac-App mit kontrollierten Providerantworten: Einrichtung, Wiederaufnahme, Aktivierungssperre und Ordnereditor. Der Editor lädt Zuordnungen, behandelt falsche Passwörter und abgelehnte Entwürfe, fügt Zuordnungen hinzu, speichert, verwirft und lädt erneut. Desktop- und schmale Ansichten geprüft.
- Linux-Paket: Architektur-/Archiv-/Prüfsummenprüfung sowie isolierte Installer-/Uninstaller-Tests mit Test-Helfern; manipulierte Paketdateien werden abgewiesen.
- Windows-ZIPs: PE-Dateiformat, CPU-Architektur, Archivinhalt und Prüfsummen geprüft.

## Grenzen

Die aktuellen Windows- und Linux-Pakete wurden auf macOS übersetzt. Native Provideranmeldungen, Betriebssystemdialoge und vollständige Cloud-Installationen wurden für diese Pakete noch nicht bestätigt. Der Intel-Test unter Rosetta ersetzt keinen Test auf einem echten Intel-Mac.

Lokale Logik- und Browsertests mit kontrollierten Antworten ersetzen keine Live-Tests mit den vom Nutzer ausgewählten Konten und Ordnern. Automatisierte Prüfungen ändern keine produktiven Cloud-Ressourcen und übertragen keine echten Dokumente.

macOS-Notarisierung und Windows-Herausgebersignaturen fehlen. Das fest gepinnte offizielle Fly-CLI-Binary 0.5.7 hat bekannte Upstream-Scannerbefunde, die vor einer stabilen Freigabe erneut geprüft werden müssen. Die Anwendung verwendet eine inoffizielle reMarkable-Schnittstelle.

Diese öffentliche Quelltextgeschichte beginnt mit einer bereinigten Momentaufnahme. Private Arbeits-, Test- und Diagnosehistorie ist nicht Bestandteil des Repositorys. Veröffentlichte Pakete und Quellarchive werden vor dem Upload auf bekannte Geheimnisse und persönliche Kennungen geprüft. Ein automatischer Scan ist keine absolute Garantie.
