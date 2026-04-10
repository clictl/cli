// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package models

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestServer_TimeoutDuration_Default(t *testing.T) {
	s := Server{}
	got := s.TimeoutDuration()
	if got != 30*time.Second {
		t.Errorf("TimeoutDuration empty: got %v, want 30s", got)
	}
}

func TestServer_TimeoutDuration_ValidDuration(t *testing.T) {
	s := Server{Timeout: "15s"}
	got := s.TimeoutDuration()
	if got != 15*time.Second {
		t.Errorf("TimeoutDuration 15s: got %v, want 15s", got)
	}
}

func TestServer_TimeoutDuration_InvalidFallback(t *testing.T) {
	s := Server{Timeout: "not-a-duration"}
	got := s.TimeoutDuration()
	if got != 30*time.Second {
		t.Errorf("TimeoutDuration invalid: got %v, want 30s", got)
	}
}

func TestServer_TimeoutDuration_Milliseconds(t *testing.T) {
	s := Server{Timeout: "500ms"}
	got := s.TimeoutDuration()
	if got != 500*time.Millisecond {
		t.Errorf("TimeoutDuration 500ms: got %v, want 500ms", got)
	}
}

func TestToolSpec_Auth_UnmarshalYAML(t *testing.T) {
	input := `name: auth-tool
auth:
  env: MY_API_KEY
  header: "Authorization: Bearer ${MY_API_KEY}"
actions:
  - name: get
    request:
      method: GET
      path: /data
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("Unmarshal spec with auth: %v", err)
	}
	if spec.Auth == nil {
		t.Fatal("expected Auth to be non-nil")
	}
	if len(spec.Auth.Env) != 1 || spec.Auth.Env[0] != "MY_API_KEY" {
		t.Errorf("Auth.Env: got %v, want [MY_API_KEY]", spec.Auth.Env)
	}
	if spec.Auth.Header != "Authorization: Bearer ${MY_API_KEY}" {
		t.Errorf("Auth.Header: got %q, want %q", spec.Auth.Header, "Authorization: Bearer ${MY_API_KEY}")
	}
}

func TestToolSpec_Auth_MultipleEnv(t *testing.T) {
	input := `name: multi-env-tool
auth:
  env:
    - API_KEY
    - API_SECRET
  header: "Authorization: Bearer ${API_KEY}"
actions:
  - name: get
    request:
      method: GET
      path: /data
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("Unmarshal multi env auth: %v", err)
	}
	if spec.Auth == nil {
		t.Fatal("expected Auth to be non-nil")
	}
	if len(spec.Auth.Env) != 2 {
		t.Fatalf("Expected 2 env vars, got %d", len(spec.Auth.Env))
	}
	if spec.Auth.Env[0] != "API_KEY" || spec.Auth.Env[1] != "API_SECRET" {
		t.Errorf("Auth.Env: got %v", spec.Auth.Env)
	}
}

