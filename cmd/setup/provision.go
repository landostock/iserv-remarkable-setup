package main

import (
	"bytes"
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

const (
	flyControlTokenLifetime  = 365 * 24 * time.Hour
	flyControlTokenCLIExpiry = "8760h"
)

type provisionJournal struct {
	AppName         string          `json:"app_name"`
	WorkerName      string          `json:"worker_name"`
	MachineID       string          `json:"machine_id,omitempty"`
	DashboardURL    string          `json:"dashboard_url,omitempty"`
	KVNamespaceID   string          `json:"kv_namespace_id,omitempty"`
	FlyTokenUntil   string          `json:"fly_token_until,omitempty"`
	FlyImageVersion string          `json:"fly_image_version,omitempty"`
	Steps           map[string]bool `json:"steps"`
}

func (j provisionJournal) needsFlyDeploy() bool {
	return !j.Steps["fly_deploy"] || j.FlyImageVersion != appVersion
}

type kvNamespaceRecord struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type kvNamespaceRunner func(args []string) (string, error)

var installationSuffixPattern = regexp.MustCompile(`^(?:[a-f0-9]{8}|[a-f0-9]{16})$`)

func (a *App) journalPath() string { return filepath.Join(a.workDir, "provision.json") }

func validateProvisionJournal(j provisionJournal) error {
	if !cloudNamePattern.MatchString(j.AppName) || !cloudNamePattern.MatchString(j.WorkerName) {
		return fmt.Errorf("ungültige Cloud-Kennung")
	}
	suffix, err := installationSuffix(j.AppName)
	if err != nil || j.WorkerName != "iserv-rm-control-"+suffix {
		return fmt.Errorf("widersprüchliche Cloud-Kennungen")
	}
	if j.MachineID != "" && !flyMachinePattern.MatchString(j.MachineID) {
		return fmt.Errorf("ungültige Fly-Machine-Kennung")
	}
	if j.KVNamespaceID != "" && !kvNamespacePattern.MatchString(j.KVNamespaceID) {
		return fmt.Errorf("ungültige Cloudflare-KV-Kennung")
	}
	if j.DashboardURL != "" {
		base, err := normalizeDashboardURL(j.DashboardURL)
		if err != nil || base != j.DashboardURL {
			return fmt.Errorf("ungültige Dashboard-Adresse")
		}
	}
	if j.FlyTokenUntil != "" {
		if _, err := time.Parse(time.RFC3339, j.FlyTokenUntil); err != nil {
			return fmt.Errorf("ungültiges Ablaufdatum der Fly-Steuerberechtigung")
		}
	}
	return nil
}

func installationSuffix(appName string) (string, error) {
	const prefix = "iserv-rm-"
	if !strings.HasPrefix(appName, prefix) {
		return "", fmt.Errorf("ungültige Installationskennung")
	}
	suffix := strings.TrimPrefix(appName, prefix)
	if !installationSuffixPattern.MatchString(suffix) {
		return "", fmt.Errorf("ungültige Installationskennung")
	}
	return suffix, nil
}

func kvNamespaceTitle(appName string) (string, error) {
	suffix, err := installationSuffix(appName)
	if err != nil {
		return "", err
	}
	return "ISERV_RM_CONFIG_" + suffix, nil
}

func findKVNamespaceID(raw, title string) (string, bool, error) {
	namespaces, err := decodeKVNamespaceRecords(raw)
	if err != nil {
		return "", false, err
	}
	found := ""
	for _, namespace := range namespaces {
		if namespace.Title != title {
			continue
		}
		if !kvNamespacePattern.MatchString(namespace.ID) {
			return "", false, fmt.Errorf("Cloudflare-KV-Liste enthält eine ungültige Kennung")
		}
		if found != "" {
			return "", false, fmt.Errorf("Cloudflare-KV-Liste enthält den Installationsspeicher mehrfach")
		}
		found = namespace.ID
	}
	return found, found != "", nil
}

func decodeKVNamespaceRecords(raw string) ([]kvNamespaceRecord, error) {
	var namespaces []kvNamespaceRecord
	trimmed := strings.TrimSpace(raw)
	if err := json.Unmarshal([]byte(trimmed), &namespaces); err == nil {
		return validateKVNamespaceRecords(namespaces)
	}

	// Wrangler writes update notices to stderr. The command runner intentionally
	// combines both streams for diagnostics, so locate one unambiguous JSON array.
	var decoded []kvNamespaceRecord
	matches := 0
	for offset, char := range trimmed {
		if char != '[' {
			continue
		}
		var candidate []kvNamespaceRecord
		decoder := json.NewDecoder(strings.NewReader(trimmed[offset:]))
		if err := decoder.Decode(&candidate); err != nil {
			continue
		}
		matches++
		validated, err := validateKVNamespaceRecords(candidate)
		if err != nil {
			continue
		}
		decoded = validated
		if matches > 1 {
			return nil, fmt.Errorf("Cloudflare-KV-Liste enthält mehrere JSON-Ergebnisse")
		}
	}
	if matches != 1 {
		return nil, fmt.Errorf("Cloudflare-KV-Liste ist ungültig")
	}
	return decoded, nil
}

func validateKVNamespaceRecords(namespaces []kvNamespaceRecord) ([]kvNamespaceRecord, error) {
	if namespaces == nil {
		return nil, fmt.Errorf("Cloudflare-KV-Liste ist kein JSON-Array")
	}
	for _, namespace := range namespaces {
		if !kvNamespacePattern.MatchString(namespace.ID) || strings.TrimSpace(namespace.Title) == "" {
			return nil, fmt.Errorf("Cloudflare-KV-Liste enthält einen ungültigen Eintrag")
		}
	}
	return namespaces, nil
}

func ensureKVNamespaceBinding(path, namespaceID string) error {
	if !kvNamespacePattern.MatchString(namespaceID) {
		return fmt.Errorf("ungültige Cloudflare-KV-Kennung")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Cloudflare-Konfiguration fehlt: %w", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return fmt.Errorf("Cloudflare-Konfiguration ist ungültig: %w", err)
	}
	bindings := []any{}
	if existing, ok := config["kv_namespaces"]; ok {
		var valid bool
		bindings, valid = existing.([]any)
		if !valid {
			return fmt.Errorf("Cloudflare-KV-Bindungen sind ungültig")
		}
	}
	configBinding := -1
	for index, value := range bindings {
		binding, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("Cloudflare-KV-Bindung ist ungültig")
		}
		name, _ := binding["binding"].(string)
		if name != "CONFIG" {
			continue
		}
		if configBinding >= 0 {
			return fmt.Errorf("Cloudflare-Konfiguration enthält mehrere CONFIG-Bindungen")
		}
		configBinding = index
		if existingID, _ := binding["id"].(string); existingID != "" && existingID != namespaceID {
			return fmt.Errorf("Cloudflare-CONFIG-Bindung verweist auf einen anderen Speicher")
		}
	}
	newBinding := map[string]any{"binding": "CONFIG", "id": namespaceID}
	if configBinding >= 0 {
		bindings[configBinding] = newBinding
	} else {
		bindings = append(bindings, newBinding)
	}
	config["kv_namespaces"] = bindings
	updated, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("Cloudflare-Konfiguration konnte nicht geschrieben werden: %w", err)
	}
	return atomicWritePrivate(path, append(updated, '\n'))
}

