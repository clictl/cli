// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binaryPath string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "clictl-e2e-build-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	binaryPath = filepath.Join(tmpDir, "clictl")
	cmd := exec.Command("go", "build",
		"-ldflags", "-X github.com/clictl/cli/internal/command.Version=v0.0.0-test",
		"-o", binaryPath,
		"../cmd/clictl",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "building clictl: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// run executes the clictl binary with the given args and env.
// Returns stdout, stderr, and error (non-nil if exit code != 0).
func run(t *testing.T, env map[string]string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	// Clean env to prevent host leakage
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	err := cmd.Run()
	return stdoutBuf.String(), stderrBuf.String(), err
}

// runWithStdin executes with piped stdin.
func runWithStdin(t *testing.T, env map[string]string, stdin string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	err := cmd.Run()
	return stdoutBuf.String(), stderrBuf.String(), err
}

// mustRun calls run and fails the test if it errors. Returns stdout.
func mustRun(t *testing.T, env map[string]string, args ...string) string {
	t.Helper()
	stdout, stderr, err := run(t, env, args...)
	if err != nil {
		t.Fatalf("clictl %s failed: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, stdout, stderr)
	}
	return stdout
}

// expectFail calls run and fails the test if exit code is 0. Returns stdout+stderr.
func expectFail(t *testing.T, env map[string]string, args ...string) (string, string) {
	t.Helper()
	stdout, stderr, err := run(t, env, args...)
	if err == nil {
		t.Fatalf("expected clictl %s to fail, but it succeeded\nstdout: %s", strings.Join(args, " "), stdout)
	}
	return stdout, stderr
}

// ---------------------------------------------------------------------------
// Home setup
// ---------------------------------------------------------------------------

// setupHome creates an isolated HOME with config, registry, and 3 fixture specs.
func setupHome(t *testing.T) (string, map[string]string) {
	t.Helper()
	tmpHome := t.TempDir()

	cliDir := filepath.Join(tmpHome, ".clictl")
	regDir := filepath.Join(cliDir, "toolboxes", "test")
	cacheDir := filepath.Join(cliDir, "cache")
	memDir := filepath.Join(cliDir, "memory")

	// Create per-tool spec directories matching the new structure
	echoDir := filepath.Join(regDir, "specs", "e", "echo-test")
	authDir := filepath.Join(regDir, "specs", "a", "auth-test")
	compDir := filepath.Join(regDir, "specs", "c", "composite-test")

	for _, d := range []string{cliDir, regDir, cacheDir, memDir, echoDir, authDir, compDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Config: local toolbox only, no network
	configYAML := fmt.Sprintf(`api_url: "http://127.0.0.1:1"
output: text
cache_dir: "%s"
first_run_done: true
update:
  auto_update: false
  last_sync_at: "2099-01-01T00:00:00Z"
  last_version_check_at: "2099-01-01T00:00:00Z"
toolboxes:
  - name: test
    type: git
    url: "file:///dev/null"
`, cacheDir)
	os.WriteFile(filepath.Join(cliDir, "config.yaml"), []byte(configYAML), 0o600)

	// Index
	index := buildTestIndex()
	indexData, _ := json.MarshalIndent(index, "", "  ")
	os.WriteFile(filepath.Join(regDir, "index.json"), indexData, 0o644)

	// Specs in per-tool folders
	os.WriteFile(filepath.Join(echoDir, "echo-test.yaml"), []byte(echoTestSpec), 0o644)
	os.WriteFile(filepath.Join(authDir, "auth-test.yaml"), []byte(authTestSpec), 0o644)
	os.WriteFile(filepath.Join(compDir, "composite-test.yaml"), []byte(compositeTestSpec), 0o644)

	env := map[string]string{
		"HOME": tmpHome,
	}
	return tmpHome, env
}

// setupHomeWithAPI creates an isolated HOME with api_url pointing to the given URL.
func setupHomeWithAPI(t *testing.T, apiURL string) (string, map[string]string) {
	t.Helper()
	home, env := setupHome(t)

	cliDir := filepath.Join(home, ".clictl")
	cacheDir := filepath.Join(cliDir, "cache")

	configYAML := fmt.Sprintf(`api_url: "%s"
output: text
cache_dir: "%s"
first_run_done: true
update:
  auto_update: false
  last_sync_at: "2099-01-01T00:00:00Z"
  last_version_check_at: "2099-01-01T00:00:00Z"
toolboxes:
  - name: test
    type: git
    url: "file:///dev/null"
`, apiURL, cacheDir)
	os.WriteFile(filepath.Join(cliDir, "config.yaml"), []byte(configYAML), 0o600)

	return home, env
}

// startSpecServer returns an httptest server that serves fixture spec YAML.
func startSpecServer(t *testing.T) *httptest.Server {
	t.Helper()
	specs := map[string]string{
		"echo-test":      echoTestSpec,
		"auth-test":      authTestSpec,
		"composite-test": compositeTestSpec,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle /api/v1/specs/{name}/yaml/
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "specs" && parts[len(parts)-1] == "yaml" {
			name := parts[3]
			if yaml, ok := specs[name]; ok {
				w.Header().Set("Content-Type", "text/yaml")
				w.Write([]byte(yaml))
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// ---------------------------------------------------------------------------
// Index builder
// ---------------------------------------------------------------------------

type indexEntry struct {
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Type        string   `json:"type"`
	Protocol    string   `json:"protocol"`
	Tags        []string `json:"tags"`
	Path        string   `json:"path"`
	Auth        string   `json:"auth"`
}

type testIndex struct {
	SchemaVersion int                    `json:"schema_version"`
	GeneratedAt   string                 `json:"generated_at"`
	Specs         map[string]*indexEntry `json:"specs"`
}

func buildTestIndex() *testIndex {
	return &testIndex{
		SchemaVersion: 1,
		GeneratedAt:   "2025-01-01T00:00:00Z",
		Specs: map[string]*indexEntry{
			"echo-test": {
				Version:     "1.0.0",
				Description: "Echo service for testing",
				Category:    "testing",
				Type:        "api",
				Protocol:    "http",
				Tags:        []string{"test", "echo", "http"},
				Path:        "specs/e/echo-test/echo-test.yaml",
				Auth:        "none",
			},
			"auth-test": {
				Version:     "1.0.0",
				Description: "Auth-required test service",
				Category:    "testing",
				Type:        "api",
				Protocol:    "http",
				Tags:        []string{"test", "auth"},
				Path:        "specs/a/auth-test/auth-test.yaml",
				Auth:        "api_key",
			},
			"composite-test": {
				Version:     "1.0.0",
				Description: "Composite workflow test",
				Category:    "testing",
				Type:        "api",
				Protocol:    "composite",
				Tags:        []string{"test", "composite"},
				Path:        "specs/c/composite-test/composite-test.yaml",
				Auth:        "none",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Fixture spec YAML constants
// ---------------------------------------------------------------------------

const echoTestSpec = `spec: "1.0"
name: echo-test
description: Echo service for testing
version: "1.0.0"
category: testing
tags: [test, echo, http]
server:
  type: http
  url: https://httpbin.org
actions:
  - name: get
    description: Echo query parameters
    method: GET
    path: /get
    params:
      - name: message
        type: string
        description: Message to echo
        in: query
    transform:
      - type: json
        extract: "$.args"
  - name: status
    description: Return a specific HTTP status code
    method: GET
    path: /status/200
`

const authTestSpec = `spec: "1.0"
name: auth-test
description: Auth-required test service
version: "1.0.0"
category: testing
tags: [test, auth]
server:
  type: http
  url: https://httpbin.org
auth:
  env: AUTH_TEST_KEY
  header: X-Api-Key
  value: "${AUTH_TEST_KEY}"
actions:
  - name: check
    description: Check auth header is sent
    method: GET
    path: /headers
    transform:
      - type: json
        extract: "$.headers"
`

const compositeTestSpec = `spec: "1.0"
name: composite-test
description: Composite workflow test
version: "1.0.0"
category: testing
tags: [test, composite]
server:
  type: http
  url: https://httpbin.org
actions:
  - name: pipeline
    description: Two-step pipeline that chains httpbin calls
    params:
      - name: message
        type: string
        required: true
        description: Message to pass through the pipeline
    steps:
      - id: step1
        method: GET
        url: "https://httpbin.org/get"
        params:
          msg: "${params.message}"
      - id: step2
        depends: [step1]
        method: GET
        url: "https://httpbin.org/get"
        params:
          echo: "${steps.step1.args.msg}"
`
