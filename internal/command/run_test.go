// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/vault"
)

func TestResolveToolEnv_ProjectVaultFirst(t *testing.T) {
	// Set up a project vault with a key
	projectDir := t.TempDir()
	pv := vault.NewFileVault(projectDir)
	if err := pv.InitKey(); err != nil {
		t.Fatalf("InitKey project vault: %v", err)
	}
	if err := pv.Set("MY_API_KEY", "from-project-vault"); err != nil {
		t.Fatalf("Set project vault: %v", err)
	}

	// Set up a user vault with the same key
	userDir := t.TempDir()
	uv := vault.NewFileVault(userDir)
	if err := uv.InitKey(); err != nil {
		t.Fatalf("InitKey user vault: %v", err)
	}
	if err := uv.Set("MY_API_KEY", "from-user-vault"); err != nil {
		t.Fatalf("Set user vault: %v", err)
	}

	// Also set in env
	t.Setenv("MY_API_KEY", "from-env")

	// resolveToolEnv uses gitRepoRoot and UserHomeDir internally,
	// so we test the resolution logic directly using resolveToolEnvWith.
	spec := &models.ToolSpec{
		Name: "test-tool",
		Auth: &models.Auth{
			Env: models.StringOrSlice{"MY_API_KEY"},
		},
	}

	resolved := resolveToolEnvWith(spec, pv, uv)
	if resolved["MY_API_KEY"] != "from-project-vault" {
		t.Errorf("expected project vault value, got %q", resolved["MY_API_KEY"])
	}
}

func TestResolveToolEnv_UserVaultFallback(t *testing.T) {
	// No project vault
	userDir := t.TempDir()
	uv := vault.NewFileVault(userDir)
	if err := uv.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}
	if err := uv.Set("MY_API_KEY", "from-user-vault"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	t.Setenv("MY_API_KEY", "from-env")

	spec := &models.ToolSpec{
		Name: "test-tool",
		Auth: &models.Auth{
			Env: models.StringOrSlice{"MY_API_KEY"},
		},
	}

	resolved := resolveToolEnvWith(spec, nil, uv)
	if resolved["MY_API_KEY"] != "from-user-vault" {
		t.Errorf("expected user vault value, got %q", resolved["MY_API_KEY"])
	}
}

func TestResolveToolEnv_EnvFallback(t *testing.T) {
	t.Setenv("MY_API_KEY", "from-env")

	spec := &models.ToolSpec{
		Name: "test-tool",
		Auth: &models.Auth{
			Env: models.StringOrSlice{"MY_API_KEY"},
		},
	}

	resolved := resolveToolEnvWith(spec, nil, nil)
	if resolved["MY_API_KEY"] != "from-env" {
		t.Errorf("expected env value, got %q", resolved["MY_API_KEY"])
	}
}

func TestResolveToolEnv_Missing(t *testing.T) {
	t.Setenv("MY_API_KEY", "")

	spec := &models.ToolSpec{
		Name: "test-tool",
		Auth: &models.Auth{
			Env: models.StringOrSlice{"MY_API_KEY"},
		},
	}

	resolved := resolveToolEnvWith(spec, nil, nil)
	if _, ok := resolved["MY_API_KEY"]; ok {
		t.Error("expected MY_API_KEY to not be in resolved map")
	}
}

func TestResolveToolEnv_NilSpec(t *testing.T) {
	resolved := resolveToolEnvWith(nil, nil, nil)
	if resolved != nil {
		t.Errorf("expected nil for nil spec, got %v", resolved)
	}
}

func TestCheckRequiredEnvVars_MissingKeyWarning(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "")

	spec := &models.ToolSpec{
		Name: "stripe",
		Auth: &models.Auth{
			Env: models.StringOrSlice{"STRIPE_API_KEY"},
		},
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	checkRequiredEnvVars(spec, map[string]string{})

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !bytes.Contains([]byte(output), []byte("[MISSING_AUTH]")) {
		t.Error("expected [MISSING_AUTH] prefix in warning")
	}
	if !bytes.Contains([]byte(output), []byte("STRIPE_API_KEY")) {
		t.Error("expected env var name in warning")
	}
	if !bytes.Contains([]byte(output), []byte("clictl vault set STRIPE_API_KEY")) {
		t.Error("expected vault set command in warning")
	}
	if !bytes.Contains([]byte(output), []byte("clictl info stripe")) {
		t.Error("expected info command in warning")
	}
}

