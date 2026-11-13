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

type dashboardState struct {
	Settings   map[string]any `json:"settings"`
	Activation struct {
		RealRunVerified bool `json:"real_run_verified"`
		Enabled         bool `json:"enabled"`
	} `json:"activation"`
	Status struct {
		MachineState string              `json:"machine_state"`
		LastReport   *dashboardRunReport `json:"last_report"`
	} `json:"status"`
}

type dashboardRunReport struct {
	FinishedAt string             `json:"finished_at"`
	OK         bool               `json:"ok"`
	DryRun     bool               `json:"dry_run"`
	Outcome    string             `json:"outcome"`
	Reason     string             `json:"reason"`
	Message    string             `json:"message"`
	Error      string             `json:"error"`
	Stats      *verificationStats `json:"stats"`
}

func dryRunSettingsConfirmed(settings map[string]any) bool {
	sync, ok := settings["sync"].(map[string]any)
	enabled, enabledOK := settings["enabled"].(bool)
	dryRun, dryRunOK := sync["dry_run"].(bool)
	return ok && enabledOK && !enabled && dryRunOK && dryRun
}

func waitForDryRunSettings(read func() (dashboardState, error), pause func(time.Duration)) (dashboardState, error) {
	var current dashboardState
	for _, delay := range []time.Duration{0, 3, 8, 15, 30} {
		if delay > 0 {
			pause(delay * time.Second)
		}
		var err error
		current, err = read()
		if err != nil {
			return current, err
		}
		if dryRunSettingsConfirmed(current.Settings) {
			return current, nil
		}
	}
	return current, fmt.Errorf("Das Dashboard hat Testmodus EIN und Automatik AUS noch nicht bestätigt")
}

func (r *dashboardRunReport) confirmsDryRun() bool {
	return r.OK && r.DryRun && r.Outcome == "success" && r.Reason == "dry_run" &&
		r.Error == "" && r.Stats != nil && r.Stats.Uploaded == 0 && r.Stats.Failed == 0
}

func (r *dashboardRunReport) result() *verificationResult {
	result := &verificationResult{FinishedAt: r.FinishedAt, Outcome: r.Outcome, Reason: r.Reason, Message: r.Message}
	if r.Stats != nil {
		result.Stats = *r.Stats
	}
	return result
}

func newerRunReport(report *dashboardRunReport, previous string) bool {
	if report == nil {
		return false
	}
	finished, err := time.Parse(time.RFC3339Nano, report.FinishedAt)
	if err != nil {
		return false
	}
	baseline, err := time.Parse(time.RFC3339Nano, previous)
	return err != nil || finished.After(baseline)
}

func reportFollowsStart(report *dashboardRunReport, started time.Time) bool {
	if report == nil {
		return false
	}
	finished, err := time.Parse(time.RFC3339Nano, report.FinishedAt)
	return err == nil && !finished.Before(started.Truncate(time.Second))
}

type adminRequestFunc func(base, password, method, requestPath string, body []byte) ([]byte, int, error)

func adminRequest(base, password, method, requestPath string, body []byte) ([]byte, int, error) {
	normalized, err := normalizeDashboardURL(base)
	if err != nil || normalized != base {
		return nil, 0, fmt.Errorf("ungültige Dashboard-Adresse")
	}
	if !strings.HasPrefix(requestPath, "/") || strings.ContainsAny(requestPath, "\r\n\x00") {
		return nil, 0, fmt.Errorf("ungültiger Dashboard-Pfad")
	}
	req, err := http.NewRequest(method, normalized+requestPath, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth("admin", password)
	req.Header.Set("Origin", normalized)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	const maxDashboardResponse = 2 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxDashboardResponse+1))
	if err != nil {
		return nil, 0, err
	}
	if len(raw) > maxDashboardResponse {
		return nil, 0, fmt.Errorf("Dashboard-Antwort ist unerwartet groß")
	}
	return raw, resp.StatusCode, nil
}

func getDashboardState(base, password string) (dashboardState, error) {
	var d dashboardState
	raw, status, err := adminRequest(base, password, http.MethodGet, "/api/admin/settings", nil)
	if err != nil {
		return d, err
	}
	if status < 200 || status >= 300 {
		return d, fmt.Errorf("Dashboard meldet HTTP %d", status)
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, err
	}
	return d, nil
}

func (a *App) verifyDryRun(base, password string) (*verificationResult, error) {
	before, err := waitForDryRunSettings(func() (dashboardState, error) {
		return getDashboardState(base, password)
	}, time.Sleep)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", a.text("Sichere Startkonfiguration konnte nicht gelesen werden", "Could not read the safe starting configuration"), err)
	}
	a.logText("Sicherer Testlauf wird gestartet – dabei wird nichts hochgeladen …", "Starting the safe test run without uploads …")
	previous := ""
	if before.Status.LastReport != nil {
		previous = before.Status.LastReport.FinishedAt
	}
	started, err := a.startRunWithRetry(base, password, adminRequest, time.Sleep)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(20 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		d, err := getDashboardState(base, password)
		if err != nil {
			continue
		}
		r := d.Status.LastReport
		if !newerRunReport(r, previous) || !reportFollowsStart(r, started) {
			continue
		}
		result := r.result()
		if !r.confirmsDryRun() {
			return result, fmt.Errorf("%s: %s", a.text("Sicherer Testlauf fehlgeschlagen", "Safe test run failed"), a.dryRunFailureMessage(r))
		}
		a.logText("Sicherer Testlauf erfolgreich: %d geprüft, %d würden synchronisiert.", "Safe test run passed: %d checked, %d would sync.", r.Stats.Checked, r.Stats.WouldUpload)
		return result, nil
	}
	return nil, fmt.Errorf("Testlauf hat innerhalb von 20 Minuten keinen Abschluss gemeldet")
}

