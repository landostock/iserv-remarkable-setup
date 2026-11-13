package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRedactCommandOutputCoversProviderAndPersistedSecretShapes(t *testing.T) {
	input := strings.Join([]string{
		"Authorization: Bearer bearer-secret-value",
		"FlyV1 fly-secret-value",
		"RCLONE_CONFIG_ISERV_URL=https://iserv.test/remote.php/dav/files/student",
		"RCLONE_CONFIG_ISERV_USER=student-name",
		"RCLONE_CONFIG_ISERV_PASS=obscured-password-value",
		"RMAPI_CONFIG_B64=base64-device-and-user-token",
		"ADMIN_PASSWORD=very-long-password",
		"SYNC_API_TOKEN=sync-token-value",
		"deviceToken=device-secret",
		`Logs were written to "/Users/fixture-user/Library/Application Support/IServ/log.txt"`,
		`Linux log: /home/student/.config/wrangler/log.txt`,
		`Windows log: C:\Users\Student\AppData\Local\wrangler.log`,
		"ordinary diagnostic text stays visible",
	}, "\n")

	redacted := redactCommandOutput(input)
	for _, secret := range []string{
		"bearer-secret-value",
		"fly-secret-value",
		"iserv.test",
		"student-name",
		"obscured-password-value",
		"base64-device-and-user-token",
		"very-long-password",
		"sync-token-value",
		"device-secret",
		"fixture-user",
		"student/.config",
		`Student\AppData`,
	} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redaction leaked %q in %q", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "ordinary diagnostic text stays visible") {
		t.Fatal("redaction removed unrelated diagnostic context")
	}
	if strings.Count(redacted, "~") < 3 {
		t.Fatalf("portable home paths were not redacted: %q", redacted)
	}
}

func TestCommandFailureOutputNeverReturnsRawSecretBearingOutput(t *testing.T) {
	raw := "operation failed: RCLONE_CONFIG_ISERV_PASS=secret-value token=another-secret"
	returned, message := commandFailureOutput(raw)
	for _, value := range []string{"secret-value", "another-secret"} {
		if strings.Contains(returned, value) || strings.Contains(message, value) {
			t.Fatalf("command failure output leaked %q", value)
		}
	}
	if !strings.Contains(returned, "[redacted]") || !strings.Contains(message, "operation failed") {
		t.Fatalf("redacted failure output lost useful context: returned=%q message=%q", returned, message)
	}
}

func TestCommandFailureMessageIsBounded(t *testing.T) {
	raw := strings.Repeat("x", 1500) + " token=secret-tail"
	returned, message := commandFailureOutput(raw)
	if len(message) > 800 {
		t.Fatalf("failure message length=%d exceeds 800", len(message))
	}
	if strings.Contains(returned, "secret-tail") || strings.Contains(message, "secret-tail") {
		t.Fatal("bounded failure output leaked a trailing credential")
	}
}

func TestCommandFailureMessageKeepsCauseAndDropsNodeStackFrames(t *testing.T) {
	raw := "Error: EACCES: permission denied, mkdir '/private/auth/config'\n" +
		"    at Object.mkdirSync (node:fs:1363:26)\n" +
		"    at Object.write (/private/wrangler/cli.js:124907:13)\n" +
		"Node.js v24.0.0\n" +
		"Logs were written to /private/wrangler.log"
	_, message := commandFailureOutput(raw)
	if !strings.Contains(message, "EACCES: permission denied") || !strings.Contains(message, "Logs were written") {
		t.Fatalf("failure message lost cause or log location: %q", message)
	}
	if strings.Contains(message, "Object.mkdirSync") || strings.Contains(message, "Node.js v") {
		t.Fatalf("failure message retained unhelpful stack frames: %q", message)
	}
}

func TestCommandRunFailurePreservesDeadlineInsteadOfReportingKilledSignal(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err := commandRunFailure(ctx, "", errors.New("signal: killed"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v, want context deadline exceeded", err)
	}
	if strings.Contains(err.Error(), "signal: killed") {
		t.Fatalf("timeout leaked misleading process signal: %v", err)
	}
}

func TestCommandOutputRemovesTerminalEscapesAndControls(t *testing.T) {
	raw := "\x1b[31mERROR\x1b[0m\n\x1b]8;;https://example.test\x07link\x1b]8;;\x07\x00done"
	got := cleanCommandOutput(raw)
	if got != "ERROR\nlinkdone" {
		t.Fatalf("clean output=%q", got)
	}
}
