// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/executor"
	"github.com/clictl/cli/internal/mcp"
	"github.com/clictl/cli/internal/models"
)

// shellFormatMode controls how the shell renders output.
type shellFormatMode int

const (
	shellFormatPretty shellFormatMode = iota
	shellFormatJSON
	shellFormatText
)

// String returns the string representation.
func (m shellFormatMode) String() string {
	switch m {
	case shellFormatJSON:
		return "json"
	case shellFormatText:
		return "text"
	default:
		return "pretty"
	}
}

// shellState holds the live state of an interactive MCP shell session.
type shellState struct {
	client    *mcp.Client
	spec      *models.ToolSpec
	format    shellFormatMode
	toolNames map[string]bool
	tools     []mcp.Tool
	resources []mcp.Resource
	prompts   []mcp.Prompt
	history   []string
}

const shellHistoryFile = "mcp_shell_history"
const shellMaxHistory = 1000

func newMCPShellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell <server>",
		Short: "Interactive MCP server shell",
		Long: `Open an interactive REPL connected to an MCP server. Supports tool
calling, resource reading, and prompt retrieval with tab completion
and command history.

  clictl mcp shell github-mcp
  clictl mcp shell filesystem-server`,
		Args: cobra.ExactArgs(1),
		RunE: runMCPShell,
	}
}

func runMCPShell(cmd *cobra.Command, args []string) error {
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

	// Pre-fetch server capabilities for completion and shorthand
	tools, _ := client.ListTools(ctx)
	resources, _ := client.ListResources(ctx)
	prompts, _ := client.ListPrompts(ctx)

	toolNames := make(map[string]bool, len(tools))
	for _, t := range tools {
		toolNames[t.Name] = true
	}

	state := &shellState{
		client:    client,
		spec:      spec,
		format:    shellFormatPretty,
		toolNames: toolNames,
		tools:     tools,
		resources: resources,
		prompts:   prompts,
	}

	// Load history
	historyPath := filepath.Join(config.BaseDir(), shellHistoryFile)
	state.loadHistory(historyPath)

	fmt.Printf("Connected to %s (%d tools, %d resources, %d prompts)\n",
		serverName, len(tools), len(resources), len(prompts))
	fmt.Println("Type 'help' for available commands, 'exit' to quit.")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("mcp> ")

		if !scanner.Scan() {
			// EOF (Ctrl-D)
			fmt.Println()
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		state.history = append(state.history, line)

		parts := shellSplitLine(line)
		if len(parts) == 0 {
			continue
		}

		command := parts[0]
		cmdArgs := parts[1:]

		switch command {
		case "exit", "quit":
			state.saveHistory(historyPath)
			fmt.Println("Goodbye.")
			return nil

		case "help":
			shellPrintHelp(os.Stdout)

		case "tools":
			shellListTools(state)

		case "resources":
			shellListResources(state)

		case "prompts":
			shellListPrompts(state)

		case "call":
			if len(cmdArgs) < 1 {
				fmt.Println("Usage: call <tool> [json-args]")
				continue
			}
			toolName := cmdArgs[0]
			var jsonArgs string
			if len(cmdArgs) > 1 {
				jsonArgs = strings.Join(cmdArgs[1:], " ")
			}
			shellCallTool(ctx, state, toolName, jsonArgs)

		case "read":
			if len(cmdArgs) < 1 {
				fmt.Println("Usage: read <uri>")
				continue
			}
			shellReadResource(ctx, state, cmdArgs[0])

		case "prompt":
			if len(cmdArgs) < 1 {
				fmt.Println("Usage: prompt <name> [json-args]")
				continue
			}
			promptName := cmdArgs[0]
			var jsonArgs string
			if len(cmdArgs) > 1 {
				jsonArgs = strings.Join(cmdArgs[1:], " ")
			}
			shellGetPrompt(ctx, state, promptName, jsonArgs)

		case "format":
			if len(cmdArgs) < 1 {
				fmt.Printf("Current format: %s\n", state.format)
				fmt.Println("Usage: format [json|text|pretty]")
				continue
			}
			shellSetFormat(state, cmdArgs[0])

		default:
			// M5.3: Direct tool calling shorthand
			if state.toolNames[command] {
				var jsonArgs string
				if len(cmdArgs) > 0 {
					jsonArgs = strings.Join(cmdArgs, " ")
				}
				shellCallTool(ctx, state, command, jsonArgs)
			} else {
				fmt.Printf("Unknown command: %s\n", command)
				fmt.Println("Type 'help' for available commands.")
			}
		}
	}

	state.saveHistory(historyPath)
	return scanner.Err()
}

