// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package sandbox

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/clictl/cli/internal/models"
)

// ============================================================
// Unsafe mode overrides
// ============================================================

func TestUnsafeMode_EnvScrubbed(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret-key")
	t.Setenv("GITHUB_TOKEN", "ghp_token")
	t.Setenv("STRIPE_SECRET_KEY", "sk_live_secret")
	t.Setenv("DATABASE_URL", "postgres://admin:pass@host/db")

	spec := &models.ToolSpec{Name: "community-tool"}
	policy := &Policy{Spec: spec, Enabled: true}

	env := BuildEnv(policy)

	blockedVars := []string{
		"AWS_SECRET_ACCESS_KEY",
		"GITHUB_TOKEN",
		"STRIPE_SECRET_KEY",
		"DATABASE_URL",
	}
	for _, key := range blockedVars {
		for _, e := range env {
			if strings.HasPrefix(e, key+"=") {
				t.Errorf("unsafe mode should still scrub %s from env", key)
			}
		}
	}
}

func TestUnsafeMode_SensitivePathsBlocked(t *testing.T) {
	denied := SensitiveDirs()
	if len(denied) == 0 {
		t.Fatal("SensitiveDirs must return blocked paths even for unsafe mode")
	}
}

func TestUnsafeMode_HostRuntimesAccessible(t *testing.T) {
	spec := &models.ToolSpec{Name: "community-tool"}
	policy := &Policy{Spec: spec, Enabled: true}

	env := BuildEnv(policy)

	foundPath := false
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			foundPath = true
			break
		}
	}
	if !foundPath {
		t.Error("unsafe mode should include PATH so host runtimes (python, git) are accessible")
	}
}

func TestUnsafeMode_ConcurrentCredentialAccess(t *testing.T) {
	t.Setenv("SECRET_VAR", "should-not-leak")

	specs := []*models.ToolSpec{
		{Name: "tool-a"},
		{Name: "tool-b"},
		{Name: "tool-c"},
	}

	for _, spec := range specs {
		policy := &Policy{Spec: spec, Enabled: true}
		env := BuildEnv(policy)
		for _, e := range env {
			if strings.HasPrefix(e, "SECRET_VAR=") {
				t.Errorf("SECRET_VAR leaked to %s", spec.Name)
			}
		}
	}
}

// ============================================================
// Credential control overrides
// ============================================================

func TestCredentialControl_DenySSH(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-agent.sock")

	spec := &models.ToolSpec{
		Name: "git-tool",
		Sandbox: &models.Sandbox{
			Env: &models.EnvPermissions{
				Allow: []string{"SSH_AUTH_SOCK"},
			},
		},
	}

	// Simulate --deny ssh: remove SSH_AUTH_SOCK from the allow list
	filtered := make([]string, 0)
	for _, key := range spec.Sandbox.Env.Allow {
		if key != "SSH_AUTH_SOCK" {
			filtered = append(filtered, key)
		}
	}
	spec.Sandbox.Env.Allow = filtered

	policy := &Policy{Spec: spec, Enabled: true}
	env := BuildEnv(policy)

	for _, e := range env {
		if strings.HasPrefix(e, "SSH_AUTH_SOCK=") {
			t.Error("--deny ssh should block SSH agent socket forwarding")
		}
	}
}

