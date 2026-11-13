package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func (a *App) metaSnapshot() *installMeta {
	a.st.mu.RLock()
	defer a.st.mu.RUnlock()
	if a.st.Meta == nil {
		return nil
	}
	copy := *a.st.Meta
	return &copy
}

func (a *App) maintenanceCheck() map[string]any {
	meta := a.metaSnapshot()
	if meta == nil {
		return map[string]any{"installed": false}
	}
	result := map[string]any{
		"installed":       true,
		"version":         meta.Version,
		"current_version": appVersion,
		"version_current": cloudComponentsCurrent(meta),
		"dashboard_url":   meta.DashboardURL,
		"app_name":        meta.AppName,
		"worker_name":     meta.WorkerName,
		"region":          meta.Region,
		"fly_token_until": meta.FlyTokenUntil,
	}
	if meta.FlyTokenUntil != "" {
		if expiry, err := time.Parse(time.RFC3339, meta.FlyTokenUntil); err == nil {
			days := int(time.Until(expiry).Hours() / 24)
			result["fly_token_days_remaining"] = days
			result["fly_token_ok"] = days > 180
			if days <= 180 {
				result["fly_token_warning"] = a.text("Die Fly-Steuerberechtigung läuft in weniger als sechs Monaten ab. Ein Cloud-Update erneuert sie.", "The Fly control permission expires in less than six months. A cloud update renews it.")
			}
		}
	}

	client := secureHTTPClient(12 * time.Second)
	healthURL := strings.TrimSuffix(meta.DashboardURL, "/") + "/healthz"
	if resp, err := client.Get(healthURL); err == nil {
		_ = resp.Body.Close()
		result["dashboard_http"] = resp.StatusCode
		result["dashboard_ok"] = resp.StatusCode >= 200 && resp.StatusCode < 300
	} else {
		result["dashboard_ok"] = false
		result["dashboard_error"] = err.Error()
	}

	fly, err := a.tools.FlyPath(nil)
	if err != nil {
		result["fly_ok"] = false
		result["fly_error"] = a.errorText(err)
		return result
	}
	if _, err := a.runFly("", fly, []string{"auth", "whoami"}, "", 30*time.Second); err != nil {
		result["fly_ok"] = false
		result["fly_auth"] = false
		result["fly_error"] = a.text("Fly.io muss für die lokale Wartung erneut angemeldet werden.", "Fly.io must be signed in again for local maintenance.")
		return result
	}
	result["fly_auth"] = true
	if _, err := a.runFly("", fly, []string{"status", "-a", meta.AppName}, "", 30*time.Second); err != nil {
		result["fly_ok"] = false
		result["fly_error"] = err.Error()
		return result
	}
	out, err := a.runFly("", fly, []string{"machine", "list", "-a", meta.AppName, "--json"}, "", 45*time.Second)
	if err != nil {
		result["fly_ok"] = false
		result["fly_error"] = err.Error()
		return result
	}
	count := countJSONMachines(out)
	result["fly_machine_count"] = count
	result["fly_ok"] = count == 1
	if count != 1 {
		result["fly_error"] = a.text(fmt.Sprintf("Sicherheitsprüfung: %d Fly-Machines gefunden; erwartet wird genau eine.", count), fmt.Sprintf("Safety check: found %d Fly Machines; exactly one is expected.", count))
	}
	return result
}

