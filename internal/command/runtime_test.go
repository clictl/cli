// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"strings"
	"testing"
)

// stubLookPath replaces lookPath for the duration of a test and restores it on cleanup.
func stubLookPath(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	orig := lookPath
	lookPath = fn
	t.Cleanup(func() { lookPath = orig })
}

// stubRunCommand replaces runCommand for the duration of a test and restores it on cleanup.
func stubRunCommand(t *testing.T, fn func(string, ...string) ([]byte, error)) {
	t.Helper()
	orig := runCommand
	runCommand = fn
	t.Cleanup(func() { runCommand = orig })
}

func TestDetectRuntime_NPM_NpxAvailable(t *testing.T) {
	clearRuntimeCache()

	stubLookPath(t, func(name string) (string, error) {
		if name == "npx" {
			return "/usr/local/bin/npx", nil
		}
		return "", fmt.Errorf("not found: %s", name)
	})
	stubRunCommand(t, func(name string, args ...string) ([]byte, error) {
		if name == "npx" && len(args) > 0 && args[0] == "--version" {
			return []byte("10.8.2\n"), nil
		}
		return nil, fmt.Errorf("unexpected command: %s", name)
	})

	rt, err := DetectRuntime("npm")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if rt.Name != "node" {
		t.Errorf("expected runtime name 'node', got %q", rt.Name)
	}
	if rt.Command != "/usr/local/bin/npx" {
		t.Errorf("expected command '/usr/local/bin/npx', got %q", rt.Command)
	}
	if rt.Version != "10.8.2" {
		t.Errorf("expected version '10.8.2', got %q", rt.Version)
	}
}

func TestDetectRuntime_NPM_NothingFound(t *testing.T) {
	clearRuntimeCache()

	stubLookPath(t, func(name string) (string, error) {
		return "", fmt.Errorf("not found: %s", name)
	})

	_, err := DetectRuntime("npm")
	if err == nil {
		t.Fatal("expected error when no node runtime found")
	}
	if !strings.Contains(err.Error(), "Node.js is required") {
		t.Errorf("expected actionable error about Node.js, got: %v", err)
	}
}

func TestDetectRuntime_NPM_NodeButNoNpx(t *testing.T) {
	clearRuntimeCache()

	stubLookPath(t, func(name string) (string, error) {
		if name == "node" {
			return "/usr/local/bin/node", nil
		}
		return "", fmt.Errorf("not found: %s", name)
	})

	_, err := DetectRuntime("npm")
	if err == nil {
		t.Fatal("expected error when npx not found")
	}
	if !strings.Contains(err.Error(), "npx not found but node is installed") {
		t.Errorf("expected targeted error about npx, got: %v", err)
	}
}

func TestDetectRuntime_PyPI_UvxAvailable(t *testing.T) {
	clearRuntimeCache()

	stubLookPath(t, func(name string) (string, error) {
		if name == "uvx" {
			return "/usr/local/bin/uvx", nil
		}
		return "", fmt.Errorf("not found: %s", name)
	})
	stubRunCommand(t, func(name string, args ...string) ([]byte, error) {
		if name == "uvx" && len(args) > 0 && args[0] == "--version" {
			return []byte("uv 0.5.1\n"), nil
		}
		return nil, fmt.Errorf("unexpected command: %s", name)
	})

	rt, err := DetectRuntime("pypi")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if rt.Name != "python" {
		t.Errorf("expected runtime name 'python', got %q", rt.Name)
	}
	if rt.Command != "/usr/local/bin/uvx" {
		t.Errorf("expected command '/usr/local/bin/uvx', got %q", rt.Command)
	}
	if rt.Version != "uv 0.5.1" {
		t.Errorf("expected version 'uv 0.5.1', got %q", rt.Version)
	}
}

func TestDetectRuntime_PyPI_FallbackToPython3(t *testing.T) {
	clearRuntimeCache()

	stubLookPath(t, func(name string) (string, error) {
		if name == "python3" {
			return "/usr/bin/python3", nil
		}
		return "", fmt.Errorf("not found: %s", name)
	})
	stubRunCommand(t, func(name string, args ...string) ([]byte, error) {
		if name == "python3" && len(args) > 0 && args[0] == "--version" {
			return []byte("Python 3.13.0\n"), nil
		}
		return nil, fmt.Errorf("unexpected command: %s", name)
	})

	rt, err := DetectRuntime("pypi")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if rt.Name != "python" {
		t.Errorf("expected runtime name 'python', got %q", rt.Name)
	}
	if rt.Command != "/usr/bin/python3" {
		t.Errorf("expected command '/usr/bin/python3', got %q", rt.Command)
	}
	if rt.Version != "Python 3.13.0" {
		t.Errorf("expected version 'Python 3.13.0', got %q", rt.Version)
	}
}

func TestDetectRuntime_PyPI_FallbackToPython(t *testing.T) {
	clearRuntimeCache()

	stubLookPath(t, func(name string) (string, error) {
		if name == "python" {
			return "/usr/bin/python", nil
		}
		return "", fmt.Errorf("not found: %s", name)
	})
	stubRunCommand(t, func(name string, args ...string) ([]byte, error) {
		if name == "python" && len(args) > 0 && args[0] == "--version" {
			return []byte("Python 3.12.4\n"), nil
		}
		return nil, fmt.Errorf("unexpected command: %s", name)
	})

	rt, err := DetectRuntime("pypi")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if rt.Command != "/usr/bin/python" {
		t.Errorf("expected command '/usr/bin/python', got %q", rt.Command)
	}
}

