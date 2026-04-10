// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
//go:build !enterprise

package enterprise

import ent "github.com/clictl/cli/pkg/enterprise"

func init() {
	ent.Provider = func() ent.EnterpriseProvider {
		return &stubProvider{}
	}
}

// stubProvider is the no-op implementation used in standard (non-enterprise) builds.
type stubProvider struct{}

func (s *stubProvider) RequireAuth() bool                          { return false }
func (s *stubProvider) LockedWorkspace() string                    { return "" }
func (s *stubProvider) IsConfigLocked() bool                       { return false }
func (s *stubProvider) CheckPermission(tool, action string) (bool, error) { return true, nil }
func (s *stubProvider) SandboxRequired() bool                      { return false }
func (s *stubProvider) PinnedCLIVersion() string                   { return "" }
func (s *stubProvider) RequireLockFile() bool                      { return false }
func (s *stubProvider) BlockUnverifiedTools() bool                 { return false }
func (s *stubProvider) AllowedRegistries() []string                { return nil }
func (s *stubProvider) MaxSessionDuration() int                    { return 0 }
func (s *stubProvider) ToolGroups() []ent.ToolGroupPolicy          { return nil }
func (s *stubProvider) AuditLog(_ string, _ map[string]string)     {}
func (s *stubProvider) AuditLogEnabled() bool                      { return false }
func (s *stubProvider) VerifySpecSignature(_ string, _ []byte, _ string) error { return nil }
func (s *stubProvider) RequireSignedSpecs() bool                   { return false }
func (s *stubProvider) BlockVulnerableTools() bool                 { return false }
