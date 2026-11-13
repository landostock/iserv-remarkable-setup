package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const flyAPIBaseURL = "https://api.fly.io"

var flySessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

type flyCLISession struct {
	ID          string `json:"id"`
	AuthURL     string `json:"auth_url"`
	AccessToken string `json:"access_token"`
	PKCE        bool   `json:"pkce"`
}

type flyCodeSubmission struct {
	code   string
	result chan error
}

type flyCodeBroker struct {
	submissions chan flyCodeSubmission
	done        chan struct{}
}

func randomURLToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func validFlyAuthURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.User != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "fly.io" || strings.HasSuffix(host, ".fly.io")
}

func decodeFlySession(resp *http.Response, expectedStatus int) (flyCLISession, error) {
	defer resp.Body.Close()
	if resp.StatusCode != expectedStatus {
		return flyCLISession{}, fmt.Errorf("Fly.io-Anmeldung antwortete mit HTTP %d", resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, 64<<10)
	var session flyCLISession
	if err := json.NewDecoder(limited).Decode(&session); err != nil {
		return flyCLISession{}, fmt.Errorf("Fly.io-Anmeldung lieferte eine ungültige Antwort")
	}
	return session, nil
}

func flyAuthHTTPClient(timeout time.Duration) *http.Client {
	client := secureHTTPClient(timeout)
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}

func redeemFlySession(ctx context.Context, apiBase string, client *http.Client, sessionID, verifier, completionCode string) (string, bool, error) {
	redeemBody, err := json.Marshal(map[string]string{"code": completionCode, "code_verifier": verifier})
	if err != nil {
		return "", false, err
	}
	redeemURL := strings.TrimRight(apiBase, "/") + "/api/v1/cli_sessions/" + url.PathEscape(sessionID) + "/redeem"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, redeemURL, bytes.NewReader(redeemBody))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "IServ-reMarkable-Setup/"+appVersion)
	resp, err := client.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("Fly.io-Anmeldung konnte nicht abgeschlossen werden: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return "", false, fmt.Errorf("Fly.io-Anmeldesitzung ist abgelaufen")
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return "", true, fmt.Errorf("Fly.io-Code wurde nicht akzeptiert")
	}
	session, err := decodeFlySession(resp, http.StatusOK)
	if err != nil {
		return "", false, err
	}
	if session.AccessToken == "" || len(session.AccessToken) > 8192 || strings.ContainsAny(session.AccessToken, "\x00\r\n") {
		return "", false, fmt.Errorf("Fly.io-Anmeldung lieferte keine gültige Berechtigung")
	}
	return session.AccessToken, false, nil
}

