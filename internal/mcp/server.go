// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.

// MCP stdio server implementation. Serves tool actions and management commands
// (search, inspect, install, run, code) via JSON-RPC over stdin/stdout.
// In global mode, management tools let agents discover and install new tools dynamically.
// In tools-only mode, only pre-loaded tool actions are exposed.

package mcp

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/clictl/cli/internal/codegen"
	"github.com/clictl/cli/internal/codemode"
	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/memory"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/vault"
)

// DispatchFunc is the function signature for dispatching tool actions.
// This avoids a direct import of the executor package (preventing import cycles).
// Arguments are map[string]any to support non-string MCP argument values.
type DispatchFunc func(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]any) ([]byte, error)

// execCommand wraps os/exec.Command for testability.
var execCommand = func(name string, args ...string) *osexec.Cmd {
	return osexec.Command(name, args...)
}

// Server is an MCP stdio server that exposes tool specs as MCP tools.
type Server struct {
	specs      []*models.ToolSpec
	writer     io.Writer
	GlobalMode bool         // When true, expose clictl management commands as MCP tools
	CodeMode   bool         // When true, expose execute_code tool for code mode
	Dispatch   DispatchFunc // Callback for dispatching tool actions (set by command layer)
	pool       *Pool        // Connection pool for proxied MCP servers

	// M1.20: Session-random boundary token for content boundary markers.
	// Generated once at server init, used to wrap proxied content.
	boundaryToken string

	// M1.17: Resource cache with TTL keyed by resource URI.
	resourceCache   map[string]*resourceCacheEntry
	resourceCacheMu sync.RWMutex
}

// resourceCacheEntry stores a cached resource read result with expiration.
type resourceCacheEntry struct {
	contents  []ResourceContent
	expiresAt time.Time
}

// defaultResourceCacheTTL is the default TTL for cached resource reads.
const defaultResourceCacheTTL = 5 * time.Minute

// NewServer creates a new MCP server with the given tool specs.
func NewServer(specs []*models.ToolSpec) *Server {
	// M1.20: Generate a session-random boundary token
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		// Fallback to a fixed token if crypto/rand fails (should not happen)
		tokenBytes = []byte("clictl-boundary-fallback!")
	}
	return &Server{
		specs:         specs,
		writer:        os.Stdout,
		pool:          NewPool(),
		boundaryToken: hex.EncodeToString(tokenBytes),
		resourceCache: make(map[string]*resourceCacheEntry),
	}
}

// Run starts the MCP server, reading JSON-RPC requests from stdin.
func (s *Server) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)
	// MCP uses newline-delimited JSON
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.sendError(nil, -32700, "Parse error")
			continue
		}

		s.handleRequest(ctx, &req)
	}

	return scanner.Err()
}

func (s *Server) handleRequest(ctx context.Context, req *Request) {
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "initialized":
		// Notification, no response needed
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolsCall(ctx, req)
	case "resources/list":
		s.handleResourcesList(ctx, req)
	case "resources/read":
		s.handleResourcesRead(ctx, req)
	case "resources/templates/list":
		s.handleResourcesTemplatesList(ctx, req)
	case "prompts/list":
		s.handlePromptsList(ctx, req)
	case "prompts/get":
		s.handlePromptsGet(ctx, req)
	case "ping":
		s.sendResult(req.ID, map[string]interface{}{})
	default:
		s.sendError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func (s *Server) handleInitialize(req *Request) {
	caps := Capability{
		Tools: &ToolsCapability{},
	}

	// M1.13: Announce resources capability when any spec has resources or
	// when in global mode (management resources are always available).
	if s.GlobalMode || s.hasResources() {
		caps.Resources = &ResourcesCapability{}
	}

	// M1.13: Announce prompts capability when any spec has prompts.
	if s.hasPrompts() {
		caps.Prompts = &PromptsCapability{}
	}

	result := InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities:    caps,
		ServerInfo: Info{
			Name:    "clictl",
			Version: "1.0.0",
		},
	}
	s.sendResult(req.ID, result)
}

// hasResources returns true if any spec declares resources.
func (s *Server) hasResources() bool {
	for _, spec := range s.specs {
		if spec.Resources != nil || spec.Discover {
			return true
		}
	}
	return false
}

// hasPrompts returns true if any spec declares prompts.
func (s *Server) hasPrompts() bool {
	for _, spec := range s.specs {
		if len(spec.Prompts.Items) > 0 || spec.Discover {
			return true
		}
	}
	return false
}

