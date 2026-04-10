// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
)

// SensitiveDirs returns absolute paths to directories that should never be
// accessible to sandboxed MCP server subprocesses. These contain credentials,
// keys, browser sessions, and wallet data that supply chain attacks target.
func SensitiveDirs() []string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}

	// Cross-platform sensitive directories
	dirs := []string{
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".aws"),
		filepath.Join(home, ".gnupg"),
		filepath.Join(home, ".docker"),
		filepath.Join(home, ".bitcoin"),
		filepath.Join(home, ".ethereum"),
		filepath.Join(home, ".solana"),
		filepath.Join(home, ".kube"),
	}

	switch runtime.GOOS {
	case "linux":
		dirs = append(dirs,
			filepath.Join(home, ".config", "gcloud"),
			filepath.Join(home, ".config", "google-chrome"),
			filepath.Join(home, ".config", "chromium"),
			filepath.Join(home, ".config", "BraveSoftware"),
			filepath.Join(home, ".mozilla"),
			filepath.Join(home, ".local", "share", "keyrings"),
			filepath.Join(home, ".password-store"),
		)

	case "darwin":
		dirs = append(dirs,
			filepath.Join(home, ".config", "gcloud"),
			filepath.Join(home, "Library", "Application Support", "Google", "Chrome"),
			filepath.Join(home, "Library", "Application Support", "Firefox"),
			filepath.Join(home, "Library", "Application Support", "BraveSoftware"),
			filepath.Join(home, "Library", "Safari"),
			filepath.Join(home, "Library", "Keychains"),
			filepath.Join(home, "Library", "Cookies"),
		)

	case "windows":
		appdata := os.Getenv("APPDATA")
		localAppdata := os.Getenv("LOCALAPPDATA")

		if appdata != "" {
			dirs = append(dirs,
				filepath.Join(appdata, "gcloud"),
				filepath.Join(appdata, "Docker"),
				filepath.Join(appdata, "Mozilla", "Firefox", "Profiles"),
			)
		}
		if localAppdata != "" {
			dirs = append(dirs,
				filepath.Join(localAppdata, "Google", "Chrome", "User Data"),
				filepath.Join(localAppdata, "BraveSoftware", "Brave-Browser", "User Data"),
				filepath.Join(localAppdata, "Microsoft", "Edge", "User Data"),
			)
		}
	}

	return dirs
}

// SystemReadOnlyPaths returns paths that sandboxed processes need read access to
// for basic operation (shared libraries, runtime files, etc.).
func SystemReadOnlyPaths() []string {
	switch runtime.GOOS {
	case "linux":
		return []string{
			"/usr/lib",
			"/usr/lib64",
			"/lib",
			"/lib64",
			"/usr/share",
			"/usr/local",
			"/etc/ssl",
			"/etc/resolv.conf",
			"/etc/hosts",
			"/etc/nsswitch.conf",
			"/etc/passwd",
			"/dev/null",
			"/dev/urandom",
			"/proc/self",
		}
	case "darwin":
		return []string{
			"/usr/lib",
			"/usr/share",
			"/usr/local",
			"/opt/homebrew",
			"/System",
			"/Library/Frameworks",
			"/etc/ssl",
			"/etc/resolv.conf",
			"/etc/hosts",
			"/dev/null",
			"/dev/urandom",
			"/private/etc/ssl",
			"/private/etc/resolv.conf",
			"/private/etc/hosts",
		}
	default:
		return nil
	}
}

// AllowedWritePaths returns paths that sandboxed processes are allowed to write to.
func AllowedWritePaths(policy *Policy) []string {
	paths := []string{"/tmp"}

	if tmpdir := os.Getenv("TMPDIR"); tmpdir != "" && tmpdir != "/tmp" {
		paths = append(paths, tmpdir)
	}

	if policy.WorkingDir != "" {
		paths = append(paths, policy.WorkingDir)
	}

	// Package manager cache directories: npx/npm and uvx/pip need write
	// access to download and cache packages before running MCP servers.
	if policy.Spec.Package != nil {
		home, _ := os.UserHomeDir()
		if home != "" {
			switch policy.Spec.Package.Registry {
			case "npm":
				paths = append(paths, filepath.Join(home, ".npm"))
			case "pypi":
				paths = append(paths, filepath.Join(home, ".cache", "uv"))
				paths = append(paths, filepath.Join(home, ".local"))
			}
		}
	}

	// Spec-declared write paths
	if policy.Spec.Sandbox != nil && policy.Spec.Sandbox.Filesystem != nil {
		for _, p := range policy.Spec.Sandbox.Filesystem.Write {
			paths = append(paths, expandHome(p))
		}
	}

	return paths
}

// AllowedReadPaths returns paths that sandboxed processes are allowed to read.
func AllowedReadPaths(policy *Policy) []string {
	paths := SystemReadOnlyPaths()

	if policy.InstallDir != "" {
		paths = append(paths, policy.InstallDir)
	}

	// Package manager config: npx needs ~/.npmrc, uvx needs ~/.config/uv
	if policy.Spec.Package != nil {
		home, _ := os.UserHomeDir()
		if home != "" {
			switch policy.Spec.Package.Registry {
			case "npm":
				paths = append(paths, filepath.Join(home, ".npm"))
				paths = append(paths, filepath.Join(home, ".npmrc"))
				paths = append(paths, filepath.Join(home, ".node_modules"))
			case "pypi":
				paths = append(paths, filepath.Join(home, ".cache", "uv"))
				paths = append(paths, filepath.Join(home, ".local"))
			}
		}
	}

	// Spec-declared read paths
	if policy.Spec.Sandbox != nil && policy.Spec.Sandbox.Filesystem != nil {
		for _, p := range policy.Spec.Sandbox.Filesystem.Read {
			paths = append(paths, expandHome(p))
		}
	}

	return paths
}

func expandHome(path string) string {
	if len(path) >= 2 && path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		if home != "" {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
