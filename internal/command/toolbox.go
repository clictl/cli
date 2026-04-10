// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	flagToolboxSyncAll     bool
	flagToolboxSyncTrigger bool
	flagToolboxSyncMode    string
	flagToolboxVisibility  string
	flagToolboxBranch      string
	flagToolboxWorkspace   bool
	flagToolboxProject     bool
	flagToolboxFrom        string
)

var toolboxCmd = &cobra.Command{
	Use:   "toolbox",
	Short: "Manage tool toolboxes",
	Long:  "Add, remove, list, and update tool toolboxes. When logged in, workspace sources are inherited automatically.",
}

var toolboxUpdateCmd = &cobra.Command{
	Use:   "update [name]",
	Short: "Update toolboxes",
	Long: `Update toolboxes from their sources.

Without arguments: re-fetches the workspace CLI index and syncs all local git toolboxes.
With a name: triggers a server-side sync for that workspace toolbox source.
With --all: triggers server-side sync for all workspace sources.
With --trigger: notifies the backend that this repo has new content (for CI/GitHub Actions).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)
		token := config.ResolveAuthToken(flagAPIKey, cfg)
		ws := cfg.Auth.ActiveWorkspace

		fmt.Printf("\033[32m\u2713\033[0m clictl \U0001F9F0 %s\n", Version)
		fmt.Println(strings.Repeat("-", 40))

		// --trigger: CI mode, notify backend to sync a specific source by URL
		if flagToolboxSyncTrigger {
			if token == "" {
				return fmt.Errorf("--trigger requires authentication (set CLICTL_API_KEY)")
			}
			if ws == "" {
				return fmt.Errorf("--trigger requires an active workspace")
			}
			repoURL := ""
			if len(args) == 1 {
				repoURL = args[0]
			}
			return triggerToolboxSync(ctx, cfg.APIURL, ws, token, repoURL)
		}

		// Sync workspace sources if logged in
		if ws != "" && token != "" {
			if flagToolboxSyncAll || len(args) == 1 {
				return syncWorkspaceToolboxes(ctx, cfg.APIURL, ws, token, args)
			}
			// Default: refresh workspace cache
			wsIdx, err := registry.FetchCLIIndex(ctx, cfg.APIURL, ws, token)
			if err != nil {
				// Silently skip auth errors - token may be expired
				errStr := err.Error()
				if !strings.Contains(errStr, "session expired") && !strings.Contains(errStr, "401") && !strings.Contains(errStr, "not valid") {
					fmt.Fprintf(os.Stderr, "Warning: could not fetch workspace index: %v\n", err)
				}
			} else {
				if err := registry.SaveCachedCLIIndex(ws, wsIdx); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not cache workspace index: %v\n", err)
				}
				fmt.Printf("  %d workspace sources, %d favorites\n", len(wsIdx.Sources), len(wsIdx.Favorites))
			}
		}

		// Sync local toolboxes
		toolboxesDir := config.ToolboxesDir()
		for _, sh := range cfg.Toolboxes {
			destDir := filepath.Join(toolboxesDir, sh.Name)
			fmt.Printf("Updating %s (%s)...\n", sh.Name, sh.Type)

			switch sh.Type {
			case "api":
				apiURL := sh.URL
				if apiURL == "" {
					apiURL = cfg.APIURL
				}
				if err := registry.SyncAPI(ctx, apiURL, destDir); err != nil {
					fmt.Fprintf(os.Stderr, "  Error updating %s: %v\n", sh.Name, err)
					continue
				}
			case "git":
				if err := registry.SyncGit(ctx, sh.URL, sh.Branch, destDir); err != nil {
					fmt.Fprintf(os.Stderr, "  Error updating %s: %v\n", sh.Name, err)
					continue
				}
			default:
				fmt.Fprintf(os.Stderr, "  Unknown toolbox type %q for %s\n", sh.Type, sh.Name)
				continue
			}

			fmt.Printf("  Updated %s successfully.\n", sh.Name)
		}

		// Check for outdated installed tools
		installed := loadInstalled()
		if len(installed) > 0 {
			lockFile, _ := LoadLockFile()
			var outdated []struct{ name, installed, latest string }

			for _, toolName := range installed {
				installedVersion := ""
				if lockFile != nil {
					if entry, ok := lockFile.Tools[toolName]; ok {
						installedVersion = entry.Version
					}
				}
				if installedVersion == "" {
					continue
				}

				latestVersion := ""
				for _, reg := range cfg.Toolboxes {
					regDir := filepath.Join(toolboxesDir, reg.Name)
					li := registry.NewLocalIndex(regDir, reg.Name)
					entry, err := li.GetEntry(toolName)
					if err != nil {
						continue
					}
					latestVersion = entry.Version
					break
				}

				if latestVersion != "" && installedVersion != latestVersion {
					outdated = append(outdated, struct{ name, installed, latest string }{
						toolName, installedVersion, latestVersion,
					})
				}
			}

			if len(outdated) > 0 {
				fmt.Printf("\n==> Outdated Tools\n")
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				for _, t := range outdated {
					fmt.Fprintf(w, "%s\tv%s -> v%s\n", t.name, t.installed, t.latest)
				}
				w.Flush()
				fmt.Printf("\nYou have %d outdated %s installed.\n", len(outdated), pluralize(len(outdated), "tool", "tools"))
				fmt.Println("You can upgrade them with clictl upgrade --all")
				fmt.Println("or list them with clictl outdated.")
			}
		}

		return nil
	},
}

var toolboxListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured toolboxes",
	Long:  "Lists all toolbox sources by scope: project > workspace > personal > curated.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

		// Try to load workspace sources
		var wsIdx *registry.CLIIndexResponse
		if cfg.Auth.ActiveWorkspace != "" {
			wsIdx, _ = registry.LoadCachedCLIIndex(cfg.Auth.ActiveWorkspace)
		}

		merged := registry.MergedRegistries(cfg, wsIdx)

		// Load project-level toolboxes
		projectToolboxes := loadProjectToolboxes()

		if len(merged) == 0 && len(projectToolboxes) == 0 {
			fmt.Println("No toolboxes configured.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tSCOPE\tTYPE\tVISIBILITY\tDEFAULT")

		// Project sources first (highest priority)
		for _, pt := range projectToolboxes {
			fmt.Fprintf(w, "\U0001f9f0 %s\tProject\tgit\t-\t\n", pt.Name)
		}

		for _, sh := range merged {
			scope := "Personal"
			vis := "-"
			if sh.FromWorkspace {
				scope = "Workspace"
				if sh.IsPrivate {
					vis = "restricted"
				} else {
					vis = "public"
				}
			} else if sh.Default || registry.IsCuratedToolbox(sh) {
				scope = "Curated"
			}
			// Workspace sources may carry a scope field from the API
			if sh.Scope != "" {
				switch sh.Scope {
				case "personal":
					scope = "Personal"
				case "workspace":
					scope = "Workspace"
				case "project":
					scope = "Project"
				}
			}
			def := ""
			if sh.Default {
				def = "yes"
			}
			fmt.Fprintf(w, "\U0001f9f0 %s\t%s\t%s\t%s\t%s\n", sh.Name, scope, sh.Type, vis, def)
		}
		return w.Flush()
	},
}

// loadProjectToolboxes reads .clictl/toolboxes.yaml from the project root.
func loadProjectToolboxes() []ProjectToolboxSource {
	projectDir := findProjectRoot()
	if projectDir == "" {
		return nil
	}
	toolboxesFile := filepath.Join(projectDir, ".clictl", "toolboxes.yaml")
	data, err := os.ReadFile(toolboxesFile)
	if err != nil {
		return nil
	}
	var toolboxes ProjectToolboxes
	if err := yaml.Unmarshal(data, &toolboxes); err != nil {
		return nil
	}
	return toolboxes.Sources
}

var toolboxAddCmd = &cobra.Command{
	Use:   "add <owner/repo or url>",
	Short: "Add a toolbox to the workspace or local config",
	Long: `Add a toolbox source. Accepts GitHub shorthand (owner/repo) or a full URL.

  clictl toolbox add garrytan/gstack                # personal scope (default)
  clictl toolbox add garrytan/gstack --workspace    # workspace scope (admin)
  clictl toolbox add garrytan/gstack --project      # project scope (.clictl/toolboxes.yaml)

Default scope is personal when logged in with a workspace. Use --workspace to
add as a workspace-wide source (visible to all members, admin only). Use
--project to write to .clictl/toolboxes.yaml in the current project.

When not logged in, toolboxes are always added to local config.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		repoURL := expandGitHubShorthand(args[0])
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)
		token := config.ResolveAuthToken(flagAPIKey, cfg)
		ws := cfg.Auth.ActiveWorkspace

		// --project: write to .clictl/toolboxes.yaml in current project
		if flagToolboxProject {
			return addProjectToolbox(repoURL)
		}

		// If logged in, add to workspace via API
		if ws != "" && token != "" {
			scope := "personal"
			if flagToolboxWorkspace {
				scope = "workspace"
			}
			return addWorkspaceToolbox(ctx, cfg.APIURL, ws, token, repoURL, scope)
		}

		// Otherwise, add locally
		name := deriveToolboxName(repoURL)
		if name == "" {
			return fmt.Errorf("could not derive toolbox name from URL %q", repoURL)
		}

		for _, sh := range cfg.Toolboxes {
			if sh.Name == name {
				return fmt.Errorf("toolbox %q already exists", name)
			}
		}

		newToolbox := config.ToolboxConfig{
			Name:    name,
			Type:    "git",
			URL:     repoURL,
			Branch:  flagToolboxBranch,
			Default: false,
		}
		cfg.Toolboxes = append(cfg.Toolboxes, newToolbox)

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		destDir := filepath.Join(config.ToolboxesDir(), name)
		if err := registry.SyncGit(ctx, repoURL, flagToolboxBranch, destDir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: initial sync failed: %v\n", err)
			fmt.Println("You can retry with: clictl toolbox update")
			return nil
		}

		fmt.Printf("\U0001f9f0 Added toolbox: %s\n", name)
		return nil
	},
}