func (s *Server) handleToolsList(req *Request) {
	// Use a background context for MCP proxy connections since tools/list
	// is not tied to a specific request timeout.
	ctx := context.Background()
	var tools []Tool

	// Add management tools in global mode
	if s.GlobalMode {
		tools = append(tools, s.managementTools()...)
	}

	// Add spec-based tools
	for _, spec := range s.specs {
		if spec.Discover {
			// Proxy: connect to the MCP server and list its tools
			mcpTools := s.proxyListTools(ctx, spec)
			tools = append(tools, mcpTools...)
		} else {
			for _, action := range spec.Actions {
				tool := Tool{
					Name:        fmt.Sprintf("%s_%s", spec.Name, action.Name),
					Description: fmt.Sprintf("[%s] %s", spec.Name, action.Description),
					InputSchema: buildInputSchema(action),
				}
				tools = append(tools, tool)
			}
		}
	}
	// Add code mode execute_code tool with type definitions
	if s.CodeMode {
		typeDecls := codegen.GenerateTypeScriptDeclarations(s.specs)
		description := "Execute JavaScript code with access to API clients. " +
			"Write code that calls the typed functions below. " +
			"Use console.log() to output results.\n\n" +
			"```typescript\n" + typeDecls + "```"

		tools = append(tools, Tool{
			Name:        "execute_code",
			Description: description,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"code": {
						Type:        "string",
						Description: "JavaScript code to execute. API clients are available as global objects.",
					},
				},
				Required: []string{"code"},
			},
		})
	}

	s.sendResult(req.ID, ToolsListResult{Tools: tools})
}

// proxyListTools connects to an MCP server and returns its tools,
// prefixed by the spec name and filtered by the spec's tools config.
func (s *Server) proxyListTools(ctx context.Context, spec *models.ToolSpec) []Tool {
	client, err := s.pool.Get(ctx, spec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not connect to MCP server %s: %v\n", spec.Name, err)
		return nil
	}
	defer s.pool.Release(spec.Name)

	serverTools, err := client.ListTools(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not list tools from %s: %v\n", spec.Name, err)
		return nil
	}

	// Determine prefix
	prefix := spec.Name + "_"

	var filtered []Tool
	for _, tool := range serverTools {
		// Apply deny filter from spec
		if isDenied(spec.Deny, tool.Name) {
			continue
		}

		// Apply allow filter from spec (if set, only matching tools pass)
		if len(spec.Allow) > 0 {
			if !isAllowed(spec.Allow, tool.Name) {
				continue
			}
		}

		// If explicit actions are defined and discover is also on,
		// override description from matching static actions.
		for _, action := range spec.Actions {
			if action.Name == tool.Name && action.Description != "" {
				tool.Description = action.Description
				break
			}
		}

		tool.Name = prefix + tool.Name
		tool.Description = StripANSI(fmt.Sprintf("[%s] %s", spec.Name, tool.Description))
		filtered = append(filtered, tool)
	}
	return filtered
}

func (s *Server) managementTools() []Tool {
	tools := []Tool{
		{
			Name:        "clictl_search",
			Description: "Search the clictl registry for tools by keyword. Returns matching tool names, categories, and descriptions. Use this BEFORE writing code to call any external API or service.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"query": {Type: "string", Description: "Search query (e.g., 'weather', 'github', 'translate')"},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "clictl_list",
			Description: "List all available tools in the registry, optionally filtered by category.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"category": {Type: "string", Description: "Filter by category (e.g., 'ai', 'developer', 'communication'). Omit for all."},
				},
			},
		},
		{
			Name:        "clictl_inspect",
			Description: "Get tool details including auth requirements and available actions. Show detailed information about a tool including all actions, parameters, auth requirements, and usage examples.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"tool": {Type: "string", Description: "Tool name to inspect (e.g., 'openweathermap', 'github')"},
				},
				Required: []string{"tool"},
			},
		},
		{
			Name:        "clictl_install",
			Description: "Install a tool by name. Downloads the tool, verifies integrity, and installs to the appropriate location. Use clictl_search first to find tools.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"tool":    {Type: "string", Description: "Tool name to install (e.g., 'xlsx', 'github-mcp')"},
					"version": {Type: "string", Description: "Version to install (optional, defaults to latest)"},
					"global":  {Type: "boolean", Description: "Install globally instead of project-scoped (default: false)"},
				},
				Required: []string{"tool"},
			},
		},
		{
			Name:        "clictl_run",
			Description: "Execute a tool action. Check deps first for auth requirements. Run a tool action with the given parameters. Use clictl_inspect first to see available actions and required parameters.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"tool":   {Type: "string", Description: "Tool name (e.g., 'openweathermap')"},
					"action": {Type: "string", Description: "Action name (e.g., 'current')"},
					"params": {Type: "string", Description: "Parameters as space-separated --key value pairs (e.g., '--q London --units metric')"},
				},
				Required: []string{"tool", "action"},
			},
		},
	}

	// clictl_code: JS sandbox with management bindings
	tools = append(tools, Tool{
		Name: "clictl_code",
		Description: `Execute JavaScript code with clictl management bindings. Use for multi-step workflows that chain tool calls, aggregate results, or apply logic.

Available API:
  clictl.search(query)                          // search registry, returns JSON array
  clictl.inspect(tool)                          // get tool details + actions
  clictl.run(tool, action, {param: value})      // execute action, returns raw JSON
  clictl.list()                                 // list all tools
  clictl.list({category: "developer"})          // list by category

Use console.log() to return output. All API calls are synchronous.

Example:
  const weather = clictl.run("open-meteo", "current", {q: "London"});
  console.log(JSON.stringify(weather, null, 2));`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"code": {
					Type:        "string",
					Description: "JavaScript code to execute. The `clictl` object is available with search, inspect, run, and list methods.",
				},
			},
			Required: []string{"code"},
		},
	})

	return tools
}