func (a *App) maintenanceUpdate() error {
	meta := a.metaSnapshot()
	if meta == nil {
		return fmt.Errorf("keine bestehende Installation gefunden")
	}
	fly, err := a.tools.FlyPath(a.localizedLogf)
	if err != nil {
		return err
	}
	if _, err := a.ensureFlySession(context.Background(), fly,
		"Fly.io-Anmeldung wird für die Wartung geöffnet …",
		"Fly.io sign-in is opening for maintenance …"); err != nil {
		return err
	}
	if err := a.ensureWorkerDeps(); err != nil {
		return err
	}
	if _, err := a.cloudflareIdentity(); err != nil {
		if _, err := a.runWrangler([]string{"login"}, "", 8*time.Minute); err != nil {
			return err
		}
		if _, err := a.cloudflareIdentity(); err != nil {
			return fmt.Errorf("Cloudflare-Anmeldung konnte nicht bestätigt werden")
		}
	}
	runtimeDir := filepath.Join(a.workDir, "runtime")
	workerDir := filepath.Join(runtimeDir, "worker")
	if err := ensureWorkerRecoveryMetadata(filepath.Join(workerDir, "wrangler.jsonc"), meta); err != nil {
		return err
	}
	a.logf("Sync-Software wird aktualisiert …")
	if _, err := a.runFly(runtimeDir, fly, []string{"deploy", "--ha=false", "-a", meta.AppName}, "", 15*time.Minute); err != nil {
		return fmt.Errorf("Fly-Update fehlgeschlagen: %w", err)
	}
	out, err := a.runFly(runtimeDir, fly, []string{"machine", "list", "-a", meta.AppName, "--json"}, "", 2*time.Minute)
	if err != nil {
		return err
	}
	machineID, err := singleFlyMachineID(out)
	if err != nil {
		return err
	}
	meta.MachineID = machineID
	if err := ensureWorkerFlyTarget(filepath.Join(workerDir, "wrangler.jsonc"), meta.AppName, meta.MachineID); err != nil {
		return err
	}
	// Renew the app-scoped control token as part of a deliberate maintenance
	// update. The one-year lifetime limits exposure while the six-month warning
	// leaves ample time for a normal cloud update to rotate it.
	flyToken, previousTokenIDs, reusedFlyToken, err := a.prepareFlyControlToken(runtimeDir, fly, meta.AppName)
	if err != nil {
		return fmt.Errorf("Fly-Steuerberechtigung konnte nicht erneuert werden: %w", err)
	}
	if reusedFlyToken {
		a.logText("Bereits erzeugte Fly-Steuerberechtigung wird erneut sicher geprüft …", "Safely rechecking the Fly control credential already created …")
	}
	a.logText("Neue Fly-Steuerberechtigung wird vor der Veröffentlichung geprüft …", "Validating the new Fly control credential before publication …")
	if err := validateFlyMachineControlToken(meta.AppName, meta.MachineID, flyToken); err != nil {
		a.logText("Technisches Detail zur Fly-Steuerberechtigung: %v", "Technical Fly control credential detail: %v", err)
		return fmt.Errorf("%s", a.text(
			"Die neue Fly-Steuerberechtigung wurde noch nicht akzeptiert. Es wurden keine bisherigen Berechtigungen widerrufen. Prüfe die Fly.io-Verbindung und starte das Cloud-Update danach erneut.",
			"The new Fly control credential has not been accepted yet. No previous credentials were revoked. Check the Fly.io connection, then start the cloud update again.",
		))
	}
	if err := a.putWranglerSecret(workerDir, "FLY_API_TOKEN", flyToken); err != nil {
		return err
	}
	meta.FlyTokenUntil = time.Now().Add(flyControlTokenLifetime).UTC().Format(time.RFC3339)
	if err := ensureWorkerRecoveryMetadata(filepath.Join(workerDir, "wrangler.jsonc"), meta); err != nil {
		return err
	}
	a.logf("Dashboard-Software wird aktualisiert …")
	if _, err := a.runWranglerIn(workerDir, wranglerDeployArgs(""), "", 10*time.Minute); err != nil {
		return fmt.Errorf("Dashboard-Update fehlgeschlagen: %w", err)
	}
	meta.Version = appVersion
	meta.UpdatedAt = time.Now()
	revokeErr := revokeFlyControlTokensWith(a.runFly, runtimeDir, fly, previousTokenIDs)
	saveErr := a.saveMeta(meta)
	if saveErr != nil {
		if revokeErr != nil {
			return fmt.Errorf("lokale Installationsdaten konnten nicht gespeichert werden; zusätzlich schlug die Token-Rotation fehl: %w", saveErr)
		}
		return saveErr
	}
	if revokeErr != nil {
		return fmt.Errorf("Cloud-Update abgeschlossen, aber Token-Rotation unvollständig: %w", revokeErr)
	}
	a.clearPendingFlyControlToken(meta.AppName, flyToken)
	return nil
}

