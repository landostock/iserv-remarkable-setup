package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

//go:embed web/index.html web/mappings.js
//go:embed payload/Dockerfile payload/config.json payload/fly.toml.template payload/rmapi_safe_api.go.txt payload/sync_job.py
//go:embed payload/worker/package.json payload/worker/package-lock.json payload/worker/wrangler.jsonc.template payload/worker/wrangler.bootstrap.jsonc.template
//go:embed payload/worker/src/*.js
var embedded embed.FS

func fatalExit(err error) {
	if err == nil {
		return
	}
	log.Printf("fatal: %v", err)
	showFatalDialog(err.Error())
	os.Exit(1)
}

func main() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		fatalExit(err)
	}
	workDir := filepath.Join(configDir, "IServ-reMarkable-Setup")
	if err := os.MkdirAll(workDir, 0700); err != nil {
		fatalExit(err)
	}
	if err := os.Chmod(workDir, 0700); err != nil {
		fatalExit(err)
	}
	if err := extractPayload(workDir); err != nil {
		fatalExit(err)
	}
	token, err := randomHex(24)
	if err != nil {
		fatalExit(err)
	}
	app := NewApp(workDir, token)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatalExit(err)
	}
	localHost := ln.Addr().String()
	url := "http://" + localHost + "/?token=" + token
	server := &http.Server{
		Handler:           localRequestGuard(localHost, app.polishedRootHandler()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	go func() {
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("server: %v", err)
		}
	}()
	fmt.Println("IServ → reMarkable Setup läuft lokal auf", "http://"+localHost+"/")
	if err := openBrowser(url); err != nil {
		log.Printf("browser: %v", err)
		showInfoDialog("Der Browser konnte nicht automatisch geöffnet werden.\n\nBitte öffne diese Adresse in deinem Browser:\n" + url + "\n\nBrowser could not be opened automatically.\n\nPlease open this address in your browser:\n" + url)
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	select {
	case <-ch:
	case <-app.quit:
	}
	_ = server.Close()
}

func extractPayload(workDir string) error {
	root := filepath.Join(workDir, "runtime")
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	if err := os.Chmod(root, 0700); err != nil {
		return err
	}
	return fs.WalkDir(embedded, "payload", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, "payload")
		if rel == "" {
			return nil
		}
		dst := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(rel, "/")))
		if d.IsDir() {
			if err := os.MkdirAll(dst, 0700); err != nil {
				return err
			}
			return os.Chmod(dst, 0700)
		}
		raw, err := embedded.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, raw, 0600)
	})
}

func loadExistingMeta(workDir string) (*installMeta, bool) {
	raw, err := os.ReadFile(filepath.Join(workDir, "installation.json"))
	if os.IsNotExist(err) {
		return nil, false
	}
	if err != nil {
		return nil, true
	}
	var meta installMeta
	if json.Unmarshal(raw, &meta) != nil {
		return nil, true
	}
	base, err := normalizeDashboardURL(meta.DashboardURL)
	if err != nil || base != meta.DashboardURL || !cloudNamePattern.MatchString(meta.AppName) || !cloudNamePattern.MatchString(meta.WorkerName) || !flyRegionPattern.MatchString(meta.Region) || !flyMachinePattern.MatchString(meta.MachineID) || !kvNamespacePattern.MatchString(meta.KVNamespaceID) {
		return nil, true
	}
	if meta.FlyTokenUntil != "" {
		if _, err := time.Parse(time.RFC3339, meta.FlyTokenUntil); err != nil {
			return nil, true
		}
	}
	return &meta, false
}