func TestDetectRuntime_PyPI_NothingFound(t *testing.T) {
	clearRuntimeCache()

	stubLookPath(t, func(name string) (string, error) {
		return "", fmt.Errorf("not found: %s", name)
	})

	_, err := DetectRuntime("pypi")
	if err == nil {
		t.Fatal("expected error when no python runtime found")
	}
	if !strings.Contains(err.Error(), "Python is required") {
		t.Errorf("expected actionable error about Python, got: %v", err)
	}
}

func TestDetectRuntime_UnknownRegistry(t *testing.T) {
	clearRuntimeCache()

	_, err := DetectRuntime("cargo")
	if err == nil {
		t.Fatal("expected error for unknown registry")
	}
	if !strings.Contains(err.Error(), "unknown package registry") {
		t.Errorf("expected 'unknown package registry' error, got: %v", err)
	}
}

func TestDetectRuntime_Caching(t *testing.T) {
	clearRuntimeCache()

	calls := 0
	stubLookPath(t, func(name string) (string, error) {
		calls++
		if name == "npx" {
			return "/usr/local/bin/npx", nil
		}
		return "", fmt.Errorf("not found: %s", name)
	})
	stubRunCommand(t, func(name string, args ...string) ([]byte, error) {
		return []byte("10.0.0\n"), nil
	})

	// First call should probe
	rt1, err := DetectRuntime("npm")
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	// Second call should use cache
	rt2, err := DetectRuntime("npm")
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	if rt1 != rt2 {
		t.Error("expected cached runtime to be the same pointer")
	}

	if calls != 1 {
		t.Errorf("expected lookPath to be called once (cached), got %d calls", calls)
	}
}

func TestDetectRuntimes_Cached(t *testing.T) {
	resetRuntimeInfoCache()

	calls := 0
	stubLookPath(t, func(name string) (string, error) {
		calls++
		switch name {
		case "uvx":
			return "/usr/local/bin/uvx", nil
		case "npx":
			return "/usr/local/bin/npx", nil
		case "docker":
			return "", fmt.Errorf("not found: %s", name)
		default:
			return "", fmt.Errorf("not found: %s", name)
		}
	})
	stubRunCommand(t, func(name string, args ...string) ([]byte, error) {
		switch name {
		case "uvx":
			return []byte("uv 0.6.0\n"), nil
		case "npx":
			return []byte("10.8.0\n"), nil
		default:
			return nil, fmt.Errorf("unexpected command: %s", name)
		}
	})

	first := DetectRuntimes()
	callsAfterFirst := calls

	second := DetectRuntimes()

	// Second call must not invoke lookPath again
	if calls != callsAfterFirst {
		t.Errorf("expected no additional lookPath calls on second DetectRuntimes, got %d extra", calls-callsAfterFirst)
	}

	// Results should be identical
	for _, name := range []string{"uvx", "npx", "docker"} {
		if first[name] != second[name] {
			t.Errorf("cached result differs for %s", name)
		}
	}

	// Verify values
	if !first["uvx"].Available || first["uvx"].Path != "/usr/local/bin/uvx" {
		t.Errorf("uvx should be available at /usr/local/bin/uvx, got %+v", first["uvx"])
	}
	if !first["npx"].Available || first["npx"].Path != "/usr/local/bin/npx" {
		t.Errorf("npx should be available at /usr/local/bin/npx, got %+v", first["npx"])
	}
	if first["docker"].Available {
		t.Errorf("docker should not be available, got %+v", first["docker"])
	}
}

func TestDetectRuntimes_MissingBinaryVersion(t *testing.T) {
	resetRuntimeInfoCache()

	stubLookPath(t, func(name string) (string, error) {
		if name == "uvx" {
			return "/usr/local/bin/uvx", nil
		}
		return "", fmt.Errorf("not found: %s", name)
	})
	stubRunCommand(t, func(name string, args ...string) ([]byte, error) {
		// Simulate version command failing
		return nil, fmt.Errorf("command failed")
	})

	runtimes := DetectRuntimes()

	if !runtimes["uvx"].Available {
		t.Error("uvx should be available even when version command fails")
	}
	if runtimes["uvx"].Version != "" {
		t.Errorf("expected empty version when command fails, got %q", runtimes["uvx"].Version)
	}
	if runtimes["npx"].Available {
		t.Error("npx should not be available")
	}
}

func TestDetectRuntime_RegistryRouting(t *testing.T) {
	clearRuntimeCache()

	stubLookPath(t, func(name string) (string, error) {
		switch name {
		case "npx":
			return "/usr/local/bin/npx", nil
		case "uvx":
			return "/usr/local/bin/uvx", nil
		default:
			return "", fmt.Errorf("not found: %s", name)
		}
	})
	stubRunCommand(t, func(name string, args ...string) ([]byte, error) {
		return []byte("1.0.0\n"), nil
	})

	npmRT, err := DetectRuntime("npm")
	if err != nil {
		t.Fatalf("npm detection failed: %v", err)
	}
	if npmRT.Name != "node" {
		t.Errorf("expected npm to route to 'node', got %q", npmRT.Name)
	}

	clearRuntimeCache()

	pypiRT, err := DetectRuntime("pypi")
	if err != nil {
		t.Fatalf("pypi detection failed: %v", err)
	}
	if pypiRT.Name != "python" {
		t.Errorf("expected pypi to route to 'python', got %q", pypiRT.Name)
	}
}