func ensureKVNamespace(run kvNamespaceRunner, configPath, title, expectedID string, pause func(time.Duration)) (string, bool, error) {
	localID, _ := readKVNamespaceID(configPath)
	if expectedID != "" && !kvNamespacePattern.MatchString(expectedID) {
		return "", false, fmt.Errorf("ungültige erwartete Cloudflare-KV-Kennung")
	}
	list := func() (string, bool, error) {
		out, err := run([]string{"kv", "namespace", "list"})
		if err != nil {
			return "", false, err
		}
		return findKVNamespaceID(out, title)
	}
	if existingID, found, err := list(); err != nil {
		return "", false, fmt.Errorf("Cloudflare-KV-Liste: %w", err)
	} else if found {
		if expectedID != "" && existingID != expectedID {
			return "", false, fmt.Errorf("Cloudflare-KV-Kennung widerspricht dem Installationsfortschritt")
		}
		if err := ensureKVNamespaceBinding(configPath, existingID); err != nil {
			return "", false, err
		}
		return existingID, true, nil
	}
	if expectedID != "" || localID != "" {
		return "", false, fmt.Errorf("lokale Cloudflare-KV-Bindung konnte dem Installationsspeicher nicht sicher zugeordnet werden")
	}
	_, createErr := run([]string{"kv", "namespace", "create", title})
	delays := []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond}
	var lastListErr error
	for _, delay := range delays {
		if delay > 0 {
			pause(delay)
		}
		namespaceID, found, err := list()
		if err != nil {
			lastListErr = err
			continue
		}
		if !found {
			continue
		}
		if err := ensureKVNamespaceBinding(configPath, namespaceID); err != nil {
			return "", false, err
		}
		return namespaceID, false, nil
	}
	if createErr != nil {
		return "", false, createErr
	}
	if lastListErr != nil {
		return "", false, fmt.Errorf("Cloudflare-KV konnte nach der Erstellung nicht bestätigt werden: %w", lastListErr)
	}
	return "", false, fmt.Errorf("Cloudflare-KV wurde nach der Erstellung nicht gefunden")
}

func (a *App) loadJournal() (provisionJournal, error) {
	raw, err := os.ReadFile(a.journalPath())
	if err == nil {
		var j provisionJournal
		if err := json.Unmarshal(raw, &j); err != nil {
			return provisionJournal{}, fmt.Errorf("lokaler Installationsfortschritt ist beschädigt; sichere Wiederherstellung erforderlich")
		}
		if err := validateProvisionJournal(j); err != nil {
			return provisionJournal{}, fmt.Errorf("lokaler Installationsfortschritt ist beschädigt: %w", err)
		}
		if j.Steps == nil {
			j.Steps = map[string]bool{}
		}
		return j, nil
	}
	if !os.IsNotExist(err) {
		return provisionJournal{}, fmt.Errorf("Installationsfortschritt konnte nicht gelesen werden: %w", err)
	}
	suffix, err := randomHex(8)
	if err != nil {
		return provisionJournal{}, fmt.Errorf("sichere Installationskennung konnte nicht erzeugt werden: %w", err)
	}
	j := provisionJournal{AppName: "iserv-rm-" + suffix, WorkerName: "iserv-rm-control-" + suffix, Steps: map[string]bool{}}
	if err := validateProvisionJournal(j); err != nil {
		return provisionJournal{}, err
	}
	return j, nil
}

