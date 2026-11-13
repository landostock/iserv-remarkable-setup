package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	remarkableFolderTimeout     = 8 * time.Minute
	remarkableFolderConcurrency = "2"
)

var (
	remarkableFolderAttemptConcurrency = []string{remarkableFolderConcurrency, "1", "1"}
	remarkableFolderRetryDelays        = []time.Duration{750 * time.Millisecond, 1500 * time.Millisecond}
)

func normalizeRemarkablePath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" || p == "." || p == "//" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = path.Clean(p)
	if p == "." || p == "" {
		return "/"
	}
	return p
}

func parseRemarkableFolderFindOutput(out string) []string {
	seen := map[string]bool{"/": true}
	result := []string{"/"}
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || !strings.HasPrefix(line, "/") || !strings.HasSuffix(line, "/") {
			continue
		}
		p := normalizeRemarkablePath(strings.TrimSuffix(line, "/"))
		if p == "/trash" || strings.HasPrefix(p, "/trash/") {
			continue
		}
		if !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i]) < strings.ToLower(result[j]) })
	return result
}

func remarkableChildren(folders []string, current string) []string {
	current = normalizeRemarkablePath(current)
	children := make([]string, 0)
	seen := map[string]bool{}
	for _, raw := range folders {
		p := normalizeRemarkablePath(raw)
		if p == "/" || p == current {
			continue
		}
		parent := path.Dir(p)
		if parent == "." {
			parent = "/"
		}
		if parent == current && !seen[p] {
			seen[p] = true
			children = append(children, p)
		}
	}
	sort.Slice(children, func(i, j int) bool {
		return strings.ToLower(path.Base(children[i])) < strings.ToLower(path.Base(children[j]))
	})
	return children
}

func (a *App) loadRemarkableFolders(device, user string) (folders []string, retErr error) {
	helper, err := bundledRemarkablePath()
	if err != nil {
		return nil, errors.New(a.text("Der integrierte reMarkable-Helfer fehlt in diesem Paket", "The bundled reMarkable helper is missing from this package"))
	}
	helperEnv, cleanup, err := newRemarkableHelperEnvironment(a.workDir, device, user)
	if err != nil {
		return nil, fmt.Errorf(a.text("Der private reMarkable-Arbeitsbereich konnte nicht vorbereitet werden: %v", "The private reMarkable workspace could not be prepared: %v"), err)
	}
	defer func() {
		if cleanupErr := cleanup(); cleanupErr != nil {
			folders = nil
			retErr = errors.New(a.text("Der private reMarkable-Arbeitsbereich konnte nicht sicher entfernt werden. Setup wurde vorsichtshalber angehalten", "The private reMarkable workspace could not be removed securely. Setup stopped as a precaution"))
		}
	}()

	a.logText("Lade reMarkable-Ordnerbaum für die grafische Zuordnung …", "Loading the reMarkable folder tree …")
	base := strings.ToLower(path.Base(strings.ReplaceAll(helper, "\\", "/")))
	var helperArgs []string
	if strings.HasPrefix(base, "remarquee") {
		// Development fallback for existing preview environments.
		tmpPath := filepath.Join(helperEnv["HOME"], "rmapi-preview.conf")
		tmp, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		if _, err := tmp.WriteString(rmapiYAML(device, user)); err != nil {
			_ = tmp.Close()
			return nil, err
		}
		if err := tmp.Close(); err != nil {
			return nil, err
		}
		helperEnv["RMAPI_CONFIG"] = tmpPath
		helperEnv["RMAPI_FILE"] = tmpPath
		helperArgs = []string{"cloud", "find", "/", "--compact", "--non-interactive"}
	}
	out, err := runRemarkableFolderAttempts(remarkableFolderTimeout, helperEnv, func(timeout time.Duration, env map[string]string) (string, error) {
		return runWithTimeoutEnv("", helper, helperArgs, "", timeout, env)
	}, time.Sleep)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New(a.text("Das Laden der reMarkable-Ordner dauerte länger als acht Minuten. Prüfe die Internetverbindung und versuche es mit „Neu laden“ erneut", "Loading the reMarkable folders took longer than eight minutes. Check the internet connection and try Reload again"))
		}
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "429") || strings.Contains(low, "too many requests") {
			return nil, errors.New(a.text("reMarkable begrenzt Anfragen gerade vorübergehend. Warte bitte etwas und lade den Ordnerbaum dann erneut", "reMarkable is temporarily rate-limiting requests. Wait a little and reload the folder tree."))
		}
		if isTransientRemarkableFolderError(err) {
			return nil, errors.New(a.text("Die Verbindung zu reMarkable wurde mehrfach unterbrochen. Prüfe die Internetverbindung und wähle „Neu laden“", "The connection to reMarkable was interrupted repeatedly. Check the internet connection and choose Reload"))
		}
		return nil, fmt.Errorf(a.text("reMarkable-Ordner konnten nicht geladen werden: %v", "reMarkable folders could not be loaded: %v"), err)
	}
	folders = parseRemarkableFolderFindOutput(out)
	a.logText("reMarkable-Ordnerbaum geladen: %d Unterordner.", "reMarkable folder tree loaded: %d subfolders.", len(folders)-1)
	return folders, nil
}

