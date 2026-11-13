package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestFlyAppCreateArgsAlwaysSpecifiesValidatedOrganization(t *testing.T) {
	want := []string{"apps", "create", "iserv-rm-abcdef12", "--org", "personal", "--yes"}
	got, err := flyAppCreateArgs("iserv-rm-abcdef12", "personal")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
	for _, values := range [][2]string{{"../unsafe", "personal"}, {"iserv-rm-abcdef12", "--unsafe"}, {"iserv-rm-abcdef12", ""}} {
		if _, err := flyAppCreateArgs(values[0], values[1]); err == nil {
			t.Fatalf("unsafe app creation input was accepted: %#v", values)
		}
	}
}

func TestInstallConfigRejectsUnsafeFlyOrganization(t *testing.T) {
	cfg := defaultConfig()
	cfg.FlyOrgSlug = "--unsafe"
	cfg.AdminPassword = "0123456789abcdef"
	cfg.Sources = []Source{{Name: "Test", IServPath: "Groups/test", RemarkableDir: "/Test"}}
	if err := validConfig(cfg); err == nil {
		t.Fatal("unsafe Fly organization was accepted in setup configuration")
	}
}

func TestDecodeFlyOrganizationsAcceptsCurrentJSONMapAndNotice(t *testing.T) {
	raw := "Update available\n{\"school-team\":\"School Team\",\"personal\":\"Personal\"}\n"
	organizations, err := decodeFlyOrganizations(raw)
	if err != nil {
		t.Fatalf("decode organizations: %v", err)
	}
	want := []flyOrganization{{Slug: "personal", Name: "Personal"}, {Slug: "school-team", Name: "School Team"}}
	if !reflect.DeepEqual(organizations, want) {
		t.Fatalf("organizations = %#v, want %#v", organizations, want)
	}
}

func TestChooseFlyOrganizationUsesExplicitSoleOrPersonal(t *testing.T) {
	organizations := []flyOrganization{{Slug: "personal", Name: "Personal"}, {Slug: "school-team", Name: "School Team"}}
	cases := []struct {
		name      string
		requested string
		want      string
	}{
		{name: "explicit", requested: "school-team", want: "school-team"},
		{name: "personal", want: "personal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := chooseFlyOrganization(organizations, tc.requested)
			if err != nil {
				t.Fatalf("choose organization: %v", err)
			}
			if got != tc.want {
				t.Fatalf("organization = %q, want %q", got, tc.want)
			}
		})
	}
	sole, err := chooseFlyOrganization([]flyOrganization{{Slug: "only-team", Name: "Only Team"}}, "")
	if err != nil || sole != "only-team" {
		t.Fatalf("sole organization = %q, %v", sole, err)
	}
}

func TestChooseFlyOrganizationRejectsAmbiguousOrStaleSelection(t *testing.T) {
	organizations := []flyOrganization{{Slug: "first-team", Name: "First"}, {Slug: "second-team", Name: "Second"}}
	if _, err := chooseFlyOrganization(organizations, ""); err == nil {
		t.Fatal("ambiguous organization list was accepted")
	}
	if _, err := chooseFlyOrganization(organizations, "removed-team"); err == nil {
		t.Fatal("stale organization selection was accepted")
	}
}

func TestDecodeFlyOrganizationsRejectsUnsafeOrAmbiguousOutput(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":     "{}",
		"unsafe":    `{"--org":"Unsafe"}`,
		"control":   "{\"personal\":\"Bad\\nName\"}",
		"ambiguous": "{\"personal\":\"Personal\"}\n{\"team\":\"Team\"}",
		"oversized": strings.Repeat("x", maxFlyOrganizationsOutput+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeFlyOrganizations(raw); err == nil {
				t.Fatal("unsafe organization output was accepted")
			}
		})
	}
}
