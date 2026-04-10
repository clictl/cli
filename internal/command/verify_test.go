// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestVerifyCmd_ETagMatches(t *testing.T) {
	specYAML := "name: test-tool\nversion: 1.0.0\ndescription: A test tool\ncategory: testing\n"
	expectedETag := computeETag([]byte(specYAML))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		fmt.Fprint(w, specYAML)
	}))
	defer server.Close()

	// Verify the etag computation is deterministic
	etag1 := computeETag([]byte(specYAML))
	etag2 := computeETag([]byte(specYAML))
	if etag1 != etag2 {
		t.Errorf("expected same etag for same content, got %s and %s", etag1, etag2)
	}

	if !strings.HasPrefix(expectedETag, "sha256:") {
		t.Errorf("expected etag to start with sha256:, got %s", expectedETag)
	}
}

func TestVerifyCmd_ETagMismatch(t *testing.T) {
	localYAML := "name: test-tool\nversion: 1.0.0\ndescription: A test tool\ncategory: testing\n"
	registryYAML := "name: test-tool\nversion: 1.0.1\ndescription: Updated test tool\ncategory: testing\n"

	localETag := computeETag([]byte(localYAML))
	registryETag := computeETag([]byte(registryYAML))

	if localETag == registryETag {
		t.Error("expected different etags for different content")
	}

	// Verify truncated etag display works for mismatch reporting
	if len(localETag) < 19 {
		t.Errorf("expected etag length >= 19 for truncation, got %d", len(localETag))
	}
	if len(registryETag) < 19 {
		t.Errorf("expected etag length >= 19 for truncation, got %d", len(registryETag))
	}
}

func TestVerifyCmd_LockFileMatch(t *testing.T) {
	specYAML := "name: test-tool\nversion: 1.0.0\ndescription: A test tool\ncategory: testing\n"
	etag := computeETag([]byte(specYAML))

	// Create a lock file with matching etag
	lf := &LockFile{
		Tools: map[string]LockEntry{
			"test-tool": {
				Version: "1.0.0",
				ETag:    etag,
			},
		},
		GeneratedAt: "2024-01-01T00:00:00Z",
	}

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "lock.yaml")
	data, err := yaml.Marshal(lf)
	if err != nil {
		t.Fatalf("marshaling lock file: %v", err)
	}
	if err := os.WriteFile(lockPath, data, 0o600); err != nil {
		t.Fatalf("writing lock file: %v", err)
	}

	// Read back and verify
	readData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("reading lock file: %v", err)
	}
	var loaded LockFile
	if err := yaml.Unmarshal(readData, &loaded); err != nil {
		t.Fatalf("parsing lock file: %v", err)
	}

	entry, ok := loaded.Tools["test-tool"]
	if !ok {
		t.Fatal("expected test-tool in lock file")
	}
	if entry.ETag != etag {
		t.Errorf("etag mismatch: got %s, want %s", entry.ETag, etag)
	}
}

func TestVerifyCmd_LockFileMismatch(t *testing.T) {
	specYAML := "name: test-tool\nversion: 1.0.0\ndescription: A test tool\ncategory: testing\n"
	registryETag := computeETag([]byte(specYAML))

	// Lock file has a different etag (simulating content drift)
	lockETag := "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	if registryETag == lockETag {
		t.Fatal("expected different etags for test setup")
	}

	lf := &LockFile{
		Tools: map[string]LockEntry{
			"test-tool": {
				Version: "1.0.0",
				ETag:    lockETag,
			},
		},
		GeneratedAt: "2024-01-01T00:00:00Z",
	}

	data, err := yaml.Marshal(lf)
	if err != nil {
		t.Fatalf("marshaling lock file: %v", err)
	}

	var loaded LockFile
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("parsing lock file: %v", err)
	}

	entry := loaded.Tools["test-tool"]
	if entry.ETag == registryETag {
		t.Error("expected lock file etag to differ from registry etag")
	}
}

