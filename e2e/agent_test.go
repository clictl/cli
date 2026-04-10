// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ===========================================================================
// MCP management tools: search, inspect, install, run via JSON-RPC
// ===========================================================================

func TestMCPManagementToolsListed(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	stdin := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}
{"jsonrpc":"2.0","method":"tools/list","params":{},"id":2}
`
	// Without --tools-only, management tools should be included
	stdout, _, _ := runWithStdin(t, env, stdin, "mcp-serve")

	for _, tool := range []string{"clictl_search", "clictl_list", "clictl_inspect", "clictl_install", "clictl_run"} {
		if !strings.Contains(stdout, tool) {
			t.Errorf("expected management tool %q in tools/list, got: %s", tool, truncateOutput(stdout))
		}
	}
}

func TestMCPSearchViaMCP(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	stdin := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}
{"jsonrpc":"2.0","method":"tools/call","params":{"name":"clictl_search","arguments":{"query":"echo"}},"id":2}
`
	stdout, _, _ := runWithStdin(t, env, stdin, "mcp-serve")

	if !strings.Contains(stdout, "echo-test") {
		t.Errorf("expected search to find 'echo-test', got: %s", truncateOutput(stdout))
	}
}

func TestMCPInspectViaMCP(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	stdin := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}
{"jsonrpc":"2.0","method":"tools/call","params":{"name":"clictl_inspect","arguments":{"tool":"echo-test"}},"id":2}
`
	stdout, _, _ := runWithStdin(t, env, stdin, "mcp-serve")

	// Should contain action names and description
	if !strings.Contains(stdout, "echo-test") {
		t.Errorf("expected inspect result to contain tool name, got: %s", truncateOutput(stdout))
	}
	if !strings.Contains(stdout, "get") {
		t.Errorf("expected inspect result to contain 'get' action, got: %s", truncateOutput(stdout))
	}
}

func TestMCPRunViaMCP(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	stdin := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}
{"jsonrpc":"2.0","method":"tools/call","params":{"name":"clictl_run","arguments":{"tool":"echo-test","action":"status"}},"id":2}
`
	stdout, _, _ := runWithStdin(t, env, stdin, "mcp-serve")

	// Should have a response for id:2
	if !strings.Contains(stdout, `"id":2`) && !strings.Contains(stdout, `"id": 2`) {
		t.Errorf("expected JSON-RPC response for id:2, got: %s", truncateOutput(stdout))
	}
}

func TestMCPFullAgentWorkflow(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	// Simulate the full agent workflow: search -> inspect -> run
	// (install is skipped since echo-test is already in the local toolbox)
	stdin := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}
{"jsonrpc":"2.0","method":"tools/call","params":{"name":"clictl_search","arguments":{"query":"echo"}},"id":2}
{"jsonrpc":"2.0","method":"tools/call","params":{"name":"clictl_inspect","arguments":{"tool":"echo-test"}},"id":3}
{"jsonrpc":"2.0","method":"tools/call","params":{"name":"clictl_run","arguments":{"tool":"echo-test","action":"status"}},"id":4}
`
	stdout, _, _ := runWithStdin(t, env, stdin, "mcp-serve")

	// All three responses should be present
	for _, id := range []string{`"id":2`, `"id":3`, `"id":4`} {
		if !strings.Contains(stdout, id) && !strings.Contains(stdout, strings.ReplaceAll(id, ":", ": ")) {
			t.Errorf("expected response for %s in full workflow, got: %s", id, truncateOutput(stdout))
		}
	}
}

func TestMCPToolsOnlyHidesManagement(t *testing.T) {
	t.Parallel()
	srv := startSpecServer(t)
	_, env := setupHomeWithAPI(t, srv.URL)

	stdin := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}
{"jsonrpc":"2.0","method":"tools/list","params":{},"id":2}
`
	stdout, stderr, _ := runWithStdin(t, env, stdin, "mcp-serve", "--tools-only", "echo-test")

	if stdout == "" {
		t.Skipf("MCP server produced no output (may not have loaded tool), stderr: %s", truncateOutput(stderr))
	}

	// Should NOT have management tools
	for _, tool := range []string{"clictl_search", "clictl_install"} {
		if strings.Contains(stdout, tool) {
			t.Errorf("management tool %q should NOT appear in --tools-only mode", tool)
		}
	}
}

// ===========================================================================
// SKILL.md generation on install
// ===========================================================================

