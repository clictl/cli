// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package sandbox provides OS-level process isolation for MCP server subprocesses.
//
// When clictl spawns an MCP server (e.g. via npx or uvx), the subprocess inherits
// the parent's full environment and filesystem access. This package restricts that
// access to protect against supply chain attacks (compromised npm/pypi packages
// stealing credentials).
//
// Isolation is applied in layers:
//  1. Environment scrubbing: only declared env vars are passed (all platforms)
//  2. Filesystem isolation: platform-native sandboxing denies access to sensitive
//     directories like ~/.ssh, ~/.aws, browser profiles (Linux: Landlock, macOS:
//     sandbox-exec, Windows: restricted tokens + Job Objects)
//
// Sandbox is on by default. Users can opt out with --no-sandbox or sandbox: false
// in config. Workspace admins can enforce it via policy.
package sandbox

import (
	"context"
	"fmt"
	osexec "os/exec"
	"os"
	"path/filepath"

	"github.com/clictl/cli/internal/models"
)

// Policy holds the resolved sandbox constraints for a single MCP server spawn.
type Policy struct {
	Spec       *models.ToolSpec
	InstallDir string // resolved directory containing the MCP server binary
	WorkingDir string // working directory for the subprocess
	Enabled    bool   // false when --no-sandbox or sandbox: false
	Strict     bool   // true by default; when true, sandbox setup failure aborts the process
}

// NewPolicy constructs a sandbox Policy from a ToolSpec.
func NewPolicy(spec *models.ToolSpec, enabled bool) *Policy {
	p := &Policy{
		Spec:    spec,
		Enabled: enabled,
	}

	// Resolve install directory from the command binary
	if spec.Server != nil && spec.Server.Command != "" {
		if path, err := osexec.LookPath(spec.Server.Command); err == nil {
			p.InstallDir = filepath.Dir(path)
		}
	}

	// Default working directory
	if wd, err := os.Getwd(); err == nil {
		p.WorkingDir = wd
	}

	return p
}

// ApplyAndStart sets the sandboxed environment on cmd and starts the process
// with platform-specific filesystem isolation.
//
// On Linux, this locks the OS thread to apply Landlock before exec.
// On macOS, this wraps the command with sandbox-exec.
// On Windows, this creates a restricted process token with Job Objects.
//
// If sandbox setup fails, it logs a warning and starts the process unsandboxed.
// The caller should NOT call cmd.Start() separately.
func ApplyAndStart(ctx context.Context, cmd *osexec.Cmd, policy *Policy) error {
	// Always apply env scrubbing when sandbox is enabled
	if policy.Enabled {
		cmd.Env = BuildEnv(policy)
	}

	// Apply platform-specific filesystem isolation and start
	if policy.Enabled {
		err := applyAndStart(ctx, cmd, policy)
		if err == nil {
			return nil
		}
		// Fail-closed by default: abort if sandbox setup fails
		if policy.Strict {
			return fmt.Errorf("sandbox setup failed for %s: %w (use --no-sandbox or strict_sandbox: false to allow degraded mode)", policy.Spec.Name, err)
		}
		fmt.Fprintf(os.Stderr, "clictl: sandbox setup failed for %s: %v (proceeding unsandboxed)\n", policy.Spec.Name, err)
	}

	return cmd.Start()
}
