//go:build !windows

package main

import "os"

func atomicReplace(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
