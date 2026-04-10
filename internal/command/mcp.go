// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/mcp"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/registry"
)

var mcpFormat string

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP server discovery and management",
	Long:  "Discover, inspect, and interact with MCP (Model Context Protocol) servers.",
}

// resolveSpec loads and validates an MCP server spec by name.
func resolveMCPSpec(cmd *cobra.Command, serverName string) (*models.ToolSpec, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	cache := registry.NewCache(cfg.CacheDir)
	spec, err := registry.ResolveSpecVersion(cmd.Context(), serverName, "", cfg, cache, flagNoCache)
	if err != nil {
		return nil, fmt.Errorf("spec %q not found: %w", serverName, err)
	}
	if !spec.IsHTTP() && !spec.IsStdio() {
		return nil, fmt.Errorf("%q is not an MCP server (server type: %s)", serverName, spec.ServerType())
	}
	return spec, nil
}

// newMCPClient creates and returns an MCP client for the given spec.
func newMCPClient(spec *models.ToolSpec) (*mcp.Client, error) {
	client, err := mcp.NewClient(spec)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", spec.Name, err)
	}
	return client, nil
}

// outputFormatted writes data in the specified format (table, json, pretty).
func outputFormatted(format string, tableFunc func(io.Writer), data any) error {
	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		return enc.Encode(data)
	case "pretty":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	default: // table
		tableFunc(os.Stdout)
		return nil
	}
}

// truncateDesc limits a description string to maxLen characters.
func truncateDesc(desc string, maxLen int) string {
	if len(desc) > maxLen {
		return desc[:maxLen-3] + "..."
	}
	return desc
}

var mcpListToolsCmd = &cobra.Command{
	Use:   "list-tools <server>",
	Short: "List tools from an MCP server",
	Long: `Connect to an MCP server defined in the registry and list its available tools.
The tool list is filtered by the spec's expose/deny configuration.

  clictl mcp list-tools github-mcp
  clictl mcp list-tools filesystem-server`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		serverName := args[0]
		ctx := cmd.Context()

		spec, err := resolveMCPSpec(cmd, serverName)
		if err != nil {
			return err
		}

		client, err := newMCPClient(spec)
		if err != nil {
			return err
		}
		defer client.Close()

		tools, err := client.ListTools(ctx)
		if err != nil {
			return fmt.Errorf("listing tools from %s: %w", serverName, err)
		}

		// Filter tools by spec config
		var filtered []mcp.Tool
		for _, tool := range tools {
			if registry.IsMCPToolAllowed(spec, tool.Name) {
				filtered = append(filtered, tool)
			}
		}

		if len(filtered) == 0 {
			fmt.Printf("%s: no tools available (all filtered by spec config)\n", serverName)
			return nil
		}

		return outputFormatted(mcpFormat, func(w io.Writer) {
			fmt.Fprintf(w, "%s tools (%d available):\n\n", serverName, len(filtered))
			for _, tool := range filtered {
				destructive := ""
				for _, action := range spec.Actions {
					if action.Name == tool.Name && action.Mutable {
						destructive = " [mutable]"
						break
					}
				}
				fmt.Fprintf(w, "  %s%s\n", tool.Name, destructive)
				if tool.Description != "" {
					fmt.Fprintf(w, "    %s\n", tool.Description)
				}
				if len(tool.InputSchema.Properties) > 0 {
					// Server declared its schema - use it
					reqSet := make(map[string]bool, len(tool.InputSchema.Required))
					for _, r := range tool.InputSchema.Required {
						reqSet[r] = true
					}
					for name, prop := range tool.InputSchema.Properties {
						marker := ""
						if reqSet[name] {
							marker = " *"
						}
						desc := prop.Description
						if desc == "" && prop.Type != "" {
							desc = prop.Type
						}
						fmt.Fprintf(w, "    --%s%s  %s\n", name, marker, desc)
					}
				} else {
					// Server didn't declare schema - fall back to spec metadata
					for _, action := range spec.Actions {
						if action.Name == tool.Name && len(action.Params) > 0 {
							for _, p := range action.Params {
								marker := ""
								if p.Required {
									marker = " *"
								}
								desc := p.Description
								if desc == "" && p.Type != "" {
									desc = p.Type
								}
								fmt.Fprintf(w, "    --%s%s  %s\n", p.Name, marker, desc)
							}
							break
						}
					}
				}
				fmt.Fprintln(w)
			}
		}, filtered)
	},
}