// addProjectToolbox writes a toolbox to the project-level .clictl/toolboxes.yaml file.
func addProjectToolbox(repoURL string) error {
	projectDir := findProjectRoot()
	if projectDir == "" {
		return fmt.Errorf("could not find project root (no .clictl/ directory or .git root found)")
	}

	toolboxesFile := filepath.Join(projectDir, ".clictl", "toolboxes.yaml")
	if err := os.MkdirAll(filepath.Dir(toolboxesFile), 0o755); err != nil {
		return fmt.Errorf("creating .clictl directory: %w", err)
	}

	var toolboxes ProjectToolboxes
	if data, err := os.ReadFile(toolboxesFile); err == nil {
		if err := yaml.Unmarshal(data, &toolboxes); err != nil {
			return fmt.Errorf("parsing %s: %w", toolboxesFile, err)
		}
	}

	name := deriveToolboxName(repoURL)
	for _, t := range toolboxes.Sources {
		if t.URL == repoURL || t.Name == name {
			return fmt.Errorf("toolbox %q already exists in project config", name)
		}
	}

	toolboxes.Sources = append(toolboxes.Sources, ProjectToolboxSource{
		Name: name,
		URL:  repoURL,
	})

	data, err := yaml.Marshal(toolboxes)
	if err != nil {
		return fmt.Errorf("marshalling project toolboxes: %w", err)
	}
	if err := os.WriteFile(toolboxesFile, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", toolboxesFile, err)
	}

	fmt.Printf("\U0001f9f0 Added project toolbox: %s\n", name)
	fmt.Printf("  Written to %s\n", toolboxesFile)
	return nil
}

