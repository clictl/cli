// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/enterprise"
	"github.com/clictl/cli/internal/executor"
	"github.com/clictl/cli/internal/mcp"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/registry"
)

var mcpToolsOnly bool
var mcpNoSandbox bool
var mcpCodeMode bool

var mcpServeCmd = &cobra.Command{
	Use:   "mcp-serve [tools...]",
	Short: "Run as an MCP stdio server exposing tool specs",
	Long: `Start an MCP (Model Context Protocol) server over stdio. This exposes
tool spec actions as MCP tools that AI providers can discover and call.

By default, clictl management commands (search, list, inspect, exec) are
exposed alongside any tool specs, so your Agent can discover new tools.

  # Serve with management commands + all installed tools
  clictl mcp-serve

  # Serve with management commands + specific tools
  clictl mcp-serve openweathermap github

  # Serve only the specified tools (no management commands)
  clictl mcp-serve openweathermap github --tools-only

Use this as the command in MCP server configurations:

  {
    "mcpServers": {
      "clictl": {
        "command": "clictl",
        "args": ["mcp-serve"]
      }
    }
  }`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
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

		// Configure sandbox: on by default, opt-out via flag or config.
		// Enterprise workspace policy can enforce sandbox (cannot be disabled).
		ep := enterprise.GetProvider()
		if ep.SandboxRequired() && (mcpNoSandbox || !cfg.Sandbox) {
			fmt.Fprintln(os.Stderr, "Workspace policy requires sandbox. The --no-sandbox flag and sandbox: false config are not allowed.")
			mcpNoSandbox = false
			cfg.Sandbox = true
		}

		sandboxEnabled := !mcpNoSandbox && cfg.Sandbox
		mcp.SetSandboxEnabled(sandboxEnabled)
		mcp.SetStrictSandbox(cfg.StrictSandboxEnabled())
		if !sandboxEnabled {
			fmt.Fprintln(os.Stderr, "Sandbox disabled: MCP servers will run with full environment access")
		}

		globalMode := !mcpToolsOnly

		// Determine which tools to serve
		toolNames := args
		if len(toolNames) == 0 {
			toolNames = loadInstalled()
		}

		// In tools-only mode, we need at least one tool
		if len(toolNames) == 0 && mcpToolsOnly {
			fmt.Fprintln(os.Stderr, "No tools specified. Install tools with 'clictl install <tool>' or remove --tools-only.")
			os.Exit(1)
		}

		// Load all specs (resolve from local toolboxes first, then API)
		var specs []*models.ToolSpec
		for _, name := range toolNames {
			spec, err := registry.ResolveSpec(cmd.Context(), name, cfg, cache, flagNoCache)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not load %s: %v\n", name, err)
				continue
			}
			specs = append(specs, spec)
		}

		if len(specs) == 0 && mcpToolsOnly {
			return fmt.Errorf("no valid tool specs loaded")
		}

		if globalMode {
			fmt.Fprintf(os.Stderr, "MCP server ready: serving %d tool(s) + clictl management commands\n", len(specs))
		} else {
			fmt.Fprintf(os.Stderr, "MCP server ready: serving %d tool(s)\n", len(specs))
		}

		server := mcp.NewServer(specs)
		server.GlobalMode = globalMode
		server.CodeMode = mcpCodeMode
		server.Dispatch = func(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]any) ([]byte, error) {
			return executor.Dispatch(ctx, spec, action, params)
		}

		if mcpCodeMode {
			fmt.Fprintf(os.Stderr, "Code mode enabled: execute_code tool available with typed API bindings\n")
		}

		return server.Run(cmd.Context())
	},
}

func init() {
	mcpServeCmd.Flags().BoolVar(&mcpToolsOnly, "tools-only", false, "Serve only the specified tools without clictl management commands")
	mcpServeCmd.Flags().BoolVar(&mcpNoSandbox, "no-sandbox", false, "Disable process sandboxing for MCP servers")
	mcpServeCmd.Flags().BoolVar(&mcpCodeMode, "code-mode", false, "Enable code mode: expose execute_code tool with typed API bindings")
	rootCmd.AddCommand(mcpServeCmd)
}