var mcpListResourcesCmd = &cobra.Command{
	Use:   "list-resources <server>",
	Short: "List resources from an MCP server",
	Long: `Connect to an MCP server and list its available resources.

  clictl mcp list-resources github-mcp
  clictl mcp list-resources github-mcp --format json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		serverName := args[0]
		ctx := cmd.Context()

		spec, err := resolveMCPSpec(cmd, serverName)
		if err != nil {
			return err
		}

		client, err := newMCPClient(spec)
		if err != nil {
			return err
		}
		defer client.Close()

		resources, err := client.ListResources(ctx)
		if err != nil {
			return fmt.Errorf("listing resources from %s: %w", serverName, err)
		}

		if len(resources) == 0 {
			if !client.HasResources() {
				fmt.Printf("%s: server does not support resources\n", serverName)
			} else {
				fmt.Printf("%s: no resources available\n", serverName)
			}
			return nil
		}

		return outputFormatted(mcpFormat, func(w io.Writer) {
			fmt.Fprintf(w, "%s resources (%d available):\n\n", serverName, len(resources))
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			for _, r := range resources {
				mime := r.MimeType
				if mime == "" {
					mime = "-"
				}
				fmt.Fprintf(tw, "  %s\t%s\t%s\n", r.URI, truncateDesc(r.Name, 40), mime)
			}
			tw.Flush()
		}, resources)
	},
}

var mcpListTemplatesCmd = &cobra.Command{
	Use:   "list-templates <server>",
	Short: "List resource templates from an MCP server",
	Long: `Connect to an MCP server and list its resource templates.

  clictl mcp list-templates github-mcp
  clictl mcp list-templates github-mcp --format json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		serverName := args[0]
		ctx := cmd.Context()

		spec, err := resolveMCPSpec(cmd, serverName)
		if err != nil {
			return err
		}

		client, err := newMCPClient(spec)
		if err != nil {
			return err
		}
		defer client.Close()

		templates, err := client.ListResourceTemplates(ctx)
		if err != nil {
			return fmt.Errorf("listing resource templates from %s: %w", serverName, err)
		}

		if len(templates) == 0 {
			fmt.Printf("%s: no resource templates available\n", serverName)
			return nil
		}

		return outputFormatted(mcpFormat, func(w io.Writer) {
			fmt.Fprintf(w, "%s resource templates (%d available):\n\n", serverName, len(templates))
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			for _, t := range templates {
				fmt.Fprintf(tw, "  %s\t%s\n", t.URITemplate, truncateDesc(t.Name, 50))
			}
			tw.Flush()
		}, templates)
	},
}

var mcpReadCmd = &cobra.Command{
	Use:   "read <server> <uri>",
	Short: "Read a resource from an MCP server",
	Long: `Read the contents of a resource from an MCP server by URI.

  clictl mcp read github-mcp file:///repo/README.md
  clictl mcp read github-mcp file:///config.yaml --raw
  clictl mcp read github-mcp file:///data.json --output data.json`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		serverName := args[0]
		uri := args[1]
		ctx := cmd.Context()

		// Apply --param substitutions to the URI template
		paramPairs, _ := cmd.Flags().GetStringSlice("param")
		for _, pair := range paramPairs {
			parts := strings.SplitN(pair, "=", 2)
			if len(parts) == 2 {
				uri = strings.ReplaceAll(uri, "{"+parts[0]+"}", parts[1])
			}
		}

		spec, err := resolveMCPSpec(cmd, serverName)
		if err != nil {
			return err
		}

		client, err := newMCPClient(spec)
		if err != nil {
			return err
		}
		defer client.Close()

		result, err := client.ReadResource(ctx, uri)
		if err != nil {
			return fmt.Errorf("reading resource %s from %s: %w", uri, serverName, err)
		}

		rawMode, _ := cmd.Flags().GetBool("raw")
		outputFile, _ := cmd.Flags().GetString("output")

		if mcpFormat == "json" || mcpFormat == "pretty" {
			return outputFormatted(mcpFormat, nil, result)
		}

		// Collect all content
		var content strings.Builder
		for _, c := range result.Contents {
			if c.Text != "" {
				content.WriteString(c.Text)
			} else if c.Blob != "" {
				if rawMode {
					content.WriteString(c.Blob)
				} else {
					fmt.Fprintf(&content, "[binary: %s, %d bytes base64]\n", c.MimeType, len(c.Blob))
				}
			}
		}

		text := content.String()

		if outputFile != "" {
			return os.WriteFile(outputFile, []byte(text), 0644)
		}

		fmt.Print(text)
		return nil
	},
}

