//go:build linux

package main

import (
	"os/exec"
)

func showFatalDialog(msg string) {
	if p, e := exec.LookPath("zenity"); e == nil {
		_ = exec.Command(p, "--error", "--title=IServ → reMarkable", "--text="+msg).Run()
		return
	}
	if p, e := exec.LookPath("kdialog"); e == nil {
		_ = exec.Command(p, "--error", msg, "--title", "IServ → reMarkable").Run()
	}
}

func showInfoDialog(msg string) {
	if p, e := exec.LookPath("zenity"); e == nil {
		_ = exec.Command(p, "--info", "--title=IServ → reMarkable", "--text="+msg).Run()
		return
	}
	if p, e := exec.LookPath("kdialog"); e == nil {
		_ = exec.Command(p, "--msgbox", msg, "--title", "IServ → reMarkable").Run()
	}
}
