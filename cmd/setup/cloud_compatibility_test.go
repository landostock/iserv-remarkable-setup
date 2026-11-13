package main

import "testing"

func TestRC26CompatibilityRequiresUnchangedDeployedPayload(t *testing.T) {
	if currentCloudPayloadDigest != compatibleRC26PayloadDigest {
		t.Fatal("deployed payload changed: remove RC26 compatibility before releasing")
	}
	if !cloudComponentsCurrent(&installMeta{Version: "0.9.0-rc26"}) {
		t.Fatal("unchanged RC26 cloud was rejected")
	}
	if cloudComponentsCurrent(&installMeta{Version: "0.9.0-rc25"}) {
		t.Fatal("older cloud was accepted")
	}
}
