// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package sandbox

import (
	"strings"
	"testing"

	"github.com/clictl/cli/internal/models"
)

func TestBuildEnv_EmptySpec(t *testing.T) {
	spec := &models.ToolSpec{Name: "test-tool"}
	policy := &Policy{Spec: spec, Enabled: true}

	env := BuildEnv(policy)

	// Should contain CLICTL_SANDBOX=1
	found := false
	for _, e := range env {
		if e == "CLICTL_SANDBOX=1" {
			found = true
		}
	}
	if !found {
		t.Error("expected CLICTL_SANDBOX=1 in env")
	}

	// Should contain PATH (almost always set)
	foundPath := false
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			foundPath = true
		}
	}
	if !foundPath {
		t.Error("expected PATH in env")
	}
}

func TestBuildEnv_StripsUndeclaredVars(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "super-secret")
	t.Setenv("GITHUB_TOKEN", "ghp_secret")
	t.Setenv("STRIPE_SECRET_KEY", "sk_live_secret")

	spec := &models.ToolSpec{Name: "test-tool"}
	policy := &Policy{Spec: spec, Enabled: true}

	env := BuildEnv(policy)

	for _, e := range env {
		if strings.HasPrefix(e, "AWS_SECRET_ACCESS_KEY=") {
			t.Error("AWS_SECRET_ACCESS_KEY should be stripped")
		}
		if strings.HasPrefix(e, "GITHUB_TOKEN=") {
			t.Error("GITHUB_TOKEN should be stripped")
		}
		if strings.HasPrefix(e, "STRIPE_SECRET_KEY=") {
			t.Error("STRIPE_SECRET_KEY should be stripped")
		}
	}
}

func TestBuildEnv_PassesDeclaredSandboxEnv(t *testing.T) {
	t.Setenv("MY_CUSTOM_VAR", "allowed-value")
	t.Setenv("ANOTHER_SECRET", "should-not-pass")

	spec := &models.ToolSpec{
		Name: "test-tool",
		Sandbox: &models.Sandbox{
			Env: &models.EnvPermissions{
				Allow: []string{"MY_CUSTOM_VAR"},
			},
		},
	}
	policy := &Policy{Spec: spec, Enabled: true}

	env := BuildEnv(policy)

	foundCustom := false
	for _, e := range env {
		if e == "MY_CUSTOM_VAR=allowed-value" {
			foundCustom = true
		}
		if strings.HasPrefix(e, "ANOTHER_SECRET=") {
			t.Error("ANOTHER_SECRET should not be passed")
		}
	}
	if !foundCustom {
		t.Error("expected MY_CUSTOM_VAR in env")
	}
}

func TestBuildEnv_InjectsAuthEnvVars(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test-token")

	spec := &models.ToolSpec{
		Name: "slack-mcp",
		Auth: &models.Auth{
			Env: models.StringOrSlice{"SLACK_BOT_TOKEN"},
		},
	}
	policy := &Policy{Spec: spec, Enabled: true}

	env := BuildEnv(policy)

	found := false
	for _, e := range env {
		if e == "SLACK_BOT_TOKEN=xoxb-test-token" {
			found = true
		}
	}
	if !found {
		t.Error("expected SLACK_BOT_TOKEN injected via auth config")
	}
}

func TestBuildEnv_InjectsMultipleAuthEnvVars(t *testing.T) {
	t.Setenv("API_KEY", "key123")
	t.Setenv("API_SECRET", "secret456")

	spec := &models.ToolSpec{
		Name: "test-tool",
		Auth: &models.Auth{
			Env: models.StringOrSlice{"API_KEY", "API_SECRET"},
		},
	}
	policy := &Policy{Spec: spec, Enabled: true}

	env := BuildEnv(policy)

	foundKey := false
	foundSecret := false
	for _, e := range env {
		if e == "API_KEY=key123" {
			foundKey = true
		}
		if e == "API_SECRET=secret456" {
			foundSecret = true
		}
	}
	if !foundKey {
		t.Error("expected API_KEY=key123")
	}
	if !foundSecret {
		t.Error("expected API_SECRET=secret456")
	}
}