func (a *App) saveJournal(j provisionJournal) error {
	if err := validateProvisionJournal(j); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return atomicWritePrivate(a.journalPath(), raw)
}

func (a *App) persistJournal(j provisionJournal) error {
	if err := a.saveJournal(j); err != nil {
		return fmt.Errorf("Installationsfortschritt konnte nicht sicher gespeichert werden: %w", err)
	}
	return nil
}

func replaceFile(path string, replacements map[string]string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(raw)
	for old, value := range replacements {
		text = strings.ReplaceAll(text, old, value)
	}
	return atomicWritePrivate(path, []byte(text))
}

func (a *App) setInstallProgress(percent int, de, en string) {
	a.st.mu.Lock()
	a.st.InstallProgress = percent
	if a.st.Language == "en" {
		a.st.InstallPhase = en
	} else {
		a.st.InstallPhase = de
	}
	a.st.mu.Unlock()
}

func countNamedFlyVolumes(raw, wanted string) (int, error) {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return 0, fmt.Errorf("Fly-Volume-Liste ist ungültig: %w", err)
	}
	var rows []any
	switch typed := value.(type) {
	case []any:
		rows = typed
	case map[string]any:
		for _, key := range []string{"volumes", "Volumes"} {
			if list, ok := typed[key].([]any); ok {
				rows = list
				break
			}
		}
	default:
		return 0, fmt.Errorf("Fly-Volume-Liste hat ein unerwartetes Format")
	}
	if rows == nil {
		return 0, fmt.Errorf("Fly-Volume-Liste hat ein unerwartetes Format")
	}
	count := 0
	for _, row := range rows {
		item, ok := row.(map[string]any)
		if !ok {
			continue
		}
		name, _ := item["name"].(string)
		if name == "" {
			name, _ = item["Name"].(string)
		}
		if name == wanted {
			count++
		}
	}
	return count, nil
}

func singleFlyMachineID(raw string) (string, error) {
	var machines []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(raw), &machines); err != nil {
		return "", fmt.Errorf("Fly-Machine-Liste ist ungültig: %w", err)
	}
	if len(machines) != 1 || !flyMachinePattern.MatchString(machines[0].ID) {
		return "", fmt.Errorf("Sicherheitsprüfung fehlgeschlagen: genau eine gültige Fly-Machine erwartet")
	}
	return machines[0].ID, nil
}

func ensureWorkerFlyTarget(path, appName, machineID string) error {
	if !cloudNamePattern.MatchString(appName) || !flyMachinePattern.MatchString(machineID) {
		return fmt.Errorf("ungültiges Fly-Ziel für den Cloudflare Worker")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Cloudflare-Konfiguration fehlt: %w", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return fmt.Errorf("Cloudflare-Konfiguration ist ungültig: %w", err)
	}
	vars, ok := config["vars"].(map[string]any)
	if !ok {
		return fmt.Errorf("Cloudflare-Konfiguration enthält keine gültigen Variablen")
	}
	vars["FLY_APP"] = appName
	vars["FLY_MACHINE_ID"] = machineID
	updated, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("Cloudflare-Konfiguration konnte nicht geschrieben werden: %w", err)
	}
	return atomicWritePrivate(path, append(updated, '\n'))
}

