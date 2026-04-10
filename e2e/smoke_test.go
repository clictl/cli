// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ===========================================================================
// Discovery tests (supplementing offline_test.go)
// ===========================================================================

func TestExplainParams(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	out := mustRun(t, env, "explain", "echo-test", "get")
	// Verify the explain JSON includes param details
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("explain output is not valid JSON: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "message") {
		t.Errorf("expected param 'message' in explain output, got: %s", out)
	}
}

func TestExplainUnknownTool(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	_, stderr := expectFail(t, env, "explain", "nonexistent-tool-xyz", "get")
	if stderr == "" {
		t.Error("expected error output for unknown tool")
	}
}

// ===========================================================================
// Vault tests
// ===========================================================================

func TestVaultInit(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	mustRun(t, env, "vault", "init")

	home := env["HOME"]
	keyPath := filepath.Join(home, ".clictl", "vault.key")
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("vault.key not created: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("vault.key permissions = %o, want 0600", perm)
	}
}

func TestVaultSetGet(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	mustRun(t, env, "vault", "init")
	mustRun(t, env, "vault", "set", "MY_KEY", "my-secret-value")
	out := mustRun(t, env, "vault", "get", "MY_KEY")

	if !strings.Contains(out, "my-secret-value") {
		t.Errorf("vault get did not return expected value, got: %s", out)
	}
}

func TestVaultList(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	mustRun(t, env, "vault", "init")
	mustRun(t, env, "vault", "set", "KEY_ALPHA", "val1")
	mustRun(t, env, "vault", "set", "KEY_BETA", "val2")

	out := mustRun(t, env, "vault", "list")

	if !strings.Contains(out, "KEY_ALPHA") {
		t.Errorf("expected KEY_ALPHA in vault list, got: %s", out)
	}
	if !strings.Contains(out, "KEY_BETA") {
		t.Errorf("expected KEY_BETA in vault list, got: %s", out)
	}
	// Values should NOT be shown in list
	if strings.Contains(out, "val1") || strings.Contains(out, "val2") {
		t.Errorf("vault list should not show values, got: %s", out)
	}
}

func TestVaultDelete(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	mustRun(t, env, "vault", "init")
	mustRun(t, env, "vault", "set", "DEL_KEY", "to-delete")
	mustRun(t, env, "vault", "delete", "DEL_KEY")

	_, _ = expectFail(t, env, "vault", "get", "DEL_KEY")
}

func TestVaultProjectScope(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)

	mustRun(t, env, "vault", "init")

	// Create a fake project directory with a .git marker
	home := env["HOME"]
	projDir := filepath.Join(home, "myproject")
	os.MkdirAll(filepath.Join(projDir, ".git"), 0o755)

	projEnv := map[string]string{
		"HOME": home,
	}

	// Run vault set --project from the project directory
	runInDir(t, projDir, projEnv, "vault", "set", "--project", "PROJ_SECRET", "proj-val")

	// Verify the project vault file exists
	vaultFile := filepath.Join(projDir, ".clictl", "vault.enc")
	if _, err := os.Stat(vaultFile); err != nil {
		t.Fatalf("expected project vault at %s, got error: %v", vaultFile, err)
	}
}

// ===========================================================================
// Codegen tests
// ===========================================================================

func TestCodegenTypeScript(t *testing.T) {
	t.Parallel()
	srv := startSpecServer(t)
	_, env := setupHomeWithAPI(t, srv.URL)

	out := mustRun(t, env, "codegen", "echo-test", "--lang", "typescript")
	if !strings.Contains(out, "export interface") {
		t.Errorf("expected 'export interface' in TypeScript output, got: %s", out)
	}
	if !strings.Contains(out, "export") {
		t.Errorf("expected 'export' declarations in TypeScript output, got: %s", out)
	}
}

