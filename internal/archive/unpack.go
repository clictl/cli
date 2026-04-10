// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Unpack extracts a .tar.gz pack archive to a target directory.
// Returns the parsed manifest. Validates against path traversal attacks.
func Unpack(archivePath string, targetDir string) (map[string]interface{}, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("opening archive: %w", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar: %w", err)
		}

		// Security: reject path traversal and symlinks
		if strings.Contains(header.Name, "..") || strings.HasPrefix(header.Name, "/") {
			return nil, fmt.Errorf("unsafe path in archive: %s", header.Name)
		}
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			return nil, fmt.Errorf("symlinks not allowed in packs: %s", header.Name)
		}

		target := filepath.Join(targetDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return nil, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return nil, err
			}
			out, err := os.Create(target)
			if err != nil {
				return nil, err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return nil, err
			}
			out.Close()
		}
	}

	// Parse manifest
	manifestPath := filepath.Join(targetDir, "manifest.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("pack is missing manifest.yaml")
	}

	var manifest map[string]interface{}
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	return manifest, nil
}

// VerifyPackContent verifies the content/ directory hash matches the expected hash.
func VerifyPackContent(targetDir string, expectedHash string) error {
	contentDir := filepath.Join(targetDir, "content")
	info, err := os.Stat(contentDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("pack is missing content/ directory")
	}
	actualHash, err := HashDirectory(contentDir)
	if err != nil {
		return fmt.Errorf("computing content hash: %w", err)
	}
	if actualHash != expectedHash {
		return fmt.Errorf("content hash mismatch: expected %s, got %s", expectedHash, actualHash)
	}
	return nil
}
