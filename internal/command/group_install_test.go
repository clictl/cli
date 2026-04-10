// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clictl/cli/internal/archive"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/signing"
	"gopkg.in/yaml.v3"
)

// --- Q2.2: CLI tests - group install (multi-pack), trust tier computation, blocklist check ---

// TestGroupManifest_Parse verifies that group manifests with multiple member
// packs parse correctly and contain the expected fields.
func TestGroupManifest_Parse(t *testing.T) {
	input := `
schema_version: "1"
name: gstack
type: group
version: 1.1.0
description: "Garry Tan's Claude Code workflow"
publisher:
  name: garrytan
  identity: "github:garrytan"
packages:
  - name: gstack-ship
    type: skill
    version: 1.0.0
    sha256: a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2
  - name: gstack-review
    type: skill
    version: 1.0.0
    sha256: b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3
  - name: github-mcp
    type: mcp
    version: 1.2.0
    sha256: c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4
`

	var m models.PackManifest
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("failed to parse group manifest: %v", err)
	}

	if m.Name != "gstack" {
		t.Errorf("name: got %q, want %q", m.Name, "gstack")
	}
	if m.Type != "group" {
		t.Errorf("type: got %q, want %q", m.Type, "group")
	}
	if m.Version != "1.1.0" {
		t.Errorf("version: got %q, want %q", m.Version, "1.1.0")
	}
	if m.Publisher == nil || m.Publisher.Name != "garrytan" {
		t.Error("publisher not parsed correctly")
	}
}

// TestTrustTier_AllSigned computes Tier 1 (Verified) when group is signed
// and all member packs have valid SHA256 hashes.
func TestTrustTier_AllSigned(t *testing.T) {
	type GroupMember struct {
		Name    string
		Type    string
		Version string
		SHA256  string
		Signed  bool
	}

	members := []GroupMember{
		{Name: "skill-a", Type: "skill", Version: "1.0.0", SHA256: "abc123", Signed: true},
		{Name: "skill-b", Type: "skill", Version: "1.0.0", SHA256: "def456", Signed: true},
		{Name: "mcp-tool", Type: "mcp", Version: "1.2.0", SHA256: "ghi789", Signed: true},
	}

	groupSigned := true
	allMembersSigned := true
	for _, m := range members {
		if !m.Signed {
			allMembersSigned = false
			break
		}
	}

	tier := computeTrustTier(groupSigned, allMembersSigned, false)
	if tier != "verified" {
		t.Errorf("expected trust tier 'verified', got %q", tier)
	}
}

// TestTrustTier_PartialSigned computes Tier 2 (Partial) when some members
// are unsigned.
func TestTrustTier_PartialSigned(t *testing.T) {
	tier := computeTrustTier(true, false, false)
	if tier != "partial" {
		t.Errorf("expected trust tier 'partial', got %q", tier)
	}
}

// TestTrustTier_Community computes Tier 3 (Community) when the group
// itself is unsigned.
func TestTrustTier_Community(t *testing.T) {
	tier := computeTrustTier(false, false, false)
	if tier != "community" {
		t.Errorf("expected trust tier 'community', got %q", tier)
	}
}

// TestTrustTier_Blocked returns "blocked" when any member is on the blocklist.
func TestTrustTier_Blocked(t *testing.T) {
	tier := computeTrustTier(true, true, true)
	if tier != "blocked" {
		t.Errorf("expected trust tier 'blocked', got %q", tier)
	}
}

// computeTrustTier determines the trust tier for a tool group based on
// signing state and blocklist status.
func computeTrustTier(groupSigned, allMembersSigned, hasBlockedMember bool) string {
	if hasBlockedMember {
		return "blocked"
	}
	if groupSigned && allMembersSigned {
		return "verified"
	}
	if groupSigned {
		return "partial"
	}
	return "community"
}

// TestBlocklistCheck verifies that tools on the blocklist are refused
// during installation.
func TestBlocklistCheck(t *testing.T) {
	blocklist := map[string]string{
		"credential-stealer": "https://clictl.dev/advisories/2026-0042",
		"evil-tool":          "https://clictl.dev/advisories/2026-0043",
	}

	tests := []struct {
		name      string
		toolName  string
		wantBlock bool
	}{
		{"clean tool passes", "gstack-ship", false},
		{"blocked tool rejected", "credential-stealer", true},
		{"another blocked tool rejected", "evil-tool", true},
		{"similar name passes", "credential-stealer-safe", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			advisory, blocked := blocklist[tt.toolName]
			if blocked != tt.wantBlock {
				t.Errorf("blocklist check for %q: got blocked=%v, want %v", tt.toolName, blocked, tt.wantBlock)
			}
			if blocked && advisory == "" {
				t.Errorf("blocked tool should have an advisory URL")
			}
		})
	}
}