type remarkableFolderRunner func(time.Duration, map[string]string) (string, error)

func runRemarkableFolderAttempts(timeout time.Duration, baseEnv map[string]string, run remarkableFolderRunner, pause func(time.Duration)) (string, error) {
	deadline := time.Now().Add(timeout)
	var lastOut string
	var lastErr error
	for attempt, concurrency := range remarkableFolderAttemptConcurrency {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return lastOut, context.DeadlineExceeded
		}
		env := make(map[string]string, len(baseEnv)+1)
		for key, value := range baseEnv {
			env[key] = value
		}
		env["RMAPI_CONCURRENT"] = concurrency
		lastOut, lastErr = run(remaining, env)
		if lastErr == nil || errors.Is(lastErr, context.DeadlineExceeded) || !isTransientRemarkableFolderError(lastErr) {
			return lastOut, lastErr
		}
		if attempt >= len(remarkableFolderAttemptConcurrency)-1 {
			break
		}
		delay := remarkableFolderRetryDelays[attempt]
		if time.Until(deadline) <= delay {
			return lastOut, context.DeadlineExceeded
		}
		pause(delay)
	}
	return lastOut, lastErr
}

func isTransientRemarkableFolderError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{
		"400 bad request",
		"401 unauthorized",
		"403 forbidden",
		"404 not found",
		"409 conflict",
		"412 wrong generation",
		"429 too many requests",
		"request failed with status 400",
		"request failed with status 401",
		"request failed with status 403",
		"request failed with status 404",
		"request failed with status 409",
		"request failed with status 412",
		"request failed with status 429",
	} {
		if strings.Contains(text, marker) {
			return false
		}
	}
	for _, marker := range []string{
		"connection reset by peer",
		"connection refused",
		"unexpected eof",
		"broken pipe",
		"tls handshake timeout",
		"i/o timeout",
		"network is unreachable",
		"no route to host",
		"temporary failure in name resolution",
		"server misbehaving",
		"request failed with status 500",
		"request failed with status 502",
		"request failed with status 503",
		"request failed with status 504",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func newRemarkableHelperEnvironment(workDir, device, user string) (map[string]string, func() error, error) {
	root, err := os.MkdirTemp(workDir, "remarkable-helper-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() error { return os.RemoveAll(root) }
	if err := os.Chmod(root, 0700); err != nil {
		_ = cleanup()
		return nil, nil, err
	}
	cache := filepath.Join(root, "cache")
	config := filepath.Join(root, "config")
	localAppData := filepath.Join(root, "local-app-data")
	appData := filepath.Join(root, "app-data")
	for _, dir := range []string{cache, config, localAppData, appData} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			_ = cleanup()
			return nil, nil, err
		}
	}
	return map[string]string{
		"HOME":                  root,
		"USERPROFILE":           root,
		"XDG_CACHE_HOME":        cache,
		"XDG_CONFIG_HOME":       config,
		"LOCALAPPDATA":          localAppData,
		"APPDATA":               appData,
		"ISERV_RM_DEVICE_TOKEN": device,
		"ISERV_RM_USER_TOKEN":   user,
		"RMAPI_CONCURRENT":      remarkableFolderConcurrency,
	}, cleanup, nil
}
