# IServ → reMarkable unter Arch Linux installieren

Dieses Paket enthält den grafischen Einrichtungsassistenten `0.9.0-rc27` für Arch Linux und Arch Linux ARM auf x86-64- beziehungsweise ARM64-Rechnern. Du brauchst weder Go, Node.js, Fly CLI, Wrangler, rclone noch remarquee selbst zu installieren. Der Assistent lädt benötigte Laufzeitwerkzeuge ausschließlich aus festgelegten Quellen und prüft deren SHA-256-Prüfsummen.

RC27 ist ein kontrollierter Betatest, noch keine signierte öffentliche Version 1.0. Der Assistent richtet neue Ressourcen in deinen eigenen Fly.io- und Cloudflare-Konten ein. Eine bereits produktiv verwendete Installation darf für diesen Test nicht überschrieben oder gelöscht werden.

## Was danach vorhanden ist

Du erhältst eine passwortgeschützte Webseite, auf der du:

- automatische Synchronisation ein- und ausschalten kannst;
- für jeden Wochentag eigene Zeitfenster und ein Intervall festlegst;
- IServ-Quellordner grafisch reMarkable-Zielordnern zuordnest;
- PDF und/oder EPUB, Dateialter und maximale Dateigröße begrenzt;
- einen sicheren Testlauf ohne Upload startest;
- anschließend bewusst einen begrenzten echten Lauf bestätigst;
- Status und Fehler des letzten Laufs einsehen kannst.

Cloudflare stellt Dashboard und Zeitplan bereit. Zu den gewählten Zeiten startet es eine private Fly-Machine, die freigegebene PDF-/EPUB-Dateien per IServ WebDAV liest und direkt in die reMarkable Cloud überträgt. Das Tablet wird weder gerootet noch verändert.

Die neue Installation startet immer mit **Automatik AUS** und **Testmodus EIN**. Erst ein erfolgreicher Testlauf, danach ein getrennt bestätigter echter Lauf mit mindestens einem erfolgreichen Upload und schließlich eine weitere bewusste Freigabe können die Automatik aktivieren.

## Voraussetzungen

- ein vollständig aktualisiertes Arch Linux oder Arch Linux ARM mit grafischer Oberfläche auf `x86_64` oder `aarch64`/ARM64;
- Internetzugang und ein aktueller Browser;
- ein Fly.io-Konto; Fly.io kann Zahlungsinformationen verlangen und nutzungsabhängige Kosten berechnen;
- ein kostenloses Cloudflare-Konto mit bestätigter E-Mail-Adresse;
- IServ-WebDAV-Adresse, Benutzername und Passwort beziehungsweise App-Passwort;
- Zugriff auf das eigene reMarkable-Konto;
- ungefähr 30 bis 60 Minuten für den ersten kontrollierten Durchlauf.

