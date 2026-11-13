package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const setupCookieName = "iserv_rm_setup_session"
const maxSetupRequestBody int64 = 512 << 10

func setupTokenEqual(left, right string) bool {
	if len(left) != len(right) || len(left) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func setSetupSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
}

type App struct {
	workDir            string
	token              string
	st                 *state
	tools              *ToolManager
	flyLoginMu         sync.Mutex
	flyAuthMu          sync.RWMutex
	flyAPIToken        string
	flyCodeMu          sync.RWMutex
	flyCodeBroker      *flyCodeBroker
	flyControlTokenMu  sync.Mutex
	pendingFlyControl  *pendingFlyControlToken
	workerDepsMu       sync.Mutex
	workerDepsReady    bool
	remarkableFolderMu sync.Mutex
	settingsMu         sync.Mutex
	quit               chan struct{}
	quitOnce           sync.Once
	localStateCorrupt  bool
}

func NewApp(workDir, token string) *App {
	st := &state{InstallStatus: "idle", MaintenanceStatus: "idle", Language: "de", Config: defaultConfig(), Logs: []string{}}
	meta, corrupt := loadExistingMeta(workDir)
	st.Meta = meta
	return &App{workDir: workDir, token: token, st: st, tools: NewToolManager(workDir), quit: make(chan struct{}), localStateCorrupt: corrupt}
}

func requestLang(r *http.Request) string {
	l := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Setup-Lang")))
	if strings.HasPrefix(l, "en") {
		return "en"
	}
	return "de"
}
func localize(r *http.Request, de, en string) string {
	if requestLang(r) == "en" {
		return en
	}
	return de
}
func (a *App) text(de, en string) string {
	a.st.mu.RLock()
	l := a.st.Language
	a.st.mu.RUnlock()
	if l == "en" {
		return en
	}
	return de
}
func (a *App) logText(de, en string, args ...any) { a.logf(a.text(de, en), args...) }
func writeAPIError(w http.ResponseWriter, r *http.Request, status int, de, en string) {
	writeJSON(w, status, map[string]string{"error": localize(r, de, en)})
}

func (a *App) logf(format string, args ...any) {
	line := time.Now().Format("15:04:05") + "  " + fmt.Sprintf(format, args...)
	a.st.mu.Lock()
	a.st.Logs = append(a.st.Logs, line)
	if len(a.st.Logs) > 300 {
		a.st.Logs = a.st.Logs[len(a.st.Logs)-300:]
	}
	a.st.mu.Unlock()
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/api/state", a.secure(a.handleState))
	mux.HandleFunc("/api/system/check", a.secure(a.handleSystemCheck))
	mux.HandleFunc("/api/recovery/connect", a.secure(a.handleRecoveryConnect))
	mux.HandleFunc("/api/cost/accept", a.secure(a.handleCostAccept))
	mux.HandleFunc("/api/fly/login", a.secure(a.handleFlyLogin))
	mux.HandleFunc("/api/fly/code", a.secure(a.handleFlyCode))
	mux.HandleFunc("/api/cloudflare/login", a.secure(a.handleCloudflareLogin))
	mux.HandleFunc("/api/iserv/connect", a.secure(a.handleIServConnect))
	mux.HandleFunc("/api/iserv/list", a.secure(a.handleIServList))
	mux.HandleFunc("/api/remarkable/connect", a.secure(a.handleRemarkableConnect))
	mux.HandleFunc("/api/remarkable/list", a.secure(a.handleRemarkableList))
	mux.HandleFunc("/api/config", a.secure(a.handleConfig))
	mux.HandleFunc("/api/install/start", a.secure(a.handleInstallStart))
	mux.HandleFunc("/api/dashboard/open", a.secure(a.handleDashboardOpen))
	mux.HandleFunc("/api/maintenance/check", a.secure(a.handleMaintenanceCheck))
	mux.HandleFunc("/api/maintenance/update", a.secure(a.handleMaintenanceUpdate))
	mux.HandleFunc("/api/maintenance/uninstall", a.secure(a.handleMaintenanceUninstall))
	mux.HandleFunc("/api/maintenance/credentials", a.secure(a.handleMaintenanceCredentials))
	mux.HandleFunc("/api/maintenance/password", a.secure(a.handleMaintenancePassword))
	mux.HandleFunc("/api/maintenance/verify", a.secure(a.handleMaintenanceVerify))
	mux.HandleFunc("/api/maintenance/mappings/read", a.secure(a.handleMappingsRead))
	mux.HandleFunc("/api/maintenance/mappings/save", a.secure(a.handleMappingsSave))
	mux.HandleFunc("/api/maintenance/diagnostics", a.secure(a.handleMaintenanceDiagnostics))
	mux.HandleFunc("/api/maintenance/diagnostics/open", a.secure(a.handleMaintenanceDiagnosticsOpen))
	mux.HandleFunc("/api/app/update/check", a.secure(a.handleAppUpdateCheck))
	mux.HandleFunc("/api/app/update/download", a.secure(a.handleAppUpdateDownload))
	mux.HandleFunc("/api/app/quit", a.secure(a.handleQuit))
	return mux
}

