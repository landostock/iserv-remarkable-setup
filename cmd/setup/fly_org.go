package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const maxFlyOrganizationsOutput = 64 * 1024

var flyOrgSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

type flyOrganization struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func decodeFlyOrganizations(raw string) ([]flyOrganization, error) {
	if len(raw) == 0 || len(raw) > maxFlyOrganizationsOutput {
		return nil, fmt.Errorf("Fly-Organisationsliste ist leer oder zu groß")
	}
	trimmed := strings.TrimSpace(raw)
	if organizations, err := decodeFlyOrganizationsJSON(trimmed); err == nil {
		return organizations, nil
	}

	// flyctl may add update notices to the combined command output. Accept one
	// unambiguous, valid JSON object and reject changed or mixed output.
	var decoded []flyOrganization
	matches := 0
	for offset, char := range trimmed {
		if char != '{' {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(trimmed[offset:]))
		var bySlug map[string]string
		if err := decoder.Decode(&bySlug); err != nil {
			continue
		}
		organizations, err := validateFlyOrganizations(bySlug)
		if err != nil {
			continue
		}
		matches++
		decoded = organizations
		if matches > 1 {
			return nil, fmt.Errorf("Fly-Organisationsliste enthält mehrere JSON-Ergebnisse")
		}
	}
	if matches != 1 {
		return nil, fmt.Errorf("Fly-Organisationsliste ist ungültig")
	}
	return decoded, nil
}

func decodeFlyOrganizationsJSON(raw string) ([]flyOrganization, error) {
	var bySlug map[string]string
	if err := json.Unmarshal([]byte(raw), &bySlug); err != nil {
		return nil, err
	}
	return validateFlyOrganizations(bySlug)
}

func validateFlyOrganizations(bySlug map[string]string) ([]flyOrganization, error) {
	if len(bySlug) == 0 || len(bySlug) > 100 {
		return nil, fmt.Errorf("Fly-Konto enthält keine oder zu viele Organisationen")
	}
	organizations := make([]flyOrganization, 0, len(bySlug))
	for slug, name := range bySlug {
		name = strings.TrimSpace(name)
		if !flyOrgSlugPattern.MatchString(slug) || name == "" || len(name) > 200 || strings.ContainsAny(name, "\x00\r\n") {
			return nil, fmt.Errorf("Fly-Organisationsliste enthält einen ungültigen Eintrag")
		}
		organizations = append(organizations, flyOrganization{Slug: slug, Name: name})
	}
	sort.Slice(organizations, func(i, j int) bool {
		return organizations[i].Slug < organizations[j].Slug
	})
	return organizations, nil
}

func chooseFlyOrganization(organizations []flyOrganization, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested != "" && !flyOrgSlugPattern.MatchString(requested) {
		return "", fmt.Errorf("ungültige Fly-Organisation")
	}
	for _, organization := range organizations {
		if organization.Slug == requested {
			return organization.Slug, nil
		}
	}
	if requested != "" {
		return "", fmt.Errorf("die ausgewählte Fly-Organisation ist nicht mehr verfügbar")
	}
	if len(organizations) == 1 {
		return organizations[0].Slug, nil
	}
	for _, organization := range organizations {
		if organization.Slug == "personal" || strings.EqualFold(organization.Name, "personal") {
			return organization.Slug, nil
		}
	}
	return "", fmt.Errorf("wähle nach der Fly.io-Anmeldung eine Organisation aus")
}

func flyAppCreateArgs(appName, organization string) ([]string, error) {
	if !cloudNamePattern.MatchString(appName) {
		return nil, fmt.Errorf("ungültige Fly-App-Kennung")
	}
	if !flyOrgSlugPattern.MatchString(organization) {
		return nil, fmt.Errorf("ungültige Fly-Organisation")
	}
	return []string{"apps", "create", appName, "--org", organization, "--yes"}, nil
}
