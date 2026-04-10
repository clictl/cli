// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package models

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestServer_TimeoutDuration(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		want    time.Duration
	}{
		{name: "empty defaults to 30s", timeout: "", want: 30 * time.Second},
		{name: "valid 10s", timeout: "10s", want: 10 * time.Second},
		{name: "valid 2m", timeout: "2m", want: 2 * time.Minute},
		{name: "valid 500ms", timeout: "500ms", want: 500 * time.Millisecond},
		{name: "invalid falls back to 30s", timeout: "nope", want: 30 * time.Second},
		{name: "bare number falls back to 30s", timeout: "42", want: 30 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Server{Timeout: tt.timeout}
			got := s.TimeoutDuration()
			if got != tt.want {
				t.Errorf("TimeoutDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServer_KeepAliveDuration(t *testing.T) {
	tests := []struct {
		name      string
		keepAlive string
		want      time.Duration
	}{
		{name: "empty defaults to 60s", keepAlive: "", want: 60 * time.Second},
		{name: "valid 5m", keepAlive: "5m", want: 5 * time.Minute},
		{name: "valid 30s", keepAlive: "30s", want: 30 * time.Second},
		{name: "invalid falls back to 60s", keepAlive: "garbage", want: 60 * time.Second},
		{name: "bare number falls back to 60s", keepAlive: "100", want: 60 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Server{KeepAlive: tt.keepAlive}
			got := s.KeepAliveDuration()
			if got != tt.want {
				t.Errorf("KeepAliveDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToolSpec_UnmarshalYAML_MCPFields(t *testing.T) {
	input := `name: filesystem-mcp
description: File system access via MCP
server:
  type: stdio
  command: npx
  args: ["-y", "@modelcontextprotocol/server-filesystem"]
  env:
    HOME: /home/user
  timeout: 45s
  requires:
    - name: node
      url: https://nodejs.org
      check: node --version
discover: true
deny:
  - delete_file
prompts:
  - name: system
    description: You are a filesystem assistant.
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("Unmarshal MCP spec: %v", err)
	}

	if spec.Name != "filesystem-mcp" {
		t.Errorf("Name = %q, want %q", spec.Name, "filesystem-mcp")
	}
	if spec.ServerType() != "stdio" {
		t.Errorf("ServerType() = %q, want %q", spec.ServerType(), "stdio")
	}

	// Server
	if spec.Server == nil {
		t.Fatal("expected Server to be non-nil")
	}
	if spec.Server.Type != "stdio" {
		t.Errorf("Server.Type = %q, want %q", spec.Server.Type, "stdio")
	}
	if spec.Server.Command != "npx" {
		t.Errorf("Server.Command = %q, want %q", spec.Server.Command, "npx")
	}
	if len(spec.Server.Args) != 2 || spec.Server.Args[1] != "@modelcontextprotocol/server-filesystem" {
		t.Errorf("Server.Args = %v", spec.Server.Args)
	}
	if spec.Server.Env["HOME"] != "/home/user" {
		t.Errorf("Server.Env[HOME] = %q", spec.Server.Env["HOME"])
	}
	if spec.Server.TimeoutDuration() != 45*time.Second {
		t.Errorf("Server.TimeoutDuration() = %v, want 45s", spec.Server.TimeoutDuration())
	}
	if len(spec.Server.Requires) != 1 || spec.Server.Requires[0].Name != "node" {
		t.Errorf("Server.Requires = %v", spec.Server.Requires)
	}

	// Discover and Deny
	if !spec.Discover {
		t.Error("expected Discover to be true")
	}
	if len(spec.Deny) != 1 || spec.Deny[0] != "delete_file" {
		t.Errorf("Deny = %v, want [delete_file]", spec.Deny)
	}

	// Prompts
	if len(spec.Prompts.Items) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(spec.Prompts.Items))
	}
	if spec.Prompts.Items[0].Name != "system" {
		t.Errorf("Prompts[0].Name = %q, want %q", spec.Prompts.Items[0].Name, "system")
	}
}

func TestToolSpec_UnmarshalYAML_MCPHTTPServer(t *testing.T) {
	input := `name: remote-mcp
server:
  type: http
  url: https://mcp.example.com/v1
  headers:
    X-Custom: value
  timeout: 1m
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("Unmarshal HTTP server: %v", err)
	}
	if spec.Server.Type != "http" {
		t.Errorf("Server.Type = %q, want %q", spec.Server.Type, "http")
	}
	if spec.Server.URL != "https://mcp.example.com/v1" {
		t.Errorf("Server.URL = %q", spec.Server.URL)
	}
	if spec.Server.Headers["X-Custom"] != "value" {
		t.Errorf("Server.Headers[X-Custom] = %q", spec.Server.Headers["X-Custom"])
	}
	if spec.Server.TimeoutDuration() != time.Minute {
		t.Errorf("Server.TimeoutDuration() = %v, want 1m", spec.Server.TimeoutDuration())
	}
}

func TestToolSpec_UnmarshalYAML_SkillFields(t *testing.T) {
	input := `name: docker-skill
description: Docker management skill
source:
  repo: clictl/skills
  path: docker/SKILL.md
  ref: main
depends:
  - docker-engine
tags:
  - devops
  - containers
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("Unmarshal skill spec: %v", err)
	}

	if spec.Name != "docker-skill" {
		t.Errorf("Name = %q, want %q", spec.Name, "docker-skill")
	}
	if !spec.IsSkill() {
		t.Error("expected IsSkill() to be true")
	}

	// Source
	if spec.Source == nil {
		t.Fatal("expected Source to be non-nil")
	}
	if spec.Source.Repo != "clictl/skills" {
		t.Errorf("Source.Repo = %q, want %q", spec.Source.Repo, "clictl/skills")
	}
	if spec.Source.Path != "docker/SKILL.md" {
		t.Errorf("Source.Path = %q, want %q", spec.Source.Path, "docker/SKILL.md")
	}
	if spec.Source.Ref != "main" {
		t.Errorf("Source.Ref = %q, want %q", spec.Source.Ref, "main")
	}

	// Depends
	if len(spec.Depends) != 1 || spec.Depends[0] != "docker-engine" {
		t.Errorf("Depends = %v, want [docker-engine]", spec.Depends)
	}
}

func TestToolSpec_MCPFieldsNilWhenOmitted(t *testing.T) {
	input := `name: plain-tool
actions:
  - name: get
    request:
      method: GET
      path: /data
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("Unmarshal plain spec: %v", err)
	}
	if spec.Server != nil {
		t.Error("expected Server to be nil when not specified")
	}
	if len(spec.Prompts.Items) != 0 {
		t.Error("expected Prompts to be empty when not specified")
	}
	if spec.Source != nil {
		t.Error("expected Source to be nil when not specified")
	}
}
