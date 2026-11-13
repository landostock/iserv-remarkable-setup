package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

func lookPathAny(names ...string) (string, error) {
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("nicht gefunden: %s", strings.Join(names, ", "))
}

func flyPath() (string, error)       { return lookPathAny("fly", "flyctl") }
func npxPath() (string, error)       { return lookPathAny("npx", "npx.cmd") }
func nodePath() (string, error)      { return lookPathAny("node", "node.exe") }
func remarqueePath() (string, error) { return lookPathAny("remarquee", "remarquee.exe") }

var commandSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)FlyV1\s+[^\s]+`),
	regexp.MustCompile(`(?i)Bearer\s+[^\s]+`),
	regexp.MustCompile(`(?i)((?:RCLONE_CONFIG_ISERV_(?:URL|USER|PASS)|ISERV_WEBDAV_USER_B64|RMAPI_CONFIG_B64|ADMIN_PASSWORD|SYNC_API_TOKEN|FLY_API_TOKEN)\s*[:=]\s*)[^\s]+`),
	regexp.MustCompile(`(?i)((?:password|passwort|pass|token|secret|devicetoken|usertoken|config_b64)\s*[:=]\s*)[^\s]+`),
}

var commandEscapePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\x1b\][^\x07]*(?:\x07|\x1b\\)`),
	regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`),
}

var portableHomePathPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:/Users|/home)/[^/\\\s"']+`),
	regexp.MustCompile(`(?i)[A-Z]:\\Users\\[^/\\\s"']+`),
}

func redactHomePaths(value string) string {
	out := value
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		out = strings.ReplaceAll(out, home, "~")
	}
	for _, re := range portableHomePathPatterns {
		out = re.ReplaceAllString(out, "~")
	}
	return out
}

func cleanCommandOutput(value string) string {
	out := value
	for _, re := range commandEscapePatterns {
		out = re.ReplaceAllString(out, "")
	}
	return strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\n' && r != '\r' && r != '\t') || r == 0x7f {
			return -1
		}
		return r
	}, out)
}

func redactCommandOutput(value string) string {
	out := redactHomePaths(cleanCommandOutput(value))
	for index, re := range commandSecretPatterns {
		if index < 2 {
			out = re.ReplaceAllString(out, "[redacted credential]")
		} else {
			out = re.ReplaceAllString(out, `${1}[redacted]`)
		}
	}
	return out
}

func commandFailureOutput(value string) (string, string) {
	redacted := redactCommandOutput(value)
	lines := strings.Split(redacted, "\n")
	useful := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "at ") || strings.HasPrefix(trimmed, "Node.js v") {
			continue
		}
		useful = append(useful, line)
	}
	text := strings.TrimSpace(strings.Join(useful, "\n"))
	runes := []rune(text)
	if len(runes) > 800 {
		text = string(runes[:470]) + "\n...\n" + string(runes[len(runes)-320:])
	}
	return redacted, text
}

func runCommand(ctx context.Context, dir, command string, args []string, stdin string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		return commandRunFailure(ctx, out.String(), err)
	}
	return out.String(), nil
}

func commandRunFailure(ctx context.Context, output string, runErr error) (string, error) {
	redacted, text := commandFailureOutput(output)
	if ctxErr := ctx.Err(); ctxErr != nil {
		if text == "" {
			return redacted, ctxErr
		}
		return redacted, fmt.Errorf("%w: %s", ctxErr, text)
	}
	if text == "" {
		text = runErr.Error()
	}
	return redacted, fmt.Errorf("%s", text)
}

func runWithTimeout(dir, command string, args []string, stdin string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return runCommand(ctx, dir, command, args, stdin)
}

func runWithTimeoutEnv(dir, command string, args []string, stdin string, timeout time.Duration, extraEnv map[string]string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = commandEnvironment(extraEnv)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		return commandRunFailure(ctx, out.String(), err)
	}
	return out.String(), nil
}

func commandEnvironment(extra map[string]string) []string {
	env := os.Environ()
	if len(extra) == 0 {
		return env
	}
	keys := make(map[string]struct{}, len(extra))
	for key := range extra {
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		keys[key] = struct{}{}
	}
	merged := make([]string, 0, len(env)+len(extra))
	for _, entry := range env {
		key := entry
		if index := strings.IndexByte(entry, '='); index >= 0 {
			key = entry[:index]
		}
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		if _, replaced := keys[key]; !replaced {
			merged = append(merged, entry)
		}
	}
	for key, value := range extra {
		merged = append(merged, key+"="+value)
	}
	return merged
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		for _, candidate := range [][]string{{"xdg-open", url}, {"gio", "open", url}, {"sensible-browser", url}, {"firefox", url}, {"google-chrome", url}, {"chromium", url}} {
			if p, err := exec.LookPath(candidate[0]); err == nil {
				cmd = exec.Command(p, candidate[1:]...)
				break
			}
		}
		if cmd == nil {
			return fmt.Errorf("kein Browser-Öffner gefunden")
		}
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Start()
}

func openPath(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("explorer.exe", path)
	default:
		for _, candidate := range [][]string{{"xdg-open", path}, {"gio", "open", path}} {
			if p, err := exec.LookPath(candidate[0]); err == nil {
				cmd = exec.Command(p, candidate[1:]...)
				break
			}
		}
		if cmd == nil {
			return fmt.Errorf("kein Datei-Öffner gefunden")
		}
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Start()
}