func ensureWorkerRecoveryMetadata(path string, meta *installMeta) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Worker-Konfiguration fehlt: %w", err)
	}
	text := string(raw)
	text, err = enforceWorkerPublicationPolicy(text)
	if err != nil {
		return err
	}
	values := [][2]string{
		{"INSTALL_WORKER_NAME", meta.WorkerName},
		{"INSTALL_REGION", meta.Region},
		{"INSTALL_VERSION", appVersion},
		{"INSTALL_KV_ID", meta.KVNamespaceID},
		{"INSTALL_FLY_TOKEN_UNTIL", meta.FlyTokenUntil},
	}
	missing := make([][2]string, 0, len(values))
	for _, pair := range values {
		key, value := pair[0], pair[1]
		re := regexp.MustCompile(`("` + regexp.QuoteMeta(key) + `"\s*:\s*")[^"]*(")`)
		if re.MatchString(text) {
			text = re.ReplaceAllString(text, `${1}`+value+`${2}`)
		} else {
			missing = append(missing, pair)
		}
	}
	if len(missing) > 0 {
		varsBlock := regexp.MustCompile(`(?s)("vars"\s*:\s*\{)(.*?)(\n\s*\},\s*\n\s*"triggers")`)
		loc := varsBlock.FindStringSubmatchIndex(text)
		if loc == nil {
			return fmt.Errorf("Worker-Konfiguration ist unvollständig")
		}
		body := text[loc[4]:loc[5]]
		body = strings.TrimRight(body, " \t\r\n")
		if strings.TrimSpace(body) != "" && !strings.HasSuffix(strings.TrimSpace(body), ",") {
			body += ","
		}
		for i, pair := range missing {
			body += "\n    \"" + pair[0] + "\": \"" + pair[1] + "\""
			if i < len(missing)-1 {
				body += ","
			}
		}
		text = text[:loc[4]] + body + text[loc[5]:]
	}
	return atomicWritePrivate(path, []byte(text))
}

func enforceWorkerPublicationPolicy(text string) (string, error) {
	settings := []struct {
		key   string
		value string
	}{
		{key: "workers_dev", value: "true"},
		{key: "preview_urls", value: "false"},
	}
	for _, setting := range settings {
		keyPattern := regexp.MustCompile(`"` + setting.key + `"\s*:`)
		re := regexp.MustCompile(`("` + setting.key + `"\s*:\s*)(?:true|false)`)
		if matches := re.FindAllStringIndex(text, -1); len(matches) == 1 && len(keyPattern.FindAllStringIndex(text, -1)) == 1 {
			text = re.ReplaceAllString(text, `${1}`+setting.value)
			continue
		}
		if keyPattern.MatchString(text) {
			return "", fmt.Errorf("Worker-Konfiguration enthält eine ungültige oder doppelte %s-Einstellung", setting.key)
		}
		compatibilityKey := regexp.MustCompile(`"compatibility_date"\s*:`)
		compatibility := regexp.MustCompile(`("compatibility_date"\s*:\s*"[^"]+",)`)
		compatibilityMatches := compatibility.FindAllStringIndex(text, -1)
		if compatibilityKey.MatchString(text) && len(compatibilityMatches) != 1 {
			return "", fmt.Errorf("Worker-Konfiguration enthält ein ungültiges oder doppeltes Kompatibilitätsdatum")
		}
		anchor := compatibility
		if len(compatibilityMatches) == 0 {
			anchor = regexp.MustCompile(`("name"\s*:\s*"[a-z0-9][a-z0-9-]{1,62}",)`)
			if len(anchor.FindAllStringIndex(text, -1)) != 1 {
				return "", fmt.Errorf("Worker-Konfiguration enthält keinen eindeutigen sicheren Einfügepunkt")
			}
		}
		text = anchor.ReplaceAllString(text, `${1}`+"\n  \""+setting.key+"\": "+setting.value+",")
	}
	return text, nil
}