func (a *App) provision() error {
	a.st.mu.RLock()
	sec := a.st.Secrets
	cfg := cloneConfig(a.st.Config)
	cost := a.st.CostAccepted
	flyOK := a.st.FlyLoggedIn
	cfOK := a.st.CloudflareLoggedIn
	iservOK := a.st.IServConnected
	rmOK := a.st.RemarkableConnected
	a.st.mu.RUnlock()
	if !cost {
		return fmt.Errorf("%s", a.text("Kostenhinweis wurde noch nicht bestätigt", "Cost notice has not been confirmed"))
	}
	if !flyOK || !cfOK || !iservOK || !rmOK {
		return fmt.Errorf("%s", a.text("noch nicht alle Dienste sind verbunden", "not all services are connected yet"))
	}
	if err := validConfig(cfg); err != nil {
		return err
	}
	a.setInstallProgress(5, "Werkzeuge werden vorbereitet …", "Preparing tools …")
	if err := a.ensureWorkerDeps(); err != nil {
		return err
	}
	runtimeDir := filepath.Join(a.workDir, "runtime")
	workerDir := filepath.Join(runtimeDir, "worker")
	flyToml := filepath.Join(runtimeDir, "fly.toml")
	wrangler := filepath.Join(workerDir, "wrangler.jsonc")
	bootstrapWrangler := filepath.Join(workerDir, "wrangler.bootstrap.jsonc")
	j, err := a.loadJournal()
	if err != nil {
		return err
	}
	a.logText("Installationskennung: %s", "Installation ID: %s", j.AppName)
	if err := a.persistJournal(j); err != nil {
		return err
	}
	if !j.Steps["templates"] {
		if err := copyFile(filepath.Join(runtimeDir, "fly.toml.template"), flyToml); err != nil {
			return err
		}
		if err := replaceFile(flyToml, map[string]string{"__APP_NAME__": j.AppName, "__REGION__": cfg.Region}); err != nil {
			return err
		}
		if err := copyFile(filepath.Join(workerDir, "wrangler.jsonc.template"), wrangler); err != nil {
			return err
		}
		j.Steps["templates"] = true
		if err := a.persistJournal(j); err != nil {
			return err
		}
	}

	a.setInstallProgress(10, "Cloudflare-Bereitschaft wird geprüft …", "Checking Cloudflare readiness …")
	if !j.Steps["worker_bootstrap"] {
		if err := prepareWorkerBootstrap(workerDir, bootstrapWrangler, j.WorkerName); err != nil {
			return err
		}
		a.logText("Cloudflare-Bereitschaft wird mit einem privaten, inaktiven Worker geprüft …", "Checking Cloudflare readiness with a private, inactive Worker …")
		if _, err := a.runWranglerIn(workerDir, wranglerDeployArgs(filepath.Base(bootstrapWrangler)), "", 10*time.Minute); err != nil {
			return a.cloudflareWorkerPreparationError(err)
		}
		j.Steps["worker_bootstrap"] = true
		if err := a.persistJournal(j); err != nil {
			return err
		}
	}

	fly, err := a.tools.FlyPath(a.localizedLogf)
	if err != nil {
		return err
	}
	a.setInstallProgress(15, "Fly.io wird eingerichtet …", "Setting up Fly.io …")
	if !j.Steps["fly_app"] {
		if _, statusErr := a.runFly(runtimeDir, fly, []string{"status", "-a", j.AppName}, "", 45*time.Second); statusErr == nil {
			j.Steps["fly_app"] = true
			if err := a.persistJournal(j); err != nil {
				return err
			}
		} else {
			a.logText("Fly-App wird erstellt …", "Creating Fly app …")
			organizationOutput, err := a.runFly(runtimeDir, fly, []string{"orgs", "list", "--json"}, "", 45*time.Second)
			if err != nil {
				return fmt.Errorf("Fly-Organisationen: %w", err)
			}
			organizations, err := decodeFlyOrganizations(organizationOutput)
			if err != nil {
				return err
			}
			organization, err := chooseFlyOrganization(organizations, cfg.FlyOrgSlug)
			if err != nil {
				return err
			}
			createArgs, err := flyAppCreateArgs(j.AppName, organization)
			if err != nil {
				return err
			}
			if _, err := a.runFly(runtimeDir, fly, createArgs, "", 2*time.Minute); err != nil {
				return fmt.Errorf("Fly app: %w", err)
			}
			j.Steps["fly_app"] = true
			if err := a.persistJournal(j); err != nil {
				return err
			}
		}
	}

	a.setInstallProgress(25, "Persistenter Speicher wird eingerichtet …", "Setting up persistent storage …")
	if !j.Steps["fly_volume"] {
		volumeList, err := a.runFly(runtimeDir, fly, []string{"volumes", "list", "-a", j.AppName, "--json"}, "", 45*time.Second)
		if err != nil {
			return fmt.Errorf("Fly volumes: %w", err)
		}
		count, err := countNamedFlyVolumes(volumeList, "sync_data")
		if err != nil {
			return err
		}
		switch count {
		case 0:
			a.logText("Persistenter 1-GB-Speicher wird erstellt …", "Creating 1 GB persistent storage …")
			if _, err := a.runFly(runtimeDir, fly, []string{"volumes", "create", "sync_data", "--region", cfg.Region, "--size", "1", "-a", j.AppName, "--yes"}, "", 3*time.Minute); err != nil {
				return fmt.Errorf("Fly volume: %w", err)
			}
		case 1:
			a.logText("Vorhandener persistenter Speicher wird sicher wiederverwendet.", "Safely reusing the existing persistent volume.")
		default:
			return fmt.Errorf("Sicherheitsprüfung fehlgeschlagen: %d Fly-Volumes namens sync_data gefunden; erwartet wird genau eines", count)
		}
		j.Steps["fly_volume"] = true
		if err := a.persistJournal(j); err != nil {
			return err
		}
	}

	secretLines, err := iservFlySecretLines(sec, true)
	if err != nil {
		return err
	}
	rmapiB64 := base64.StdEncoding.EncodeToString([]byte(rmapiYAML(sec.DeviceToken, sec.UserToken)))
	initialSecrets := strings.Join(append(secretLines, "RMAPI_CONFIG_B64="+rmapiB64, "DRY_RUN=true"), "\n") + "\n"
	a.setInstallProgress(40, "Sync-Anwendung wird bereitgestellt …", "Deploying sync application …")
	if j.needsFlyDeploy() {
		a.logText("Zugangsdaten werden sicher als Fly-Secrets vorbereitet …", "Staging credentials securely as Fly secrets …")
		if _, err := a.runFly(runtimeDir, fly, []string{"secrets", "import", "--stage", "-a", j.AppName}, initialSecrets, 3*time.Minute); err != nil {
			return fmt.Errorf("Fly secrets: %w", err)
		}
		a.logText("Sync-Anwendung wird bereitgestellt …", "Deploying sync application …")
		if _, err := a.runFly(runtimeDir, fly, []string{"deploy", "--ha=false", "-a", j.AppName}, "", 15*time.Minute); err != nil {
			return fmt.Errorf("Fly deployment: %w", err)
		}
		j.Steps["fly_deploy"] = true
		j.FlyImageVersion = appVersion
		if err := a.persistJournal(j); err != nil {
			return err
		}
	} else {
		a.logText("Bereits bereitgestellte Sync-Anwendung wird sicher weiterverwendet.", "Safely reusing the deployed sync application.")
	}
	out, err := a.runFly(runtimeDir, fly, []string{"machine", "list", "-a", j.AppName, "--json"}, "", 2*time.Minute)
	if err != nil {
		return err
	}
	machineID, err := singleFlyMachineID(out)
	if err != nil {
		return fmt.Errorf("%s", a.text(err.Error(), "Safety check failed: exactly one valid Fly Machine is required"))
	}
	j.MachineID = machineID
	if err := a.persistJournal(j); err != nil {
		return err
	}

	if !j.Steps["worker_config"] {
		if err := replaceFile(wrangler, map[string]string{"__WORKER_NAME__": j.WorkerName, "__APP_NAME__": j.AppName, "__MACHINE_ID__": j.MachineID, "__REGION__": cfg.Region, "__INSTALL_VERSION__": appVersion, "__KV_ID__": j.KVNamespaceID, "__FLY_TOKEN_UNTIL__": j.FlyTokenUntil}); err != nil {
			return err
		}
		j.Steps["worker_config"] = true
		if err := a.persistJournal(j); err != nil {
			return err
		}
	}
	if err := ensureWorkerFlyTarget(wrangler, j.AppName, j.MachineID); err != nil {
		return err
	}
	if err := ensureWorkerRecoveryMetadata(wrangler, &installMeta{WorkerName: j.WorkerName, Region: cfg.Region, MachineID: j.MachineID, KVNamespaceID: j.KVNamespaceID, FlyTokenUntil: j.FlyTokenUntil}); err != nil {
		return err
	}

	a.setInstallProgress(58, "Cloudflare wird eingerichtet …", "Setting up Cloudflare …")
	if !j.Steps["kv"] {
		title, err := kvNamespaceTitle(j.AppName)
		if err != nil {
			return err
		}
		a.logText("Cloudflare-Konfigurationsspeicher wird sicher geprüft …", "Safely checking Cloudflare configuration storage …")
		kvID, reused, err := ensureKVNamespace(func(args []string) (string, error) {
			return a.runWranglerIn(workerDir, args, "", 3*time.Minute)
		}, wrangler, title, j.KVNamespaceID, time.Sleep)
		if err != nil {
			a.logText("Technisches Detail zur Cloudflare-KV-Einrichtung: %v", "Technical Cloudflare KV setup detail: %v", err)
			return fmt.Errorf("%s", a.text(
				"Der persönliche Cloudflare-Konfigurationsspeicher konnte nicht eingerichtet werden. Vorhandene Speicher wurden nicht verändert. Prüfe die Verbindung und klicke danach erneut auf „Installation fortsetzen“.",
				"The personal Cloudflare configuration store could not be prepared. Existing stores were not changed. Check the connection, then click “Continue installation” again.",
			))
		}
		j.KVNamespaceID = kvID
		j.Steps["kv"] = true
		if reused {
			a.logText("Der zu dieser Installation gehörende Cloudflare-Konfigurationsspeicher wird sicher wiederverwendet.", "Safely reusing the Cloudflare configuration store belonging to this installation.")
		} else {
			a.logText("Persönlicher Cloudflare-Konfigurationsspeicher wurde erstellt.", "Personal Cloudflare configuration storage was created.")
		}
		if err := a.persistJournal(j); err != nil {
			return err
		}
	}
	if j.KVNamespaceID == "" {
		kvID, err := readKVNamespaceID(wrangler)
		if err != nil {
			return err
		}
		j.KVNamespaceID = kvID
		if err := a.persistJournal(j); err != nil {
			return err
		}
	}
	syncToken, err := randomHex(32)
	if err != nil {
		return fmt.Errorf("sicheres Sync-Token konnte nicht erzeugt werden: %w", err)
	}
	a.setInstallProgress(68, "Sichere Cloud-Verknüpfungen werden erstellt …", "Creating secure cloud connections …")
	a.logText("Dashboard-Secrets werden gesetzt …", "Setting dashboard secrets …")
	if err := a.putWranglerSecret(workerDir, "ADMIN_PASSWORD", cfg.AdminPassword); err != nil {
		return err
	}
	if err := a.putWranglerSecret(workerDir, "SYNC_API_TOKEN", syncToken); err != nil {
		return err
	}
	flyToken, previousTokenIDs, reusedFlyToken, err := a.prepareFlyControlToken(runtimeDir, fly, j.AppName)
	if err != nil {
		return err
	}
	if reusedFlyToken {
		a.logText("Bereits erzeugte Fly-Steuerberechtigung wird erneut sicher geprüft …", "Safely rechecking the Fly control credential already created …")
	}
	a.setInstallProgress(68, "Fly-Steuerberechtigung wird aktiviert und geprüft …", "Activating and validating Fly control permission …")
	a.logText("Neue Fly-Steuerberechtigung wird vor der Veröffentlichung geprüft …", "Validating the new Fly control credential before publication …")
	if err := validateFlyMachineControlToken(j.AppName, j.MachineID, flyToken); err != nil {
		a.logText("Technisches Detail zur Fly-Steuerberechtigung: %v", "Technical Fly control credential detail: %v", err)
		return fmt.Errorf("%s", a.text(
			"Fly.io hat die neue Steuerberechtigung auch nach sicherem Abwarten noch nicht akzeptiert. Es wurden keine bisherigen Berechtigungen widerrufen. Gehe zu „Cloud“, verbinde Fly.io neu und klicke danach auf „Installation fortsetzen“; eine Neuinstallation ist nicht erforderlich.",
			"Fly.io still did not accept the new control credential after a safe waiting period. No previous credentials were revoked. Go to “Cloud”, reconnect Fly.io, then click “Continue installation”; reinstallation is not required.",
		))
	}
	j.FlyTokenUntil = time.Now().Add(flyControlTokenLifetime).UTC().Format(time.RFC3339)
	if err := a.persistJournal(j); err != nil {
		return err
	}
	if err := ensureWorkerRecoveryMetadata(wrangler, &installMeta{WorkerName: j.WorkerName, Region: cfg.Region, MachineID: j.MachineID, KVNamespaceID: j.KVNamespaceID, FlyTokenUntil: j.FlyTokenUntil}); err != nil {
		return err
	}
	if err := a.putWranglerSecret(workerDir, "FLY_API_TOKEN", flyToken); err != nil {
		return err
	}

	a.setInstallProgress(78, "Dashboard wird veröffentlicht …", "Publishing dashboard …")
	a.logText("Dashboard wird auf Cloudflare bereitgestellt …", "Deploying dashboard to Cloudflare …")
	out, err = a.runWranglerIn(workerDir, wranglerDeployArgs(""), "", 10*time.Minute)
	if err != nil {
		if cloudflareEmailVerificationRequired(err.Error()) {
			return a.cloudflareWorkerPreparationError(err)
		}
		if onboardingURL := cloudflareWorkersOnboardingURL(err.Error()); onboardingURL != "" {
			if openErr := openBrowser(onboardingURL); openErr != nil {
				a.logText("Cloudflare-Einrichtungsseite konnte nicht automatisch geöffnet werden: %v", "Cloudflare setup page could not be opened automatically: %v", openErr)
				return fmt.Errorf("%s %s", a.text(
					"Cloudflare benötigt für dieses neue Konto einmalig eine workers.dev-Kontoadresse. Öffne diese sichere Cloudflare-Adresse und lege nur die Kontoadresse an; ändere keine Production- oder Preview-Schalter. Klicke danach erneut auf „Installation fortsetzen“:",
					"Cloudflare requires a one-time workers.dev account address for this new account. Open this secure Cloudflare page and create only the account address; do not change Production or Preview switches. Then click “Continue installation” again:",
				), onboardingURL)
			}
			return fmt.Errorf("%s", a.text(
				"Cloudflare benötigt für dieses neue Konto einmalig eine workers.dev-Kontoadresse. Die sichere Cloudflare-Seite wurde geöffnet. Lege dort nur die Kontoadresse an und ändere keine Production- oder Preview-Schalter. Klicke danach hier erneut auf „Installation fortsetzen“.",
				"Cloudflare requires a one-time workers.dev account address for this new account. The secure Cloudflare page has been opened. Create only the account address and do not change Production or Preview switches. Then click “Continue installation” here again.",
			))
		}
		return fmt.Errorf("Cloudflare deployment: %w", err)
	}
	if j.DashboardURL == "" {
		j.DashboardURL = extractWorkersURL(out)
	}
	if j.DashboardURL == "" {
		return fmt.Errorf("dashboard URL missing")
	}
	base, err := normalizeDashboardURL(j.DashboardURL)
	if err != nil || base != j.DashboardURL {
		return fmt.Errorf("dashboard URL is invalid")
	}
	j.Steps["worker_deploy"] = true
	if err := a.persistJournal(j); err != nil {
		return err
	}
	if err := revokeFlyControlTokensWith(a.runFly, runtimeDir, fly, previousTokenIDs); err != nil {
		return fmt.Errorf("Fly app token rotation: %w", err)
	}
	a.clearPendingFlyControlToken(j.AppName, flyToken)

	a.setInstallProgress(88, "Sichere Startkonfiguration wird gesetzt …", "Applying safe initial configuration …")
	settings := map[string]any{"enabled": false, "interval_minutes": cfg.IntervalMinutes, "schedule": cfg.Schedule, "sync": map[string]any{"dry_run": true, "lookback_days": cfg.LookbackDays, "max_file_mb": cfg.MaxFileMB, "extensions": cfg.Extensions, "sources": cfg.Sources}}
	a.logText("Sichere Startkonfiguration wird gespeichert …", "Saving safe initial configuration …")
	if err := setDashboardSettings(j.DashboardURL, cfg.AdminPassword, settings); err != nil {
		return err
	}
	// Applying secrets can start the Machine. Publish the selected safe settings
	// first, and apply the current credentials even when reusing an image.
	finalSecrets := initialSecrets + "CONTROL_URL=" + j.DashboardURL + "\nSYNC_API_TOKEN=" + syncToken + "\n"
	if _, err := a.runFly(runtimeDir, fly, []string{"secrets", "import", "-a", j.AppName}, finalSecrets, 4*time.Minute); err != nil {
		return fmt.Errorf("dashboard connection: %w", err)
	}
	a.setInstallProgress(94, "Sicherer Testlauf läuft …", "Running safe test …")
	vr, err := a.verifyDryRun(j.DashboardURL, cfg.AdminPassword)
	a.st.mu.Lock()
	a.st.Verification = vr
	a.st.mu.Unlock()
	if err != nil {
		return err
	}
	meta := &installMeta{AppName: j.AppName, WorkerName: j.WorkerName, DashboardURL: j.DashboardURL, Region: cfg.Region, MachineID: j.MachineID, KVNamespaceID: j.KVNamespaceID, FlyTokenUntil: j.FlyTokenUntil, Version: appVersion, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := a.finalizeInstallation(j, meta); err != nil {
		return err
	}
	a.remarkableFolderMu.Lock()
	a.st.mu.Lock()
	a.st.Secrets = secrets{}
	a.st.Config.AdminPassword = ""
	a.st.IServConnected = false
	a.st.RemarkableConnected = false
	a.st.RemarkableFolders = nil
	a.st.mu.Unlock()
	a.remarkableFolderMu.Unlock()
	a.logText("Installation und sicherer Testlauf abgeschlossen. Automatik bleibt AUS.", "Installation and safe test run completed. Automation remains OFF.")
	return nil
}

func (a *App) finalizeInstallation(j provisionJournal, meta *installMeta) error {
	if err := validateProvisionJournal(j); err != nil {
		return err
	}
	if meta == nil || meta.AppName != j.AppName || meta.WorkerName != j.WorkerName || meta.MachineID != j.MachineID || meta.KVNamespaceID != j.KVNamespaceID || meta.DashboardURL != j.DashboardURL {
		return fmt.Errorf("Installationsabschluss enthält widersprüchliche Kennungen")
	}
	if err := a.saveMeta(meta); err != nil {
		return err
	}
	if err := os.Remove(a.journalPath()); err != nil && !os.IsNotExist(err) {
		a.logText("Hinweis: Der abgeschlossene Installationsfortschritt konnte lokal nicht bereinigt werden: %v", "Note: completed local installation progress could not be cleaned up: %v", err)
	}
	return nil
}

func readKVNamespaceID(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("Cloudflare-Konfiguration fehlt: %w", err)
	}
	re := regexp.MustCompile(`"binding"\s*:\s*"CONFIG"[\s\S]{0,300}?"id"\s*:\s*"([A-Za-z0-9_-]+)"`)
	m := re.FindSubmatch(raw)
	if len(m) < 2 || !kvNamespacePattern.Match(m[1]) {
		return "", fmt.Errorf("Cloudflare-KV-Kennung konnte nicht sicher ermittelt werden")
	}
	return string(m[1]), nil
}