func (a *App) secure(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setSetupSecurityHeaders(w)
		r.Body = http.MaxBytesReader(w, r.Body, maxSetupRequestBody)
		if !setupTokenEqual(r.Header.Get("X-Setup-Token"), a.token) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		a.st.mu.Lock()
		a.st.Language = requestLang(r)
		a.st.mu.Unlock()
		w.Header().Set("Cache-Control", "no-store")
		next(w, r)
	}
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	queryOK := setupTokenEqual(r.URL.Query().Get("token"), a.token)
	cookieOK := false
	if c, err := r.Cookie(setupCookieName); err == nil && setupTokenEqual(c.Value, a.token) {
		cookieOK = true
	}
	if !queryOK && !cookieOK {
		http.Error(w, "Invalid setup session", http.StatusForbidden)
		return
	}
	if queryOK {
		http.SetCookie(w, &http.Cookie{Name: setupCookieName, Value: a.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	}
	raw, err := embedded.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	nonce, err := randomHex(16)
	if err != nil {
		http.Error(w, "setup session error", http.StatusInternalServerError)
		return
	}
	page := strings.ReplaceAll(string(raw), "__SETUP_TOKEN__", a.token)
	mappingsScript, err := embedded.ReadFile("web/mappings.js")
	if err != nil {
		http.Error(w, "setup assets unavailable", 500)
		return
	}
	page = strings.ReplaceAll(page, "__MAPPINGS_SCRIPT__", string(mappingsScript))
	page = strings.ReplaceAll(page, "__CSP_NONCE__", nonce)
	page = strings.ReplaceAll(page, "__APP_VERSION__", appVersion)
	setSetupSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", fmt.Sprintf("default-src 'none'; script-src 'nonce-%s'; style-src 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'", nonce))
	_, _ = w.Write([]byte(page))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func cloneConfig(cfg InstallConfig) InstallConfig {
	out := cfg
	out.Extensions = append([]string(nil), cfg.Extensions...)
	out.Sources = append([]Source(nil), cfg.Sources...)
	out.Schedule = make(map[string][]Window, len(cfg.Schedule))
	for day, windows := range cfg.Schedule {
		out.Schedule[day] = append([]Window(nil), windows...)
	}
	return out
}

func (a *App) snapshot() map[string]any {
	a.st.mu.RLock()
	defer a.st.mu.RUnlock()
	cfg := cloneConfig(a.st.Config)
	cfg.AdminPassword = ""
	var meta *installMeta
	if a.st.Meta != nil {
		copy := *a.st.Meta
		meta = &copy
	}
	var verification *verificationResult
	if a.st.Verification != nil {
		copy := *a.st.Verification
		verification = &copy
	}
	return map[string]any{"version": appVersion, "local_state_corrupt": a.localStateCorrupt, "cost_accepted": a.st.CostAccepted, "fly_logged_in": a.st.FlyLoggedIn, "fly_organizations": append([]flyOrganization(nil), a.st.FlyOrganizations...), "cloudflare_logged_in": a.st.CloudflareLoggedIn, "iserv_connected": a.st.IServConnected, "remarkable_connected": a.st.RemarkableConnected, "install_status": a.st.InstallStatus, "install_phase": a.st.InstallPhase, "install_progress": a.st.InstallProgress, "install_error": a.st.InstallError, "maintenance_status": a.st.MaintenanceStatus, "maintenance_action": a.st.MaintenanceAction, "maintenance_error": a.st.MaintenanceError, "verification": verification, "logs": append([]string(nil), a.st.Logs...), "meta": meta, "config": cfg}
}
func (a *App) handleState(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.snapshot()) }

func (a *App) handleRecoveryConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	a.st.mu.RLock()
	hasMeta := a.st.Meta != nil
	a.st.mu.RUnlock()
	if hasMeta {
		writeAPIError(w, r, http.StatusConflict, "Auf diesem Computer ist bereits eine Installation verbunden.", "An installation is already connected on this computer.")
		return
	}
	var in struct {
		DashboardURL string `json:"dashboard_url"`
		Password     string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Ungültige Eingabe.", "Invalid input.")
		return
	}
	meta, err := a.recoverExistingInstallation(in.DashboardURL, in.Password)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": a.errorText(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "meta": meta})
}

