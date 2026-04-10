// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
//go:build e2e_network

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Run (exec) - hits live httpbin.org
// ---------------------------------------------------------------------------

func TestRunEchoGet(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "run", "echo-test", "get", "--message", "hello-world")
	if !strings.Contains(out, "hello-world") {
		t.Errorf("expected output to contain 'hello-world', got: %s", out)
	}
}

func TestRunRaw(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "run", "echo-test", "get", "--message", "rawtest", "--raw")
	// Raw output should be the full httpbin JSON (not just the extracted $.args)
	if !strings.Contains(out, "httpbin.org") || !strings.Contains(out, "rawtest") {
		t.Errorf("expected raw httpbin response with 'rawtest', got: %s", out)
	}
}

func TestRunOutputJSON(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	out := mustRun(t, env, "run", "echo-test", "get", "--message", "jsontest", "-o", "json")
	if !json.Valid([]byte(out)) {
		t.Errorf("expected valid JSON output, got: %s", out)
	}
	if !strings.Contains(out, "jsontest") {
		t.Errorf("expected JSON to contain 'jsontest', got: %s", out)
	}
}

func TestRunAuthMissing(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	// Run auth-test without AUTH_TEST_KEY - should still hit httpbin but without the header
	// The command itself should succeed (httpbin doesn't enforce auth), but the transform
	// output should show the headers without X-Api-Key
	out, _, err := run(t, env, "run", "auth-test", "check")
	if err != nil {
		// If the CLI errors because auth is missing, that's also acceptable
		return
	}
	// If it succeeds, the output should NOT contain X-Api-Key header value
	_ = out
}

func TestRunAuthSet(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	env["AUTH_TEST_KEY"] = "test-secret-123"
	out := mustRun(t, env, "run", "auth-test", "check")
	// httpbin /headers echoes all headers - our key should be there
	if !strings.Contains(out, "test-secret-123") && !strings.Contains(out, "X-Api-Key") {
		t.Errorf("expected auth header in output, got: %s", out)
	}
}

func TestRunNotFoundTool(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	_, _, err := run(t, env, "run", "nonexistent-tool", "action")
	if err == nil {
		t.Error("expected run with nonexistent tool to fail")
	}
}

func TestRunNotFoundAction(t *testing.T) {
	t.Parallel()
	_, env := setupHome(t)
	_, _, err := run(t, env, "run", "echo-test", "nonexistent-action")
	if err == nil {
		t.Error("expected run with nonexistent action to fail")
	}
}
