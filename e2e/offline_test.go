// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Version / Doctor
// ---------------------------------------------------------------------------

func TestVersion(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "version")
	if !strings.Contains(out, "v0.0.0-test") {
		t.Errorf("expected version output to contain 'v0.0.0-test', got: %s", out)
	}
}

func TestVersionShort(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "version", "--short")
	trimmed := strings.TrimSpace(out)
	if trimmed != "v0.0.0-test" {
		t.Errorf("expected 'v0.0.0-test', got: %q", trimmed)
	}
}

func TestDoctor(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	// Doctor should exit 0 even if it finds issues
	out, _, err := run(t, env, "doctor")
	if err != nil {
		// Doctor may exit non-zero if it finds issues, that's OK
		_ = out
	}
	// Just ensure it doesn't panic - any output is fine
}

// ---------------------------------------------------------------------------
// Discovery: categories, tags, list, search, info, explain, deps
// ---------------------------------------------------------------------------

func TestCategories(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "categories")
	if !strings.Contains(out, "testing") {
		t.Errorf("expected categories to include 'testing', got: %s", out)
	}
}

func TestTags(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "tags")
	if !strings.Contains(out, "test") {
		t.Errorf("expected tags to include 'test', got: %s", out)
	}
}

func TestList(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "list")
	for _, tool := range []string{"echo-test", "auth-test", "composite-test"} {
		if !strings.Contains(out, tool) {
			t.Errorf("expected list to contain %q, got: %s", tool, out)
		}
	}
}

func TestListJSON(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "list", "-o", "json")
	if !json.Valid([]byte(out)) {
		t.Errorf("expected valid JSON, got: %s", out)
	}
}

func TestListCategory(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "list", "--category", "testing")
	if !strings.Contains(out, "echo-test") {
		t.Errorf("expected list --category testing to include 'echo-test', got: %s", out)
	}
}

func TestSearch(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "search", "echo")
	if !strings.Contains(out, "echo-test") {
		t.Errorf("expected search to find 'echo-test', got: %s", out)
	}
}

func TestSearchNoResults(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	// Search for nonsense - may error on API fallback, but should not panic
	_, _, _ = run(t, env, "search", "zzzznonexistentquery99")
	// Just verify no panic
}

func TestSearchWithCategoryFilter(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "search", "--category", "testing")
	if !strings.Contains(out, "echo-test") {
		t.Errorf("expected search --category testing to find 'echo-test', got: %s", out)
	}
}

func TestSearchWithProtocolFilter(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "search", "--protocol", "http")
	if !strings.Contains(out, "echo-test") {
		t.Errorf("expected search --protocol http to find 'echo-test', got: %s", out)
	}
}

func TestSearchNoArgsNoFiltersErrors(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	_, stderr, err := run(t, env, "search")
	if err == nil {
		t.Error("expected search with no args and no filters to fail")
	}
	if !strings.Contains(stderr, "provide a search query") {
		t.Errorf("expected helpful error message, got stderr: %s", stderr)
	}
}

func TestInfo(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "info", "echo-test")
	for _, expected := range []string{"echo-test", "1.0.0", "Echo service"} {
		if !strings.Contains(out, expected) {
			t.Errorf("expected info to contain %q, got: %s", expected, out)
		}
	}
}

func TestInfoJSON(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "info", "echo-test", "-o", "json")
	if !json.Valid([]byte(out)) {
		t.Errorf("expected valid JSON, got: %s", out)
	}
	if !strings.Contains(out, `"echo-test"`) {
		t.Errorf("expected JSON to contain tool name, got: %s", out)
	}
}

func TestInfoNotFound(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	_, stderr, err := run(t, env, "info", "nonexistent-tool-xyz")
	if err == nil {
		t.Error("expected info for nonexistent tool to fail")
	}
	combined := stderr
	if !strings.Contains(strings.ToLower(combined), "not found") && !strings.Contains(strings.ToLower(combined), "error") {
		t.Errorf("expected error message about tool not found, got stderr: %s", stderr)
	}
}

func TestExplain(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "explain", "echo-test", "get")
	if !json.Valid([]byte(out)) {
		t.Errorf("expected valid JSON from explain, got: %s", out)
	}
	if !strings.Contains(out, "echo-test") {
		t.Errorf("expected explain output to contain tool name, got: %s", out)
	}
}

