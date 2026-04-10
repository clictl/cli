// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package sandbox

import (
	"time"
)

// NetworkMode controls how network access is handled inside the sandbox.
type NetworkMode string

const (
	// NetworkNone disables all network access.
	NetworkNone NetworkMode = "none"
	// NetworkHost grants full host network access.
	NetworkHost NetworkMode = "host"
	// NetworkAllowlist permits traffic only to declared hosts.
	NetworkAllowlist NetworkMode = "allowlist"
)

// Mount describes a filesystem mount inside the sandbox.
type Mount struct {
	// Source is the host path to mount.
	Source string
	// Target is the path inside the sandbox where Source is mounted.
	Target string
	// ReadOnly indicates the mount should be read-only.
	ReadOnly bool
}

// Config holds all sandbox parameters for a single tool execution.
type Config struct {
	// ToolName identifies the tool being sandboxed (for logging).
	ToolName string

	// RootfsPath is the path to the minimal rootfs for pivot_root (Linux only).
	// If empty, the sandbox runs on the host filesystem with deny rules.
	RootfsPath string

	// Mounts are additional filesystem mounts inside the sandbox.
	Mounts []Mount

	// NetworkMode controls network access.
	NetworkMode NetworkMode
	// AllowedHosts is the list of permitted hosts when NetworkMode is allowlist.
	AllowedHosts []string

	// MemoryLimitMB is the cgroup memory limit in megabytes (0 = unlimited).
	MemoryLimitMB int
	// CPUQuotaPercent is the cgroup CPU quota as a percentage (0 = unlimited, 100 = 1 core).
	CPUQuotaPercent int

	// Timeout is the maximum execution time. Zero means no timeout.
	Timeout time.Duration

	// SeccompProfile selects the seccomp-bpf profile.
	// Options: "minimal", "standard", "permissive".
	SeccompProfile string

	// Env is the explicit environment variable list for the sandboxed process.
	// If nil, the parent environment is used (with scrubbing).
	Env []string

	// WorkingDir is the working directory inside the sandbox.
	WorkingDir string

	// Credentials controls which credentials are forwarded.
	Credentials CredentialConfig
}

// CredentialConfig controls which host credentials are forwarded into the sandbox.
type CredentialConfig struct {
	// SSHAgent forwards the SSH_AUTH_SOCK into the sandbox.
	SSHAgent bool
	// GitConfig mounts ~/.gitconfig as read-only.
	GitConfig bool
	// NPMConfig mounts ~/.npmrc as read-only.
	NPMConfig bool
	// DenyAll overrides all credential forwarding to deny everything.
	DenyAll bool
	// DenyList is a list of specific credentials to deny (e.g., "ssh", "git", "npm").
	DenyList []string
}

// IsCredentialDenied checks if a specific credential type is denied.
func (cc *CredentialConfig) IsCredentialDenied(cred string) bool {
	if cc.DenyAll {
		return true
	}
	for _, d := range cc.DenyList {
		if d == cred {
			return true
		}
	}
	return false
}

// Result holds the outcome of a sandboxed execution.
type Result struct {
	// ExitCode is the process exit code.
	ExitCode int
	// Stdout is the captured standard output.
	Stdout []byte
	// Stderr is the captured standard error.
	Stderr []byte
	// Duration is how long the process ran.
	Duration time.Duration
	// Violations is a list of sandbox policy violations detected during execution.
	Violations []Violation
	// TimedOut indicates the process was killed due to timeout.
	TimedOut bool
}

// Violation represents a detected sandbox policy violation.
type Violation struct {
	// Type is the violation category (e.g., "network", "filesystem", "syscall").
	Type string
	// Description is a human-readable description of the violation.
	Description string
	// Timestamp is when the violation was detected.
	Timestamp time.Time
	// ToolName identifies which tool triggered the violation.
	ToolName string
}

// Backend is the interface that all sandbox implementations must satisfy.
// Each platform (Linux namespaces, macOS sandbox-exec, Docker, unsandboxed)
// implements this interface.
type Backend interface {
	// Name returns the backend identifier (e.g., "linux-ns", "macos-sandbox", "docker", "unsandboxed").
	Name() string

	// Available reports whether this backend can run on the current system.
	Available() bool

	// Run executes a command inside the sandbox with the given configuration.
	// It blocks until the command completes or the context is cancelled.
	Run(cfg *Config, command string, args []string) (*Result, error)
}