// ProjectToolboxes represents the .clictl/toolboxes.yaml file.
type ProjectToolboxes struct {
	Sources []ProjectToolboxSource `yaml:"sources"`
}

// ProjectToolboxSource is a single toolbox entry in project config.
type ProjectToolboxSource struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

// findProjectRoot walks up from CWD to find .clictl/ or .git root.
func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		// Check for .clictl/toolboxes.yaml first
		if info, err := os.Stat(filepath.Join(dir, ".clictl")); err == nil && info.IsDir() {
			return dir
		}
		// Check for .git root
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

var toolboxRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a toolbox",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

		found := false
		filtered := make([]config.ToolboxConfig, 0, len(cfg.Toolboxes))
		for _, sh := range cfg.Toolboxes {
			if sh.Name == name {
				found = true
				continue
			}
			filtered = append(filtered, sh)
		}

		if !found {
			return fmt.Errorf("toolbox %q not found", name)
		}

		cfg.Toolboxes = filtered

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		shDir := filepath.Join(config.ToolboxesDir(), name)
		if err := os.RemoveAll(shDir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not remove local directory %s: %v\n", shDir, err)
		}

		fmt.Printf("Removed toolbox %q.\n", name)
		return nil
	},
}

var toolboxValidateCmd = &cobra.Command{
	Use:   "validate <path>",
	Short: "Validate all specs in a toolbox directory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := args[0]
		valid := 0
		invalid := 0
		warned := 0

		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(info.Name(), ".yaml") {
				return nil
			}
			// Skip .meta.yaml
			if info.Name() == ".meta.yaml" {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  FAIL %s: %v\n", relPath(dir, path), err)
				invalid++
				return nil
			}

			// Parse as raw map for old-format detection
			var raw map[string]interface{}
			if err := yaml.Unmarshal(data, &raw); err != nil {
				fmt.Fprintf(os.Stderr, "  FAIL %s: invalid YAML: %v\n", relPath(dir, path), err)
				invalid++
				return nil
			}

			name, _ := raw["name"].(string)
			if name == "" {
				fmt.Fprintf(os.Stderr, "  FAIL %s: missing 'name' field\n", relPath(dir, path))
				invalid++
				return nil
			}

			// Run V1 validation checks
			specErrors, specWarns := validateSpecV1(raw, relPath(dir, path))

			if len(specErrors) > 0 {
				for _, e := range specErrors {
					fmt.Fprintf(os.Stderr, "  FAIL %s: %s\n", name, e)
				}
				invalid++
			} else {
				fmt.Printf("  OK   %s\n", name)
				valid++
			}
			for _, w := range specWarns {
				fmt.Fprintf(os.Stderr, "  WARN %s: %s\n", name, w)
				warned++
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("walking directory: %w", err)
		}

		fmt.Printf("\n%d valid, %d invalid", valid, invalid)
		if warned > 0 {
			fmt.Printf(", %d warnings", warned)
		}
		fmt.Println()
		if invalid > 0 {
			os.Exit(1)
		}
		return nil
	},
}

