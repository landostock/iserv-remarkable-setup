package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func mappingFixture(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"enabled": false, "interval_minutes": float64(30),
		"schedule":       map[string]any{"2": []any{map[string]any{"start": "08:00", "end": "09:00"}}},
		"future_setting": "keep me",
		"sync":           map[string]any{"dry_run": false, "lookback_days": float64(17), "max_file_mb": float64(51), "extensions": []any{"pdf"}, "sources": []any{map[string]any{"name": "Alt", "iserv_path": "Groups/old", "remarkable_dir": "/Old"}}},
	}
}

func TestMappingSavePreservesFreshUnrelatedSettingsAndDoesNotStartRun(t *testing.T) {
	settings := mappingFixture(t)
	old, err := sourcesFromSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	revision := mappingRevision(old)
	// A schedule change made after opening the editor must be retained.
	settings["interval_minutes"] = float64(60)
	wanted := []Source{{Name: "Deutsch", IServPath: "Groups/Deutsch & mehr", RemarkableDir: "/Schule/Deutsch"}}
	var written map[string]any
	request := func(base, password, method, path string, body []byte) ([]byte, int, error) {
		if path != "/api/admin/settings" {
			t.Fatalf("unexpected operation %s", path)
		}
		if method == http.MethodGet {
			raw, _ := json.Marshal(map[string]any{"settings": settings})
			return raw, 200, nil
		}
		if method != http.MethodPut {
			t.Fatal(method)
		}
		if err := json.Unmarshal(body, &written); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"ok": true, "settings": written})
		return raw, 200, nil
	}
	if err := saveMappings("https://example.workers.dev", "private-password", revision, wanted, request); err != nil {
		t.Fatal(err)
	}
	got, err := sourcesFromSettings(written)
	if err != nil || !reflect.DeepEqual(got, wanted) {
		t.Fatalf("got=%v err=%v", got, err)
	}
	delete(written["sync"].(map[string]any), "sources")
	delete(settings["sync"].(map[string]any), "sources")
	if !reflect.DeepEqual(written, settings) {
		t.Fatalf("unrelated settings changed: %#v", written)
	}
}

func TestMappingSaveRejectsStaleDraftActiveAutomationAndBusyMachine(t *testing.T) {
	for _, mode := range []string{"stale", "enabled", "running", "missing-mode", "unauthorized", "write-failed", "unconfirmed"} {
		t.Run(mode, func(t *testing.T) {
			settings := mappingFixture(t)
			old, _ := sourcesFromSettings(settings)
			revision := mappingRevision(old)
			status := map[string]any{}
			if mode == "stale" {
				revision = "old"
			}
			if mode == "enabled" {
				settings["enabled"] = true
			}
			if mode == "missing-mode" {
				delete(settings, "enabled")
			}
			if mode == "running" {
				status["machine_state"] = "running"
			}
			writes := 0
			request := func(_, _, method, path string, body []byte) ([]byte, int, error) {
				if method == http.MethodGet {
					if mode == "unauthorized" {
						return []byte("private-provider-output"), 401, nil
					}
					raw, _ := json.Marshal(map[string]any{"settings": settings, "status": status})
					return raw, 200, nil
				}
				writes++
				if mode == "write-failed" {
					return nil, 503, nil
				}
				return []byte(`{"ok":true}`), 200, nil
			}
			if err := saveMappings("https://example.workers.dev", "private-password", revision, old, request); err == nil {
				t.Fatal("unsafe save accepted")
			}
			if mode != "write-failed" && mode != "unconfirmed" && writes != 0 {
				t.Fatal("a rejected save still wrote settings")
			}
		})
	}
}

func TestMappingValidationRejectsUnsafeAndDuplicateSources(t *testing.T) {
	for _, sources := range [][]Source{
		nil, {{Name: "Unsafe", IServPath: "Groups/../secret", RemarkableDir: "/Valid"}},
		{{Name: "A", IServPath: "Groups/A", RemarkableDir: "/Valid"}, {Name: "B", IServPath: "Groups/A", RemarkableDir: "/Other"}},
	} {
		if validateMappingSources(sources) == nil {
			t.Fatal("invalid mappings accepted")
		}
	}
}

func TestMappingsEndpointsRequireLocalSessionAndConnectedInstallation(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	for _, path := range []string{"/api/maintenance/mappings/read", "/api/maintenance/mappings/save"} {
		for _, authenticated := range []bool{false, true} {
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{"password":"private-password"}`))
			if authenticated {
				req.Header.Set("X-Setup-Token", "test-token")
			}
			rr := httptest.NewRecorder()
			app.routes().ServeHTTP(rr, req)
			want := 403
			if authenticated {
				want = 409
			}
			if rr.Code != want {
				t.Fatalf("%s status=%d want=%d", path, rr.Code, want)
			}
		}
	}
}

func TestSettingsChangesBlockActivationDuringMappingSave(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	app.st.MaintenanceStatus = "running"
	for _, handler := range []func(http.ResponseWriter, *http.Request){app.handleFirstRealSyncStart, app.handleEnableAutomaticSync} {
		rr := httptest.NewRecorder()
		handler(rr, httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"password":"private-password"}`)))
		if rr.Code != 409 {
			t.Fatalf("status=%d", rr.Code)
		}
	}
}
