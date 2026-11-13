package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const flyControlTokenName = "IServ-reMarkable dashboard control"

var (
	flyTokenANSI       = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	flyTokenDate       = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	flyTokenID         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~+/=:-]{7,255}$`)
	flyTokenRevokeDone = regexp.MustCompile(`(?m)^1 tokens revoked\s*$`)
)

func activeFlyControlTokenIDs(output string) ([]string, error) {
	clean := flyTokenANSI.ReplaceAllString(output, "")
	headerFound := false
	ids := make([]string, 0)
	seen := make(map[string]bool)
	for _, line := range strings.Split(clean, "\n") {
		columns := splitFlyTokenColumns(line)
		if len(columns) == 5 && strings.EqualFold(columns[0], "ID") && strings.EqualFold(columns[1], "NAME") && strings.EqualFold(columns[2], "CREATED BY") && strings.EqualFold(columns[3], "EXPIRES AT") && strings.EqualFold(columns[4], "REVOKED AT") {
			headerFound = true
			continue
		}
		if !strings.Contains(line, flyControlTokenName) {
			continue
		}
		if len(columns) != 5 || columns[1] != flyControlTokenName || !flyTokenID.MatchString(columns[0]) {
			return nil, fmt.Errorf("Fly-Token-Liste enthält eine ungültige Steuerberechtigung")
		}
		if len(flyTokenDate.FindAllString(columns[3], -1)) != 1 {
			return nil, fmt.Errorf("Fly-Token-Liste hat ein unerwartetes Ablaufdatum")
		}
		if columns[4] == "" {
			if !seen[columns[0]] {
				seen[columns[0]] = true
				ids = append(ids, columns[0])
			}
			continue
		}
		if len(flyTokenDate.FindAllString(columns[4], -1)) != 1 {
			return nil, fmt.Errorf("Fly-Token-Liste hat ein unerwartetes Widerrufsdatum")
		}
		// Already revoked; flyctl deliberately keeps it in the list.
	}
	if !headerFound {
		return nil, fmt.Errorf("Fly-Token-Liste hat ein unerwartetes Format")
	}
	return ids, nil
}

func splitFlyTokenColumns(line string) []string {
	if !strings.ContainsAny(line, "│|") {
		return nil
	}
	parts := strings.Split(strings.ReplaceAll(line, "|", "│"), "│")
	columns := make([]string, len(parts))
	for i, part := range parts {
		columns[i] = strings.TrimSpace(part)
	}
	return columns
}

func listActiveFlyControlTokenIDsWith(run func(string, string, []string, string, time.Duration) (string, error), runtimeDir, fly, appName string) ([]string, error) {
	out, err := run(runtimeDir, fly, []string{"tokens", "list", "-a", appName}, "", 2*time.Minute)
	if err != nil {
		return nil, err
	}
	return activeFlyControlTokenIDs(out)
}

func flyTokenRevokeSucceeded(output string) bool {
	clean := flyTokenANSI.ReplaceAllString(output, "")
	return !strings.Contains(strings.ToLower(clean), "failed to revoke") && flyTokenRevokeDone.MatchString(clean)
}

func revokeFlyControlTokensWith(run func(string, string, []string, string, time.Duration) (string, error), runtimeDir, fly string, ids []string) error {
	failed := false
	for _, id := range ids {
		out, err := run(runtimeDir, fly, []string{"tokens", "revoke", id}, "", 2*time.Minute)
		if err != nil || !flyTokenRevokeSucceeded(out) {
			failed = true
		}
	}
	if failed {
		return fmt.Errorf("mindestens eine vorherige Fly-Steuerberechtigung konnte nicht sicher widerrufen werden")
	}
	return nil
}
