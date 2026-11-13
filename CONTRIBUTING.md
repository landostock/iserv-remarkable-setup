# Mitwirken

Fehlerberichte und Pull Requests sind willkommen. Gib Betriebssystem, CPU-Architektur, App-Version, den betroffenen Schritt und eine Beschreibung zum Reproduzieren an. Verwende Testdateien und fiktive Kontodaten; füge keine vollständigen Diagnoseberichte oder private Schulpfade ungeprüft bei.

Sicherheitslücken bitte über die private GitHub-Meldefunktion melden, nicht in einem öffentlichen Issue. Details stehen in SECURITY.md.

Vor Änderungen die Prüfungen aus der README ausführen. Für Go-Dateien `gofmt` verwenden. Änderungen an Setup, Worker und Sync-Job müssen die Sicherheitssequenz Testlauf → begrenzter Echtlauf → ausdrückliche Automatikfreigabe erhalten. Fehler dürfen weder Berechtigungen erweitern noch einen unbestätigten Lauf als erfolgreich kennzeichnen.

Cloud- oder Uploadtests erfordern eigene, ausdrücklich dafür vorgesehene Testkonten und freigegebene Testdateien. Gewöhnliche Unit-Tests sollen keine realen Providerkonten verändern. Keine Zugangsdaten oder lokalen Zustandsdateien committen.

Mit einem Beitrag bestätigst du, dass du ihn unter der Projektlizenz AGPL-3.0 beisteuern darfst. Urheberrechts- und Lizenzhinweise fremder Komponenten müssen erhalten bleiben.

Die Plattform- und Packaging-Workflows sind bewusste Release-Gates. Ein neuer Push soll keine unnötigen Plattform-Builds auslösen. Auf dem öffentlichen Repository werden Actions zunächst nicht automatisch ausgeführt; lokale Prüfungen und Testgrenzen werden in den Release-Notizen dokumentiert.
