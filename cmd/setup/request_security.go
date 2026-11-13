package main

import (
	"net"
	"net/http"
	"strings"
)

func localRequestGuard(allowedHost string, next http.Handler) http.Handler {
	allowedHost = strings.TrimSpace(allowedHost)
	allowedOrigin := "http://" + allowedHost
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// The server listens on a random 127.0.0.1 port. Requiring the exact
		// listener host defeats DNS-rebinding requests that happen to reach the
		// loopback socket through an attacker-controlled Host header.
		if allowedHost == "" || r.Host != allowedHost {
			http.Error(w, "invalid local host", http.StatusForbidden)
			return
		}
		remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, "invalid local peer", http.StatusForbidden)
			return
		}
		remoteIP := net.ParseIP(remoteHost)
		if remoteIP == nil || !remoteIP.IsLoopback() {
			http.Error(w, "local access only", http.StatusForbidden)
			return
		}

		// Modern browsers expose the navigation context. Cross-site requests are
		// never needed by this localhost application.
		if site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); site != "" && site != "same-origin" && site != "none" {
			http.Error(w, "cross-site request rejected", http.StatusForbidden)
			return
		}

		// Unsafe browser requests must originate from this exact ephemeral local
		// origin. The per-session X-Setup-Token remains an additional CSRF guard.
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if r.Header.Get("Origin") != allowedOrigin {
				http.Error(w, "invalid local origin", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