func copyFile(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return atomicWritePrivate(dst, raw)
}

func prepareWorkerBootstrap(workerDir, target, workerName string) error {
	if !cloudNamePattern.MatchString(workerName) {
		return fmt.Errorf("ungültige Cloud-Kennung")
	}
	template := filepath.Join(workerDir, "wrangler.bootstrap.jsonc.template")
	if err := copyFile(template, target); err != nil {
		return fmt.Errorf("Cloudflare-Worker-Vorbereitung fehlt: %w", err)
	}
	if err := replaceFile(target, map[string]string{"__WORKER_NAME__": workerName}); err != nil {
		return fmt.Errorf("Cloudflare-Worker-Vorbereitung ist ungültig: %w", err)
	}
	return nil
}

func (a *App) ensureWorkerDeps() error {
	a.workerDepsMu.Lock()
	defer a.workerDepsMu.Unlock()
	if a.workerDepsReady {
		return nil
	}
	node, npmCLI, err := a.tools.NodePaths(a.localizedLogf)
	if err != nil {
		return err
	}
	workerDir := filepath.Join(a.workDir, "runtime", "worker")
	a.logText("Cloudflare-Werkzeug wird vorbereitet …", "Preparing Cloudflare tooling …")
	if _, err := runWithTimeoutEnv(workerDir, node, []string{npmCLI, "ci", "--no-audit", "--no-fund"}, "", 10*time.Minute, managedNodeEnvironment(node)); err != nil {
		return fmt.Errorf("Wrangler preparation: %w", err)
	}
	a.workerDepsReady = true
	return nil
}

