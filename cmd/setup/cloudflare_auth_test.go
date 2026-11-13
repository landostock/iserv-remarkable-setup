package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWranglerAccountDirectoryDoesNotLoadDeploymentConfig(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	worker := filepath.Join(app.workDir, "runtime", "worker")
	if err := os.MkdirAll(worker, 0700); err != nil {
		t.Fatal(err)
	}
	template := []byte(`{"name":"__WORKER_NAME__"}`)
	if err := os.WriteFile(filepath.Join(worker, "wrangler.jsonc"), template, 0600); err != nil {
		t.Fatal(err)
	}
	dir, err := app.wranglerAccountCommandDirectory()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "wrangler.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if json.Unmarshal(raw, &config) != nil || len(config) != 1 || config["name"] != "iserv-rm-account" {
		t.Fatalf("unexpected account configuration: %s", raw)
	}
	if raw, err := os.ReadFile(filepath.Join(worker, "wrangler.jsonc")); err != nil || string(raw) != string(template) {
		t.Fatal("deployment configuration was modified")
	}
	if runtime.GOOS != "windows" {
		for _, check := range []struct {
			path string
			mode os.FileMode
		}{
			{dir, 0700}, {filepath.Join(dir, "wrangler.jsonc"), 0600},
		} {
			if info, err := os.Stat(check.path); err != nil || info.Mode().Perm() != check.mode {
				t.Fatalf("account path has incorrect permissions: %s", check.path)
			}
		}
	}
}

func TestWranglerAccountDirectoryRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	app := NewApp(t.TempDir(), "test-token")
	tools := filepath.Join(app.workDir, "tools")
	if err := os.Mkdir(tools, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(tools, "cloudflare-account-command")); err != nil {
		t.Fatal(err)
	}
	if _, err := app.wranglerAccountCommandDirectory(); err == nil {
		t.Fatal("symlink was accepted")
	}
}

func TestParseCloudflareIdentityRequiresAuthenticatedAccount(t *testing.T) {
	valid := `{"loggedIn":true,"email":"person@example.test","accounts":[{"id":"0123456789abcdef0123456789abcdef","name":"School"}]}`
	if account, err := parseCloudflareIdentity(valid); err != nil || account != "School" {
		t.Fatalf("account=%q err=%v", account, err)
	}
	for _, raw := range []string{
		`{"loggedIn":false}`,
		`{"loggedIn":true,"accounts":[]}`,
		`{"loggedIn":true,"accounts":[{"id":"bad id","name":"School"}]}`,
		`You are not authenticated. Please run wrangler login.`,
	} {
		if _, err := parseCloudflareIdentity(raw); err == nil {
			t.Fatalf("invalid identity was accepted: %s", raw)
		}
	}
}

func TestWranglerEnvironmentIsIsolated(t *testing.T) {
	for _, key := range []string{"CLOUDFLARE_API_TOKEN", "CF_API_TOKEN", "CLOUDFLARE_ACCOUNT_ID", "WRANGLER_AUTH_URL"} {
		t.Setenv(key, "ambient-secret")
	}
	app := NewApp(t.TempDir(), "test-token")
	env, err := app.wranglerEnvironment(filepath.Join(app.workDir, "node", "bin", "node"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"CLOUDFLARE_API_TOKEN", "CF_API_TOKEN", "CLOUDFLARE_ACCOUNT_ID"} {
		if env[key] != "" {
			t.Fatalf("%s was inherited", key)
		}
	}
	if env["WRANGLER_AUTH_URL"] != "https://dash.cloudflare.com/oauth2/auth" || env["CLOUDFLARE_API_BASE_URL"] != "https://api.cloudflare.com/client/v4" {
		t.Fatalf("Cloudflare endpoints were not pinned to production: %#v", env)
	}
	if env["HOME"] == "" || env["XDG_CONFIG_HOME"] == "" || env["CLOUDFLARE_AUTH_USE_KEYRING"] != "false" {
		t.Fatalf("Cloudflare environment is not isolated: %#v", env)
	}
	authDir := filepath.Join(env["XDG_CONFIG_HOME"], ".wrangler", "config")
	info, err := os.Stat(authDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("Cloudflare auth directory was not prepared: info=%v err=%v", info, err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
		t.Fatalf("Cloudflare auth directory mode=%o want=700", info.Mode().Perm())
	}
	probe := filepath.Join(authDir, "write-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0600); err != nil {
		t.Fatalf("Cloudflare auth directory is not writable: %v", err)
	}
}

func TestWranglerEnvironmentIgnoresConflictingLegacyAuthPath(t *testing.T) {
	work := t.TempDir()
	legacy := filepath.Join(work, "tools", "cloudflare-home")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "config"), []byte("stale collision"), 0600); err != nil {
		t.Fatal(err)
	}
	app := NewApp(work, "test-token")
	env, err := app.wranglerEnvironment(filepath.Join(work, "node", "bin", "node"))
	if err != nil {
		t.Fatalf("legacy collision blocked the versioned auth layout: %v", err)
	}
	if !strings.Contains(env["HOME"], cloudflareAuthDirectory) {
		t.Fatalf("unexpected Cloudflare auth home: %s", env["HOME"])
	}
}

func TestWranglerEnvironmentRejectsConflictingCurrentAuthPath(t *testing.T) {
	work := t.TempDir()
	tools := filepath.Join(work, "tools")
	if err := os.MkdirAll(tools, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tools, cloudflareAuthDirectory), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	app := NewApp(work, "test-token")
	if _, err := app.wranglerEnvironment(filepath.Join(work, "node", "bin", "node")); err == nil {
		t.Fatal("unsafe Cloudflare auth path was accepted")
	}
}
