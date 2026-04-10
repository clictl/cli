// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/vault"
)

var (
	vaultProject   bool
	vaultWorkspace bool
	vaultForce     bool
	vaultPassword  bool
	vaultFormat    string
	vaultConfirm   bool
	vaultExclude   string
)

var vaultCmd = &cobra.Command{
	Use:   "vault",
	Short: "Manage encrypted secrets",
	Long: `Store, retrieve, and manage encrypted secrets locally.

Secrets are encrypted with AES-256-GCM and stored in ~/.clictl/vault.enc.
Use vault:// references in .env files to avoid exposing plaintext secrets.

  # Store a secret
  clictl vault set STRIPE_KEY sk_live_abc123

  # Store in project vault
  clictl vault set STRIPE_KEY sk_live_abc123 --project

  # List secrets (names only)
  clictl vault list

  # Retrieve a secret
  clictl vault get STRIPE_KEY

  # Import from .env file
  clictl vault import .env`,
}

var vaultSetCmd = &cobra.Command{
	Use:   "set <name> <value>",
	Short: "Store a secret in the vault",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, value := args[0], args[1]

		// Workspace mode: POST to workspace secrets API
		if vaultWorkspace {
			return workspaceVaultSet(name, value)
		}

		v, projectDir, err := resolveVault(vaultProject)
		if err != nil {
			return err
		}

		if !v.HasKey() {
			if err := v.InitKey(); err != nil {
				return fmt.Errorf("initializing vault key: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Vault initialized at %s\n", v.KeyPath())
		}

		if err := v.Set(name, value); err != nil {
			return fmt.Errorf("storing secret: %w", err)
		}

		if vaultProject {
			vault.EnsureGitignore(projectDir)
		}

		fmt.Printf("Stored %s in vault\n", name)

		// Auto-update .env if it exists in cwd
		updateDotEnv(name)

		return nil
	},
}

var vaultGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Retrieve a secret from the vault",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		// Try project vault first, then user vault
		projectVault := projectVaultIfExists()
		if projectVault != nil {
			if val, err := projectVault.Get(name); err == nil {
				fmt.Println(val)
				return nil
			}
		}

		userVault, err := userVaultInstance()
		if err != nil {
			return err
		}
		val, err := userVault.Get(name)
		if err != nil {
			return fmt.Errorf("retrieving secret: %w", err)
		}
		fmt.Println(val)
		return nil
	},
}

var vaultListCmd = &cobra.Command{
	Use:   "list",
	Short: "List stored secrets (names only, never values)",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Workspace mode: GET from workspace secrets API
		if vaultWorkspace {
			return workspaceVaultList()
		}

		type entry struct {
			Name   string
			SetAt  time.Time
			Source string
		}
		var entries []entry

		// Collect from project vault
		projectVault := projectVaultIfExists()
		if projectVault != nil {
			metas, err := projectVault.List()
			if err == nil {
				for _, m := range metas {
					entries = append(entries, entry{Name: m.Name, SetAt: m.SetAt, Source: "project"})
				}
			}
		}

		// Collect from user vault
		userVault, err := userVaultInstance()
		if err == nil {
			metas, err := userVault.List()
			if err == nil {
				for _, m := range metas {
					// Skip if already in project vault
					found := false
					for _, e := range entries {
						if e.Name == m.Name {
							found = true
							break
						}
					}
					if !found {
						entries = append(entries, entry{Name: m.Name, SetAt: m.SetAt, Source: "user"})
					}
				}
			}
		}

		if len(entries) == 0 {
			fmt.Println("No secrets stored in vault.")
			return nil
		}

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name < entries[j].Name
		})

		for _, e := range entries {
			age := formatAge(e.SetAt)
			fmt.Printf("  %-30s (%s, %s)\n", e.Name, age, e.Source)
		}
		return nil
	},
}

var vaultDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Remove a secret from the vault",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		// Workspace mode: DELETE from workspace secrets API
		if vaultWorkspace {
			return workspaceVaultDelete(name)
		}

		v, _, err := resolveVault(vaultProject)
		if err != nil {
			return err
		}

		if err := v.Delete(name); err != nil {
			return fmt.Errorf("deleting secret: %w", err)
		}

		fmt.Printf("Deleted %s from vault\n", name)
		return nil
	},
}

var vaultExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export secrets as plaintext (requires --confirm)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !vaultConfirm {
			return fmt.Errorf("export requires --confirm flag to acknowledge plaintext output")
		}

		v, _, err := resolveVault(vaultProject)
		if err != nil {
			return err
		}

		data, err := v.List()
		if err != nil {
			return fmt.Errorf("listing secrets: %w", err)
		}

		for _, m := range data {
			val, err := v.Get(m.Name)
			if err != nil {
				continue
			}
			switch vaultFormat {
			case "env":
				fmt.Printf("%s=%s\n", m.Name, val)
			default:
				fmt.Printf("%s=%s\n", m.Name, val)
			}
		}
		return nil
	},
}

