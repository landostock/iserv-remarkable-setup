package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type diagnosticInstallation struct {
	Present             bool   `json:"present"`
	InstalledVersion    string `json:"installed_version,omitempty"`
	Region              string `json:"region,omitempty"`
	AppConfigured       bool   `json:"fly_app_configured"`
	WorkerConfigured    bool   `json:"worker_configured"`
	DashboardConfigured bool   `json:"dashboard_configured"`
	CreatedAt           string `json:"created_at,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
}

type diagnosticCloudHealth struct {
	DashboardReachable bool `json:"dashboard_reachable"`
	DashboardHTTP      int  `json:"dashboard_http,omitempty"`
	FlyToolPresent     bool `json:"fly_tool_present"`
	FlyAuthenticated   bool `json:"fly_authenticated"`
	FlyAppReachable    bool `json:"fly_app_reachable"`
	FlyMachineCount    int  `json:"fly_machine_count,omitempty"`
}

type diagnosticReport struct {
	Format             int                    `json:"format"`
	GeneratedAt        string                 `json:"generated_at"`
	SetupVersion       string                 `json:"setup_version"`
	OS                 string                 `json:"os"`
	Architecture       string                 `json:"architecture"`
	Language           string                 `json:"language"`
	Installation       diagnosticInstallation `json:"installation"`
	CloudHealth        diagnosticCloudHealth  `json:"cloud_health"`
	InstallStatus      string                 `json:"install_status"`
	InstallPhase       string                 `json:"install_phase,omitempty"`
	InstallProgress    int                    `json:"install_progress,omitempty"`
	MaintenanceStatus  string                 `json:"maintenance_status,omitempty"`
	MaintenanceAction  string                 `json:"maintenance_action,omitempty"`
	MaintenanceError   string                 `json:"maintenance_error,omitempty"`
	LastVerification   *verificationResult    `json:"last_safe_verification,omitempty"`
	FirstRealStatus    string                 `json:"first_real_status,omitempty"`
	FirstRealError     string                 `json:"first_real_error,omitempty"`
	LastRealRun        *verificationResult    `json:"last_real_run,omitempty"`
	RecentMessages     []string               `json:"recent_messages,omitempty"`
	PrivacyDescription string                 `json:"privacy_note"`
}

var diagnosticCredentialRedactors = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*(?:bearer\s+)?)([^\s]+)`),
	regexp.MustCompile(`(?i)(bearer\s+)([^\s]+)`),
	regexp.MustCompile(`(?i)(FlyV1\s+)([^\s]+)`),
	regexp.MustCompile(`(?i)((?:RCLONE_CONFIG_ISERV_(?:USER|PASS)|ISERV_WEBDAV_USER_B64|RMAPI_CONFIG_B64|ADMIN_PASSWORD|SYNC_API_TOKEN|FLY_API_TOKEN)\s*[:=]\s*)([^\s]+)`),
	regexp.MustCompile(`(?i)((?:password|passwort|pass|token|secret|devicetoken|usertoken|config_b64)\s*[:=]\s*)([^\s]+)`),
}

var diagnosticURLRedactor = regexp.MustCompile(`(?i)https?://[^\s"']+`)
var diagnosticEmailRedactor = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

func diagnosticHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("diagnostic health check does not follow redirects")
		},
	}
}

func redactDiagnosticText(value string) string {
	s := value
	for _, re := range diagnosticCredentialRedactors {
		s = re.ReplaceAllString(s, `${1}[redacted]`)
	}
	s = diagnosticURLRedactor.ReplaceAllString(s, "[url redacted]")
	s = diagnosticEmailRedactor.ReplaceAllString(s, "[email redacted]")
	return redactHomePaths(s)
}

func redactDiagnosticVerification(value *verificationResult) *verificationResult {
	if value == nil {
		return nil
	}
	redacted := *value
	redacted.Reason = redactDiagnosticText(redacted.Reason)
	redacted.Message = redactDiagnosticText(redacted.Message)
	return &redacted
}