func TestToolSpec_ActionMutableField(t *testing.T) {
	input := `name: test-tool
actions:
  - name: create
    mutable: true
    request:
      method: POST
      path: /create
  - name: read
    request:
      method: GET
      path: /read
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("Unmarshal spec with mutable: %v", err)
	}
	if len(spec.Actions) != 2 {
		t.Fatalf("Expected 2 actions, got %d", len(spec.Actions))
	}
	if !spec.Actions[0].Mutable {
		t.Error("Expected first action to have mutable: true")
	}
	if spec.Actions[1].Mutable {
		t.Error("Expected second action to have mutable: false")
	}
}

func TestToolSpec_CompositeAction(t *testing.T) {
	input := `name: composite-tool
actions:
  - name: weather-report
    description: Get weather for a city
    composite: true
    steps:
      - id: geocode
        action: geocode
        params:
          city: "{{params.city}}"
      - id: forecast
        action: get-forecast
        depends: [geocode]
        params:
          lat: "{{steps.geocode.output.lat}}"
          lon: "{{steps.geocode.output.lon}}"
        on_error: skip
        retry:
          max_attempts: 3
          delay: "1s"
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("Unmarshal composite action: %v", err)
	}
	if len(spec.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(spec.Actions))
	}
	action := spec.Actions[0]
	if !action.IsComposite() {
		t.Error("expected action to be composite")
	}
	if len(action.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(action.Steps))
	}

	step0 := action.Steps[0]
	if step0.ID != "geocode" {
		t.Errorf("step 0 ID: got %q, want %q", step0.ID, "geocode")
	}
	if step0.Params["city"] != "{{params.city}}" {
		t.Errorf("step 0 city param: got %q", step0.Params["city"])
	}

	step1 := action.Steps[1]
	if step1.ID != "forecast" {
		t.Errorf("step 1 ID: got %q, want %q", step1.ID, "forecast")
	}
	if len(step1.DependsOn) != 1 || step1.DependsOn[0] != "geocode" {
		t.Errorf("step 1 depends: got %v, want [geocode]", step1.DependsOn)
	}
	if step1.OnError != "skip" {
		t.Errorf("step 1 on_error: got %q, want %q", step1.OnError, "skip")
	}
	if step1.Retry == nil {
		t.Fatal("expected step 1 retry to be non-nil")
	}
	if step1.Retry.MaxAttempts != 3 {
		t.Errorf("step 1 retry max_attempts: got %d, want 3", step1.Retry.MaxAttempts)
	}
}

func TestToolSpec_TransformPipeline(t *testing.T) {
	input := `name: transform-tool
actions:
  - name: fetch
    request:
      method: GET
      path: /data
    transform:
      - type: json
        extract: "$.data"
      - type: truncate
        max_items: 10
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("Unmarshal spec with transform pipeline: %v", err)
	}
	if len(spec.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(spec.Actions))
	}
	action := spec.Actions[0]
	if len(action.Transform) != 2 {
		t.Fatalf("expected 2 transform steps, got %d", len(action.Transform))
	}
	if action.Transform[0].Type != "json" {
		t.Errorf("Transform[0].Type: got %q, want %q", action.Transform[0].Type, "json")
	}
	if action.Transform[0].Extract != "$.data" {
		t.Errorf("Transform[0].Extract: got %q, want %q", action.Transform[0].Extract, "$.data")
	}
	if action.Transform[1].Type != "truncate" {
		t.Errorf("Transform[1].Type: got %q, want %q", action.Transform[1].Type, "truncate")
	}
	if action.Transform[1].MaxItems != 10 {
		t.Errorf("Transform[1].MaxItems: got %d, want 10", action.Transform[1].MaxItems)
	}
}

func TestToolSpec_NoAuthIsNil(t *testing.T) {
	input := `name: simple-tool
actions:
  - name: get
    request:
      method: GET
      path: /data
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("Unmarshal simple spec: %v", err)
	}
	if spec.Auth != nil {
		t.Error("expected Auth to be nil when not specified")
	}
}

