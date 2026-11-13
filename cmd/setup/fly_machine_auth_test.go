package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidateFlyMachineControlTokenUsesExactFlyV1HeaderAndRetriesPropagation(t *testing.T) {
	const token = "FlyV1 fm2_permission_token,fm2_discharge_token"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps/iserv-rm-deadbeef/machines/1234567890abcd" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != token {
			t.Fatalf("authorization=%q", got)
		}
		if requests.Add(1) == 1 {
			http.Error(w, "not ready", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1234567890abcd"}`))
	}))
	defer server.Close()

	var sleeps []time.Duration
	err := validateFlyMachineControlTokenWith(server.Client(), server.URL, "iserv-rm-deadbeef", "1234567890abcd", token, func(delay time.Duration) {
		sleeps = append(sleeps, delay)
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || len(sleeps) != 1 || sleeps[0] != 500*time.Millisecond {
		t.Fatalf("requests=%d sleeps=%v", requests.Load(), sleeps)
	}
}

func TestValidateFlyMachineControlTokenFailsClosedWithoutLeakingToken(t *testing.T) {
	const token = "FlyV1 fm2_private_token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "provider detail", http.StatusForbidden)
	}))
	defer server.Close()

	err := validateFlyMachineControlTokenWith(server.Client(), server.URL, "iserv-rm-deadbeef", "1234567890abcd", token, func(time.Duration) {})
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "provider detail") {
		t.Fatalf("unexpected error %v", err)
	}
	for _, invalid := range []string{"", "Bearer " + token, token + ",", token + ",short", token + "\nInjected: value"} {
		if err := validateFlyMachineControlTokenWith(server.Client(), server.URL, "iserv-rm-deadbeef", "1234567890abcd", invalid, func(time.Duration) {}); err == nil {
			t.Fatalf("accepted invalid token %q", invalid)
		}
	}
}

func TestValidateFlyMachineControlTokenUsesFullBoundedPropagationWindow(t *testing.T) {
	const token = "FlyV1 fm2_test_token"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "not ready", http.StatusUnauthorized)
	}))
	defer server.Close()

	var sleeps []time.Duration
	err := validateFlyMachineControlTokenWith(server.Client(), server.URL, "iserv-rm-deadbeef", "1234567890abcd", token, func(delay time.Duration) {
		sleeps = append(sleeps, delay)
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("unexpected error %v", err)
	}
	if requests.Load() != int32(len(flyControlTokenValidationDelays)+1) {
		t.Fatalf("requests=%d want=%d", requests.Load(), len(flyControlTokenValidationDelays)+1)
	}
	if len(sleeps) != len(flyControlTokenValidationDelays) {
		t.Fatalf("sleeps=%v", sleeps)
	}
	for index, delay := range sleeps {
		if delay != flyControlTokenValidationDelays[index] {
			t.Fatalf("sleep[%d]=%v want=%v", index, delay, flyControlTokenValidationDelays[index])
		}
	}
}

func TestPrepareFlyControlTokenReusesPendingCredential(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	app.pendingFlyControl = &pendingFlyControlToken{
		appName:          "iserv-rm-deadbeef",
		token:            "FlyV1 fm2_pending_token",
		previousTokenIDs: []string{"previous-token-id"},
		createdAt:        time.Now(),
	}
	token, previous, reused, err := app.prepareFlyControlToken("", "must-not-run", "iserv-rm-deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if !reused || token != "FlyV1 fm2_pending_token" || len(previous) != 1 || previous[0] != "previous-token-id" {
		t.Fatalf("token=%q previous=%v reused=%v", token, previous, reused)
	}
	previous[0] = "mutated"
	if app.pendingFlyControl.previousTokenIDs[0] != "previous-token-id" {
		t.Fatal("pending token inventory was returned without a defensive copy")
	}
	app.clearPendingFlyControlToken("iserv-rm-deadbeef", token)
	if app.pendingFlyControl != nil {
		t.Fatal("published pending credential was not cleared")
	}
}

func TestValidateFlyMachineControlTokenHonorsOverallDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := validateFlyMachineControlTokenWithContext(ctx, http.DefaultClient, "https://api.machines.dev", "iserv-rm-deadbeef", "1234567890abcd", "FlyV1 fm2_test_token", func(time.Duration) {
		t.Fatal("deadline validation unexpectedly slept")
	})
	if err == nil || !strings.Contains(err.Error(), "nicht rechtzeitig") {
		t.Fatalf("unexpected error %v", err)
	}
}