func (s *Server) handleToolsCall(ctx context.Context, req *Request) {
	// Parse params
	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		s.sendError(req.ID, -32602, "Invalid params")
		return
	}

	var params CallToolParams
	if err := json.Unmarshal(paramsJSON, &params); err != nil {
		s.sendError(req.ID, -32602, "Invalid params")
		return
	}

	// Handle code mode execute_code tool
	if s.CodeMode && params.Name == "execute_code" {
		s.handleCodeModeCall(ctx, req, params)
		return
	}

	// Handle management tools in global mode
	if s.GlobalMode && strings.HasPrefix(params.Name, "clictl_") {
		s.handleManagementCall(ctx, req, params)
		return
	}

	// Parse tool name: specName_actionName (or specName_mcpToolName for proxied MCP)
	parts := strings.SplitN(params.Name, "_", 2)
	if len(parts) != 2 {
		s.sendError(req.ID, -32602, fmt.Sprintf("Invalid tool name: %s", params.Name))
		return
	}
	specName, toolOrAction := parts[0], parts[1]

	// Find spec
	var spec *models.ToolSpec
	for _, sp := range s.specs {
		if sp.Name == specName {
			spec = sp
			break
		}
		// Also check discover-mode specs with name prefix
		if sp.Discover {
			prefix := sp.Name + "_"
			if strings.HasPrefix(params.Name, prefix) {
				spec = sp
				toolOrAction = strings.TrimPrefix(params.Name, prefix)
				break
			}
		}
	}

	if spec == nil {
		s.sendError(req.ID, -32602, fmt.Sprintf("Tool spec not found: %s", specName))
		return
	}

	// MCP proxy: forward the call to the MCP server
	if spec.Discover {
		s.proxyToolCall(ctx, req, spec, toolOrAction, params)
		return
	}

	// Standard spec: find action and dispatch
	var action *models.Action
	for i := range spec.Actions {
		if spec.Actions[i].Name == toolOrAction {
			action = &spec.Actions[i]
			break
		}
	}
	if action == nil {
		s.sendError(req.ID, -32602, fmt.Sprintf("Action not found: %s.%s", specName, toolOrAction))
		return
	}

	// Execute via the dispatch callback
	if s.Dispatch == nil {
		s.sendError(req.ID, -32603, "No dispatch function configured")
		return
	}
	result, err := s.Dispatch(ctx, spec, action, params.Arguments)
	if err != nil {
		s.sendResult(req.ID, CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error: %s", err.Error())}},
			IsError: true,
		})
		return
	}

	s.sendResult(req.ID, CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: StripANSI(string(result))}},
	})
}

// proxyToolCall forwards a tools/call request to a proxied MCP server.
func (s *Server) proxyToolCall(ctx context.Context, req *Request, spec *models.ToolSpec, toolName string, params CallToolParams) {
	client, err := s.pool.Get(ctx, spec)
	if err != nil {
		s.sendResult(req.ID, CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error connecting to %s: %s", spec.Name, err.Error())}},
			IsError: true,
		})
		return
	}
	defer s.pool.Release(spec.Name)

	result, err := client.CallTool(ctx, toolName, params.Arguments)
	if err != nil {
		s.sendResult(req.ID, CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error: %s", err.Error())}},
			IsError: true,
		})
		return
	}

	// Sanitize ANSI escape sequences from proxied tool results
	for i := range result.Content {
		result.Content[i].Text = StripANSI(result.Content[i].Text)
	}

	s.sendResult(req.ID, *result)
}

