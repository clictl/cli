// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the current authenticated user",
	Long: `Display the currently authenticated user and API URL.

Validates credentials by calling the API, so it also serves as a connectivity check.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

		token := config.ResolveAuthToken("", cfg)
		if token == "" {
			return fmt.Errorf("not logged in. Run 'clictl login' to authenticate")
		}

		client := registry.NewClient(cfg.APIURL, nil, true)
		client.AuthToken = token

		user, err := client.GetCurrentUser(cmd.Context())
		if err != nil {
			return fmt.Errorf("not logged in. Run 'clictl login' to authenticate")
		}

		name := user.Username
		if user.FullName != "" {
			name = user.FullName
		}
		fmt.Printf("User:      %s (%s)\n", name, user.Email)
		if cfg.Auth.ActiveWorkspace != "" {
			fmt.Printf("Workspace: %s\n", cfg.Auth.ActiveWorkspace)
		} else {
			fmt.Println("Workspace: (none)")
		}
		fmt.Printf("API URL:   %s\n", cfg.APIURL)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}
