package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type recoveryMetadata struct {
	Format        int    `json:"format"`
	AppName       string `json:"app_name"`
	WorkerName    string `json:"worker_name"`
	DashboardURL  string `json:"dashboard_url"`
	Region        string `json:"region"`
	MachineID     string `json:"machine_id"`
	KVNamespaceID string `json:"kv_namespace_id"`
	FlyTokenUntil string `json:"fly_token_until"`
	Version       string `json:"version"`
}

var cloudNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)
var flyRegionPattern = regexp.MustCompile(`^[a-z0-9-]{2,20}$`)
var flyMachinePattern = regexp.MustCompile(`^[a-z0-9]{10,64}$`)
var kvNamespacePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,100}$`)

func normalizeDashboardURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("Dashboard-Adresse muss eine gültige HTTPS-Adresse sein")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("Dashboard-Adresse darf keine Parameter enthalten")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if u.Path != "" {
		return "", fmt.Errorf("Dashboard-Adresse muss auf die Hauptadresse des Dashboards zeigen")
	}
	return u.Scheme + "://" + u.Host, nil
}

func (a *App) recoverExistingInstallation(dashboardURL, password string) (*installMeta, error) {
	base, err := normalizeDashboardURL(dashboardURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("Dashboard-Passwort fehlt")
	}
	raw, status, err := adminRequest(base, password, http.MethodGet, "/api/admin/recovery", nil)
	if err != nil {
		return nil, fmt.Errorf("Dashboard konnte nicht erreicht werden: %w", err)
	}
	if status == http.StatusUnauthorized {
		return nil, fmt.Errorf("Dashboard-Passwort ist nicht korrekt")
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("Dashboard unterstützt die sichere Wiederherstellung noch nicht (HTTP %d)", status)
	}
	var recovered recoveryMetadata
	if err := json.Unmarshal(raw, &recovered); err != nil {
		return nil, fmt.Errorf("Wiederherstellungsdaten sind ungültig")
	}
	returnedBase, err := normalizeDashboardURL(recovered.DashboardURL)
	if err != nil || returnedBase != base {
		return nil, fmt.Errorf("Wiederherstellungsdaten gehören nicht zu diesem Dashboard")
	}
	if recovered.Format != 1 || !cloudNamePattern.MatchString(recovered.AppName) || !cloudNamePattern.MatchString(recovered.WorkerName) || !flyRegionPattern.MatchString(recovered.Region) || !flyMachinePattern.MatchString(recovered.MachineID) || !kvNamespacePattern.MatchString(recovered.KVNamespaceID) {
		return nil, fmt.Errorf("Wiederherstellungsdaten sind unvollständig")
	}
	if recovered.FlyTokenUntil != "" {
		if _, err := time.Parse(time.RFC3339, recovered.FlyTokenUntil); err != nil {
			return nil, fmt.Errorf("Wiederherstellungsdaten enthalten ein ungültiges Ablaufdatum")
		}
	}
	version := strings.TrimSpace(recovered.Version)
	if version == "" {
		version = "unknown"
	}
	meta := &installMeta{
		AppName:       recovered.AppName,
		WorkerName:    recovered.WorkerName,
		DashboardURL:  base,
		Region:        recovered.Region,
		MachineID:     recovered.MachineID,
		KVNamespaceID: recovered.KVNamespaceID,
		FlyTokenUntil: recovered.FlyTokenUntil,
		Version:       version,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := a.rebuildRecoveredRuntime(meta); err != nil {
		return nil, err
	}
	if err := a.saveMeta(meta); err != nil {
		return nil, err
	}
	a.logText("Bestehende Installation wurde auf diesem Computer sicher wieder verbunden.", "Existing installation was securely reconnected on this computer.")
	return meta, nil
}

func (a *App) rebuildRecoveredRuntime(meta *installMeta) error {
	if meta == nil {
		return fmt.Errorf("Wiederherstellungsdaten fehlen")
	}
	runtimeDir := filepath.Join(a.workDir, "runtime")
	workerDir := filepath.Join(runtimeDir, "worker")
	flyToml := filepath.Join(runtimeDir, "fly.toml")
	wrangler := filepath.Join(workerDir, "wrangler.jsonc")
	if err := copyFile(filepath.Join(runtimeDir, "fly.toml.template"), flyToml); err != nil {
		return err
	}
	if err := replaceFile(flyToml, map[string]string{"__APP_NAME__": meta.AppName, "__REGION__": meta.Region}); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(workerDir, "wrangler.jsonc.template"), wrangler); err != nil {
		return err
	}
	if err := replaceFile(wrangler, map[string]string{
		"__WORKER_NAME__":     meta.WorkerName,
		"__APP_NAME__":        meta.AppName,
		"__MACHINE_ID__":      meta.MachineID,
		"__REGION__":          meta.Region,
		"__INSTALL_VERSION__": appVersion,
		"__KV_ID__":           meta.KVNamespaceID,
		"__FLY_TOKEN_UNTIL__": meta.FlyTokenUntil,
	}); err != nil {
		return err
	}
	raw, err := os.ReadFile(wrangler)
	if err != nil {
		return err
	}
	text := string(raw)
	if !strings.Contains(text, `"kv_namespaces"`) {
		needle := `  "triggers": {`
		block := fmt.Sprintf("  \"kv_namespaces\": [\n    { \"binding\": \"CONFIG\", \"id\": \"%s\" }\n  ],\n", meta.KVNamespaceID)
		if !strings.Contains(text, needle) {
			return fmt.Errorf("Worker-Konfiguration ist unvollständig")
		}
		text = strings.Replace(text, needle, block+needle, 1)
	}
	return atomicWritePrivate(wrangler, []byte(text))
}
