package main

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func revealForTest(value string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(rcloneCryptKey)
	if err != nil {
		return "", err
	}
	out := append([]byte(nil), raw[aes.BlockSize:]...)
	cipher.NewCTR(block, raw[:aes.BlockSize]).XORKeyStream(out, out)
	return string(out), nil
}

func TestRcloneObscureRoundTrip(t *testing.T) {
	got, err := obscureRclonePassword("päss word! 123")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := revealForTest(got)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "päss word! 123" {
		t.Fatalf("roundtrip = %q", plain)
	}
}

func TestRandomUUIDShape(t *testing.T) {
	id, err := randomUUID()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
		t.Fatalf("bad uuid: %s", id)
	}
}

func TestExtractWorkersURL(t *testing.T) {
	out := "Uploaded\nhttps://iserv-rm-control-abcd.example.workers.dev\n"
	if got := extractWorkersURL(out); got != "https://iserv-rm-control-abcd.example.workers.dev" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractFlyToken(t *testing.T) {
	out := "warning\nFlyV1 fm2_permission_token,fm2_discharge_token\n"
	if got := extractFlyToken(out); got != "FlyV1 fm2_permission_token,fm2_discharge_token" {
		t.Fatalf("got %q", got)
	}
	if got := extractFlyToken(out + "FlyV1 fm2_second_token\n"); got != "" {
		t.Fatalf("ambiguous output returned %q", got)
	}
	for _, invalid := range []string{
		"FlyV1 fm2_valid_token,short\n",
		"FlyV1 fm2_valid_token,\n",
		"Bearer FlyV1 fm2_valid_token\n",
	} {
		if got := extractFlyToken(invalid); got != "" {
			t.Fatalf("invalid output returned %q", got)
		}
	}
}

func TestValidConfig(t *testing.T) {
	cfg := defaultConfig()
	cfg.AdminPassword = "0123456789abcdef"
	cfg.Sources = []Source{{Name: "Deutsch", IServPath: "Groups/deutsch", RemarkableDir: "/Schule/01 | Deutsch/IServ"}}
	if err := validConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Sources[0].RemarkableDir = "Schule/Deutsch"
	if err := validConfig(cfg); err == nil {
		t.Fatal("expected invalid target")
	}
}

func TestDecodeWebDAVDirectoriesSkipsSelfWithBasePath(t *testing.T) {
	xmlBody := `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">
<d:response><d:href>/webdav/Groups/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>
<d:response><d:href>/webdav/Groups/Mathe/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>
<d:response><d:href>/webdav/Groups/Deutsch/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>
<d:response><d:href>Fragen%3F%20%23%25/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>
<d:response><d:href>/webdav/Other/Unrelated/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>
<d:response><d:href>/webdav/Groups/Mathe/Deep/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>
</d:multistatus>`
	dirs, err := decodeWebDAVDirectories("https://schule.example/webdav/Groups", "Groups", strings.NewReader(xmlBody))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Groups/Deutsch", "Groups/Fragen? #%", "Groups/Mathe"}
	if !reflect.DeepEqual(dirs, want) {
		t.Fatalf("dirs=%v want=%v", dirs, want)
	}
}

func TestIServSecretsEncodeUsernameForFlyImport(t *testing.T) {
	sec := secrets{IServURL: "https://school.example/webdav", IServUser: `"test#name"`, IServPassword: "fixture-password"}
	lines, err := iservFlySecretLines(sec, true)
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "ISERV_WEBDAV_USER_B64="
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			encoded := strings.TrimPrefix(line, prefix)
			if encoded != "InRlc3QjbmFtZSI=" || strings.Contains(redactCommandOutput(line), encoded) {
				t.Fatal("username encoding or redaction is incorrect")
			}
			return
		}
	}
	t.Fatal("encoded username secret was not included")
}

func TestWebDAVPermissionErrorIsFriendly(t *testing.T) {
	raw := `<?xml version="1.0"?><d:error><s:exception>IServ\\Library\\Sudo\\Exception\\SudoRuntimeException</s:exception><s:message>scandir(): (errno 13): Permission denied in file /usr/lib/iserv/sudo_php on line 145</s:message></d:error>`
	err := webDAVStatusError(500, raw, "Groups/Locked")
	msg := err.Error()
	if !strings.Contains(msg, "nicht zugänglich") {
		t.Fatalf("unexpected message: %s", msg)
	}
	if strings.Contains(msg, "/usr/lib/iserv") || strings.Contains(msg, "SudoRuntimeException") {
		t.Fatalf("raw server internals leaked: %s", msg)
	}
}

