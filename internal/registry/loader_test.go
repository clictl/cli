// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package registry

import (
	"testing"
)

const validSpec = `
name: testool
description: A test tool
version: "1.0"
category: testing
server:
  type: http
  url: https://api.example.com
actions:
  - name: hello
    description: Say hello
    request:
      method: GET
      path: /hello
    params:
      - name: name
        type: string
        required: true
        in: query
`

func TestParseSpec_Valid(t *testing.T) {
	spec, err := ParseSpec([]byte(validSpec))
	if err != nil {
		t.Fatalf("ParseSpec valid: %v", err)
	}
	if spec.Name != "testool" {
		t.Errorf("Name: got %q, want %q", spec.Name, "testool")
	}
	if spec.ServerType() != "http" {
		t.Errorf("ServerType: got %q, want %q", spec.ServerType(), "http")
	}
	if len(spec.Actions) != 1 {
		t.Fatalf("Actions count: got %d, want 1", len(spec.Actions))
	}
	if spec.Actions[0].Name != "hello" {
		t.Errorf("Action name: got %q, want %q", spec.Actions[0].Name, "hello")
	}
}

func TestParseSpec_MissingName(t *testing.T) {
	yaml := `
actions:
  - name: test
    request:
      method: GET
      path: /test
`
	_, err := ParseSpec([]byte(yaml))
	if err == nil {
		t.Fatal("ParseSpec missing name: expected error")
	}
}

func TestParseSpec_HTTPServerNoActionsOK(t *testing.T) {
	yaml := `
name: testool
server:
  type: http
  url: https://api.example.com
`
	// HTTP server type is valid without actions (MCP discovery may provide tools)
	spec, err := ParseSpec([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Name != "testool" {
		t.Errorf("Name: got %q, want %q", spec.Name, "testool")
	}
}

func TestParseSpec_InvalidYAML(t *testing.T) {
	_, err := ParseSpec([]byte("{{not yaml"))
	if err == nil {
		t.Fatal("ParseSpec invalid YAML: expected error")
	}
}

func TestFindAction_Found(t *testing.T) {
	spec, _ := ParseSpec([]byte(validSpec))
	action, err := FindAction(spec, "hello")
	if err != nil {
		t.Fatalf("FindAction: %v", err)
	}
	if action.Name != "hello" {
		t.Errorf("Action name: got %q, want %q", action.Name, "hello")
	}
}

func TestFindAction_NotFound(t *testing.T) {
	spec, _ := ParseSpec([]byte(validSpec))
	_, err := FindAction(spec, "nonexistent")
	if err == nil {
		t.Fatal("FindAction nonexistent: expected error")
	}
}

// MCP packaged tools (npm/pypi) are valid without actions or server block.
func TestParseSpec_MCPPackageNoActions(t *testing.T) {
	const mcpSpec = `
name: github-mcp
description: MCP server for GitHub
protocol: mcp
version: "1.0"
category: developer
package:
  registry: npm
  name: "@modelcontextprotocol/server-github"
  version: 0.6.2
  manager: npx
`
	spec, err := ParseSpec([]byte(mcpSpec))
	if err != nil {
		t.Fatalf("ParseSpec MCP package: %v", err)
	}
	if spec.Name != "github-mcp" {
		t.Errorf("Name: got %q", spec.Name)
	}
	if spec.Package == nil {
		t.Error("expected Package to be set")
	}
}

// Stdio MCP servers always get Discover=true, even without explicit discover: true.
func TestParseSpec_StdioImpliesDiscover(t *testing.T) {
	const stdioSpec = `
name: dynamic-mcp
description: Dynamic MCP server
version: "1.0"
server:
  type: stdio
  command: npx some-server
`
	spec, err := ParseSpec([]byte(stdioSpec))
	if err != nil {
		t.Fatalf("ParseSpec stdio: %v", err)
	}
	if !spec.Discover {
		t.Error("expected Discover to be true for stdio server")
	}
}

// Stdio MCP servers with static actions still get Discover=true.
// Static actions are metadata for search/docs, not execution.
func TestParseSpec_StdioWithActionsStillDiscovers(t *testing.T) {
	const spec = `
name: mcp-with-actions
description: MCP server with static action metadata
version: "1.0"
server:
  type: stdio
  command: npx some-server
actions:
  - name: list-items
    description: List all items
`
	s, err := ParseSpec([]byte(spec))
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	if !s.Discover {
		t.Error("stdio with static actions should still discover")
	}
	if len(s.Actions) != 1 {
		t.Error("static actions should be preserved as metadata")
	}
}

// HTTP servers do NOT get Discover=true.
func TestParseSpec_HTTPDoesNotDiscover(t *testing.T) {
	s, err := ParseSpec([]byte(validSpec))
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	if s.Discover {
		t.Error("HTTP servers should not have Discover=true")
	}
}

// Specs with prompts in map format (guidance) should parse successfully.
func TestParseSpec_PromptsMapFormat(t *testing.T) {
	const promptSpec = `
name: guided-mcp
description: MCP with guidance prompts
version: "1.0"
package:
  registry: npm
  name: some-server
  version: 1.0.0
prompts:
  system: |
    This server provides API access.
  tool_instructions:
    create_item: Always validate input first.
`
	spec, err := ParseSpec([]byte(promptSpec))
	if err != nil {
		t.Fatalf("ParseSpec prompts map: %v", err)
	}
	if spec.Prompts.System == "" {
		t.Error("expected System prompt to be set")
	}
	if spec.Prompts.ToolInstructions["create_item"] == "" {
		t.Error("expected tool_instructions to be set")
	}
}

// Specs with prompts in array format (MCP protocol) should still work.
func TestParseSpec_PromptsArrayFormat(t *testing.T) {
	const promptSpec = `
name: mcp-with-prompts
description: MCP with standard prompts
version: "1.0"
server:
  type: stdio
  command: npx some-server
prompts:
  - name: review
    description: Review code changes
`
	spec, err := ParseSpec([]byte(promptSpec))
	if err != nil {
		t.Fatalf("ParseSpec prompts array: %v", err)
	}
	if len(spec.Prompts.Items) != 1 {
		t.Fatalf("expected 1 prompt item, got %d", len(spec.Prompts.Items))
	}
	if spec.Prompts.Items[0].Name != "review" {
		t.Errorf("prompt name: got %q", spec.Prompts.Items[0].Name)
	}
}

// Specs without actions and without package/discover should fail.
func TestParseSpec_HTTPMissingServerURL(t *testing.T) {
	const badSpec = `
name: broken-tool
protocol: http
description: Missing server URL
version: "1.0"
`
	_, err := ParseSpec([]byte(badSpec))
	if err == nil {
		t.Fatal("expected ParseSpec to fail for http spec without server.url")
	}
}

func TestParseSpec_UnknownProtocol(t *testing.T) {
	const badSpec = `
name: broken-tool
protocol: graphql
description: Invalid protocol
`
	_, err := ParseSpec([]byte(badSpec))
	if err == nil {
		t.Fatal("expected ParseSpec to fail for unknown protocol")
	}
}
