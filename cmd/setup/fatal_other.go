//go:build !windows && !darwin && !linux

package main

func showFatalDialog(msg string) {}
func showInfoDialog(msg string)  {}
