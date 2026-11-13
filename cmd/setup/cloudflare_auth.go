package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var cloudflareAccountIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

const cloudflareAuthDirectory = "cloudflare-home-v2"

type cloudflareIdentityResult struct {
	LoggedIn bool   `json:"loggedIn"`
	Email    string `json:"email"`
	Accounts []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"accounts"`
}

func parseCloudflareIdentity(raw string) (string, error) {
	var identity cloudflareIdentityResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &identity); err != nil {
		return "", fmt.Errorf("Cloudflare-Anmeldung lieferte eine ungültige Antwort")
	}
	if !identity.LoggedIn || len(identity.Accounts) == 0 {
		return "", fmt.Errorf("Cloudflare ist nicht angemeldet")
	}
	for _, account := range identity.Accounts {
		if !cloudflareAccountIDPattern.MatchString(account.ID) {
			continue
		}
		if name := strings.TrimSpace(account.Name); name != "" {
			return name, nil
		}
		return account.ID, nil
	}
	return "", fmt.Errorf("Cloudflare-Anmeldung enthält kein gültiges Konto")
}

func (a *App) cloudflareIdentity() (string, error) {
	out, err := a.runWrangler([]string{"whoami", "--json"}, "", 45*time.Second)
	if err != nil {
		return "", err
	}
	return parseCloudflareIdentity(out)
}

// Account commands must work before the deployment template has been filled in,
// and must not load a previous installation's bindings or local environment files.
func (a *App) wranglerAccountCommandDirectory() (string, error) {
	tools := filepath.Join(a.workDir, "tools")
	dir := filepath.Join(tools, "cloudflare-account-command")
	for _, path := range []string{tools, dir} {
		if err := ensurePrivateCloudflareDirectory(path); err != nil {
			return "", err
		}
	}
	if err := atomicWritePrivate(filepath.Join(dir, "wrangler.jsonc"), []byte("{\"name\":\"iserv-rm-account\"}\n")); err != nil {
		return "", fmt.Errorf("Cloudflare account configuration: %w", err)
	}
	return dir, nil
}

func (a *App) wranglerEnvironment(node string) (map[string]string, error) {
	toolsRoot := filepath.Join(a.workDir, "tools")
	home := filepath.Join(toolsRoot, cloudflareAuthDirectory)
	config := filepath.Join(home, "config")
	cache := filepath.Join(home, "cache")
	authConfig := filepath.Join(config, ".wrangler", "config")
	for _, dir := range []string{
		toolsRoot,
		home,
		config,
		cache,
		filepath.Join(config, ".wrangler"),
		authConfig,
		filepath.Join(config, "wrangler"),
		filepath.Join(config, "wrangler", "logs"),
	} {
		if err := ensurePrivateCloudflareDirectory(dir); err != nil {
			return nil, err
		}
	}
	probe, err := os.CreateTemp(authConfig, ".setup-write-test-")
	if err != nil {
		return nil, fmt.Errorf("lokale Cloudflare-Anmeldung ist nicht beschreibbar: %w", err)
	}
	probeName := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(probeName)
		return nil, fmt.Errorf("lokale Cloudflare-Schreibprüfung konnte nicht abgeschlossen werden: %w", err)
	}
	if err := os.Remove(probeName); err != nil {
		return nil, fmt.Errorf("lokale Cloudflare-Schreibprüfung konnte nicht bereinigt werden: %w", err)
	}
	env := managedNodeEnvironment(node)
	for key, value := range map[string]string{
		"HOME":                            home,
		"USERPROFILE":                     home,
		"APPDATA":                         config,
		"LOCALAPPDATA":                    cache,
		"XDG_CONFIG_HOME":                 config,
		"XDG_CACHE_HOME":                  cache,
		"CLOUDFLARE_AUTH_USE_KEYRING":     "false",
		"CLOUDFLARE_API_TOKEN":            "",
		"CLOUDFLARE_API_KEY":              "",
		"CLOUDFLARE_EMAIL":                "",
		"CLOUDFLARE_ACCOUNT_ID":           "",
		"CF_API_TOKEN":                    "",
		"CF_API_KEY":                      "",
		"CF_EMAIL":                        "",
		"CF_ACCOUNT_ID":                   "",
		"WRANGLER_CF_AUTHORIZATION_TOKEN": "",
		"WRANGLER_API_ENVIRONMENT":        "production",
		"WRANGLER_AUTH_DOMAIN":            "dash.cloudflare.com",
		"WRANGLER_AUTH_URL":               "https://dash.cloudflare.com/oauth2/auth",
		"WRANGLER_TOKEN_URL":              "https://dash.cloudflare.com/oauth2/token",
		"WRANGLER_REVOKE_URL":             "https://dash.cloudflare.com/oauth2/revoke",
		"WRANGLER_CLIENT_ID":              "54d11594-84e4-41aa-b438-e81b8fa78ee7",
		"CLOUDFLARE_API_BASE_URL":         "https://api.cloudflare.com/client/v4",
		"CLOUDFLARE_BASE_URL":             "https://api.cloudflare.com/client/v4",
	} {
		env[key] = value
	}
	return env, nil
}

func ensurePrivateCloudflareDirectory(path string) error {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("lokaler Cloudflare-Pfad ist kein sicherer Ordner: %s", path)
		}
	case os.IsNotExist(err):
		if err := os.Mkdir(path, 0700); err != nil {
			return fmt.Errorf("lokale Cloudflare-Konfiguration konnte nicht vorbereitet werden: %w", err)
		}
	default:
		return fmt.Errorf("lokaler Cloudflare-Pfad konnte nicht geprüft werden: %w", err)
	}
	if err := os.Chmod(path, 0700); err != nil {
		return fmt.Errorf("lokale Cloudflare-Konfiguration konnte nicht geschützt werden: %w", err)
	}
	return nil
}