// relPath returns path relative to base, or the full path on error.
func relPath(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	return rel
}

// validateSpecV1 runs spec 1.0 validation checks on a raw parsed YAML map.
// Returns (errors, warnings).
func validateSpecV1(raw map[string]interface{}, path string) ([]string, []string) {
	var errs, warns []string

	// Check for old format fields at top level
	oldFields := []string{"protocol", "connection", "transport"}
	for _, f := range oldFields {
		if _, ok := raw[f]; ok {
			errs = append(errs, fmt.Sprintf("old format field %q found, use spec 1.0 format", f))
		}
	}

	// Validate server.type if present
	validServerTypes := map[string]bool{"http": true, "stdio": true, "command": true}
	if server, ok := raw["server"].(map[string]interface{}); ok {
		if stype, ok := server["type"].(string); ok {
			if !validServerTypes[stype] {
				errs = append(errs, fmt.Sprintf("server.type %q is not valid (expected http, stdio, or command)", stype))
			}
		}
	}

	// Validate auth.env identifiers
	if auth, ok := raw["auth"].(map[string]interface{}); ok {
		if env := auth["env"]; env != nil {
			var envKeys []string
			switch v := env.(type) {
			case string:
				envKeys = []string{v}
			case []interface{}:
				for _, e := range v {
					if s, ok := e.(string); ok {
						envKeys = append(envKeys, s)
					}
				}
			}
			for _, key := range envKeys {
				if !isValidEnvIdentifier(key) {
					errs = append(errs, fmt.Sprintf("auth.env %q is not a valid identifier (alphanumeric and underscore only)", key))
				}
			}
		}
	}

	// Validate actions
	validMethods := map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}
	validTransformTypes := map[string]bool{
		"json": true, "truncate": true, "format": true, "template": true,
		"html_to_markdown": true, "sort": true, "filter": true, "unique": true,
		"group": true, "count": true, "join": true, "split": true, "flatten": true,
		"unwrap": true, "date_format": true, "xml_to_json": true, "csv_to_json": true,
		"base64_decode": true, "redact": true, "cost": true, "pipe": true,
		"jq": true, "js": true, "prompt": true, "prefix": true, "merge": true,
		"markdown_to_text": true,
	}
	validAssertTypes := map[string]bool{
		"status": true, "json": true, "jq": true, "js": true, "cel": true, "contains": true,
	}

	if actions, ok := raw["actions"].([]interface{}); ok {
		for i, a := range actions {
			action, ok := a.(map[string]interface{})
			if !ok {
				continue
			}
			actionName, _ := action["name"].(string)
			if actionName == "" {
				actionName = fmt.Sprintf("actions[%d]", i)
			}

			// Check for old format field "safe" on actions
			if _, ok := action["safe"]; ok {
				errs = append(errs, fmt.Sprintf("action %q: old format field \"safe\" found, use \"mutable\" instead", actionName))
			}

			// Validate method (flattened format: method directly on action)
			if method, ok := action["method"].(string); ok {
				upper := strings.ToUpper(method)
				if !validMethods[upper] {
					errs = append(errs, fmt.Sprintf("action %q: method %q is not a valid HTTP method", actionName, method))
				}
			}

			// Validate old nested request block (still accepted for backward compat)
			if req, ok := action["request"].(map[string]interface{}); ok {
				warns = append(warns, fmt.Sprintf("action %q: nested 'request' block is deprecated, use method/url/path directly on the action", actionName))
				if method, ok := req["method"].(string); ok {
					upper := strings.ToUpper(method)
					if !validMethods[upper] {
						errs = append(errs, fmt.Sprintf("action %q: request.method %q is not a valid HTTP method", actionName, method))
					}
				}
			}

			// Warn on actions without method, url, run, or steps (might be MCP static)
			_, hasMethod := action["method"]
			_, hasURL := action["url"]
			_, hasRun := action["run"]
			_, hasSteps := action["steps"]
			_, hasRequest := action["request"]
			if !hasMethod && !hasURL && !hasRun && !hasSteps && !hasRequest {
				warns = append(warns, fmt.Sprintf("action %q: no method, url, run, or steps (MCP static?)", actionName))
			}

			// Validate transform types
			if transforms, ok := action["transform"].([]interface{}); ok {
				for j, t := range transforms {
					if tm, ok := t.(map[string]interface{}); ok {
						if ttype, ok := tm["type"].(string); ok {
							if !validTransformTypes[ttype] {
								errs = append(errs, fmt.Sprintf("action %q: transform[%d].type %q is not a known type", actionName, j, ttype))
							}
						}
					}
				}
			}

			// Validate assert types
			if asserts, ok := action["assert"].([]interface{}); ok {
				for j, a := range asserts {
					if am, ok := a.(map[string]interface{}); ok {
						if atype, ok := am["type"].(string); ok {
							if !validAssertTypes[atype] {
								errs = append(errs, fmt.Sprintf("action %q: assert[%d].type %q is not a known type", actionName, j, atype))
							}
						}
					}
				}
			}
		}
	}

	return errs, warns
}