func resourceAlreadyAbsent(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not found") || strings.Contains(s, "does not exist") || strings.Contains(s, "could not find") || strings.Contains(s, "404")
}

func (a *App) maintenanceUninstall(confirm string) error {
	if confirm != "DELETE" {
		return fmt.Errorf("Bestätigung DELETE fehlt")
	}
	meta := a.metaSnapshot()
	if meta == nil {
		return fmt.Errorf("keine bestehende Installation gefunden")
	}
	if err := a.ensureCloudflareAuthenticated(); err != nil {
		return fmt.Errorf("Cloudflare-Anmeldung für die Deinstallation fehlgeschlagen: %w", err)
	}
	workerDir := filepath.Join(a.workDir, "runtime", "worker")
	if _, err := a.runWranglerIn(workerDir, []string{"delete", "--name", meta.WorkerName, "--force"}, "", 4*time.Minute); err != nil && !resourceAlreadyAbsent(err) {
		return fmt.Errorf("Cloudflare Worker konnte nicht gelöscht werden: %w", err)
	}
	if err := a.deleteKVFromConfig(); err != nil && !resourceAlreadyAbsent(err) {
		return fmt.Errorf("Cloudflare KV konnte nicht gelöscht werden: %w", err)
	}
	fly, err := a.ensureFlyAuthenticated()
	if err != nil {
		return fmt.Errorf("Fly.io-Anmeldung für die Deinstallation fehlgeschlagen: %w", err)
	}
	if _, err := a.runFly("", fly, []string{"apps", "destroy", meta.AppName, "--yes"}, "", 5*time.Minute); err != nil && !resourceAlreadyAbsent(err) {
		return fmt.Errorf("Fly-App konnte nicht gelöscht werden: %w", err)
	}
	for _, p := range []string{filepath.Join(a.workDir, "installation.json"), a.journalPath()} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("lokale Installationsdaten konnten nicht vollständig entfernt werden: %w", err)
		}
	}
	a.st.mu.Lock()
	a.st.Meta = nil
	a.localStateCorrupt = false
	a.st.mu.Unlock()
	return nil
}

func (a *App) deleteKVFromConfig() error {
	p := filepath.Join(a.workDir, "runtime", "worker", "wrangler.jsonc")
	kvID, err := readKVNamespaceID(p)
	if err != nil {
		return err
	}
	_, err = a.runWranglerIn(filepath.Dir(p), []string{"kv", "namespace", "delete", "--namespace-id", kvID}, "", 3*time.Minute)
	return err
}

func (a *App) saveMeta(meta *installMeta) error {
	if meta == nil {
		return fmt.Errorf("Installationsmetadaten fehlen")
	}
	base, err := normalizeDashboardURL(meta.DashboardURL)
	if err != nil || base != meta.DashboardURL || !cloudNamePattern.MatchString(meta.AppName) || !cloudNamePattern.MatchString(meta.WorkerName) || !flyRegionPattern.MatchString(meta.Region) || !flyMachinePattern.MatchString(meta.MachineID) || !kvNamespacePattern.MatchString(meta.KVNamespaceID) {
		return fmt.Errorf("Installationsmetadaten sind unvollständig")
	}
	if meta.FlyTokenUntil != "" {
		if _, err := time.Parse(time.RFC3339, meta.FlyTokenUntil); err != nil {
			return fmt.Errorf("Installationsmetadaten enthalten ein ungültiges Ablaufdatum")
		}
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWritePrivate(filepath.Join(a.workDir, "installation.json"), raw); err != nil {
		return err
	}
	copy := *meta
	a.st.mu.Lock()
	a.st.Meta = &copy
	a.localStateCorrupt = false
	a.st.mu.Unlock()
	return nil
}

func countJSONMachines(out string) int {
	var machines []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &machines); err != nil {
		return -1
	}
	count := 0
	for _, machine := range machines {
		if flyMachinePattern.MatchString(machine.ID) {
			count++
		}
	}
	return count
}

