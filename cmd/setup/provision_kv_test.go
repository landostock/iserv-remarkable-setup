package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testKVNamespaceID = "0123456789abcdef0123456789abcdef"

func writeTestWorkerConfig(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wrangler.jsonc")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestKVNamespaceTitleIsInstallationScoped(t *testing.T) {
	title, err := kvNamespaceTitle("iserv-rm-abcdef12")
	if err != nil {
		t.Fatal(err)
	}
	if title != "ISERV_RM_CONFIG_abcdef12" {
		t.Fatalf("title=%q", title)
	}
	for _, invalid := range []string{"ISERV_RM_CONFIG", "iserv-rm-", "iserv-rm-not-hex", "iserv-rm-gggggggg", "iserv-rm-012345678"} {
		if _, err := kvNamespaceTitle(invalid); err == nil {
			t.Fatalf("invalid app name accepted: %q", invalid)
		}
	}
}

func TestFindKVNamespaceIDRequiresOneExactInstallationTitle(t *testing.T) {
	raw := `[
  {"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","title":"ISERV_RM_CONFIG"},
  {"id":"0123456789abcdef0123456789abcdef","title":"ISERV_RM_CONFIG_abcdef12"}
]`
	id, found, err := findKVNamespaceID(raw, "ISERV_RM_CONFIG_abcdef12")
	if err != nil || !found || id != testKVNamespaceID {
		t.Fatalf("id=%q found=%v err=%v", id, found, err)
	}
	if _, found, err := findKVNamespaceID(raw, "ISERV_RM_CONFIG_deadbeef"); err != nil || found {
		t.Fatalf("unrelated namespace was adopted: found=%v err=%v", found, err)
	}
	duplicate := `[
  {"id":"0123456789abcdef0123456789abcdef","title":"ISERV_RM_CONFIG_abcdef12"},
  {"id":"fedcba9876543210fedcba9876543210","title":"ISERV_RM_CONFIG_abcdef12"}
]`
	if _, _, err := findKVNamespaceID(duplicate, "ISERV_RM_CONFIG_abcdef12"); err == nil {
		t.Fatal("duplicate exact namespaces were accepted")
	}
}

func TestFindKVNamespaceIDAcceptsOneJSONListAroundWranglerNotices(t *testing.T) {
	raw := `wrangler 4.125.0 (update available 4.129.0)
Resource location: remote
[{"id":"0123456789abcdef0123456789abcdef","title":"ISERV_RM_CONFIG_abcdef12"}]
Logs were written to "~/wrangler.log"`
	id, found, err := findKVNamespaceID(raw, "ISERV_RM_CONFIG_abcdef12")
	if err != nil || !found || id != testKVNamespaceID {
		t.Fatalf("id=%q found=%v err=%v", id, found, err)
	}
	if _, _, err := findKVNamespaceID(`[] trailing []`, "ISERV_RM_CONFIG_abcdef12"); err == nil {
		t.Fatal("ambiguous JSON output was accepted")
	}
	for _, malformed := range []string{`null`, `[null]`, `[{"title":"ISERV_RM_CONFIG_abcdef12"}]`, `[{"id":"0123456789abcdef0123456789abcdef"}]`} {
		if _, _, err := findKVNamespaceID(malformed, "ISERV_RM_CONFIG_abcdef12"); err == nil {
			t.Fatalf("malformed namespace list was accepted: %s", malformed)
		}
	}
}

func TestEnsureKVNamespaceCreatesUniqueNameAndWritesBinding(t *testing.T) {
	config := writeTestWorkerConfig(t, `{"name":"iserv-rm-control-abcdef12","vars":{"keep":"value"}}`)
	var calls [][]string
	runner := func(args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		switch len(calls) {
		case 1:
			return `[{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","title":"ISERV_RM_CONFIG"}]`, nil
		case 2:
			return "created", nil
		default:
			return `[{"id":"0123456789abcdef0123456789abcdef","title":"ISERV_RM_CONFIG_abcdef12"}]`, nil
		}
	}
	id, reused, err := ensureKVNamespace(runner, config, "ISERV_RM_CONFIG_abcdef12", "", func(time.Duration) {})
	if err != nil || reused || id != testKVNamespaceID {
		t.Fatalf("id=%q reused=%v err=%v", id, reused, err)
	}
	wantCalls := [][]string{
		{"kv", "namespace", "list"},
		{"kv", "namespace", "create", "ISERV_RM_CONFIG_abcdef12"},
		{"kv", "namespace", "list"},
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls=%v want=%v", calls, wantCalls)
	}
	boundID, err := readKVNamespaceID(config)
	if err != nil || boundID != testKVNamespaceID {
		t.Fatalf("bound id=%q err=%v", boundID, err)
	}
	raw, err := os.ReadFile(config)
	if err != nil || !strings.Contains(string(raw), `"keep": "value"`) {
		t.Fatalf("unrelated config was not preserved: %s err=%v", raw, err)
	}
}