func (a *App) dryRunFailureMessage(report *dashboardRunReport) string {
	if report.Outcome == "failed" {
		switch report.Reason {
		case "remarkable_rate_limit", "cooldown":
			return a.text("reMarkable begrenzt die Anfragen. Warte die im Dashboard angezeigte Sperrzeit ab und setze dann fort.", "reMarkable is limiting requests. Wait until the cooldown shown in the dashboard ends, then continue.")
		case "remarkable_auth":
			return a.text("Die Fly-Machine konnte sich bei reMarkable nicht anmelden. Verbinde reMarkable erneut und setze danach die Installation fort.", "The Fly Machine could not sign in to reMarkable. Reconnect reMarkable, then continue the installation.")
		case "remarkable_timeout":
			return a.text("Das erstmalige Lesen der reMarkable Cloud hat die Zeitgrenze von acht Minuten erreicht. Es wurde nichts hochgeladen. Prüfe die Verbindung und setze danach fort. Erneutes Anmelden ist dafür nicht erforderlich.", "The initial reMarkable Cloud read reached its eight-minute time limit. Nothing was uploaded. Check the connection and continue. Signing in again is not required for this timeout.")
		case "remarkable_network", "remarkable_provider", "remarkable_protocol", "remarkable_generation":
			return a.text("Die Fly-Machine konnte den reMarkable-Ordnerbaum nicht lesen. Warte kurz und setze fort. Falls der Fehler bleibt, erstelle einen Diagnosebericht.", "The Fly Machine could not read the reMarkable folder tree. Wait a moment and continue. Create a diagnostic report if the problem persists.")
		case "iserv_auth", "iserv_source", "iserv_certificate", "iserv_storage", "iserv_network", "iserv_provider":
			return a.firstRealFailureMessage(report.Reason, verificationStats{})
		}
	}
	return a.text("Der Lauf hat keinen vollständigen Test ohne Uploads bestätigt. Prüfe das Ergebnis im Dashboard oder erstelle einen Diagnosebericht.", "The run did not confirm a complete test without uploads. Check the dashboard result or create a diagnostic report.")
}

func dashboardErrorCode(raw []byte) string {
	var response struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return ""
	}
	return response.Code
}

func (a *App) startRunWithRetry(base, password string, request adminRequestFunc, pause func(time.Duration)) (time.Time, error) {
	if request == nil || pause == nil {
		return time.Time{}, fmt.Errorf("Lauf konnte nicht sicher gestartet werden")
	}
	delays := []time.Duration{3, 5, 8, 12, 20, 30, 30}
	for attempt := 0; ; attempt++ {
		raw, status, err := request(base, password, http.MethodPost, "/api/admin/run", []byte(`{}`))
		if err != nil {
			return time.Time{}, err
		}
		code := dashboardErrorCode(raw)
		if status >= 200 && status < 300 {
			var response struct {
				Result struct {
					Started     *bool  `json:"started"`
					State       string `json:"state"`
					RequestedAt string `json:"requested_at"`
				} `json:"result"`
			}
			if json.Unmarshal(raw, &response) != nil || response.Result.Started == nil {
				return time.Time{}, fmt.Errorf("Das Dashboard hat den Laufstart nicht bestätigt")
			}
			result := response.Result
			if *result.Started {
				started, err := time.Parse(time.RFC3339Nano, result.RequestedAt)
				if err != nil {
					return time.Time{}, fmt.Errorf("Das Dashboard hat einen ungültigen Startzeitpunkt gemeldet")
				}
				return started, nil
			}
			if result.State == "started" || result.State == "starting" || result.State == "stopping" {
				code = "fly_machine_transition"
			}
		}
		if (code != "fly_machine_transition" && code != "status_storage_busy") || attempt >= len(delays) {
			return time.Time{}, a.runStartError(status, raw)
		}
		delay := delays[attempt] * time.Second
		a.logText(
			"Die Sync-Machine wird für einen neuen Lauf vorbereitet; neuer Startversuch in %d Sekunden …",
			"The sync Machine is being prepared for a new run; retrying in %d seconds …",
			int(delay/time.Second),
		)
		pause(delay)
	}
}

func (a *App) runStartError(status int, raw []byte) error {
	_, detail := commandFailureOutput(strings.TrimSpace(string(raw)))
	if detail == "" {
		detail = "[empty response]"
	}
	a.logText("Technisches Detail zum Laufstart (HTTP %d): %s", "Technical run start detail (HTTP %d): %s", status, detail)
	code := dashboardErrorCode(raw)
	if code == "fly_auth_rejected" {
		return fmt.Errorf("%s", a.text(
			"Die Fly-Steuerberechtigung wurde abgelehnt. Der Assistent erneuert sie sicher, wenn du auf „Installation fortsetzen“ klickst; eine Neuinstallation ist nicht erforderlich.",
			"The Fly control credential was rejected. The assistant will safely renew it when you click “Continue installation”; reinstallation is not required.",
		))
	}
	if code == "fly_machine_transition" {
		return fmt.Errorf("%s", a.text(
			"Fly.io konnte die Aktualisierung der Sync-Machine nicht rechtzeitig abschließen. Die Installation bleibt sicher angehalten und kann ohne Neuinstallation fortgesetzt werden.",
			"Fly.io could not finish updating the sync Machine in time. Installation remains safely paused and can continue without reinstallation.",
		))
	}
	return fmt.Errorf("%s", a.text(
		"Der Lauf konnte noch nicht gestartet werden. Prüfe die Internetverbindung und wiederhole den Start beziehungsweise klicke auf „Installation fortsetzen“.",
		"The run could not be started yet. Check the internet connection and retry the run or click “Continue installation”.",
	))
}