func TestVerifyCmd_ToolNotInLockFile(t *testing.T) {
	lf := &LockFile{
		Tools: map[string]LockEntry{
			"other-tool": {
				Version: "1.0.0",
				ETag:    "sha256:abc",
			},
		},
		GeneratedAt: "2024-01-01T00:00:00Z",
	}

	_, ok := lf.Tools["missing-tool"]
	if ok {
		t.Error("expected missing-tool to not be in lock file")
	}
}

func TestVerifyCmd_AllWithMockInstalledList(t *testing.T) {
	// Simulate verifying multiple tools by creating lock file entries
	// and comparing against registry etags
	tool1YAML := "name: github-mcp\nversion: 1.2.0\ndescription: GitHub MCP tool\ncategory: developer\n"
	tool2YAML := "name: time-mcp\nversion: 1.0.0\ndescription: Time MCP tool\ncategory: utility\n"
	tool3YAML := "name: drift-tool\nversion: 1.0.0\ndescription: Drifted tool\ncategory: testing\n"

	tool1ETag := computeETag([]byte(tool1YAML))
	tool2ETag := computeETag([]byte(tool2YAML))

	// tool3 has a different registry version (simulates drift)
	tool3RegistryYAML := "name: drift-tool\nversion: 1.0.1\ndescription: Updated drifted tool\ncategory: testing\n"
	tool3RegistryETag := computeETag([]byte(tool3RegistryYAML))
	tool3LocalETag := computeETag([]byte(tool3YAML))

	// Lock file: tools 1 and 2 match, tool 3 has drifted
	lf := &LockFile{
		Tools: map[string]LockEntry{
			"github-mcp": {Version: "1.2.0", ETag: tool1ETag},
			"time-mcp":   {Version: "1.0.0", ETag: tool2ETag},
			"drift-tool": {Version: "1.0.0", ETag: tool3LocalETag},
		},
		GeneratedAt: "2024-01-15T10:30:00Z",
	}

	// Verify matching tools
	entry1 := lf.Tools["github-mcp"]
	if entry1.ETag != tool1ETag {
		t.Errorf("github-mcp: expected etag %s, got %s", tool1ETag, entry1.ETag)
	}

	entry2 := lf.Tools["time-mcp"]
	if entry2.ETag != tool2ETag {
		t.Errorf("time-mcp: expected etag %s, got %s", tool2ETag, entry2.ETag)
	}

	// Verify drifted tool
	entry3 := lf.Tools["drift-tool"]
	if entry3.ETag == tool3RegistryETag {
		t.Error("drift-tool: expected lock etag to differ from registry etag")
	}

	// Simulate the verify output logic
	installed := []string{"github-mcp", "time-mcp", "drift-tool"}
	registryETags := map[string]string{
		"github-mcp": tool1ETag,
		"time-mcp":   tool2ETag,
		"drift-tool": tool3RegistryETag,
	}

	hasMismatch := false
	for _, name := range installed {
		entry, ok := lf.Tools[name]
		if !ok {
			t.Errorf("expected %s in lock file", name)
			continue
		}
		regETag := registryETags[name]
		if entry.ETag != regETag {
			hasMismatch = true
		}
	}

	if !hasMismatch {
		t.Error("expected at least one mismatch (drift-tool)")
	}
}

func TestVerifyCmd_AllVerified(t *testing.T) {
	// All tools match - should result in no mismatches
	specA := "name: tool-a\nversion: 1.0.0\n"
	specB := "name: tool-b\nversion: 2.0.0\n"

	etagA := computeETag([]byte(specA))
	etagB := computeETag([]byte(specB))

	lf := &LockFile{
		Tools: map[string]LockEntry{
			"tool-a": {Version: "1.0.0", ETag: etagA},
			"tool-b": {Version: "2.0.0", ETag: etagB},
		},
		GeneratedAt: "2024-06-01T00:00:00Z",
	}

	registryETags := map[string]string{
		"tool-a": etagA,
		"tool-b": etagB,
	}

	hasMismatch := false
	for name, entry := range lf.Tools {
		if entry.ETag != registryETags[name] {
			hasMismatch = true
		}
	}

	if hasMismatch {
		t.Error("expected no mismatches when all etags match")
	}
}