// argString extracts a string value from a map[string]any argument map.
func argString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// handleCodeModeCall executes JavaScript code with API client bindings.
func (s *Server) handleCodeModeCall(ctx context.Context, req *Request, params CallToolParams) {
	code, _ := params.Arguments["code"].(string)
	if code == "" {
		s.sendResult(req.ID, CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Error: code parameter is required"}},
			IsError: true,
		})
		return
	}

	// Create a dispatch function that wraps our MCP dispatch
	dispatch := func(dCtx context.Context, spec *models.ToolSpec, action *models.Action, dParams map[string]any) ([]byte, error) {
		if s.Dispatch == nil {
			return nil, fmt.Errorf("no dispatch function configured")
		}
		return s.Dispatch(dCtx, spec, action, dParams)
	}

	result, err := codemode.Execute(ctx, code, s.specs, dispatch)
	if err != nil {
		s.sendResult(req.ID, CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Sandbox error: %s", err.Error())}},
			IsError: true,
		})
		return
	}

	// Combine output and error
	var text string
	if result.Output != "" {
		text = result.Output
	}
	if result.Error != "" {
		if text != "" {
			text += "\n"
		}
		text += "Error: " + result.Error
	}
	if text == "" {
		text = "(no output - use console.log() to return results)"
	}

	s.sendResult(req.ID, CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: text}},
		IsError: result.Error != "",
	})
}

// handleCodeManagementCall runs JS code with clictl management bindings
// (search, inspect, run, list). Unlike execute_code which uses spec-specific
// bindings, this provides the clictl namespace for dynamic tool discovery.
func (s *Server) handleCodeManagementCall(ctx context.Context, req *Request, code string) {
	exe, err := os.Executable()
	if err != nil {
		exe = "clictl"
	}

	cliFn := codemode.DefaultCLIFunc(exe, os.Environ())
	result, err := codemode.ExecuteWithManagement(ctx, code, cliFn)
	if err != nil {
		s.sendResult(req.ID, CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Sandbox error: %s", err.Error())}},
			IsError: true,
		})
		return
	}

	var text string
	if result.Output != "" {
		text = result.Output
	}
	if result.Error != "" {
		if text != "" {
			text += "\n"
		}
		text += "Error: " + result.Error
	}
	if text == "" {
		text = "(no output - use console.log() to return results)"
	}

	s.sendResult(req.ID, CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: text}},
		IsError: result.Error != "",
	})
}

func (s *Server) handleManagementCall(ctx context.Context, req *Request, params CallToolParams) {
	args := params.Arguments

	switch params.Name {
	case "clictl_search":
		query := argString(args, "query")
		if query == "" {
			s.sendError(req.ID, -32602, "Missing required parameter: query")
			return
		}
		s.runCLI(req.ID, "search", query)

	case "clictl_list":
		category := argString(args, "category")
		if category != "" {
			s.runCLI(req.ID, "list", "--category", category)
		} else {
			s.runCLI(req.ID, "list")
		}

	case "clictl_inspect":
		tool := argString(args, "tool")
		if tool == "" {
			s.sendError(req.ID, -32602, "Missing required parameter: tool")
			return
		}
		s.runCLI(req.ID, "inspect", tool)

	case "clictl_install":
		tool := argString(args, "tool")
		if tool == "" {
			s.sendError(req.ID, -32602, "Missing required parameter: tool")
			return
		}
		cmdArgs := []string{"install", tool}
		if v := argString(args, "version"); v != "" {
			cmdArgs = []string{"install", tool + "@" + v}
		}
		if argString(args, "global") == "true" {
			cmdArgs = append(cmdArgs, "--global")
		}
		s.runCLI(req.ID, cmdArgs...)

	case "clictl_run":
		tool := argString(args, "tool")
		action := argString(args, "action")
		if tool == "" || action == "" {
			s.sendError(req.ID, -32602, "Missing required parameters: tool and action")
			return
		}
		cmdArgs := []string{"run", tool, action}
		if paramStr := argString(args, "params"); paramStr != "" {
			cmdArgs = append(cmdArgs, strings.Fields(paramStr)...)
		}
		s.runCLI(req.ID, cmdArgs...)

	case "clictl_code":
		code := argString(args, "code")
		if code == "" {
			s.sendError(req.ID, -32602, "Missing required parameter: code")
			return
		}
		s.handleCodeManagementCall(ctx, req, code)

	default:
		s.sendError(req.ID, -32601, fmt.Sprintf("Unknown management tool: %s", params.Name))
	}
}

func (s *Server) runCLI(id interface{}, args ...string) {
	exe, err := os.Executable()
	if err != nil {
		exe = "clictl"
	}

	cmd := execCommand(exe, args...)
	output, err := cmd.CombinedOutput()
	sanitized := StripANSI(string(output))
	if err != nil {
		s.sendResult(id, CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: sanitized}},
			IsError: true,
		})
		return
	}

	s.sendResult(id, CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: sanitized}},
	})
}