// isValidEnvIdentifier checks that a string is alphanumeric + underscore only.
func isValidEnvIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}

var toolboxCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Scaffold a new toolbox directory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		exampleDir := filepath.Join(name, "toolbox", "example")
		if err := os.MkdirAll(exampleDir, 0o755); err != nil {
			return err
		}

		// .meta.yaml
		meta := fmt.Sprintf("name: %s\ndescription: A clictl tool toolbox\n", name)
		os.WriteFile(filepath.Join(name, "toolbox", ".meta.yaml"), []byte(meta), 0o644)

		// Example spec (in its own directory)
		example := "name: example\ndescription: An example tool spec\nversion: \"1.0\"\ncategory: developer\n\nactions:\n  - name: hello\n    description: Say hello\n    url: https://api.example.com\n    path: /hello\n"
		os.WriteFile(filepath.Join(exampleDir, "example.yaml"), []byte(example), 0o644)

		// README
		readme := fmt.Sprintf("# %s\n\nA clictl tool toolbox.\n\n## Usage\n\n```bash\nclictl toolbox add https://github.com/you/%s\n```\n\n## Structure\n\nEach tool gets its own directory inside `toolbox/`:\n\n```\ntoolbox/\n  my-tool/\n    my-tool.yaml         # latest version\n    1.0.yaml             # pinned version (optional)\n```\n", name, name)
		os.WriteFile(filepath.Join(name, "README.md"), []byte(readme), 0o644)

		// .gitignore
		os.WriteFile(filepath.Join(name, ".gitignore"), []byte(".DS_Store\n"), 0o644)

		fmt.Printf("\U0001f9f0 Created toolbox: %s/\n", name)
		fmt.Printf("  toolbox/.meta.yaml\n  toolbox/example/example.yaml\n  README.md\n")
		return nil
	},
}

