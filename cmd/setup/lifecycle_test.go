package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func validLifecycleMeta() *installMeta {
	now := time.Now().UTC().Truncate(time.Second)
	return &installMeta{
		AppName:       "iserv-rm-test1234",
		WorkerName:    "iserv-rm-control-test1234",
		DashboardURL:  "https://iserv-rm-test1234.example.workers.dev",
		Region:        "fra",
		MachineID:     "abcdef123456",
		KVNamespaceID: "abcdefghijklmnop1234567890",
		FlyTokenUntil: now.Add(365 * 24 * time.Hour).Format(time.RFC3339),
		Version:       appVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func TestProvisionJournalRoundTripAndPrivatePermissions(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	journal := provisionJournal{
		AppName:       "iserv-rm-deadbeef",
		WorkerName:    "iserv-rm-control-deadbeef",
		MachineID:     "abcdef123456",
		DashboardURL:  "https://iserv-rm-deadbeef.example.workers.dev",
		KVNamespaceID: "abcdefghijklmnop1234567890",
		FlyTokenUntil: time.Now().UTC().Add(365 * 24 * time.Hour).Format(time.RFC3339),
		Steps:         map[string]bool{"templates": true, "fly_app": true},
	}
	if err := app.saveJournal(journal); err != nil {
		t.Fatal(err)
	}
	got, err := app.loadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, journal) {
		t.Fatalf("journal round trip mismatch: got=%+v want=%+v", got, journal)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(app.journalPath())
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Fatalf("journal permissions=%#o want=0600", perm)
		}
	}
}

func TestResumingProvisionDeploysOlderOrUnversionedFlyImage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		done    bool
		version string
		deploy  bool
	}{
		{"fresh", false, "", true},
		{"legacy journal", true, "", true},
		{"previous release", true, "0.9.0-rc21", true},
		{"interrupted deployment", false, appVersion, true},
		{"current image", true, appVersion, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := provisionJournal{Steps: map[string]bool{"fly_deploy": tc.done}, FlyImageVersion: tc.version}
			if j.needsFlyDeploy() != tc.deploy {
				t.Fatalf("expected deployment=%t", tc.deploy)
			}
		})
	}
}

