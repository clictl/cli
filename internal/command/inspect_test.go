// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/clictl/cli/internal/models"
)

func TestRenderInspectText_PublisherInfo(t *testing.T) {
	spec := &models.ToolSpec{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "A test tool",
		Category:    "testing",
		Actions: []models.Action{
			{Name: "get", Method: "GET", Path: "/data", Description: "Get data"},
		},
	}

	tests := []struct {
		name       string
		ownership  map[string]any
		wantTier   string
		wantNS     string
		wantVerify bool
	}{
		{
			name: "verified publisher",
			ownership: map[string]any{
				"tier":           "verified",
				"namespace":      "acme-corp",
				"verified":       true,
				"workspace_name": "Acme Corp",
			},
			wantTier:   "Verified",
			wantNS:     "acme-corp",
			wantVerify: true,
		},
		{
			name: "partner publisher",
			ownership: map[string]any{
				"tier":      "partner",
				"namespace": "partner-org",
				"verified":  true,
			},
			wantTier:   "Partner",
			wantNS:     "partner-org",
			wantVerify: true,
		},
		{
			name: "premier publisher",
			ownership: map[string]any{
				"tier":           "premier",
				"namespace":      "big-co",
				"verified":       true,
				"workspace_name": "Big Company",
			},
			wantTier:   "Premier Partner",
			wantNS:     "big-co",
			wantVerify: true,
		},
		{
			name: "community publisher",
			ownership: map[string]any{
				"tier":     "community",
				"verified": false,
			},
			wantTier:   "Community",
			wantVerify: false,
		},
		{
			name:      "no ownership data",
			ownership: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			old := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			err := renderInspectText(spec, nil, tt.ownership, "")

			w.Close()
			os.Stdout = old

			if err != nil {
				t.Fatalf("renderInspectText returned error: %v", err)
			}

			var buf bytes.Buffer
			buf.ReadFrom(r)
			output := buf.String()

			if tt.ownership == nil {
				if strings.Contains(output, "==> Publisher") {
					t.Error("expected no Publisher section when ownership is nil")
				}
				return
			}

			tier, _ := tt.ownership["tier"].(string)
			if tier == "" {
				return
			}

			if !strings.Contains(output, "==> Publisher") {
				t.Error("expected '==> Publisher' section in output")
			}

			if tt.wantTier != "" && !strings.Contains(output, "Tier: "+tt.wantTier) {
				t.Errorf("expected tier %q in output, got:\n%s", tt.wantTier, output)
			}

			if tt.wantNS != "" && !strings.Contains(output, "Namespace: "+tt.wantNS) {
				t.Errorf("expected namespace %q in output, got:\n%s", tt.wantNS, output)
			}

			if tt.wantVerify && !strings.Contains(output, "Verified: yes") {
				t.Errorf("expected 'Verified: yes' in output, got:\n%s", output)
			}
		})
	}
}

func TestRenderInspectText_PublisherWithWorkspaceName(t *testing.T) {
	spec := &models.ToolSpec{
		Name:        "ws-tool",
		Version:     "2.0.0",
		Description: "Workspace tool",
		Category:    "development",
		Actions: []models.Action{
			{Name: "run", Method: "POST", Path: "/run", Description: "Run it"},
		},
	}

	ownership := map[string]any{
		"tier":           "partner",
		"namespace":      "ws-ns",
		"verified":       true,
		"workspace_name": "My Workspace",
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := renderInspectText(spec, nil, ownership, "")

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("renderInspectText returned error: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "Maintained by My Workspace") {
		t.Errorf("expected 'Maintained by My Workspace' in output, got:\n%s", output)
	}
}

func TestRenderInspectText_PackageInfo(t *testing.T) {
	spec := &models.ToolSpec{
		Name:        "github-mcp",
		Version:     "1.2.0",
		Description: "GitHub MCP server",
		Category:    "developer-tools",
		Package: &models.Package{
			Registry: "npm",
			Name:     "@modelcontextprotocol/server-github",
			Version:  "1.2.0",
			SHA256:   "abc123def456",
		},
		Actions: []models.Action{
			{Name: "search", Method: "GET", Path: "/search", Description: "Search repos"},
		},
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := renderInspectText(spec, nil, nil, "")

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("renderInspectText returned error: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "==> Package") {
		t.Error("expected '==> Package' section in output")
	}
	if !strings.Contains(output, "Registry: npm") {
		t.Errorf("expected 'Registry: npm' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Package:  @modelcontextprotocol/server-github") {
		t.Errorf("expected package name in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Version:  1.2.0") {
		t.Errorf("expected 'Version:  1.2.0' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "SHA256:   abc123def456") {
		t.Errorf("expected SHA256 in output, got:\n%s", output)
	}
}

func TestRenderInspectText_NoPackageInfo(t *testing.T) {
	spec := &models.ToolSpec{
		Name:        "simple-tool",
		Version:     "1.0.0",
		Description: "A simple tool",
		Category:    "testing",
		Actions: []models.Action{
			{Name: "get", Method: "GET", Path: "/data", Description: "Get data"},
		},
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := renderInspectText(spec, nil, nil, "")

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("renderInspectText returned error: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if strings.Contains(output, "==> Package") {
		t.Error("expected no Package section when no package fields are set")
	}
}