func TestToolSpec_CompositeWithCrossTool(t *testing.T) {
	input := `name: orchestrator
actions:
  - name: pipeline
    composite: true
    steps:
      - id: step1
        tool: geocoding
        action: lookup
        params:
          address: "123 Main St"
      - id: step2
        action: local-action
        depends: [step1]
        condition: "{{steps.step1.output.found}}"
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("Unmarshal cross-tool composite: %v", err)
	}
	action := spec.Actions[0]
	if action.Steps[0].Tool != "geocoding" {
		t.Errorf("step 0 tool: got %q, want %q", action.Steps[0].Tool, "geocoding")
	}
	if action.Steps[1].Condition != "{{steps.step1.output.found}}" {
		t.Errorf("step 1 condition: got %q", action.Steps[1].Condition)
	}
}

func TestToolSpec_ServerHelpers(t *testing.T) {
	tests := []struct {
		name      string
		spec      ToolSpec
		wantType  string
		wantHTTP  bool
		wantStdio bool
		wantCmd   bool
		wantSkill bool
	}{
		{
			name:     "http server",
			spec:     ToolSpec{Server: &Server{Type: "http"}},
			wantType: "http", wantHTTP: true,
		},
		{
			name:     "stdio server",
			spec:     ToolSpec{Server: &Server{Type: "stdio"}},
			wantType: "stdio", wantStdio: true,
		},
		{
			name:     "command server",
			spec:     ToolSpec{Server: &Server{Type: "command"}},
			wantType: "command", wantCmd: true,
		},
		{
			name:      "skill (source, no server)",
			spec:      ToolSpec{Source: &SkillSource{Repo: "org/repo"}},
			wantType:  "",
			wantSkill: true,
		},
		{
			name:     "no server",
			spec:     ToolSpec{},
			wantType: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.spec.ServerType(); got != tt.wantType {
				t.Errorf("ServerType() = %q, want %q", got, tt.wantType)
			}
			if got := tt.spec.IsHTTP(); got != tt.wantHTTP {
				t.Errorf("IsHTTP() = %v, want %v", got, tt.wantHTTP)
			}
			if got := tt.spec.IsStdio(); got != tt.wantStdio {
				t.Errorf("IsStdio() = %v, want %v", got, tt.wantStdio)
			}
			if got := tt.spec.IsCommand(); got != tt.wantCmd {
				t.Errorf("IsCommand() = %v, want %v", got, tt.wantCmd)
			}
			if got := tt.spec.IsSkill(); got != tt.wantSkill {
				t.Errorf("IsSkill() = %v, want %v", got, tt.wantSkill)
			}
		})
	}
}

func TestToolSpec_AuthEnvVars(t *testing.T) {
	spec := ToolSpec{Auth: &Auth{Env: StringOrSlice{"KEY_A", "KEY_B"}}}
	got := spec.AuthEnvVars()
	if len(got) != 2 || got[0] != "KEY_A" || got[1] != "KEY_B" {
		t.Errorf("AuthEnvVars() = %v, want [KEY_A KEY_B]", got)
	}

	specNil := ToolSpec{}
	if got := specNil.AuthEnvVars(); got != nil {
		t.Errorf("AuthEnvVars() nil auth = %v, want nil", got)
	}
}

func TestToolSpec_SpecField(t *testing.T) {
	input := `spec: "1.0"
name: test-tool
protocol: http
description: test
server:
  url: https://example.com
actions:
  - name: get
    path: /data
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if spec.Spec != "1.0" {
		t.Errorf("Spec: got %q, want %q", spec.Spec, "1.0")
	}
}

func TestToolSpec_ProtocolField(t *testing.T) {
	input := `spec: "1.0"
name: test-tool
protocol: mcp
description: test
package:
  registry: npm
  name: test-server
  version: 1.0.0
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if spec.Protocol != "mcp" {
		t.Errorf("Protocol: got %q, want mcp", spec.Protocol)
	}
}

func TestToolSpec_PublisherField(t *testing.T) {
	input := `spec: "1.0"
name: test-tool
protocol: http
description: test
publisher:
  name: Acme Corp
  url: https://acme.com
server:
  url: https://api.acme.com
actions:
  - name: get
    path: /data
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if spec.Publisher == nil {
		t.Fatal("Publisher should be set")
	}
	if spec.Publisher.Name != "Acme Corp" {
		t.Errorf("Publisher.Name: got %q", spec.Publisher.Name)
	}
	if spec.Publisher.URL != "https://acme.com" {
		t.Errorf("Publisher.URL: got %q", spec.Publisher.URL)
	}
}

// ---------------------------------------------------------------------------
// Spec validation tests (merged from spec_validate_test.go)
// ---------------------------------------------------------------------------

