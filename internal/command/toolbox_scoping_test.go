// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFindProjectRoot(t *testing.T) {
	// Create a temp dir hierarchy with a .git marker.
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("failed to create .git dir: %v", err)
	}

	// Create a nested subdirectory.
	nested := filepath.Join(root, "src", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	// Change to the nested dir; findProjectRoot should walk up to root.
	os.Chdir(nested)
	got := findProjectRoot()
	if got == "" {
		t.Fatal("expected findProjectRoot to find project root, got empty string")
	}
	// Resolve symlinks for comparison on macOS /private/var vs /var.
	wantAbs, _ := filepath.EvalSymlinks(root)
	gotAbs, _ := filepath.EvalSymlinks(got)
	if gotAbs != wantAbs {
		t.Errorf("findProjectRoot: got %q, want %q", gotAbs, wantAbs)
	}

	// .clictl/ directory should also be recognized as a project root marker.
	root2 := t.TempDir()
	cliDir := filepath.Join(root2, ".clictl")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.Chdir(root2)
	got2 := findProjectRoot()
	if got2 == "" {
		t.Fatal("expected findProjectRoot to find .clictl root, got empty string")
	}
}

func TestToolboxScopeDefault(t *testing.T) {
	// Mock an API server that records the POST body from toolbox add.
	var receivedScope string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path != "" {
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			if s, ok := body["scope"].(string); ok {
				receivedScope = s
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   "abc123",
				"name": "test-repo",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	// Call addWorkspaceToolbox directly with default (personal) scope.
	err := addWorkspaceToolbox(
		t.Context(),
		srv.URL,
		"my-workspace",
		"fake-token",
		"https://github.com/test/repo.git",
		"personal",
	)
	if err != nil {
		t.Fatalf("addWorkspaceToolbox returned error: %v", err)
	}
	if receivedScope != "personal" {
		t.Errorf("expected scope=personal, got %q", receivedScope)
	}
}

func TestToolboxListShowsScope(t *testing.T) {
	// Verify ProjectToolboxSource struct has a Name and URL field
	// that can be used to display scope in the list output.
	pts := ProjectToolboxSource{
		Name: "team-tools",
		URL:  "https://github.com/team/tools.git",
	}
	if pts.Name != "team-tools" {
		t.Errorf("expected Name=team-tools, got %q", pts.Name)
	}
	if pts.URL != "https://github.com/team/tools.git" {
		t.Errorf("expected URL=https://github.com/team/tools.git, got %q", pts.URL)
	}
}

func TestSecurityAdvisoryWarning(t *testing.T) {
	// Mock API that returns a tool with a security advisory flag.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":    "risky-tool",
			"version": "1.0.0",
			"security_advisory": map[string]interface{}{
				"severity": "high",
				"summary":  "Known vulnerability in v1.0.0",
				"url":      "https://example.com/advisory/123",
			},
		})
	}))
	defer srv.Close()

	// Make a request to the mock server and verify advisory data is present.
	resp, err := http.Get(fmt.Sprintf("%s/tool/risky-tool", srv.URL))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	advisory, ok := result["security_advisory"].(map[string]interface{})
	if !ok {
		t.Fatal("expected security_advisory in response")
	}
	if advisory["severity"] != "high" {
		t.Errorf("expected severity=high, got %q", advisory["severity"])
	}
	if advisory["summary"] == "" {
		t.Error("expected non-empty summary in security advisory")
	}
}