func (a *App) handleSystemCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	writeJSON(w, 200, runPreflight(requestLang(r)))
}
func (a *App) handleCostAccept(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	var in struct {
		Accepted bool `json:"accepted"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || !in.Accepted {
		writeAPIError(w, r, 400, "Bestätigung fehlt", "Confirmation is required")
		return
	}
	a.st.mu.Lock()
	a.st.CostAccepted = true
	a.st.mu.Unlock()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) handleFlyLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	a.st.mu.Lock()
	a.st.FlyLoggedIn = false
	a.st.FlyOrganizations = nil
	a.st.mu.Unlock()
	var in struct {
		Force bool `json:"force"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.Force {
		a.setFlyAPIToken("")
		a.clearAllPendingFlyControlTokens()
	}
	fly, err := a.tools.FlyPath(a.localizedLogf)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": a.errorText(err)})
		return
	}
	out, err := a.ensureFlySession(r.Context(), fly,
		"Fly.io-Anmeldung wird im Browser geöffnet …",
		"Fly.io sign-in is opening in your browser …")
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": a.errorText(err)})
		return
	}
	organizationOutput, err := a.runFly("", fly, []string{"orgs", "list", "--json"}, "", 30*time.Second)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": a.errorText(fmt.Errorf("Fly-Organisationen: %w", err))})
		return
	}
	organizations, err := decodeFlyOrganizations(organizationOutput)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": a.errorText(err)})
		return
	}
	a.st.mu.Lock()
	selected, selectErr := chooseFlyOrganization(organizations, a.st.Config.FlyOrgSlug)
	if selectErr != nil {
		selected, _ = chooseFlyOrganization(organizations, "")
	}
	a.st.Config.FlyOrgSlug = selected
	a.st.FlyOrganizations = append([]flyOrganization(nil), organizations...)
	a.st.FlyLoggedIn = true
	a.st.mu.Unlock()
	writeJSON(w, 200, map[string]any{"ok": true, "account": strings.TrimSpace(out), "organizations": organizations, "selected_org": selected})
}

func (a *App) handleFlyCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Ungültiger Fly.io-Code.", "Invalid Fly.io code.")
		return
	}
	code := strings.TrimSpace(in.Code)
	if code == "" || len(code) > 4096 || strings.ContainsAny(code, "\x00\r\n") {
		writeAPIError(w, r, http.StatusBadRequest, "Ungültiger Fly.io-Code.", "Invalid Fly.io code.")
		return
	}

	a.flyCodeMu.RLock()
	broker := a.flyCodeBroker
	a.flyCodeMu.RUnlock()
	if broker == nil {
		writeAPIError(w, r, http.StatusConflict, "Keine Fly.io-Anmeldung wartet auf einen Code.", "No Fly.io sign-in is waiting for a code.")
		return
	}
	submission := flyCodeSubmission{code: code, result: make(chan error, 1)}
	select {
	case broker.submissions <- submission:
	case <-broker.done:
		writeAPIError(w, r, http.StatusConflict, "Die Fly.io-Anmeldung ist nicht mehr aktiv.", "The Fly.io sign-in is no longer active.")
		return
	case <-r.Context().Done():
		return
	}
	select {
	case err := <-submission.result:
		if err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "Der Fly.io-Code wurde nicht akzeptiert. Prüfe ihn und versuche es erneut.", "Fly.io did not accept the code. Check it and try again.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case <-r.Context().Done():
	}
}

