package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSemverPrereleasePrecedence(t *testing.T) {
	cases := []struct {
		newer   string
		current string
	}{
		{"0.9.0-rc22", "0.9.0-rc21"},
		{"0.9.0-rc20", "0.9.0-rc19"},
		{"0.9.0-rc18", "0.9.0-rc17"},
		{"0.9.0-rc17", "0.9.0-rc16"},
		{"0.9.0-rc16", "0.9.0-rc15"},
		{"0.9.0-rc15", "0.9.0-rc14"},
		{"0.9.0-rc14", "0.9.0-rc13"},
		{"0.9.0-rc13", "0.9.0-rc12"},
		{"0.9.0-rc12", "0.9.0-rc11"},
		{"0.9.0-rc11", "0.9.0-rc10"},
		{"0.9.0-rc10", "0.9.0-rc9"},
		{"1.0.0-alpha10", "1.0.0-alpha9"},
		{"0.9.0-rc8", "0.9.0-rc7"},
		{"0.9.0-rc9", "0.9.0-rc8"},
		{"0.9.0-rc7", "0.9.0-rc6"},
		{"0.9.0-rc6", "0.9.0-rc5"},
		{"0.9.0-rc5", "0.9.0-rc4"},
		{"0.9.0-rc4", "0.9.0-rc3"},
		{"0.9.0", "0.9.0-rc1"},
		{"0.9.0-rc3", "0.9.0-rc2"},
		{"0.9.0-rc2", "0.9.0-rc1"},
		{"1.0.0-alpha.2", "1.0.0-alpha.1"},
		{"1.0.0-beta", "1.0.0-alpha.99"},
	}
	for _, tc := range cases {
		if !semverGreater(tc.newer, tc.current) {
			t.Fatalf("expected %s > %s", tc.newer, tc.current)
		}
	}
	for _, tc := range [][2]string{{"0.9.0-rc1", "0.9.0"}, {"0.9.0", "0.9.0"}, {"garbage", "0.9.0"}, {"1.02.0", "1.1.0"}} {
		if semverGreater(tc[0], tc[1]) {
			t.Fatalf("did not expect %s > %s", tc[0], tc[1])
		}
	}
}

func TestValidConfigAllowsRemarkableRoot(t *testing.T) {
	cfg := defaultConfig()
	cfg.AdminPassword = "0123456789abcdef"
	cfg.Sources = []Source{{Name: "Root", IServPath: "Groups/Root", RemarkableDir: "/"}}
	if err := validConfig(cfg); err != nil {
		t.Fatalf("reMarkable root should be a valid target: %v", err)
	}
}

func TestInstallStartReturnsWithoutMutexDeadlock(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	req := httptest.NewRequest(http.MethodPost, "/api/install/start", nil)
	req.Header.Set("X-Setup-Token", "test-token")
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		app.routes().ServeHTTP(rr, req)
		close(done)
	}()
	select {
	case <-done:
		if rr.Code != http.StatusAccepted {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("install start deadlocked")
	}
}

