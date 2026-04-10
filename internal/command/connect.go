// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
	"github.com/spf13/cobra"
)

var connectCmd = &cobra.Command{
	Use:   "connect <tool>",
	Short: "Connect to a tool that requires OAuth authorization",
	Long: `Initiate an OAuth connection to a tool that requires user authorization.
Opens your browser to authorize access. The connection is stored on the clictl
platform and used automatically when you exec the tool.

Login is required for this command.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		toolName := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token == "" {
			return fmt.Errorf("login required. Run: clictl login")
		}

		// Resolve the tool spec to verify it exists and uses OAuth
		cache := registry.NewCache(cfg.CacheDir)
		spec, err := registry.ResolveSpec(cmd.Context(), toolName, cfg, cache, flagNoCache)
		if err != nil {
			msg := fmt.Sprintf("tool %q not found", toolName)
			if dym := toolSuggestion(toolName, cfg); dym != "" {
				msg += dym
			}
			return fmt.Errorf("%s", msg)
		}

		// OAuth is not supported; guide users to env-based auth instead
		if spec.Auth == nil || len(spec.Auth.Env) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "%s does not require authentication.\n", toolName)
			return nil
		}
		authKeyEnv := spec.Auth.Env[0]
		fmt.Fprintf(cmd.OutOrStdout(), "%s uses environment variable authentication.\n", toolName)
		fmt.Fprintf(cmd.OutOrStdout(), "Set the required environment variable and use 'clictl run' directly.\n")
		fmt.Fprintf(cmd.OutOrStdout(), "  export %s=your-key\n", authKeyEnv)
		return nil
	},
}

// Connect command disabled - OAuth tool connections not yet implemented.
// Will be re-enabled as an enterprise feature.
// func init() {
// 	rootCmd.AddCommand(connectCmd)
// }
