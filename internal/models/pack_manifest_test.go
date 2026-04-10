// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package models

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPackManifest_ParseYAML(t *testing.T) {
	input := `
schema_version: "1"
name: test-skill
type: skill
version: "1.0.0"
description: "A test skill"
content_sha256: abc123def456
publisher:
  name: testuser
  identity: "github:testuser"
provenance:
  builder: clictl.dev/registry
  source_repo: testuser/repo
  source_ref: v1.0.0
sandbox:
  runtimes:
    - python
  network: none
  credentials:
    - ssh
targets:
  - type: claude-code
    min_version: "1.0.0"
  - type: cursor
`
	var m PackManifest
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if m.Name != "test-skill" {
		t.Errorf("name: got %q, want %q", m.Name, "test-skill")
	}
	if m.Type != "skill" {
		t.Errorf("type: got %q, want %q", m.Type, "skill")
	}
	if m.ContentSHA256 != "abc123def456" {
		t.Errorf("content_sha256: got %q", m.ContentSHA256)
	}
	if m.Publisher == nil || m.Publisher.Name != "testuser" {
		t.Error("publisher not parsed")
	}
	if m.Provenance == nil || m.Provenance.SourceRepo != "testuser/repo" {
		t.Error("provenance not parsed")
	}
	if m.Sandbox == nil || m.Sandbox.Network != "none" {
		t.Error("sandbox not parsed")
	}
	if len(m.Sandbox.Runtimes) != 1 || m.Sandbox.Runtimes[0] != "python" {
		t.Error("sandbox runtimes not parsed")
	}
	if len(m.Targets) != 2 {
		t.Errorf("targets: got %d, want 2", len(m.Targets))
	}
}

func TestPackManifest_RoundTrip(t *testing.T) {
	m := PackManifest{
		SchemaVersion: "1",
		Name:          "round-trip",
		Type:          "skill",
		Version:       "2.0.0",
		ContentSHA256: "deadbeef",
	}

	data, err := yaml.Marshal(&m)
	if err != nil {
		t.Fatal(err)
	}

	var m2 PackManifest
	if err := yaml.Unmarshal(data, &m2); err != nil {
		t.Fatal(err)
	}

	if m2.Name != m.Name || m2.Version != m.Version || m2.ContentSHA256 != m.ContentSHA256 {
		t.Error("round-trip mismatch")
	}
}