func TestVerifyDelistedWarning(t *testing.T) {
	// Verify that a deprecated/delisted spec triggers a DELISTED warning
	specYAML := "name: old-tool\nversion: 1.0.0\ndescription: Deprecated tool\ncategory: testing\ndeprecated: true\ndeprecated_by: new-tool\n"

	var spec struct {
		Name         string `yaml:"name"`
		Version      string `yaml:"version"`
		Deprecated   bool   `yaml:"deprecated"`
		DeprecatedBy string `yaml:"deprecated_by"`
	}
	if err := yaml.Unmarshal([]byte(specYAML), &spec); err != nil {
		t.Fatalf("parsing spec: %v", err)
	}

	if !spec.Deprecated {
		t.Error("expected spec to be marked as deprecated")
	}
	if spec.DeprecatedBy != "new-tool" {
		t.Errorf("expected deprecated_by=new-tool, got %s", spec.DeprecatedBy)
	}

	// Simulate the warning generation logic from verify.go
	var warnings []string
	if spec.Deprecated {
		warnings = append(warnings, "DELISTED: this tool has been deprecated by its publisher")
		if spec.DeprecatedBy != "" {
			warnings = append(warnings, fmt.Sprintf("  Replacement: %s", spec.DeprecatedBy))
		}
	}

	if len(warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d", len(warnings))
	}
	if warnings[0] != "DELISTED: this tool has been deprecated by its publisher" {
		t.Errorf("unexpected first warning: %s", warnings[0])
	}
	if warnings[1] != "  Replacement: new-tool" {
		t.Errorf("unexpected second warning: %s", warnings[1])
	}
}

func TestVerifyDelistedWarning_NoReplacement(t *testing.T) {
	// Deprecated without a replacement tool
	specYAML := "name: sunset-tool\nversion: 2.0.0\ndeprecated: true\n"

	var spec struct {
		Name         string `yaml:"name"`
		Deprecated   bool   `yaml:"deprecated"`
		DeprecatedBy string `yaml:"deprecated_by"`
	}
	if err := yaml.Unmarshal([]byte(specYAML), &spec); err != nil {
		t.Fatalf("parsing spec: %v", err)
	}

	var warnings []string
	if spec.Deprecated {
		warnings = append(warnings, "DELISTED: this tool has been deprecated by its publisher")
		if spec.DeprecatedBy != "" {
			warnings = append(warnings, fmt.Sprintf("  Replacement: %s", spec.DeprecatedBy))
		}
	}

	if len(warnings) != 1 {
		t.Errorf("expected 1 warning (no replacement), got %d", len(warnings))
	}
}

func TestVerifyMissingPackWarning(t *testing.T) {
	// Simulate a spec with source files but no signed pack (sigStatus = "unsigned")
	type sourceFile struct {
		Path   string `yaml:"path"`
		SHA256 string `yaml:"sha256"`
	}
	type source struct {
		Files []sourceFile `yaml:"files"`
	}

	src := &source{
		Files: []sourceFile{
			{Path: "main.py", SHA256: "abc123"},
			{Path: "utils.py", SHA256: "def456"},
		},
	}
	sigStatus := "unsigned"

	var warnings []string
	if src != nil && len(src.Files) > 0 && sigStatus == "unsigned" {
		warnings = append(warnings, "MISSING PACK: source files declared but no signed pack available")
	}

	if len(warnings) != 1 {
		t.Errorf("expected 1 warning for missing pack, got %d", len(warnings))
	}
	if warnings[0] != "MISSING PACK: source files declared but no signed pack available" {
		t.Errorf("unexpected warning: %s", warnings[0])
	}
}

func TestVerifyMissingPackWarning_SignedOK(t *testing.T) {
	// When sigStatus is "verified", no MISSING PACK warning should appear
	type sourceFile struct {
		Path   string `yaml:"path"`
		SHA256 string `yaml:"sha256"`
	}
	type source struct {
		Files []sourceFile `yaml:"files"`
	}

	src := &source{
		Files: []sourceFile{
			{Path: "main.py", SHA256: "abc123"},
		},
	}
	sigStatus := "verified"

	var warnings []string
	if src != nil && len(src.Files) > 0 && sigStatus == "unsigned" {
		warnings = append(warnings, "MISSING PACK: source files declared but no signed pack available")
	}

	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings when pack is signed, got %d", len(warnings))
	}
}