func (a *App) maintenanceMessage(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	return strings.TrimSpace(s)
}

func (a *App) ensureFlyAuthenticated() (string, error) {
	fly, err := a.tools.FlyPath(a.localizedLogf)
	if err != nil {
		return "", err
	}
	if _, err := a.ensureFlySession(context.Background(), fly,
		"Fly.io-Anmeldung wird für die Wartung geöffnet …",
		"Fly.io sign-in is opening for maintenance …"); err != nil {
		return "", err
	}
	return fly, nil
}

func (a *App) ensureCloudflareAuthenticated() error {
	if err := a.ensureWorkerDeps(); err != nil {
		return err
	}
	if _, err := a.cloudflareIdentity(); err != nil {
		a.logText("Cloudflare-Anmeldung wird für die Wartung geöffnet …", "Cloudflare sign-in is opening for maintenance …")
		if _, err := a.runWrangler([]string{"login"}, "", 8*time.Minute); err != nil {
			return err
		}
	}
	if _, err := a.cloudflareIdentity(); err != nil {
		return fmt.Errorf("Cloudflare-Anmeldung konnte nicht bestätigt werden")
	}
	return nil
}

func (a *App) maintenanceUpdateCredentials(updateIServ, updateRemarkable bool) error {
	meta := a.metaSnapshot()
	a.st.mu.RLock()
	sec := a.st.Secrets
	a.st.mu.RUnlock()
	if meta == nil {
		return fmt.Errorf("keine bestehende Installation gefunden")
	}
	fly, err := a.ensureFlyAuthenticated()
	if err != nil {
		return err
	}
	lines := []string{}
	if updateIServ {
		if sec.IServURL == "" || sec.IServUser == "" || sec.IServPassword == "" {
			return fmt.Errorf("IServ-Zugangsdaten fehlen")
		}
		iservLines, err := iservFlySecretLines(sec, cloudComponentsCurrent(meta))
		if err != nil {
			return err
		}
		lines = append(lines, iservLines...)
	}
	if updateRemarkable {
		if sec.DeviceToken == "" || sec.UserToken == "" {
			return fmt.Errorf("reMarkable-Verbindung fehlt")
		}
		rmapiB64 := base64.StdEncoding.EncodeToString([]byte(rmapiYAML(sec.DeviceToken, sec.UserToken)))
		lines = append(lines, "RMAPI_CONFIG_B64="+rmapiB64)
	}
	if len(lines) == 0 {
		return fmt.Errorf("keine Verbindung zum Aktualisieren ausgewählt")
	}
	a.logText("Neue Verbindungsdaten werden sicher als Fly-Secrets übernommen …", "Securely applying the new connection credentials as Fly secrets …")
	if _, err := a.runFly(filepath.Join(a.workDir, "runtime"), fly, []string{"secrets", "import", "-a", meta.AppName}, strings.Join(lines, "\n")+"\n", 4*time.Minute); err != nil {
		return fmt.Errorf("Fly secrets: %w", err)
	}
	if updateRemarkable {
		a.remarkableFolderMu.Lock()
		defer a.remarkableFolderMu.Unlock()
	}
	a.st.mu.Lock()
	if updateIServ {
		a.st.Secrets.IServURL = ""
		a.st.Secrets.IServUser = ""
		a.st.Secrets.IServPassword = ""
		a.st.IServConnected = false
	}
	if updateRemarkable {
		a.st.Secrets.DeviceToken = ""
		a.st.Secrets.UserToken = ""
		a.st.RemarkableConnected = false
		a.st.RemarkableFolders = nil
	}
	a.st.mu.Unlock()
	a.logText("Verbindungsdaten wurden aktualisiert.", "Connection credentials were updated.")
	return nil
}