func TestRegisterRemarkableUsesCurrentBrowserAuthFlow(t *testing.T) {
	var deviceSeen, userSeen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token/json/2/device/new":
			deviceSeen = true
			if r.Method != http.MethodPost {
				t.Fatalf("device method = %s", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer" {
				t.Fatalf("device authorization = %q", got)
			}
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["code"] != "abcdefgh" {
				t.Fatalf("code = %q", payload["code"])
			}
			if payload["deviceDesc"] != "browser-chrome" {
				t.Fatalf("deviceDesc = %q", payload["deviceDesc"])
			}
			if !regexp.MustCompile(`^[0-9a-f-]{36}$`).MatchString(payload["deviceID"]) {
				t.Fatalf("deviceID = %q", payload["deviceID"])
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("device-token"))
		case "/token/json/2/user/new":
			userSeen = true
			if r.Method != http.MethodPost {
				t.Fatalf("user method = %s", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer device-token" {
				t.Fatalf("user authorization = %q", got)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("user-token"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	device, user, err := registerRemarkableAt("abcdefgh", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if device != "device-token" || user != "user-token" {
		t.Fatalf("tokens = %q / %q", device, user)
	}
	if !deviceSeen || !userSeen {
		t.Fatalf("deviceSeen=%v userSeen=%v", deviceSeen, userSeen)
	}
}

func TestRegisterRemarkableRejectsWrongCodeLength(t *testing.T) {
	_, _, err := registerRemarkableAt("abcdefg", "http://127.0.0.1:1", &http.Client{})
	if err == nil || !strings.Contains(err.Error(), "8 Zeichen") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisterRemarkableHidesServerBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte("internal secret implementation detail"))
	}))
	defer server.Close()
	_, _, err := registerRemarkableAt("abcdefgh", server.URL, server.Client())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "internal secret") {
		t.Fatalf("server internals leaked: %s", err)
	}
}

func TestEmbeddedFlyImageUsesCurrentRemarkableAuthHost(t *testing.T) {
	raw, err := embedded.ReadFile("payload/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "webapp-prod.cloud.remarkable.engineering/token/json/2/") {
		t.Fatal("current reMarkable auth host missing from embedded Dockerfile")
	}
	if !strings.Contains(text, "sed -i 's#https://my.remarkable.com/token/json/2/#https://webapp-prod.cloud.remarkable.engineering/token/json/2/#g'") {
		t.Fatal("embedded Dockerfile does not explicitly patch the legacy rmapi auth host")
	}
	for _, marker := range []string{
		"golang:1.26.11-bookworm@sha256:dc40a13b61304647ed7e5d581d246b86413dfc29d63c6d018310feb93cd98fc0",
		"python:3.13-slim-bookworm@sha256:a5a7017efc5969e00ae2b91c8eb0631049a569652aa4ffc4501769049ae45c72",
		"COPY rmapi_safe_api.go.txt /tmp/rmapi_safe_api.go",
		"cp /tmp/rmapi_safe_api.go api/api.go",
		"go mod edit -droprequire=github.com/golang-jwt/jwt",
		"go get google.golang.org/grpc@v1.83.1 golang.org/x/net@v0.60.0",
		"go mod tidy",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("embedded Dockerfile is missing rmapi hardening %q", marker)
		}
	}

	safeRaw, err := embedded.ReadFile("payload/rmapi_safe_api.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	safeSource := string(safeRaw)
	for _, marker := range []string{
		"const maxUserTokenBytes = 64 << 10",
		"base64.RawURLEncoding.DecodeString",
		"len(parts) != 3",
	} {
		if !strings.Contains(safeSource, marker) {
			t.Errorf("embedded bounded token parser is missing %q", marker)
		}
	}
	for _, forbidden := range []string{"github.com/golang-jwt/jwt", "ParseUnverified", "StandardClaims"} {
		if strings.Contains(safeSource, forbidden) {
			t.Errorf("embedded bounded token parser retains %q", forbidden)
		}
	}
}

