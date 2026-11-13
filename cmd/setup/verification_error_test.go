package main

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDryRunStartErrorRecognizesFlyCredentialWithoutLeakingProviderDetails(t *testing.T) {
	app := NewApp(t.TempDir(), "session-token")
	raw := []byte(`{"error":"Fly-Status: HTTP 401 provider-private-detail","code":"fly_auth_rejected"}`)
	err := app.runStartError(502, raw)
	if err == nil || !strings.Contains(err.Error(), "erneuert") || !strings.Contains(err.Error(), "Neuinstallation ist nicht erforderlich") {
		t.Fatalf("unexpected primary error: %v", err)
	}
	if strings.Contains(err.Error(), "provider-private-detail") || strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("primary error leaked provider detail: %v", err)
	}
}

func TestDryRunStartErrorKeepsUnknownFailureBounded(t *testing.T) {
	app := NewApp(t.TempDir(), "session-token")
	err := app.runStartError(502, []byte(`{"error":"unexpected provider-private-detail","token":"FlyV1 fm2_private_token"}`+strings.Repeat("x", 2000)))
	if err == nil || !strings.Contains(err.Error(), "Installation fortsetzen") || strings.Contains(err.Error(), "provider-private-detail") {
		t.Fatalf("unexpected primary error: %v", err)
	}
	app.st.mu.RLock()
	logs := strings.Join(app.st.Logs, "\n")
	app.st.mu.RUnlock()
	if strings.Contains(logs, "fm2_private_token") || len(logs) > 1200 {
		t.Fatalf("technical detail was not safely bounded and redacted: %q", logs)
	}
}

func TestDryRunStartRetriesFlyMachineReplacementThenSucceeds(t *testing.T) {
	app := NewApp(t.TempDir(), "session-token")
	calls := 0
	var pauses []time.Duration
	request := func(base, password, method, requestPath string, body []byte) ([]byte, int, error) {
		calls++
		if base != "https://example.workers.dev" || password != "test-password" || method != http.MethodPost || requestPath != "/api/admin/run" || string(body) != `{}` {
			t.Fatalf("unexpected request: %q %q %q %q %q", base, password, method, requestPath, body)
		}
		if calls < 3 {
			return []byte(`{"error":"provider-private-detail","code":"fly_machine_transition"}`), 503, nil
		}
		return []byte(`{"ok":true,"result":{"started":true,"state":"starting","requested_at":"2026-11-11T08:00:01Z"}}`), 200, nil
	}
	_, err := app.startRunWithRetry("https://example.workers.dev", "test-password", request, func(delay time.Duration) {
		pauses = append(pauses, delay)
	})
	if err != nil || calls != 3 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	if len(pauses) != 2 || pauses[0] != 3*time.Second || pauses[1] != 5*time.Second {
		t.Fatalf("unexpected retry delays: %v", pauses)
	}
}

