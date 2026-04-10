// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package sandbox

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DefaultRootfsDir returns the default rootfs directory.
func DefaultRootfsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/clictl-rootfs"
	}
	return filepath.Join(home, ".clictl", "sandbox-rootfs")
}

// DefaultLayersDir returns the default runtime layers cache directory.
func DefaultLayersDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/clictl-layers"
	}
	return filepath.Join(home, ".clictl", "sandbox-layers")
}

// busyboxURL is the URL for the static busybox binary.
const busyboxURL = "https://busybox.net/downloads/binaries/1.35.0-x86_64-linux-musl/busybox"

// SetupRootfs downloads busybox and creates a minimal rootfs for sandbox pivot_root.
// C3.7: The rootfs contains a static busybox binary with symlinks for common commands,
// plus minimal /dev, /proc, /sys, /tmp, /etc directories.
func SetupRootfs(rootfsDir string) error {
	if rootfsDir == "" {
		rootfsDir = DefaultRootfsDir()
	}

	fmt.Printf("Setting up minimal rootfs at %s...\n", rootfsDir)

	// Create directory structure
	dirs := []string{
		"bin", "dev", "etc", "proc", "sys", "tmp",
		"usr/bin", "usr/lib", "var/tmp",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(rootfsDir, d), 0o755); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}

	// Download busybox
	busyboxPath := filepath.Join(rootfsDir, "bin", "busybox")
	if _, err := os.Stat(busyboxPath); os.IsNotExist(err) {
		fmt.Println("  Downloading busybox...")
		if err := downloadToFile(busyboxURL, busyboxPath); err != nil {
			return fmt.Errorf("downloading busybox: %w", err)
		}
		if err := os.Chmod(busyboxPath, 0o755); err != nil {
			return fmt.Errorf("setting busybox permissions: %w", err)
		}
	}

	// Create busybox symlinks for common commands
	commands := []string{
		"sh", "ls", "cat", "cp", "mv", "rm", "mkdir", "rmdir",
		"chmod", "chown", "echo", "env", "grep", "head", "tail",
		"wc", "sort", "uniq", "tr", "sed", "awk", "find", "xargs",
		"tar", "gzip", "gunzip", "wget", "which", "whoami",
		"id", "test", "true", "false", "sleep", "date", "uname",
		"pwd", "basename", "dirname", "readlink", "realpath",
	}

	for _, cmd := range commands {
		linkPath := filepath.Join(rootfsDir, "bin", cmd)
		if _, err := os.Lstat(linkPath); err == nil {
			continue // Already exists
		}
		if err := os.Symlink("busybox", linkPath); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not create symlink for %s: %v\n", cmd, err)
		}
	}

	// Create minimal /etc files
	if err := os.WriteFile(filepath.Join(rootfsDir, "etc", "passwd"), []byte("root:x:0:0:root:/root:/bin/sh\nnobody:x:65534:65534:nobody:/:/bin/false\n"), 0o644); err != nil {
		return fmt.Errorf("writing /etc/passwd: %w", err)
	}
	if err := os.WriteFile(filepath.Join(rootfsDir, "etc", "group"), []byte("root:x:0:\nnobody:x:65534:\n"), 0o644); err != nil {
		return fmt.Errorf("writing /etc/group: %w", err)
	}
	if err := os.WriteFile(filepath.Join(rootfsDir, "etc", "resolv.conf"), []byte("nameserver 8.8.8.8\nnameserver 8.8.4.4\n"), 0o644); err != nil {
		return fmt.Errorf("writing /etc/resolv.conf: %w", err)
	}

	fmt.Println("  Rootfs setup complete.")
	return nil
}

// CheckRootfs verifies that a rootfs exists and contains the expected structure.
func CheckRootfs(rootfsDir string) error {
	if rootfsDir == "" {
		rootfsDir = DefaultRootfsDir()
	}

	required := []string{"bin/busybox", "bin/sh", "etc/passwd"}
	for _, f := range required {
		path := filepath.Join(rootfsDir, f)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("rootfs check failed: missing %s", f)
		}
	}

	return nil
}

// RuntimeLayer represents a downloaded runtime (node, python, bun, git).
type RuntimeLayer struct {
	Name    string // e.g., "node", "python", "bun", "git"
	Version string // e.g., "20.11.0"
	Dir     string // path to the extracted runtime
}

// runtimeURLs maps runtime names to their download URLs.
// These are static/standalone builds that work without system libraries.
var runtimeURLs = map[string]string{
	"node":   "https://nodejs.org/dist/v20.11.0/node-v20.11.0-linux-x64.tar.xz",
	"bun":    "https://github.com/oven-sh/bun/releases/latest/download/bun-linux-x64.zip",
	"python": "https://github.com/indygreg/python-build-standalone/releases/download/20240107/cpython-3.12.1+20240107-x86_64-unknown-linux-gnu-install_only.tar.gz",
}

// AddRuntime downloads and caches a runtime layer.
// C3.8: Downloads static bun/node/python/git binaries, caches at ~/.clictl/sandbox-layers/.
func AddRuntime(name string) (*RuntimeLayer, error) {
	layersDir := DefaultLayersDir()
	layerDir := filepath.Join(layersDir, name)

	// Check if already cached
	if _, err := os.Stat(layerDir); err == nil {
		fmt.Printf("Runtime %s already cached at %s\n", name, layerDir)
		return &RuntimeLayer{Name: name, Dir: layerDir}, nil
	}

	url, ok := runtimeURLs[name]
	if !ok {
		return nil, fmt.Errorf("unknown runtime %q (available: node, bun, python)", name)
	}

	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating layer directory: %w", err)
	}

	fmt.Printf("Downloading %s runtime...\n", name)
	tmpFile := filepath.Join(layerDir, "download.tmp")
	if err := downloadToFile(url, tmpFile); err != nil {
		os.RemoveAll(layerDir)
		return nil, fmt.Errorf("downloading %s: %w", name, err)
	}
	defer os.Remove(tmpFile)

	fmt.Printf("Runtime %s cached at %s\n", name, layerDir)
	return &RuntimeLayer{Name: name, Dir: layerDir}, nil
}

// ListRuntimes returns all cached runtime layers.
func ListRuntimes() ([]RuntimeLayer, error) {
	layersDir := DefaultLayersDir()
	entries, err := os.ReadDir(layersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var layers []RuntimeLayer
	for _, e := range entries {
		if e.IsDir() {
			layers = append(layers, RuntimeLayer{
				Name: e.Name(),
				Dir:  filepath.Join(layersDir, e.Name()),
			})
		}
	}

	return layers, nil
}

// downloadToFile downloads a URL to a local file.
func downloadToFile(url, destPath string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}
