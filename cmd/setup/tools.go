package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	managedFlyVersion     = "0.5.7"
	managedNodeVersion    = "22.23.4"
	maxExtractedToolBytes = int64(1 << 30)
	maxToolArchiveEntries = 200000
)

type ToolManager struct {
	root string
	mu   sync.Mutex
}

func NewToolManager(workDir string) *ToolManager {
	return &ToolManager{root: filepath.Join(workDir, "tools")}
}

func (t *ToolManager) FlyPath(logf func(string, ...any)) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	name := "flyctl"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst := filepath.Join(t.root, "flyctl-"+managedFlyVersion, name)
	if isExecutableFile(dst) {
		return dst, nil
	}
	osName, arch, ext, err := flyAssetParts()
	if err != nil {
		return "", err
	}
	asset := fmt.Sprintf("flyctl_%s_%s_%s.%s", managedFlyVersion, osName, arch, ext)
	base := "https://github.com/superfly/flyctl/releases/download/v" + managedFlyVersion + "/"
	if logf != nil {
		logf("Fly.io-Werkzeug wird sicher heruntergeladen …")
	}
	archive, err := t.downloadGitHubReleaseAssetVerified("superfly/flyctl", "v"+managedFlyVersion, asset, base+asset)
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(t.root, "fly-extract-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if strings.HasSuffix(asset, ".zip") {
		err = extractZipSafe(archive, tmp)
	} else {
		err = extractTarGzSafe(archive, tmp)
	}
	if err != nil {
		return "", err
	}
	found := findNamedFile(tmp, name)
	if found == "" {
		return "", fmt.Errorf("Fly.io-Werkzeug fehlt im geprüften Archiv")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return "", err
	}
	if err := copyFileMode(found, dst, 0700); err != nil {
		return "", err
	}
	return dst, nil
}

func (t *ToolManager) NodePaths(logf func(string, ...any)) (nodePath, npmCLI string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	folder := filepath.Join(t.root, "node-v"+managedNodeVersion)
	nodeName := "node"
	if runtime.GOOS == "windows" {
		nodeName = "node.exe"
		nodePath = filepath.Join(folder, nodeName)
		npmCLI = filepath.Join(folder, "node_modules", "npm", "bin", "npm-cli.js")
	} else {
		nodePath = filepath.Join(folder, "bin", nodeName)
		npmCLI = filepath.Join(folder, "lib", "node_modules", "npm", "bin", "npm-cli.js")
	}
	if isExecutableFile(nodePath) {
		if _, e := os.Stat(npmCLI); e == nil {
			return nodePath, npmCLI, nil
		}
	}
	osName, arch, ext, err := nodeAssetParts()
	if err != nil {
		return "", "", err
	}
	asset := fmt.Sprintf("node-v%s-%s-%s.%s", managedNodeVersion, osName, arch, ext)
	base := "https://nodejs.org/dist/v" + managedNodeVersion + "/"
	if logf != nil {
		logf("Cloudflare-Laufzeit wird sicher heruntergeladen …")
	}
	archive, err := t.downloadVerified(base+asset, base+"SHASUMS256.txt", asset)
	if err != nil {
		return "", "", err
	}
	tmp, err := os.MkdirTemp(t.root, "node-extract-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmp)
	if strings.HasSuffix(asset, ".zip") {
		err = extractZipSafe(archive, tmp)
	} else {
		err = extractTarGzSafe(archive, tmp)
	}
	if err != nil {
		return "", "", err
	}
	root := findNodeRoot(tmp, nodeName)
	if root == "" {
		return "", "", fmt.Errorf("Node.js fehlt im geprüften Archiv")
	}
	_ = os.RemoveAll(folder)
	if err := copyTreeSafe(root, folder); err != nil {
		return "", "", err
	}
	if err := os.Chmod(nodePath, 0700); err != nil {
		return "", "", fmt.Errorf("Node.js konnte nicht ausführbar gemacht werden: %w", err)
	}
	if _, err := os.Stat(npmCLI); err != nil {
		return "", "", fmt.Errorf("npm fehlt im geprüften Node.js-Archiv")
	}
	return nodePath, npmCLI, nil
}

var flyReleaseSHA256 = map[string]string{
	"flyctl_0.5.7_Linux_arm64.tar.gz":  "01cb77e62cc00b61f20c31a070c462b61733dd7d6a2c5aee7e5287d41b4ec1c8",
	"flyctl_0.5.7_Linux_x86_64.tar.gz": "1b340c3f326abc34659b7bc1f2eed69eac31115c9796e325596e27687ea421a5",
	"flyctl_0.5.7_Windows_arm64.zip":   "cc83d999a7ef938b445010207272e46ec7c2168c34a6cf0251910ffa2bc1d4ba",
	"flyctl_0.5.7_Windows_x86_64.zip":  "20d7f3784df7291b4c19904edfe2b3e98371b56e9619ef9983fce1cf8fc90938",
	"flyctl_0.5.7_macOS_arm64.tar.gz":  "8550445844e24a77412878777461bba2e8aa12b1e4dfd57603fe72b77bbc2242",
	"flyctl_0.5.7_macOS_x86_64.tar.gz": "18f4551bfee40121a1db2188644520c67b4e9ab761596c0a096d74f413d32f1d",
}

func (t *ToolManager) downloadGitHubReleaseAssetVerified(repo, tag, assetName, expectedURL string) (string, error) {
	if repo != "superfly/flyctl" || tag != "v"+managedFlyVersion || assetName == "" {
		return "", fmt.Errorf("ungültige Fly.io-Release-Anforderung")
	}
	base := "https://github.com/superfly/flyctl/releases/download/v" + managedFlyVersion + "/"
	if expectedURL != base+assetName {
		return "", fmt.Errorf("Fly.io-Release-Asset hat eine unerwartete URL")
	}
	expected, ok := flyReleaseSHA256[assetName]
	if !ok || len(expected) != 64 {
		return "", fmt.Errorf("keine fest gepinnte SHA-256-Prüfsumme für %s", assetName)
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return "", fmt.Errorf("ungültige fest gepinnte SHA-256-Prüfsumme für %s", assetName)
	}
	if err := os.MkdirAll(filepath.Join(t.root, "downloads"), 0700); err != nil {
		return "", err
	}
	dst := filepath.Join(t.root, "downloads", assetName)
	if err := downloadHTTPS(expectedURL, dst); err != nil {
		return "", err
	}
	actual, err := sha256File(dst)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(actual, expected) {
		_ = os.Remove(dst)
		return "", fmt.Errorf("Sicherheitsprüfung für %s fehlgeschlagen", assetName)
	}
	return dst, nil
}

func (t *ToolManager) downloadVerified(assetURL, checksumURL, assetName string) (string, error) {
	if err := os.MkdirAll(t.root, 0700); err != nil {
		return "", err
	}
	checks, err := fetchSmallHTTPS(checksumURL, 2<<20)
	if err != nil {
		return "", fmt.Errorf("Prüfsummen konnten nicht geladen werden: %w", err)
	}
	expected := ""
	s := bufio.NewScanner(strings.NewReader(string(checks)))
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) >= 2 && strings.TrimPrefix(fields[len(fields)-1], "*") == assetName {
			expected = fields[0]
			break
		}
	}
	if len(expected) != 64 {
		return "", fmt.Errorf("keine gültige SHA-256-Prüfsumme für %s gefunden", assetName)
	}
	dst := filepath.Join(t.root, "downloads", assetName)
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return "", err
	}
	if err := downloadHTTPS(assetURL, dst); err != nil {
		return "", err
	}
	actual, err := sha256File(dst)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(actual, expected) {
		_ = os.Remove(dst)
		return "", fmt.Errorf("Sicherheitsprüfung für %s fehlgeschlagen", assetName)
	}
	return dst, nil
}