func TestDryRunStartDoesNotRetryPermanentFailure(t *testing.T) {
	app := NewApp(t.TempDir(), "session-token")
	calls := 0
	_, err := app.startRunWithRetry("https://example.workers.dev", "test-password", func(_, _, _, _ string, _ []byte) ([]byte, int, error) {
		calls++
		return []byte(`{"error":"provider-private-detail","code":"fly_start_failed"}`), 502, nil
	}, func(time.Duration) {
		t.Fatal("permanent failure must not pause for a retry")
	})
	if err == nil || calls != 1 || !strings.Contains(err.Error(), "Installation fortsetzen") {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestDryRunStartBoundsPersistentMachineReplacement(t *testing.T) {
	app := NewApp(t.TempDir(), "session-token")
	calls := 0
	pauses := 0
	_, err := app.startRunWithRetry("https://example.workers.dev", "test-password", func(_, _, _, _ string, _ []byte) ([]byte, int, error) {
		calls++
		return []byte(`{"error":"provider-private-detail","code":"fly_machine_transition"}`), 503, nil
	}, func(time.Duration) {
		pauses++
	})
	if err == nil || calls != 8 || pauses != 7 {
		t.Fatalf("calls=%d pauses=%d err=%v", calls, pauses, err)
	}
	if !strings.Contains(err.Error(), "ohne Neuinstallation fortgesetzt") || strings.Contains(err.Error(), "provider-private-detail") {
		t.Fatalf("unexpected bounded transition error: %v", err)
	}
}

func TestRunStartWaitsForExistingMachineAndTemporaryStatusStorage(t *testing.T) {
	app := NewApp(t.TempDir(), "session-token")
	calls := 0
	started, err := app.startRunWithRetry("https://example.workers.dev", "test-password",
		func(_, _, _, _ string, _ []byte) ([]byte, int, error) {
			calls++
			switch calls {
			case 1:
				return []byte(`{"result":{"started":false,"state":"started"}}`), 200, nil
			case 2:
				return []byte(`{"code":"status_storage_busy"}`), 503, nil
			default:
				return []byte(`{"result":{"started":true,"requested_at":"2026-11-11T08:00:01.123Z"}}`), 200, nil
			}
		}, func(time.Duration) {})
	if err != nil || calls != 3 || started.IsZero() {
		t.Fatalf("calls=%d started=%v err=%v", calls, started, err)
	}
	if reportFollowsStart(&dashboardRunReport{FinishedAt: "2026-11-11T08:00:00Z"}, started) {
		t.Fatal("previous Machine report was accepted")
	}
	if !reportFollowsStart(&dashboardRunReport{FinishedAt: "2026-11-11T08:00:01Z"}, started) {
		t.Fatal("same-second report was rejected")
	}
}

func TestRunStartRequiresExplicitStartConfirmation(t *testing.T) {
	for _, raw := range []string{`{"ok":true}`, `{"result":{"started":true}}`, `{"result":{"started":false,"state":"unknown"}}`} {
		app := NewApp(t.TempDir(), "session-token")
		_, err := app.startRunWithRetry("https://example.workers.dev", "test-password",
			func(_, _, _, _ string, _ []byte) ([]byte, int, error) { return []byte(raw), 200, nil },
			func(time.Duration) { t.Fatal("ambiguous success must not retry") })
		if err == nil {
			t.Fatalf("unconfirmed start accepted: %s", raw)
		}
	}
}

func TestDryRunRequiresSafeSettingsAndSuccessfulReport(t *testing.T) {
	settings := map[string]any{"enabled": false, "sync": map[string]any{"dry_run": true}}
	if !dryRunSettingsConfirmed(settings) {
		t.Fatal("explicit safe settings rejected")
	}
	for _, unsafe := range []map[string]any{
		nil,
		{"sync": settings["sync"]},
		{"enabled": true, "sync": settings["sync"]},
		{"enabled": false, "sync": map[string]any{"dry_run": false}},
	} {
		if dryRunSettingsConfirmed(unsafe) {
			t.Fatalf("accepted unconfirmed settings: %v", unsafe)
		}
	}
	valid := dashboardRunReport{
		FinishedAt: "2026-11-11T08:00:01Z", OK: true, DryRun: true,
		Outcome: "success", Reason: "dry_run", Stats: &verificationStats{Checked: 1, WouldUpload: 1},
	}
	if !valid.confirmsDryRun() || !newerRunReport(&valid, "2026-11-11T09:00:00+01:00") {
		t.Fatal("current successful test report rejected")
	}
	if newerRunReport(&valid, "2026-11-11T09:00:01+01:00") ||
		newerRunReport(&valid, "2026-11-11T09:00:02+01:00") {
		t.Fatal("stale or identical report accepted")
	}
	for name, corrupt := range map[string]func(*dashboardRunReport){
		"setup placeholder": func(r *dashboardRunReport) { r.Reason = "setup" },
		"error":             func(r *dashboardRunReport) { r.Error = "diagnostic" },
		"partial":           func(r *dashboardRunReport) { r.Outcome = "partial" },
		"missing stats":     func(r *dashboardRunReport) { r.Stats = nil },
		"failure count":     func(r *dashboardRunReport) { r.Stats = &verificationStats{Failed: 1} },
		"upload count":      func(r *dashboardRunReport) { r.Stats = &verificationStats{Uploaded: 1} },
		"not OK":            func(r *dashboardRunReport) { r.OK = false },
		"live report":       func(r *dashboardRunReport) { r.DryRun = false },
	} {
		report := valid
		corrupt(&report)
		if report.confirmsDryRun() {
			t.Errorf("%s was treated as a successful dry run", name)
		}
	}
}

func TestSafeSettingsWaitsForPropagationAndBoundsUnconfirmedState(t *testing.T) {
	for _, becomesSafe := range []bool{true, false} {
		reads, pauses := 0, 0
		_, err := waitForDryRunSettings(func() (dashboardState, error) {
			reads++
			current := dashboardState{}
			current.Settings = map[string]any{"enabled": true, "sync": map[string]any{"dry_run": false}}
			if becomesSafe && reads == 3 {
				current.Settings = map[string]any{"enabled": false, "sync": map[string]any{"dry_run": true}}
			}
			return current, nil
		}, func(time.Duration) { pauses++ })
		if becomesSafe && (err != nil || reads != 3 || pauses != 2) {
			t.Fatalf("safe settings were not recognized: reads=%d pauses=%d err=%v", reads, pauses, err)
		}
		if !becomesSafe && (err == nil || reads != 5 || pauses != 4) {
			t.Fatalf("unsafe settings wait was not bounded: reads=%d pauses=%d err=%v", reads, pauses, err)
		}
	}
}

func TestDryRunStartStopsOnRequestError(t *testing.T) {
	app := NewApp(t.TempDir(), "session-token")
	want := errors.New("network unavailable")
	_, err := app.startRunWithRetry("https://example.workers.dev", "test-password", func(_, _, _, _ string, _ []byte) ([]byte, int, error) {
		return nil, 0, want
	}, func(time.Duration) {
		t.Fatal("ambiguous request failure must not be retried")
	})
	if !errors.Is(err, want) {
		t.Fatalf("err=%v want=%v", err, want)
	}
}