func TestWorkerBootstrapIsPrivateInertAndResumable(t *testing.T) {
	work := t.TempDir()
	if err := extractPayload(work); err != nil {
		t.Fatal(err)
	}
	workerDir := filepath.Join(work, "runtime", "worker")
	target := filepath.Join(workerDir, "wrangler.bootstrap.jsonc")
	if err := prepareWorkerBootstrap(workerDir, target, "iserv-rm-control-test1234"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, marker := range []string{`"name": "iserv-rm-control-test1234"`, `"main": "src/bootstrap.js"`, `"workers_dev": false`, `"preview_urls": false`} {
		if !strings.Contains(text, marker) {
			t.Fatalf("bootstrap config missing %q", marker)
		}
	}
	for _, forbidden := range []string{"__WORKER_NAME__", `"vars"`, `"triggers"`, `"kv_namespaces"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("bootstrap config contains %q", forbidden)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("bootstrap permissions=%v", info.Mode().Perm())
		}
	}
	bootstrap, err := embedded.ReadFile("payload/worker/src/bootstrap.js")
	if err != nil {
		t.Fatal(err)
	}
	bootstrapText := string(bootstrap)
	if !strings.Contains(bootstrapText, "status: 503") || !strings.Contains(bootstrapText, `"Cache-Control": "no-store"`) || strings.Contains(bootstrapText, "scheduled") {
		t.Fatalf("bootstrap Worker is not inert and fail-closed: %s", bootstrapText)
	}
}

func TestWranglerSecretsNeverAutoCreateWorker(t *testing.T) {
	want := []string{"secret", "put", "ADMIN_PASSWORD", "--x-auto-create=false"}
	if got := wranglerSecretArgs("ADMIN_PASSWORD"); !reflect.DeepEqual(got, want) {
		t.Fatalf("secret args=%v want=%v", got, want)
	}
}

func TestWranglerDeploysNeverAutoconfigureOrCreateResources(t *testing.T) {
	wantDefault := []string{"deploy", "--autoconfig=false", "--x-auto-create=false"}
	if got := wranglerDeployArgs(""); !reflect.DeepEqual(got, wantDefault) {
		t.Fatalf("deploy args=%v want=%v", got, wantDefault)
	}
	wantBootstrap := append(append([]string(nil), wantDefault...), "--config", "wrangler.bootstrap.jsonc")
	if got := wranglerDeployArgs("wrangler.bootstrap.jsonc"); !reflect.DeepEqual(got, wantBootstrap) {
		t.Fatalf("bootstrap deploy args=%v want=%v", got, wantBootstrap)
	}
}

func TestCloudflareOnboardingURLIsStrictlyAllowlisted(t *testing.T) {
	account := "0123456789abcdef0123456789abcdef"
	want := "https://dash.cloudflare.com/" + account + "/workers/onboarding"
	if got := cloudflareWorkersOnboardingURL("Register here:\n" + want); got != want {
		t.Fatalf("onboarding URL=%q want=%q", got, want)
	}
	for _, value := range []string{
		"https://evil.example/" + account + "/workers/onboarding",
		"https://dash.cloudflare.com/bad/account/workers/onboarding",
		"https://dash.cloudflare.com/" + account + "/workers/onboarding/extra",
	} {
		if got := cloudflareWorkersOnboardingURL(value); got != "" {
			t.Fatalf("unsafe onboarding URL accepted: %q", got)
		}
	}
}

func TestCloudflareEmailVerificationErrorIsRecognizedWithoutBroadMatching(t *testing.T) {
	for _, value := range []string{
		"You need to verify your email address to use Workers.",
		"ERROR: VERIFY YOUR EMAIL before creating Workers resources",
	} {
		if !cloudflareEmailVerificationRequired(value) {
			t.Fatalf("verification requirement was not recognized: %q", value)
		}
	}
	for _, value := range []string{
		"verify your email address",
		"Workers deployment failed",
		"https://evil.example/verify-your-email-to-use-workers",
	} {
		if cloudflareEmailVerificationRequired(value) {
			t.Fatalf("unrelated error was misclassified: %q", value)
		}
	}
}

func TestCloudflareEmailVerificationMessageIsActionableAndHidesProviderOutput(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	providerOutput := `A request failed. You need to verify your email address to use Workers. secret-provider-detail`
	err := app.cloudflareWorkerPreparationError(fmt.Errorf("%s", providerOutput))
	message := err.Error()
	for _, marker := range []string{"Bestätigungs-E-Mail", "Spam-Ordner", "Installation fortsetzen", "Kein Code", "keine Neuinstallation"} {
		if !strings.Contains(message, marker) {
			t.Fatalf("actionable message missing %q: %s", marker, message)
		}
	}
	if strings.Contains(message, "secret-provider-detail") || strings.Contains(message, "A request failed") {
		t.Fatalf("provider output leaked into primary error: %s", message)
	}

	app.st.mu.Lock()
	app.st.Language = "en"
	app.st.mu.Unlock()
	message = app.cloudflareWorkerPreparationError(fmt.Errorf("%s", providerOutput)).Error()
	for _, marker := range []string{"verification email", "check spam", "Continue installation", "No code", "reinstallation"} {
		if !strings.Contains(message, marker) {
			t.Fatalf("actionable English message missing %q: %s", marker, message)
		}
	}
	if strings.Contains(message, "secret-provider-detail") {
		t.Fatalf("provider output leaked into English primary error: %s", message)
	}

	message = app.cloudflareWorkerPreparationError(fmt.Errorf("unclassified secret-provider-detail")).Error()
	if strings.Contains(message, "secret-provider-detail") || !strings.Contains(message, "private readiness check") || !strings.Contains(message, "further Fly resource steps") {
		t.Fatalf("generic primary error is not bounded and actionable: %s", message)
	}
}

func TestRecoveryMetadataMigratesRC7PublicationPolicy(t *testing.T) {
	work := t.TempDir()
	path := filepath.Join(work, "wrangler.jsonc")
	legacy := `{
  "name": "iserv-rm-control-test1234",
  "main": "src/guarded_index.js",
  "compatibility_date": "2026-08-18",
  "vars": {
    "INSTALL_WORKER_NAME": "iserv-rm-control-test1234",
    "INSTALL_REGION": "fra",
    "INSTALL_VERSION": "0.9.0-rc7",
    "INSTALL_KV_ID": "abcdefghijklmnop1234567890",
    "INSTALL_FLY_TOKEN_UNTIL": ""
  },
  "triggers": { "crons": ["* * * * *"] }
}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	meta := validLifecycleMeta()
	if err := ensureWorkerRecoveryMetadata(path, meta); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, marker := range []string{`"workers_dev": true`, `"preview_urls": false`, `"INSTALL_VERSION": "` + appVersion + `"`} {
		if strings.Count(text, marker) != 1 {
			t.Fatalf("migrated config marker count for %q is not one:\n%s", marker, text)
		}
	}
}

func TestWorkerPublicationPolicyRejectsAmbiguousConfig(t *testing.T) {
	for _, value := range []string{
		`{"compatibility_date":"2026-08-18","workers_dev":true,"workers_dev":false}`,
		`{"compatibility_date":"2026-08-18","workers_dev":"true"}`,
		`{"name":"iserv-rm-safe","workers_dev":true,"preview_urls":false,"preview_urls":true}`,
		`{"name":"bad/name","workers_dev":true}`,
	} {
		if _, err := enforceWorkerPublicationPolicy(value); err == nil {
			t.Fatalf("ambiguous Worker config was accepted: %s", value)
		}
	}
}

func TestCorruptJournalNeverBecomesFreshInstall(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	if err := os.WriteFile(app.journalPath(), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.loadJournal(); err == nil {
		t.Fatal("corrupt journal was accepted")
	}
}

func TestRecoveredRuntimeRebuildsOnlyFromNonSecretMetadata(t *testing.T) {
	work := t.TempDir()
	if err := extractPayload(work); err != nil {
		t.Fatal(err)
	}
	app := NewApp(work, "test-token")
	meta := validLifecycleMeta()
	if err := app.rebuildRecoveredRuntime(meta); err != nil {
		t.Fatal(err)
	}

	flyRaw, err := os.ReadFile(filepath.Join(work, "runtime", "fly.toml"))
	if err != nil {
		t.Fatal(err)
	}
	workerRaw, err := os.ReadFile(filepath.Join(work, "runtime", "worker", "wrangler.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	combined := string(flyRaw) + "\n" + string(workerRaw)
	for _, expected := range []string{meta.AppName, meta.WorkerName, meta.MachineID, meta.KVNamespaceID, meta.Region} {
		if !strings.Contains(combined, expected) {
			t.Fatalf("recovered runtime missing identifier %q", expected)
		}
	}
	for _, secret := range []string{"SUPER-SECRET-ISERV-PASSWORD", "SUPER-SECRET-DEVICE-TOKEN", "SUPER-SECRET-USER-TOKEN"} {
		if strings.Contains(combined, secret) {
			t.Fatalf("recovered runtime unexpectedly contained secret %q", secret)
		}
	}
}

func TestInstallationMetadataNeverPersistsConnectionSecrets(t *testing.T) {
	work := t.TempDir()
	app := NewApp(work, "test-token")
	app.st.mu.Lock()
	app.st.Secrets = secrets{
		IServURL:      "https://private-school.example/webdav",
		IServUser:     "student-private",
		IServPassword: "SUPER-SECRET-ISERV-PASSWORD",
		DeviceToken:   "SUPER-SECRET-DEVICE-TOKEN",
		UserToken:     "SUPER-SECRET-USER-TOKEN",
	}
	app.st.mu.Unlock()
	if err := app.saveMeta(validLifecycleMeta()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(work, "installation.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{
		"private-school.example", "student-private", "SUPER-SECRET-ISERV-PASSWORD",
		"SUPER-SECRET-DEVICE-TOKEN", "SUPER-SECRET-USER-TOKEN",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("installation metadata leaked %q", forbidden)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(work, "installation.json"))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Fatalf("installation metadata permissions=%#o want=0600", perm)
		}
	}
}

func TestFinalizeInstallationPublishesMetadataAndRemovesJournal(t *testing.T) {
	work := t.TempDir()
	app := NewApp(work, "test-token")
	meta := validLifecycleMeta()
	meta.AppName = "iserv-rm-deadbeef"
	meta.WorkerName = "iserv-rm-control-deadbeef"
	meta.DashboardURL = "https://iserv-rm-control-deadbeef.example.workers.dev"
	j := provisionJournal{
		AppName:       meta.AppName,
		WorkerName:    meta.WorkerName,
		MachineID:     meta.MachineID,
		DashboardURL:  meta.DashboardURL,
		KVNamespaceID: meta.KVNamespaceID,
		FlyTokenUntil: meta.FlyTokenUntil,
		Steps:         map[string]bool{"worker_deploy": true},
	}
	if err := app.saveJournal(j); err != nil {
		t.Fatal(err)
	}
	if err := app.finalizeInstallation(j, meta); err != nil {
		t.Fatal(err)
	}
	if app.metaSnapshot() == nil {
		t.Fatal("completed installation metadata was not published")
	}
	if _, err := os.Stat(app.journalPath()); !os.IsNotExist(err) {
		t.Fatalf("completed journal still exists: %v", err)
	}
	bad := *meta
	bad.MachineID = "deadbeefdeadbe"
	if err := app.finalizeInstallation(j, &bad); err == nil {
		t.Fatal("mismatched completion metadata was accepted")
	}
}

func TestDiagnosticsRedactSecretsURLsEmailsAndHomePaths(t *testing.T) {
	work := t.TempDir()
	app := NewApp(work, "test-token")
	home, _ := os.UserHomeDir()
	app.st.mu.Lock()
	app.st.Language = "en"
	app.st.Meta = &installMeta{AppName: "iserv-rm-diagnostic-test", Version: appVersion}
	app.st.MaintenanceError = "Authorization: Bearer SUPER-BEARER password=SUPER-PASSWORD https://dashboard.example/path admin@example.com"
	app.st.Logs = []string{
		"FlyV1 SUPER-FLY-TOKEN",
		"token=SUPER-TOKEN",
		"visit https://school.example/files/Math.pdf for student@example.com",
		"home=" + home + "/Documents/private.pdf",
	}
	app.st.Secrets = secrets{IServPassword: "SUPER-STATE-PASSWORD", DeviceToken: "SUPER-DEVICE", UserToken: "SUPER-USER"}
	app.st.mu.Unlock()
	if err := app.saveFirstRealState(firstRealSyncState{
		Status:  "failed",
		AppName: "iserv-rm-diagnostic-test",
		Error:   "provider detail https://private.example/path student@example.com " + home + "/private.pdf",
		Result: &verificationResult{
			Outcome: "failed",
			Reason:  "remarkable_provider token=SUPER-FIRST-REAL-TOKEN",
			Message: "Diagnosecode RM-UPLOAD-HTTP-400. https://private.example/document.pdf",
		},
	}); err != nil {
		t.Fatal(err)
	}

	path, err := app.createDiagnostics()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{
		"SUPER-BEARER", "SUPER-PASSWORD", "dashboard.example", "admin@example.com",
		"SUPER-FLY-TOKEN", "SUPER-TOKEN", "school.example", "student@example.com",
		"SUPER-STATE-PASSWORD", "SUPER-DEVICE", "SUPER-USER", "SUPER-FIRST-REAL-TOKEN",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("diagnostics leaked %q", forbidden)
		}
	}
	if home != "" && strings.Contains(text, home) {
		t.Fatalf("diagnostics leaked home directory %q", home)
	}
	for _, marker := range []string{"[redacted]", "[url redacted]", "[email redacted]"} {
		if !strings.Contains(text, marker) {
			t.Fatalf("diagnostics missing redaction marker %q", marker)
		}
	}

	var report diagnosticReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.Format != 1 || report.SetupVersion != appVersion {
		t.Fatalf("unexpected diagnostic metadata: format=%d setup=%q", report.Format, report.SetupVersion)
	}
	if report.FirstRealStatus != "failed" || report.LastRealRun == nil {
		t.Fatalf("first-real diagnostic context is missing: %#v", report)
	}
	if report.LastRealRun.Reason == "remarkable_provider token=SUPER-FIRST-REAL-TOKEN" || !strings.Contains(report.LastRealRun.Message, "RM-UPLOAD-HTTP-400") {
		t.Fatalf("first-real diagnostic context was not safely redacted: %#v", report.LastRealRun)
	}
}

func TestSetupIndexRendersCurrentVersionAndSecurityHeaders(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	req := httptest.NewRequest(http.MethodGet, "/?token=test-token", nil)
	rr := httptest.NewRecorder()
	app.handleIndex(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Setup Version "+appVersion) {
		t.Fatalf("rendered setup does not show current version %q", appVersion)
	}
	if strings.Contains(body, "__APP_VERSION__") || regexp.MustCompile(`0\.9\.0-rc1(?:[^0-9]|$)`).MatchString(body) {
		t.Fatal("rendered setup contains a stale or unresolved version marker")
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("setup index must be no-store")
	}
	if !strings.Contains(rr.Header().Get("Content-Security-Policy"), "script-src 'nonce-") {
		t.Fatal("setup index CSP nonce is missing")
	}
}

func TestAccessibilityBaselineMarkersRemainPresent(t *testing.T) {
	raw, err := embedded.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	for _, marker := range []string{
		"focus-visible",
		"prefers-reduced-motion",
		"aria-live",
		"<html lang=\"de\"",
		".statusbox.error{background:#fff0f0;border-color:#f1caca;color:var(--danger);max-height:14rem;overflow:auto}",
		".statusbox{padding:12px;border-radius:11px;background:#f7f9fa;border:1px solid #e7ebef;margin:10px 0;overflow-wrap:anywhere}",
	} {
		if !strings.Contains(html, marker) {
			t.Fatalf("accessibility baseline marker missing: %q", marker)
		}
	}
}

func TestCloudDeletionRequiresExactConfirmation(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	if err := app.maintenanceUninstall("delete"); err == nil {
		t.Fatal("destructive uninstall accepted a non-exact confirmation")
	}
	if err := app.maintenanceUninstall("DELETE"); err == nil || !strings.Contains(err.Error(), "keine bestehende Installation") {
		t.Fatalf("expected no-installation failure without external calls, got %v", err)
	}
}