func (s *Server) sendResult(id interface{}, result interface{}) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(s.writer, string(data))
}

func (s *Server) sendError(id interface{}, code int, message string) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &Error{Code: code, Message: message},
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(s.writer, string(data))
}

// isDenied checks if a tool name matches any deny patterns.
func isDenied(patterns []string, name string) bool {
	return matchesAny(name, patterns)
}

// isAllowed checks if a tool name matches any allow patterns.
func isAllowed(patterns []string, name string) bool {
	return matchesAny(name, patterns)
}

// ---------------------------------------------------------------------------
// Resource handlers (M1.11, M1.14-M1.18)
// ---------------------------------------------------------------------------

// handleResourcesList returns all available resources from specs and management.
func (s *Server) handleResourcesList(ctx context.Context, req *Request) {
	var resources []Resource

	// M1.18: Management resources in global mode
	if s.GlobalMode {
		resources = append(resources, s.managementResources()...)
	}

	for _, spec := range s.specs {
		if spec.Discover {
			// M1.14: Proxy resources/list to upstream MCP server
			proxyRes := s.proxyResourcesList(ctx, spec)
			resources = append(resources, proxyRes...)
		} else if spec.Resources != nil {
			// M1.15: Static resources from spec config
			staticRes := s.staticResourcesList(spec)
			resources = append(resources, staticRes...)
		}
	}

	s.sendResult(req.ID, ResourcesListResult{Resources: resources})
}

// handleResourcesTemplatesList returns resource templates from specs.
func (s *Server) handleResourcesTemplatesList(ctx context.Context, req *Request) {
	var templates []ResourceTemplate

	// M1.18: Management resource templates in global mode
	if s.GlobalMode {
		templates = append(templates, ResourceTemplate{
			URITemplate: "clictl://memory/{tool}",
			Name:        "Tool Memory",
			Description: "Remembered notes for a specific tool",
			MimeType:    "application/json",
		})
	}

	for _, spec := range s.specs {
		if spec.Discover {
			proxyTpl := s.proxyResourcesTemplatesList(ctx, spec)
			templates = append(templates, proxyTpl...)
		}
	}

	s.sendResult(req.ID, ResourceTemplatesListResult{ResourceTemplates: templates})
}

// handleResourcesRead reads a specific resource by URI.
func (s *Server) handleResourcesRead(ctx context.Context, req *Request) {
	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		s.sendError(req.ID, -32602, "Invalid params")
		return
	}

	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(paramsJSON, &params); err != nil {
		s.sendError(req.ID, -32602, "Invalid params")
		return
	}

	if params.URI == "" {
		s.sendError(req.ID, -32602, "Missing required parameter: uri")
		return
	}

	// M1.17: Check cache first
	if cached := s.getCachedResource(params.URI); cached != nil {
		s.sendResult(req.ID, ResourceReadResult{Contents: cached})
		return
	}

	// M1.18: Handle management resource URIs
	if strings.HasPrefix(params.URI, "clictl://") {
		contents := s.readManagementResource(params.URI)
		if contents != nil {
			s.cacheResource(params.URI, contents)
			s.sendResult(req.ID, ResourceReadResult{Contents: contents})
			return
		}
		s.sendError(req.ID, -32602, fmt.Sprintf("Unknown management resource: %s", params.URI))
		return
	}

	// Find the spec that owns this resource
	for _, spec := range s.specs {
		if spec.Discover {
			// M1.14: Proxy resource read to upstream
			result := s.proxyResourceRead(ctx, spec, params.URI)
			if result != nil {
				// M1.16: Apply resource transforms if configured
				result = s.applyResourceTransforms(spec, params.URI, result)
				// M1.20: Wrap with boundary markers
				result = s.wrapWithBoundary(spec.Name, result)
				s.cacheResource(params.URI, result)
				s.sendResult(req.ID, ResourceReadResult{Contents: result})
				return
			}
		}
	}

	s.sendError(req.ID, -32602, fmt.Sprintf("Resource not found: %s", params.URI))
}

// managementResources returns the built-in clictl management resources.
func (s *Server) managementResources() []Resource {
	return []Resource{
		{
			URI:         "clictl://installed",
			Name:        "Installed Tools",
			Description: "List of tools currently installed via clictl",
			MimeType:    "text/plain",
		},
		{
			URI:         "clictl://vault/keys",
			Name:        "Vault Keys",
			Description: "List of secret key names stored in the clictl vault (values not exposed)",
			MimeType:    "application/json",
		},
		{
			URI:         "clictl://workspace",
			Name:        "Workspace Info",
			Description: "Current workspace configuration and active workspace slug",
			MimeType:    "application/json",
		},
	}
}

