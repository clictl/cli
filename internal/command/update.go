// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update toolboxes (alias for 'toolbox update')",
	Long:  "Syncs all configured toolboxes. Equivalent to running 'clictl toolbox update'.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return toolboxUpdateCmd.RunE(cmd, args)
	},
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