func (a *App) maintenanceResetDashboardPassword(password string) error {
	if len(password) < 16 || len(password) > 256 || strings.ContainsAny(password, "\x00\r\n") {
		return fmt.Errorf("Dashboard-Passwort muss 16–256 Zeichen lang sein und darf keine Steuerzeichen enthalten")
	}
	meta := a.metaSnapshot()
	if meta == nil {
		return fmt.Errorf("keine bestehende Installation gefunden")
	}
	if err := a.ensureCloudflareAuthenticated(); err != nil {
		return err
	}
	workerDir := filepath.Join(a.workDir, "runtime", "worker")
	a.logText("Dashboard-Passwort wird sicher ersetzt …", "Securely replacing dashboard password …")
	if err := a.putWranglerSecret(workerDir, "ADMIN_PASSWORD", password); err != nil {
		return err
	}
	a.logText("Dashboard-Passwort aktualisiert. Bestehende Dashboard-Sitzungen sind damit ungültig.", "Dashboard password updated. Existing dashboard sessions are now invalid.")
	return nil
}

func (a *App) maintenanceDryRun(password string) (*verificationResult, error) {
	meta := a.metaSnapshot()
	if meta == nil || strings.TrimSpace(meta.DashboardURL) == "" {
		return nil, fmt.Errorf("keine bestehende Installation gefunden")
	}

	before, err := getDashboardState(meta.DashboardURL, password)
	if err != nil {
		return nil, fmt.Errorf("Dashboard-Anmeldung oder Verbindung fehlgeschlagen: %w", err)
	}
	originalRaw, err := json.Marshal(before.Settings)
	if err != nil {
		return nil, err
	}
	var safeSettings map[string]any
	if err := json.Unmarshal(originalRaw, &safeSettings); err != nil {
		return nil, err
	}
	syncSettings, ok := safeSettings["sync"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Dashboard-Einstellungen sind unvollständig")
	}
	syncSettings["dry_run"] = true
	safeSettings["enabled"] = false
	safeRaw, err := json.Marshal(safeSettings)
	if err != nil {
		return nil, err
	}
	if raw, status, err := adminRequest(meta.DashboardURL, password, http.MethodPut, "/api/admin/settings", safeRaw); err != nil {
		return nil, err
	} else if status < 200 || status >= 300 {
		return nil, fmt.Errorf("Testmodus konnte nicht aktiviert werden (HTTP %d): %s", status, strings.TrimSpace(string(raw)))
	}

	var restoreErr error
	a.logText("Sicherer Wartungs-Testlauf wird gestartet – Uploads sind vorübergehend zwangsweise deaktiviert …", "Starting safe maintenance test — uploads are temporarily forced off …")
	result, verifyErr := a.verifyDryRun(meta.DashboardURL, password)

	if raw, status, err := adminRequest(meta.DashboardURL, password, http.MethodPut, "/api/admin/settings", originalRaw); err != nil {
		restoreErr = err
	} else if status < 200 || status >= 300 {
		restoreErr = fmt.Errorf("HTTP %d: %s", status, strings.TrimSpace(string(raw)))
	}
	if restoreErr != nil {
		if verifyErr != nil {
			return result, fmt.Errorf("%v; ursprüngliche Dashboard-Einstellungen konnten zusätzlich nicht wiederhergestellt werden: %w", verifyErr, restoreErr)
		}
		return result, fmt.Errorf("Testlauf war erfolgreich, aber die ursprünglichen Dashboard-Einstellungen konnten nicht wiederhergestellt werden: %w", restoreErr)
	}
	if verifyErr != nil {
		return result, verifyErr
	}
	a.logText("Sicherer Wartungs-Testlauf abgeschlossen; die vorherigen Dashboard-Einstellungen wurden wiederhergestellt.", "Safe maintenance test completed; the previous dashboard settings were restored.")
	return result, nil
}
