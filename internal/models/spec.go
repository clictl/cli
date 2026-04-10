// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.

// Package models defines the core data structures for the clictl tool spec format.
// The ToolSpec struct is the canonical representation of a tool - parsed from YAML,
// validated by the loader, and consumed by executors, installers, and the MCP server.
package models

import (
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Spec 1.0 types
// ---------------------------------------------------------------------------

// StringOrSlice accepts either a single string or a list of strings in YAML.
type StringOrSlice []string

// UnmarshalYAML implements custom YAML unmarshaling for StringOrSlice.
func (s *StringOrSlice) UnmarshalYAML(unmarshal func(any) error) error {
	var single string
	if err := unmarshal(&single); err == nil {
		*s = StringOrSlice{single}
		return nil
	}
	var multi []string
	if err := unmarshal(&multi); err != nil {
		return fmt.Errorf("must be a string or list of strings: %w", err)
	}
	*s = StringOrSlice(multi)
	return nil
}

// ToolSpec represents a complete tool specification parsed from YAML (spec 1.0).
type ToolSpec struct {
	// Identity
	Spec        string   `yaml:"spec,omitempty" json:"spec,omitempty"`               // format version, MUST be "1.0"
	Name        string   `yaml:"name" json:"name"`                                   // unique identifier, kebab-case
	Protocol    string   `yaml:"protocol" json:"protocol"`                           // http, mcp, skill, website, command
	Description string   `yaml:"description" json:"description"`                     // one-line description
	Version     string   `yaml:"version,omitempty" json:"version,omitempty"`         // tool version
	Category    string   `yaml:"category,omitempty" json:"category,omitempty"`       // category slug
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`               // search tags
	Namespace   string   `yaml:"namespace,omitempty" json:"namespace,omitempty"`     // publisher namespace

	// Execution
	Server  *Server  `yaml:"server,omitempty" json:"server,omitempty"`   // how to reach the service
	Auth    *Auth    `yaml:"auth,omitempty" json:"auth,omitempty"`       // authentication config
	Depends []string `yaml:"depends,omitempty" json:"depends,omitempty"` // tool dependencies

	// MCP filtering
	Discover bool     `yaml:"discover,omitempty" json:"discover,omitempty"` // set automatically for stdio, not user-facing
	Allow    []string `yaml:"allow,omitempty" json:"allow,omitempty"`       // MCP tool allow patterns
	Deny     []string `yaml:"deny,omitempty" json:"deny,omitempty"`         // MCP tool deny patterns

	// Marketplace
	Pricing *Pricing `yaml:"pricing,omitempty" json:"pricing,omitempty"` // cost model + signup URL
	Privacy *Privacy `yaml:"privacy,omitempty" json:"privacy,omitempty"` // data handling hints

	// Agent guidance
	Instructions string `yaml:"instructions,omitempty" json:"instructions,omitempty"` // markdown guidance

	// Actions (REST, CLI, MCP static, composite)
	Actions []Action `yaml:"actions,omitempty" json:"actions,omitempty"`

	// MCP-specific
	Prompts    PromptList                    `yaml:"prompts,omitempty" json:"prompts,omitempty"`       // prompt templates (MCP array or guidance map)
	Resources  *Resources                    `yaml:"resources,omitempty" json:"resources,omitempty"`   // resource config
	Transforms map[string][]TransformStep    `yaml:"transforms,omitempty" json:"transforms,omitempty"` // per-action transforms

	// Skill-specific
	Source  *SkillSource `yaml:"source,omitempty" json:"source,omitempty"`   // where to fetch skill files
	Runtime *Runtime     `yaml:"runtime,omitempty" json:"runtime,omitempty"` // manager + dependencies

	// Security
	Sandbox *Sandbox `yaml:"sandbox,omitempty" json:"sandbox,omitempty"` // runtime restrictions

	// Package metadata (MCP servers)
	Package *Package `yaml:"package,omitempty" json:"package,omitempty"` // registry + version info

	// Attribution
	Publisher *Publisher `yaml:"publisher,omitempty" json:"publisher,omitempty"` // spec author

	// Lifecycle
	Deprecated    bool   `yaml:"deprecated,omitempty" json:"deprecated,omitempty"`
	DeprecatedMsg string `yaml:"deprecated_message,omitempty" json:"deprecated_message,omitempty"`
	DeprecatedBy  string `yaml:"deprecated_by,omitempty" json:"deprecated_by,omitempty"`
	Canonical     string `yaml:"canonical,omitempty" json:"canonical,omitempty"` // canonical source URL

	// Test
	Test *TestConfig `yaml:"test,omitempty" json:"test,omitempty"`

	// Runtime metadata (set by resolver, not parsed from YAML)
	DisplayName      string `yaml:"-" json:"-"`
	IsVerified       bool   `yaml:"-" json:"-"`
	ResolvedFrom     string `yaml:"-" json:"-"`
	TrustTier        string `yaml:"-" json:"-"`
	RawYAML          string `yaml:"-" json:"-"`
	SecurityAdvisory string `yaml:"-" json:"-"` // populated from registry if tool has known vulnerabilities
}

// ServerType returns the server type string, inferring from context if not set.
func (s *ToolSpec) ServerType() string {
	if s.Server == nil {
		return ""
	}
	if s.Server.Type != "" {
		return s.Server.Type
	}
	// Infer type: if server has a URL, treat as HTTP (covers website protocol)
	if s.Server.URL != "" {
		return "http"
	}
	// If server has a command, treat as stdio
	if s.Server.Command != "" {
		return "stdio"
	}
	return ""
}

// IsHTTP returns true if this is an HTTP-based tool (REST, GraphQL, remote MCP).
func (s *ToolSpec) IsHTTP() bool { return s.ServerType() == "http" }

// IsStdio returns true if this is a stdio-based tool (local MCP server).
func (s *ToolSpec) IsStdio() bool { return s.ServerType() == "stdio" }

// IsWebSocket returns true if this is a WebSocket-based tool.
func (s *ToolSpec) IsWebSocket() bool { return s.ServerType() == "websocket" }

// IsCommand returns true if this is a CLI wrapper tool.
func (s *ToolSpec) IsCommand() bool { return s.ServerType() == "command" }

// IsSkill returns true if this is a skill (has source, no server).
func (s *ToolSpec) IsSkill() bool { return s.Source != nil && s.Server == nil }

// IsMCPPackage returns true if this spec defines an MCP server via a package manager.
func (s *ToolSpec) IsMCPPackage() bool { return s.Package != nil && s.Server == nil }

// EnsureServer synthesizes a Server block from the Package field if one
// is not already set. This allows package-based MCP specs (npm/pypi) to
// work with code that expects a server config. Returns true if a server
// was synthesized.
func (s *ToolSpec) EnsureServer() bool {
	if s.Server != nil || s.Package == nil {
		return false
	}
	manager := s.Package.Manager
	if manager == "" {
		switch s.Package.Registry {
		case "npm":
			manager = "npx"
		case "pypi":
			manager = "uvx"
		default:
			return false
		}
	}
	pkgRef := s.Package.Name
	if s.Package.Version != "" {
		switch s.Package.Registry {
		case "npm":
			pkgRef = s.Package.Name + "@" + s.Package.Version
		case "pypi":
			pkgRef = s.Package.Name + "==" + s.Package.Version
		}
	}
	s.Server = &Server{
		Type:    "stdio",
		Command: manager,
		Args:    []string{pkgRef},
	}
	return true
}

// AuthEnvVars returns the list of environment variable names required for auth.
func (s *ToolSpec) AuthEnvVars() []string {
	if s.Auth == nil {
		return nil
	}
	return []string(s.Auth.Env)
}

// ResolveActionURL returns the effective base URL for an action.
// Precedence: action.URL > spec.Server.URL.
func (s *ToolSpec) ResolveActionURL(action *Action) string {
	if action.URL != "" {
		return action.URL
	}
	if s.Server != nil {
		return s.Server.URL
	}
	return ""
}

// ResolveActionAuth returns the effective auth config for an action.
// Precedence: action.Auth > spec.Auth > nil.
func (s *ToolSpec) ResolveActionAuth(action *Action) *Auth {
	if action.Auth != nil {
		return action.Auth
	}
	return s.Auth
}

// ResolveActionHeaders merges spec-level and action-level headers.
// Action headers take precedence over spec.Server.Headers.
func (s *ToolSpec) ResolveActionHeaders(action *Action) map[string]string {
	merged := make(map[string]string)
	if s.Server != nil {
		for k, v := range s.Server.Headers {
			merged[k] = v
		}
	}
	for k, v := range action.Headers {
		merged[k] = v
	}
	return merged
}

// ResolveStepURL returns the effective URL for a composite step.
// Precedence: step.URL > parent action.URL > spec.Server.URL.
func (s *ToolSpec) ResolveStepURL(step *CompositeStep, parent *Action) string {
	if step.URL != "" {
		return step.URL
	}
	return s.ResolveActionURL(parent)
}

// ResolveStepAuth returns the effective auth for a composite step.
// Precedence: step.Auth > parent action.Auth > spec.Auth > nil.
func (s *ToolSpec) ResolveStepAuth(step *CompositeStep, parent *Action) *Auth {
	if step.Auth != nil {
		return step.Auth
	}
	return s.ResolveActionAuth(parent)
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

// Server describes how to connect to the tool's service.
type Server struct {
	Type      string            `yaml:"type" json:"type"`                               // http, stdio, command, websocket
	URL       string            `yaml:"url,omitempty" json:"url,omitempty"`             // http/websocket: base URL
	Command   string            `yaml:"command,omitempty" json:"command,omitempty"`     // stdio: binary to execute
	Args      []string          `yaml:"args,omitempty" json:"args,omitempty"`           // stdio: command arguments
	Env       map[string]string `yaml:"env,omitempty" json:"env,omitempty"`             // stdio: env vars for child
	Shell     string            `yaml:"shell,omitempty" json:"shell,omitempty"`         // command: shell to use
	Headers   map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`     // http: default headers
	Timeout   string            `yaml:"timeout,omitempty" json:"timeout,omitempty"`     // default "30s"
	KeepAlive string            `yaml:"keep_alive,omitempty" json:"keep_alive,omitempty"` // stdio: keep alive between calls
	Requires  []Requirement     `yaml:"requires,omitempty" json:"requires,omitempty"`   // binary prerequisites
}

// TimeoutDuration parses the timeout string. Returns 30s if empty or invalid.
func (s Server) TimeoutDuration() time.Duration {
	if s.Timeout == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(s.Timeout)
	if err != nil {
		return 30 * time.Second
	}
	return d
}

// KeepAliveDuration parses the keep_alive string. Returns 60s if empty or invalid.
func (s Server) KeepAliveDuration() time.Duration {
	if s.KeepAlive == "" {
		return 60 * time.Second
	}
	d, err := time.ParseDuration(s.KeepAlive)
	if err != nil {
		return 60 * time.Second
	}
	return d
}

// Requirement describes a system binary that must be available.
type Requirement struct {
	Name  string `yaml:"name" json:"name"`                       // binary name
	Check string `yaml:"check,omitempty" json:"check,omitempty"` // verification command
	URL   string `yaml:"url,omitempty" json:"url,omitempty"`     // install instructions
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

// Auth describes how to authenticate with the tool's API.
// In 1.0 format, Header is a template string: "HeaderName: ${ENV_VAR}".
type Auth struct {
	Env    StringOrSlice `yaml:"env,omitempty" json:"env,omitempty"`       // vault key name(s)
	Header string        `yaml:"header,omitempty" json:"header,omitempty"` // header template: "Authorization: Bearer ${KEY}"
	Param  string        `yaml:"param,omitempty" json:"param,omitempty"`   // query param name
}

// ---------------------------------------------------------------------------
// Action
// ---------------------------------------------------------------------------

// Action represents a single operation a tool can perform.
// HTTP actions define method/url/path/headers/auth directly. CLI actions use Run.
// MCP static actions have neither. If Steps is non-empty, the action is composite.
type Action struct {
	Name          string `yaml:"name" json:"name"`
	Description   string `yaml:"description" json:"description"`
	Output        string `yaml:"output,omitempty" json:"output,omitempty"`               // json, markdown, text, csv, html
	Mutable       bool   `yaml:"mutable,omitempty" json:"mutable,omitempty"`             // changes state
	Stream        bool   `yaml:"stream,omitempty" json:"stream,omitempty"`               // streams output
	StreamTimeout string `yaml:"stream_timeout,omitempty" json:"stream_timeout,omitempty"` // idle timeout (default 30s)
	Instructions  string `yaml:"instructions,omitempty" json:"instructions,omitempty"`   // action-specific guidance

	// HTTP execution (each action can target a different API endpoint)
	Method  string            `yaml:"method,omitempty" json:"method,omitempty"`   // GET (default), POST, PUT, PATCH, DELETE
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`         // base URL for this action
	Path    string            `yaml:"path,omitempty" json:"path,omitempty"`       // URL path with {param} placeholders
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"` // request headers
	Auth    *Auth             `yaml:"auth,omitempty" json:"auth,omitempty"`       // auth config (nil = inherit spec-level or none)

	// CLI execution
	Run string `yaml:"run,omitempty" json:"run,omitempty"` // CLI actions

	// WebSocket execution
	Message string `yaml:"message,omitempty" json:"message,omitempty"` // Message to send (supports ${param} templating)
	Wait    string `yaml:"wait,omitempty" json:"wait,omitempty"`       // How long to listen for responses (default "5s")
	Collect int    `yaml:"collect,omitempty" json:"collect,omitempty"` // How many messages to collect (0 = all until timeout)

	// Input / output
	Params   []Param   `yaml:"params,omitempty" json:"params,omitempty"`
	Response *Response `yaml:"response,omitempty" json:"response,omitempty"`

	// Error handling
	Retry *Retry `yaml:"retry,omitempty" json:"retry,omitempty"`

	// Pagination
	Pagination *Pagination `yaml:"pagination,omitempty" json:"pagination,omitempty"`

	// Transform pipeline
	Transform []TransformStep `yaml:"transform,omitempty" json:"transform,omitempty"`
	Assert    []AssertStep    `yaml:"assert,omitempty" json:"assert,omitempty"`

	// Composite actions (presence of Steps implies composite)
	Steps []CompositeStep `yaml:"steps,omitempty" json:"steps,omitempty"`

	// Lifecycle
	Deprecated    bool   `yaml:"deprecated,omitempty" json:"deprecated,omitempty"`
	DeprecatedMsg string `yaml:"deprecated_message,omitempty" json:"deprecated_message,omitempty"`
	DeprecatedBy  string `yaml:"deprecated_by,omitempty" json:"deprecated_by,omitempty"`
}

// IsComposite returns true if this action has orchestration steps.
func (a *Action) IsComposite() bool { return len(a.Steps) > 0 }

// WaitDuration parses the Wait field. Returns 5s if empty or invalid.
func (a Action) WaitDuration() time.Duration {
	if a.Wait == "" {
		return 5 * time.Second
	}
	d, err := time.ParseDuration(a.Wait)
	if err != nil {
		return 5 * time.Second
	}
	return d
}

// Param describes an input parameter for an action.
type Param struct {
	Name        string   `yaml:"name" json:"name"`
	Type        string   `yaml:"type,omitempty" json:"type,omitempty"`               // default "string"
	Required    bool     `yaml:"required,omitempty" json:"required,omitempty"`
	Default     string   `yaml:"default,omitempty" json:"default,omitempty"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	In          string   `yaml:"in,omitempty" json:"in,omitempty"`                   // query, header, path, body (usually inferred)
	Example     string   `yaml:"example,omitempty" json:"example,omitempty"`
	Values      []string `yaml:"values,omitempty" json:"values,omitempty"`           // enum values
}

// Response describes the expected output of an action (post-transform).
type Response struct {
	Example     string `yaml:"example,omitempty" json:"example,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// Retry controls retry behavior on transient errors.
type Retry struct {
	On          []int  `yaml:"on,omitempty" json:"on,omitempty"`                     // status codes that trigger retry
	MaxAttempts int    `yaml:"max_attempts,omitempty" json:"max_attempts,omitempty"` // default 3
	Backoff     string `yaml:"backoff,omitempty" json:"backoff,omitempty"`           // exponential, linear, fixed
	Delay       string `yaml:"delay,omitempty" json:"delay,omitempty"`               // initial delay, default "1s"
}

// Pagination describes how to auto-paginate results.
type Pagination struct {
	Type           string `yaml:"type" json:"type"`                                         // page, cursor, offset
	Param          string `yaml:"param" json:"param"`                                       // page/cursor/offset param name
	PerPageParam   string `yaml:"per_page_param,omitempty" json:"per_page_param,omitempty"` // items per page param
	PerPageDefault int    `yaml:"per_page_default,omitempty" json:"per_page_default,omitempty"`
	MaxPages       int    `yaml:"max_pages,omitempty" json:"max_pages,omitempty"`
	CursorPath     string `yaml:"cursor_path,omitempty" json:"cursor_path,omitempty"`       // JSONPath for next cursor
	HasMorePath    string `yaml:"has_more_path,omitempty" json:"has_more_path,omitempty"`   // JSONPath for has_more
	LimitParam     string `yaml:"limit_param,omitempty" json:"limit_param,omitempty"`       // offset: limit param
	LimitDefault   int    `yaml:"limit_default,omitempty" json:"limit_default,omitempty"`
}

// ---------------------------------------------------------------------------
// Transform pipeline
// ---------------------------------------------------------------------------

// TransformStep is a single step in a transform pipeline.
// The Type field determines which other fields are used.
type TransformStep struct {
	Type string `yaml:"type" json:"type"` // json, truncate, format, template, html_to_markdown, etc.

	// Lifecycle phase
	On string `yaml:"on,omitempty" json:"on,omitempty"` // request, response, output (default: output)

	// DAG fields
	ID          string   `yaml:"id,omitempty" json:"id,omitempty"`
	Input       string   `yaml:"input,omitempty" json:"input,omitempty"`
	DependsOn   []string `yaml:"depends,omitempty" json:"depends,omitempty"`
	Each        bool     `yaml:"each,omitempty" json:"each,omitempty"`
	When        string   `yaml:"when,omitempty" json:"when,omitempty"`
	Concurrency int      `yaml:"concurrency,omitempty" json:"concurrency,omitempty"` // default 10

	// type: json
	Extract       string            `yaml:"extract,omitempty" json:"extract,omitempty"`
	Select        []string          `yaml:"select,omitempty" json:"select,omitempty"`
	Rename        map[string]string `yaml:"rename,omitempty" json:"rename,omitempty"`
	Only          []string          `yaml:"only,omitempty" json:"only,omitempty"`
	Inject        map[string]any    `yaml:"inject,omitempty" json:"inject,omitempty"`
	Flatten       bool              `yaml:"flatten,omitempty" json:"flatten,omitempty"`
	Unwrap        bool              `yaml:"unwrap,omitempty" json:"unwrap,omitempty"`
	DefaultFields map[string]any    `yaml:"default,omitempty" json:"default,omitempty"`

	// type: truncate
	MaxItems  int `yaml:"max_items,omitempty" json:"max_items,omitempty"`
	MaxLength int `yaml:"max_length,omitempty" json:"max_length,omitempty"`

	// type: format, template, prompt
	Template string `yaml:"template,omitempty" json:"template,omitempty"`
	Value    string `yaml:"value,omitempty" json:"value,omitempty"` // prompt, prefix

	// type: html_to_markdown
	RemoveImages bool `yaml:"remove_images,omitempty" json:"remove_images,omitempty"`
	RemoveLinks  bool `yaml:"remove_links,omitempty" json:"remove_links,omitempty"`

	// type: sort
	Field string `yaml:"field,omitempty" json:"field,omitempty"` // also used by date_format, unique, base64_decode
	Order string `yaml:"order,omitempty" json:"order,omitempty"` // asc, desc

	// type: filter, jq
	Filter string `yaml:"filter,omitempty" json:"filter,omitempty"` // jq expression

	// type: join, split
	Separator string `yaml:"separator,omitempty" json:"separator,omitempty"`

	// type: date_format
	From string `yaml:"from,omitempty" json:"from,omitempty"`
	To   string `yaml:"to,omitempty" json:"to,omitempty"`

	// type: csv_to_json
	CSVHeaders bool `yaml:"headers,omitempty" json:"headers,omitempty"`

	// type: redact
	Patterns []RedactPattern `yaml:"patterns,omitempty" json:"patterns,omitempty"`

	// type: cost
	InputTokens  string `yaml:"input_tokens,omitempty" json:"input_tokens,omitempty"`
	OutputTokens string `yaml:"output_tokens,omitempty" json:"output_tokens,omitempty"`
	Model        string `yaml:"model,omitempty" json:"model,omitempty"`

	// type: pipe
	PipeTool   string            `yaml:"tool,omitempty" json:"tool,omitempty"`
	PipeAction string            `yaml:"action,omitempty" json:"action,omitempty"`
	PipeParams map[string]string `yaml:"params,omitempty" json:"params,omitempty"`
	PipeRun    string            `yaml:"run,omitempty" json:"run,omitempty"`

	// type: merge
	Sources  []string `yaml:"sources,omitempty" json:"sources,omitempty"`
	Strategy string   `yaml:"strategy,omitempty" json:"strategy,omitempty"` // zip, concat, first, join, object
	JoinOn   string   `yaml:"join_on,omitempty" json:"join_on,omitempty"`

	// pre-request types (on: request)
	DefaultParams map[string]string `yaml:"default_params,omitempty" json:"default_params,omitempty"`
	RenameParams  map[string]string `yaml:"rename_params,omitempty" json:"rename_params,omitempty"`
	TemplateBody  string            `yaml:"template_body,omitempty" json:"template_body,omitempty"`

	// type: js
	Script string `yaml:"script,omitempty" json:"script,omitempty"`
}

// RedactPattern describes a field pattern to redact from output.
type RedactPattern struct {
	Field   string `yaml:"field" json:"field"`
	Replace string `yaml:"replace" json:"replace"`
}

// ---------------------------------------------------------------------------
// Assert
// ---------------------------------------------------------------------------

// AssertStep is a single assertion in the validation pipeline.
type AssertStep struct {
	Type       string `yaml:"type" json:"type"`                                   // status, json, jq, js, cel, contains
	Values     []int  `yaml:"values,omitempty" json:"values,omitempty"`           // status: accepted codes
	Exists     string `yaml:"exists,omitempty" json:"exists,omitempty"`           // json: JSONPath must exist
	NotEmpty   string `yaml:"not_empty,omitempty" json:"not_empty,omitempty"`     // json: must not be empty
	Filter     string `yaml:"filter,omitempty" json:"filter,omitempty"`           // jq: expression
	Script     string `yaml:"script,omitempty" json:"script,omitempty"`           // js: script
	Expression string `yaml:"expression,omitempty" json:"expression,omitempty"`   // cel: expression
	Value      string `yaml:"value,omitempty" json:"value,omitempty"`             // contains: string
}

// ---------------------------------------------------------------------------
// Composite actions
// ---------------------------------------------------------------------------

// CompositeStep is a single step in a composite action.
// HTTP fields (Method, URL, Path, Headers, Auth) inherit from the parent action
// unless overridden. Steps can also delegate to another tool via Tool + Action.
type CompositeStep struct {
	ID        string            `yaml:"id" json:"id"`
	DependsOn []string          `yaml:"depends,omitempty" json:"depends,omitempty"`

	// Inline HTTP request (inherits parent action's URL/Auth/Headers if empty)
	Method  string            `yaml:"method,omitempty" json:"method,omitempty"`
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`
	Path    string            `yaml:"path,omitempty" json:"path,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Auth    *Auth             `yaml:"auth,omitempty" json:"auth,omitempty"`

	// Delegate to another tool
	Tool   string `yaml:"tool,omitempty" json:"tool,omitempty"`
	Action string `yaml:"action,omitempty" json:"action,omitempty"`

	Params    map[string]string `yaml:"params,omitempty" json:"params,omitempty"`
	Condition string            `yaml:"condition,omitempty" json:"condition,omitempty"`
	OnError   string            `yaml:"on_error,omitempty" json:"on_error,omitempty"`
	Retry     *Retry            `yaml:"retry,omitempty" json:"retry,omitempty"`
	Transform []TransformStep   `yaml:"transform,omitempty" json:"transform,omitempty"`
}

// ---------------------------------------------------------------------------
// Sandbox
// ---------------------------------------------------------------------------

// Sandbox describes runtime security restrictions.
type Sandbox struct {
	Commands   []string               `yaml:"commands,omitempty" json:"commands,omitempty"`     // allowed CLI commands
	Filesystem *FilesystemPermissions `yaml:"filesystem,omitempty" json:"filesystem,omitempty"`
	Network    *NetworkPermissions    `yaml:"network,omitempty" json:"network,omitempty"`
	Env        *EnvPermissions        `yaml:"env,omitempty" json:"env,omitempty"`
}

// FilesystemPermissions declares filesystem access patterns.
type FilesystemPermissions struct {
	Read  []string `yaml:"read,omitempty" json:"read,omitempty"`
	Write []string `yaml:"write,omitempty" json:"write,omitempty"`
}

// NetworkPermissions declares allowed network hosts.
type NetworkPermissions struct {
	Allow []string `yaml:"allow,omitempty" json:"allow,omitempty"`
}

// EnvPermissions declares which env vars are passed to subprocesses.
type EnvPermissions struct {
	Allow []string `yaml:"allow,omitempty" json:"allow,omitempty"`
}

// ---------------------------------------------------------------------------
// Publisher
// ---------------------------------------------------------------------------

// Publisher provides attribution for the spec author.
type Publisher struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	URL  string `yaml:"url,omitempty" json:"url,omitempty"`
}

// ---------------------------------------------------------------------------
// Package, Runtime, Pricing, Privacy
// ---------------------------------------------------------------------------

// Package describes the underlying software package for MCP servers.
type Package struct {
	Registry string `yaml:"registry,omitempty" json:"registry,omitempty"` // npm, pypi
	Name     string `yaml:"name,omitempty" json:"name,omitempty"`
	Version  string `yaml:"version,omitempty" json:"version,omitempty"`
	Manager  string `yaml:"manager,omitempty" json:"manager,omitempty"` // npx, uvx, bunx
	Pinned   bool   `yaml:"pinned,omitempty" json:"pinned,omitempty"`   // suppress upgrade suggestions
	SHA256   string `yaml:"sha256,omitempty" json:"sha256,omitempty"`
}

// Runtime describes the runtime environment for skill helper scripts.
type Runtime struct {
	Manager      string   `yaml:"manager,omitempty" json:"manager,omitempty"`           // uvx, npx, bunx, deno
	Dependencies []string `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

// Pricing signals whether a tool has associated costs.
type Pricing struct {
	Model string `yaml:"model,omitempty" json:"model,omitempty"` // free, freemium, paid, contact
	URL   string `yaml:"url,omitempty" json:"url,omitempty"`     // signup/pricing page
}

// Privacy provides optional data handling hints.
type Privacy struct {
	Local bool `yaml:"local,omitempty" json:"local,omitempty"` // no data leaves the machine
	PII   bool `yaml:"pii,omitempty" json:"pii,omitempty"`     // processes personal data
}

// ---------------------------------------------------------------------------
// MCP prompts and resources
// ---------------------------------------------------------------------------

// Prompt is an MCP prompt template.
type Prompt struct {
	Name        string  `yaml:"name" json:"name"`
	Description string  `yaml:"description,omitempty" json:"description,omitempty"`
	Params      []Param `yaml:"params,omitempty" json:"params,omitempty"`
}

// PromptList holds prompts and supports both MCP array format and guidance map format.
// MCP format: [{name: "review", description: "...", params: [...]}]
// Guidance format: {system: "...", tool_instructions: {tool: "..."}}
type PromptList struct {
	Items            []Prompt          `json:"items,omitempty"`
	System           string            `json:"system,omitempty"`
	ToolInstructions map[string]string `json:"tool_instructions,omitempty"`
}

// UnmarshalYAML handles both array and map formats for prompts.
func (p *PromptList) UnmarshalYAML(value *yaml.Node) error {
	// Try array format first (MCP prompts)
	if value.Kind == yaml.SequenceNode {
		var items []Prompt
		if err := value.Decode(&items); err != nil {
			return err
		}
		p.Items = items
		return nil
	}

	// Try map format (guidance prompts)
	if value.Kind == yaml.MappingNode {
		var m map[string]any
		if err := value.Decode(&m); err != nil {
			return err
		}
		if s, ok := m["system"].(string); ok {
			p.System = s
		}
		if ti, ok := m["tool_instructions"]; ok {
			if tiMap, ok := ti.(map[string]any); ok {
				p.ToolInstructions = make(map[string]string, len(tiMap))
				for k, v := range tiMap {
					if s, ok := v.(string); ok {
						p.ToolInstructions[k] = s
					}
				}
			}
		}
		return nil
	}

	return nil
}

// MarshalJSON produces a clean JSON representation.
func (p PromptList) MarshalJSON() ([]byte, error) {
	if len(p.Items) > 0 {
		return json.Marshal(p.Items)
	}
	if p.System != "" || len(p.ToolInstructions) > 0 {
		m := map[string]any{}
		if p.System != "" {
			m["system"] = p.System
		}
		if len(p.ToolInstructions) > 0 {
			m["tool_instructions"] = p.ToolInstructions
		}
		return json.Marshal(m)
	}
	return []byte("null"), nil
}

// Resources describes MCP resource exposure and transforms.
type Resources struct {
	Expose     any                        `yaml:"expose,omitempty" json:"expose,omitempty"`
	Transforms map[string][]TransformStep `yaml:"transforms,omitempty" json:"transforms,omitempty"`
}

// ---------------------------------------------------------------------------
// Skill source
// ---------------------------------------------------------------------------

// SkillSource describes where to fetch skill files.
type SkillSource struct {
	Repo  string            `yaml:"repo,omitempty" json:"repo,omitempty"` // org/repo
	Path  string            `yaml:"path,omitempty" json:"path,omitempty"` // path within repo
	Ref   string            `yaml:"ref,omitempty" json:"ref,omitempty"`   // branch or tag
	Files []SkillSourceFile `yaml:"files,omitempty" json:"files,omitempty"`
}

// SkillSourceFile describes a single file within a skill.
type SkillSourceFile struct {
	Path   string `yaml:"path" json:"path"`
	Name   string `yaml:"name,omitempty" json:"name,omitempty"` // legacy alias for Path
	SHA256 string `yaml:"sha256,omitempty" json:"sha256,omitempty"`
}

// FilePath returns the file path, preferring Path over the legacy Name field.
func (f SkillSourceFile) FilePath() string {
	if f.Path != "" {
		return f.Path
	}
	return f.Name
}

// ---------------------------------------------------------------------------
// Pack manifest
// ---------------------------------------------------------------------------

// PackManifest represents the manifest inside a signed .tar.gz pack.
type PackManifest struct {
	SchemaVersion string            `yaml:"schema_version" json:"schema_version"`
	Name          string            `yaml:"name" json:"name"`
	Type          string            `yaml:"type" json:"type"` // skill, mcp, http, website, composite, group
	Version       string            `yaml:"version" json:"version"`
	Description   string            `yaml:"description,omitempty" json:"description,omitempty"`
	Publisher     *PackPublisher    `yaml:"publisher,omitempty" json:"publisher,omitempty"`
	ContentSHA256 string            `yaml:"content_sha256,omitempty" json:"content_sha256,omitempty"`
	Provenance    *PackProvenance   `yaml:"provenance,omitempty" json:"provenance,omitempty"`
	Sandbox       *PackSandbox      `yaml:"sandbox,omitempty" json:"sandbox,omitempty"`
	Targets       []PackTarget      `yaml:"targets,omitempty" json:"targets,omitempty"`
	Tags          []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
	Signature     string            `yaml:"signature,omitempty" json:"signature,omitempty"`
	Dependencies  *PackDependencies `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

// PackPublisher identifies the pack publisher.
type PackPublisher struct {
	Name     string `yaml:"name" json:"name"`
	Identity string `yaml:"identity,omitempty" json:"identity,omitempty"`
}

// PackProvenance records build provenance for a pack.
type PackProvenance struct {
	Builder      string `yaml:"builder,omitempty" json:"builder,omitempty"`
	SourceRepo   string `yaml:"source_repo,omitempty" json:"source_repo,omitempty"`
	SourceRef    string `yaml:"source_ref,omitempty" json:"source_ref,omitempty"`
	SourceCommit string `yaml:"source_commit,omitempty" json:"source_commit,omitempty"`
	BuiltAt      string `yaml:"built_at,omitempty" json:"built_at,omitempty"`
}

// PackSandbox defines sandbox constraints for a pack.
type PackSandbox struct {
	Runtimes    []string        `yaml:"runtimes,omitempty" json:"runtimes,omitempty"`
	Network     string          `yaml:"network,omitempty" json:"network,omitempty"` // none, host
	Credentials []string        `yaml:"credentials,omitempty" json:"credentials,omitempty"`
	Filesystem  *PackFilesystem `yaml:"filesystem,omitempty" json:"filesystem,omitempty"`
	Timeout     string          `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// PackFilesystem defines filesystem access rules for a pack.
type PackFilesystem struct {
	ReadWrite []string `yaml:"read_write,omitempty" json:"read_write,omitempty"`
	ReadOnly  []string `yaml:"read_only,omitempty" json:"read_only,omitempty"`
}

// PackTarget specifies a compatible target for a pack.
type PackTarget struct {
	Type       string `yaml:"type" json:"type"` // claude-code, cursor, codex
	MinVersion string `yaml:"min_version,omitempty" json:"min_version,omitempty"`
}

// PackDependencies lists pack runtime and optional dependencies.
type PackDependencies struct {
	Runtime  []string `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Optional []string `yaml:"optional,omitempty" json:"optional,omitempty"`
}

// ---------------------------------------------------------------------------
// Test
// ---------------------------------------------------------------------------

// TestConfig holds test cases for `clictl test`.
type TestConfig struct {
	Actions []ActionTest `yaml:"actions,omitempty" json:"actions,omitempty"`
}

// ActionTest is a test case for a single action.
type ActionTest struct {
	Action  string            `yaml:"action" json:"action"`
	Params  map[string]string `yaml:"params,omitempty" json:"params,omitempty"`
	Timeout int               `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Assert  []AssertStep      `yaml:"assert,omitempty" json:"assert,omitempty"`
}

// ---------------------------------------------------------------------------
// Search and index types (not part of the spec format)
// ---------------------------------------------------------------------------

// SearchResult represents a single result from the API search endpoint.
type SearchResult struct {
	Name            string `json:"name"`
	QualifiedName   string `json:"qualified_name,omitempty"`
	Description     string `json:"description"`
	Category        string `json:"category"`
	Version         string `json:"version"`
	Source          string `json:"source,omitempty"`
	Protocol        string `json:"protocol,omitempty"`
	PackageRegistry string `json:"package_registry,omitempty"`
	TrustTier       string `json:"trust_tier,omitempty"`
	Publisher       string `json:"publisher,omitempty"`
	Deprecated      bool   `json:"deprecated,omitempty"`
	DeprecatedBy    string `json:"deprecated_by,omitempty"`
	DeprecatedMsg   string `json:"deprecated_message,omitempty"`
}

// SearchResponse is the API response for the search endpoint.
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Count   int            `json:"count"`
}

// ListResponse is the API response for the list endpoint.
type ListResponse struct {
	Results []SearchResult `json:"results"`
	Count   int            `json:"count"`
}

// CategoryResponse is the API response for the categories endpoint.
type CategoryResponse struct {
	Categories []string `json:"categories"`
}