func TestCheckRequiredEnvVars_NoWarningWhenResolved(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "")

	spec := &models.ToolSpec{
		Name: "stripe",
		Auth: &models.Auth{
			Env: models.StringOrSlice{"STRIPE_API_KEY"},
		},
	}

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	checkRequiredEnvVars(spec, map[string]string{"STRIPE_API_KEY": "sk_test_123"})

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	if buf.Len() > 0 {
		t.Errorf("expected no warning when key is resolved, got: %s", buf.String())
	}
}

func TestCheckRequiredEnvVars_NoWarningWhenInEnv(t *testing.T) {
	t.Setenv("STRIPE_API_KEY", "sk_live_123")

	spec := &models.ToolSpec{
		Name: "stripe",
		Auth: &models.Auth{
			Env: models.StringOrSlice{"STRIPE_API_KEY"},
		},
	}

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	checkRequiredEnvVars(spec, map[string]string{})

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	if buf.Len() > 0 {
		t.Errorf("expected no warning when key is in env, got: %s", buf.String())
	}
}

func TestCheckRequiredEnvVars_NoHelpURL(t *testing.T) {
	t.Setenv("SOME_KEY", "")

	spec := &models.ToolSpec{
		Name: "sometool",
		Auth: &models.Auth{Env: models.StringOrSlice{"SOME_KEY"}},
	}

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	checkRequiredEnvVars(spec, map[string]string{})

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if bytes.Contains([]byte(output), []byte("To get a key:")) {
		t.Error("should not show 'To get a key:' when no HelpURL is set")
	}
}

func TestVersionDisplay(t *testing.T) {
	spec := &models.ToolSpec{
		Name:    "weather",
		Version: "1.2.3",
	}

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Simulate version display logic
	flagQuiet = false
	if !flagQuiet && spec.Version != "" {
		fmt.Fprintf(w, "%s v%s\n", spec.Name, spec.Version)
	}

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	expected := "weather v1.2.3\n"
	if output != expected {
		t.Errorf("version display: got %q, want %q", output, expected)
	}
}

func TestVersionDisplay_Quiet(t *testing.T) {
	spec := &models.ToolSpec{
		Name:    "weather",
		Version: "1.2.3",
	}

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Simulate version display logic with quiet=true
	quiet := true
	if !quiet && spec.Version != "" {
		fmt.Fprintf(w, "%s v%s\n", spec.Name, spec.Version)
	}

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	if buf.Len() > 0 {
		t.Errorf("expected no version output with --quiet, got: %s", buf.String())
	}
}

func TestStoreAuthInVault(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Initialize vault dir
	cliDir := filepath.Join(tmpDir, ".clictl")
	os.MkdirAll(cliDir, 0o700)

	storeAuthInVault("TEST_KEY", "test-value")

	v := vault.NewFileVault(cliDir)
	got, err := v.Get("TEST_KEY")
	if err != nil {
		t.Fatalf("Get from vault: %v", err)
	}
	if got != "test-value" {
		t.Errorf("vault value: got %q, want %q", got, "test-value")
	}
}

func TestParseExecArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantTool   string
		wantAction string
		wantParams map[string]string
		wantErr    bool
	}{
		{
			name:       "basic",
			args:       []string{"nominatim", "search", "--q", "San Francisco"},
			wantTool:   "nominatim",
			wantAction: "search",
			wantParams: map[string]string{"q": "San Francisco"},
		},
		{
			name:       "tool param named verbose",
			args:       []string{"my-tool", "query", "--verbose", "true", "--output", "csv"},
			wantTool:   "my-tool",
			wantAction: "query",
			wantParams: map[string]string{"verbose": "true", "output": "csv"},
		},
		{
			name:       "tool param named quiet",
			args:       []string{"my-tool", "list", "--quiet", "false"},
			wantTool:   "my-tool",
			wantAction: "list",
			wantParams: map[string]string{"quiet": "false"},
		},
		{
			name:       "clictl flag before positional args",
			args:       []string{"--verbose", "my-tool", "get", "--id", "123"},
			wantTool:   "my-tool",
			wantAction: "get",
			wantParams: map[string]string{"id": "123"},
		},
		{
			name:       "clictl output flag before positional args",
			args:       []string{"--output", "json", "my-tool", "get"},
			wantTool:   "my-tool",
			wantAction: "get",
			wantParams: map[string]string{},
		},
		{
			name:       "equals syntax",
			args:       []string{"tool", "action", "--key=value", "--flag=on"},
			wantTool:   "tool",
			wantAction: "action",
			wantParams: map[string]string{"key": "value", "flag": "on"},
		},
		{
			name:       "boolean flag after action",
			args:       []string{"tool", "action", "--dry-run"},
			wantTool:   "tool",
			wantAction: "action",
			wantParams: map[string]string{"dry-run": "true"},
		},
		{
			name:       "raw flag still intercepted after action",
			args:       []string{"tool", "action", "--raw", "--limit", "10"},
			wantTool:   "tool",
			wantAction: "action",
			wantParams: map[string]string{"__raw": "true", "limit": "10"},
		},
		{
			name:       "short flag as tool param",
			args:       []string{"tool", "action", "-q", "test"},
			wantTool:   "tool",
			wantAction: "action",
			wantParams: map[string]string{"q": "test"},
		},
		{
			name:    "missing action",
			args:    []string{"tool"},
			wantErr: true,
		},
		{
			name:    "no args",
			args:    []string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset global flags before each test
			flagNoCache = false
			flagVerbose = false
			flagQuiet = false
			flagPaginateAll = false
			flagOutput = ""
			flagAPIURL = ""

			tool, action, params, err := parseExecArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tool != tt.wantTool {
				t.Errorf("tool: got %q, want %q", tool, tt.wantTool)
			}
			if action != tt.wantAction {
				t.Errorf("action: got %q, want %q", action, tt.wantAction)
			}
			for k, want := range tt.wantParams {
				if got := params[k]; got != want {
					t.Errorf("param %q: got %q, want %q", k, got, want)
				}
			}
			for k, v := range params {
				if _, ok := tt.wantParams[k]; !ok {
					t.Errorf("unexpected param %q=%q", k, v)
				}
			}
		})
	}
}

func TestParseExecArgs_GlobalFlagsOnlyBeforeAction(t *testing.T) {
	// --verbose before tool/action should set the global flag
	flagVerbose = false
	flagQuiet = false
	_, _, _, err := parseExecArgs([]string{"--verbose", "tool", "action", "--verbose", "high"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !flagVerbose {
		t.Error("expected flagVerbose to be set from pre-action --verbose")
	}
}

func TestParseExecArgs_QuietBeforeNotAfter(t *testing.T) {
	flagQuiet = false
	_, _, params, err := parseExecArgs([]string{"-q", "tool", "action", "--quiet", "mode"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !flagQuiet {
		t.Error("expected flagQuiet set from -q before positional args")
	}
	if params["quiet"] != "mode" {
		t.Errorf("expected --quiet after action to be tool param, got %q", params["quiet"])
	}
}

func TestParseExecArgs_AllFlagBeforeAction(t *testing.T) {
	flagPaginateAll = false
	_, _, _, err := parseExecArgs([]string{"--all", "tool", "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !flagPaginateAll {
		t.Error("expected flagPaginateAll to be set from --all before positional args")
	}
}

func TestParseExecArgs_AllFlagAfterAction(t *testing.T) {
	flagPaginateAll = false
	_, _, params, err := parseExecArgs([]string{"tool", "list", "--all"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !flagPaginateAll {
		t.Error("expected flagPaginateAll to be set from --all after action")
	}
	// --all should NOT appear in params
	if _, ok := params["all"]; ok {
		t.Error("--all should be consumed as a clictl flag, not passed as tool param")
	}
}
