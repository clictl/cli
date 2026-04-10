// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Pack creates a .tar.gz archive from a content directory and manifest.
// Returns the path to the created archive.
func Pack(contentDir string, manifest map[string]interface{}, outputDir string) (string, error) {
	name, _ := manifest["name"].(string)
	version, _ := manifest["version"].(string)
	if name == "" || version == "" {
		return "", fmt.Errorf("manifest must have name and version")
	}

	filename := fmt.Sprintf("%s-%s.tar.gz", name, version)
	archivePath := filepath.Join(outputDir, filename)

	f, err := os.Create(archivePath)
	if err != nil {
		return "", fmt.Errorf("creating archive: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Write manifest.yaml
	manifestBytes, err := yaml.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("marshaling manifest: %w", err)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name: "manifest.yaml",
		Size: int64(len(manifestBytes)),
		Mode: 0644,
	}); err != nil {
		return "", err
	}
	if _, err := tw.Write(manifestBytes); err != nil {
		return "", err
	}

	// Write content/ directory
	err = filepath.WalkDir(contentDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(contentDir, path)
		arcName := "content/" + filepath.ToSlash(relPath)

		info, err := d.Info()
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = arcName

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(tw, file)
		return err
	})

	return archivePath, err
}
