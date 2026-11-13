package main

import "testing"

func TestManagedFlyChecksumURLUsesPublishedAssetName(t *testing.T) {
	want := "https://github.com/superfly/flyctl/releases/download/v" + managedFlyVersion + "/flyctl_" + managedFlyVersion + "_checksums.txt"
	if got := managedFlyChecksumURL(); got != want {
		t.Fatalf("managed Fly.io checksum URL = %q, want %q", got, want)
	}
}