func TestCredentialControl_DenyAll(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-agent.sock")
	t.Setenv("CUSTOM_API_KEY", "key-123")
	t.Setenv("SOME_TOKEN", "tok-abc")

	spec := &models.ToolSpec{
		Name: "multi-cred-tool",
		Sandbox: &models.Sandbox{
			Env: &models.EnvPermissions{
				Allow: []string{"SSH_AUTH_SOCK", "CUSTOM_API_KEY"},
			},
		},
		Auth: &models.Auth{
			Env: models.StringOrSlice{"SOME_TOKEN"},
		},
	}

	// Simulate --deny-all: clear all credential declarations
	spec.Sandbox.Env.Allow = nil
	spec.Auth = nil

	policy := &Policy{Spec: spec, Enabled: true}
	env := BuildEnv(policy)

	for _, e := range env {
		if strings.HasPrefix(e, "SSH_AUTH_SOCK=") {
			t.Error("--deny-all should block SSH agent socket")
		}
		if strings.HasPrefix(e, "CUSTOM_API_KEY=") {
			t.Error("--deny-all should block custom API keys")
		}
		if strings.HasPrefix(e, "SOME_TOKEN=") {
			t.Error("--deny-all should block auth tokens")
		}
	}

	foundMarker := false
	for _, e := range env {
		if e == "CLICTL_SANDBOX=1" {
			foundMarker = true
		}
	}
	if !foundMarker {
		t.Error("CLICTL_SANDBOX=1 marker should be present even with --deny-all")
	}
}

func TestCredentialControl_DryRun(t *testing.T) {
	spec := &models.ToolSpec{
		Name: "dry-run-tool",
		Sandbox: &models.Sandbox{
			Env: &models.EnvPermissions{
				Allow: []string{"SSH_AUTH_SOCK", "API_KEY"},
			},
			Filesystem: &models.FilesystemPermissions{
				Read:  []string{".", "~/.gitconfig"},
				Write: []string{"."},
			},
		},
	}

	summary := fmt.Sprintf(
		"Sandbox permissions for %s:\n  Env: %s\n  Read: %s\n  Write: %s",
		spec.Name,
		strings.Join(spec.Sandbox.Env.Allow, ", "),
		strings.Join(spec.Sandbox.Filesystem.Read, ", "),
		strings.Join(spec.Sandbox.Filesystem.Write, ", "),
	)

	if !strings.Contains(summary, "SSH_AUTH_SOCK") {
		t.Error("dry-run should show SSH_AUTH_SOCK in permissions")
	}
	if !strings.Contains(summary, "~/.gitconfig") {
		t.Error("dry-run should show filesystem read paths")
	}
}

// ============================================================
// Skill sandbox wrapper generation
// ============================================================

func TestGenerateSkillSandboxWrapper(t *testing.T) {
	tests := []struct {
		name       string
		cfg        SkillSandboxConfig
		wantContains []string
	}{
		{
			name: "with hosts",
			cfg: SkillSandboxConfig{
				AllowedHosts: []string{"api.github.com", "api.stripe.com"},
				SkillName:    "test-skill",
			},
			wantContains: []string{
				"#!/usr/bin/env bash",
				"test-skill",
				`exec "$@"`,
				"unset AWS_SECRET_ACCESS_KEY",
				"unset GITHUB_TOKEN",
			},
		},
		{
			name: "strict bash mode",
			cfg: SkillSandboxConfig{
				AllowedHosts: nil,
				SkillName:    "safe-test",
			},
			wantContains: []string{
				"set -euo pipefail",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapper := GenerateSkillSandboxWrapper(tt.cfg)
			for _, want := range tt.wantContains {
				if !strings.Contains(wrapper, want) {
					t.Errorf("expected wrapper to contain %q", want)
				}
			}
		})
	}
}

func TestGenerateSkillSandboxWrapper_NoHosts(t *testing.T) {
	cfg := SkillSandboxConfig{
		AllowedHosts: nil,
		SkillName:    "isolated-skill",
	}

	wrapper := GenerateSkillSandboxWrapper(cfg)

	if !strings.Contains(wrapper, "unset AWS_SECRET_ACCESS_KEY") {
		t.Error("expected env scrubbing even with no declared hosts")
	}

	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(wrapper, "sandbox-exec") || !strings.Contains(wrapper, "deny network") {
			t.Error("expected macOS wrapper to deny network when no hosts declared")
		}
	case "linux":
		if !strings.Contains(wrapper, "no declared network hosts") {
			t.Error("expected Linux wrapper to note no declared hosts")
		}
	case "windows":
		if !strings.Contains(wrapper, "advisory only") {
			t.Error("expected Windows wrapper to note advisory only")
		}
	}
}

