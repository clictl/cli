// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"

	"github.com/clictl/cli/internal/updater"
	"github.com/spf13/cobra"
)

var selfUpdateSkipVerify bool

var selfUpdateCmd = &cobra.Command{
	Use:   "self-update",
	Short: "Update clictl to the latest version",
	Long: `Check for a new version of clictl on GitHub releases and update the binary.

  clictl self-update
  clictl self-update --skip-verify  # Skip checksum verification (air-gapped environments)`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintf(cmd.OutOrStdout(), "Current version: %s\n", Version)
		fmt.Fprintf(cmd.OutOrStdout(), "Checking for updates...\n")

		if err := updater.SelfUpdate(selfUpdateSkipVerify); err != nil {
			return fmt.Errorf("updating: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Updated successfully.\n")
		return nil
	},
}

func init() {
	selfUpdateCmd.Flags().BoolVar(&selfUpdateSkipVerify, "skip-verify", false, "Skip checksum verification (for air-gapped environments)")
	rootCmd.AddCommand(selfUpdateCmd)
}