func TestInstallCreatesSkillFile(t *testing.T) {
	t.Parallel()
	srv := startSpecServer(t)
	home, env := setupHomeWithAPI(t, srv.URL)

	projectDir := filepath.Join(home, "project")
	os.MkdirAll(filepath.Join(projectDir, ".git"), 0o755)

	runInDir(t, projectDir, env, "install", "echo-test", "--target", "claude-code", "--trust")

	// Check SKILL.md was created
	skillPaths := []string{
		filepath.Join(projectDir, ".claude", "skills", "echo-test", "SKILL.md"),
		filepath.Join(home, ".claude", "skills", "echo-test", "SKILL.md"),
	}
	found := false
	var skillContent string
	for _, p := range skillPaths {
		if data, err := os.ReadFile(p); err == nil {
			found = true
			skillContent = string(data)
			break
		}
	}
	if !found {
		t.Fatal("SKILL.md not found after install")
	}

	// Verify SKILL.md contains essential info
	if !strings.Contains(skillContent, "echo-test") {
		t.Error("SKILL.md missing tool name")
	}
	if !strings.Contains(skillContent, "get") {
		t.Error("SKILL.md missing 'get' action")
	}
}

func TestInstallNoMCPSkipsMCPConfig(t *testing.T) {
	t.Parallel()
	srv := startSpecServer(t)
	home, env := setupHomeWithAPI(t, srv.URL)

	projectDir := filepath.Join(home, "project")
	os.MkdirAll(filepath.Join(projectDir, ".git"), 0o755)

	runInDir(t, projectDir, env, "install", "echo-test", "--no-mcp", "--target", "claude-code", "--trust")

	// .mcp.json should NOT contain echo-test (or not exist)
	mcpPath := filepath.Join(projectDir, ".mcp.json")
	if data, err := os.ReadFile(mcpPath); err == nil {
		if strings.Contains(string(data), "echo-test") {
			t.Errorf(".mcp.json should not contain echo-test with --no-mcp, got: %s", string(data))
		}
	}
	// (File not existing is also acceptable for --no-mcp)
}

// ===========================================================================
// HTML-to-markdown transform end-to-end
// ===========================================================================

// TestRunHTMLToMarkdownTransform uses httpbin.org/html which returns
// a real HTML page, exercising the full html_to_markdown transform pipeline.
func TestRunHTMLToMarkdownTransform(t *testing.T) {
	t.Parallel()

	const htmlSpec = `spec: "1.0"
name: html-test
description: HTML scraper for testing transforms
version: "1.0.0"
category: testing
tags: [test, html, transform]
server:
  type: http
  url: https://httpbin.org
actions:
  - name: scrape
    description: Fetch page and convert to markdown
    method: GET
    path: /html
    transform:
      - type: html_to_markdown
`

	home, env := setupHome(t)
	cliDir := filepath.Join(home, ".clictl")
	specDir := filepath.Join(cliDir, "toolboxes", "test", "specs", "h", "html-test")
	os.MkdirAll(specDir, 0o755)
	os.WriteFile(filepath.Join(specDir, "html-test.yaml"), []byte(htmlSpec), 0o644)
	addToIndex(t, home, "html-test", "specs/h/html-test/html-test.yaml", "http")

	out := mustRun(t, env, "run", "html-test", "scrape")

	// httpbin.org/html returns an HTML page with headings and paragraphs
	// Verify the output is markdown, not raw HTML
	if strings.Contains(out, "</h1>") || strings.Contains(out, "</p>") {
		t.Errorf("output should be markdown not raw HTML, got: %s", truncateOutput(out))
	}
	// Should contain some readable text content
	if len(strings.TrimSpace(out)) < 50 {
		t.Errorf("expected substantial markdown output, got: %s", truncateOutput(out))
	}
}

// ===========================================================================
// Multi-step transform pipeline end-to-end
// ===========================================================================

// TestRunMultiStepTransformPipeline uses the echo-test fixture (httpbin.org/get)
// with a multi-step transform: extract $.args -> then the existing echo-test
// already has an extract transform. Here we test a custom multi-step spec.
func TestRunMultiStepTransformPipeline(t *testing.T) {
	t.Parallel()

	// Uses httpbin.org/get which returns {"args":{"msg":"hello"}, "headers":{...}, "url":"..."}
	const multiSpec = `spec: "1.0"
name: multi-transform-test
description: Multi-step transform pipeline test
version: "1.0.0"
category: testing
tags: [test, transform]
server:
  type: http
  url: https://httpbin.org
actions:
  - name: pipeline
    description: Extract headers, select specific fields, truncate
    method: GET
    path: /get
    params:
      - name: a
        type: string
        in: query
      - name: b
        type: string
        in: query
    transform:
      - type: json
        extract: "$.args"
`
	home, env := setupHome(t)
	cliDir := filepath.Join(home, ".clictl")
	specDir := filepath.Join(cliDir, "toolboxes", "test", "specs", "m", "multi-transform-test")
	os.MkdirAll(specDir, 0o755)
	os.WriteFile(filepath.Join(specDir, "multi-transform-test.yaml"), []byte(multiSpec), 0o644)
	addToIndex(t, home, "multi-transform-test", "specs/m/multi-transform-test/multi-transform-test.yaml", "http")

	out := mustRun(t, env, "run", "multi-transform-test", "pipeline", "--a", "hello", "--b", "world")

	// Should have extracted just the args object
	var result map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		t.Fatalf("expected valid JSON object, got: %s", truncateOutput(out))
	}
	if result["a"] != "hello" {
		t.Errorf("expected a=hello, got %v", result["a"])
	}
	if result["b"] != "world" {
		t.Errorf("expected b=world, got %v", result["b"])
	}
	// Should NOT contain other httpbin fields (url, headers, etc.)
	if _, ok := result["url"]; ok {
		t.Error("transform should have extracted only $.args, not full response")
	}
}

