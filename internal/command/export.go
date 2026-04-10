// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/registry"
)

var (
	exportOutput string
	exportFormat string
)

var exportCmd = &cobra.Command{
	Use:   "export <tool>",
	Short: "Export a tool spec as YAML or SKILL.md",
	Long: `Export a tool specification to stdout or a file.

  # Export spec YAML to stdout
  clictl export github

  # Export to a file
  clictl export github --output github.yaml

  # Export as SKILL.md format
  clictl export github --format skill

  # Export a namespaced tool
  clictl export anthropic/xlsx`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		toolRef := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

		cache := registry.NewCache(cfg.CacheDir)
		client := registry.NewClient(cfg.APIURL, cache, flagNoCache)

		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token != "" {
			client.AuthToken = token
		}

		// Parse namespace if present
		namespace, name := parseNamespacedTool(toolRef)

		var spec *models.ToolSpec
		var rawYAML []byte

		if namespace != "" {
			// Try workspace export endpoint first
			if token != "" && cfg.Auth.ActiveWorkspace != "" {
				exportedYAML, exportErr := fetchWorkspaceExport(cmd, cfg, token, toolRef)
				if exportErr == nil && len(exportedYAML) > 0 {
					rawYAML = exportedYAML
					spec, _ = registry.ParseSpec(rawYAML)
				}
			}
			// Fall back to API lookup
			if spec == nil {
				var fetchErr error
				spec, rawYAML, fetchErr = client.GetSpecYAML(ctx, name)
				if fetchErr != nil {
					return fmt.Errorf("tool %q not found: %w", toolRef, fetchErr)
				}
				if spec.Namespace != "" && spec.Namespace != namespace {
					return fmt.Errorf("tool %q resolved to namespace %q, not %q", name, spec.Namespace, namespace)
				}
			}
		} else {
			var fetchErr error
			spec, rawYAML, fetchErr = client.GetSpecYAML(ctx, name)
			if fetchErr != nil {
				msg := fmt.Sprintf("tool %q not found", toolRef)
				if dym := toolSuggestion(toolRef, cfg); dym != "" {
					msg += dym
				}
				return fmt.Errorf("%s", msg)
			}
		}

		// Generate output based on format
		var output []byte
		switch exportFormat {
		case "skill":
			output = []byte(exportAsSkillMD(spec))
		default:
			if len(rawYAML) > 0 {
				output = rawYAML
			} else {
				// Marshal spec as YAML
				data, marshalErr := yaml.Marshal(spec)
				if marshalErr != nil {
					return fmt.Errorf("marshaling spec: %w", marshalErr)
				}
				output = data
			}
		}

		// Write to file or stdout
		if exportOutput != "" {
			if err := os.WriteFile(exportOutput, output, 0o644); err != nil {
				return fmt.Errorf("writing output file: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Exported %s to %s\n", spec.Name, exportOutput)
			return nil
		}

		fmt.Print(string(output))
		return nil
	},
}

// fetchWorkspaceExport tries to GET the spec from the workspace export endpoint.
func fetchWorkspaceExport(cmd *cobra.Command, cfg *config.Config, token, toolRef string) ([]byte, error) {
	apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
	// The workspace export endpoint expects a tool ID or name.
	// We try the name-based lookup.
	client := registry.NewClient(apiURL, nil, true)
	client.AuthToken = token

	spec, rawYAML, err := client.GetSpecYAML(cmd.Context(), toolRef)
	if err != nil {
		return nil, err
	}
	_ = spec
	return rawYAML, nil
}

// exportAsSkillMD converts a tool spec into SKILL.md format.
func exportAsSkillMD(spec *models.ToolSpec) string {
	var b strings.Builder

	// Frontmatter
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("name: %s\n", spec.Name))
	if spec.Description != "" {
		b.WriteString(fmt.Sprintf("description: %s\n", spec.Description))
	}

	// Determine allowed tools from sandbox
	allowedTools := resolveAllowedTools(spec)
	b.WriteString(fmt.Sprintf("allowed-tools: [%s]\n", strings.Join(allowedTools, ", ")))
	b.WriteString("---\n\n")

	// Title
	b.WriteString(fmt.Sprintf("# %s\n\n", spec.Name))
	b.WriteString(fmt.Sprintf("%s\n\n", spec.Description))

	if spec.Version != "" {
		b.WriteString(fmt.Sprintf("Version: %s\n\n", spec.Version))
	}

	// Instructions
	if spec.Instructions != "" {
		b.WriteString("## Instructions\n\n")
		b.WriteString(spec.Instructions)
		b.WriteString("\n\n")
	}

	// Actions
	if len(spec.Actions) > 0 {
		b.WriteString("## Actions\n\n")
		for _, action := range spec.Actions {
			b.WriteString(fmt.Sprintf("### %s\n\n", action.Name))
			b.WriteString(fmt.Sprintf("%s\n\n", action.Description))

			if len(action.Params) > 0 {
				b.WriteString("Parameters:\n")
				for _, p := range action.Params {
					req := ""
					if p.Required {
						req = " (required)"
					}
					b.WriteString(fmt.Sprintf("- `%s` (%s)%s: %s\n", p.Name, p.Type, req, p.Description))
				}
				b.WriteString("\n")
			}

			// Example usage
			example := fmt.Sprintf("clictl run %s %s", spec.Name, action.Name)
			for _, p := range action.Params {
				if p.Required {
					example += fmt.Sprintf(" --%s <value>", p.Name)
				}
			}
			b.WriteString(fmt.Sprintf("```bash\n%s\n```\n\n", example))
		}
	}

	// Auth
	if spec.Auth != nil && len(spec.Auth.Env) > 0 {
		b.WriteString("## Authentication\n\n")
		b.WriteString("Required environment variables:\n")
		for _, envKey := range spec.Auth.Env {
			b.WriteString(fmt.Sprintf("- `%s`\n", envKey))
		}
		b.WriteString("\nSet via: `clictl vault set <KEY> <value>`\n")
	}

	return b.String()
}

func init() {
	exportCmd.Flags().StringVar(&exportOutput, "output", "", "Write to file instead of stdout")
	exportCmd.Flags().StringVar(&exportFormat, "format", "yaml", "Output format: yaml or skill")
	rootCmd.AddCommand(exportCmd)
}
