package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCloudComponentsCurrentRequiresExactInstallerVersion(t *testing.T) {
	if cloudComponentsCurrent(nil) {
		t.Fatal("missing installation was accepted as current")
	}
	if cloudComponentsCurrent(&installMeta{Version: "0.9.0-rc16"}) {
		t.Fatal("older cloud components were accepted as current")
	}
	if !cloudComponentsCurrent(&installMeta{Version: appVersion}) {
		t.Fatal("matching cloud components were rejected")
	}
}

func TestRealRunAndAutomationRejectOutdatedCloudComponentsBeforeRemoteAccess(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler func(*App, http.ResponseWriter, *http.Request)
	}{
		{"limited real run", (*App).handleFirstRealSyncStart},
		{"automatic sync", (*App).handleEnableAutomaticSync},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := NewApp(t.TempDir(), "test-token")
			app.st.Meta = &installMeta{Version: "0.9.0-rc16", AppName: "iserv-rm-old", DashboardURL: "https://unreachable.invalid"}
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"password":"correct-horse-battery-staple"}`))
			req.Header.Set("X-Setup-Lang", "en")
			rr := httptest.NewRecorder()
			tc.handler(app, rr, req)
			if rr.Code != http.StatusConflict {
				t.Fatalf("status=%d want=%d body=%s", rr.Code, http.StatusConflict, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), "Update cloud components") {
				t.Fatalf("response is not actionable: %s", rr.Body.String())
			}
		})
	}
}

func TestInjectFirstRealSyncUIAddsGatedPostInstallFlow(t *testing.T) {
	raw, err := embedded.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	enhanced, ok := injectFirstRealSyncUI(string(raw))
	if !ok {
		t.Fatal("first-real-sync UI was not injected")
	}
	for _, marker := range []string{
		"firstRealSyncPanel",
		"/api/install/first-real/start",
		"/api/install/first-real/status",
		"/api/install/enable-automatic",
		"at most five upload attempts",
	} {
		if !strings.Contains(enhanced, marker) {
			t.Fatalf("enhanced setup page missing %q", marker)
		}
	}
	if strings.Count(enhanced, "firstRealSyncPanel") != 2 {
		// One occurrence is the DOM lookup and one is the assigned element id.
		t.Fatalf("unexpected first-real UI marker count: %d", strings.Count(enhanced, "firstRealSyncPanel"))
	}
}

func TestPrepareFirstRealSettingsKeepsAutomationOff(t *testing.T) {
	current := map[string]any{
		"enabled": false,
		"sync": map[string]any{
			"dry_run": true,
			"sources": []any{map[string]any{"name": "Test"}},
		},
	}
	realSettings, restoreSettings, err := prepareFirstRealSettings(current)
	if err != nil {
		t.Fatal(err)
	}
	if realSettings["enabled"] != false {
		t.Fatal("real verification unexpectedly enabled automation")
	}
	if realSettings["sync"].(map[string]any)["dry_run"] != false {
		t.Fatal("real verification did not switch out of dry-run mode")
	}
	if restoreSettings["enabled"] != false || restoreSettings["sync"].(map[string]any)["dry_run"] != true {
		t.Fatal("restore settings are not fail-closed")
	}
	if current["enabled"] != false || current["sync"].(map[string]any)["dry_run"] != true {
		t.Fatal("input settings were mutated")
	}
}

func TestPrepareFirstRealSettingsRejectsUnsafeStartingState(t *testing.T) {
	cases := []map[string]any{
		{"enabled": true, "sync": map[string]any{"dry_run": true}},
		{"enabled": false, "sync": map[string]any{"dry_run": false}},
		{"enabled": false, "sync": map[string]any{}},
	}
	for _, current := range cases {
		if _, _, err := prepareFirstRealSettings(current); err == nil {
			t.Fatalf("unsafe starting state was accepted: %#v", current)
		}
	}
}

func TestRecoverFirstRealSafeStartingSettingsLeavesSafeStateUnchanged(t *testing.T) {
	current := map[string]any{
		"enabled": false,
		"sync": map[string]any{
			"dry_run": true,
			"sources": []any{map[string]any{"name": "Test"}},
		},
	}
	recovered, changed, err := recoverFirstRealSafeStartingSettings(current)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("already-safe state was reported as recovered")
	}
	if recovered["sync"].(map[string]any)["dry_run"] != true {
		t.Fatal("safe dry-run state was changed")
	}
}

func TestRecoverFirstRealSafeStartingSettingsRepairsInterruptedRealModeWithoutMutatingInput(t *testing.T) {
	current := map[string]any{
		"enabled": false,
		"sync": map[string]any{
			"dry_run": false,
			"sources": []any{map[string]any{"name": "Test"}},
		},
	}
	recovered, changed, err := recoverFirstRealSafeStartingSettings(current)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("interrupted real-mode state was not detected")
	}
	if recovered["enabled"] != false || recovered["sync"].(map[string]any)["dry_run"] != true {
		t.Fatalf("recovered state is not fail-closed: %#v", recovered)
	}
	if current["sync"].(map[string]any)["dry_run"] != false {
		t.Fatal("recovery mutated the input state before it was deliberately persisted")
	}
}

func TestRecoverFirstRealSafeStartingSettingsRejectsUnsafeOrMalformedState(t *testing.T) {
	cases := []map[string]any{
		{"enabled": true, "sync": map[string]any{"dry_run": false}},
		{"enabled": false},
		{"enabled": false, "sync": map[string]any{}},
		{"enabled": false, "sync": "invalid"},
	}
	for _, current := range cases {
		if _, _, err := recoverFirstRealSafeStartingSettings(current); err == nil {
			t.Fatalf("unsafe or malformed recovery state was accepted: %#v", current)
		}
	}
	if _, _, err := recoverFirstRealSafeStartingSettings(nil); err == nil {
		t.Fatal("nil recovery state was accepted")
	}
}

func TestFirstRealResultVerifiedRequiresActualSuccessfulUpload(t *testing.T) {
	valid := []*verificationResult{
		{Outcome: "success", Stats: verificationStats{Uploaded: 1, Failed: 0}},
		{Outcome: "partial", Stats: verificationStats{Uploaded: 5, Deferred: 3, Failed: 0}},
	}
	for _, result := range valid {
		if !firstRealResultVerified(result) {
			t.Fatalf("valid first-real result was rejected: %#v", result)
		}
	}

	invalid := []*verificationResult{
		nil,
		{Outcome: "success", Stats: verificationStats{Uploaded: 0, Failed: 0}},
		{Outcome: "partial", Stats: verificationStats{Uploaded: 0, Deferred: 4, Failed: 0}},
		{Outcome: "success", Stats: verificationStats{Uploaded: 1, Failed: 1}},
		{Outcome: "failed", Stats: verificationStats{Uploaded: 1, Failed: 0}},
		{Outcome: "unexpected", Stats: verificationStats{Uploaded: 1, Failed: 0}},
	}
	for _, result := range invalid {
		if firstRealResultVerified(result) {
			t.Fatalf("unsafe first-real result was accepted: %#v", result)
		}
	}
}

func TestFirstRealActivationCanRecoverAuthoritativeDashboardVerification(t *testing.T) {
	local := firstRealSyncState{Status: "idle"}
	var dashboard dashboardState
	dashboard.Activation.RealRunVerified = true
	if !firstRealActivationVerified(local, dashboard) {
		t.Fatal("authoritative dashboard verification was not accepted for local recovery")
	}

	dashboard.Activation.RealRunVerified = false
	if firstRealActivationVerified(local, dashboard) {
		t.Fatal("unverified local and dashboard state unlocked activation")
	}
	local.Status = "enabled"
	if firstRealActivationVerified(local, dashboard) {
		t.Fatal("stale local enabled state overrode authoritative locked dashboard state")
	}

	local = firstRealSyncState{
		Status: "completed",
		Result: &verificationResult{Outcome: "success", Stats: verificationStats{Uploaded: 1}},
	}
	if !firstRealActivationVerified(local, dashboard) {
		t.Fatal("valid local verification was rejected")
	}
}

func TestFirstRealFailureMessageIsActionableAndLocalized(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	stats := verificationStats{Failed: 1, Deferred: 60}
	de := app.firstRealFailureMessage("remarkable_auth", stats)
	if !strings.Contains(de, "reMarkable") || !strings.Contains(de, "erneut") {
		t.Fatalf("unexpected German action message: %q", de)
	}
	app.st.mu.Lock()
	app.st.Language = "en"
	app.st.mu.Unlock()
	en := app.firstRealFailureMessage("remarkable_auth", stats)
	if !strings.Contains(en, "Reconnect") {
		t.Fatalf("unexpected English action message: %q", en)
	}
}

func TestFirstRealFailureMessagesCoverIServReasons(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	for _, reason := range []string{"iserv_auth", "iserv_source", "iserv_certificate", "iserv_storage", "iserv_network", "iserv_provider"} {
		message := app.firstRealFailureMessage(reason, verificationStats{})
		if !strings.Contains(message, "IServ") && !strings.Contains(message, "Fly-Speicher") {
			t.Fatalf("IServ reason %q has no actionable safe message: %q", reason, message)
		}
		if strings.Contains(message, "/Users/") || strings.Contains(message, "rclone") {
			t.Fatalf("IServ reason %q exposed technical or private details: %q", reason, message)
		}
	}
}

func TestFirstRealFailureMessagesDistinguishRemarkableRecovery(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	for _, reason := range []string{"remarkable_generation", "remarkable_protocol", "remarkable_provider"} {
		message := app.firstRealFailureMessage(reason, verificationStats{})
		if !strings.Contains(message, "reMarkable") {
			t.Fatalf("reMarkable reason %q has no actionable safe message: %q", reason, message)
		}
	}
	provider := app.firstRealFailureMessage("remarkable_provider", verificationStats{})
	if strings.Contains(provider, "Verbinde reMarkable erneut") {
		t.Fatalf("generic provider failure incorrectly recommends reauthentication: %q", provider)
	}
	provider = app.firstRealFailureMessage(
		"remarkable_provider",
		verificationStats{},
		"private response omitted. Diagnosecode RM-UPLOAD-HTTP-400. /Users/private/document.pdf",
	)
	if !strings.Contains(provider, "RM-UPLOAD-HTTP-400") || strings.Contains(provider, "/Users/") {
		t.Fatalf("provider diagnostic code was not preserved safely: %q", provider)
	}
	protocol := app.firstRealFailureMessage("remarkable_protocol", verificationStats{})
	if !strings.Contains(protocol, "Cloud-Komponenten") {
		t.Fatalf("protocol failure does not direct the user to the component update: %q", protocol)
	}
}

func TestFirstRealFailureMessageDoesNotHideFailuresBehindUploadLimit(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	message := app.firstRealFailureMessage("upload_limit", verificationStats{Failed: 5, Deferred: 56})
	if !strings.Contains(message, "fehlgeschlagen") || !strings.Contains(message, "Automatik bleibt AUS") {
		t.Fatalf("upload failures were hidden by limit message: %q", message)
	}
}

func TestFirstRealFailureMessageDoesNotExposeUnknownProviderText(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	message := app.firstRealFailureMessage("unknown_provider_reason", verificationStats{Failed: 1})
	if strings.Contains(message, "/Users/private/document.pdf") || message != "Der begrenzte echte Lauf ist fehlgeschlagen. Automatik bleibt AUS." {
		t.Fatalf("unexpected unknown-provider message: %q", message)
	}
}

func TestFirstRealStateIsPrivateAndScopedToInstallation(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	state := firstRealSyncState{
		Status:    "completed",
		AppName:   "iserv-rm-test1234",
		UpdatedAt: time.Now().UTC(),
		Result:    &verificationResult{Outcome: "success", Stats: verificationStats{Uploaded: 1}},
	}
	if err := app.saveFirstRealState(state); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(app.firstRealStatePath())
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("first-real state permissions=%#o want=0600", info.Mode().Perm())
		}
	}
	matching := app.loadFirstRealState(&installMeta{AppName: state.AppName})
	if matching.Status != "completed" {
		t.Fatalf("matching installation lost state: %#v", matching)
	}
	other := app.loadFirstRealState(&installMeta{AppName: "iserv-rm-other1234"})
	if other.Status != "idle" {
		t.Fatalf("state leaked across installations: %#v", other)
	}
}

func TestLoadFirstRealStateRecoversAbandonedRunningStateImmediatelyAfterRelaunch(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	state := firstRealSyncState{
		Status:    "running",
		AppName:   "iserv-rm-test1234",
		StartedAt: firstRealProcessStartedAt.Add(-time.Minute),
		UpdatedAt: firstRealProcessStartedAt.Add(-time.Minute),
	}
	if err := app.saveFirstRealState(state); err != nil {
		t.Fatal(err)
	}
	loaded := app.loadFirstRealState(&installMeta{AppName: state.AppName})
	if loaded.Status != "failed" {
		t.Fatalf("abandoned prior-process run remained stuck as running: %#v", loaded)
	}
	if !strings.Contains(loaded.Error, "nicht sauber abgeschlossen") {
		t.Fatalf("abandoned-run recovery returned unexpected explanation: %q", loaded.Error)
	}
}

func TestLoadFirstRealStateKeepsCurrentProcessRunRunning(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	state := firstRealSyncState{
		Status:    "running",
		AppName:   "iserv-rm-test1234",
		StartedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if !state.StartedAt.After(firstRealProcessStartedAt) {
		t.Skip("test clock resolution did not produce a post-process-start timestamp")
	}
	if err := app.saveFirstRealState(state); err != nil {
		t.Fatal(err)
	}
	loaded := app.loadFirstRealState(&installMeta{AppName: state.AppName})
	if loaded.Status != "running" {
		t.Fatalf("current-process run was incorrectly recovered: %#v", loaded)
	}
}

func TestLoadFirstRealStateRejectsLegacyCompletedStateWithoutVerifiedUpload(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	state := firstRealSyncState{
		Status:    "completed",
		AppName:   "iserv-rm-test1234",
		UpdatedAt: time.Now().UTC(),
		Result:    &verificationResult{Outcome: "success", Stats: verificationStats{Uploaded: 0, Failed: 0}},
	}
	if err := app.saveFirstRealState(state); err != nil {
		t.Fatal(err)
	}
	loaded := app.loadFirstRealState(&installMeta{AppName: state.AppName})
	if loaded.Status != "failed" {
		t.Fatalf("unverified legacy completion remained trusted: %#v", loaded)
	}
	if !strings.Contains(loaded.Error, "keinen gültigen erfolgreichen Upload") {
		t.Fatalf("legacy completion recovery returned unexpected explanation: %q", loaded.Error)
	}
}

func TestFirstRealPasswordShape(t *testing.T) {
	if !firstRealPasswordValid("correct-horse-battery-staple") {
		t.Fatal("valid dashboard password shape was rejected")
	}
	for _, value := range []string{"short", strings.Repeat("a", 257), "123456789012345\nunsafe"} {
		if firstRealPasswordValid(value) {
			t.Fatalf("unsafe dashboard password shape accepted: %q", value)
		}
	}
}

func TestEnableAutomaticRollsBackWhenLocalStateCannotBeSaved(t *testing.T) {
	enabled := map[string]any{"enabled": true, "sync": map[string]any{"dry_run": false}}
	rollback := map[string]any{"enabled": false, "sync": map[string]any{"dry_run": true}}
	var writes []bool
	err := enableAutomaticWithRollback(func(settings map[string]any) error {
		writes = append(writes, settings["enabled"].(bool))
		return nil
	}, enabled, rollback, func() error {
		return os.ErrPermission
	})
	if err == nil || !strings.Contains(err.Error(), "sicher zurückgenommen") {
		t.Fatalf("missing rollback error after local save failure: %v", err)
	}
	if len(writes) != 2 || !writes[0] || writes[1] {
		t.Fatalf("unexpected remote settings sequence: %#v", writes)
	}
}

func TestEnableAutomaticReportsFailedRollback(t *testing.T) {
	calls := 0
	err := enableAutomaticWithRollback(func(map[string]any) error {
		calls++
		if calls == 2 {
			return os.ErrDeadlineExceeded
		}
		return nil
	}, map[string]any{}, map[string]any{}, func() error {
		return os.ErrPermission
	})
	if err == nil || !strings.Contains(err.Error(), "Zurücksetzen ist fehlgeschlagen") || !strings.Contains(err.Error(), os.ErrDeadlineExceeded.Error()) {
		t.Fatalf("failed rollback was not surfaced: %v", err)
	}
}
