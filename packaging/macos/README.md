# IServ → reMarkable unter macOS installieren

Dieses Paket enthält den grafischen Einrichtungsassistenten `0.9.0-rc27` als universelle macOS-App für Apple Silicon und Intel. Du brauchst weder Homebrew, Go, Node.js, Fly CLI, Wrangler, rclone noch remarquee selbst zu installieren. Der Assistent lädt benötigte Laufzeitwerkzeuge ausschließlich aus festgelegten Quellen und prüft deren SHA-256-Prüfsummen.

RC27 ist ein kontrollierter Betatest, noch keine notarisiert signierte öffentliche Version 1.0. Der Assistent richtet neue Ressourcen in deinen eigenen Fly.io- und Cloudflare-Konten ein. Eine bereits produktiv verwendete Installation darf für diesen Test nicht überschrieben oder gelöscht werden.

## Was danach vorhanden ist

Du erhältst eine passwortgeschützte Webseite, auf der du:

- automatische Synchronisation ein- und ausschalten kannst;
- für jeden Wochentag Zeitfenster und ein Intervall festlegst;
- IServ-Quellordner grafisch reMarkable-Zielordnern zuordnest;
- PDF und/oder EPUB, Dateialter und maximale Dateigröße begrenzt;
- einen sicheren Testlauf ohne Upload startest;
- anschließend bewusst einen begrenzten echten Lauf bestätigst;
- Status und Fehler des letzten Laufs einsehen kannst.

Cloudflare stellt Dashboard und Zeitplan bereit. Zu den gewählten Zeiten startet es eine private Fly-Machine, die freigegebene PDF-/EPUB-Dateien per IServ WebDAV liest und direkt in die reMarkable Cloud überträgt. Das Tablet wird weder gerootet noch verändert.

Die neue Installation startet immer mit **Automatik AUS** und **Testmodus EIN**. Erst ein erfolgreicher Testlauf, danach ein getrennt bestätigter echter Lauf mit mindestens einem erfolgreichen Upload und schließlich eine weitere bewusste Freigabe können die Automatik aktivieren.

## Voraussetzungen

- macOS 13.5 oder neuer auf einem Apple-Silicon- oder Intel-Mac;
- Internetzugang und ein aktueller Browser;
- ein Fly.io-Konto; Fly.io kann Zahlungsinformationen verlangen und nutzungsabhängige Kosten berechnen;
- ein kostenloses Cloudflare-Konto mit bestätigter E-Mail-Adresse;
- IServ-WebDAV-Adresse, Benutzername und Passwort beziehungsweise App-Passwort;
- Zugriff auf das eigene reMarkable-Konto;
- ungefähr 30 bis 60 Minuten für den ersten kontrollierten Durchlauf.

