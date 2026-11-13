package main

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestActiveFlyControlTokenIDsReturnsOnlyManagedActiveTokens(t *testing.T) {
	output := `Tokens for app "iserv-rm-test1234":
 ID                   │ NAME                                │ CREATED BY       │ EXPIRES AT                    │ REVOKED AT
 active-token-id-1234 │ IServ-reMarkable dashboard control │ owner@example.de │ 2027-09-02 12:00:00 +0000 UTC │
 revoked-token-id-12  │ IServ-reMarkable dashboard control │ owner@example.de │ 2027-08-01 12:00:00 +0000 UTC │ 2026-08-02 12:00:00 +0000 UTC
 unrelated-token-id   │ another deploy token               │ owner@example.de │ 2027-09-02 12:00:00 +0000 UTC │
`
	got, err := activeFlyControlTokenIDs(output)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"active-token-id-1234"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("active token IDs=%v want=%v", got, want)
	}
}

func TestActiveFlyControlTokenIDsAcceptsFreshAppWithoutRows(t *testing.T) {
	output := "Tokens for app \"fresh-app\":\n ID │ NAME │ CREATED BY │ EXPIRES AT │ REVOKED AT \n"
	got, err := activeFlyControlTokenIDs(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("fresh app unexpectedly returned token IDs: %v", got)
	}
}

func TestActiveFlyControlTokenIDsRejectsAmbiguousOrChangedOutput(t *testing.T) {
	cases := []string{
		"Tokens for app \"x\":\n ID │ NAME │ CREATED BY │ EXPIRES AT\n",
		" ID │ NAME │ CREATED BY │ EXPIRES AT │ REVOKED AT\n invalid! │ IServ-reMarkable dashboard control │ owner@example.de │ 2027-09-02 12:00:00 +0000 UTC │\n",
		" ID │ NAME │ CREATED BY │ EXPIRES AT │ REVOKED AT\n valid-token-id │ IServ-reMarkable dashboard control │ owner@example.de │ no-date │\n",
	}
	for _, output := range cases {
		if _, err := activeFlyControlTokenIDs(output); err == nil {
			t.Fatalf("unexpected token list output was accepted: %q", output)
		}
	}
}

func TestFlyTokenRevokeSucceededRequiresExplicitSuccess(t *testing.T) {
	if !flyTokenRevokeSucceeded("Revoked active-token-id-1234\n1 tokens revoked\n") {
		t.Fatal("explicit single-token revocation was rejected")
	}
	for _, output := range []string{
		"0 tokens revoked\n",
		"failed to revoke token active-token-id-1234: already revoked\n0 tokens revoked\n",
		"Revoked active-token-id-1234\n",
	} {
		if flyTokenRevokeSucceeded(output) {
			t.Fatalf("ambiguous revocation output was accepted: %q", output)
		}
	}
}

func TestRevokeFlyControlTokensAttemptsEveryCapturedToken(t *testing.T) {
	var got [][]string
	run := func(_ string, _ string, args []string, _ string, _ time.Duration) (string, error) {
		got = append(got, append([]string(nil), args...))
		if args[2] == "first-token-id" {
			return "0 tokens revoked\n", errors.New("temporary failure")
		}
		return "Revoked second-token-id\n1 tokens revoked\n", nil
	}
	err := revokeFlyControlTokensWith(run, "runtime", "flyctl", []string{"first-token-id", "second-token-id"})
	if err == nil {
		t.Fatal("partial revocation failure was not reported")
	}
	want := [][]string{{"tokens", "revoke", "first-token-id"}, {"tokens", "revoke", "second-token-id"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("revocation calls=%v want=%v", got, want)
	}
}