func TestCodegenPython(t *testing.T) {
	t.Parallel()
	srv := startSpecServer(t)
	_, env := setupHomeWithAPI(t, srv.URL)

	out := mustRun(t, env, "codegen", "echo-test", "--lang", "python")
	if !strings.Contains(out, "dataclass") {
		t.Errorf("expected 'dataclass' in Python output, got: %s", out)
	}
	if !strings.Contains(out, "async def") {
		t.Errorf("expected 'async def' in Python output, got: %s", out)
	}
}

func TestCodegenToFile(t *testing.T) {
	t.Parallel()
	srv := startSpecServer(t)
	home, env := setupHomeWithAPI(t, srv.URL)

	outFile := filepath.Join(home, "echo_test.ts")
	mustRun(t, env, "codegen", "echo-test", "--lang", "typescript", "--out", outFile)

	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("expected output file %s to exist: %v", outFile, err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	if !strings.Contains(string(data), "export interface") {
		t.Errorf("expected 'export interface' in generated file, got: %s", string(data))
	}
}

func TestCodegenInvalidLang(t *testing.T) {
	t.Parallel()
	srv := startSpecServer(t)
	_, env := setupHomeWithAPI(t, srv.URL)

	_, stderr := expectFail(t, env, "codegen", "echo-test", "--lang", "rust")
	if !strings.Contains(stderr, "unsupported language") {
		t.Errorf("expected 'unsupported language' error, got stderr: %s", stderr)
	}
}

// ===========================================================================
// MCP tests (JSON-RPC over stdin)
// ===========================================================================

func TestMCPToolsList(t *testing.T) {
	t.Parallel()
	srv := startSpecServer(t)
	_, env := setupHomeWithAPI(t, srv.URL)

	stdin := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}
{"jsonrpc":"2.0","method":"tools/list","params":{},"id":2}
`
	stdout, stderr, err := runWithStdin(t, env, stdin, "mcp-serve", "--tools-only", "echo-test")
	if err != nil {
		// MCP server may exit with error when stdin closes, that is acceptable
		// as long as we got tool list output
		if !strings.Contains(stdout, "tools") {
			t.Fatalf("mcp-serve failed with no tool output: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}
	}

	if !strings.Contains(stdout, "echo-test") {
		t.Errorf("expected 'echo-test' tool in MCP tools/list response, got: %s", stdout)
	}
}

func TestMCPToolsCall(t *testing.T) {
	t.Parallel()
	srv := startSpecServer(t)
	_, env := setupHomeWithAPI(t, srv.URL)

	stdin := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}
{"jsonrpc":"2.0","method":"tools/call","params":{"name":"echo-test_get","arguments":{"message":"hello"}},"id":2}
`
	stdout, stderr, err := runWithStdin(t, env, stdin, "mcp-serve", "--tools-only", "echo-test")
	if err != nil {
		if !strings.Contains(stdout, "result") && !strings.Contains(stdout, "error") {
			t.Fatalf("mcp-serve tools/call failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}
	}

	// Should have a JSON-RPC response for id:2
	if !strings.Contains(stdout, `"id":2`) && !strings.Contains(stdout, `"id": 2`) {
		t.Errorf("expected JSON-RPC response for id 2, got: %s", stdout)
	}
}

func TestMCPCodeMode(t *testing.T) {
	t.Parallel()
	srv := startSpecServer(t)
	_, env := setupHomeWithAPI(t, srv.URL)

	stdin := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}
{"jsonrpc":"2.0","method":"tools/list","params":{},"id":2}
`
	stdout, stderr, err := runWithStdin(t, env, stdin, "mcp-serve", "--code-mode", "--tools-only", "echo-test")
	if err != nil {
		if !strings.Contains(stdout, "tools") {
			t.Fatalf("mcp-serve --code-mode failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}
	}

	if !strings.Contains(stdout, "execute_code") {
		t.Errorf("expected 'execute_code' tool in code mode, got: %s", stdout)
	}
}