func TestSandbox_FilesystemPermissions_YAMLParsing(t *testing.T) {
	input := `
name: test-skill
sandbox:
  commands:
    - read
    - write
  filesystem:
    read:
      - ./src
      - ./tests
    write:
      - ./src
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if spec.Sandbox == nil {
		t.Fatal("expected sandbox to be set")
	}
	if len(spec.Sandbox.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(spec.Sandbox.Commands))
	}
	if spec.Sandbox.Commands[0] != "read" || spec.Sandbox.Commands[1] != "write" {
		t.Errorf("unexpected commands: %v", spec.Sandbox.Commands)
	}
	if spec.Sandbox.Filesystem == nil {
		t.Fatal("expected filesystem permissions to be set")
	}
	if len(spec.Sandbox.Filesystem.Read) != 2 {
		t.Fatalf("expected 2 read paths, got %d", len(spec.Sandbox.Filesystem.Read))
	}
	if len(spec.Sandbox.Filesystem.Write) != 1 {
		t.Fatalf("expected 1 write path, got %d", len(spec.Sandbox.Filesystem.Write))
	}
	if spec.Sandbox.Filesystem.Read[0] != "./src" {
		t.Errorf("unexpected read path: %s", spec.Sandbox.Filesystem.Read[0])
	}
	if spec.Sandbox.Filesystem.Write[0] != "./src" {
		t.Errorf("unexpected write path: %s", spec.Sandbox.Filesystem.Write[0])
	}
}

func TestSandbox_NilWhenOmitted(t *testing.T) {
	input := `
name: test-skill
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if spec.Sandbox != nil {
		t.Error("expected sandbox to be nil when omitted")
	}
}