// addWorkspaceToolbox adds a toolbox source to the workspace via the API.
// scope is "personal" or "workspace".
func addWorkspaceToolbox(ctx context.Context, apiURL, workspace, token, repoURL, scope string) error {
	u := fmt.Sprintf("%s/api/v1/workspaces/%s/registries/", apiURL, url.PathEscape(workspace))

	body := map[string]interface{}{
		"url":   repoURL,
		"scope": scope,
	}
	if flagToolboxSyncMode != "" {
		body["sync_mode"] = flagToolboxSyncMode
	}
	if flagToolboxVisibility != "" {
		body["visibility"] = flagToolboxVisibility
	}
	if flagToolboxBranch != "" {
		body["default_branch"] = flagToolboxBranch
	}

	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("adding toolbox: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		name, _ := result["name"].(string)
		scopeLabel := "personal"
		if scope == "workspace" {
			scopeLabel = "workspace"
		}
		fmt.Printf("\U0001f9f0 Added %s toolbox: %s\n", scopeLabel, name)
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("failed to add toolbox: %d %s", resp.StatusCode, string(respBody))
}

// syncWorkspaceToolboxes triggers server-side sync for workspace sources.
func syncWorkspaceToolboxes(ctx context.Context, apiURL, workspace, token string, args []string) error {
	// Fetch current sources
	wsIdx, err := registry.FetchCLIIndex(ctx, apiURL, workspace, token)
	if err != nil {
		return fmt.Errorf("fetching workspace sources: %w", err)
	}

	for _, src := range wsIdx.Sources {
		if len(args) == 1 && src.Name != args[0] {
			continue
		}

		fmt.Printf("Triggering sync for %s...\n", src.Name)
		u := fmt.Sprintf("%s/api/v1/workspaces/%s/registries/%s/sync/",
			apiURL, url.PathEscape(workspace), src.ID)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusAccepted {
			fmt.Printf("  Sync queued for %s\n", src.Name)
		} else if resp.StatusCode == http.StatusConflict {
			fmt.Printf("  %s is already syncing\n", src.Name)
		} else {
			fmt.Fprintf(os.Stderr, "  Sync trigger returned %d for %s\n", resp.StatusCode, src.Name)
		}
	}

	return nil
}

// triggerToolboxSync notifies the backend to sync a specific source or all sources.
// Used in CI/GitHub Actions with CLICTL_API_KEY.
func triggerToolboxSync(ctx context.Context, apiURL, workspace, token, repoURL string) error {
	// Fetch workspace sources to find the matching one
	wsIdx, err := registry.FetchCLIIndex(ctx, apiURL, workspace, token)
	if err != nil {
		return fmt.Errorf("fetching workspace sources: %w", err)
	}

	matched := false
	for _, src := range wsIdx.Sources {
		if repoURL != "" && !strings.Contains(src.URL, repoURL) && src.Name != repoURL {
			continue
		}

		fmt.Printf("Triggering sync for %s (%s)...\n", src.Name, src.URL)
		u := fmt.Sprintf("%s/api/v1/workspaces/%s/registries/%s/sync/",
			apiURL, url.PathEscape(workspace), src.ID)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("triggering sync: %w", err)
		}
		resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusAccepted:
			fmt.Printf("  Sync queued successfully\n")
		case http.StatusConflict:
			fmt.Printf("  Already syncing\n")
		default:
			return fmt.Errorf("sync trigger returned %d", resp.StatusCode)
		}
		matched = true
	}

	if !matched {
		if repoURL != "" {
			return fmt.Errorf("no workspace source matches %q", repoURL)
		}
		fmt.Println("No workspace sources to sync.")
	}

	return nil
}

