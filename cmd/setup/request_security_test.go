package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func guardedRecorder(t *testing.T, method, host, remote, origin, fetchSite string) *httptest.ResponseRecorder {
	t.Helper()
	hit := false
	handler := localRequestGuard("127.0.0.1:42424", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(method, "http://127.0.0.1:42424/api/test", nil)
	req.Host = host
	req.RemoteAddr = remote
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if fetchSite != "" {
		req.Header.Set("Sec-Fetch-Site", fetchSite)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code == http.StatusNoContent && !hit {
		t.Fatal("guard returned success without invoking downstream handler")
	}
	if rr.Code != http.StatusNoContent && hit {
		t.Fatal("guard invoked downstream handler for a rejected request")
	}
	return rr
}

func TestLocalRequestGuardAcceptsExactLoopbackNavigation(t *testing.T) {
	rr := guardedRecorder(t, http.MethodGet, "127.0.0.1:42424", "127.0.0.1:53111", "", "none")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestLocalRequestGuardRejectsDNSRebindingHost(t *testing.T) {
	rr := guardedRecorder(t, http.MethodGet, "attacker.example", "127.0.0.1:53111", "", "none")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusForbidden)
	}
}

func TestLocalRequestGuardRejectsNonLoopbackPeer(t *testing.T) {
	rr := guardedRecorder(t, http.MethodGet, "127.0.0.1:42424", "192.0.2.50:53111", "", "none")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusForbidden)
	}
}

func TestLocalRequestGuardRequiresExactOriginForUnsafeMethods(t *testing.T) {
	for _, origin := range []string{"", "https://127.0.0.1:42424", "http://localhost:42424", "https://attacker.example"} {
		rr := guardedRecorder(t, http.MethodPost, "127.0.0.1:42424", "127.0.0.1:53111", origin, "same-origin")
		if rr.Code != http.StatusForbidden {
			t.Fatalf("origin=%q status=%d want=%d", origin, rr.Code, http.StatusForbidden)
		}
	}
	rr := guardedRecorder(t, http.MethodPost, "127.0.0.1:42424", "127.0.0.1:53111", "http://127.0.0.1:42424", "same-origin")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestLocalRequestGuardRejectsCrossSiteFetchContext(t *testing.T) {
	rr := guardedRecorder(t, http.MethodGet, "127.0.0.1:42424", "127.0.0.1:53111", "", "cross-site")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusForbidden)
	}
}