func flyAssetParts() (string, string, string, error) {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	if arch != "x86_64" && arch != "arm64" {
		return "", "", "", fmt.Errorf("CPU-Architektur %s wird noch nicht unterstützt", runtime.GOARCH)
	}
	switch runtime.GOOS {
	case "darwin":
		return "macOS", arch, "tar.gz", nil
	case "linux":
		return "Linux", arch, "tar.gz", nil
	case "windows":
		return "Windows", arch, "zip", nil
	default:
		return "", "", "", fmt.Errorf("Betriebssystem %s wird noch nicht unterstützt", runtime.GOOS)
	}
}

func nodeAssetParts() (string, string, string, error) {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	if arch != "x64" && arch != "arm64" {
		return "", "", "", fmt.Errorf("CPU-Architektur %s wird noch nicht unterstützt", runtime.GOARCH)
	}
	switch runtime.GOOS {
	case "darwin":
		return "darwin", arch, "tar.gz", nil
	case "linux":
		return "linux", arch, "tar.gz", nil
	case "windows":
		return "win", arch, "zip", nil
	default:
		return "", "", "", fmt.Errorf("Betriebssystem %s wird noch nicht unterstützt", runtime.GOOS)
	}
}

func secureHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("zu viele Weiterleitungen")
			}
			if !strings.EqualFold(req.URL.Scheme, "https") {
				return fmt.Errorf("unsichere HTTP-Weiterleitung blockiert")
			}
			return nil
		},
	}
}

func fetchSmallHTTPS(rawURL string, limit int64) ([]byte, error) {
	if !strings.HasPrefix(strings.ToLower(rawURL), "https://") {
		return nil, fmt.Errorf("nur HTTPS ist erlaubt")
	}
	client := secureHTTPClient(45 * time.Second)
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("Antwort ist unerwartet groß")
	}
	return data, nil
}

