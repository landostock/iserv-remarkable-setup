package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// reMarkable moved browser/device authentication away from my.remarkable.com.
// Keep the auth host in one place so we can update it without touching the rest
// of the setup flow if the unofficial cloud API changes again.
const remarkableAuthHost = "https://webapp-prod.cloud.remarkable.engineering"

func registerRemarkable(code string) (string, string, error) {
	// Authentication responses contain bearer credentials. Never follow redirects:
	// even a same-site redirect is unnecessary here and forwarding credentials to a
	// different endpoint would increase the blast radius of a compromised redirect.
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return registerRemarkableAt(code, remarkableAuthHost, client)
}

func readRemarkableToken(r io.Reader, limit int64, label string) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > limit {
		return "", fmt.Errorf("reMarkable hat ein unerwartet großes %s geliefert", label)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("reMarkable hat kein %s geliefert", label)
	}
	if strings.ContainsAny(token, "\r\n\x00") {
		return "", fmt.Errorf("reMarkable hat ein ungültiges %s geliefert", label)
	}
	return token, nil
}

func registerRemarkableAt(code, authHost string, client *http.Client) (string, string, error) {
	code = strings.TrimSpace(code)
	if len(code) != 8 {
		return "", "", fmt.Errorf("der reMarkable-Code muss aus genau 8 Zeichen bestehen; erzeuge bitte einen neuen Code")
	}
	for _, ch := range code {
		if !((ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')) {
			return "", "", fmt.Errorf("der reMarkable-Code enthält ungültige Zeichen; erzeuge bitte einen neuen Code")
		}
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	authHost = strings.TrimRight(strings.TrimSpace(authHost), "/")
	if authHost == "" {
		return "", "", fmt.Errorf("reMarkable-Anmeldedienst ist nicht konfiguriert")
	}

	id, err := randomUUID()
	if err != nil {
		return "", "", err
	}
	body, err := json.Marshal(map[string]string{
		"code":       code,
		"deviceDesc": "browser-chrome",
		"deviceID":   id,
	})
	if err != nil {
		return "", "", err
	}

	deviceURL := authHost + "/token/json/2/device/new"
	req, err := http.NewRequest("POST", deviceURL, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	// Current browser registration expects the empty Bearer header. This mirrors
	// the maintained rmapi-js implementation and avoids the HTTP 405 returned by
	// the legacy my.remarkable.com endpoint.
	req.Header.Set("Authorization", "Bearer")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("reMarkable-Anmeldedienst nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return "", "", fmt.Errorf("reMarkable hat den Code nicht akzeptiert (HTTP %d). Erzeuge bitte einen neuen einmaligen Code und versuche es erneut", resp.StatusCode)
		}
		return "", "", fmt.Errorf("reMarkable-Gerätefreigabe fehlgeschlagen (HTTP %d). Bitte später erneut versuchen", resp.StatusCode)
	}

	device, err := readRemarkableToken(resp.Body, 32<<10, "Geräte-Token")
	if err != nil {
		return "", "", err
	}

	userURL := authHost + "/token/json/2/user/new"
	userReq, err := http.NewRequest("POST", userURL, nil)
	if err != nil {
		return "", "", err
	}
	userReq.Header.Set("Authorization", "Bearer "+device)
	userResp, err := client.Do(userReq)
	if err != nil {
		return "", "", fmt.Errorf("reMarkable-Benutzeranmeldung nicht erreichbar: %w", err)
	}
	defer userResp.Body.Close()
	if userResp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("reMarkable-Benutzeranmeldung fehlgeschlagen (HTTP %d). Erzeuge bitte einen neuen Code und versuche es erneut", userResp.StatusCode)
	}

	user, err := readRemarkableToken(userResp.Body, 64<<10, "Benutzer-Token")
	if err != nil {
		return "", "", err
	}
	return device, user, nil
}

func rmapiYAML(device, user string) string {
	return "devicetoken: " + device + "\n" + "usertoken: " + user + "\n"
}

func remarkableErrorForLang(err error, lang string) string {
	if err == nil || lang != "en" {
		if err == nil {
			return ""
		}
		return err.Error()
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "8 zeichen"):
		return "The reMarkable code must contain exactly 8 characters. Create a new code and try again."
	case strings.Contains(s, "ungültige zeichen"):
		return "The reMarkable code contains invalid characters. Create a new code and try again."
	case strings.Contains(s, "nicht akzeptiert"):
		return "reMarkable did not accept the code. Create a new one-time code and try again."
	case strings.Contains(s, "nicht erreichbar"):
		return "The reMarkable sign-in service could not be reached. Check your internet connection and try again."
	default:
		return "reMarkable connection failed. Please create a new one-time code and try again."
	}
}