// readManagementResource reads a clictl:// management resource.
func (s *Server) readManagementResource(uri string) []ResourceContent {
	switch {
	case uri == "clictl://installed":
		return s.readInstalledResource()
	case uri == "clictl://vault/keys":
		return s.readVaultKeysResource()
	case uri == "clictl://workspace":
		return s.readWorkspaceResource()
	case strings.HasPrefix(uri, "clictl://memory/"):
		toolName := strings.TrimPrefix(uri, "clictl://memory/")
		return s.readMemoryResource(toolName)
	}
	return nil
}

func (s *Server) readInstalledResource() []ResourceContent {
	installedPath := filepath.Join(config.BaseDir(), "installed.yaml")
	data, err := os.ReadFile(installedPath)
	if err != nil {
		return []ResourceContent{{URI: "clictl://installed", MimeType: "text/plain", Text: "No tools installed."}}
	}
	return []ResourceContent{{URI: "clictl://installed", MimeType: "text/plain", Text: string(data)}}
}

func (s *Server) readVaultKeysResource() []ResourceContent {
	v := vault.NewVault(config.BaseDir())
	entries, err := v.List()
	if err != nil {
		return []ResourceContent{{URI: "clictl://vault/keys", MimeType: "application/json", Text: "[]"}}
	}
	var keys []string
	for _, e := range entries {
		keys = append(keys, e.Name)
	}
	data, _ := json.Marshal(keys)
	return []ResourceContent{{URI: "clictl://vault/keys", MimeType: "application/json", Text: string(data)}}
}

func (s *Server) readWorkspaceResource() []ResourceContent {
	cfg, err := config.Load()
	info := map[string]string{"workspace": ""}
	if err == nil && cfg.Auth.ActiveWorkspace != "" {
		info["workspace"] = cfg.Auth.ActiveWorkspace
	}
	data, _ := json.Marshal(info)
	return []ResourceContent{{URI: "clictl://workspace", MimeType: "application/json", Text: string(data)}}
}

func (s *Server) readMemoryResource(toolName string) []ResourceContent {
	entries, err := memory.Load(toolName)
	if err != nil || len(entries) == 0 {
		return []ResourceContent{{
			URI:      "clictl://memory/" + toolName,
			MimeType: "application/json",
			Text:     "[]",
		}}
	}
	data, _ := json.Marshal(entries)
	return []ResourceContent{{
		URI:      "clictl://memory/" + toolName,
		MimeType: "application/json",
		Text:     string(data),
	}}
}

// proxyResourcesList forwards resources/list to an upstream MCP server.
func (s *Server) proxyResourcesList(ctx context.Context, spec *models.ToolSpec) []Resource {
	client, err := s.pool.Get(ctx, spec)
	if err != nil {
		return nil
	}
	defer s.pool.Release(spec.Name)

	req := client.newRequest("resources/list", nil)
	resp, err := client.transport.Send(ctx, req)
	if err != nil || resp.Error != nil {
		return nil
	}

	data, _ := json.Marshal(resp.Result)
	var result ResourcesListResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}

	// Prefix resource names with spec name for disambiguation
	for i := range result.Resources {
		result.Resources[i].Name = fmt.Sprintf("[%s] %s", spec.Name, StripANSI(result.Resources[i].Name))
		if result.Resources[i].Description != "" {
			result.Resources[i].Description = StripANSI(result.Resources[i].Description)
		}
	}
	return result.Resources
}

// proxyResourcesTemplatesList forwards resources/templates/list to upstream.
func (s *Server) proxyResourcesTemplatesList(ctx context.Context, spec *models.ToolSpec) []ResourceTemplate {
	client, err := s.pool.Get(ctx, spec)
	if err != nil {
		return nil
	}
	defer s.pool.Release(spec.Name)

	req := client.newRequest("resources/templates/list", nil)
	resp, err := client.transport.Send(ctx, req)
	if err != nil || resp.Error != nil {
		return nil
	}

	data, _ := json.Marshal(resp.Result)
	var result ResourceTemplatesListResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}

	for i := range result.ResourceTemplates {
		result.ResourceTemplates[i].Name = fmt.Sprintf("[%s] %s", spec.Name, StripANSI(result.ResourceTemplates[i].Name))
		if result.ResourceTemplates[i].Description != "" {
			result.ResourceTemplates[i].Description = StripANSI(result.ResourceTemplates[i].Description)
		}
	}
	return result.ResourceTemplates
}