func TestEnsureKVNamespaceRecoversAfterCreateResultIsLost(t *testing.T) {
	config := writeTestWorkerConfig(t, `{"name":"iserv-rm-control-abcdef12"}`)
	call := 0
	runner := func(args []string) (string, error) {
		call++
		switch call {
		case 1:
			return `[]`, nil
		case 2:
			return "", errors.New("connection reset after provider accepted creation")
		default:
			return `[{"id":"0123456789abcdef0123456789abcdef","title":"ISERV_RM_CONFIG_abcdef12"}]`, nil
		}
	}
	id, reused, err := ensureKVNamespace(runner, config, "ISERV_RM_CONFIG_abcdef12", "", func(time.Duration) {})
	if err != nil || reused || id != testKVNamespaceID {
		t.Fatalf("lost result was not recovered: id=%q reused=%v err=%v", id, reused, err)
	}
}

func TestEnsureKVNamespaceResumesExactExistingNamespaceWithoutCreate(t *testing.T) {
	config := writeTestWorkerConfig(t, `{"name":"iserv-rm-control-abcdef12"}`)
	calls := 0
	runner := func(args []string) (string, error) {
		calls++
		if !reflect.DeepEqual(args, []string{"kv", "namespace", "list"}) {
			t.Fatalf("unexpected command: %v", args)
		}
		return `[{"id":"0123456789abcdef0123456789abcdef","title":"ISERV_RM_CONFIG_abcdef12"}]`, nil
	}
	id, reused, err := ensureKVNamespace(runner, config, "ISERV_RM_CONFIG_abcdef12", "", func(time.Duration) {})
	if err != nil || !reused || id != testKVNamespaceID || calls != 1 {
		t.Fatalf("id=%q reused=%v calls=%d err=%v", id, reused, calls, err)
	}
}

func TestEnsureKVNamespaceResumesMatchingLocalAndRemoteBinding(t *testing.T) {
	config := writeTestWorkerConfig(t, `{
	  "kv_namespaces": [{"binding":"CONFIG","id":"0123456789abcdef0123456789abcdef"}]
	}`)
	calls := 0
	id, reused, err := ensureKVNamespace(func(args []string) (string, error) {
		calls++
		return `[{"id":"0123456789abcdef0123456789abcdef","title":"ISERV_RM_CONFIG_abcdef12"}]`, nil
	}, config, "ISERV_RM_CONFIG_abcdef12", testKVNamespaceID, func(time.Duration) {})
	if err != nil || !reused || id != testKVNamespaceID || calls != 1 {
		t.Fatalf("matching binding was not resumed: id=%q reused=%v calls=%d err=%v", id, reused, calls, err)
	}
}

func TestEnsureKVNamespaceFailsClosedOnUnownedLocalBinding(t *testing.T) {
	config := writeTestWorkerConfig(t, `{
	  "kv_namespaces": [{"binding":"CONFIG","id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]
	}`)
	calls := 0
	_, _, err := ensureKVNamespace(func(args []string) (string, error) {
		calls++
		return `[{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","title":"ISERV_RM_CONFIG"}]`, nil
	}, config, "ISERV_RM_CONFIG_abcdef12", "", func(time.Duration) {})
	if err == nil || calls != 1 {
		t.Fatalf("unowned binding was accepted or caused a create: calls=%d err=%v", calls, err)
	}
}

func TestEnsureKVNamespaceReturnsCreateErrorWhenNothingWasCreated(t *testing.T) {
	config := writeTestWorkerConfig(t, `{"name":"iserv-rm-control-abcdef12"}`)
	createErr := errors.New("permission denied")
	calls := 0
	_, _, err := ensureKVNamespace(func(args []string) (string, error) {
		calls++
		if len(args) >= 3 && args[2] == "create" {
			return "", createErr
		}
		return `[]`, nil
	}, config, "ISERV_RM_CONFIG_abcdef12", "", func(time.Duration) {})
	if !errors.Is(err, createErr) || calls != 5 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