func (a *App) diagnosticsCloudHealth(meta *installMeta) diagnosticCloudHealth {
	result := diagnosticCloudHealth{}
	if meta == nil {
		return result
	}
	if strings.HasPrefix(meta.DashboardURL, "https://") {
		client := diagnosticHTTPClient()
		if resp, err := client.Get(strings.TrimSuffix(meta.DashboardURL, "/") + "/healthz"); err == nil {
			result.DashboardHTTP = resp.StatusCode
			result.DashboardReachable = resp.StatusCode >= 200 && resp.StatusCode < 300
			resp.Body.Close()
		}
	}
	name := "flyctl"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	fly := filepath.Join(a.tools.root, "flyctl-"+managedFlyVersion, name)
	if !isExecutableFile(fly) {
		return result
	}
	result.FlyToolPresent = true
	if _, err := a.runFly("", fly, []string{"auth", "whoami"}, "", 12*time.Second); err != nil {
		return result
	}
	result.FlyAuthenticated = true
	if _, err := a.runFly("", fly, []string{"status", "-a", meta.AppName}, "", 15*time.Second); err != nil {
		return result
	}
	result.FlyAppReachable = true
	if out, err := a.runFly("", fly, []string{"machine", "list", "-a", meta.AppName, "--json"}, "", 20*time.Second); err == nil {
		result.FlyMachineCount = countJSONMachines(out)
	}
	return result
}

func (a *App) createDiagnostics() (string, error) {
	a.st.mu.RLock()
	meta := a.st.Meta
	lang := a.st.Language
	installStatus := a.st.InstallStatus
	installPhase := a.st.InstallPhase
	installProgress := a.st.InstallProgress
	maintenanceStatus := a.st.MaintenanceStatus
	maintenanceAction := a.st.MaintenanceAction
	maintenanceError := a.st.MaintenanceError
	verification := a.st.Verification
	logs := append([]string(nil), a.st.Logs...)
	a.st.mu.RUnlock()

	inst := diagnosticInstallation{}
	if meta != nil {
		inst = diagnosticInstallation{
			Present:             true,
			InstalledVersion:    meta.Version,
			Region:              meta.Region,
			AppConfigured:       strings.TrimSpace(meta.AppName) != "",
			WorkerConfigured:    strings.TrimSpace(meta.WorkerName) != "",
			DashboardConfigured: strings.HasPrefix(meta.DashboardURL, "https://"),
			CreatedAt:           meta.CreatedAt.UTC().Format(time.RFC3339),
		}
		if !meta.UpdatedAt.IsZero() {
			inst.UpdatedAt = meta.UpdatedAt.UTC().Format(time.RFC3339)
		}
	}
	if len(logs) > 80 {
		logs = logs[len(logs)-80:]
	}
	for i := range logs {
		logs[i] = redactDiagnosticText(logs[i])
	}
	firstReal := a.loadFirstRealState(meta)
	report := diagnosticReport{
		Format:             1,
		GeneratedAt:        time.Now().UTC().Format(time.RFC3339),
		SetupVersion:       appVersion,
		OS:                 runtime.GOOS,
		Architecture:       runtime.GOARCH,
		Language:           lang,
		Installation:       inst,
		CloudHealth:        a.diagnosticsCloudHealth(meta),
		InstallStatus:      installStatus,
		InstallPhase:       installPhase,
		InstallProgress:    installProgress,
		MaintenanceStatus:  maintenanceStatus,
		MaintenanceAction:  maintenanceAction,
		MaintenanceError:   redactDiagnosticText(maintenanceError),
		LastVerification:   redactDiagnosticVerification(verification),
		FirstRealStatus:    firstReal.Status,
		FirstRealError:     redactDiagnosticText(firstReal.Error),
		LastRealRun:        redactDiagnosticVerification(firstReal.Result),
		RecentMessages:     logs,
		PrivacyDescription: "This report is designed to omit known passwords, authentication tokens, IServ usernames, IServ/dashboard URLs, and document names from structured fields. Free-form diagnostic text receives best-effort credential, URL, email, and home-path redaction before export.",
	}

	dir := filepath.Join(a.workDir, "diagnostics")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	name := "iserv-remarkable-diagnostics-" + time.Now().Format("20060102-150405") + ".json"
	path := filepath.Join(dir, name)
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	if err := atomicWritePrivate(path, raw); err != nil {
		return "", err
	}
	a.st.mu.Lock()
	a.st.DiagnosticsPath = path
	a.st.mu.Unlock()
	a.logText("Datenschutzfreundlicher Diagnosebericht wurde erstellt.", "Privacy-friendly diagnostic report was created.")
	return path, nil
}

func (a *App) openDiagnosticsFolder() error {
	a.st.mu.RLock()
	path := a.st.DiagnosticsPath
	a.st.mu.RUnlock()
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("noch kein Diagnosebericht erstellt")
	}
	return openPath(filepath.Dir(path))
}
