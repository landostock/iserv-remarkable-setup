package main

import (
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"
)

type preflightCheck struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func supportedPlatform() (bool, string) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		return false, fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return false, fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return true, fmt.Sprintf("%s / %s", runtime.GOOS, runtime.GOARCH)
}

func managedFlyChecksumURL() string {
	return "https://github.com/superfly/flyctl/releases/download/v" + managedFlyVersion + "/flyctl_" + managedFlyVersion + "_checksums.txt"
}

func probeHTTPS(url, lang string) preflightCheck {
	client := secureHTTPClient(12 * time.Second)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "IServ-reMarkable-Setup/"+appVersion)
	resp, err := client.Do(req)
	if err != nil {
		return preflightCheck{false, err.Error()}
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return preflightCheck{false, fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	if lang == "en" {
		return preflightCheck{true, "reachable"}
	}
	return preflightCheck{true, "erreichbar"}
}

func runPreflight(lang string) map[string]any {
	ok, platform := supportedPlatform()
	result := map[string]any{"os": runtime.GOOS, "arch": runtime.GOARCH, "platform": preflightCheck{ok, platform}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	checks := map[string]string{
		"fly":        managedFlyChecksumURL(),
		"node":       "https://nodejs.org/dist/v" + managedNodeVersion + "/SHASUMS256.txt",
		"cloudflare": "https://registry.npmjs.org/wrangler",
	}
	for name, url := range checks {
		wg.Add(1)
		go func(n, u string) { defer wg.Done(); c := probeHTTPS(u, lang); mu.Lock(); result[n] = c; mu.Unlock() }(name, url)
	}
	wg.Wait()
	if _, err := bundledRemarkablePath(); err == nil {
		if lang == "en" {
			result["remarquee"] = preflightCheck{true, "bundled"}
		} else {
			result["remarquee"] = preflightCheck{true, "integriert"}
		}
	} else {
		if lang == "en" {
			result["remarquee"] = preflightCheck{false, "helper is missing from this development build"}
		} else {
			result["remarquee"] = preflightCheck{false, "Helfer fehlt in dieser Entwicklungsdatei"}
		}
	}
	all := ok
	for _, k := range []string{"fly", "node", "cloudflare", "remarquee"} {
		if c, yes := result[k].(preflightCheck); !yes || !c.OK {
			all = false
		}
	}
	result["ok"] = all
	return result
}
