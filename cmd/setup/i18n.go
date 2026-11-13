package main

import (
	"fmt"
	"regexp"
	"strings"
)

// translateSetupMessage keeps user-facing setup errors and background messages
// readable when the English UI is active. Provider CLI output is left intact:
// it is already normally English and changing arbitrary external text would be
// risky and could hide useful diagnostics.
func translateSetupMessage(message, lang string) string {
	if lang != "en" {
		return message
	}
	raw := strings.TrimSpace(message)
	exact := map[string]string{
		"Die Zuordnungen wurden inzwischen geändert. Lade sie erneut und prüfe deine Auswahl":    "The mappings changed in the meantime. Reload them and review your selection",
		"Pausiere zuerst die Automatik im Dashboard, bevor du Ordnerzuordnungen änderst":         "Pause automation in the dashboard before changing folder mappings",
		"Warte, bis der aktuelle Synchronisationslauf abgeschlossen ist":                         "Wait until the current sync run has completed",
		"Ordnerzuordnungen konnten nicht gespeichert werden":                                     "Folder mappings could not be saved",
		"Dashboard-Anmeldung oder Verbindung fehlgeschlagen":                                     "Dashboard sign-in or connection failed",
		"Das Dashboard hat die gespeicherten Zuordnungen nicht bestätigt. Lade den Stand erneut": "The dashboard did not confirm the saved mappings. Reload the state",
		"Testlauf hat innerhalb von 20 Minuten keinen Abschluss gemeldet":                        "The test run did not report completion within 20 minutes",
		"ungültige Fly-Region":                                                                 "Invalid Fly region",
		"ungültige Fly-Organisation":                                                           "Invalid Fly organization",
		"ungültige Fly-App-Kennung":                                                            "Invalid Fly app identifier",
		"die ausgewählte Fly-Organisation ist nicht mehr verfügbar":                            "The selected Fly organization is no longer available",
		"wähle nach der Fly.io-Anmeldung eine Organisation aus":                                "Choose an organization after signing in to Fly.io",
		"Fly-Organisationsliste ist leer oder zu groß":                                         "The Fly organization list is empty or too large",
		"Fly-Organisationsliste enthält mehrere JSON-Ergebnisse":                               "The Fly organization list contains multiple JSON results",
		"Fly-Organisationsliste ist ungültig":                                                  "The Fly organization list is invalid",
		"Fly-Konto enthält keine oder zu viele Organisationen":                                 "The Fly account has no organizations or too many organizations",
		"Fly-Organisationsliste enthält einen ungültigen Eintrag":                              "The Fly organization list contains an invalid entry",
		"ungültiges Intervall":                                                                 "Invalid interval",
		"Rückblick muss 1–3650 Tage betragen":                                                  "Lookback must be between 1 and 3650 days",
		"Dateigröße muss 1–95 MB betragen":                                                     "File size must be between 1 and 95 MB",
		"Dashboard-Passwort muss mindestens 16 Zeichen haben":                                  "Dashboard password must be at least 16 characters",
		"ungültiger Wochenplan":                                                                "Invalid weekly schedule",
		"ungültiges Zeitfenster":                                                               "Invalid time window",
		"mindestens ein Dateityp erforderlich":                                                 "At least one file type is required",
		"ungültiger Dateityp":                                                                  "Invalid file type",
		"1–20 Ordnerzuordnungen erforderlich":                                                  "Between 1 and 20 folder mappings are required",
		"Zuordnungsnamen müssen eindeutig sein":                                                "Mapping names must be unique",
		"ungültiger Ordnerpfad":                                                                "Invalid folder path",
		"keine bestehende Installation gefunden":                                               "No existing installation was found",
		"Sicherheitsprüfung: erwartet genau eine Fly-Machine":                                  "Safety check: expected exactly one Fly Machine",
		"Bestätigung DELETE fehlt":                                                             "DELETE confirmation is missing",
		"Fly app token missing":                                                                "Fly app token is missing",
		"dashboard URL missing":                                                                "Dashboard URL is missing",
		"Wrangler is not prepared":                                                             "Cloudflare tooling is not prepared",
		"für diese Entwicklungsdatei ist noch kein öffentlicher Release-Kanal konfiguriert":    "This development build does not have a public release channel yet",
		"für diese Plattform gibt es noch kein Updatepaket":                                    "No update package is available for this platform yet",
		"Releasepaket oder Prüfsumme fehlt":                                                    "Release package or checksum is missing",
		"ungültige Release-Prüfsumme":                                                          "Invalid release checksum",
		"Release-Prüfsumme stimmt nicht":                                                       "Release checksum does not match",
		"Testlauf hat innerhalb von 7 Minuten keinen Abschluss gemeldet":                       "The test run did not report completion within 7 minutes",
		"Dashboard-Passwort fehlt":                                                             "Dashboard password is required",
		"Dashboard-Einstellungen sind unvollständig":                                           "Dashboard settings are incomplete",
		"Neue Verbindungsdaten werden sicher als Fly-Secrets übernommen …":                     "Securely applying the new connection credentials as Fly secrets …",
		"Verbindungsdaten wurden aktualisiert.":                                                "Connection credentials were updated.",
		"Dashboard-Passwort wird sicher ersetzt …":                                             "Securely replacing dashboard password …",
		"Dashboard-Passwort aktualisiert. Bestehende Dashboard-Sitzungen sind damit ungültig.": "Dashboard password updated. Existing dashboard sessions are now invalid.",
		"Sicherer Wartungs-Testlauf wird gestartet – Uploads sind vorübergehend zwangsweise deaktiviert …":           "Starting safe maintenance test — uploads are temporarily forced off …",
		"Sicherer Wartungs-Testlauf abgeschlossen; die vorherigen Dashboard-Einstellungen wurden wiederhergestellt.": "Safe maintenance test completed; the previous dashboard settings were restored.",
		"Datenschutzfreundlicher Diagnosebericht wurde erstellt.":                                                    "Privacy-friendly diagnostic report was created.",
		"noch kein Diagnosebericht erstellt":                                                                         "No diagnostic report has been created yet",
		"Dashboard-Adresse muss eine gültige HTTPS-Adresse sein":                                                     "Dashboard address must be a valid HTTPS URL",
		"Dashboard-Adresse darf keine Parameter enthalten":                                                           "Dashboard address must not contain query parameters",
		"Dashboard-Adresse muss auf die Hauptadresse des Dashboards zeigen":                                          "Dashboard address must point to the dashboard root",
		"Dashboard-Passwort ist nicht korrekt":                                                                       "Dashboard password is incorrect",
		"Wiederherstellungsdaten sind ungültig":                                                                      "Recovery data is invalid",
		"Wiederherstellungsdaten gehören nicht zu diesem Dashboard":                                                  "Recovery data does not belong to this dashboard",
		"Wiederherstellungsdaten sind unvollständig":                                                                 "Recovery data is incomplete",
		"Bestehende Installation wurde auf diesem Computer sicher wieder verbunden.":                                 "Existing installation was securely reconnected on this computer.",
		"Fly.io-Werkzeug wird sicher heruntergeladen …":                                                              "Securely downloading Fly.io tooling …",
		"Cloudflare-Laufzeit wird sicher heruntergeladen …":                                                          "Securely downloading Cloudflare runtime …",
		"Sync-Software wird aktualisiert …":                                                                          "Updating sync software …",
		"Dashboard-Software wird aktualisiert …":                                                                     "Updating dashboard software …",
		"Sicherer Testlauf wird gestartet – dabei wird nichts hochgeladen …":                                         "Starting safe test run — nothing will be uploaded …",
		"Fly.io-Anmeldung lieferte eine ungültige Antwort":                                                           "Fly.io sign-in returned an invalid response",
		"Fly.io-Anmeldung lieferte ungültige Sitzungsdaten":                                                          "Fly.io sign-in returned invalid session data",
		"Fly.io-Anmeldung wurde nicht rechtzeitig abgeschlossen":                                                     "Fly.io sign-in was not completed in time",
		"Fly.io-Anmeldung lieferte keine gültige Berechtigung":                                                       "Fly.io sign-in did not return a valid credential",
	}
	if v, ok := exact[raw]; ok {
		return v
	}

	prefixes := [][2]string{
		{"Prüfsummen konnten nicht geladen werden:", "Checksums could not be downloaded:"},
		{"keine gültige SHA-256-Prüfsumme für ", "No valid SHA-256 checksum found for "},
		{"Sicherheitsprüfung für ", "Security verification failed for "},
		{"CPU-Architektur ", "CPU architecture "},
		{"Betriebssystem ", "Operating system "},
		{"Download meldet HTTP ", "Download returned HTTP "},
		{"Fly-Update fehlgeschlagen:", "Fly update failed:"},
		{"Dashboard-Update fehlgeschlagen:", "Dashboard update failed:"},
		{"Fly app:", "Fly app:"},
		{"Fly-Organisationen:", "Fly organizations:"},
		{"Fly volume:", "Fly volume:"},
		{"Fly secrets:", "Fly secrets:"},
		{"Fly deployment:", "Fly deployment:"},
		{"Cloudflare KV:", "Cloudflare KV:"},
		{"Fly app token:", "Fly app token:"},
		{"Cloudflare deployment:", "Cloudflare deployment:"},
		{"dashboard connection:", "Dashboard connection:"},
		{"Wrangler preparation:", "Cloudflare tooling preparation:"},
		{"Cloudflare secret ", "Cloudflare secret "},
		{"Update-Dienst meldet HTTP ", "Update service returned HTTP "},
		{"Testlauf konnte nicht gestartet werden ", "Test run could not be started "},
		{"Sicherheitsprüfung abgebrochen:", "Safety verification stopped:"},
		{"Testlauf fehlgeschlagen:", "Test run failed:"},
		{"Dashboard-Anmeldung oder Verbindung fehlgeschlagen:", "Dashboard sign-in or connection failed:"},
		{"Dashboard konnte nicht erreicht werden:", "Dashboard could not be reached:"},
		{"Fly.io-Anmeldung antwortete mit HTTP ", "Fly.io sign-in returned HTTP "},
		{"Fly.io-Anmeldung konnte nicht vorbereitet werden:", "Fly.io sign-in could not be prepared:"},
		{"lokaler Fly.io-Anmeldekanal konnte nicht geöffnet werden:", "The local Fly.io sign-in channel could not be opened:"},
		{"Fly.io-Anmeldung konnte nicht gestartet werden:", "Fly.io sign-in could not be started:"},
		{"Browser für die Fly.io-Anmeldung konnte nicht geöffnet werden:", "The browser for Fly.io sign-in could not be opened:"},
		{"Fly.io-Anmeldung konnte nicht abgeschlossen werden:", "Fly.io sign-in could not be completed:"},
		{"Fly.io-Anmeldung konnte nicht bestätigt werden:", "Fly.io sign-in could not be confirmed:"},
		{"lokale Fly.io-Konfiguration konnte nicht vorbereitet werden:", "The local Fly.io configuration could not be prepared:"},
		{"lokale Fly.io-Konfiguration konnte nicht geschützt werden:", "The local Fly.io configuration could not be protected:"},
		{"Dashboard unterstützt die sichere Wiederherstellung noch nicht", "Dashboard does not support secure recovery yet"},
		{"Testmodus konnte nicht aktiviert werden ", "Test mode could not be enabled "},
		{"Testlauf war erfolgreich, aber die ursprünglichen Dashboard-Einstellungen konnten nicht wiederhergestellt werden:", "The test run succeeded, but the previous dashboard settings could not be restored:"},
	}
	for _, pair := range prefixes {
		if strings.HasPrefix(raw, pair[0]) {
			return pair[1] + strings.TrimPrefix(raw, pair[0])
		}
	}

	if m := regexp.MustCompile(`^Sicherer Testlauf erfolgreich: (\d+) geprüft, (\d+) würden synchronisiert\.$`).FindStringSubmatch(raw); m != nil {
		return fmt.Sprintf("Safe test run successful: %s checked, %s would be synchronized.", m[1], m[2])
	}
	if strings.Contains(raw, "wird noch nicht unterstützt") {
		return strings.Replace(raw, "wird noch nicht unterstützt", "is not supported yet", 1)
	}
	if raw == "nur HTTPS ist erlaubt" {
		return "Only HTTPS is allowed"
	}
	if raw == "unsicherer Archivpfad" {
		return "Unsafe archive path"
	}
	if raw == "Symlinks im Werkzeugarchiv sind nicht erlaubt" || raw == "Links im Werkzeugarchiv sind nicht erlaubt" {
		return "Links are not allowed in tool archives"
	}
	if raw == "reMarkable-Helfer fehlt in diesem Paket" {
		return "The reMarkable helper is missing from this package"
	}
	return message
}

func (a *App) errorText(err error) string {
	if err == nil {
		return ""
	}
	a.st.mu.RLock()
	lang := a.st.Language
	a.st.mu.RUnlock()
	return translateSetupMessage(err.Error(), lang)
}

func (a *App) localizedLogf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	a.st.mu.RLock()
	lang := a.st.Language
	a.st.mu.RUnlock()
	a.logf("%s", translateSetupMessage(message, lang))
}