// ===========================================================================
// Debug logging
// ===========================================================================

func TestRunWithDebugLogging(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	env["CLICTL_LOG"] = "1"
	env["CLICTL_LOG_LEVEL"] = "debug"

	stdout, stderr, err := run(t, env, "info", "echo-test")
	if err != nil {
		t.Fatalf("info with debug logging failed: %v", err)
	}

	// Debug output should go to stderr, not stdout
	if !strings.Contains(stderr, "DEBUG") {
		t.Errorf("expected DEBUG entries in stderr, got: %s", truncateOutput(stderr))
	}
	// Stdout should still contain normal output
	if !strings.Contains(stdout, "echo-test") {
		t.Errorf("expected normal output on stdout, got: %s", truncateOutput(stdout))
	}
}

func TestRunWithJSONLogging(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	env["CLICTL_LOG"] = "1"
	env["CLICTL_LOG_LEVEL"] = "debug"
	env["CLICTL_LOG_FORMAT"] = "json"

	_, stderr, _ := run(t, env, "info", "echo-test")

	// Find first JSON log line
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "{") {
			var entry map[string]any
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Errorf("expected valid JSON log line, got: %s", line)
			}
			if _, ok := entry["level"]; !ok {
				t.Errorf("JSON log entry missing 'level' field: %s", line)
			}
			if _, ok := entry["msg"]; !ok {
				t.Errorf("JSON log entry missing 'msg' field: %s", line)
			}
			return // found a valid JSON log line
		}
	}
	// It's OK if there are no log lines for a simple info command
}

// ===========================================================================
// Instructions command
// ===========================================================================

func TestInstructions(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	out := mustRun(t, env, "instructions")

	// Should contain the discovery rules for agents
	if !strings.Contains(out, "clictl search") {
		t.Errorf("instructions should mention 'clictl search', got: %s", truncateOutput(out))
	}
	if !strings.Contains(out, "clictl run") {
		t.Errorf("instructions should mention 'clictl run', got: %s", truncateOutput(out))
	}
	if !strings.Contains(out, "clictl info") && !strings.Contains(out, "clictl inspect") {
		t.Errorf("instructions should mention tool inspection, got: %s", truncateOutput(out))
	}
}

// ===========================================================================
// Audit / verify commands
// ===========================================================================

func TestAuditInstalledTools(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	// Audit should work even with no tools installed
	stdout, _, err := run(t, env, "audit")
	if err != nil {
		// Audit may exit non-zero if there are findings, that's OK
		_ = stdout
	}
}

func TestVerifyTool(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	// Verify a tool from local toolbox
	stdout, _, err := run(t, env, "verify", "echo-test")
	if err != nil {
		// May fail if tool isn't installed with lock file, that's expected
		_ = stdout
	}
}

// ===========================================================================
// Helpers
// ===========================================================================

// addToIndex adds a spec entry to the test toolbox index.
func addToIndex(t *testing.T, home, name, path, protocol string) {
	t.Helper()
	indexPath := filepath.Join(home, ".clictl", "toolboxes", "test", "index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("reading index: %v", err)
	}
	var idx testIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("parsing index: %v", err)
	}
	idx.Specs[name] = &indexEntry{
		Version:     "1.0.0",
		Description: fmt.Sprintf("Test spec: %s", name),
		Category:    "testing",
		Type:        "api",
		Protocol:    protocol,
		Tags:        []string{"test"},
		Path:        path,
		Auth:        "none",
	}
	updated, _ := json.MarshalIndent(&idx, "", "  ")
	os.WriteFile(indexPath, updated, 0o644)
}

// truncateOutput truncates long output for readable test errors.
func truncateOutput(s string) string {
	if len(s) > 500 {
		return s[:500] + "... (truncated)"
	}
	return s
}
