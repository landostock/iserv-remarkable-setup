package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestDiagnosticRedactionCoversKnownCredentialAndIdentifierShapes(t *testing.T) {
	input := strings.Join([]string{
		"Authorization: Bearer browser-token",
		"FlyV1 fm2_permission_token,fm2_discharge_token",
		"RCLONE_CONFIG_ISERV_URL=https://iserv.test/remote.php/dav/files/student",
		"RCLONE_CONFIG_ISERV_USER=student-name",
		"RCLONE_CONFIG_ISERV_PASS=obscured-value",
		"RMAPI_CONFIG_B64=encoded-remarkable-config",
		"ADMIN_PASSWORD=dashboard-secret",
		"contact=student@school.test",
	}, " ")

	redacted := redactDiagnosticText(input)
	for _, value := range []string{
		"browser-token",
		"fm2_permission_token",
		"fm2_discharge_token",
		"iserv.test",
		"student-name",
		"obscured-value",
		"encoded-remarkable-config",
		"dashboard-secret",
		"student@school.test",
	} {
		if strings.Contains(redacted, value) {
			t.Fatalf("diagnostic redaction leaked %q in %q", value, redacted)
		}
	}
	if !strings.Contains(redacted, "[url redacted]") || !strings.Contains(redacted, "[email redacted]") {
		t.Fatalf("diagnostic redaction did not leave useful redaction markers: %q", redacted)
	}
}

func TestDiagnosticVerificationIsCopiedAndRedacted(t *testing.T) {
	original := &verificationResult{
		Outcome: "failed",
		Reason:  "provider_error token=secret-token",
		Message: "See https://iserv.test/path for student@school.test",
		Stats:   verificationStats{Checked: 2, Failed: 1},
	}
	redacted := redactDiagnosticVerification(original)
	if redacted == original {
		t.Fatal("diagnostic verification reused the mutable source pointer")
	}
	if strings.Contains(redacted.Reason, "secret-token") || strings.Contains(redacted.Message, "iserv.test") || strings.Contains(redacted.Message, "student@school.test") {
		t.Fatalf("verification diagnostic text was not redacted: %#v", redacted)
	}
	if original.Reason != "provider_error token=secret-token" || !strings.Contains(original.Message, "iserv.test") {
		t.Fatal("redaction mutated the in-memory verification result")
	}
	if redacted.Stats != original.Stats || redacted.Outcome != original.Outcome {
		t.Fatal("redaction changed non-sensitive verification counters or outcome")
	}
}

func TestDiagnosticVerificationHandlesNil(t *testing.T) {
	if redactDiagnosticVerification(nil) != nil {
		t.Fatal("nil verification should stay nil")
	}
}

func TestDiagnosticHealthClientDoesNotFollowRedirects(t *testing.T) {
	client := diagnosticHTTPClient()
	req, err := http.NewRequest(http.MethodGet, "https://dashboard.example.test/other", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(req, []*http.Request{{}}); err == nil {
		t.Fatal("diagnostic health check followed a redirect")
	}
}