var mcpListPromptsCmd = &cobra.Command{
	Use:   "list-prompts <server>",
	Short: "List prompts from an MCP server",
	Long: `Connect to an MCP server and list its available prompts.

  clictl mcp list-prompts github-mcp
  clictl mcp list-prompts github-mcp --format json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		serverName := args[0]
		ctx := cmd.Context()

		spec, err := resolveMCPSpec(cmd, serverName)
		if err != nil {
			return err
		}

		client, err := newMCPClient(spec)
		if err != nil {
			return err
		}
		defer client.Close()

		prompts, err := client.ListPrompts(ctx)
		if err != nil {
			return fmt.Errorf("listing prompts from %s: %w", serverName, err)
		}

		if len(prompts) == 0 {
			fmt.Printf("%s: no prompts available\n", serverName)
			return nil
		}

		return outputFormatted(mcpFormat, func(w io.Writer) {
			fmt.Fprintf(w, "%s prompts (%d available):\n\n", serverName, len(prompts))
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			for _, p := range prompts {
				argNames := make([]string, 0, len(p.Arguments))
				for _, a := range p.Arguments {
					label := a.Name
					if a.Required {
						label += "*"
					}
					argNames = append(argNames, label)
				}
				argStr := ""
				if len(argNames) > 0 {
					argStr = " (" + strings.Join(argNames, ", ") + ")"
				}
				fmt.Fprintf(tw, "  %s\t%s%s\n", p.Name, truncateDesc(p.Description, 50), argStr)
			}
			tw.Flush()
		}, prompts)
	},
}

var mcpPromptCmd = &cobra.Command{
	Use:   "prompt <server> <name>",
	Short: "Get a prompt from an MCP server",
	Long: `Retrieve and display a prompt from an MCP server.

  clictl mcp prompt github-mcp summarize
  clictl mcp prompt github-mcp review --param language=go --param style=concise`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		serverName := args[0]
		promptName := args[1]
		ctx := cmd.Context()

		paramPairs, _ := cmd.Flags().GetStringSlice("param")
		promptArgs := make(map[string]string, len(paramPairs))
		for _, pair := range paramPairs {
			parts := strings.SplitN(pair, "=", 2)
			if len(parts) == 2 {
				promptArgs[parts[0]] = parts[1]
			}
		}

		spec, err := resolveMCPSpec(cmd, serverName)
		if err != nil {
			return err
		}

		client, err := newMCPClient(spec)
		if err != nil {
			return err
		}
		defer client.Close()

		result, err := client.GetPrompt(ctx, promptName, promptArgs)
		if err != nil {
			return fmt.Errorf("getting prompt %s from %s: %w", promptName, serverName, err)
		}

		if mcpFormat == "json" || mcpFormat == "pretty" {
			return outputFormatted(mcpFormat, nil, result)
		}

		if result.Description != "" {
			fmt.Printf("==> %s\n%s\n\n", promptName, result.Description)
		}

		for _, msg := range result.Messages {
			fmt.Printf("[%s]\n", msg.Role)
			if msg.Content.Text != "" {
				fmt.Println(msg.Content.Text)
			}
			fmt.Println()
		}

		return nil
	},
}

var mcpDiscoverCmd = &cobra.Command{
	Use:   "discover <url>",
	Short: "Discover tools from an ad-hoc MCP server",
	Long: `Connect to an HTTP MCP server URL and list its tools. Useful for testing
or private servers not in the registry.

  clictl mcp discover https://mcp.internal.corp/sse
  clictl mcp discover https://mcp.internal.corp/sse --generate-spec`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		serverURL := args[0]
		ctx := cmd.Context()

		generateSpec, _ := cmd.Flags().GetBool("generate-spec")

		// Create an ad-hoc spec for the HTTP server
		spec := &mcp.AdHocSpec{
			URL: serverURL,
		}

		client, err := mcp.NewAdHocHTTPClient(spec)
		if err != nil {
			return fmt.Errorf("connecting to %s: %w", serverURL, err)
		}
		defer client.Close()

		tools, err := client.ListTools(ctx)
		if err != nil {
			return fmt.Errorf("discovering tools from %s: %w", serverURL, err)
		}

		// Also discover resources and prompts for --generate-spec
		var resources []mcp.Resource
		var prompts []mcp.Prompt

		resources, _ = client.ListResources(ctx)
		prompts, _ = client.ListPrompts(ctx)

		if generateSpec {
			return generateSpecYAML(serverURL, tools, resources, prompts)
		}

		if mcpFormat == "json" || mcpFormat == "pretty" {
			result := map[string]any{
				"url":       serverURL,
				"tools":     tools,
				"resources": resources,
				"prompts":   prompts,
			}
			return outputFormatted(mcpFormat, nil, result)
		}

		fmt.Printf("Discovered %d tools", len(tools))
		if len(resources) > 0 {
			fmt.Printf(", %d resources", len(resources))
		}
		if len(prompts) > 0 {
			fmt.Printf(", %d prompts", len(prompts))
		}
		fmt.Printf(" from %s:\n\n", serverURL)

		if len(tools) > 0 {
			fmt.Printf("==> Tools\n")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, tool := range tools {
				fmt.Fprintf(w, "  %s\t%s\n", tool.Name, truncateDesc(tool.Description, 60))
			}
			w.Flush()
		}

		if len(resources) > 0 {
			fmt.Printf("\n==> Resources\n")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, r := range resources {
				fmt.Fprintf(w, "  %s\t%s\n", r.URI, truncateDesc(r.Name, 50))
			}
			w.Flush()
		}

		if len(prompts) > 0 {
			fmt.Printf("\n==> Prompts\n")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, p := range prompts {
				fmt.Fprintf(w, "  %s\t%s\n", p.Name, truncateDesc(p.Description, 50))
			}
			w.Flush()
		}

		return nil
	},
}