func flyBrowserLogin(ctx context.Context, apiBase string, client *http.Client, opener func(string) error, manualCodes <-chan flyCodeSubmission) (string, error) {
	verifier, err := randomURLToken(32)
	if err != nil {
		return "", fmt.Errorf("Fly.io-Anmeldung konnte nicht vorbereitet werden: %w", err)
	}
	state, err := randomURLToken(16)
	if err != nil {
		return "", fmt.Errorf("Fly.io-Anmeldung konnte nicht vorbereitet werden: %w", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("lokaler Fly.io-Anmeldekanal konnte nicht geöffnet werden: %w", err)
	}
	defer listener.Close()

	code := make(chan string, 1)
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		callbackCode := r.URL.Query().Get("code")
		if r.URL.Query().Get("state") != state || callbackCode == "" || len(callbackCode) > 4096 || strings.ContainsAny(callbackCode, "\x00\r\n") {
			http.Error(w, "invalid callback", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><html><body><p>Angemeldet. Dieses Fenster kann geschlossen werden.</p><p>Signed in. You can close this window.</p></body></html>")
		once.Do(func() { code <- callbackCode })
	})
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       5 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	serveDone := make(chan struct{})
	go func() {
		_ = server.Serve(listener)
		close(serveDone)
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		<-serveDone
	}()

	challenge := sha256.Sum256([]byte(verifier))
	requestBody, err := json.Marshal(map[string]any{
		"signup":                false,
		"target":                "auth",
		"name":                  "IServ-reMarkable Setup",
		"code_challenge":        base64.RawURLEncoding.EncodeToString(challenge[:]),
		"code_challenge_method": "S256",
		"state":                 state,
		"redirect_port":         listener.Addr().(*net.TCPAddr).Port,
	})
	if err != nil {
		return "", err
	}
	startURL := strings.TrimRight(apiBase, "/") + "/api/v1/cli_sessions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, startURL, bytes.NewReader(requestBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "IServ-reMarkable-Setup/"+appVersion)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Fly.io-Anmeldung konnte nicht gestartet werden: %w", err)
	}
	session, err := decodeFlySession(resp, http.StatusCreated)
	if err != nil {
		return "", err
	}
	if !session.PKCE || !flySessionIDPattern.MatchString(session.ID) || !validFlyAuthURL(session.AuthURL) {
		return "", fmt.Errorf("Fly.io-Anmeldung lieferte ungültige Sitzungsdaten")
	}
	if err := opener(session.AuthURL); err != nil {
		return "", fmt.Errorf("Browser für die Fly.io-Anmeldung konnte nicht geöffnet werden: %w", err)
	}

	for {
		var completionCode string
		var manual *flyCodeSubmission
		select {
		case completionCode = <-code:
		case submission := <-manualCodes:
			completionCode = submission.code
			manual = &submission
		case <-ctx.Done():
			return "", fmt.Errorf("Fly.io-Anmeldung wurde nicht rechtzeitig abgeschlossen")
		}
		token, retryable, redeemErr := redeemFlySession(ctx, apiBase, client, session.ID, verifier, completionCode)
		if manual != nil {
			manual.result <- redeemErr
		}
		if redeemErr == nil {
			return token, nil
		}
		if !retryable {
			return "", redeemErr
		}
	}
}

func (a *App) setFlyAPIToken(token string) {
	a.flyAuthMu.Lock()
	a.flyAPIToken = token
	a.flyAuthMu.Unlock()
}

func (a *App) runFly(dir, fly string, args []string, stdin string, timeout time.Duration) (string, error) {
	configDir := filepath.Join(a.workDir, "tools", "fly-config")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", fmt.Errorf("lokale Fly.io-Konfiguration konnte nicht vorbereitet werden: %w", err)
	}
	if err := os.Chmod(configDir, 0700); err != nil {
		return "", fmt.Errorf("lokale Fly.io-Konfiguration konnte nicht geschützt werden: %w", err)
	}
	a.flyAuthMu.RLock()
	token := a.flyAPIToken
	a.flyAuthMu.RUnlock()
	env := map[string]string{
		"FLY_API_TOKEN":  "",
		"FLY_CONFIG_DIR": configDir,
	}
	if token != "" {
		env["FLY_API_TOKEN"] = token
	}
	return runWithTimeoutEnv(dir, fly, args, stdin, timeout, env)
}

func (a *App) ensureFlySession(parent context.Context, fly, messageDE, messageEN string) (string, error) {
	a.flyLoginMu.Lock()
	defer a.flyLoginMu.Unlock()
	if out, err := a.runFly("", fly, []string{"auth", "whoami"}, "", 30*time.Second); err == nil {
		return out, nil
	}
	a.logText(messageDE, messageEN)
	ctx, cancel := context.WithTimeout(parent, 8*time.Minute)
	defer cancel()
	broker := &flyCodeBroker{submissions: make(chan flyCodeSubmission), done: make(chan struct{})}
	a.flyCodeMu.Lock()
	a.flyCodeBroker = broker
	a.flyCodeMu.Unlock()
	defer func() {
		a.flyCodeMu.Lock()
		if a.flyCodeBroker == broker {
			a.flyCodeBroker = nil
			close(broker.done)
		}
		a.flyCodeMu.Unlock()
	}()
	token, err := flyBrowserLogin(ctx, flyAPIBaseURL, flyAuthHTTPClient(8*time.Minute), openBrowser, broker.submissions)
	if err != nil {
		return "", err
	}
	a.setFlyAPIToken(token)
	out, err := a.runFly("", fly, []string{"auth", "whoami"}, "", 30*time.Second)
	if err != nil {
		a.setFlyAPIToken("")
		return "", fmt.Errorf("Fly.io-Anmeldung konnte nicht bestätigt werden: %w", err)
	}
	return out, nil
}
