// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/enterprise"
	"github.com/clictl/cli/internal/executor"
	"github.com/clictl/cli/internal/logger"
	"github.com/clictl/cli/internal/telemetry"
	"github.com/clictl/cli/internal/updater"
)

// bannerText returns the startup banner.
func bannerText() string {
	if edition != "" {
		return "clictl (" + edition + ")"
	}
	return "clictl"
}

var (
	flagOutput  string
	flagAPIURL  string
	flagHome    string
	flagNoCache bool
	flagAPIKey  string
	flagVerbose bool
)

var rootCmd = &cobra.Command{
	Use:   "clictl",
	Short: "CLI tool manager - discover, inspect, and execute API tools",
	Long:  "\n" + "  🧰 clictl" + "\n\n  Discover, inspect, and execute API tools from clictl.dev.\n  Install tools as Claude skills or MCP servers for any AI provider.",
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Set config home directory override (must be before any config access)
		if flagHome != "" {
			config.SetHome(flagHome)
		}

		// Wire CLI version into the executor for User-Agent headers
		executor.SetVersion(Version)

		// Load .env files (cwd, project root, ~/.clictl/.env)
		config.LoadDotEnv()

		// Initialize logger from config (--verbose overrides)
		cfg, err := config.Load()
		if err == nil {
			logEnabled := cfg.Log.Enabled || flagVerbose
			logLevel := cfg.Log.Level
			if flagVerbose {
				logLevel = "debug"
			}
			logger.Init(logEnabled, logLevel, cfg.Log.Format, cfg.Log.File)
			logger.Debug("config loaded", logger.F("api_url", cfg.APIURL), logger.F("toolboxes", len(cfg.Toolboxes)))

			// Auto-refresh expired OAuth tokens
			if config.RefreshAuth(cmd.Context(), cfg) {
				logger.Debug("access token refreshed")
			}

			// Initialize telemetry (reads opt-out preference from config)
			telemetry.Init(cfg, Version)
		}

		// Enterprise: check pinned CLI version
		ep := enterprise.GetProvider()
		if pinned := ep.PinnedCLIVersion(); pinned != "" && Version != "" && pinned != Version {
			return fmt.Errorf("workspace requires CLI version %s (running %s). Run: clictl update", pinned, Version)
		}

		// Enterprise: require authentication
		if ep.RequireAuth() {
			if cfg == nil || config.ResolveAuthToken("", cfg) == "" {
				return fmt.Errorf("workspace requires authentication. Run: clictl login")
			}
		}

		// Enterprise/strict: refuse git-tracked .env files
		if ep.SandboxRequired() {
			if err := config.EnforceDotEnvSafety(); err != nil {
				return err
			}
		}

		// Skip auto-checks for update/version/completion commands
		name := cmd.Name()
		if name == "update" || name == "version" || name == "completion" || name == "help" || name == "mcp-serve" {
			return nil
		}

		// First-run welcome message
		if cfg != nil && !cfg.FirstRunDone {
			showFirstRunWelcome()
			cfg.FirstRunDone = true
			_ = config.Save(cfg)
		}

		// Run background checks for index sync and version updates.
		// Only sync on commands that actually use the toolbox index.
		if cfg != nil {
			cmdName := ""
			if cmd.HasParent() {
				cmdName = cmd.Name()
			}
			registryCommands := map[string]bool{
				"search": true, "list": true, "info": true, "inspect": true,
				"run": true, "exec": true, "install": true, "uninstall": true,
				"upgrade": true, "outdated": true, "explain": true,
				"categories": true, "tags": true, "audit": true, "verify": true,
				"test": true, "mcp-serve": true,
			}
			if registryCommands[cmdName] {
				updater.AutoCheck(cfg)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "text", "Output format (text, json, yaml)")
	rootCmd.PersistentFlags().StringVar(&flagAPIURL, "api-url", "", "Override the API URL")
	rootCmd.PersistentFlags().StringVar(&flagHome, "home", "", "Override the config directory (default: ~/.clictl, env: CLICTL_HOME)")
	rootCmd.PersistentFlags().BoolVar(&flagNoCache, "no-cache", false, "Bypass the local spec cache")
	rootCmd.PersistentFlags().StringVar(&flagAPIKey, "api-key", "", "API key for authentication")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "Enable verbose output (debug logging to stderr)")

}

// showFirstRunWelcome prints a one-time welcome message for new users.
func showFirstRunWelcome() {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Welcome to clictl - the package manager for your Agent.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Quick start:")
	fmt.Fprintln(os.Stderr, "  clictl install               # install the clictl skill for your AI tool")
	fmt.Fprintln(os.Stderr, "  clictl search weather        # find tools")
	fmt.Fprintln(os.Stderr, "  clictl install github        # install a specific tool")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Run 'clictl doctor' to check your setup.")
	fmt.Fprintln(os.Stderr, "Run 'clictl help' for all commands.")
	fmt.Fprintln(os.Stderr, "")
}

// Execute runs the root command.
func Execute() {
	rootCmd.SilenceErrors = true
	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		defaultHelp(cmd, args)
		fmt.Fprintln(cmd.OutOrStdout(), "\nDocumentation: https://clictl.dev/docs")
	})
	if err := rootCmd.Execute(); err != nil {
		telemetry.Flush()
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	telemetry.Flush()
}