func TestBuildEnv_PassesServerEnvLiterals(t *testing.T) {
	spec := &models.ToolSpec{
		Name: "test-tool",
		Server: &models.Server{
			Type:    "stdio",
			Command: "echo",
			Env: map[string]string{
				"LOG_LEVEL": "debug",
				"NODE_ENV":  "production",
			},
		},
	}
	policy := &Policy{Spec: spec, Enabled: true}

	env := BuildEnv(policy)

	foundLog := false
	foundNode := false
	for _, e := range env {
		if e == "LOG_LEVEL=debug" {
			foundLog = true
		}
		if e == "NODE_ENV=production" {
			foundNode = true
		}
	}
	if !foundLog {
		t.Error("expected LOG_LEVEL=debug from server.env")
	}
	if !foundNode {
		t.Error("expected NODE_ENV=production from server.env")
	}
}

func TestBuildEnv_SkipsUnsetSandboxEnv(t *testing.T) {
	// Do NOT set MISSING_VAR in the test env
	spec := &models.ToolSpec{
		Name: "test-tool",
		Sandbox: &models.Sandbox{
			Env: &models.EnvPermissions{
				Allow: []string{"MISSING_VAR"},
			},
		},
	}
	policy := &Policy{Spec: spec, Enabled: true}

	env := BuildEnv(policy)

	for _, e := range env {
		if strings.HasPrefix(e, "MISSING_VAR=") {
			t.Error("MISSING_VAR should not appear when not set in parent env")
		}
	}
}

func TestEnvScrubBlocklist(t *testing.T) {
	// These environment variables enable library injection / code execution
	// and must NEVER leak into a sandboxed subprocess.
	dangerousVars := []struct {
		key   string
		value string
		why   string
	}{
		{"LD_PRELOAD", "/tmp/evil.so", "Linux shared library injection"},
		{"LD_LIBRARY_PATH", "/tmp/evil", "Linux library search path hijack"},
		{"DYLD_INSERT_LIBRARIES", "/tmp/evil.dylib", "macOS dylib injection"},
		{"DYLD_LIBRARY_PATH", "/tmp/evil", "macOS library search path hijack"},
		{"NODE_OPTIONS", "--require=/tmp/evil.js", "Node.js code injection"},
		{"RUBYOPT", "-r/tmp/evil.rb", "Ruby code injection"},
		{"PERL5OPT", "-M/tmp/Evil", "Perl module injection"},
		{"PYTHONSTARTUP", "/tmp/evil.py", "Python startup script injection"},
		{"BASH_ENV", "/tmp/evil.sh", "Bash environment script injection"},
		{"ENV", "/tmp/evil.sh", "Shell environment script injection"},
		{"CDPATH", "/tmp/evil", "cd path hijack"},
	}

	for _, dv := range dangerousVars {
		t.Run(dv.key, func(t *testing.T) {
			t.Setenv(dv.key, dv.value)

			spec := &models.ToolSpec{Name: "test-tool"}
			policy := &Policy{Spec: spec, Enabled: true}

			env := BuildEnv(policy)

			for _, e := range env {
				if strings.HasPrefix(e, dv.key+"=") {
					t.Errorf("%s should be scrubbed (%s), but found in env", dv.key, dv.why)
				}
			}
		})
	}
}

// TestEnvScrubBlocklist_NotInEssentialVars ensures that dangerous vars are not
// accidentally included in the essentialVars allowlist.
func TestEnvScrubBlocklist_NotInEssentialVars(t *testing.T) {
	dangerous := []string{
		"LD_PRELOAD", "LD_LIBRARY_PATH",
		"DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH",
		"NODE_OPTIONS", "RUBYOPT", "PERL5OPT",
		"PYTHONSTARTUP", "BASH_ENV", "ENV", "CDPATH",
	}

	for _, key := range dangerous {
		for _, essential := range essentialVars {
			if essential == key {
				t.Errorf("%q must not be in essentialVars (injection risk)", key)
			}
		}
	}
}

func TestBuildEnv_NilAuthDoesNotInject(t *testing.T) {
	t.Setenv("SHOULD_NOT_PASS", "secret")

	spec := &models.ToolSpec{
		Name: "test-tool",
		Auth: nil,
	}
	policy := &Policy{Spec: spec, Enabled: true}

	env := BuildEnv(policy)

	for _, e := range env {
		if strings.HasPrefix(e, "SHOULD_NOT_PASS=") {
			t.Error("nil auth should not inject env vars")
		}
	}
}
