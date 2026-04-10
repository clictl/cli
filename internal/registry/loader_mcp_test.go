// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package registry

import (
	"testing"

	"github.com/clictl/cli/internal/models"
)

// --- ParseSpec: MCP (server type stdio/http) ---

func TestParseSpec_MCP_ValidStdio(t *testing.T) {
	data := []byte(`
name: my-mcp
server:
  type: stdio
  command: my-server
`)
	spec, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Name != "my-mcp" {
		t.Errorf("expected name %q, got %q", "my-mcp", spec.Name)
	}
	if spec.ServerType() != "stdio" {
		t.Errorf("expected server type %q, got %q", "stdio", spec.ServerType())
	}
	if spec.Server == nil {
		t.Fatal("expected server to be set")
	}
	if spec.Server.Type != "stdio" {
		t.Errorf("expected server type %q, got %q", "stdio", spec.Server.Type)
	}
}

func TestParseSpec_MCP_ValidHTTP(t *testing.T) {
	data := []byte(`
name: my-mcp-http
server:
  type: http
  url: https://example.com/mcp
`)
	spec, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Server.Type != "http" {
		t.Errorf("expected server type %q, got %q", "http", spec.Server.Type)
	}
}

func TestParseSpec_MCP_MissingServerAndPackage(t *testing.T) {
	data := []byte(`
name: my-mcp
protocol: mcp
description: Missing server and package
`)
	_, err := ParseSpec(data)
	if err == nil {
		t.Fatal("expected error for MCP spec without server or package, got nil")
	}
}

// --- ParseSpec: skill ---

func TestParseSpec_Skill_Valid(t *testing.T) {
	data := []byte(`
name: my-skill
source:
  repo: org/repo
`)
	spec, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !spec.IsSkill() {
		t.Error("expected IsSkill() to be true")
	}
	if spec.Source == nil {
		t.Fatal("expected source to be set")
	}
}

// --- ParseSpec: HTTP with actions ---

func TestParseSpec_HTTP_NoActionsAllowed(t *testing.T) {
	data := []byte(`
name: my-http
server:
  type: http
  url: https://api.example.com
`)
	// http server type is valid even without actions (MCP discovery may provide tools)
	spec, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.ServerType() != "http" {
		t.Errorf("expected server type %q, got %q", "http", spec.ServerType())
	}
}

func TestParseSpec_HTTP_ValidWithActions(t *testing.T) {
	data := []byte(`
name: my-http
server:
  type: http
  url: https://api.example.com
actions:
  - name: get-thing
    request:
      method: GET
      path: /things
`)
	spec, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spec.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(spec.Actions))
	}
}

// --- IsMCPToolAllowed ---

func TestIsMCPToolAllowed_NilConfig(t *testing.T) {
	spec := &models.ToolSpec{Name: "test"}
	// No allow/deny means allow all
	if !IsMCPToolAllowed(spec, "anything") {
		t.Error("expected tool to be allowed when no allow/deny is set")
	}
}

func TestIsMCPToolAllowed_DiscoverAll_NoDeny(t *testing.T) {
	spec := &models.ToolSpec{
		Name:     "test",
		Discover: true,
	}
	if !IsMCPToolAllowed(spec, "any-tool") {
		t.Error("expected tool to be allowed with discover: true")
	}
}

func TestIsMCPToolAllowed_WithDeny(t *testing.T) {
	spec := &models.ToolSpec{
		Name:     "test",
		Discover: true,
		Deny:     []string{"dangerous-tool"},
	}
	if IsMCPToolAllowed(spec, "dangerous-tool") {
		t.Error("expected denied tool to be rejected")
	}
	if !IsMCPToolAllowed(spec, "safe-tool") {
		t.Error("expected non-denied tool to be allowed")
	}
}

func TestIsMCPToolAllowed_AllowList(t *testing.T) {
	spec := &models.ToolSpec{
		Name:  "test",
		Allow: []string{"tool-a", "tool-b"},
	}
	if !IsMCPToolAllowed(spec, "tool-a") {
		t.Error("expected allowed tool-a to be allowed")
	}
	if !IsMCPToolAllowed(spec, "tool-b") {
		t.Error("expected allowed tool-b to be allowed")
	}
	if IsMCPToolAllowed(spec, "tool-c") {
		t.Error("expected non-allowed tool-c to be rejected")
	}
}

func TestIsMCPToolAllowed_DenyOverridesAllow(t *testing.T) {
	spec := &models.ToolSpec{
		Name:  "test",
		Allow: []string{"tool-x"},
		Deny:  []string{"tool-x"},
	}
	if IsMCPToolAllowed(spec, "tool-x") {
		t.Error("expected deny to override allow list")
	}
}
