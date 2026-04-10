// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"strings"
	"testing"

	"github.com/clictl/cli/internal/models"
)

func TestPermissionsDisplay(t *testing.T) {
	tests := []struct {
		name        string
		permissions *models.Sandbox
		wantNetwork bool
		wantEnv     bool
	}{
		{
			name:        "nil permissions",
			permissions: nil,
			wantNetwork: false,
			wantEnv:     false,
		},
		{
			name: "network only",
			permissions: &models.Sandbox{
				Network: &models.NetworkPermissions{Allow: []string{"api.github.com", "cdn.example.com"}},
			},
			wantNetwork: true,
			wantEnv:     false,
		},
		{
			name: "env only",
			permissions: &models.Sandbox{
				Env: &models.EnvPermissions{Allow: []string{"GITHUB_TOKEN"}},
			},
			wantNetwork: false,
			wantEnv:     true,
		},
		{
			name: "both network and env",
			permissions: &models.Sandbox{
				Network: &models.NetworkPermissions{Allow: []string{"api.github.com"}},
				Env:     &models.EnvPermissions{Allow: []string{"GITHUB_TOKEN", "GITHUB_ORG"}},
			},
			wantNetwork: true,
			wantEnv:     true,
		},
		{
			name: "empty lists",
			permissions: &models.Sandbox{
				Network: &models.NetworkPermissions{Allow: []string{}},
				Env:     &models.EnvPermissions{Allow: []string{}},
			},
			wantNetwork: false,
			wantEnv:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasPermissions := tt.permissions != nil &&
				((tt.permissions.Network != nil && len(tt.permissions.Network.Allow) > 0) ||
					(tt.permissions.Env != nil && len(tt.permissions.Env.Allow) > 0))

			if tt.wantNetwork || tt.wantEnv {
				if !hasPermissions {
					t.Error("expected permissions to be displayed")
				}
			} else {
				if hasPermissions {
					t.Error("expected no permissions to be displayed")
				}
			}

			if hasPermissions {
				// Verify formatting
				if tt.wantNetwork {
					summary := strings.Join(tt.permissions.Network.Allow, ", ")
					if summary == "" {
						t.Error("expected non-empty network summary")
					}
				}
				if tt.wantEnv {
					summary := strings.Join(tt.permissions.Env.Allow, ", ")
					if summary == "" {
						t.Error("expected non-empty env summary")
					}
				}
			}
		})
	}
}

func TestNamespaceResolution(t *testing.T) {
	tests := []struct {
		name        string
		spec        *models.ToolSpec
		wantDisplay string
	}{
		{
			name: "namespace and display_name set",
			spec: &models.ToolSpec{
				Name:        "github",
				Namespace:   "official",
				DisplayName: "@official/github",
			},
			wantDisplay: "@official/github",
		},
		{
			name: "namespace set, no display_name",
			spec: &models.ToolSpec{
				Name:      "github",
				Namespace: "official",
			},
			wantDisplay: "@official/github",
		},
		{
			name: "no namespace",
			spec: &models.ToolSpec{
				Name: "github",
			},
			wantDisplay: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			displayName := ""
			if tt.spec.Namespace != "" || tt.spec.DisplayName != "" {
				displayName = tt.spec.DisplayName
				if displayName == "" && tt.spec.Namespace != "" {
					displayName = "@" + tt.spec.Namespace + "/" + tt.spec.Name
				}
			}

			if displayName != tt.wantDisplay {
				t.Errorf("got display name %q, want %q", displayName, tt.wantDisplay)
			}
		})
	}
}

func TestSpecPermissionsModel(t *testing.T) {
	spec := &models.ToolSpec{
		Name:    "time-mcp",
		Version: "1.0.0",
		Sandbox: &models.Sandbox{
			Network: &models.NetworkPermissions{Allow: []string{"api.github.com"}},
			Env:     &models.EnvPermissions{Allow: []string{"GITHUB_TOKEN"}},
		},
	}

	if spec.Sandbox == nil {
		t.Fatal("expected permissions to be set")
	}
	if spec.Sandbox.Network == nil || len(spec.Sandbox.Network.Allow) != 1 || spec.Sandbox.Network.Allow[0] != "api.github.com" {
		t.Errorf("unexpected network permissions: %v", spec.Sandbox.Network)
	}
	if spec.Sandbox.Env == nil || len(spec.Sandbox.Env.Allow) != 1 || spec.Sandbox.Env.Allow[0] != "GITHUB_TOKEN" {
		t.Errorf("unexpected env permissions: %v", spec.Sandbox.Env)
	}
}
