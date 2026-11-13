package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFlyBrowserLoginCompletesPKCEWithoutTerminal(t *testing.T) {
	var start map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/cli_sessions":
			if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&start) != nil {
				http.Error(w, "bad start", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"session-123","auth_url":"https://fly.io/auth/session-123","pkce":true}`))
		case "/api/v1/cli_sessions/session-123/redeem":
			var redeem map[string]string
			if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&redeem) != nil || redeem["code"] != "completion-code" {
				http.Error(w, "bad redeem", http.StatusBadRequest)
				return
			}
			challenge := sha256.Sum256([]byte(redeem["code_verifier"]))
			if start["code_challenge"] != base64.RawURLEncoding.EncodeToString(challenge[:]) {
				http.Error(w, "bad verifier", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"FlyV1 test-account-token"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	opener := func(authURL string) error {
		if authURL != "https://fly.io/auth/session-123" || start["code_challenge_method"] != "S256" {
			return fmt.Errorf("unexpected auth request")
		}
		port, err := numericJSONValue(start["redirect_port"])
		if err != nil {
			return err
		}
		state, _ := start["state"].(string)
		bad, err := http.Get("http://127.0.0.1:" + port + "/callback?state=wrong&code=ignored")
		if err != nil {
			return err
		}
		_ = bad.Body.Close()
		if bad.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("invalid state returned HTTP %d", bad.StatusCode)
		}
		good, err := http.Get("http://127.0.0.1:" + port + "/callback?state=" + state + "&code=completion-code")
		if err != nil {
			return err
		}
		defer good.Body.Close()
		if good.StatusCode != http.StatusOK {
			return fmt.Errorf("valid callback returned HTTP %d", good.StatusCode)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	token, err := flyBrowserLogin(ctx, server.URL, server.Client(), opener, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "FlyV1 test-account-token" {
		t.Fatalf("unexpected token: %q", token)
	}
}

func TestFlyBrowserLoginRejectsUnsafeAuthURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"session-123","auth_url":"http://example.test/login","pkce":true}`))
	}))
	defer server.Close()
	openerCalled := false
	_, err := flyBrowserLogin(context.Background(), server.URL, server.Client(), func(string) error {
		openerCalled = true
		return nil
	}, nil)
	if err == nil || openerCalled {
		t.Fatal("unsafe Fly auth URL was accepted")
	}
}

func TestFlyBrowserLoginAcceptsManualCompletionCodeAndAllowsRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/cli_sessions":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"manual-session","auth_url":"https://fly.io/auth/manual","pkce":true}`))
		case "/api/v1/cli_sessions/manual-session/redeem":
			attempts++
			var redeem map[string]string
			_ = json.NewDecoder(r.Body).Decode(&redeem)
			if redeem["code"] != "valid-code" {
				http.Error(w, "invalid", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"FlyV1 manual-token"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	manual := make(chan flyCodeSubmission)
	done := make(chan struct{})
	var token string
	var loginErr error
	go func() {
		token, loginErr = flyBrowserLogin(context.Background(), server.URL, server.Client(), func(string) error { return nil }, manual)
		close(done)
	}()
	for _, test := range []struct {
		code    string
		wantErr bool
	}{{"wrong-code", true}, {"valid-code", false}} {
		result := make(chan error, 1)
		manual <- flyCodeSubmission{code: test.code, result: result}
		if gotErr := <-result; (gotErr != nil) != test.wantErr {
			t.Fatalf("code %q error=%v, wantErr=%v", test.code, gotErr, test.wantErr)
		}
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("manual Fly.io login did not finish")
	}
	if loginErr != nil || token != "FlyV1 manual-token" || attempts != 2 {
		t.Fatalf("token=%q attempts=%d err=%v", token, attempts, loginErr)
	}
}

func TestFlyCodeEndpointForwardsOnlyToActiveLogin(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	request := httptest.NewRequest(http.MethodPost, "/api/fly/code", bytes.NewBufferString(`{"code":"manual-code"}`))
	recorder := httptest.NewRecorder()
	app.handleFlyCode(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("inactive login status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	broker := &flyCodeBroker{submissions: make(chan flyCodeSubmission), done: make(chan struct{})}
	app.flyCodeBroker = broker
	done := make(chan struct{})
	request = httptest.NewRequest(http.MethodPost, "/api/fly/code", bytes.NewBufferString(`{"code":" manual-code "}`))
	recorder = httptest.NewRecorder()
	go func() {
		app.handleFlyCode(recorder, request)
		close(done)
	}()
	submission := <-broker.submissions
	if submission.code != "manual-code" {
		t.Fatalf("forwarded code=%q", submission.code)
	}
	submission.result <- nil
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("code endpoint did not return")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("active login status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func numericJSONValue(value any) (string, error) {
	number, ok := value.(float64)
	if !ok || number < 1 || number > 65535 {
		return "", fmt.Errorf("invalid callback port")
	}
	return strconv.Itoa(int(number)), nil
}

func TestCommandEnvironmentReplacesPATH(t *testing.T) {
	t.Setenv("PATH", "old-path")
	env := commandEnvironment(map[string]string{"PATH": "new-path", "ISERV_RM_TEST_VALUE": "kept"})
	pathCount := 0
	extraFound := false
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if strings.EqualFold(key, "PATH") {
			pathCount++
			if value != "new-path" {
				t.Fatalf("PATH=%q", value)
			}
		}
		if key == "ISERV_RM_TEST_VALUE" && value == "kept" {
			extraFound = true
		}
	}
	if pathCount != 1 {
		t.Fatalf("PATH entries=%d, want 1", pathCount)
	}
	if !extraFound {
		t.Fatal("additional environment value was not preserved")
	}
}

func TestManagedNodeEnvironmentClearsInjectionOptions(t *testing.T) {
	t.Setenv("NODE_OPTIONS", "--require=/tmp/injected.js")
	t.Setenv("NODE_PATH", "/tmp/injected-modules")
	env := commandEnvironment(managedNodeEnvironment("/managed/bin/node"))
	for _, entry := range env {
		if entry == "NODE_OPTIONS=--require=/tmp/injected.js" || entry == "NODE_PATH=/tmp/injected-modules" {
			t.Fatalf("managed Node inherited injection option: %s", entry)
		}
	}
}