func generateSpecYAML(serverURL string, tools []mcp.Tool, resources []mcp.Resource, prompts []mcp.Prompt) error {
	// Build expose list from discovered tools
	expose := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		entry := map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
		}
		expose = append(expose, entry)
	}

	spec := map[string]any{
		"spec":        "1.0",
		"name":        "my-mcp-server",
		"description": fmt.Sprintf("MCP server at %s", serverURL),
		"version":     "1.0",
		"category":    "developer",
		"tags":        []string{"mcp"},
		"canonical":   serverURL,
		"server": map[string]any{
			"type": "http",
			"url":  serverURL,
		},
		"discover": true,
		"actions":  expose,
	}

	// Include discovered resources in the spec
	if len(resources) > 0 {
		resourceList := make([]map[string]any, 0, len(resources))
		for _, r := range resources {
			entry := map[string]any{
				"uri":  r.URI,
				"name": r.Name,
			}
			if r.Description != "" {
				entry["description"] = r.Description
			}
			if r.MimeType != "" {
				entry["mime_type"] = r.MimeType
			}
			resourceList = append(resourceList, entry)
		}
		spec["resources"] = map[string]any{
			"expose": resourceList,
		}
	}

	// Include discovered prompts in the spec
	if len(prompts) > 0 {
		promptList := make([]map[string]any, 0, len(prompts))
		for _, p := range prompts {
			entry := map[string]any{
				"name": p.Name,
			}
			if p.Description != "" {
				entry["description"] = p.Description
			}
			if len(p.Arguments) > 0 {
				params := make([]map[string]any, 0, len(p.Arguments))
				for _, a := range p.Arguments {
					param := map[string]any{"name": a.Name}
					if a.Description != "" {
						param["description"] = a.Description
					}
					if a.Required {
						param["required"] = true
					}
					params = append(params, param)
				}
				entry["params"] = params
			}
			promptList = append(promptList, entry)
		}
		spec["prompts"] = promptList
	}

	enc := yaml.NewEncoder(os.Stdout)
	enc.SetIndent(2)
	return enc.Encode(spec)
}

