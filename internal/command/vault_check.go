// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
	"github.com/clictl/cli/internal/vault"
)

var vaultCheckCmd = &cobra.Command{
	Use:   "check <tool>",
	Short: "Show required secrets and their resolution status for a tool",
	Long: `Check which secrets a tool requires and whether they are available
in the vault, environment, or .env files.

  clictl vault check stripe
  clictl vault check github`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		toolName := args[0]
		ctx := cmd.Context()

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		cache := registry.NewCache(cfg.CacheDir)
		spec, err := registry.ResolveSpec(ctx, toolName, cfg, cache, flagNoCache)
		if err != nil {
			return fmt.Errorf("tool %q not found: %w", toolName, err)
		}

		if spec.Auth == nil || len(spec.Auth.Env) == 0 {
			fmt.Printf("%s has no secret requirements.\n", toolName)
			return nil
		}

		// Resolve vaults
		var projectVault *vault.Vault
		if root, gitErr := gitRepoRoot(); gitErr == nil {
			projectVault = vault.NewProjectVault(root)
		}
		userVault := vault.NewVault(config.BaseDir())

		fmt.Printf("Secret requirements for %s:\n\n", toolName)

		allResolved := true
		for _, envVar := range spec.Auth.Env {
			if envVar == "" {
				continue
			}

			status, source := resolveSecretStatus(envVar, projectVault, userVault)
			icon := "\033[32m[OK]\033[0m"
			if !status {
				icon = "\033[31m[MISSING]\033[0m"
				allResolved = false
			}

			fmt.Printf("  %s %s", icon, envVar)
			if source != "" {
				fmt.Printf(" (from %s)", source)
			}
			fmt.Println()
		}

		fmt.Println()
		if allResolved {
			fmt.Printf("All secrets are configured. %s is ready to use.\n", toolName)
		} else {
			fmt.Printf("Some secrets are missing. Run 'clictl vault setup %s' to configure them.\n", toolName)
		}

		return nil
	},
}

var vaultSetupCmd = &cobra.Command{
	Use:   "setup <tool>",
	Short: "Interactively set all missing secrets for a tool",
	Long: `Prompt for each missing secret required by a tool and store them
in the vault.

  clictl vault setup stripe
  clictl vault setup github`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		toolName := args[0]
		ctx := cmd.Context()

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		cache := registry.NewCache(cfg.CacheDir)
		spec, err := registry.ResolveSpec(ctx, toolName, cfg, cache, flagNoCache)
		if err != nil {
			return fmt.Errorf("tool %q not found: %w", toolName, err)
		}

		if spec.Auth == nil || len(spec.Auth.Env) == 0 {
			fmt.Printf("%s has no secret requirements.\n", toolName)
			return nil
		}

		// Resolve vaults
		var projectVault *vault.Vault
		if root, gitErr := gitRepoRoot(); gitErr == nil {
			projectVault = vault.NewProjectVault(root)
		}
		userVault := vault.NewVault(config.BaseDir())

		// Ensure vault key exists
		if !userVault.HasKey() {
			if err := userVault.InitKey(); err != nil {
				return fmt.Errorf("initializing vault key: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Vault initialized at %s\n", userVault.KeyPath())
		}

		reader := bufio.NewReader(os.Stdin)
		configured := 0
		skipped := 0

		fmt.Printf("Setting up secrets for %s:\n\n", toolName)

		for _, envVar := range spec.Auth.Env {
			if envVar == "" {
				continue
			}

			resolved, source := resolveSecretStatus(envVar, projectVault, userVault)
			if resolved {
				fmt.Printf("  [OK] %s (already set from %s)\n", envVar, source)
				skipped++
				continue
			}

			fmt.Printf("  Enter value for %s (or press Enter to skip): ", envVar)
			value, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading input: %w", err)
			}
			value = strings.TrimSpace(value)

			if value == "" {
				fmt.Printf("  Skipped %s\n", envVar)
				skipped++
				continue
			}

			// Store in the appropriate vault
			targetVault := userVault
			if vaultProject && projectVault != nil {
				targetVault = projectVault
			}

			if err := targetVault.Set(envVar, value); err != nil {
				fmt.Fprintf(os.Stderr, "  Error storing %s: %v\n", envVar, err)
				continue
			}

			fmt.Printf("  Stored %s in vault\n", envVar)
			updateDotEnv(envVar)
			configured++
		}

		fmt.Printf("\nConfigured %d secrets, skipped %d.\n", configured, skipped)
		if configured > 0 {
			fmt.Printf("%s is ready to use.\n", toolName)
		}

		return nil
	},
}

// resolveSecretStatus checks if a secret is available from any source.
// Returns (found, source) where source is one of "project vault", "user vault", "environment".
func resolveSecretStatus(key string, projectVault, userVault *vault.Vault) (bool, string) {
	// Check project vault
	if projectVault != nil && projectVault.HasKey() {
		if v, err := projectVault.Get(key); err == nil && v != "" {
			return true, "project vault"
		}
	}

	// Check user vault
	if userVault != nil && userVault.HasKey() {
		if v, err := userVault.Get(key); err == nil && v != "" {
			return true, "user vault"
		}
	}

	// Check environment (includes .env loaded vars)
	if v := os.Getenv(key); v != "" {
		return true, "environment"
	}

	return false, ""
}

func init() {
	vaultSetupCmd.Flags().BoolVar(&vaultProject, "project", false, "Store secrets in project vault")
	vaultCmd.AddCommand(vaultCheckCmd)
	vaultCmd.AddCommand(vaultSetupCmd)
}
