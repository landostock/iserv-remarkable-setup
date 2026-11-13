//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

func showWindowsDialog(msg string, flags uintptr) {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	text, _ := syscall.UTF16PtrFromString(msg)
	title, _ := syscall.UTF16PtrFromString("IServ → reMarkable")
	proc.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), flags)
}

func showFatalDialog(msg string) { showWindowsDialog(msg, 0x10) }
func showInfoDialog(msg string)  { showWindowsDialog(msg, 0x40) }