Preise und Bedingungen können sich ändern. Prüfe vor der Installation die aktuellen Angaben von [Fly.io](https://fly.io/docs/about/pricing/) und [Cloudflare](https://developers.cloudflare.com/workers/platform/pricing/). Der Assistent verlangt vor der Ressourcenerstellung eine ausdrückliche Kostenbestätigung.

## 1. Paket prüfen

Du solltest diese beiden Dateien erhalten haben:

```text
IServ-reMarkable-macos.dmg
IServ-reMarkable-macos.dmg.sha256
```

Lege beide in denselben Ordner. Öffne dort über einen Rechtsklick auf den Ordner **Neues Terminal beim Ordner** und prüfe das Disk-Image:

```bash
shasum -a 256 -c IServ-reMarkable-macos.dmg.sha256
```

Nur bei `OK` fortfahren. Bei `FAILED`, einer fehlenden Datei oder einer anderen Prüfsumme nichts öffnen und den Absender informieren.

## 2. App installieren

1. Öffne `IServ-reMarkable-macos.dmg`.
2. Ziehe **IServ reMarkable Setup** auf den im Fenster angezeigten Ordner **Applications**.
3. Warte, bis der Kopiervorgang abgeschlossen ist.
4. Wirf das Disk-Image im Finder aus.
5. Öffne **Programme** und starte **IServ reMarkable Setup** von dort.

Die RC-App ist technisch ad-hoc signiert, aber noch nicht mit einer öffentlichen Developer-ID notarisiert. Falls macOS sie deshalb beim ersten Start blockiert: Öffne **Systemeinstellungen → Datenschutz & Sicherheit**, prüfe dort ausdrücklich den Namen der blockierten App und wähle **Dennoch öffnen**. Deaktiviere Gatekeeper nicht und führe keine Befehle zum Entfernen von Quarantäneattributen aus.

## 3. System prüfen

1. Klicke **Systemprüfung starten**.
2. Prüfe, dass macOS und die Netzwerkziele erreichbar sind.
3. Lies und bestätige den Kostenhinweis.
4. Klicke **Weiter**.

Die Prüfung legt noch keine Fly- oder Cloudflare-Ressourcen an.

## 4. Fly.io und Cloudflare verbinden

1. Klicke **Bei Fly.io anmelden** und bestätige die Anmeldung im Browser.
2. Falls Fly.io einen Code mit „Hand this code back to your terminal“ zeigt, kopiere ihn in das dafür erscheinende Codefeld im Assistenten. Andernfalls ist keine manuelle Codeeingabe nötig.
3. Klicke **Bei Cloudflare anmelden** und erlaube Wrangler den Zugriff.
4. Beide Bereiche müssen ausdrücklich **Verbunden** anzeigen.

Für ein neues Cloudflare-Konto muss die E-Mail-Adresse zuerst bestätigt sein. Falls Cloudflare nach einer `workers.dev`-Adresse fragt, lege dort die **Kontoadresse** an und klicke anschließend im Assistenten auf **Installation fortsetzen**. Die Veröffentlichungsoptionen für den Worker setzt der Assistent. Konten oder Zugangscodes niemals per Chat, E-Mail oder Screenshot weitergeben.

## 5. IServ und reMarkable verbinden

Verwende die von deiner Schule angegebene WebDAV-Adresse. Sie sieht häufig ähnlich aus wie:

```text
https://webdav.DEINE-SCHULE.de
```

Nicht raten: Öffne in IServ **Dateien** beziehungsweise die WebDAV-Anleitung deiner Schule. Gib Adresse, Benutzername und Passwort nur im Assistenten ein. Bei `401 Unauthorized` Adresse und Benutzernamen kontrollieren und prüfen, ob die Schule ein App-Passwort verlangt oder WebDAV gesperrt hat.

Für reMarkable öffnet der Assistent die offizielle Verbindungsseite. Erzeuge dort den einmaligen Gerätecode und gib ausschließlich diesen Code in den Assistenten ein.

## 6. Ordner grafisch zuordnen

1. Navigiere links durch den geladenen IServ-Ordnerbaum und wähle die Quelle.
2. Navigiere rechts durch den reMarkable-Ordnerbaum und wähle das Ziel.
3. Klicke **Ausgewählte Ordner verknüpfen**.
4. Wiederhole das bei Bedarf für weitere Ordner.

Die Zuordnung liest nur Ordnerstrukturen und lädt noch keine Dokumente hoch. Falls eine große reMarkable-Bibliothek vorübergehend nicht geladen werden kann, **Neu laden** verwenden. Zugangsdaten nicht neu eingeben und die Installation nicht löschen.

## 7. Zeitplan und Grenzen festlegen

Wähle Wochentage, Zeitfenster, Intervall, PDF/EPUB, Rückblick und maximale Dateigröße. Beginne konservativ mit wenigen Testdateien. Der Installer begrenzt den ersten echten Lauf zusätzlich auf höchstens fünf Uploadversuche.

## 8. Sicher installieren und prüfen

1. Klicke **Installation starten**.
2. Schließe die Browseranmeldungen bei Fly.io oder Cloudflare vollständig ab und kehre zum Assistenten zurück.
3. Erledige bei einer angehaltenen Installation nur die angezeigte Voraussetzung und klicke **Installation fortsetzen**. Nicht deinstallieren und keine Cloud-Ressourcen manuell löschen.
4. Der Assistent startet abschließend automatisch einen Testlauf. Die Fly-Machine prüft den IServ-Zugriff und liest den reMarkable-Ordnerbaum. Die Automatik bleibt aus und der Test überträgt keine Dateien an reMarkable.

Fly.io kann beim Aktualisieren einer Machine kurz `machine getting replaced` melden. Der Assistent wartet in diesem Fall und versucht den sicheren Start erneut. Dauert die Provideraktualisierung ungewöhnlich lange, bleibt die Installation sicher angehalten und kann ohne Neuinstallation fortgesetzt werden.

## 9. Begrenzten echten Lauf bestätigen

Nach dem erfolgreichen Testlauf bleibt die Automatik aus. Bestätige nur dann einen begrenzten echten Lauf, wenn Quell- und Zielordner stimmen. Prüfe anschließend, dass mindestens eine erwartete Datei hochgeladen wurde, keine unerwartete Datei erschien und der Assistent null Fehler meldet.

Nur ein echter Lauf mit mindestens einem erfolgreichen Upload und null Fehlern entsperrt die getrennte Automatikfreigabe. Bei einem Fehler Automatik ausgeschaltet lassen und die redigierte Diagnose exportieren.

## 10. Automatik bewusst aktivieren

Prüfe Zeitplan und Ordnerzuordnungen ein letztes Mal und aktiviere die Automatik erst danach mit der eigenen Bestätigung. Der Mac kann anschließend ausgeschaltet werden; Dashboard und Zeitgeber laufen in den eigenen Cloudkonten.

## Spätere Bedienung

Starte die App aus **Programme**. Sie kann eine vorhandene Installation erkennen, das Dashboard öffnen, Zugangsdaten reparieren, Cloud-Verbindungen erneuern, Updates durchführen und eine redigierte Diagnose exportieren. Manuelle Fly-, Wrangler-, rclone- oder remarquee-Befehle aus älteren Anleitungen sind nicht erforderlich.

Der lokale Einrichtungsstand liegt unter:

```text
~/Library/Application Support/IServ-reMarkable-Setup/
```

Dieses Verzeichnis nicht löschen: Es ermöglicht die sichere Wiederaufnahme und enthält die Installationskennungen.

### Ordnerzuordnungen nach der Einrichtung ändern

1. Starte die lokale App und öffne **Ordnerverknüpfungen bearbeiten** in der Verwaltung der vorhandenen Installation.
2. Gib dein aktuelles Dashboard-Passwort ein und klicke **Gespeicherte Zuordnungen laden**.
3. Verbinde unter **Ordnerzugang für diese Sitzung verbinden** IServ und reMarkable, falls sie lokal noch nicht verbunden sind. Diese Verbindung dient dem Durchsuchen; sie ersetzt keine Cloud-Zugangsdaten.
4. Wähle links den IServ-Quellordner und rechts den reMarkable-Zielordner. Klicke **Ausgewählte Ordner verknüpfen**. Entferne nicht mehr benötigte Testzuordnungen über **Entfernen**.
5. Klicke **Zuordnungen speichern**. Zeitplan und Dateifilter werden aus dem aktuellen Dashboard übernommen. Speichern startet keinen Lauf und schaltet die Automatik nicht ein.
6. Prüfe die neuen Zuordnungen mit dem **Testlauf ohne Upload**. Aktiviere die Automatik anschließend bewusst im Abschnitt **Sichere Freigabe abschließen**.

Bei bereits aktiver Automatik pausiere sie vor dem Speichern im Dashboard und warte das Ende eines laufenden Jobs ab. Bei anderweitig geänderten Zuordnungen lade den aktuellen Stand neu und prüfe deine Auswahl erneut.

Für die vorhandenen RC26-Cloud-Komponenten ist beim Wechsel auf RC27 kein Cloud-Update nötig: Der Installer prüft die Übereinstimmung des eingebetteten Cloud-Codes. Ein bereits bestätigter Echtlauf bleibt erhalten. Ältere Cloud-Versionen benötigen weiterhin das ausdrückliche Cloud-Update.

## Lokale App entfernen

Beende den Assistenten und verschiebe **IServ reMarkable Setup** aus **Programme** in den Papierkorb. Das lokale Zustandsverzeichnis sowie Fly- und Cloudflare-Ressourcen bleiben absichtlich bestehen, damit keine laufende Synchronisation unbeabsichtigt ausfällt. Die vollständige Cloud-Deinstallation sollte nur über die dafür vorgesehene Wartungsfunktion erfolgen.

## Häufige Probleme

| Anzeige | Vorgehen |
| --- | --- |
| Prüfsumme `FAILED` | Nichts öffnen; Dateien erneut sicher übertragen. |
| macOS blockiert die RC-App | Unter **Datenschutz & Sicherheit** den Namen prüfen und einmalig **Dennoch öffnen** wählen. |
| IServ `401 Unauthorized` | WebDAV-Adresse, Benutzername und gegebenenfalls App-Passwort prüfen. |
| Cloudflare verlangt E-Mail-Bestätigung | Bestätigungslink öffnen, danach dieselbe Installation fortsetzen. |
| Cloudflare verlangt `workers.dev` | Einmalig die Kontoadresse anlegen und im Assistenten fortsetzen. |
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