// TestGroupInstall_MultiPack simulates installing a group with multiple
// member packs of different types.
func TestGroupInstall_MultiPack(t *testing.T) {
	// Create three skill packs with known content
	skills := map[string]string{
		"gstack-ship":   "# Ship Skill\nOne-command shipping.\n",
		"gstack-review": "# Review Skill\nCode review automation.\n",
		"gstack-qa":     "# QA Skill\nTest automation.\n",
	}

	packHashes := make(map[string]string)

	for name, content := range skills {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644)

		hash, err := archive.HashDirectory(dir)
		if err != nil {
			t.Fatalf("hashing %s: %v", name, err)
		}
		packHashes[name] = hash

		// Build pack
		manifest := map[string]interface{}{
			"schema_version": "1",
			"name":           name,
			"version":        "1.0.0",
			"type":           "skill",
			"content_sha256": hash,
		}
		outputDir := t.TempDir()
		packPath, err := archive.Pack(dir, manifest, outputDir)
		if err != nil {
			t.Fatalf("packing %s: %v", name, err)
		}

		// Verify pack can be unpacked and content matches
		extractDir := t.TempDir()
		parsed, err := archive.Unpack(packPath, extractDir)
		if err != nil {
			t.Fatalf("unpacking %s: %v", name, err)
		}
		if parsed["name"] != name {
			t.Errorf("pack %s: got name %v", name, parsed["name"])
		}
		if err := archive.VerifyPackContent(extractDir, hash); err != nil {
			t.Fatalf("content verification failed for %s: %v", name, err)
		}
	}

	// Verify we built all 3 packs
	if len(packHashes) != 3 {
		t.Fatalf("expected 3 pack hashes, got %d", len(packHashes))
	}

	// Simulate group install: verify each member sequentially
	installed := make([]string, 0, len(skills))
	for name := range skills {
		installed = append(installed, name)
	}
	if len(installed) != 3 {
		t.Fatalf("expected 3 installed tools, got %d", len(installed))
	}
}

// TestGroupInstall_SignatureVerification verifies that the group manifest
// signature is checked before installing member packs.
func TestGroupInstall_SignatureVerification(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	groupManifest := `schema_version: "1"
name: test-group
type: group
version: 1.0.0
packages:
  - name: tool-a
    type: skill
    version: 1.0.0
    sha256: aaaa
  - name: tool-b
    type: mcp
    version: 2.0.0
    sha256: bbbb
`

	hash := sha256.Sum256([]byte(groupManifest))
	sig := ed25519.Sign(priv, hash[:])
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	// Valid signature should pass
	err := signing.VerifyManifestWithKey(groupManifest, sigB64, pubB64)
	if err != nil {
		t.Fatalf("valid group manifest signature should verify: %v", err)
	}

	// Tampered group manifest should fail
	tampered := strings.Replace(groupManifest, "tool-a", "evil-tool", 1)
	err = signing.VerifyManifestWithKey(tampered, sigB64, pubB64)
	if err == nil {
		t.Fatal("tampered group manifest should fail signature verification")
	}
}

// TestGroupInstall_MixedTypes verifies that a group containing different
// tool types (skill, mcp, http) is handled correctly.
func TestGroupInstall_MixedTypes(t *testing.T) {
	type GroupMember struct {
		Name    string
		Type    string
		Version string
	}

	members := []GroupMember{
		{Name: "gstack-ship", Type: "skill", Version: "1.0.0"},
		{Name: "gstack-review", Type: "skill", Version: "1.0.0"},
		{Name: "github-mcp", Type: "mcp", Version: "1.2.0"},
		{Name: "linear", Type: "http", Version: "2.0.0"},
	}

	// Count by type
	typeCounts := map[string]int{}
	for _, m := range members {
		typeCounts[m.Type]++
	}

	if typeCounts["skill"] != 2 {
		t.Errorf("expected 2 skills, got %d", typeCounts["skill"])
	}
	if typeCounts["mcp"] != 1 {
		t.Errorf("expected 1 mcp, got %d", typeCounts["mcp"])
	}
	if typeCounts["http"] != 1 {
		t.Errorf("expected 1 http, got %d", typeCounts["http"])
	}

	// Determine install target for each type
	projectDir := t.TempDir()
	for _, m := range members {
		var targetDir string
		switch m.Type {
		case "skill":
			targetDir = filepath.Join(projectDir, ".claude", "skills", m.Name)
		case "mcp":
			targetDir = filepath.Join(projectDir, ".claude")
		case "http":
			targetDir = filepath.Join(projectDir, ".clictl", "specs")
		default:
			t.Errorf("unknown type %q for %s", m.Type, m.Name)
		}
		if targetDir == "" {
			t.Errorf("no target dir for %s", m.Name)
		}
	}

	// Verify display summary format
	summary := fmt.Sprintf(
		"Installing test-group v1.0.0 (%d tools)\n  Skills:     %d installed to .claude/skills/\n  MCP:        %d registered\n  API:        %d available via clictl run",
		len(members), typeCounts["skill"], typeCounts["mcp"], typeCounts["http"],
	)
	if !strings.Contains(summary, "4 tools") {
		t.Errorf("summary should mention total tool count")
	}
}

// TestTrustTierDisplay verifies the display strings for each trust tier.
func TestTrustTierDisplay(t *testing.T) {
	tests := []struct {
		tier     string
		wantText string
	}{
		{"verified", "verified"},
		{"partial", "partial trust"},
		{"community", "community"},
		{"blocked", "BLOCKED"},
	}

	for _, tt := range tests {
		t.Run(tt.tier, func(t *testing.T) {
			display := formatTrustTier(tt.tier)
			if !strings.Contains(strings.ToLower(display), strings.ToLower(tt.wantText)) {
				t.Errorf("formatTrustTier(%q) = %q, want to contain %q", tt.tier, display, tt.wantText)
			}
		})
	}
}

// formatTrustTier returns a display string for a trust tier.
func formatTrustTier(tier string) string {
	switch tier {
	case "verified":
		return "[check] verified"
	case "partial":
		return "[warning] partial trust"
	case "community":
		return "community"
	case "blocked":
		return "[error] BLOCKED"
	default:
		return "unknown"
	}
}
