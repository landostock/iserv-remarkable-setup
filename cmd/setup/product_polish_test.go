package main

import (
	"strings"
	"testing"
)

func TestInjectProductPolish(t *testing.T) {
	page := `<!doctype html><html><head><style>body{}</style></head><body><script nonce="abc">const x=1;</script></body></html>`
	enhanced, ok := injectProductPolish(page)
	if !ok {
		t.Fatal("expected polish injection")
	}
	if !strings.Contains(enhanced, "productPolishMarker") {
		t.Fatal("missing product polish CSS marker")
	}
	if !strings.Contains(enhanced, "Complete safe activation") {
		t.Fatal("missing product polish JavaScript")
	}
	if !strings.Contains(enhanced, `<script nonce="abc">`) {
		t.Fatal("existing nonce-bearing script tag must be preserved")
	}
	if strings.Count(enhanced, "<script") != 1 {
		t.Fatal("polish must stay inside the existing CSP-approved script")
	}
	if strings.Count(enhanced, "<style") != 1 {
		t.Fatal("polish must stay inside the existing style element")
	}

	again, ok := injectProductPolish(enhanced)
	if ok || again != enhanced {
		t.Fatal("polish injection must be idempotent")
	}
}

func TestDashboardInspectionIsAvailableBeforeRealUpload(t *testing.T) {
	if strings.Contains(productPolishJS, "openDashboard.disabled") {
		t.Fatal("opening the dashboard must not require a real upload")
	}
	if !strings.Contains(productPolishCSS, "grid-template-columns:repeat(6,minmax(0,1fr));overflow:visible") ||
		!strings.Contains(productPolishJS, "steps.scrollLeft=") {
		t.Fatal("step navigation must keep the current step reachable on narrow screens")
	}
}