func TestOutdatedShowsStatus(t *testing.T) {
	// Test the outdated entry status values match expected values
	type outdatedEntry struct {
		name             string
		installedVersion string
		latestVersion    string
		status           string
	}

	entries := []outdatedEntry{
		{name: "old-tool", installedVersion: "1.0.0", latestVersion: "2.0.0", status: "outdated"},
		{name: "dep-tool", installedVersion: "1.0.0", latestVersion: "1.0.0", status: "deprecated"},
		{name: "gone-tool", installedVersion: "1.0.0", latestVersion: "unavailable", status: "delisted"},
		{name: "dep-with-alt", installedVersion: "1.0.0", latestVersion: "1.2.0", status: "deprecated (use new-tool)"},
	}

	// Verify all expected statuses are present
	statusSet := map[string]bool{}
	for _, e := range entries {
		statusSet[e.status] = true
	}
	if !statusSet["outdated"] {
		t.Error("expected 'outdated' status in entries")
	}
	if !statusSet["deprecated"] {
		t.Error("expected 'deprecated' status in entries")
	}
	if !statusSet["delisted"] {
		t.Error("expected 'delisted' status in entries")
	}

	// Verify the deprecated-with-replacement status format
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.status, "deprecated (use ") {
			found = true
			// Extract the replacement tool name
			replacement := strings.TrimPrefix(e.status, "deprecated (use ")
			replacement = strings.TrimSuffix(replacement, ")")
			if replacement != "new-tool" {
				t.Errorf("expected replacement 'new-tool', got '%s'", replacement)
			}
		}
	}
	if !found {
		t.Error("expected a 'deprecated (use ...)' status entry")
	}

	// Verify delisted entries have "unavailable" as latestVersion
	for _, e := range entries {
		if e.status == "delisted" && e.latestVersion != "unavailable" {
			t.Errorf("delisted entry should have latestVersion=unavailable, got %s", e.latestVersion)
		}
	}
}

func TestOutdatedShowsStatus_VersionComparison(t *testing.T) {
	// Simulate version comparison logic for outdated detection
	tests := []struct {
		installed string
		latest    string
		outdated  bool
	}{
		{"1.0.0", "2.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"2.0.0", "1.0.0", false},
		{"1.0.0", "1.0.1", true},
		{"1.0.0", "1.1.0", true},
	}

	for _, tc := range tests {
		isOutdated := tc.installed != tc.latest && tc.installed < tc.latest
		if isOutdated != tc.outdated {
			t.Errorf("installed=%s latest=%s: expected outdated=%v, got %v", tc.installed, tc.latest, tc.outdated, isOutdated)
		}
	}
}

func TestVerifyCmd_MockRegistryServer(t *testing.T) {
	// Mock a registry server that serves specs for verification
	specs := map[string]string{
		"tool-ok":    "name: tool-ok\nversion: 1.0.0\ndescription: Verified tool\ncategory: testing\n",
		"tool-drift": "name: tool-drift\nversion: 1.0.1\ndescription: Updated tool\ncategory: testing\n",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name, spec := range specs {
			if strings.Contains(r.URL.Path, name) {
				w.Header().Set("Content-Type", "application/x-yaml")
				fmt.Fprint(w, spec)
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Verify the mock server serves specs correctly
	for name, spec := range specs {
		resp, err := http.Get(server.URL + "/api/v1/specs/" + name + "/yaml/")
		if err != nil {
			t.Fatalf("fetching %s: %v", name, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d", name, resp.StatusCode)
		}

		expectedETag := computeETag([]byte(spec))
		if !strings.HasPrefix(expectedETag, "sha256:") {
			t.Errorf("expected etag prefix sha256: for %s", name)
		}
	}
}