// deriveToolboxName extracts a toolbox name from a git URL.
// expandGitHubShorthand expands "owner/repo" to "https://github.com/owner/repo".
// Full URLs and SSH-style URLs are returned as-is.
func expandGitHubShorthand(input string) string {
	// Already a URL or SSH-style
	if strings.Contains(input, "://") || strings.HasPrefix(input, "git@") {
		return input
	}
	// Looks like owner/repo (exactly one slash, no dots suggesting a domain)
	parts := strings.Split(input, "/")
	if len(parts) == 2 && !strings.Contains(parts[0], ".") {
		return "https://github.com/" + input
	}
	return input
}

func deriveToolboxName(u string) string {
	u = strings.TrimSuffix(u, ".git")

	// Handle SSH-style URLs: git@github.com:owner/repo
	if strings.Contains(u, ":") && strings.HasPrefix(u, "git@") {
		parts := strings.SplitN(u, ":", 2)
		if len(parts) == 2 {
			return parts[1]
		}
	}

	// Handle HTTPS URLs: https://github.com/owner/repo
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")

	segments := strings.Split(u, "/")
	if len(segments) >= 3 {
		return segments[len(segments)-2] + "/" + segments[len(segments)-1]
	}
	if len(segments) >= 2 {
		return segments[len(segments)-1]
	}

	return ""
}

func init() {
	toolboxUpdateCmd.Flags().BoolVar(&flagToolboxSyncAll, "all", false, "Sync all workspace sources")
	toolboxUpdateCmd.Flags().BoolVar(&flagToolboxSyncTrigger, "trigger", false, "Trigger backend sync (for CI/GitHub Actions)")
	toolboxAddCmd.Flags().StringVar(&flagToolboxSyncMode, "sync-mode", "", "Sync mode: full or metadata_only")
	toolboxAddCmd.Flags().StringVar(&flagToolboxVisibility, "visibility", "", "Visibility: public, workspace, or restricted")
	toolboxAddCmd.Flags().StringVar(&flagToolboxBranch, "branch", "", "Git branch (default: main)")
	toolboxAddCmd.Flags().BoolVar(&flagToolboxWorkspace, "workspace", false, "Add as workspace-wide source (admin only)")
	toolboxAddCmd.Flags().BoolVar(&flagToolboxProject, "project", false, "Add to .clictl/toolboxes.yaml in the project root")
	toolboxCmd.PersistentFlags().StringVar(&flagToolboxFrom, "from", "", "Override resolution source (scope or source name)")

	toolboxCmd.AddCommand(toolboxUpdateCmd)
	toolboxCmd.AddCommand(toolboxListCmd)
	toolboxCmd.AddCommand(toolboxAddCmd)
	toolboxCmd.AddCommand(toolboxRemoveCmd)
	toolboxCmd.AddCommand(toolboxValidateCmd)
	toolboxCmd.AddCommand(toolboxCreateCmd)
	rootCmd.AddCommand(toolboxCmd)
}