func (a *App) runWrangler(args []string, stdin string, timeout time.Duration) (string, error) {
	if len(args) == 0 || (args[0] != "login" && args[0] != "logout" && args[0] != "whoami") {
		return "", fmt.Errorf("unsupported Cloudflare account command")
	}
	dir, err := a.wranglerAccountCommandDirectory()
	if err != nil {
		return "", err
	}
	return a.runWranglerIn(dir, args, stdin, timeout)
}

func (a *App) runWranglerIn(dir string, args []string, stdin string, timeout time.Duration) (string, error) {
	node, _, err := a.tools.NodePaths(nil)
	if err != nil {
		return "", err
	}
	cli := filepath.Join(a.workDir, "runtime", "worker", "node_modules", "wrangler", "bin", "wrangler.js")
	if _, err := os.Stat(cli); err != nil {
		return "", fmt.Errorf("Wrangler is not prepared")
	}
	env, err := a.wranglerEnvironment(node)
	if err != nil {
		return "", err
	}
	return runWithTimeoutEnv(dir, node, append([]string{cli}, args...), stdin, timeout, env)
}

func managedNodeEnvironment(node string) map[string]string {
	path := filepath.Dir(node)
	if current := os.Getenv("PATH"); current != "" {
		path += string(os.PathListSeparator) + current
	}
	return map[string]string{"PATH": path, "NODE_OPTIONS": "", "NODE_PATH": ""}
}

