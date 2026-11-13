package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}
type latestRelease struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []releaseAsset `json:"assets"`
}

func fetchLatestRelease() (latestRelease, error) {
	var rel latestRelease
	if strings.TrimSpace(releaseRepo) == "" {
		return rel, fmt.Errorf("kein Release-Kanal konfiguriert")
	}
	endpoint := "https://api.github.com/repos/" + releaseRepo + "/releases/latest"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return rel, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "IServ-reMarkable-Setup/"+appVersion)
	resp, err := secureHTTPClient(20 * time.Second).Do(req)
	if err != nil {
		return rel, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return rel, fmt.Errorf("Update-Dienst meldet HTTP %d", resp.StatusCode)
	}
	const maxReleaseResponse = 2 << 20
	limited := io.LimitReader(resp.Body, maxReleaseResponse+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return rel, err
	}
	if len(data) > maxReleaseResponse {
		return rel, fmt.Errorf("Update-Dienst liefert eine unerwartet große Antwort")
	}
	if err := json.Unmarshal(data, &rel); err != nil {
		return rel, err
	}
	return rel, nil
}

func checkLatestRelease() (map[string]any, error) {
	if strings.TrimSpace(releaseRepo) == "" {
		return map[string]any{"configured": false, "current": appVersion, "available": false}, nil
	}
	rel, err := fetchLatestRelease()
	if err != nil {
		return nil, err
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	available := semverGreater(latest, appVersion)
	return map[string]any{"configured": true, "current": appVersion, "latest": latest, "available": available, "release_url": rel.HTMLURL}, nil
}

type semVersion struct {
	core [3]int
	pre  []string
}

func parseSemver(v string) (semVersion, bool) {
	var out semVersion
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	if plus := strings.IndexByte(v, '+'); plus >= 0 {
		v = v[:plus]
	}
	main := v
	if dash := strings.IndexByte(v, '-'); dash >= 0 {
		main = v[:dash]
		pre := v[dash+1:]
		if pre == "" {
			return semVersion{}, false
		}
		out.pre = strings.Split(pre, ".")
	}
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return semVersion{}, false
	}
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return semVersion{}, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return semVersion{}, false
		}
		out.core[i] = n
	}
	for _, id := range out.pre {
		if id == "" {
			return semVersion{}, false
		}
		for _, ch := range id {
			if !((ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || ch == '-') {
				return semVersion{}, false
			}
		}
		if _, err := strconv.Atoi(id); err == nil && len(id) > 1 && id[0] == '0' {
			return semVersion{}, false
		}
	}
	return out, true
}

func semverGreater(a, b string) bool {
	aa, okA := parseSemver(a)
	bb, okB := parseSemver(b)
	if !okA || !okB {
		return false
	}
	for i := 0; i < 3; i++ {
		if aa.core[i] != bb.core[i] {
			return aa.core[i] > bb.core[i]
		}
	}
	if len(aa.pre) == 0 || len(bb.pre) == 0 {
		return len(aa.pre) == 0 && len(bb.pre) != 0
	}
	limit := len(aa.pre)
	if len(bb.pre) < limit {
		limit = len(bb.pre)
	}
	for i := 0; i < limit; i++ {
		if aa.pre[i] == bb.pre[i] {
			continue
		}
		an, aerr := strconv.Atoi(aa.pre[i])
		bn, berr := strconv.Atoi(bb.pre[i])
		switch {
		case aerr == nil && berr == nil:
			return an > bn
		case aerr == nil:
			return false
		case berr == nil:
			return true
		default:
			aprefix, anumber, anatural := splitNaturalPrerelease(aa.pre[i])
			bprefix, bnumber, bnatural := splitNaturalPrerelease(bb.pre[i])
			if anatural && bnatural && aprefix == bprefix {
				return anumber > bnumber
			}
			return aa.pre[i] > bb.pre[i]
		}
	}
	return len(aa.pre) > len(bb.pre)
}

func splitNaturalPrerelease(value string) (string, int, bool) {
	index := len(value)
	for index > 0 && value[index-1] >= '0' && value[index-1] <= '9' {
		index--
	}
	if index == 0 || index == len(value) {
		return "", 0, false
	}
	number, err := strconv.Atoi(value[index:])
	if err != nil {
		return "", 0, false
	}
	return value[:index], number, true
}

func downloadLatestPackage() (string, error) {
	if releaseRepo == "" {
		return "", fmt.Errorf("für diese Entwicklungsdatei ist noch kein öffentlicher Release-Kanal konfiguriert")
	}
	rel, err := fetchLatestRelease()
	if err != nil {
		return "", err
	}
	wanted := releasePackageName(runtime.GOOS, runtime.GOARCH)
	if wanted == "" {
		return "", fmt.Errorf("für diese Plattform gibt es noch kein Updatepaket")
	}
	var asset, checksum string
	for _, a := range rel.Assets {
		if a.Name == wanted {
			asset = a.BrowserDownloadURL
		}
		if a.Name == wanted+".sha256" {
			checksum = a.BrowserDownloadURL
		}
	}
	if asset == "" || checksum == "" {
		return "", fmt.Errorf("Releasepaket oder Prüfsumme fehlt")
	}
	checks, err := fetchSmallHTTPS(checksum, 128*1024)
	if err != nil {
		return "", err
	}
	expected := strings.Fields(string(checks))
	if len(expected) == 0 || len(expected[0]) != 64 {
		return "", fmt.Errorf("ungültige Release-Prüfsumme")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dstDir := filepath.Join(home, "Downloads")
	if runtime.GOOS == "windows" {
		if p := os.Getenv("USERPROFILE"); p != "" {
			dstDir = filepath.Join(p, "Downloads")
		}
	}
	if err := os.MkdirAll(dstDir, 0700); err != nil {
		return "", err
	}
	dst := filepath.Join(dstDir, wanted)
	if err := downloadHTTPS(asset, dst); err != nil {
		return "", err
	}
	actual, err := sha256File(dst)
	if err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	if !strings.EqualFold(actual, expected[0]) {
		_ = os.Remove(dst)
		return "", fmt.Errorf("Release-Prüfsumme stimmt nicht")
	}
	_ = openPath(dstDir)
	return dst, nil
}

func releasePackageName(goos, arch string) string {
	switch goos {
	case "darwin":
		if arch == "amd64" || arch == "arm64" {
			return "IServ-reMarkable-macos.dmg"
		}
	case "windows":
		if arch == "amd64" {
			return "IServ-reMarkable-windows-amd64-Setup.exe"
		}
		if arch == "arm64" {
			return "IServ-reMarkable-windows-arm64-Setup.exe"
		}
	case "linux":
		if arch == "amd64" || arch == "arm64" {
			return "IServ-reMarkable-linux-" + arch + ".tar.gz"
		}
	}
	return ""
}