func downloadHTTPS(rawURL, dst string) error {
	if !strings.HasPrefix(strings.ToLower(rawURL), "https://") {
		return fmt.Errorf("nur HTTPS ist erlaubt")
	}
	client := secureHTTPClient(5 * time.Minute)
	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Download meldet HTTP %d", resp.StatusCode)
	}
	const maxDownloadBytes int64 = 512 << 20
	if resp.ContentLength > maxDownloadBytes {
		return fmt.Errorf("Download ist unerwartet groß")
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".part-*")
	if err != nil {
		return err
	}
	tmp := tmpFile.Name()
	defer os.Remove(tmp)
	if err := tmpFile.Chmod(0600); err != nil {
		_ = tmpFile.Close()
		return err
	}
	written, cpErr := io.Copy(tmpFile, io.LimitReader(resp.Body, maxDownloadBytes+1))
	if cpErr == nil && written > maxDownloadBytes {
		cpErr = fmt.Errorf("Download ist unerwartet groß")
	}
	if cpErr == nil {
		cpErr = tmpFile.Sync()
	}
	closeErr := tmpFile.Close()
	if cpErr != nil {
		return cpErr
	}
	if closeErr != nil {
		return closeErr
	}
	return atomicReplace(tmp, dst)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func safeJoin(root, name string) (string, error) {
	// Archive names are provider-controlled input. Reject rooted, drive/UNC,
	// alternate-data-stream and backslash forms before platform normalization so
	// the same archive is safe on Unix and Windows. filepath.IsAbs alone is not
	// sufficient on Windows: a path such as \"\\absolute\" is rooted but has no
	// volume and therefore is not reported as absolute.
	if name == "" || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") || strings.ContainsAny(name, "\\:") {
		return "", fmt.Errorf("unsicherer Archivpfad")
	}
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if cleaned == "." || filepath.IsAbs(cleaned) || filepath.VolumeName(cleaned) != "" || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) || cleaned == ".." {
		return "", fmt.Errorf("unsicherer Archivpfad")
	}
	joined := filepath.Join(root, cleaned)
	rel, err := filepath.Rel(root, joined)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsicherer Archivpfad")
	}
	return joined, nil
}

func extractZipSafe(archive, dst string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	if len(r.File) > maxToolArchiveEntries {
		return fmt.Errorf("Werkzeugarchiv enthält unerwartet viele Einträge")
	}
	var extracted uint64
	for _, f := range r.File {
		if f.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !f.FileInfo().IsDir() {
			size := f.UncompressedSize64
			limit := uint64(maxExtractedToolBytes)
			if size > limit || extracted > limit-size {
				return fmt.Errorf("Werkzeugarchiv ist entpackt unerwartet groß")
			}
			extracted += size
		}
		out, err := safeJoin(dst, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(out, 0700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		wf, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			rc.Close()
			return err
		}
		_, e1 := io.Copy(wf, rc)
		e2 := wf.Close()
		e3 := rc.Close()
		if e1 != nil {
			return e1
		}
		if e2 != nil {
			return e2
		}
		if e3 != nil {
			return e3
		}
	}
	return nil
}

func extractTarGzSafe(archive, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	entries := 0
	var extracted int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		entries++
		if entries > maxToolArchiveEntries {
			return fmt.Errorf("Werkzeugarchiv enthält unerwartet viele Einträge")
		}
		if h.Typeflag == tar.TypeSymlink || h.Typeflag == tar.TypeLink {
			continue
		}
		if h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA {
			if h.Size < 0 || h.Size > maxExtractedToolBytes || extracted > maxExtractedToolBytes-h.Size {
				return fmt.Errorf("Werkzeugarchiv ist entpackt unerwartet groß")
			}
			extracted += h.Size
		}
		out, err := safeJoin(dst, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(out, 0700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
				return err
			}
			wf, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(wf, tr); err != nil {
				wf.Close()
				return err
			}
			if err := wf.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

func findNamedFile(root, name string) string {
	var result string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(d.Name(), name) && result == "" {
			result = p
		}
		return nil
	})
	return result
}
func findNodeRoot(root, nodeName string) string {
	p := findNamedFile(root, nodeName)
	if p == "" {
		return ""
	}
	dir := filepath.Dir(p)
	if strings.EqualFold(filepath.Base(dir), "bin") {
		return filepath.Dir(dir)
	}
	return dir
}
func isExecutableFile(p string) bool { info, err := os.Stat(p); return err == nil && !info.IsDir() }
func copyFileMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, e1 := io.Copy(out, in)
	e2 := out.Close()
	if e1 != nil {
		return e1
	}
	return e2
}
func copyTreeSafe(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		out, err := safeJoin(dst, filepath.ToSlash(rel))
		if err != nil && rel != "." {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0700)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(out, 0700)
		}
		mode := os.FileMode(0600)
		if info, e := d.Info(); e == nil && info.Mode()&0111 != 0 {
			mode = 0700
		}
		return copyFileMode(p, out, mode)
	})
}

func bundledRemarkablePath() (string, error) {
	name := "iserv-rm-folder-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	exe, _ := os.Executable()
	base := filepath.Dir(exe)
	candidates := []string{filepath.Join(base, name), filepath.Join(base, "helpers", name), filepath.Join(base, "..", "Resources", name)}
	for _, p := range candidates {
		if isExecutableFile(p) {
			return p, nil
		}
	}
	// Development fallback: existing preview testers may already have remarquee.
	legacy := "remarquee"
	if runtime.GOOS == "windows" {
		legacy += ".exe"
	}
	if p, err := exec.LookPath(legacy); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("reMarkable-Helfer fehlt in diesem Paket")
}