func TestSandbox_NetworkPermissions(t *testing.T) {
	input := `
name: test-skill
sandbox:
  network:
    allow:
      - api.example.com
      - cdn.example.com
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if spec.Sandbox == nil || spec.Sandbox.Network == nil {
		t.Fatal("expected sandbox.network to be set")
	}
	if len(spec.Sandbox.Network.Allow) != 2 {
		t.Fatalf("expected 2 network allow entries, got %d", len(spec.Sandbox.Network.Allow))
	}
	if spec.Sandbox.Network.Allow[0] != "api.example.com" {
		t.Errorf("unexpected network allow: %s", spec.Sandbox.Network.Allow[0])
	}
}

func TestSandbox_EnvPermissions(t *testing.T) {
	input := `
name: test-skill
sandbox:
  env:
    allow:
      - HOME
      - PATH
`
	var spec ToolSpec
	if err := yaml.Unmarshal([]byte(input), &spec); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if spec.Sandbox == nil || spec.Sandbox.Env == nil {
		t.Fatal("expected sandbox.env to be set")
	}
	if len(spec.Sandbox.Env.Allow) != 2 {
		t.Fatalf("expected 2 env allow entries, got %d", len(spec.Sandbox.Env.Allow))
	}
}

// ---------------------------------------------------------------------------
// ServerType inference tests
// ---------------------------------------------------------------------------

func TestServerType_ExplicitType(t *testing.T) {
	spec := &ToolSpec{Server: &Server{Type: "http"}}
	if got := spec.ServerType(); got != "http" {
		t.Errorf("ServerType explicit: got %q, want http", got)
	}
}

func TestServerType_InferHTTPFromURL(t *testing.T) {
	spec := &ToolSpec{Server: &Server{URL: "https://api.example.com"}}
	if got := spec.ServerType(); got != "http" {
		t.Errorf("ServerType infer from URL: got %q, want http", got)
	}
}

func TestServerType_InferStdioFromCommand(t *testing.T) {
	spec := &ToolSpec{Server: &Server{Command: "npx some-mcp-server"}}
	if got := spec.ServerType(); got != "stdio" {
		t.Errorf("ServerType infer from command: got %q, want stdio", got)
	}
}

func TestServerType_NilServer(t *testing.T) {
	spec := &ToolSpec{}
	if got := spec.ServerType(); got != "" {
		t.Errorf("ServerType nil server: got %q, want empty", got)
	}
}

func TestServerType_EmptyServer(t *testing.T) {
	spec := &ToolSpec{Server: &Server{}}
	if got := spec.ServerType(); got != "" {
		t.Errorf("ServerType empty server: got %q, want empty", got)
	}
}

func TestServerType_URLPrioritizedOverCommand(t *testing.T) {
	// When both URL and command are set, URL wins (inferred as http)
	spec := &ToolSpec{Server: &Server{URL: "https://example.com", Command: "some-cmd"}}
	if got := spec.ServerType(); got != "http" {
		t.Errorf("ServerType URL+command: got %q, want http", got)
	}
}

func TestIsHTTP_Website(t *testing.T) {
	// Website-style tools have a URL but may not have explicit type
	spec := &ToolSpec{
		Server: &Server{URL: "https://news.ycombinator.com"},
	}
	if !spec.IsHTTP() {
		t.Error("spec with URL should be IsHTTP()")
	}
}

func TestIsSkill(t *testing.T) {
	spec := &ToolSpec{
		Source: &SkillSource{Repo: "org/repo"},
	}
	if !spec.IsSkill() {
		t.Error("spec with Source and no Server should be IsSkill()")
	}
}

func TestIsSkill_NotWhenServerPresent(t *testing.T) {
	spec := &ToolSpec{
		Source: &SkillSource{Repo: "org/repo"},
		Server: &Server{Type: "http"},
	}
	if spec.IsSkill() {
		t.Error("spec with Source and Server should not be IsSkill()")
	}
}

func TestIsMCPPackage(t *testing.T) {
	spec := &ToolSpec{
		Package: &Package{Registry: "npm", Name: "@mcp/server", Version: "1.0"},
	}
	if !spec.IsMCPPackage() {
		t.Error("spec with Package and no Server should be IsMCPPackage()")
	}
}

func TestIsMCPPackage_FalseWithServer(t *testing.T) {
	spec := &ToolSpec{
		Package: &Package{Registry: "npm", Name: "@mcp/server"},
		Server:  &Server{Type: "stdio", Command: "npx"},
	}
	if spec.IsMCPPackage() {
		t.Error("spec with Package AND Server should not be IsMCPPackage()")
	}
}

func TestEnsureServer_NPM(t *testing.T) {
	spec := &ToolSpec{
		Package: &Package{Registry: "npm", Name: "@mcp/server-github", Version: "0.6.2"},
	}
	if !spec.EnsureServer() {
		t.Fatal("EnsureServer should return true for npm package")
	}
	if spec.Server == nil {
		t.Fatal("Server should be synthesized")
	}
	if spec.Server.Type != "stdio" {
		t.Errorf("Type: got %q, want stdio", spec.Server.Type)
	}
	if spec.Server.Command != "npx" {
		t.Errorf("Command: got %q, want npx", spec.Server.Command)
	}
	if len(spec.Server.Args) != 1 || spec.Server.Args[0] != "@mcp/server-github@0.6.2" {
		t.Errorf("Args: got %v, want [@mcp/server-github@0.6.2]", spec.Server.Args)
	}
}

func TestEnsureServer_PyPI(t *testing.T) {
	spec := &ToolSpec{
		Package: &Package{Registry: "pypi", Name: "mcp-server-sqlite", Version: "0.1.0"},
	}
	spec.EnsureServer()
	if spec.Server.Command != "uvx" {
		t.Errorf("Command: got %q, want uvx", spec.Server.Command)
	}
	if spec.Server.Args[0] != "mcp-server-sqlite==0.1.0" {
		t.Errorf("Args: got %v, want [mcp-server-sqlite==0.1.0]", spec.Server.Args)
	}
}

func TestEnsureServer_CustomManager(t *testing.T) {
	spec := &ToolSpec{
		Package: &Package{Registry: "npm", Name: "some-server", Version: "1.0", Manager: "bunx"},
	}
	spec.EnsureServer()
	if spec.Server.Command != "bunx" {
		t.Errorf("Command: got %q, want bunx", spec.Server.Command)
	}
}

func TestEnsureServer_NoopWithExistingServer(t *testing.T) {
	spec := &ToolSpec{
		Server:  &Server{Type: "stdio", Command: "my-server"},
		Package: &Package{Registry: "npm", Name: "unused"},
	}
	if spec.EnsureServer() {
		t.Error("EnsureServer should return false when Server already exists")
	}
	if spec.Server.Command != "my-server" {
		t.Error("existing Server should not be overwritten")
	}
}

func TestEnsureServer_NoopWithoutPackage(t *testing.T) {
	spec := &ToolSpec{}
	if spec.EnsureServer() {
		t.Error("EnsureServer should return false without Package")
	}
	if spec.Server != nil {
		t.Error("Server should remain nil")
	}
}