var vaultImportCmd = &cobra.Command{
	Use:   "import <file>",
	Short: "Import secrets from a .env file into the vault",
	Long: `Read a .env file, store each value in the vault, and replace
the plaintext values with vault:// references in the original file.

  clictl vault import .env
  clictl vault import .env --exclude KEY1,KEY2`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		envPath := args[0]
		return importDotEnv(envPath, vaultProject, vaultExclude)
	},
}

var vaultInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize vault key",
	Long: `Generate a new vault encryption key. If a key already exists,
use --force to regenerate (this will wipe the existing vault).
Use --password to derive the key from a password.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		v, projectDir, err := resolveVault(vaultProject)
		if err != nil {
			return err
		}

		if vaultPassword {
			fmt.Fprint(os.Stderr, "Enter vault password: ")
			reader := bufio.NewReader(os.Stdin)
			password, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading password: %w", err)
			}
			password = strings.TrimSpace(password)
			if password == "" {
				return fmt.Errorf("password cannot be empty")
			}
			if err := v.InitKeyFromPassword(password); err != nil {
				return fmt.Errorf("initializing vault from password: %w", err)
			}
			fmt.Printf("Vault initialized with password-derived key at %s\n", v.KeyPath())
		} else if vaultForce {
			if err := v.InitKeyForce(); err != nil {
				return fmt.Errorf("reinitializing vault: %w", err)
			}
			fmt.Printf("Vault reinitialized at %s\n", v.KeyPath())
		} else {
			if err := v.InitKey(); err != nil {
				return fmt.Errorf("initializing vault: %w", err)
			}
			fmt.Printf("Vault initialized at %s\n", v.KeyPath())
		}

		if vaultProject {
			vault.EnsureGitignore(projectDir)
		}

		return nil
	},
}

func init() {
	vaultSetCmd.Flags().BoolVar(&vaultProject, "project", false, "Store in project vault (.clictl/ in cwd)")
	vaultSetCmd.Flags().BoolVar(&vaultWorkspace, "workspace", false, "Store in workspace secrets (remote)")
	vaultDeleteCmd.Flags().BoolVar(&vaultProject, "project", false, "Delete from project vault")
	vaultDeleteCmd.Flags().BoolVar(&vaultWorkspace, "workspace", false, "Delete from workspace secrets (remote)")
	vaultListCmd.Flags().BoolVar(&vaultWorkspace, "workspace", false, "List workspace secrets (remote)")
	vaultExportCmd.Flags().BoolVar(&vaultProject, "project", false, "Export from project vault")
	vaultExportCmd.Flags().StringVar(&vaultFormat, "format", "env", "Export format (env)")
	vaultExportCmd.Flags().BoolVar(&vaultConfirm, "confirm", false, "Confirm plaintext export")
	vaultImportCmd.Flags().BoolVar(&vaultProject, "project", false, "Import into project vault")
	vaultImportCmd.Flags().StringVar(&vaultExclude, "exclude", "", "Comma-separated keys to skip")
	vaultInitCmd.Flags().BoolVar(&vaultProject, "project", false, "Initialize project vault")
	vaultInitCmd.Flags().BoolVar(&vaultForce, "force", false, "Regenerate key and wipe vault")
	vaultInitCmd.Flags().BoolVar(&vaultPassword, "password", false, "Derive key from password")

	vaultCmd.AddCommand(vaultSetCmd)
	vaultCmd.AddCommand(vaultGetCmd)
	vaultCmd.AddCommand(vaultListCmd)
	vaultCmd.AddCommand(vaultDeleteCmd)
	vaultCmd.AddCommand(vaultExportCmd)
	vaultCmd.AddCommand(vaultImportCmd)
	vaultCmd.AddCommand(vaultInitCmd)

	rootCmd.AddCommand(vaultCmd)
}

// resolveVault returns the appropriate vault instance and project directory.
func resolveVault(project bool) (*vault.Vault, string, error) {
	if project {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, "", fmt.Errorf("getting working directory: %w", err)
		}
		return vault.NewProjectVault(cwd), cwd, nil
	}

	return vault.NewVault(config.BaseDir()), "", nil
}

// userVaultInstance returns a vault pointing to ~/.clictl.
func userVaultInstance() (*vault.Vault, error) {
	return vault.NewVault(config.BaseDir()), nil
}

// projectVaultIfExists returns a project vault if the key file exists, nil otherwise.
func projectVaultIfExists() *vault.Vault {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	v := vault.NewProjectVault(cwd)
	if v.HasKey() {
		return v
	}
	return nil
}

// formatAge returns a human-readable age string.
func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d min ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

// updateDotEnv checks if .env exists in cwd and updates it with a vault:// reference.
func updateDotEnv(name string) {
	envPath := ".env"
	data, err := os.ReadFile(envPath)
	if err != nil {
		return // no .env file, skip
	}

	lines := strings.Split(string(data), "\n")
	vaultRef := "vault://" + name
	found := false
	updated := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		idx := strings.IndexByte(trimmed, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		if key != name {
			continue
		}

		found = true
		value := strings.TrimSpace(trimmed[idx+1:])

		// Already a vault reference, skip
		if value == vaultRef {
			return
		}

		// Replace with vault reference
		lines[i] = name + "=" + vaultRef
		updated = true
		break
	}

	if !found {
		// Append to end
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = append(lines[:len(lines)-1], name+"="+vaultRef, "")
		} else {
			lines = append(lines, name+"="+vaultRef)
		}
		updated = true
	}

	if updated {
		content := strings.Join(lines, "\n")
		if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not update .env: %v\n", err)
			return
		}
		fmt.Fprintf(os.Stderr, "Updated .env: %s=%s\n", name, vaultRef)
	}
}

// workspaceConfigOrErr loads config and validates that a workspace and auth token are available.
func workspaceConfigOrErr() (*config.Config, string, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", "", fmt.Errorf("loading config: %w", err)
	}
	slug := cfg.Auth.ActiveWorkspace
	if slug == "" {
		return nil, "", "", fmt.Errorf("no active workspace. Run: clictl workspace switch <slug>")
	}
	token := config.ResolveAuthToken("", cfg)
	if token == "" {
		return nil, "", "", fmt.Errorf("not authenticated. Run: clictl login")
	}
	return cfg, slug, token, nil
}

// workspaceSecretsURL returns the base URL for workspace secrets API.
func workspaceSecretsURL(apiURL, slug string) string {
	return strings.TrimRight(apiURL, "/") + "/api/v1/workspaces/" + slug + "/secrets/"
}

// workspaceVaultSet stores a secret in the workspace via the API.
func workspaceVaultSet(name, value string) error {
	cfg, slug, token, err := workspaceConfigOrErr()
	if err != nil {
		return err
	}
	apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
	url := workspaceSecretsURL(apiURL, slug)

	payload, _ := json.Marshal(map[string]string{"name": name, "value": value})
	req, err := http.NewRequest("POST", url, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := SecureHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("workspace API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("workspace API returned %d: %s", resp.StatusCode, string(body))
	}

	fmt.Printf("Stored %s in workspace %q\n", name, slug)
	return nil
}

// workspaceVaultList lists secrets in the workspace via the API.
func workspaceVaultList() error {
	cfg, slug, token, err := workspaceConfigOrErr()
	if err != nil {
		return err
	}
	apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
	url := workspaceSecretsURL(apiURL, slug)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := SecureHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("workspace API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("workspace API returned %d: %s", resp.StatusCode, string(body))
	}

	var secrets []struct {
		Name      string `json:"name"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&secrets); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	if len(secrets) == 0 {
		fmt.Printf("No secrets stored in workspace %q.\n", slug)
		return nil
	}

	for _, s := range secrets {
		fmt.Printf("  %-30s (workspace: %s)\n", s.Name, slug)
	}
	return nil
}

