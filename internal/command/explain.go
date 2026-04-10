// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/mcp"
	"github.com/clictl/cli/internal/memory"
	"github.com/clictl/cli/internal/registry"
)

// explainParam is a parameter entry in the explain output.
type explainParam struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Default     string `json:"default,omitempty"`
	Example     string `json:"example,omitempty"`
}

// explainMemory is a memory entry in the explain output.
type explainMemory struct {
	Note string `json:"note"`
	Type string `json:"type"`
}

// explainOutput is the structured output of the explain command.
type explainOutput struct {
	Tool           string          `json:"tool"`
	Action         string          `json:"action"`
	Description    string          `json:"description"`
	Usage          string          `json:"usage"`
	Output         string          `json:"output,omitempty"`
	RequiredParams []explainParam  `json:"required_params"`
	OptionalParams []explainParam  `json:"optional_params"`
	AuthRequired   bool            `json:"auth_required"`
	AuthEnv        string          `json:"auth_env,omitempty"`
	AuthSet        bool            `json:"auth_set"`
	Safe           bool            `json:"safe"`
	Memories       []explainMemory `json:"memories,omitempty"`
}

var explainCmd = &cobra.Command{
	Use:   "explain <tool> <action>",
	Short: "Agent-friendly structured help for a tool action",
	Long: `Returns a structured JSON explanation of a tool action that agents can parse.

  clictl explain openweathermap current
  clictl explain github list-repos`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		toolName := args[0]
		actionName := args[1]
		ctx := cmd.Context()

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

		cache := registry.NewCache(cfg.CacheDir)
		spec, err := registry.ResolveSpec(ctx, toolName, cfg, cache, flagNoCache)
		if err != nil {
			msg := fmt.Sprintf("tool %q not found", toolName)
			if dym := toolSuggestion(toolName, cfg); dym != "" {
				msg += dym
			}
			return fmt.Errorf("%s", msg)
		}

		// MCP runtime discovery: merge discovered tools with static actions
		if spec.Discover {
			discovered, discoverErr := mcp.DiscoverTools(ctx, spec)
			if discoverErr == nil {
				spec.Actions = mcp.MergeActions(spec.Actions, discovered, spec.Allow, spec.Deny)
			}
		}

		action, err := registry.FindAction(spec, actionName)
		if err != nil {
			return err
		}

		// Build usage string, preferring examples when available
		usage := fmt.Sprintf("clictl run %s %s", spec.Name, action.Name)
		for _, p := range action.Params {
			if p.Required {
				if p.Example != "" {
					usage += fmt.Sprintf(" --%s %q", p.Name, p.Example)
				} else {
					usage += fmt.Sprintf(" --%s <%s>", p.Name, p.Type)
				}
			}
		}

		// Separate required and optional params
		var required, optional []explainParam
		for _, p := range action.Params {
			ep := explainParam{
				Name:        p.Name,
				Type:        p.Type,
				Description: p.Description,
				Default:     p.Default,
				Example:     p.Example,
			}
			if p.Required {
				required = append(required, ep)
			} else {
				optional = append(optional, ep)
			}
		}

		// Check auth
		authRequired := false
		authEnv := ""
		authSet := false
		if spec.Auth != nil && len(spec.Auth.Env) > 0 {
			authRequired = true
			authEnv = spec.Auth.Env[0]
			if authEnv != "" && os.Getenv(authEnv) != "" {
				authSet = true
			}
		}

		// Load memories for this tool
		var memories []explainMemory
		if mem, _ := memory.Load(spec.Name); len(mem) > 0 {
			for _, m := range mem {
				memories = append(memories, explainMemory{
					Note: m.Note,
					Type: string(m.Type),
				})
			}
		}

		output := explainOutput{
			Tool:           spec.Name,
			Action:         action.Name,
			Description:    action.Description,
			Usage:          usage,
			Output:         action.Output,
			RequiredParams: required,
			OptionalParams: optional,
			AuthRequired:   authRequired,
			AuthEnv:        authEnv,
			AuthSet:        authSet,
			Safe:           !action.Mutable,
			Memories:       memories,
		}

		// Default to empty slices for clean JSON output
		if output.RequiredParams == nil {
			output.RequiredParams = []explainParam{}
		}
		if output.OptionalParams == nil {
			output.OptionalParams = []explainParam{}
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	},
}

func init() {
	rootCmd.AddCommand(explainCmd)
}