// proxyResourceRead forwards a resources/read to an upstream MCP server.
func (s *Server) proxyResourceRead(ctx context.Context, spec *models.ToolSpec, uri string) []ResourceContent {
	client, err := s.pool.Get(ctx, spec)
	if err != nil {
		return nil
	}
	defer s.pool.Release(spec.Name)

	req := client.newRequest("resources/read", map[string]any{"uri": uri})
	resp, err := client.transport.Send(ctx, req)
	if err != nil || resp.Error != nil {
		return nil
	}

	data, _ := json.Marshal(resp.Result)
	var result ResourceReadResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}

	// Sanitize ANSI from text content
	for i := range result.Contents {
		result.Contents[i].Text = StripANSI(result.Contents[i].Text)
	}
	return result.Contents
}

// staticResourcesList returns resources from a spec's static config.
func (s *Server) staticResourcesList(spec *models.ToolSpec) []Resource {
	if spec.Resources == nil {
		return nil
	}

	// The expose field can be "all" (bool true) or a list of resource descriptors.
	// For now, we report that the spec has resources but defer actual listing
	// to the spec's declared structure.
	switch v := spec.Resources.Expose.(type) {
	case bool:
		if v {
			return []Resource{{
				URI:         fmt.Sprintf("clictl://%s/resources", spec.Name),
				Name:        fmt.Sprintf("[%s] Resources", spec.Name),
				Description: fmt.Sprintf("Resources provided by %s", spec.Name),
			}}
		}
	case []interface{}:
		var resources []Resource
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				r := Resource{
					Name: fmt.Sprintf("[%s] %s", spec.Name, m["name"]),
				}
				if uri, ok := m["uri"].(string); ok {
					r.URI = uri
				}
				if desc, ok := m["description"].(string); ok {
					r.Description = desc
				}
				if mime, ok := m["mimeType"].(string); ok {
					r.MimeType = mime
				}
				resources = append(resources, r)
			}
		}
		return resources
	}
	return nil
}

// M1.16: applyResourceTransforms applies configured transforms to resource content.
func (s *Server) applyResourceTransforms(spec *models.ToolSpec, uri string, contents []ResourceContent) []ResourceContent {
	if spec.Resources == nil || len(spec.Resources.Transforms) == 0 {
		return contents
	}

	// Look up transforms by URI or wildcard
	steps, ok := spec.Resources.Transforms[uri]
	if !ok {
		steps, ok = spec.Resources.Transforms["*"]
	}
	if !ok || len(steps) == 0 {
		return contents
	}

	// Apply transform steps to text content
	for i := range contents {
		for _, step := range steps {
			switch step.Type {
			case "truncate":
				if step.MaxLength > 0 && len(contents[i].Text) > step.MaxLength {
					contents[i].Text = contents[i].Text[:step.MaxLength] + "... (truncated)"
				}
			case "prefix":
				if step.Value != "" {
					contents[i].Text = step.Value + contents[i].Text
				}
			}
		}
	}
	return contents
}

// M1.17: Resource cache methods.
func (s *Server) getCachedResource(uri string) []ResourceContent {
	s.resourceCacheMu.RLock()
	defer s.resourceCacheMu.RUnlock()
	entry, ok := s.resourceCache[uri]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil
	}
	return entry.contents
}

func (s *Server) cacheResource(uri string, contents []ResourceContent) {
	s.resourceCacheMu.Lock()
	defer s.resourceCacheMu.Unlock()
	s.resourceCache[uri] = &resourceCacheEntry{
		contents:  contents,
		expiresAt: time.Now().Add(defaultResourceCacheTTL),
	}
}

// M1.20: wrapWithBoundary adds content boundary markers using the session token.
func (s *Server) wrapWithBoundary(specName string, contents []ResourceContent) []ResourceContent {
	for i := range contents {
		if contents[i].Text != "" {
			contents[i].Text = fmt.Sprintf("--- BEGIN %s [%s] ---\n%s\n--- END %s [%s] ---",
				specName, s.boundaryToken, contents[i].Text, specName, s.boundaryToken)
		}
	}
	return contents
}

// ---------------------------------------------------------------------------
// Prompt handlers (M1.12)
// ---------------------------------------------------------------------------