func (a *App) handleCloudflareLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	a.st.mu.Lock()
	a.st.CloudflareLoggedIn = false
	a.st.mu.Unlock()
	var in struct {
		Force bool `json:"force"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if err := a.ensureWorkerDeps(); err != nil {
		writeJSON(w, 400, map[string]string{"error": a.errorText(err)})
		return
	}
	if _, err := a.cloudflareIdentity(); err != nil || in.Force {
		a.logText("Cloudflare-Anmeldung wird im Browser geöffnet …", "Cloudflare sign-in is opening in your browser …")
		if _, err := a.runWrangler([]string{"login", "--no-use-keyring"}, "", 8*time.Minute); err != nil {
			writeJSON(w, 400, map[string]string{"error": a.errorText(err)})
			return
		}
	}
	account, err := a.cloudflareIdentity()
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "Cloudflare-Anmeldung konnte nicht bestätigt werden.", "Cloudflare sign-in could not be confirmed.")
		return
	}
	a.st.mu.Lock()
	a.st.CloudflareLoggedIn = true
	a.st.mu.Unlock()
	writeJSON(w, 200, map[string]any{"ok": true, "account": account})
}

func (a *App) handleIServConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	var in struct{ URL, User, Password string }
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeAPIError(w, r, 400, "Ungültige Eingabe", "Invalid input")
		return
	}
	in.URL = strings.TrimSpace(in.URL)
	in.User = strings.TrimSpace(in.User)
	if in.User == "" || len(in.User) > 256 || strings.ContainsAny(in.User, "\x00\r\n") || in.Password == "" || len(in.Password) > 4096 {
		writeAPIError(w, r, 400, "Ungültige IServ-Zugangsdaten", "Invalid IServ credentials")
		return
	}
	dirs, err := listWebDAV(in.URL, in.User, in.Password, "")
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": webDAVErrorForLang(err, requestLang(r))})
		return
	}
	a.st.mu.Lock()
	a.st.Secrets.IServURL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	a.st.Secrets.IServUser = in.User
	a.st.Secrets.IServPassword = in.Password
	a.st.IServConnected = true
	a.st.mu.Unlock()
	writeJSON(w, 200, map[string]any{"ok": true, "directories": dirs})
}
func (a *App) handleIServList(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	var in struct {
		Path string `json:"path"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	a.st.mu.RLock()
	sec := a.st.Secrets
	ok := a.st.IServConnected
	a.st.mu.RUnlock()
	if !ok {
		writeAPIError(w, r, 400, "IServ ist noch nicht verbunden", "IServ is not connected yet")
		return
	}
	dirs, err := listWebDAV(sec.IServURL, sec.IServUser, sec.IServPassword, in.Path)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": webDAVErrorForLang(err, requestLang(r))})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "directories": dirs})
}

