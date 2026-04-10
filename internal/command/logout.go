// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear stored authentication credentials",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		cfg.Auth = config.AuthConfig{}

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		fmt.Println("Logged out. Credentials cleared from ~/.clictl/config.yaml")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}