func (a *App) putWranglerSecret(workerDir, name, value string) error {
	if _, err := a.runWranglerIn(workerDir, wranglerSecretArgs(name), value+"\n", 3*time.Minute); err != nil {
		return fmt.Errorf("Cloudflare secret %s: %w", name, err)
	}
	return nil
}

func wranglerSecretArgs(name string) []string {
	return []string{"secret", "put", name, "--x-auto-create=false"}
}

func wranglerDeployArgs(config string) []string {
	args := []string{"deploy", "--autoconfig=false", "--x-auto-create=false"}
	if config != "" {
		args = append(args, "--config", config)
	}
	return args
}

var cloudflareWorkersOnboardingPattern = regexp.MustCompile(`https://dash\.cloudflare\.com/([A-Za-z0-9_-]{16,128})/workers/onboarding(?:\s|$)`)

func cloudflareEmailVerificationRequired(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "verify your email address to use workers") ||
		(strings.Contains(value, "verify your email") && strings.Contains(value, "workers"))
}

func (a *App) cloudflareWorkerPreparationError(err error) error {
	a.logText("Technisches Detail zur Cloudflare-Bereitschaftsprüfung: %v", "Technical Cloudflare readiness detail: %v", err)
	if cloudflareEmailVerificationRequired(err.Error()) {
		return fmt.Errorf("%s", a.text(
			"Cloudflare hat die Bereitstellung blockiert, weil die E-Mail-Adresse deines Cloudflare-Kontos noch nicht bestätigt ist. Öffne die Bestätigungs-E-Mail von Cloudflare (prüfe auch den Spam-Ordner), bestätige die Adresse und klicke danach auf „Installation fortsetzen“. Kein Code und keine Neuinstallation sind erforderlich.",
			"Cloudflare blocked deployment because your Cloudflare account email address has not been verified yet. Open Cloudflare's verification email (also check spam), verify the address, then click “Continue installation”. No code or reinstallation is required.",
		))
	}
	return fmt.Errorf("%s", a.text(
		"Cloudflare konnte den privaten Vorabtest nicht abschließen. Die Prüfung wurde vor weiteren Fly-Ressourcenschritten angehalten. Prüfe die Internetverbindung und die Cloudflare-Anmeldung und klicke danach erneut auf „Installation fortsetzen“.",
		"Cloudflare could not complete the private readiness check. The check stopped before any further Fly resource steps. Check the internet connection and Cloudflare sign-in, then click “Continue installation” again.",
	))
}

