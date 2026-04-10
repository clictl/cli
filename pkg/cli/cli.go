// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package cli provides the public API for extending clictl.
// Used by the enterprise CLI wrapper to set edition and execute the root command.
package cli

import (
	"github.com/clictl/cli/internal/command"
)

// SetEdition sets the CLI edition label (e.g., "Enterprise").
// Displayed in version output and banner.
func SetEdition(edition string) {
	command.SetEdition(edition)
}

// Execute runs the root CLI command.
func Execute() {
	command.Execute()
}

// SetVersion sets the CLI version string.
func SetVersion(version string) {
	command.Version = version
}