// workspaceVaultDelete removes a secret from the workspace via the API.
func workspaceVaultDelete(name string) error {
	cfg, slug, token, err := workspaceConfigOrErr()
	if err != nil {
		return err
	}
	apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
	url := workspaceSecretsURL(apiURL, slug) + name + "/"

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := SecureHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("workspace API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("workspace API returned %d: %s", resp.StatusCode, string(body))
	}

	fmt.Printf("Deleted %s from workspace %q\n", name, slug)
	return nil
}

// importDotEnv reads a .env file, stores values in vault, and replaces with vault:// refs.
func importDotEnv(envPath string, project bool, exclude string) error {
	v, projectDir, err := resolveVault(project)
	if err != nil {
		return err
	}

	if !v.HasKey() {
		if err := v.InitKey(); err != nil {
			return fmt.Errorf("initializing vault key: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Vault initialized at %s\n", v.KeyPath())
	}

	excludeSet := make(map[string]bool)
	if exclude != "" {
		for _, k := range strings.Split(exclude, ",") {
			excludeSet[strings.TrimSpace(k)] = true
		}
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", envPath, err)
	}

	lines := strings.Split(string(data), "\n")
	imported := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		idx := strings.IndexByte(trimmed, '=')
		if idx < 0 {
			continue
		}

		key := strings.TrimSpace(trimmed[:idx])
		value := strings.TrimSpace(trimmed[idx+1:])

		// Skip excluded keys
		if excludeSet[key] {
			continue
		}

		// Skip already-vault references
		if strings.HasPrefix(value, "vault://") {
			continue
		}

		// Strip surrounding quotes
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		// Skip empty values
		if value == "" {
			continue
		}

		if err := v.Set(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to store %s: %v\n", key, err)
			continue
		}

		lines[i] = key + "=vault://" + key
		imported++
	}

	if imported > 0 {
		content := strings.Join(lines, "\n")
		if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing updated %s: %w", envPath, err)
		}
	}

	if project {
		vault.EnsureGitignore(projectDir)
	}

	fmt.Printf("Imported %d secrets from %s\n", imported, envPath)
	return nil
}
