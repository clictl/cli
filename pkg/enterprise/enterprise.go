// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package enterprise defines the public interface for enterprise CLI features.
// This package is importable by external modules (e.g., cli-enterprise).
// The internal/enterprise package wraps this and provides the Provider variable.
package enterprise

// EnterpriseProvider defines the interface for enterprise-only CLI features
// such as workspace locking, mandatory authentication, and permission checks.
type EnterpriseProvider interface {
	// RequireAuth returns true if the CLI must authenticate before any operation.
	RequireAuth() bool

	// LockedWorkspace returns the workspace slug that this CLI installation is
	// locked to. Returns "" if the workspace is not locked.
	LockedWorkspace() string

	// IsConfigLocked returns true if the CLI configuration is managed centrally
	// and should not be modified by the user.
	IsConfigLocked() bool

	// CheckPermission checks whether the current user has permission to use
	// the given tool and action. Returns (allowed, error).
	CheckPermission(tool, action string) (bool, error)

	// SandboxRequired returns true if the workspace policy enforces sandbox
	// for all MCP server subprocesses. When true, --no-sandbox is rejected.
	SandboxRequired() bool

	// PinnedCLIVersion returns the required CLI version, or "" if any version is allowed.
	PinnedCLIVersion() string

	// RequireLockFile returns true if clictl lock must exist before tool execution.
	RequireLockFile() bool

	// BlockUnverifiedTools returns true if tools from unverified publishers are blocked.
	BlockUnverifiedTools() bool

	// AllowedRegistries returns the list of allowed registry names, or nil for no restriction.
	AllowedRegistries() []string

	// MaxSessionDuration returns the max session duration in hours, or 0 for no limit.
	MaxSessionDuration() int

	// ToolGroups returns the list of tool groups from the workspace policy.
	ToolGroups() []ToolGroupPolicy

	// AuditLog records a CLI operation for enterprise audit trail.
	// Events: install, uninstall, upgrade, run, login, logout
	AuditLog(event string, details map[string]string)

	// AuditLogEnabled returns true if audit logging is active.
	AuditLogEnabled() bool

	// VerifySpecSignature verifies the cryptographic signature of a spec.
	// Returns nil if valid or not required, error if invalid.
	VerifySpecSignature(specName string, specData []byte, signature string) error

	// RequireSignedSpecs returns true if only signed specs are allowed.
	RequireSignedSpecs() bool

	// BlockVulnerableTools returns true if tools with known security advisories are blocked.
	BlockVulnerableTools() bool
}

// ToolGroupPolicy represents a tool group from the enterprise manifest.
type ToolGroupPolicy struct {
	Name        string   `json:"name"`
	Tools       []string `json:"tools"`
	AutoInstall bool     `json:"auto_install"`
	Locked      bool     `json:"locked"`
}

// Provider is a factory function that returns the active EnterpriseProvider.
// In the standard build (no enterprise tag), this returns a no-op stub.
// Enterprise builds override this at init time.
var Provider func() EnterpriseProvider