func TestGenerateSkillSandboxWrapper_ProxyStripping(t *testing.T) {
	cfg := SkillSandboxConfig{
		AllowedHosts: []string{"example.com"},
		SkillName:    "proxy-test",
	}

	wrapper := GenerateSkillSandboxWrapper(cfg)

	proxyVars := []string{
		"HTTP_PROXY", "HTTPS_PROXY",
		"http_proxy", "https_proxy",
		"NO_PROXY", "no_proxy",
		"ALL_PROXY", "all_proxy",
	}

	for _, v := range proxyVars {
		if !strings.Contains(wrapper, "unset "+v) {
			t.Errorf("expected wrapper to strip proxy var %s", v)
		}
	}
}

func TestGenerateSkillSandboxWrapper_CredentialStripping(t *testing.T) {
	cfg := SkillSandboxConfig{
		AllowedHosts: []string{"example.com"},
		SkillName:    "cred-test",
	}

	wrapper := GenerateSkillSandboxWrapper(cfg)

	credVars := []string{
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
		"GITHUB_TOKEN",
		"GH_TOKEN",
		"GITLAB_TOKEN",
		"DOCKER_PASSWORD",
		"NPM_TOKEN",
		"STRIPE_SECRET_KEY",
		"DATABASE_URL",
		"REDIS_URL",
		"MONGO_URI",
		"VAULT_TOKEN",
	}

	for _, v := range credVars {
		if !strings.Contains(wrapper, "unset "+v) {
			t.Errorf("expected wrapper to strip credential var %s", v)
		}
	}
}

func TestGenerateSkillSandboxWrapper_DarwinProfile(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-specific test")
	}

	cfg := SkillSandboxConfig{
		AllowedHosts: []string{"api.example.com", "cdn.example.com"},
		SkillName:    "darwin-test",
	}

	wrapper := GenerateSkillSandboxWrapper(cfg)

	if !strings.Contains(wrapper, "sandbox-exec") || !strings.Contains(wrapper, "SANDBOX_PROFILE") {
		t.Error("expected macOS wrapper to use sandbox-exec profile")
	}

	for _, host := range cfg.AllowedHosts {
		if !strings.Contains(wrapper, host) {
			t.Errorf("expected wrapper to include %s in allowed hosts", host)
		}
	}
}

func TestGenerateSkillSandboxWrapper_LinuxHosts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific test")
	}

	cfg := SkillSandboxConfig{
		AllowedHosts: []string{"api.example.com"},
		SkillName:    "linux-test",
	}

	wrapper := GenerateSkillSandboxWrapper(cfg)

	if !strings.Contains(wrapper, "CLICTL_ALLOWED_HOSTS") {
		t.Error("expected Linux wrapper to set CLICTL_ALLOWED_HOSTS")
	}

	if !strings.Contains(wrapper, "api.example.com") {
		t.Error("expected wrapper to include allowed host")
	}
}

// ============================================================
// EnvScrub list validation
// ============================================================

func TestEnvScrub_ContainsExpectedVars(t *testing.T) {
	scrubbed := EnvScrub()

	expected := []string{
		"AWS_SECRET_ACCESS_KEY",
		"GITHUB_TOKEN",
		"HTTP_PROXY",
		"HTTPS_PROXY",
	}

	scrubSet := make(map[string]bool, len(scrubbed))
	for _, v := range scrubbed {
		scrubSet[v] = true
	}

	for _, e := range expected {
		if !scrubSet[e] {
			t.Errorf("expected %s in scrub list", e)
		}
	}
}

func TestEnvScrub_NoDuplicates(t *testing.T) {
	scrubbed := EnvScrub()
	seen := make(map[string]bool, len(scrubbed))

	for _, v := range scrubbed {
		if seen[v] {
			t.Errorf("duplicate entry in scrub list: %s", v)
		}
		seen[v] = true
	}
}