func cloudflareWorkersOnboardingURL(value string) string {
	match := cloudflareWorkersOnboardingPattern.FindStringSubmatch(value)
	if len(match) != 2 || !cloudflareAccountIDPattern.MatchString(match[1]) {
		return ""
	}
	return "https://dash.cloudflare.com/" + match[1] + "/workers/onboarding"
}

func extractFlyToken(output string) string {
	matches := make([]string, 0, 1)
	for _, line := range strings.Split(cleanCommandOutput(output), "\n") {
		candidate := strings.TrimSpace(line)
		if validFlyControlToken(candidate) {
			matches = append(matches, candidate)
		}
	}
	if len(matches) != 1 {
		return ""
	}
	return matches[0]
}

func extractWorkersURL(output string) string {
	return regexp.MustCompile(`https://[A-Za-z0-9._-]+\.workers\.dev`).FindString(output)
}

func setDashboardSettings(baseURL, password string, settings any) error {
	base, err := normalizeDashboardURL(baseURL)
	if err != nil || base != baseURL {
		return fmt.Errorf("ungültige Dashboard-Adresse")
	}
	body, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	var last int
	for attempt := 0; attempt < 8; attempt++ {
		req, err := http.NewRequest(http.MethodPut, base+"/api/admin/settings", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.SetBasicAuth("admin", password)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", base)
		resp, err := client.Do(req)
		if err == nil {
			last = resp.StatusCode
			_ = resp.Body.Close()
			if last >= 200 && last < 300 {
				return nil
			}
		}
		time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
	}
	if last == 0 {
		return fmt.Errorf("Dashboard ist nicht erreichbar")
	}
	return fmt.Errorf("Dashboard HTTP %d", last)
}
