package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const flyMachinesAPIBase = "https://api.machines.dev"

var flyControlTokenValidationDelays = []time.Duration{
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	12 * time.Second,
	15 * time.Second,
	20 * time.Second,
}

// flyctl returns the complete Authorization header value. A FlyV1 credential
// can contain multiple comma-separated macaroons; every component is required.
var flyControlTokenPattern = regexp.MustCompile(`^FlyV1 [A-Za-z0-9._~+/=-]{8,}(,[A-Za-z0-9._~+/=-]{8,})*$`)

func validFlyControlToken(value string) bool {
	if len(value) > 32*1024 || !flyControlTokenPattern.MatchString(value) {
		return false
	}
	for _, component := range strings.Split(strings.TrimPrefix(value, "FlyV1 "), ",") {
		if len(component) > 4096 {
			return false
		}
	}
	return true
}

func validateFlyMachineControlToken(appName, machineID, token string) error {
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return validateFlyMachineControlTokenWithContext(ctx, client, flyMachinesAPIBase, appName, machineID, token, func(delay time.Duration) {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
		}
	})
}

func validateFlyMachineControlTokenWith(client *http.Client, apiBase, appName, machineID, token string, sleep func(time.Duration)) error {
	return validateFlyMachineControlTokenWithContext(context.Background(), client, apiBase, appName, machineID, token, sleep)
}

func validateFlyMachineControlTokenWithContext(ctx context.Context, client *http.Client, apiBase, appName, machineID, token string, sleep func(time.Duration)) error {
	if ctx == nil || client == nil || sleep == nil || !cloudNamePattern.MatchString(appName) || !flyMachinePattern.MatchString(machineID) || !validFlyControlToken(token) {
		return fmt.Errorf("ungültige Fly-Steuerberechtigung oder Zielkennung")
	}
	base, err := url.Parse(apiBase)
	if err != nil || base.Path != "" || base.RawQuery != "" || base.Fragment != "" || base.User != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" {
		return fmt.Errorf("ungültige Fly-API-Adresse")
	}
	endpoint := strings.TrimRight(base.String(), "/") + "/v1/apps/" + url.PathEscape(appName) + "/machines/" + url.PathEscape(machineID)
	var lastStatus int
	for attempt := 0; attempt <= len(flyControlTokenValidationDelays); attempt++ {
		if ctx.Err() != nil {
			return fmt.Errorf("Fly Machines API ist nicht rechtzeitig erreichbar")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "IServ-reMarkable-Setup/"+appVersion)
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			lastStatus = resp.StatusCode
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
				break
			}
		}
		if attempt < len(flyControlTokenValidationDelays) {
			sleep(flyControlTokenValidationDelays[attempt])
		}
	}
	if lastStatus != 0 {
		return fmt.Errorf("Fly Machines API meldet HTTP %d", lastStatus)
	}
	return fmt.Errorf("Fly Machines API ist nicht erreichbar")
}

type pendingFlyControlToken struct {
	appName          string
	token            string
	previousTokenIDs []string
	createdAt        time.Time
}

func (a *App) prepareFlyControlToken(runtimeDir, fly, appName string) (string, []string, bool, error) {
	a.flyControlTokenMu.Lock()
	defer a.flyControlTokenMu.Unlock()
	if pending := a.pendingFlyControl; pending != nil && pending.appName == appName && time.Since(pending.createdAt) < 10*time.Minute {
		return pending.token, append([]string(nil), pending.previousTokenIDs...), true, nil
	}
	a.pendingFlyControl = nil
	previousTokenIDs, err := listActiveFlyControlTokenIDsWith(a.runFly, runtimeDir, fly, appName)
	if err != nil {
		return "", nil, false, fmt.Errorf("Fly app token inventory: %w", err)
	}
	tokenOut, err := a.runFly(runtimeDir, fly, []string{"tokens", "create", "deploy", "-a", appName, "--name", flyControlTokenName, "-x", flyControlTokenCLIExpiry}, "", 2*time.Minute)
	if err != nil {
		return "", nil, false, fmt.Errorf("Fly app token: %w", err)
	}
	token := extractFlyToken(tokenOut)
	if token == "" {
		return "", nil, false, fmt.Errorf("Fly app token missing")
	}
	a.pendingFlyControl = &pendingFlyControlToken{
		appName:          appName,
		token:            token,
		previousTokenIDs: append([]string(nil), previousTokenIDs...),
		createdAt:        time.Now(),
	}
	return token, previousTokenIDs, false, nil
}

func (a *App) clearPendingFlyControlToken(appName, token string) {
	a.flyControlTokenMu.Lock()
	defer a.flyControlTokenMu.Unlock()
	if pending := a.pendingFlyControl; pending != nil && pending.appName == appName && pending.token == token {
		a.pendingFlyControl = nil
	}
}

func (a *App) clearAllPendingFlyControlTokens() {
	a.flyControlTokenMu.Lock()
	a.pendingFlyControl = nil
	a.flyControlTokenMu.Unlock()
}