Preise und Bedingungen können sich ändern. Prüfe vor der Installation die aktuellen Angaben von [Fly.io](https://fly.io/docs/about/pricing/) und [Cloudflare](https://developers.cloudflare.com/workers/platform/pricing/). Der Assistent verlangt vor der Ressourcenerstellung eine ausdrückliche Kostenbestätigung.

## 1. Paket unverändert übertragen

Du solltest diese beiden Dateien erhalten haben:

```text
IServ-reMarkable-Arch-Linux-0.9.0-rc27.tar.gz
IServ-reMarkable-Arch-Linux-0.9.0-rc27.tar.gz.sha256
```

Lege beide in denselben Ordner. Öffne dort ein Terminal und prüfe zuerst das übertragene Archiv:

```bash
sha256sum --check IServ-reMarkable-Arch-Linux-0.9.0-rc27.tar.gz.sha256
```

Nur bei `OK` fortfahren. Bei `FAILED`, einer fehlenden Datei oder einer anderen Prüfsumme nichts starten und den Absender informieren.

## 2. Entpacken und installieren

```bash
tar -xzf IServ-reMarkable-Arch-Linux-0.9.0-rc27.tar.gz
cd IServ-reMarkable-Arch-Linux-0.9.0-rc27
chmod +x install.sh uninstall.sh
./install.sh
```

Die Installation benötigt kein `sudo`. Sie prüft alle enthaltenen Dateien erneut, erkennt die CPU-Architektur, führt einen Selbsttest des reMarkable-Helfers aus und installiert nur für das aktuelle Benutzerkonto.

Falls `xdg-open` oder Zertifikate fehlen, aktualisiere das System vollständig und installiere die Arch-Pakete:

```bash
sudo pacman -Syu --needed xdg-utils ca-certificates
```

Arch unterstützt keine sicheren Teilaktualisierungen; verwende deshalb den vollständigen `-Syu`-Befehl. Danach `./install.sh` erneut ausführen. Keine TLS- oder Zertifikatsprüfung abschalten.

## 3. Assistent starten

Öffne im Anwendungsmenü **IServ → reMarkable Setup**. Alternativ:

```bash
~/.local/bin/iserv-remarkable-setup
```

Der Assistent öffnet eine zufällige lokale Adresse auf `127.0.0.1`. Diese Oberfläche ist nur auf diesem Rechner erreichbar und benötigt zusätzlich ein zufälliges Sitzungstoken. Das startende Terminalfenster beziehungsweise der gestartete Prozess muss während der Einrichtung geöffnet bleiben.

## 4. Systemprüfung

1. Klicke **Systemprüfung starten**.
2. Prüfe, dass Arch Linux und die Netzwerkziele erreichbar sind.
3. Lies und bestätige den Kostenhinweis.
4. Klicke **Weiter**.

Die Anzeige prüft lediglich Erreichbarkeit und Paketbestandteile. Sie legt noch keine Fly- oder Cloudflare-Ressourcen an.

## 5. Fly.io und Cloudflare verbinden

1. Klicke **Bei Fly.io anmelden** und bestätige die Anmeldung im Browser.
2. Falls Fly.io einen Code mit „Hand this code back to your terminal“ zeigt, kopiere ihn in das dafür erscheinende Codefeld im Assistenten. Andernfalls ist keine manuelle Codeeingabe nötig.
3. Klicke **Bei Cloudflare anmelden** und erlaube Wrangler den Zugriff.
4. Beide Bereiche müssen ausdrücklich **Verbunden** anzeigen.

Für ein neues Cloudflare-Konto muss die E-Mail-Adresse zuerst bestätigt sein. Cloudflare kann außerdem einmalig die Einrichtung einer `workers.dev`-Adresse verlangen. Aktiviere dabei die Produktionsadresse. Vorschau-/Preview-URLs werden vom Installer nicht benötigt und bleiben deaktiviert.

Konten oder Zugangscodes niemals per Chat, E-Mail oder Screenshot weitergeben.

## 6. IServ und reMarkable verbinden

Für IServ benötigst du die von deiner Schule angegebene WebDAV-Adresse. Sie sieht häufig ähnlich aus wie:

```text
https://webdav.DEINE-SCHULE.de
```

Nicht raten: Öffne in IServ **Dateien** beziehungsweise die WebDAV-Anleitung deiner Schule. Gib Adresse, Benutzername und Passwort nur im Assistenten ein. Bei `401 Unauthorized` Adresse und Benutzernamen kontrollieren und prüfen, ob die Schule ein App-Passwort verlangt oder WebDAV gesperrt hat.

Für reMarkable öffnet der Assistent die offizielle Verbindungsseite. Erzeuge dort den einmaligen Gerätecode und gib ausschließlich diesen Code in den Assistenten ein. Das daraus entstehende Kontotoken wird nicht in der lokalen Installationsbeschreibung gespeichert.

## 7. Ordner grafisch zuordnen

1. Navigiere links durch den geladenen IServ-Ordnerbaum.
2. Wähle den gewünschten IServ-Quellordner.
3. Navigiere rechts durch den reMarkable-Ordnerbaum.
4. Wähle das Ziel und klicke **Ausgewählte Ordner verknüpfen**.
5. Wiederhole das bei Bedarf für weitere Ordner.

Die Zuordnung liest Ordnerstrukturen, lädt aber noch keine Dokumente hoch. Falls eine große reMarkable-Bibliothek vorübergehend nicht geladen werden kann, **Neu laden** verwenden. Zugangsdaten nicht neu eingeben und die Installation nicht löschen.

## 8. Zeitplan und Grenzen festlegen

Wähle Wochentage, Zeitfenster, Intervall, PDF/EPUB, Rückblick und maximale Dateigröße. Beginne konservativ mit wenigen Testdateien. Der Installer begrenzt den ersten echten Lauf zusätzlich auf höchstens fünf Uploadversuche.

## 9. Sicher installieren und prüfen

1. Klicke **Installation starten**.
2. Browserfenster für Fly.io oder Cloudflare vollständig abschließen und danach zum Assistenten zurückkehren.
3. Bei einer angehaltenen, aber fortsetzbaren Installation die angezeigte Voraussetzung erledigen und **Installation fortsetzen** klicken. Nicht deinstallieren und keine Cloud-Ressourcen manuell löschen.
4. Der Assistent startet abschließend automatisch einen reinen Testlauf ohne Upload.

Fly.io kann beim Aktualisieren einer Machine kurz `machine getting replaced` melden. Der Assistent wartet in genau diesem Fall automatisch und versucht den sicheren Start erneut. Dauert die Provideraktualisierung ungewöhnlich lange, bleibt die Installation sicher angehalten und kann erneut fortgesetzt werden, ohne die bereits bereitgestellte Fly-Anwendung nochmals zu deployen.

## 10. Begrenzten echten Lauf bestätigen

Nach dem erfolgreichen Testlauf bleibt die Automatik aus. Bestätige nur dann einen begrenzten echten Lauf, wenn Quell- und Zielordner stimmen. Prüfe anschließend auf dem reMarkable:

- mindestens eine erwartete Datei wurde hochgeladen;
- keine unerwarteten Dateien erschienen;
- im Assistenten werden null Fehler gemeldet.

Nur ein echter Lauf mit mindestens einem erfolgreichen Upload und null Fehlern entsperrt die getrennte Automatikfreigabe. Bei einem Fehler Automatik ausgeschaltet lassen, Diagnose exportieren und Absender informieren.

## 11. Automatik bewusst aktivieren

Prüfe Zeitplan und Ordnerzuordnungen ein letztes Mal. Aktiviere die Automatik erst danach mit der eigenen Bestätigung. Der Arch-Rechner kann anschließend ausgeschaltet werden; Dashboard und Zeitgeber laufen in den eigenen Cloudkonten.

## Spätere Bedienung und Wartung

RC27 enthält auch nach abgeschlossener Einrichtung den grafischen Editor: **Ordnerverknüpfungen bearbeiten → Gespeicherte Zuordnungen laden**. Authentifiziere dich mit dem Dashboard-Passwort und verbinde bei Bedarf unter **Ordnerzugang für diese Sitzung verbinden** IServ und reMarkable zum lokalen Durchsuchen. Wähle links die Quelle und rechts das Ziel, verknüpfe sie, entferne alte Testzuordnungen und klicke **Zuordnungen speichern**. Speichern startet keinen Lauf und aktiviert keine Automatik. Prüfe die neuen Zuordnungen danach mit dem Testlauf ohne Upload. Eine bereits aktive Automatik vorher im Dashboard pausieren.

Bei vorhandenen RC26-Cloud-Komponenten ist für RC27 kein Cloud-Update erforderlich, solange der eingebettete Cloud-Code unverändert ist; der Installer prüft dessen Fingerabdruck. Dein bestätigter Echtlauf bleibt erhalten. Ältere Cloud-Versionen brauchen weiterhin ein ausdrückliches Update.

Über die installierte Anwendung kannst du die bestehende Installation erkennen, das Dashboard öffnen, Zugangsdaten reparieren, Cloud-Verbindungen erneuern, Updates durchführen und eine redigierte Diagnose exportieren. Manuelle Fly-, Wrangler-, rclone- oder remarquee-Befehle aus älteren Anleitungen sind nicht mehr erforderlich und sollten nicht verwendet werden.

Wichtige lokale Pfade:

```text
Programm:       ~/.local/lib/iserv-remarkable-setup/
Starter:        ~/.local/bin/iserv-remarkable-setup
Einrichtungsstand: ~/.config/IServ-reMarkable-Setup/
```

Bei gesetztem `XDG_CONFIG_HOME` liegt der Einrichtungsstand entsprechend dort. Dieses Zustandsverzeichnis nicht löschen: Es ermöglicht die sichere Wiederaufnahme und enthält die nicht geheimen Installationskennungen.

## Deinstallation der lokalen Anwendung

Im Terminal genügt auch nach dem Löschen des übertragenen Paketordners:

```bash
~/.local/bin/iserv-remarkable-uninstall
```

Der Uninstaller entfernt Programm, Starter, Menüeintrag und Symbol. Einrichtungsstand und Wiederaufnahmejournal bleiben absichtlich erhalten. Cloud-Ressourcen werden nicht automatisch gelöscht, damit keine bestehende Synchronisation unbeabsichtigt ausfällt.

## Häufige Probleme

| Anzeige | Vorgehen |
| --- | --- |
| Paketprüfung `FAILED` | Nichts starten; Dateien erneut sicher übertragen. |
| `Nicht unterstützte CPU-Architektur` | Nur x86-64 und ARM64 sind in diesem Paket enthalten. |
| Browser öffnet sich nicht | `xdg-utils` installieren und die im Terminal angezeigte lokale Adresse manuell öffnen. |
| IServ `401 Unauthorized` | WebDAV-Adresse, Benutzername und gegebenenfalls App-Passwort prüfen. |
| Cloudflare verlangt E-Mail-Bestätigung | Bestätigungslink öffnen, danach dieselbe Installation fortsetzen. |
| Cloudflare verlangt `workers.dev` | Einmalig Produktionsadresse einrichten; Preview nicht aktivieren. |
| Fly.io zeigt einen Übergabecode | Code in das sichtbare Fly-Codefeld des Assistenten einfügen. |
| Installation ist bei 58/68/94 % angehalten | Hauptmeldung lesen, Voraussetzung beheben und fortsetzen; nicht neu installieren. |
| `machine getting replaced` | Der Assistent wartet automatisch; bei Zeitüberschreitung erneut fortsetzen. |
| Unterbrochene Installation oder fehlgeschlagener Upload aus RC15–RC22 | RC27 überinstallieren und dieselbe Installation fortsetzen. Bei einer vorhandenen Testinstallation zuerst **Cloud-Komponenten aktualisieren**. |
| reMarkable-Ordner laden vorübergehend nicht | Einige Minuten warten und **Neu laden** verwenden. |

## Datenschutz und Sicherheit

- Synchronisiere nur eigene oder ausdrücklich freigegebene Dateien. Bei schulischen Vorgaben zu externen Clouds vorher die erforderliche Erlaubnis einholen.
- Dashboard-Adresse, Dashboard-Passwort, IServ-Passwort, Gerätecodes und Tokens nicht teilen.
- Die Fly-Machine erhält keinen öffentlichen Dienstport.
- IServ- und reMarkable-Zugangsdaten werden als Provider-Secrets übergeben und nicht im Dashboard an den Browser ausgeliefert.
- Dokumente werden nicht automatisch aus reMarkable gelöscht, wenn sie aus IServ verschwinden.
- Geänderte Quelldateien werden als neue versionierte Kopie übertragen, um vorhandene Annotationen nicht still zu überschreiben.
- Diagnoseexporte werden bestmöglich redigiert; vor Weitergabe trotzdem kurz prüfen.

## Weiterführende Quellen

- [IServ WebDAV](https://doku.iserv.de/cookbook/external/webdav/)
- [Fly.io Preise](https://fly.io/docs/about/pricing/)
- [Fly.io Sicherheit und Tokens](https://fly.io/docs/security/tokens/)
- [Cloudflare Workers](https://developers.cloudflare.com/workers/)
- [Cloudflare Cron Triggers](https://developers.cloudflare.com/workers/configuration/cron-triggers/)
- [reMarkable Geräteverbindung](https://my.remarkable.com/device/browser/connect)
