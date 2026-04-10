// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"

	"github.com/clictl/cli/internal/config"
	"github.com/spf13/cobra"
)

var toolCmd = &cobra.Command{
	Use:   "tool",
	Short: "Manage tool settings (pin, unpin, disable, enable)",
}

var toolPinCmd = &cobra.Command{
	Use:   "pin <tool>",
	Short: "Pin a tool version to prevent upgrades",
	Long: `Pin a tool to its current version so it is not upgraded by 'clictl upgrade'.

  clictl tool pin openweathermap`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		toolName := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		if cfg.IsToolPinned(toolName) {
			fmt.Fprintf(cmd.OutOrStdout(), "%s is already pinned.\n", toolName)
			return nil
		}

		cfg.PinTool(toolName)
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Pinned %s. It will not be upgraded by 'clictl upgrade'.\n", toolName)
		return nil
	},
}

var toolUnpinCmd = &cobra.Command{
	Use:   "unpin <tool>",
	Short: "Unpin a tool version to allow upgrades",
	Long: `Remove the version pin from a tool so it can be upgraded.

  clictl tool unpin openweathermap`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		toolName := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		if !cfg.IsToolPinned(toolName) {
			fmt.Fprintf(cmd.OutOrStdout(), "%s is not pinned.\n", toolName)
			return nil
		}

		cfg.UnpinTool(toolName)
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Unpinned %s. It can now be upgraded.\n", toolName)
		return nil
	},
}

var toolDisableCmd = &cobra.Command{
	Use:   "disable <tool>",
	Short: "Disable a tool so it cannot be executed",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		toolName := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		if cfg.IsToolDisabled(toolName) {
			fmt.Fprintf(cmd.OutOrStdout(), "Tool %q is already disabled.\n", toolName)
			return nil
		}

		cfg.DisableTool(toolName)

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Disabled tool %q. Run 'clictl tool enable %s' to re-enable it.\n", toolName, toolName)
		return nil
	},
}

var toolEnableCmd = &cobra.Command{
	Use:   "enable <tool>",
	Short: "Re-enable a previously disabled tool",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		toolName := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		if !cfg.IsToolDisabled(toolName) {
			fmt.Fprintf(cmd.OutOrStdout(), "Tool %q is not disabled.\n", toolName)
			return nil
		}

		cfg.EnableTool(toolName)

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Enabled tool %q.\n", toolName)
		return nil
	},
}

var toolInfoCmd = &cobra.Command{
	Use:   "info <tool>",
	Short: "Show detailed information about a tool (alias for 'clictl info')",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return inspectCmd.RunE(cmd, args)
	},
}

func init() {
	toolCmd.AddCommand(toolPinCmd)
	toolCmd.AddCommand(toolUnpinCmd)
	toolCmd.AddCommand(toolDisableCmd)
	toolCmd.AddCommand(toolEnableCmd)
	toolCmd.AddCommand(toolInfoCmd)
	rootCmd.AddCommand(toolCmd)
}