// handlePromptsList returns all available prompts from specs.
func (s *Server) handlePromptsList(ctx context.Context, req *Request) {
	var prompts []Prompt

	for _, spec := range s.specs {
		if spec.Discover {
			// Proxy prompts/list to upstream
			proxyPrompts := s.proxyPromptsList(ctx, spec)
			prompts = append(prompts, proxyPrompts...)
		} else if len(spec.Prompts.Items) > 0 {
			// Static prompts from spec
			for _, p := range spec.Prompts.Items {
				prompt := Prompt{
					Name:        fmt.Sprintf("%s_%s", spec.Name, p.Name),
					Description: StripANSI(fmt.Sprintf("[%s] %s", spec.Name, p.Description)),
				}
				for _, param := range p.Params {
					prompt.Arguments = append(prompt.Arguments, PromptArg{
						Name:        param.Name,
						Description: param.Description,
						Required:    param.Required,
					})
				}
				prompts = append(prompts, prompt)
			}
		}
	}

	s.sendResult(req.ID, PromptsListResult{Prompts: prompts})
}

// handlePromptsGet returns a specific prompt by name.
func (s *Server) handlePromptsGet(ctx context.Context, req *Request) {
	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		s.sendError(req.ID, -32602, "Invalid params")
		return
	}

	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments,omitempty"`
	}
	if err := json.Unmarshal(paramsJSON, &params); err != nil {
		s.sendError(req.ID, -32602, "Invalid params")
		return
	}

	if params.Name == "" {
		s.sendError(req.ID, -32602, "Missing required parameter: name")
		return
	}

	// Parse prompt name: specName_promptName
	parts := strings.SplitN(params.Name, "_", 2)
	if len(parts) != 2 {
		s.sendError(req.ID, -32602, fmt.Sprintf("Invalid prompt name: %s", params.Name))
		return
	}
	specName, promptName := parts[0], parts[1]

	// Find the spec
	for _, spec := range s.specs {
		if spec.Discover && spec.Name == specName {
			// Proxy to upstream
			result := s.proxyPromptGet(ctx, spec, promptName, params.Arguments)
			if result != nil {
				s.sendResult(req.ID, *result)
				return
			}
		}
		if spec.Name == specName {
			// Static prompt from spec
			for _, p := range spec.Prompts.Items {
				if p.Name == promptName {
					messages := []PromptMessage{{
						Role:    "user",
						Content: ContentBlock{Type: "text", Text: p.Description},
					}}
					s.sendResult(req.ID, PromptGetResult{
						Description: p.Description,
						Messages:    messages,
					})
					return
				}
			}
		}
	}

	s.sendError(req.ID, -32602, fmt.Sprintf("Prompt not found: %s", params.Name))
}

// proxyPromptsList forwards prompts/list to an upstream MCP server.
func (s *Server) proxyPromptsList(ctx context.Context, spec *models.ToolSpec) []Prompt {
	client, err := s.pool.Get(ctx, spec)
	if err != nil {
		return nil
	}
	defer s.pool.Release(spec.Name)

	req := client.newRequest("prompts/list", nil)
	resp, err := client.transport.Send(ctx, req)
	if err != nil || resp.Error != nil {
		return nil
	}

	data, _ := json.Marshal(resp.Result)
	var result PromptsListResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}

	// Prefix prompt names with spec name
	prefix := spec.Name + "_"
	for i := range result.Prompts {
		result.Prompts[i].Name = prefix + result.Prompts[i].Name
		result.Prompts[i].Description = StripANSI(fmt.Sprintf("[%s] %s", spec.Name, result.Prompts[i].Description))
	}
	return result.Prompts
}

// proxyPromptGet forwards prompts/get to an upstream MCP server.
func (s *Server) proxyPromptGet(ctx context.Context, spec *models.ToolSpec, promptName string, args map[string]any) *PromptGetResult {
	client, err := s.pool.Get(ctx, spec)
	if err != nil {
		return nil
	}
	defer s.pool.Release(spec.Name)

	reqParams := map[string]any{"name": promptName}
	if len(args) > 0 {
		reqParams["arguments"] = args
	}

	req := client.newRequest("prompts/get", reqParams)
	resp, err := client.transport.Send(ctx, req)
	if err != nil || resp.Error != nil {
		return nil
	}

	data, _ := json.Marshal(resp.Result)
	var result PromptGetResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}

	// Sanitize ANSI from prompt message content
	for i := range result.Messages {
		result.Messages[i].Content.Text = StripANSI(result.Messages[i].Content.Text)
	}
	return &result
}

func buildInputSchema(action models.Action) InputSchema {
	schema := InputSchema{
		Type:       "object",
		Properties: make(map[string]PropertySchema),
	}

	for _, p := range action.Params {
		propType := "string"
		switch p.Type {
		case "int", "integer":
			propType = "number"
		case "float", "number":
			propType = "number"
		case "bool", "boolean":
			propType = "boolean"
		}

		schema.Properties[p.Name] = PropertySchema{
			Type:        propType,
			Description: p.Description,
			Default:     p.Default,
		}

		if p.Required {
			schema.Required = append(schema.Required, p.Name)
		}
	}

	return schema
}