func (a *App) handleRemarkableConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeAPIError(w, r, 400, "Ungültige Eingabe", "Invalid input")
		return
	}
	device, user, err := registerRemarkable(in.Code)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": remarkableErrorForLang(err, requestLang(r))})
		return
	}
	a.remarkableFolderMu.Lock()
	defer a.remarkableFolderMu.Unlock()
	a.st.mu.Lock()
	a.st.Secrets.DeviceToken = device
	a.st.Secrets.UserToken = user
	a.st.RemarkableConnected = true
	a.st.RemarkableFolders = nil
	a.st.mu.Unlock()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) handleRemarkableList(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	var in struct {
		Path    string `json:"path"`
		Refresh bool   `json:"refresh"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	a.remarkableFolderMu.Lock()
	defer a.remarkableFolderMu.Unlock()
	a.st.mu.RLock()
	connected := a.st.RemarkableConnected
	device := a.st.Secrets.DeviceToken
	user := a.st.Secrets.UserToken
	cached := append([]string(nil), a.st.RemarkableFolders...)
	a.st.mu.RUnlock()
	if !connected || device == "" || user == "" {
		writeAPIError(w, r, 400, "reMarkable ist noch nicht verbunden", "reMarkable is not connected yet")
		return
	}
	if len(cached) == 0 || in.Refresh {
		folders, err := a.loadRemarkableFolders(device, user)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": a.errorText(err)})
			return
		}
		cached = folders
		a.st.mu.Lock()
		a.st.RemarkableFolders = append([]string(nil), folders...)
		a.st.mu.Unlock()
	}
	current := normalizeRemarkablePath(in.Path)
	writeJSON(w, 200, map[string]any{"ok": true, "path": current, "directories": remarkableChildren(cached, current)})
}

var regionRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,20}$`)
var hhmmRe = regexp.MustCompile(`^(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)

func safePathPart(s string) bool {
	if s == "" || len(s) > 500 || strings.Contains(s, "\\") || strings.Contains(s, "//") || strings.ContainsAny(s, "\x00\r\n") {
		return false
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func validConfig(cfg InstallConfig) error {
	if !regionRe.MatchString(cfg.Region) {
		return fmt.Errorf("ungültige Fly-Region")
	}
	if cfg.FlyOrgSlug != "" && !flyOrgSlugPattern.MatchString(cfg.FlyOrgSlug) {
		return fmt.Errorf("ungültige Fly-Organisation")
	}
	allowed := map[int]bool{5: true, 10: true, 15: true, 20: true, 30: true, 60: true}
	if !allowed[cfg.IntervalMinutes] {
		return fmt.Errorf("ungültiges Intervall")
	}
	if cfg.LookbackDays < 1 || cfg.LookbackDays > 3650 {
		return fmt.Errorf("Rückblick muss 1–3650 Tage betragen")
	}
	if cfg.MaxFileMB < 1 || cfg.MaxFileMB > 95 {
		return fmt.Errorf("Dateigröße muss 1–95 MB betragen")
	}
	if len(cfg.AdminPassword) < 16 || len(cfg.AdminPassword) > 256 || strings.ContainsAny(cfg.AdminPassword, "\x00\r\n") {
		return fmt.Errorf("Dashboard-Passwort muss 16–256 Zeichen lang sein und darf keine Steuerzeichen enthalten")
	}
	if len(cfg.Schedule) > 7 {
		return fmt.Errorf("ungültiger Wochenplan")
	}
	for d, ws := range cfg.Schedule {
		if d < "1" || d > "7" || len(d) != 1 || len(ws) > 8 {
			return fmt.Errorf("ungültiger Wochenplan")
		}
		for _, win := range ws {
			if !hhmmRe.MatchString(win.Start) || !hhmmRe.MatchString(win.End) || win.Start > win.End {
				return fmt.Errorf("ungültiges Zeitfenster")
			}
		}
	}
	if len(cfg.Extensions) < 1 || len(cfg.Extensions) > 2 {
		return fmt.Errorf("mindestens ein Dateityp erforderlich")
	}
	for _, e := range cfg.Extensions {
		if e != "pdf" && e != "epub" {
			return fmt.Errorf("ungültiger Dateityp")
		}
	}
	if len(cfg.Sources) < 1 || len(cfg.Sources) > 20 {
		return fmt.Errorf("1–20 Ordnerzuordnungen erforderlich")
	}
	names := map[string]bool{}
	for _, source := range cfg.Sources {
		name := strings.TrimSpace(source.Name)
		if name == "" || len(name) > 100 || strings.ContainsAny(name, "\x00\r\n") || names[strings.ToLower(name)] {
			return fmt.Errorf("Zuordnungsnamen müssen eindeutig sein")
		}
		names[strings.ToLower(name)] = true
		rmDir := strings.TrimSpace(source.RemarkableDir)
		if !safePathPart(strings.Trim(source.IServPath, "/")) || !strings.HasPrefix(rmDir, "/") || (rmDir != "/" && !safePathPart(strings.TrimPrefix(rmDir, "/"))) {
			return fmt.Errorf("ungültiger Ordnerpfad")
		}
	}
	return nil
}
func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		http.Error(w, "method", 405)
		return
	}
	var cfg InstallConfig
	if json.NewDecoder(r.Body).Decode(&cfg) != nil {
		writeAPIError(w, r, 400, "Konfiguration konnte nicht gelesen werden", "Configuration could not be read")
		return
	}
	if err := validConfig(cfg); err != nil {
		writeJSON(w, 400, map[string]string{"error": a.errorText(err)})
		return
	}
	a.st.mu.Lock()
	a.st.Config = cloneConfig(cfg)
	a.st.mu.Unlock()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) handleInstallStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	preparing := localize(r, "Installation wird vorbereitet …", "Preparing installation …")
	stopped := localize(r, "Installation angehalten", "Installation stopped")
	done := localize(r, "Fertig", "Done")
	a.st.mu.Lock()
	if a.localStateCorrupt {
		a.st.mu.Unlock()
		writeAPIError(w, r, 409, "Die lokalen Installationsdaten sind beschädigt. Nutze zuerst die sichere Wiederherstellung, damit keine zweite Cloud-Installation entsteht.", "The local installation data is corrupted. Use secure recovery first so a second cloud installation cannot be created.")
		return
	}
	if a.st.Meta != nil {
		a.st.mu.Unlock()
		writeAPIError(w, r, 409, "Es ist bereits eine Installation eingerichtet. Nutze den Wartungsbereich statt eine zweite Cloud-Installation anzulegen.", "An installation already exists. Use the maintenance area instead of creating a second cloud installation.")
		return
	}
	if a.st.InstallStatus == "running" {
		a.st.mu.Unlock()
		writeAPIError(w, r, 409, "Installation läuft bereits", "Installation is already running")
		return
	}
	a.st.InstallStatus = "running"
	a.st.InstallPhase = preparing
	a.st.InstallProgress = 1
	a.st.InstallError = ""
	a.st.Verification = nil
	a.st.mu.Unlock()
	go func() {
		err := a.provision()
		errText := a.errorText(err)
		a.st.mu.Lock()
		defer a.st.mu.Unlock()
		if err != nil {
			a.st.InstallStatus = "failed"
			a.st.InstallPhase = stopped
			a.st.InstallError = errText
			a.st.Logs = append(a.st.Logs, time.Now().Format("15:04:05")+"  ERROR: "+errText)
		} else {
			a.st.InstallStatus = "completed"
			a.st.InstallPhase = done
			a.st.InstallProgress = 100
		}
	}()
	writeJSON(w, 202, map[string]bool{"started": true})
}
func (a *App) handleDashboardOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	a.st.mu.RLock()
	dashboardURL := ""
	if a.st.Meta != nil {
		dashboardURL = a.st.Meta.DashboardURL
	}
	a.st.mu.RUnlock()
	if dashboardURL == "" {
		writeAPIError(w, r, 400, "Noch kein Dashboard vorhanden", "No dashboard exists yet")
		return
	}
	if err := openBrowser(dashboardURL); err != nil {
		writeJSON(w, 400, map[string]string{"error": a.errorText(err)})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) handleMaintenanceCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	writeJSON(w, 200, a.maintenanceCheck())
}
func (a *App) runMaintenance(action string, fn func() error) bool {
	a.st.mu.Lock()
	if a.st.MaintenanceStatus == "running" {
		a.st.mu.Unlock()
		return false
	}
	a.st.MaintenanceStatus = "running"
	a.st.MaintenanceAction = action
	a.st.MaintenanceError = ""
	a.st.mu.Unlock()
	go func() {
		err := fn()
		errText := a.errorText(err)
		a.st.mu.Lock()
		defer a.st.mu.Unlock()
		if err != nil {
			a.st.MaintenanceStatus = "failed"
			a.st.MaintenanceError = errText
		} else {
			a.st.MaintenanceStatus = "completed"
		}
	}()
	return true
}
func (a *App) handleMaintenanceUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	if !a.runMaintenance("update", a.maintenanceUpdate) {
		writeAPIError(w, r, 409, "Es läuft bereits eine Wartungsaktion", "A maintenance action is already running")
		return
	}
	writeJSON(w, 202, map[string]bool{"started": true})
}
func (a *App) handleMaintenanceUninstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	var in struct {
		Confirm string `json:"confirm"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.Confirm != "DELETE" {
		writeAPIError(w, r, 400, "Zur Deinstallation DELETE eingeben", "Type DELETE to uninstall")
		return
	}
	if !a.runMaintenance("uninstall", func() error { return a.maintenanceUninstall(in.Confirm) }) {
		writeAPIError(w, r, 409, "Es läuft bereits eine Wartungsaktion", "A maintenance action is already running")
		return
	}
	writeJSON(w, 202, map[string]bool{"started": true})
}
func (a *App) handleMaintenanceCredentials(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	var in struct {
		IServ      bool `json:"iserv"`
		Remarkable bool `json:"remarkable"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || (!in.IServ && !in.Remarkable) {
		writeAPIError(w, r, 400, "Wähle mindestens eine Verbindung zum Aktualisieren aus", "Select at least one connection to update")
		return
	}
	if in.IServ {
		a.st.mu.RLock()
		ok := a.st.IServConnected && a.st.Secrets.IServURL != "" && a.st.Secrets.IServUser != "" && a.st.Secrets.IServPassword != ""
		a.st.mu.RUnlock()
		if !ok {
			writeAPIError(w, r, 400, "Teste zuerst die neuen IServ-Zugangsdaten", "Test the new IServ credentials first")
			return
		}
	}
	if in.Remarkable {
		a.st.mu.RLock()
		ok := a.st.RemarkableConnected && a.st.Secrets.DeviceToken != "" && a.st.Secrets.UserToken != ""
		a.st.mu.RUnlock()
		if !ok {
			writeAPIError(w, r, 400, "Verbinde reMarkable zuerst mit einem neuen Einmalcode", "Connect reMarkable with a new one-time code first")
			return
		}
	}
	if !a.runMaintenance("credentials", func() error { return a.maintenanceUpdateCredentials(in.IServ, in.Remarkable) }) {
		writeAPIError(w, r, 409, "Es läuft bereits eine Wartungsaktion", "A maintenance action is already running")
		return
	}
	writeJSON(w, 202, map[string]bool{"started": true})
}

func (a *App) handleMaintenancePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.Password) < 16 || len(in.Password) > 256 || strings.ContainsAny(in.Password, "\x00\r\n") {
		writeAPIError(w, r, 400, "Das neue Dashboard-Passwort muss mindestens 16 Zeichen haben", "The new dashboard password must be at least 16 characters")
		return
	}
	password := in.Password
	if !a.runMaintenance("password", func() error { return a.maintenanceResetDashboardPassword(password) }) {
		writeAPIError(w, r, 409, "Es läuft bereits eine Wartungsaktion", "A maintenance action is already running")
		return
	}
	writeJSON(w, 202, map[string]bool{"started": true})
}

func (a *App) handleMaintenanceVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.Password) < 1 {
		writeAPIError(w, r, 400, "Dashboard-Passwort fehlt", "Dashboard password is required")
		return
	}
	password := in.Password
	if !a.runMaintenance("verify", func() error {
		result, err := a.maintenanceDryRun(password)
		if result != nil {
			a.st.mu.Lock()
			a.st.Verification = result
			a.st.mu.Unlock()
		}
		return err
	}) {
		writeAPIError(w, r, 409, "Es läuft bereits eine Wartungsaktion", "A maintenance action is already running")
		return
	}
	writeJSON(w, 202, map[string]bool{"started": true})
}

func (a *App) handleMaintenanceDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	path, err := a.createDiagnostics()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": a.errorText(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

func (a *App) handleMaintenanceDiagnosticsOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if err := a.openDiagnosticsFolder(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": a.errorText(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"opened": true})
}

func (a *App) handleAppUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	d, err := checkLatestRelease()
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": a.errorText(err)})
		return
	}
	writeJSON(w, 200, d)
}
func (a *App) handleAppUpdateDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	p, err := downloadLatestPackage()
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": a.errorText(err)})
		return
	}
	writeJSON(w, 200, map[string]string{"path": p})
}
func (a *App) handleQuit(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
	a.quitOnce.Do(func() { close(a.quit) })
}

var _ = context.Background
var _ = filepath.Join
