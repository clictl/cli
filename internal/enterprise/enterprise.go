// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package enterprise re-exports the public enterprise interface for internal use.
// The actual interface definition lives in pkg/enterprise so external modules
// (like cli-enterprise) can import it.
package enterprise

import ent "github.com/clictl/cli/pkg/enterprise"

// EnterpriseProvider is the interface for enterprise CLI features.
type EnterpriseProvider = ent.EnterpriseProvider

// Provider returns the active EnterpriseProvider.
// This delegates to the public package's Provider variable.
func GetProvider() EnterpriseProvider {
	if ent.Provider == nil {
		return &fallbackStub{}
	}
	return ent.Provider()
}

// fallbackStub is used if Provider is nil (should not happen in normal builds).
type fallbackStub struct{}

func (f *fallbackStub) RequireAuth() bool                          { return false }
func (f *fallbackStub) LockedWorkspace() string                    { return "" }
func (f *fallbackStub) IsConfigLocked() bool                       { return false }
func (f *fallbackStub) CheckPermission(tool, action string) (bool, error) { return true, nil }
func (f *fallbackStub) SandboxRequired() bool                      { return false }
func (f *fallbackStub) PinnedCLIVersion() string                   { return "" }
func (f *fallbackStub) RequireLockFile() bool                      { return false }
func (f *fallbackStub) BlockUnverifiedTools() bool                 { return false }
func (f *fallbackStub) AllowedRegistries() []string                { return nil }
func (f *fallbackStub) MaxSessionDuration() int                    { return 0 }
func (f *fallbackStub) ToolGroups() []ent.ToolGroupPolicy          { return nil }
func (f *fallbackStub) AuditLog(_ string, _ map[string]string)     {}
func (f *fallbackStub) AuditLogEnabled() bool                      { return false }
func (f *fallbackStub) VerifySpecSignature(_ string, _ []byte, _ string) error { return nil }
func (f *fallbackStub) RequireSignedSpecs() bool                   { return false }
func (f *fallbackStub) BlockVulnerableTools() bool                 { return false }
