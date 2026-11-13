# Manueller Betatest

Verwende eigene Testkonten und freigegebene Testdateien. Vorhandene produktive Ressourcen dürfen nicht als Wegwerf-Testinstallation verwendet werden.

1. Paket und Prüfsumme auf dem Zielsystem prüfen, vollständig entpacken/installieren und Versionsanzeige kontrollieren.
2. Neue Einrichtung durchlaufen: Systemprüfung, Kostenbestätigung, Fly.io-/Cloudflare-Anmeldung, gegebenenfalls Fly-Codeübergabe, Cloudflare-E-Mail-Bestätigung und workers.dev-Kontoadresse.
3. IServ und reMarkable verbinden, reale Ordnerbäume lesen und ausschließlich kleine Testordner zuordnen.
4. Installation durchführen. Bei einer Unterbrechung dieselbe Einrichtung fortsetzen; keine zweiten Ressourcen oder duplizierten Uploads erzeugen.
5. Testlauf muss null Uploads und null Fehler melden. Automatik bleibt aus.
6. Begrenzten echten Lauf ausdrücklich bestätigen. Ergebnis auf dem reMarkable kontrollieren. Nur mindestens ein bestätigter Upload mit null Fehlern entsperrt die weitere Aktivierung.
7. Vor der Automatik den lokalen Assistenten neu starten, vorhandene Zuordnungen laden, grafisch ändern, speichern und mit einem Testlauf ohne Upload prüfen.
8. Automatik bewusst aktivieren, einen geplanten Lauf kontrollieren und danach testweise pausieren.
9. Wartung, Verbindungsreparatur und redigierten Diagnoseexport testen. Keine Rohdiagnosen in öffentlichen Issues posten.
10. Eine Cloud-Deinstallation nur für eindeutig zugeordnete Wegwerfressourcen ausdrücklich starten. Dokumente auf dem Tablet bleiben bestehen.

Berichte Betriebssystem, Architektur, Version, reproduzierbare Schritte und datensparsame Fehlermeldungen. Zugangsdaten, Dokumentnamen, Schulpfade und Kontokennungen nicht veröffentlichen.
