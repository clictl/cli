// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// Runtime represents a package runtime (node/npm or python/uvx).
type Runtime struct {
	Name    string // "node" or "python"
	Command string // path to the runtime command (npx, uvx, python3, etc.)
	Version string // detected version string
}

// runtimeCache stores previously detected runtimes so we only probe once per
// registry type within a single CLI invocation.
var (
	runtimeCache   = make(map[string]*Runtime)
	runtimeCacheMu sync.Mutex
)

// lookPath is a variable so tests can replace it with a stub.
var lookPath = exec.LookPath

// runCommand is a variable so tests can replace it with a stub.
var runCommand = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// DetectRuntime checks if required runtimes are available for a package registry.
// Results are cached for the lifetime of the process.
func DetectRuntime(registry string) (*Runtime, error) {
	runtimeCacheMu.Lock()
	if cached, ok := runtimeCache[registry]; ok {
		runtimeCacheMu.Unlock()
		return cached, nil
	}
	runtimeCacheMu.Unlock()

	var rt *Runtime
	var err error

	switch registry {
	case "npm":
		rt, err = detectNodeRuntime()
	case "pypi":
		rt, err = detectPythonRuntime()
	default:
		return nil, fmt.Errorf("unknown package registry: %s", registry)
	}

	if err != nil {
		return nil, err
	}

	runtimeCacheMu.Lock()
	runtimeCache[registry] = rt
	runtimeCacheMu.Unlock()

	return rt, nil
}

// detectNodeRuntime looks for npx (preferred) and falls back to checking
// whether node is available at all. Returns an actionable error when
// nothing is found.
func detectNodeRuntime() (*Runtime, error) {
	path, err := lookPath("npx")
	if err == nil {
		version := commandVersion("npx", "--version")
		return &Runtime{
			Name:    "node",
			Command: path,
			Version: version,
		}, nil
	}

	// npx missing - check if node itself is available so we can give a
	// more targeted message.
	if _, nodeErr := lookPath("node"); nodeErr == nil {
		return nil, fmt.Errorf("npx not found but node is installed. Reinstall Node.js from https://nodejs.org to get npx")
	}

	return nil, fmt.Errorf("Node.js is required for npm packages. Install from https://nodejs.org")
}

// detectPythonRuntime prefers uvx, then falls back to python3/python.
func detectPythonRuntime() (*Runtime, error) {
	// Prefer uvx
	path, err := lookPath("uvx")
	if err == nil {
		version := commandVersion("uvx", "--version")
		return &Runtime{
			Name:    "python",
			Command: path,
			Version: version,
		}, nil
	}

	// Fall back to python3
	path, err = lookPath("python3")
	if err == nil {
		version := commandVersion("python3", "--version")
		fmt.Println("Tip: uvx recommended for Python packages. Install with: curl -LsSf https://astral.sh/uv/install.sh | sh")
		return &Runtime{
			Name:    "python",
			Command: path,
			Version: version,
		}, nil
	}

	// Fall back to python
	path, err = lookPath("python")
	if err == nil {
		version := commandVersion("python", "--version")
		fmt.Println("Tip: uvx recommended for Python packages. Install with: curl -LsSf https://astral.sh/uv/install.sh | sh")
		return &Runtime{
			Name:    "python",
			Command: path,
			Version: version,
		}, nil
	}

	return nil, fmt.Errorf("Python is required for PyPI packages. Install from https://python.org")
}

// commandVersion runs "<cmd> <arg>" and returns the first line of output,
// trimmed. Returns an empty string on any error.
func commandVersion(cmd string, arg string) string {
	out, err := runCommand(cmd, arg)
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(out))
	if idx := strings.IndexByte(s, '\n'); idx > 0 {
		s = s[:idx]
	}
	return s
}

// RuntimeInfo describes a single runtime binary (uvx, npx, docker) and
// whether it is available on this system.
type RuntimeInfo struct {
	Name      string
	Available bool
	Path      string
	Version   string
}

var (
	runtimeInfoCache map[string]RuntimeInfo
	runtimeInfoOnce  sync.Once
)

// DetectRuntimes checks for uvx, npx, and docker availability.
// Results are cached after the first call for the lifetime of the process.
func DetectRuntimes() map[string]RuntimeInfo {
	runtimeInfoOnce.Do(func() {
		runtimeInfoCache = make(map[string]RuntimeInfo)
		for _, name := range []string{"uvx", "npx", "docker"} {
			path, err := lookPath(name)
			if err == nil {
				version := commandVersion(name, "--version")
				runtimeInfoCache[name] = RuntimeInfo{Name: name, Available: true, Path: path, Version: version}
			} else {
				runtimeInfoCache[name] = RuntimeInfo{Name: name, Available: false}
			}
		}
	})
	return runtimeInfoCache
}

// resetRuntimeInfoCache resets the DetectRuntimes cache. Used by tests only.
func resetRuntimeInfoCache() {
	runtimeInfoOnce = sync.Once{}
	runtimeInfoCache = nil
}

// RuntimeAvailable returns true if the runtime for the given registry is
// available on this system. Uses cached detection results.
func RuntimeAvailable(registry string) bool {
	if registry == "" {
		return true
	}
	_, err := DetectRuntime(registry)
	return err == nil
}

// clearRuntimeCache resets the cache. Exported for testing only.
func clearRuntimeCache() {
	runtimeCacheMu.Lock()
	runtimeCache = make(map[string]*Runtime)
	runtimeCacheMu.Unlock()
}
