// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clictl/cli/internal/models"
)

// TestEscape_PythonCredentialStealer simulates a compromised Python MCP server
// that tries to read ~/.ssh/id_rsa and ~/.aws/credentials via env vars.
// With sandbox enabled, these env vars should not be available.
func TestEscape_PythonCredentialStealer(t *testing.T) {
	// Simulate sensitive env vars that a credential stealer would target
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	t.Setenv("AWS_SESSION_TOKEN", "FwoGZXIvYXdzEBY...")
	t.Setenv("GITHUB_TOKEN", "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/home/user/.config/gcloud/application_default_credentials.json")
	t.Setenv("STRIPE_SECRET_KEY", "sk_live_xxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	t.Setenv("DATABASE_URL", "postgres://admin:password@prod-db:5432/app")

	// Spec declares only the vars it legitimately needs
	spec := &models.ToolSpec{
		Name: "malicious-python-mcp",
		Server: &models.Server{
			Type:    "stdio",
			Command: "python3",
			Args:    []string{"-m", "some_mcp_server"},
		},
		Auth: &models.Auth{
			Env: models.StringOrSlice{"LEGITIMATE_API_KEY"},
		},
	}
	policy := &Policy{Spec: spec, Enabled: true}

	env := BuildEnv(policy)

	// None of the sensitive vars should be present
	blocked := []string{
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
		"GITHUB_TOKEN",
		"GOOGLE_APPLICATION_CREDENTIALS",
		"STRIPE_SECRET_KEY",
		"DATABASE_URL",
	}

	for _, key := range blocked {
		for _, e := range env {
			if strings.HasPrefix(e, key+"=") {
				t.Errorf("credential stealer would have access to %s", key)
			}
		}
	}
}

// TestEscape_NodeJSEnvExfiltration simulates a compromised npm package that
// iterates over process.env to find secrets. With sandbox, process.env is
// the scrubbed allowlist.
func TestEscape_NodeJSEnvExfiltration(t *testing.T) {
	// Set a variety of secrets that npm supply chain attacks target
	secrets := map[string]string{
		"NPM_TOKEN":             "npm_xxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"GH_TOKEN":              "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"DOCKER_PASSWORD":       "dckr_xxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"SLACK_WEBHOOK_URL":     "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX",
		"VERCEL_TOKEN":          "XXXXXXXXXXXXXXXXXXXXXXXX",
		"NETLIFY_AUTH_TOKEN":    "XXXXXXXXXXXXXXXXXXXXXXXX",
		"HEROKU_API_KEY":        "XXXXXXXXXXXXXXXXXXXXXXXX",
		"TWILIO_AUTH_TOKEN":     "XXXXXXXXXXXXXXXXXXXXXXXX",
		"SENDGRID_API_KEY":      "SG.XXXXXXXXXXXXXXXXXXXX",
		"OPENAI_API_KEY":        "sk-XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
		"ANTHROPIC_API_KEY":     "sk-ant-XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
		"SSH_PRIVATE_KEY":       "-----BEGIN OPENSSH PRIVATE KEY-----",
	}

	for k, v := range secrets {
		t.Setenv(k, v)
	}

	// Typical MCP server spec - only needs its own API key
	spec := &models.ToolSpec{
		Name: "compromised-npm-mcp",
		Server: &models.Server{
			Type:    "stdio",
			Command: "npx",
			Args:    []string{"-y", "@some-org/mcp-server"},
		},
		Sandbox: &models.Sandbox{
			Env: &models.EnvPermissions{
				Allow: []string{"OPENAI_API_KEY"}, // legitimately needs this one
			},
		},
	}
	policy := &Policy{Spec: spec, Enabled: true}

	env := BuildEnv(policy)

	// OPENAI_API_KEY should be present (declared in sandbox.env.allow)
	foundOpenAI := false
	for _, e := range env {
		if strings.HasPrefix(e, "OPENAI_API_KEY=") {
			foundOpenAI = true
		}
	}
	if !foundOpenAI {
		t.Error("OPENAI_API_KEY should be allowed (declared in sandbox.env.allow)")
	}

	// All other secrets should be blocked
	for key := range secrets {
		if key == "OPENAI_API_KEY" {
			continue
		}
		for _, e := range env {
			if strings.HasPrefix(e, key+"=") {
				t.Errorf("exfiltration would succeed for %s - should be blocked", key)
			}
		}
	}
}

// TestEscape_MCPToolCallExfiltration simulates an MCP server that tries to
// use its tool responses to exfiltrate data from the filesystem via env vars.
// Even if the tool can't read files directly (Phase 2), it tries env vars.
func TestEscape_MCPToolCallExfiltration(t *testing.T) {
	// Simulate a CI environment with many secrets
	ciSecrets := map[string]string{
		"CI_JOB_TOKEN":     "glcbt-xxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"CI_DEPLOY_KEY":    "-----BEGIN RSA PRIVATE KEY-----",
		"KUBECONFIG":       "/home/user/.kube/config",
		"VAULT_TOKEN":      "hvs.xxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"CONSUL_HTTP_TOKEN": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
		"REDIS_URL":        "redis://:password@redis.internal:6379/0",
		"MONGO_URI":        "mongodb+srv://admin:password@cluster.mongodb.net/db",
	}

	for k, v := range ciSecrets {
		t.Setenv(k, v)
	}

	spec := &models.ToolSpec{
		Name: "malicious-mcp-tool",
		Server: &models.Server{
			Type:    "stdio",
			Command: "node",
			Args:    []string{"malicious-server.js"},
		},
	}
	policy := &Policy{Spec: spec, Enabled: true}

	env := BuildEnv(policy)

	for key := range ciSecrets {
		for _, e := range env {
			if strings.HasPrefix(e, key+"=") {
				t.Errorf("CI secret %s would be exfiltrated", key)
			}
		}
	}
}

// TestEscape_FilesystemDenyList verifies that sensitive directories are
// in the deny list for all platforms.
func TestEscape_FilesystemDenyList(t *testing.T) {
	home, _ := os.UserHomeDir()
	if home == "" {
		t.Skip("no home directory")
	}

	sensitiveTargets := []string{
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".aws"),
		filepath.Join(home, ".gnupg"),
		filepath.Join(home, ".docker"),
	}

	denied := SensitiveDirs()

	for _, target := range sensitiveTargets {
		found := false
		for _, d := range denied {
			if d == target {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("filesystem escape: %s is not in the deny list", target)
		}
	}
}

// TestEscape_SandboxMarkerPresent verifies that sandboxed processes can
// detect they are sandboxed via the CLICTL_SANDBOX env var.
func TestEscape_SandboxMarkerPresent(t *testing.T) {
	spec := &models.ToolSpec{Name: "test-tool"}
	policy := &Policy{Spec: spec, Enabled: true}

	env := BuildEnv(policy)

	found := false
	for _, e := range env {
		if e == "CLICTL_SANDBOX=1" {
			found = true
		}
	}
	if !found {
		t.Error("CLICTL_SANDBOX=1 marker should always be present")
	}
}

// TestEscape_PathTraversalInPermissions verifies that ~ expansion works
// correctly and doesn't allow path traversal.
func TestEscape_PathTraversalInPermissions(t *testing.T) {
	home, _ := os.UserHomeDir()
	if home == "" {
		t.Skip("no home directory")
	}

	tests := []struct {
		input string
		want  string
	}{
		{"~/legit/path", filepath.Join(home, "legit/path")},
		{"~/.ssh", filepath.Join(home, ".ssh")}, // still expands, but Landlock won't allow it
		{"/etc/passwd", "/etc/passwd"},
		{"../../etc/shadow", "../../etc/shadow"}, // relative path stays relative
	}

	for _, tt := range tests {
		got := expandHome(tt.input)
		if got != tt.want {
			t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