// mcpInfoCmd shows detailed info about an MCP server including resources and prompts.
// This extends the existing info command's output with MCP-specific sections (M1.35).
var mcpInfoCmd = &cobra.Command{
	Use:   "info <server>",
	Short: "Show detailed MCP server info including resources and prompts",
	Long: `Connect to an MCP server and show a summary of its tools, resources, and prompts.

  clictl mcp info github-mcp
  clictl mcp info github-mcp --format json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		serverName := args[0]
		ctx := cmd.Context()

		spec, err := resolveMCPSpec(cmd, serverName)
		if err != nil {
			return err
		}

		client, err := newMCPClient(spec)
		if err != nil {
			return err
		}
		defer client.Close()

		tools, toolsErr := client.ListTools(ctx)
		resources, resourcesErr := client.ListResources(ctx)
		templates, templatesErr := client.ListResourceTemplates(ctx)
		prompts, promptsErr := client.ListPrompts(ctx)

		if mcpFormat == "json" || mcpFormat == "pretty" {
			info := map[string]any{
				"name":  serverName,
				"tools": tools,
			}
			if resourcesErr == nil {
				info["resources"] = resources
			}
			if templatesErr == nil {
				info["resource_templates"] = templates
			}
			if promptsErr == nil {
				info["prompts"] = prompts
			}
			return outputFormatted(mcpFormat, nil, info)
		}

		fmt.Printf("==> %s\n", serverName)
		if spec.Description != "" {
			fmt.Printf("%s\n", spec.Description)
		}
		fmt.Println()

		// Tools
		if toolsErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not list tools: %v\n", toolsErr)
		} else {
			fmt.Printf("==> Tools (%d)\n", len(tools))
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, t := range tools {
				fmt.Fprintf(w, "  %s\t%s\n", t.Name, truncateDesc(t.Description, 60))
			}
			w.Flush()
			fmt.Println()
		}

		// Resources
		if resourcesErr == nil && len(resources) > 0 {
			fmt.Printf("==> Resources (%d)\n", len(resources))
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, r := range resources {
				fmt.Fprintf(w, "  %s\t%s\n", r.URI, truncateDesc(r.Name, 50))
			}
			w.Flush()
			fmt.Println()
		}

		// Resource Templates
		if templatesErr == nil && len(templates) > 0 {
			fmt.Printf("==> Resource Templates (%d)\n", len(templates))
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, t := range templates {
				fmt.Fprintf(w, "  %s\t%s\n", t.URITemplate, truncateDesc(t.Name, 50))
			}
			w.Flush()
			fmt.Println()
		}

		// Prompts
		if promptsErr == nil && len(prompts) > 0 {
			fmt.Printf("==> Prompts (%d)\n", len(prompts))
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, p := range prompts {
				fmt.Fprintf(w, "  %s\t%s\n", p.Name, truncateDesc(p.Description, 50))
			}
			w.Flush()
			fmt.Println()
		}

		return nil
	},
}

func init() {
	// --format flag on all mcp subcommands
	mcpCmd.PersistentFlags().StringVar(&mcpFormat, "format", "table", "Output format (table, json, pretty)")

	// discover
	mcpDiscoverCmd.Flags().Bool("generate-spec", false, "Generate a spec YAML skeleton from discovered tools")

	// read
	mcpReadCmd.Flags().Bool("raw", false, "Output raw content without formatting")
	mcpReadCmd.Flags().Bool("no-cache", false, "Bypass resource caching")
	mcpReadCmd.Flags().StringP("output", "O", "", "Write output to file instead of stdout")
	mcpReadCmd.Flags().StringSlice("param", nil, "URI template parameter (key=value), can be repeated")

	// prompt
	mcpPromptCmd.Flags().StringSlice("param", nil, "Prompt argument (key=value), can be repeated")

	mcpCmd.AddCommand(mcpListToolsCmd)
	mcpCmd.AddCommand(mcpListResourcesCmd)
	mcpCmd.AddCommand(mcpListTemplatesCmd)
	mcpCmd.AddCommand(mcpReadCmd)
	mcpCmd.AddCommand(mcpListPromptsCmd)
	mcpCmd.AddCommand(mcpPromptCmd)
	mcpCmd.AddCommand(mcpDiscoverCmd)
	mcpCmd.AddCommand(mcpInfoCmd)
	mcpCmd.AddCommand(newMCPShellCmd())
	rootCmd.AddCommand(mcpCmd)
}
