// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package archive

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HashDirectory computes a deterministic Merkle-style SHA256 of a directory tree.
// Files are walked in sorted order. Hash is of "path\0hash\n" for each file.
func HashDirectory(dir string) (string, error) {
	var entries []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		// Normalize to forward slashes for cross-platform consistency
		relPath = filepath.ToSlash(relPath)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fileHash := sha256.Sum256(data)
		entries = append(entries, fmt.Sprintf("%s\x00%x", relPath, fileHash))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(entries)
	treeHash := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	return fmt.Sprintf("%x", treeHash), nil
}