func TestEmbeddedPayloadExcludesGeneratedWorkerDependencies(t *testing.T) {
	if _, err := embedded.ReadDir("payload/worker/node_modules"); !os.IsNotExist(err) {
		t.Fatalf("embedded payload must not include generated node_modules, err=%v", err)
	}
}

func TestExtractPayloadReplacesStaleSoftwareAndPreservesInstallationRuntime(t *testing.T) {
	work := t.TempDir()
	runtimeDir := filepath.Join(work, "runtime")
	if err := os.MkdirAll(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	syncPath := filepath.Join(runtimeDir, "sync_job.py")
	if err := os.WriteFile(syncPath, []byte("stale RC payload"), 0600); err != nil {
		t.Fatal(err)
	}
	flyConfig := filepath.Join(runtimeDir, "fly.toml")
	const existingConfig = "app = \"iserv-rm-existing\"\n"
	if err := os.WriteFile(flyConfig, []byte(existingConfig), 0600); err != nil {
		t.Fatal(err)
	}

	if err := extractPayload(work); err != nil {
		t.Fatal(err)
	}
	wantSync, err := embedded.ReadFile("payload/sync_job.py")
	if err != nil {
		t.Fatal(err)
	}
	gotSync, err := os.ReadFile(syncPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotSync) != string(wantSync) || strings.Contains(string(gotSync), "stale RC payload") {
		t.Fatal("new setup did not replace the stale embedded sync payload")
	}
	gotConfig, err := os.ReadFile(flyConfig)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotConfig) != existingConfig {
		t.Fatalf("existing installation runtime was changed: %q", gotConfig)
	}
}

func TestSetupUIKeepsAccessibleFieldAndStepNames(t *testing.T) {
	raw, err := embedded.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	for _, id := range []string{
		"repairIServURL", "repairIServUser", "repairIServPassword", "repairRMCode",
		"maintenanceNewPassword", "maintenanceVerifyPassword", "uninstallConfirm",
		"recoveryDashboardURL", "recoveryPassword", "iservURL", "iservUser",
		"iservPassword", "rmCode", "region", "interval", "lookback", "maxFile",
		"adminPassword", "flyCode",
	} {
		if !strings.Contains(page, `for="`+id+`"`) {
			t.Errorf("setup field %s has no associated label", id)
		}
	}
	for _, marker := range []string{
		`aria-label="${n}. ${esc(label)}"`,
		`<label for="${startID}">`,
		`<label for="${endID}">`,
		`for="mapping-name-${i}"`,
	} {
		if !strings.Contains(page, marker) {
			t.Errorf("setup UI is missing accessible dynamic marker %q", marker)
		}
	}
	if !strings.Contains(firstRealSyncUIJS, `for="firstRealPassword"`) {
		t.Error("first-real password field has no associated label")
	}
}

func TestSetupDiagnosticsCopyDescribesBestEffortRedaction(t *testing.T) {
	raw, err := embedded.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	if !strings.Contains(page, "bestmöglich entfernt") || !strings.Contains(page, "best-effort redaction") {
		t.Fatal("diagnostics UI does not accurately describe best-effort redaction")
	}
}

func TestParseRemarkableFolderFindOutput(t *testing.T) {
	out := strings.Join([]string{
		"//",
		"/Schule/",
		"/Schule/01 | Deutsch/",
		"/Schule/01 | Deutsch/Arbeitsblatt.pdf",
		"/trash/",
		"/trash/Alt/",
		"some log line",
	}, "\n")
	got := parseRemarkableFolderFindOutput(out)
	want := []string{"/", "/Schule", "/Schule/01 | Deutsch"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("folders=%v want=%v", got, want)
	}
}

func TestRemarkableChildren(t *testing.T) {
	folders := []string{"/", "/Schule", "/Schule/Deutsch", "/Schule/Mathe", "/Archiv", "/Schule/Deutsch/IServ"}
	got := remarkableChildren(folders, "/Schule")
	want := []string{"/Schule/Deutsch", "/Schule/Mathe"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("children=%v want=%v", got, want)
	}
}

func TestSafeJoinRejectsTraversalAndAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	bad := []string{"../escape", "../../escape", "/absolute", "\\absolute", "C:/absolute", "C:relative", "//server/share", "folder\\..\\escape", "file:stream", ".."}
	for _, name := range bad {
		if _, err := safeJoin(root, name); err == nil {
			t.Fatalf("safeJoin accepted %q", name)
		}
	}
	if got, err := safeJoin(root, "nested/tool"); err != nil || !strings.HasPrefix(got, root) {
		t.Fatalf("safe path failed: %q %v", got, err)
	}
}

func TestSemverComparisonAndReleasePackageNames(t *testing.T) {
	if !semverGreater("0.4.1", "0.4.0") || !semverGreater("1.0.0", "0.99.9") || semverGreater("0.4.0", "0.4.0") {
		t.Fatal("semver comparison is incorrect")
	}
	cases := map[[2]string]string{
		{"darwin", "arm64"}:  "IServ-reMarkable-macos.dmg",
		{"darwin", "amd64"}:  "IServ-reMarkable-macos.dmg",
		{"windows", "amd64"}: "IServ-reMarkable-windows-amd64-Setup.exe",
		{"windows", "arm64"}: "IServ-reMarkable-windows-arm64-Setup.exe",
		{"linux", "amd64"}:   "IServ-reMarkable-linux-amd64.tar.gz",
		{"linux", "arm64"}:   "IServ-reMarkable-linux-arm64.tar.gz",
	}
	for platform, want := range cases {
		if got := releasePackageName(platform[0], platform[1]); got != want {
			t.Fatalf("releasePackageName(%q,%q)=%q want %q", platform[0], platform[1], got, want)
		}
	}
}

func TestValidConfigRejectsDuplicateNamesAndUnsafePaths(t *testing.T) {
	base := defaultConfig()
	base.AdminPassword = "0123456789abcdef"
	base.Sources = []Source{
		{Name: "Chemie", IServPath: "Groups/Chemie", RemarkableDir: "/Schule/Chemie/IServ"},
		{Name: "chemie", IServPath: "Groups/Chemie2", RemarkableDir: "/Schule/Chemie2/IServ"},
	}
	if err := validConfig(base); err == nil {
		t.Fatal("duplicate mapping names were accepted")
	}
	base.Sources = []Source{{Name: "Chemie", IServPath: "Groups/../Secret", RemarkableDir: "/Schule/Chemie/IServ"}}
	if err := validConfig(base); err == nil {
		t.Fatal("unsafe IServ path was accepted")
	}
	base.Sources = []Source{{Name: "Chemie", IServPath: "Groups/Chemie", RemarkableDir: "/Schule//Chemie"}}
	if err := validConfig(base); err == nil {
		t.Fatal("unsafe reMarkable path was accepted")
	}
}

func TestSetupHTMLContainsLanguageMaintenanceAndQuitControls(t *testing.T) {
	raw, err := embedded.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	for _, id := range []string{
		`id="langDE"`, `id="langEN"`, `id="quitSetup"`, `id="maintenanceCard"`,
		`id="maintenanceActivationSlot"`,
		`id="maintenanceOpen"`, `id="maintenanceCheck"`, `id="maintenanceFlyLogin"`, `id="maintenanceCFLogin"`, `id="maintenanceFlyCodePanel"`, `id="maintenanceFlyCode"`, `id="maintenanceFlyCodeSubmit"`, `id="maintenanceFlyCodeStatus"`, `id="maintenanceConnectionStatus"`, `id="maintenanceUpdate"`,
		`id="appUpdateCheck"`, `id="maintenanceUninstall"`, `id="maintenanceStatus"`,
		`id="repairIServTest"`, `id="repairIServApply"`, `id="repairRMTest"`, `id="repairRMApply"`,
		`id="maintenanceNewPassword"`, `id="maintenancePasswordApply"`, `id="maintenanceVerify"`,
		`id="maintenanceDiagnostics"`, `id="maintenanceDiagnosticsOpen"`, `id="maintenanceDiagnosticsStatus"`,
		`id="recoveryBox"`, `id="recoveryDashboardURL"`, `id="recoveryPassword"`, `id="recoveryConnect"`, `id="recoveryStatus"`,
		`id="flyCodePanel"`, `id="flyCode"`, `id="flyCodeSubmit"`, `id="flyCodeStatus"`,
	} {
		if strings.Count(html, id) != 1 {
			t.Fatalf("expected exactly one %s", id)
		}
	}
	if !strings.Contains(html, "Setup Version __APP_VERSION__") {
		t.Fatal("setup version badge missing")
	}
	if !strings.Contains(html, "lädt aber keine Datei hoch und schaltet die Automatik nicht frei") || !strings.Contains(html, "uploads no files and does not unlock automation") {
		t.Fatal("maintenance dry-run copy does not distinguish it from activation")
	}
}