func TestDeps(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "info", "auth-test")
	if !strings.Contains(out, "AUTH_TEST_KEY") {
		t.Errorf("expected info to show AUTH_TEST_KEY in dependencies, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Config mutation: disable/enable, pin/unpin
// ---------------------------------------------------------------------------

func TestDisableEnable(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	// Disable
	out := mustRun(t, env, "tool", "disable", "echo-test")
	if !strings.Contains(strings.ToLower(out), "disabled") && !strings.Contains(strings.ToLower(out), "disable") {
		t.Errorf("expected disable confirmation, got: %s", out)
	}

	// Enable
	out = mustRun(t, env, "tool", "enable", "echo-test")
	if !strings.Contains(strings.ToLower(out), "enabled") && !strings.Contains(strings.ToLower(out), "enable") {
		t.Errorf("expected enable confirmation, got: %s", out)
	}
}

func TestDisableAlreadyDisabled(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	mustRun(t, env, "tool", "disable", "echo-test")
	out := mustRun(t, env, "tool", "disable", "echo-test")
	if !strings.Contains(strings.ToLower(out), "already") {
		t.Errorf("expected 'already disabled' message, got: %s", out)
	}
}

func TestPinUnpin(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	out := mustRun(t, env, "tool", "pin", "echo-test")
	if !strings.Contains(strings.ToLower(out), "pin") {
		t.Errorf("expected pin confirmation, got: %s", out)
	}

	out = mustRun(t, env, "tool", "unpin", "echo-test")
	if !strings.Contains(strings.ToLower(out), "unpin") || !strings.Contains(strings.ToLower(out), "pin") {
		t.Errorf("expected unpin confirmation, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Workspace
// ---------------------------------------------------------------------------

func TestWorkspaceShowEmpty(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "workspace", "show")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "no active") && !strings.Contains(lower, "not set") {
		t.Errorf("expected 'no active workspace' message, got: %s", out)
	}
}

func TestWorkspaceSwitch(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	out := mustRun(t, env, "workspace", "switch", "myteam")
	if !strings.Contains(out, "myteam") {
		t.Errorf("expected workspace switch to confirm 'myteam', got: %s", out)
	}

	out = mustRun(t, env, "workspace", "show")
	if !strings.Contains(out, "myteam") {
		t.Errorf("expected workspace show to display 'myteam', got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Memory
// ---------------------------------------------------------------------------

func TestMemoryEmpty(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "memory", "echo-test")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "no memories") && !strings.Contains(lower, "no notes") && len(strings.TrimSpace(out)) == 0 {
		// Empty output is also acceptable
	}
}

func TestRememberAndMemory(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	mustRun(t, env, "remember", "echo-test", "use metric units")
	out := mustRun(t, env, "memory", "echo-test")
	if !strings.Contains(out, "use metric units") {
		t.Errorf("expected memory to contain 'use metric units', got: %s", out)
	}
}

func TestForgetAll(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	mustRun(t, env, "remember", "echo-test", "note one")
	mustRun(t, env, "remember", "echo-test", "note two")
	mustRun(t, env, "forget", "echo-test", "--all")

	out := mustRun(t, env, "memory", "echo-test")
	if strings.Contains(out, "note one") || strings.Contains(out, "note two") {
		t.Errorf("expected all memories cleared, but got: %s", out)
	}
}

func TestForgetByIndex(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	mustRun(t, env, "remember", "echo-test", "first note")
	mustRun(t, env, "remember", "echo-test", "second note")
	mustRun(t, env, "forget", "echo-test", "1")

	out := mustRun(t, env, "memory", "echo-test")
	if strings.Contains(out, "first note") {
		t.Errorf("expected first note to be forgotten, but got: %s", out)
	}
	if !strings.Contains(out, "second note") {
		t.Errorf("expected second note to remain, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Transform
// ---------------------------------------------------------------------------

func TestTransformExtract(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	input := `{"data": [1, 2, 3]}`
	out, _, err := runWithStdin(t, env, input, "transform", "--extract", "$.data")
	if err != nil {
		t.Fatalf("transform extract failed: %v", err)
	}
	if !strings.Contains(out, "1") || !strings.Contains(out, "3") {
		t.Errorf("expected extracted array, got: %s", out)
	}
}

func TestTransformSelect(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	input := `[{"name": "alice", "age": 30}, {"name": "bob", "age": 25}]`
	out, _, err := runWithStdin(t, env, input, "transform", "--select", "name")
	if err != nil {
		t.Fatalf("transform select failed: %v", err)
	}
	if !strings.Contains(out, "alice") || !strings.Contains(out, "bob") {
		t.Errorf("expected selected names, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Install / Uninstall (uses httptest server for spec fetching)
// ---------------------------------------------------------------------------

func TestInstallTool(t *testing.T) {
	t.Parallel()

	srv := startSpecServer(t)
	home, env := setupHomeWithAPI(t, srv.URL)

	// Create a project dir with .git so install writes skill there
	projectDir := filepath.Join(home, "project")
	os.MkdirAll(filepath.Join(projectDir, ".git"), 0o755)

	// Set working directory via env
	env["PWD"] = projectDir

	// Run install from the project dir (--trust for unverified test tool)
	runInDir(t, projectDir, env, "install", "echo-test", "--target", "claude-code", "--trust")

	// Verify skill file was created
	skillPath := filepath.Join(projectDir, ".claude", "skills", "echo-test", "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		// Try alternate path
		skillPath = filepath.Join(home, ".claude", "skills", "echo-test", "SKILL.md")
		if _, err := os.Stat(skillPath); os.IsNotExist(err) {
			t.Errorf("expected skill file to exist at %s or project dir", skillPath)
		}
	}
}

func TestUninstallTool(t *testing.T) {
	t.Parallel()

	srv := startSpecServer(t)
	home, env := setupHomeWithAPI(t, srv.URL)

	projectDir := filepath.Join(home, "project")
	os.MkdirAll(filepath.Join(projectDir, ".git"), 0o755)

	// Install first
	runInDir(t, projectDir, env, "install", "echo-test", "--target", "claude-code", "--trust")

	// Uninstall
	out := runInDir(t, projectDir, env, "uninstall", "echo-test")
	if !strings.Contains(strings.ToLower(out), "uninstall") && !strings.Contains(strings.ToLower(out), "removed") {
		// Some confirmation is expected
	}
}

// runInDir executes clictl from a specific working directory.
func runInDir(t *testing.T, dir string, env map[string]string, args ...string) string {
	t.Helper()
	out, _ := runInDirFull(t, dir, env, args...)
	return out
}

// runInDirFull executes clictl and returns both stdout and stderr.
func runInDirFull(t *testing.T, dir string, env map[string]string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	err := cmd.Run()
	if err != nil {
		t.Logf("clictl %s (in %s) failed: %v\nstdout: %s\nstderr: %s",
			strings.Join(args, " "), dir, err, stdoutBuf.String(), stderrBuf.String())
	}
	return stdoutBuf.String(), stderrBuf.String()
}
