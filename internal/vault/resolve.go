// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package vault

import (
	"regexp"
	"strings"
)

// validSecretName restricts vault reference names to safe identifiers.
// Names must start with a letter or underscore, followed by letters, digits, or underscores.
var validSecretName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

const vaultPrefix = "vault://"

// ResolveEnv resolves vault:// references in environment variable values.
// For each value starting with "vault://", the prefix is stripped and the
// secret name is looked up in the project vault first, then the user vault,
// then the optional workspace resolver.
// Non-vault values are passed through unchanged. If a vault lookup fails,
// the original vault:// reference is preserved.
func ResolveEnv(env map[string]string, projectVault, userVault *Vault, opts ...ResolveOption) map[string]string {
	var ro resolveOptions
	for _, opt := range opts {
		opt(&ro)
	}

	resolved := make(map[string]string, len(env))

	for key, value := range env {
		if !strings.HasPrefix(value, vaultPrefix) {
			resolved[key] = value
			continue
		}

		secretName := strings.TrimPrefix(value, vaultPrefix)

		// Validate secret name to prevent path traversal and injection
		if !validSecretName.MatchString(secretName) {
			// Invalid name, skip resolution, keep original value
			resolved[key] = value
			continue
		}

		// Try project vault first
		if projectVault != nil && projectVault.HasKey() {
			if val, err := projectVault.Get(secretName); err == nil {
				resolved[key] = val
				continue
			}
		}

		// Fall back to user vault
		if userVault != nil && userVault.HasKey() {
			if val, err := userVault.Get(secretName); err == nil {
				resolved[key] = val
				continue
			}
		}

		// Fall back to workspace resolver
		if ro.workspaceResolver != nil {
			if val := ro.workspaceResolver.Resolve(secretName); val != "" {
				resolved[key] = val
				continue
			}
		}

		// Keep original reference if lookup fails
		resolved[key] = value
	}

	return resolved
}

// resolveOptions holds optional parameters for ResolveEnv.
type resolveOptions struct {
	workspaceResolver *WorkspaceVaultResolver
}

// ResolveOption configures optional behavior for ResolveEnv.
type ResolveOption func(*resolveOptions)

// WithWorkspaceResolver sets a workspace vault resolver for fallback resolution.
func WithWorkspaceResolver(resolver *WorkspaceVaultResolver) ResolveOption {
	return func(o *resolveOptions) {
		o.workspaceResolver = resolver
	}
}

// IsVaultRef returns true if the value is a vault:// reference.
func IsVaultRef(value string) bool {
	return strings.HasPrefix(value, vaultPrefix)
}

// VaultRefName extracts the secret name from a vault:// reference.
// Returns empty string if the value is not a vault reference.
func VaultRefName(value string) string {
	if !IsVaultRef(value) {
		return ""
	}
	return strings.TrimPrefix(value, vaultPrefix)
}
