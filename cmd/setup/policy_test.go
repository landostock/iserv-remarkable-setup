package main

import (
	"strings"
	"testing"
	"time"
)

func TestFlyControlTokenPolicyUsesOneYearRotation(t *testing.T) {
	if flyControlTokenLifetime != 365*24*time.Hour {
		t.Fatalf("fly control token lifetime=%s want=%s", flyControlTokenLifetime, 365*24*time.Hour)
	}
	if flyControlTokenCLIExpiry != "8760h" {
		t.Fatalf("fly CLI expiry=%q want=%q", flyControlTokenCLIExpiry, "8760h")
	}
}

func TestMaintenanceDashboardPasswordPolicyRejectsUnsafeValuesBeforeCloudAccess(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	invalid := []string{
		"too-short",
		strings.Repeat("a", 257),
		"123456789012345\nunsafe",
		"123456789012345\runsafe",
		"123456789012345\x00unsafe",
	}
	for _, password := range invalid {
		if err := app.maintenanceResetDashboardPassword(password); err == nil || !strings.Contains(err.Error(), "16–256") {
			t.Fatalf("password policy accepted unsafe value or returned wrong error: %v", err)
		}
	}
}

func TestMaintenanceDashboardPasswordPolicyAllowsValidShapeBeforeInstallationCheck(t *testing.T) {
	app := NewApp(t.TempDir(), "test-token")
	err := app.maintenanceResetDashboardPassword("correct-horse-battery-staple")
	if err == nil || !strings.Contains(err.Error(), "keine bestehende Installation") {
		t.Fatalf("valid password shape should reach installation check, got %v", err)
	}
}