func TestManagedToolAssetsCoverSupportedPlatforms(t *testing.T) {
	flyCases := []struct{ os, arch, osName, assetArch, ext string }{
		{"darwin", "amd64", "macOS", "x86_64", "tar.gz"},
		{"darwin", "arm64", "macOS", "arm64", "tar.gz"},
		{"linux", "amd64", "Linux", "x86_64", "tar.gz"},
		{"linux", "arm64", "Linux", "arm64", "tar.gz"},
		{"windows", "amd64", "Windows", "x86_64", "zip"},
		{"windows", "arm64", "Windows", "arm64", "zip"},
	}
	// The actual helpers use runtime.GOOS/GOARCH. This table protects the public
	// package naming contract used by the release workflow and documents every
	// platform the product promises to ship.
	for _, tc := range flyCases {
		asset := fmt.Sprintf("flyctl_%s_%s_%s.%s", managedFlyVersion, tc.osName, tc.assetArch, tc.ext)
		if !strings.HasPrefix(asset, "flyctl_"+managedFlyVersion+"_") {
			t.Fatalf("bad Fly asset contract for %s/%s: %s", tc.os, tc.arch, asset)
		}
	}
}

func TestMaintenanceVerifyRequiresDashboardPassword(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	req := httptest.NewRequest(http.MethodPost, "/api/maintenance/verify", strings.NewReader(`{}`))
	req.Header.Set("X-Setup-Token", "test-token")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	app.routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(strings.ToLower(rr.Body.String()), "token") {
		t.Fatalf("unexpected secret-related output: %s", rr.Body.String())
	}
}

func TestDiagnosticRedactionRemovesSensitiveValues(t *testing.T) {
	input := "Authorization: Bearer abc.def.ghi password=hunter2 token=FlyV1-secret https://webdav.school.example/user john@example.com"
	got := redactDiagnosticText(input)
	for _, forbidden := range []string{"abc.def.ghi", "hunter2", "FlyV1-secret", "webdav.school.example", "john@example.com"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("diagnostic text leaked %q: %s", forbidden, got)
		}
	}
	if !strings.Contains(got, "[redacted]") || !strings.Contains(got, "[url redacted]") || !strings.Contains(got, "[email redacted]") {
		t.Fatalf("expected redaction markers, got: %s", got)
	}
}

func TestDiagnosticsEndpointCreatesPrivacyFriendlyReport(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	app.st.mu.Lock()
	app.st.Logs = []string{"19:00:00  password=supersecret https://example.invalid jane@example.com"}
	app.st.mu.Unlock()
	req := httptest.NewRequest(http.MethodPost, "/api/maintenance/diagnostics", nil)
	req.Header.Set("X-Setup-Token", "test-token")
	rr := httptest.NewRecorder()
	app.routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	path := body["path"]
	if path == "" {
		t.Fatal("diagnostic path missing")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"supersecret", "example.invalid", "jane@example.com"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("diagnostic report leaked %q: %s", forbidden, text)
		}
	}
	if strings.Contains(text, "admin_password") || strings.Contains(text, "device_token") || strings.Contains(text, "iserv_password") {
		t.Fatalf("diagnostic schema contains secret fields: %s", text)
	}
}

func TestNormalizeDashboardURLRequiresSafeRootHTTPSURL(t *testing.T) {
	good, err := normalizeDashboardURL(" https://example.workers.dev/ ")
	if err != nil || good != "https://example.workers.dev" {
		t.Fatalf("good URL failed: %q %v", good, err)
	}
	for _, bad := range []string{
		"http://example.workers.dev",
		"https://user:pass@example.workers.dev",
		"https://example.workers.dev/path",
		"https://example.workers.dev/?x=1",
		"not a url",
	} {
		if _, err := normalizeDashboardURL(bad); err == nil {
			t.Fatalf("accepted unsafe dashboard URL %q", bad)
		}
	}
}

