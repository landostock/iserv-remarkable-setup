package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"strings"
)

// RC27 changes the local editor only. RC26 cloud deployments remain safe to
// use without redeploying or discarding a successful real-run verification.
// Any deployed payload change automatically closes this compatibility path.
const compatibleRC26PayloadDigest = "b4c3ba09226245f5d6ef1baa0eba067b46a4356f9f602d94d65be80f5121e551"

var currentCloudPayloadDigest = func() string {
	hash := sha256.New()
	err := fs.WalkDir(embedded, "payload", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || strings.HasSuffix(path, ".test.js") {
			return nil
		}
		raw, err := embedded.ReadFile(path)
		if err != nil {
			return err
		}
		hash.Write([]byte(strings.TrimPrefix(path, "payload/") + "\x00"))
		hash.Write(raw)
		hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}()
