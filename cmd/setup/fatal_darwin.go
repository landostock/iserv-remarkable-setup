//go:build darwin

package main

import (
	"os/exec"
	"strings"
)

func escapeAppleScript(msg string) string {
	msg = strings.ReplaceAll(msg, "\\", "\\\\")
	return strings.ReplaceAll(msg, "\"", "\\\"")
}

func showFatalDialog(msg string) {
	msg = escapeAppleScript(msg)
	_ = exec.Command("osascript", "-e", `display dialog "`+msg+`" with title "IServ → reMarkable" buttons {"OK"} default button "OK" with icon stop`).Run()
}

func showInfoDialog(msg string) {
	msg = escapeAppleScript(msg)
	_ = exec.Command("osascript", "-e", `display dialog "`+msg+`" with title "IServ → reMarkable" buttons {"OK"} default button "OK" with icon note`).Run()
}