func TestEnsureWorkerRecoveryMetadataMigratesOldConfigIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrangler.jsonc")
	old := `{
  "name": "iserv-rm-control-old",
  "vars": {
    "FLY_APP": "iserv-rm-old",
    "FLY_MACHINE_ID": "machine-1"
  },
  "triggers": {"crons":["* * * * *"]}
}`
	if err := os.WriteFile(path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	meta := &installMeta{WorkerName: "iserv-rm-control-old", Region: "fra", MachineID: "1234567890abcd", KVNamespaceID: "0123456789abcdef0123456789abcdef", FlyTokenUntil: "2031-08-18T00:00:00Z"}
	if err := ensureWorkerRecoveryMetadata(path, meta); err != nil {
		t.Fatal(err)
	}
	if err := ensureWorkerRecoveryMetadata(path, meta); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("migrated config is invalid JSON: %v\n%s", err, raw)
	}
	vars := parsed["vars"].(map[string]any)
	if vars["INSTALL_WORKER_NAME"] != meta.WorkerName || vars["INSTALL_REGION"] != "fra" || vars["INSTALL_VERSION"] != appVersion || vars["INSTALL_KV_ID"] != meta.KVNamespaceID || vars["INSTALL_FLY_TOKEN_UNTIL"] != meta.FlyTokenUntil {
		t.Fatalf("recovery vars missing: %#v", vars)
	}
	if strings.Count(string(raw), "INSTALL_WORKER_NAME") != 1 || strings.Count(string(raw), "INSTALL_REGION") != 1 || strings.Count(string(raw), "INSTALL_VERSION") != 1 || strings.Count(string(raw), "INSTALL_KV_ID") != 1 || strings.Count(string(raw), "INSTALL_FLY_TOKEN_UNTIL") != 1 {
		t.Fatalf("migration was not idempotent: %s", raw)
	}
}

func TestSecureHTTPClientRejectsDowngradeRedirect(t *testing.T) {
	client := secureHTTPClient(time.Second)
	req := httptest.NewRequest(http.MethodGet, "http://example.test/file", nil)
	if err := client.CheckRedirect(req, []*http.Request{{}}); err == nil {
		t.Fatal("HTTPS downgrade redirect was accepted")
	}
}

func TestRebuildRecoveredRuntimeCreatesCompleteLocalMaintenanceConfig(t *testing.T) {
	work := t.TempDir()
	if err := extractPayload(work); err != nil {
		t.Fatal(err)
	}
	app := NewApp(work, "test-token")
	meta := &installMeta{
		AppName: "iserv-rm-abcd1234", WorkerName: "iserv-rm-control-abcd1234",
		DashboardURL: "https://example.workers.dev", Region: "fra",
		MachineID: "1234567890abcd", KVNamespaceID: "0123456789abcdef0123456789abcdef",
		FlyTokenUntil: "2031-08-18T00:00:00Z", Version: appVersion,
	}
	if err := app.rebuildRecoveredRuntime(meta); err != nil {
		t.Fatal(err)
	}
	flyRaw, err := os.ReadFile(filepath.Join(work, "runtime", "fly.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(flyRaw), meta.AppName) || strings.Contains(string(flyRaw), "__APP_NAME__") {
		t.Fatalf("fly config not rebuilt: %s", flyRaw)
	}
	wrRaw, err := os.ReadFile(filepath.Join(work, "runtime", "worker", "wrangler.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(wrRaw)
	for _, want := range []string{meta.WorkerName, meta.AppName, meta.MachineID, meta.KVNamespaceID, meta.FlyTokenUntil, `"binding": "CONFIG"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in recovered worker config: %s", want, text)
		}
	}
	for _, placeholder := range []string{"__WORKER_NAME__", "__APP_NAME__", "__MACHINE_ID__", "__KV_ID__", "__FLY_TOKEN_UNTIL__"} {
		if strings.Contains(text, placeholder) {
			t.Fatalf("placeholder %s remained in recovered config", placeholder)
		}
	}
}