// shellSplitLine splits an input line into a command and arguments.
// JSON objects (delimited by braces) are kept as a single argument.
func shellSplitLine(line string) []string {
	var parts []string
	var current strings.Builder
	braceDepth := 0
	inQuotes := false
	quoteChar := byte(0)

	for i := 0; i < len(line); i++ {
		ch := line[i]

		if inQuotes {
			current.WriteByte(ch)
			if ch == quoteChar && (i == 0 || line[i-1] != '\\') {
				inQuotes = false
			}
			continue
		}

		if ch == '"' || ch == '\'' {
			inQuotes = true
			quoteChar = ch
			current.WriteByte(ch)
			continue
		}

		if ch == '{' || ch == '[' {
			braceDepth++
			current.WriteByte(ch)
			continue
		}

		if ch == '}' || ch == ']' {
			if braceDepth > 0 {
				braceDepth--
			}
			current.WriteByte(ch)
			continue
		}

		if ch == ' ' && braceDepth == 0 {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteByte(ch)
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

func shellPrintHelp(w io.Writer) {
	fmt.Fprintln(w, "Available commands:")
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  tools\t%s\n", "List available tools")
	fmt.Fprintf(tw, "  resources\t%s\n", "List available resources")
	fmt.Fprintf(tw, "  prompts\t%s\n", "List available prompts")
	fmt.Fprintf(tw, "  call <tool> [json]\t%s\n", "Call a tool with optional JSON arguments")
	fmt.Fprintf(tw, "  read <uri>\t%s\n", "Read a resource by URI")
	fmt.Fprintf(tw, "  prompt <name> [json]\t%s\n", "Get a prompt with optional JSON arguments")
	fmt.Fprintf(tw, "  format [json|text|pretty]\t%s\n", "Set output format (default: pretty)")
	fmt.Fprintf(tw, "  <tool_name> [json]\t%s\n", "Shorthand for 'call <tool_name> [json]'")
	fmt.Fprintf(tw, "  help\t%s\n", "Show this help message")
	fmt.Fprintf(tw, "  exit / quit\t%s\n", "Exit the shell")
	tw.Flush()
}

func shellListTools(state *shellState) {
	if len(state.tools) == 0 {
		fmt.Println("No tools available.")
		return
	}

	switch state.format {
	case shellFormatJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.Encode(state.tools)
	case shellFormatText:
		for _, t := range state.tools {
			fmt.Println(mcp.StripANSI(t.Name))
		}
	default:
		fmt.Printf("Tools (%d):\n", len(state.tools))
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, t := range state.tools {
			fmt.Fprintf(tw, "  %s\t%s\n",
				mcp.StripANSI(t.Name),
				mcp.StripANSI(truncateDesc(t.Description, 60)))
		}
		tw.Flush()
	}
}

func shellListResources(state *shellState) {
	if len(state.resources) == 0 {
		fmt.Println("No resources available.")
		return
	}

	switch state.format {
	case shellFormatJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.Encode(state.resources)
	case shellFormatText:
		for _, r := range state.resources {
			fmt.Println(mcp.StripANSI(r.URI))
		}
	default:
		fmt.Printf("Resources (%d):\n", len(state.resources))
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, r := range state.resources {
			mime := r.MimeType
			if mime == "" {
				mime = "-"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\n",
				mcp.StripANSI(r.URI),
				mcp.StripANSI(truncateDesc(r.Name, 40)),
				mime)
		}
		tw.Flush()
	}
}

func shellListPrompts(state *shellState) {
	if len(state.prompts) == 0 {
		fmt.Println("No prompts available.")
		return
	}

	switch state.format {
	case shellFormatJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.Encode(state.prompts)
	case shellFormatText:
		for _, p := range state.prompts {
			fmt.Println(mcp.StripANSI(p.Name))
		}
	default:
		fmt.Printf("Prompts (%d):\n", len(state.prompts))
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, p := range state.prompts {
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
			fmt.Fprintf(tw, "  %s\t%s%s\n",
				mcp.StripANSI(p.Name),
				mcp.StripANSI(truncateDesc(p.Description, 50)),
				argStr)
		}
		tw.Flush()
	}
}

func shellCallTool(ctx context.Context, state *shellState, toolName string, jsonArgs string) {
	args := make(map[string]any)
	if jsonArgs != "" {
		if err := json.Unmarshal([]byte(jsonArgs), &args); err != nil {
			fmt.Printf("Error: invalid JSON arguments: %v\n", err)
			return
		}
	}

	// M5.7: If spec has transforms, use the executor which applies them.
	if state.spec != nil && len(state.spec.Transforms) > 0 {
		result, err := executor.DispatchMCPWithClient(ctx, state.spec, toolName, args, state.client)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		shellPrintOutput(state, string(result))
		return
	}

	result, err := state.client.CallTool(ctx, toolName, args)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if result.IsError {
		fmt.Printf("Tool error: %s\n", mcp.StripANSI(extractCallToolText(result)))
		return
	}

	shellPrintOutput(state, extractCallToolText(result))
}

// extractCallToolText extracts displayable text from an MCP CallToolResult.
func extractCallToolText(result *mcp.CallToolResult) string {
	var parts []string
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		case "image":
			mime := block.MimeType
			if mime == "" {
				mime = "image/png"
			}
			if block.Data != "" {
				parts = append(parts, fmt.Sprintf("[image: %s, %d bytes base64]", mime, len(block.Data)))
			}
		case "audio":
			mime := block.MimeType
			if mime == "" {
				mime = "audio/wav"
			}
			if block.Data != "" {
				parts = append(parts, fmt.Sprintf("[audio: %s, %d bytes base64]", mime, len(block.Data)))
			}
		case "resource_link":
			if block.URI != "" {
				parts = append(parts, fmt.Sprintf("[resource: %s]", block.URI))
			}
		case "embedded_resource":
			if block.Resource != nil {
				if block.Resource.Text != "" {
					parts = append(parts, block.Resource.Text)
				} else if block.Resource.Blob != "" {
					mime := block.Resource.MimeType
					if mime == "" {
						mime = "application/octet-stream"
					}
					parts = append(parts, fmt.Sprintf("[embedded resource: %s, %s, %d bytes]",
						block.Resource.URI, mime, len(block.Resource.Blob)))
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

func shellReadResource(ctx context.Context, state *shellState, uri string) {
	result, err := state.client.ReadResource(ctx, uri)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	var content strings.Builder
	for _, c := range result.Contents {
		if c.Text != "" {
			content.WriteString(c.Text)
		} else if c.Blob != "" {
			fmt.Fprintf(&content, "[binary: %s, %d bytes base64]\n", c.MimeType, len(c.Blob))
		}
	}

	shellPrintOutput(state, content.String())
}

func shellGetPrompt(ctx context.Context, state *shellState, promptName string, jsonArgs string) {
	args := make(map[string]string)
	if jsonArgs != "" {
		if err := json.Unmarshal([]byte(jsonArgs), &args); err != nil {
			fmt.Printf("Error: invalid JSON arguments: %v\n", err)
			return
		}
	}

	result, err := state.client.GetPrompt(ctx, promptName, args)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	switch state.format {
	case shellFormatJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.Encode(result)
	case shellFormatText:
		for _, msg := range result.Messages {
			if msg.Content.Text != "" {
				fmt.Println(mcp.StripANSI(msg.Content.Text))
			}
		}
	default:
		if result.Description != "" {
			fmt.Printf("==> %s\n%s\n\n", promptName, mcp.StripANSI(result.Description))
		}
		for _, msg := range result.Messages {
			fmt.Printf("[%s]\n", msg.Role)
			if msg.Content.Text != "" {
				fmt.Println(mcp.StripANSI(msg.Content.Text))
			}
			fmt.Println()
		}
	}
}

func shellSetFormat(state *shellState, format string) {
	switch format {
	case "json":
		state.format = shellFormatJSON
		fmt.Println("Output format: json")
	case "text":
		state.format = shellFormatText
		fmt.Println("Output format: text")
	case "pretty":
		state.format = shellFormatPretty
		fmt.Println("Output format: pretty")
	default:
		fmt.Printf("Unknown format: %s (use json, text, or pretty)\n", format)
	}
}

func shellPrintOutput(state *shellState, text string) {
	// M5.6: Sanitize ANSI escape sequences from server output
	text = mcp.StripANSI(text)

	switch state.format {
	case shellFormatJSON:
		var data any
		if err := json.Unmarshal([]byte(text), &data); err == nil {
			enc := json.NewEncoder(os.Stdout)
			enc.Encode(data)
		} else {
			enc := json.NewEncoder(os.Stdout)
			enc.Encode(text)
		}
	case shellFormatText:
		fmt.Println(text)
	default:
		var data any
		if err := json.Unmarshal([]byte(text), &data); err == nil {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(data)
		} else {
			fmt.Println(text)
		}
	}
}

// shellCompleter provides tab completion for the MCP shell.
type shellCompleter struct {
	state *shellState
}

func newShellCompleter(state *shellState) *shellCompleter {
	return &shellCompleter{state: state}
}

// Complete returns completion suggestions for the given input line.
func (c *shellCompleter) Complete(line string) []string {
	parts := strings.SplitN(line, " ", 2)
	prefix := parts[0]

	// If no space yet, complete command names and tool names
	if len(parts) == 1 {
		return c.completeCommands(prefix)
	}

	argPrefix := ""
	if len(parts) > 1 {
		argPrefix = strings.TrimSpace(parts[1])
	}

	switch prefix {
	case "call":
		return c.completeToolNames(argPrefix)
	case "read":
		return c.completeResourceURIs(argPrefix)
	case "prompt":
		return c.completePromptNames(argPrefix)
	case "format":
		return c.completeFormats(argPrefix)
	}

	return nil
}

func (c *shellCompleter) completeCommands(prefix string) []string {
	commands := []string{"tools", "resources", "prompts", "call", "read", "prompt", "format", "help", "exit", "quit"}

	// Also add tool names for shorthand completion
	for _, t := range c.state.tools {
		commands = append(commands, t.Name)
	}

	sort.Strings(commands)

	if prefix == "" {
		return commands
	}

	var matches []string
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, prefix) {
			matches = append(matches, cmd)
		}
	}
	return matches
}

func (c *shellCompleter) completeToolNames(prefix string) []string {
	var matches []string
	for _, t := range c.state.tools {
		if prefix == "" || strings.HasPrefix(t.Name, prefix) {
			matches = append(matches, t.Name)
		}
	}
	sort.Strings(matches)
	return matches
}

func (c *shellCompleter) completeResourceURIs(prefix string) []string {
	var matches []string
	for _, r := range c.state.resources {
		if prefix == "" || strings.HasPrefix(r.URI, prefix) {
			matches = append(matches, r.URI)
		}
	}
	sort.Strings(matches)
	return matches
}

func (c *shellCompleter) completePromptNames(prefix string) []string {
	var matches []string
	for _, p := range c.state.prompts {
		if prefix == "" || strings.HasPrefix(p.Name, prefix) {
			matches = append(matches, p.Name)
		}
	}
	sort.Strings(matches)
	return matches
}

func (c *shellCompleter) completeFormats(prefix string) []string {
	formats := []string{"json", "text", "pretty"}
	if prefix == "" {
		return formats
	}
	var matches []string
	for _, f := range formats {
		if strings.HasPrefix(f, prefix) {
			matches = append(matches, f)
		}
	}
	return matches
}

// M5.5: History persistence

func (state *shellState) loadHistory(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			state.history = append(state.history, line)
		}
	}
}

func (state *shellState) saveHistory(path string) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}

	// Trim history to max size
	entries := state.history
	if len(entries) > shellMaxHistory {
		entries = entries[len(entries)-shellMaxHistory:]
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return
	}
	defer f.Close()

	for _, line := range entries {
		fmt.Fprintln(f, line)
	}
}