func TestCorruptInstallationMetadataBlocksNewInstall(t *testing.T) {
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "installation.json"), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	app := NewApp(work, "test-token")
	if !app.localStateCorrupt {
		t.Fatal("corrupt local metadata was not detected")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/install/start", nil)
	req.Header.Set("X-Setup-Token", "test-token")
	rr := httptest.NewRecorder()
	app.routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMaintenanceConflictReturns409(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	app.st.mu.Lock()
	app.st.MaintenanceStatus = "running"
	app.st.mu.Unlock()
	req := httptest.NewRequest(http.MethodPost, "/api/maintenance/update", nil)
	req.Header.Set("X-Setup-Token", "test-token")
	rr := httptest.NewRecorder()
	app.routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCorruptProvisionJournalFailsClosed(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	if err := os.WriteFile(app.journalPath(), []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.loadJournal(); err == nil {
		t.Fatal("corrupt provision journal was treated as a fresh installation")
	}
}

func TestProvisionJournalRejectsInvalidCloudIdentifiers(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	bad := provisionJournal{AppName: "../bad", WorkerName: "iserv-rm-control-abcd1234", Steps: map[string]bool{}}
	raw := []byte(`{"app_name":"../bad","worker_name":"iserv-rm-control-abcd1234","steps":{}}`)
	if err := os.WriteFile(app.journalPath(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.loadJournal(); err == nil {
		t.Fatal("invalid journal identifiers were accepted")
	}
	if err := app.saveJournal(bad); err == nil {
		t.Fatal("invalid journal was persisted")
	}
}

func TestProvisionJournalRejectsMismatchedInstallationIdentifiers(t *testing.T) {
	journal := provisionJournal{
		AppName:    "iserv-rm-abcd1234",
		WorkerName: "iserv-rm-control-deadbeef",
		Steps:      map[string]bool{},
	}
	if err := validateProvisionJournal(journal); err == nil {
		t.Fatal("mismatched app and Worker identifiers were accepted")
	}
}

func TestFlyVolumeJSONCountsOnlyNamedVolume(t *testing.T) {
	raw := `[{"id":"v1","name":"sync_data"},{"id":"v2","name":"other"}]`
	got, err := countNamedFlyVolumes(raw, "sync_data")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("count=%d want=1", got)
	}
	if _, err := countNamedFlyVolumes(`{"unexpected":true}`, "sync_data"); err == nil {
		t.Fatal("unexpected Fly volume JSON shape was accepted")
	}
}

func TestSingleFlyMachineIDRequiresExactlyOneValidMachine(t *testing.T) {
	const machineID = "1234567890abcd"
	got, err := singleFlyMachineID(`[{"id":"` + machineID + `","state":"stopped"}]`)
	if err != nil || got != machineID {
		t.Fatalf("machine=%q err=%v", got, err)
	}
	for _, raw := range []string{
		`[]`,
		`[{"id":"bad"}]`,
		`[{"id":"1234567890abcd"},{"id":"18590d2a341e19"}]`,
		`{"id":"1234567890abcd"}`,
	} {
		if _, err := singleFlyMachineID(raw); err == nil {
			t.Fatalf("unsafe Machine list accepted: %s", raw)
		}
	}
}

func TestEnsureWorkerFlyTargetReplacesOnlyValidatedTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrangler.jsonc")
	raw := `{"name":"iserv-rm-control-abcdef12","vars":{"FLY_APP":"iserv-rm-old00000","FLY_MACHINE_ID":"1234567890abce","OTHER":"kept"}}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ensureWorkerFlyTarget(path, "iserv-rm-abcdef12", "1234567890abcd"); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	for _, want := range []string{`"FLY_APP": "iserv-rm-abcdef12"`, `"FLY_MACHINE_ID": "1234567890abcd"`, `"OTHER": "kept"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("updated target is incomplete: %s", text)
		}
	}
	if err := ensureWorkerFlyTarget(path, "../unsafe", "1234567890abcd"); err == nil {
		t.Fatal("unsafe app name was accepted")
	}
}

func TestReadKVNamespaceIDRejectsTemplatePlaceholder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrangler.jsonc")
	if err := os.WriteFile(path, []byte(`{"kv_namespaces":[{"binding":"CONFIG","id":"__KV_ID__"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readKVNamespaceID(path); err == nil {
		t.Fatal("KV template placeholder was accepted as a real namespace ID")
	}
}

func TestReadKVNamespaceIDRejectsUnboundID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrangler.jsonc")
	if err := os.WriteFile(path, []byte(`{"vars":{"id":"0123456789abcdef0123456789abcdef"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readKVNamespaceID(path); err == nil {
		t.Fatal("unbound ID was accepted as the CONFIG namespace")
	}
}

func TestWebDAVURLRejectsCredentialAndTraversalInputs(t *testing.T) {
	bad := []struct {
		base string
		path string
	}{
		{"http://schule.example/webdav", "Groups"},
		{"https://user:pass@schule.example/webdav", "Groups"},
		{"https://schule.example/webdav?token=x", "Groups"},
		{"https://schule.example/webdav#frag", "Groups"},
		{"https://schule.example/webdav", "Groups/../Secret"},
	}
	for _, tc := range bad {
		if got, err := webdavURL(tc.base, tc.path); err == nil {
			t.Fatalf("unsafe WebDAV input accepted: %q", got)
		}
	}
}

func TestRemarkableRejectsControlCharactersInToken(t *testing.T) {
	if _, err := readRemarkableToken(strings.NewReader("token\nsecond"), 1024, "Token"); err == nil {
		t.Fatal("newline in reMarkable token was accepted")
	}
}

func TestSafePathPartAllowsOrdinaryDoubleDotsButRejectsTraversal(t *testing.T) {
	if !safePathPart("Material..alt/Blatt.pdf") {
		t.Fatal("ordinary double dots in a path segment should be allowed")
	}
	for _, value := range []string{"A/../B", "A/./B", "A//B", "A\\B", "A\nB"} {
		if safePathPart(value) {
			t.Fatalf("unsafe path accepted: %q", value)
		}
	}
}

func TestValidConfigRejectsControlCharactersInPasswordAndName(t *testing.T) {
	cfg := defaultConfig()
	cfg.AdminPassword = "0123456789abcdef\n"
	cfg.Sources = []Source{{Name: "Safe", IServPath: "Groups/Safe", RemarkableDir: "/Safe"}}
	if err := validConfig(cfg); err == nil {
		t.Fatal("password with newline was accepted")
	}
	cfg.AdminPassword = "0123456789abcdef"
	cfg.Sources[0].Name = "Bad\nName"
	if err := validConfig(cfg); err == nil {
		t.Fatal("mapping name with newline was accepted")
	}
}

func TestSetupIndexUsesNonceCSP(t *testing.T) {
	app := NewApp(t.TempDir(), "0123456789abcdef0123456789abcdef")
	req := httptest.NewRequest(http.MethodGet, "/?token=0123456789abcdef0123456789abcdef", nil)
	rr := httptest.NewRecorder()
	app.routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	csp := rr.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'nonce-") || strings.Contains(csp, "script-src 'unsafe-inline'") {
		t.Fatalf("unexpected CSP: %s", csp)
	}
	if !strings.Contains(rr.Body.String(), `<script nonce="`) {
		t.Fatal("setup page script is missing a CSP nonce")
	}
}

func TestManagedToolDownloadsIntegration(t *testing.T) {
	if os.Getenv("ISERV_RM_TEST_MANAGED_TOOLS") != "1" {
		t.Skip("managed tool download integration test is opt-in")
	}
	workDir := filepath.Join(t.TempDir(), "Application Support", "IServ-reMarkable-Setup")
	if err := os.MkdirAll(workDir, 0700); err != nil {
		t.Fatalf("managed-tools work directory could not be prepared: %v", err)
	}
	manager := NewToolManager(workDir)
	node, npmCLI, err := manager.NodePaths(nil)
	if err != nil {
		t.Fatalf("managed Node.js preparation failed: %v", err)
	}
	if out, err := runWithTimeout("", node, []string{"--version"}, "", 30*time.Second); err != nil || !strings.Contains(out, managedNodeVersion) {
		t.Fatalf("managed Node.js did not execute correctly: out=%q err=%v", out, err)
	}
	if out, err := runWithTimeout("", node, []string{npmCLI, "--version"}, "", 30*time.Second); err != nil || strings.TrimSpace(out) == "" {
		t.Fatalf("managed npm did not execute correctly: out=%q err=%v", out, err)
	}
	if err := extractPayload(workDir); err != nil {
		t.Fatalf("setup payload extraction failed: %v", err)
	}
	app := NewApp(workDir, "test-token")
	app.tools = manager
	if err := app.ensureWorkerDeps(); err != nil {
		t.Fatalf("managed npm could not install Wrangler dependencies: %v", err)
	}
	wranglerCLI := filepath.Join(workDir, "runtime", "worker", "node_modules", "wrangler", "bin", "wrangler.js")
	if _, err := os.Stat(wranglerCLI); err != nil {
		t.Fatalf("Wrangler CLI was not installed: %v", err)
	}
	wranglerEnv, err := app.wranglerEnvironment(node)
	if err != nil {
		t.Fatalf("isolated Wrangler environment was not prepared: %v", err)
	}
	nodeWriteProbe := `const fs=require("node:fs"),path=require("node:path");const file=path.join(process.env.XDG_CONFIG_HOME,".wrangler","config","native-write-probe");fs.mkdirSync(path.dirname(file),{recursive:true});fs.writeFileSync(file,"ok",{mode:0o600});fs.rmSync(file);`
	if out, err := runWithTimeoutEnv("", node, []string{"-e", nodeWriteProbe}, "", 30*time.Second, wranglerEnv); err != nil {
		t.Fatalf("managed Node could not write Wrangler's isolated auth path: out=%q err=%v", out, err)
	}
	if out, err := runWithTimeoutEnv(filepath.Join(workDir, "runtime", "worker"), node, []string{wranglerCLI, "login", "--no-use-keyring", "--help"}, "", 30*time.Second, wranglerEnv); err != nil || !strings.Contains(out, "--use-keyring") {
		t.Fatalf("pinned Wrangler did not accept the explicit file-backed login option: out=%q err=%v", out, err)
	}
	workerDir := filepath.Join(workDir, "runtime", "worker")
	// Wrangler validates project configuration even for account commands. A fresh
	// installer still has unresolved deployment placeholders at this point.
	invalidConfig := []byte(`{"name":"__WORKER_NAME__","vars":{"FLY_APP":"__APP_NAME__"}}`)
	if err := os.WriteFile(filepath.Join(workerDir, "wrangler.jsonc"), invalidConfig, 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := app.runWrangler([]string{"whoami", "--json"}, "", 45*time.Second); strings.TrimSpace(out) != `{"loggedIn":false}` {
		t.Fatalf("account command did not report a clean logged-out identity: out=%q err=%v", out, err)
	}
	if raw, err := os.ReadFile(filepath.Join(workerDir, "wrangler.jsonc")); err != nil || string(raw) != string(invalidConfig) {
		t.Fatal("account command changed the deployment template")
	}
	bootstrapConfig := filepath.Join(workerDir, "wrangler.bootstrap.jsonc")
	if err := prepareWorkerBootstrap(workerDir, bootstrapConfig, "iserv-rm-control-native-test"); err != nil {
		t.Fatalf("bootstrap Worker config could not be prepared: %v", err)
	}
	deployArgs := append([]string{wranglerCLI}, wranglerDeployArgs(filepath.Base(bootstrapConfig))...)
	deployArgs = append(deployArgs, "--dry-run")
	if out, err := runWithTimeoutEnv(workerDir, node, deployArgs, "", 30*time.Second, wranglerEnv); err != nil || !strings.Contains(out, "No bindings found") {
		t.Fatalf("pinned Wrangler could not bundle the inert bootstrap Worker: out=%q err=%v", out, err)
	}
	if err := app.rebuildRecoveredRuntime(validLifecycleMeta()); err != nil {
		t.Fatalf("dashboard configuration could not be prepared: %v", err)
	}
	dashboardArgs := append([]string{wranglerCLI}, wranglerDeployArgs("")...)
	dashboardArgs = append(dashboardArgs, "--dry-run")
	if out, err := runWithTimeoutEnv(workerDir, node, dashboardArgs, "", 30*time.Second, wranglerEnv); err != nil || !strings.Contains(out, "KV Namespace") {
		t.Fatalf("pinned Wrangler could not bundle the guarded dashboard: out=%q err=%v", out, err)
	}
	secretArgs := append([]string{wranglerCLI}, wranglerSecretArgs("SETUP_NATIVE_TEST")...)
	secretArgs = append(secretArgs, "--help")
	if out, err := runWithTimeoutEnv(workerDir, node, secretArgs, "", 30*time.Second, wranglerEnv); err != nil || !strings.Contains(out, "Create or update a secret") {
		t.Fatalf("pinned Wrangler rejected the no-auto-create secret command: out=%q err=%v", out, err)
	}
	for _, check := range []struct {
		args   []string
		marker string
	}{
		{[]string{"kv", "namespace", "list", "--help"}, "Output a list of all KV namespaces"},
		{[]string{"kv", "namespace", "create", "--help"}, "Create a new namespace"},
	} {
		args := append([]string{wranglerCLI}, check.args...)
		if out, err := runWithTimeoutEnv(workerDir, node, args, "", 30*time.Second, wranglerEnv); err != nil || !strings.Contains(out, check.marker) {
			t.Fatalf("pinned Wrangler rejected KV command %v: out=%q err=%v", check.args, out, err)
		}
	}
	if identity, err := app.cloudflareIdentity(); err == nil {
		t.Fatalf("fresh isolated Wrangler unexpectedly inherited a Cloudflare identity: %q", identity)
	}
	fly, err := manager.FlyPath(nil)
	if err != nil {
		t.Fatalf("managed flyctl preparation failed: %v", err)
	}
	if out, err := runWithTimeoutEnv("", fly, []string{"version"}, "", 30*time.Second, map[string]string{"FLY_CONFIG_DIR": filepath.Join(workDir, "fly-test-config")}); err != nil || !strings.Contains(out, managedFlyVersion) {
		t.Fatalf("managed flyctl did not execute correctly: out=%q err=%v", out, err)
	}
	args, err := flyAppCreateArgs("iserv-rm-deadbeef", "test-organization")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := runWithTimeoutEnv("", fly, append(args, "--help"), "", 30*time.Second, map[string]string{"FLY_API_TOKEN": "", "FLY_CONFIG_DIR": filepath.Join(workDir, "fly-test-config")}); err != nil || !strings.Contains(out, "--org") {
		t.Fatalf("pinned flyctl rejected non-interactive app creation arguments: out=%q err=%v", out, err)
	}
	if check := probeHTTPS(managedFlyChecksumURL(), "en"); !check.OK {
		t.Fatalf("managed Fly.io preflight endpoint is not reachable: %s", check.Message)
	}
}

func TestCommandOutputRedactsCredentials(t *testing.T) {
	raw := "Authorization: Bearer abc123\nFlyV1 fm2_permission_secret,fm2_discharge_secret\ntoken=hello"
	redacted := redactCommandOutput(raw)
	for _, secret := range []string{"abc123", "fm2_permission_secret", "fm2_discharge_secret", "hello"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("credential leaked after redaction: %s", secret)
		}
	}
}
