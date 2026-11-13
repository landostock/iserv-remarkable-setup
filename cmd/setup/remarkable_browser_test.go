package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRemarkableFolderAttemptsRetryTransientFailuresWithLessConcurrency(t *testing.T) {
	baseEnv := map[string]string{"UNCHANGED": "value", "RMAPI_CONCURRENT": "unexpected"}
	var gotConcurrency []string
	var gotPauses []time.Duration
	out, err := runRemarkableFolderAttempts(time.Minute, baseEnv, func(timeout time.Duration, env map[string]string) (string, error) {
		if timeout <= 0 || timeout > time.Minute {
			t.Fatalf("invalid remaining timeout %v", timeout)
		}
		if env["UNCHANGED"] != "value" {
			t.Fatalf("base environment was not preserved: %#v", env)
		}
		gotConcurrency = append(gotConcurrency, env["RMAPI_CONCURRENT"])
		if len(gotConcurrency) < 3 {
			return "transient output", errors.New("read: connection reset by peer")
		}
		return "/\n/School/\n", nil
	}, func(delay time.Duration) {
		gotPauses = append(gotPauses, delay)
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "/\n/School/\n" {
		t.Fatalf("output=%q", out)
	}
	if strings.Join(gotConcurrency, ",") != "2,1,1" {
		t.Fatalf("concurrency sequence=%v", gotConcurrency)
	}
	if len(gotPauses) != 2 || gotPauses[0] != 750*time.Millisecond || gotPauses[1] != 1500*time.Millisecond {
		t.Fatalf("retry pauses=%v", gotPauses)
	}
	if baseEnv["RMAPI_CONCURRENT"] != "unexpected" {
		t.Fatalf("base environment was mutated: %#v", baseEnv)
	}
}

func TestRemarkableFolderAttemptsDoNotRetryPermanentOrDeadlineErrors(t *testing.T) {
	for name, failure := range map[string]error{
		"authentication": errors.New("401 Unauthorized"),
		"deadline":       context.DeadlineExceeded,
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			_, err := runRemarkableFolderAttempts(time.Minute, nil, func(time.Duration, map[string]string) (string, error) {
				calls++
				return "", failure
			}, func(time.Duration) { t.Fatal("unexpected retry pause") })
			if !errors.Is(err, failure) && err.Error() != failure.Error() {
				t.Fatalf("error=%v want %v", err, failure)
			}
			if calls != 1 {
				t.Fatalf("calls=%d want 1", calls)
			}
		})
	}
}

func TestTransientRemarkableFolderErrors(t *testing.T) {
	for _, message := range []string{
		"read: connection reset by peer",
		"unexpected EOF",
		"request failed with status 503",
	} {
		if !isTransientRemarkableFolderError(errors.New(message)) {
			t.Errorf("did not recognize transient error %q", message)
		}
	}
	for _, message := range []string{
		"401 Unauthorized",
		"request failed with status 400",
		"too many requests",
		"401 Unauthorized after connection reset by peer",
	} {
		if isTransientRemarkableFolderError(errors.New(message)) {
			t.Errorf("incorrectly recognized permanent error %q", message)
		}
	}
}

func TestRemarkableHelperEnvironmentIsPrivateIsolatedAndTemporary(t *testing.T) {
	workDir := t.TempDir()
	env, cleanup, err := newRemarkableHelperEnvironment(workDir, "device-secret", "user-secret")
	if err != nil {
		t.Fatal(err)
	}
	root := env["HOME"]
	if root == "" || !strings.HasPrefix(root, workDir+string(os.PathSeparator)) {
		_ = cleanup()
		t.Fatalf("helper home %q is not isolated below work dir %q", root, workDir)
	}
	if env["ISERV_RM_DEVICE_TOKEN"] != "device-secret" || env["ISERV_RM_USER_TOKEN"] != "user-secret" {
		_ = cleanup()
		t.Fatal("helper credentials were not scoped to its environment")
	}
	if env["RMAPI_CONCURRENT"] != remarkableFolderConcurrency {
		_ = cleanup()
		t.Fatalf("concurrency=%q want %q", env["RMAPI_CONCURRENT"], remarkableFolderConcurrency)
	}
	for key, want := range map[string]string{
		"XDG_CACHE_HOME":  filepath.Join(root, "cache"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"LOCALAPPDATA":    filepath.Join(root, "local-app-data"),
		"APPDATA":         filepath.Join(root, "app-data"),
	} {
		if env[key] != want {
			_ = cleanup()
			t.Fatalf("%s=%q want %q", key, env[key], want)
		}
		info, statErr := os.Stat(want)
		if statErr != nil || !info.IsDir() {
			_ = cleanup()
			t.Fatalf("%s is not a prepared directory: %v", key, statErr)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
			_ = cleanup()
			t.Fatalf("%s permissions=%#o want private", key, info.Mode().Perm())
		}
	}
	if env["USERPROFILE"] != root {
		_ = cleanup()
		t.Fatalf("USERPROFILE=%q want %q", env["USERPROFILE"], root)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("temporary helper home remains after cleanup: %v", err)
	}
}

func TestRemarkableFolderUISeparatesConnectionLoadingAndSelection(t *testing.T) {
	raw, err := embedded.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	for _, marker := range []string{
		`selectedRM="",rmLoading=false`,
		`if(rmLoading)return`,
		`if(connectedNow)loadRM("/").catch(()=>{})`,
		`function clearSelected(){selectedDav="";selectedRM=""`,
		"Das erste Laden kann bei einer großen Bibliothek mehrere Minuten dauern.",
	} {
		if !strings.Contains(page, marker) {
			t.Errorf("setup UI is missing reMarkable loading safeguard %q", marker)
		}
	}
}
