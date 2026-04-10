// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	osexec "os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/sandbox"
)

// Transport is the interface for MCP communication.
type Transport interface {
	// Send sends a JSON-RPC request and returns the response.
	Send(ctx context.Context, req *Request) (*Response, error)
	// Notify sends a JSON-RPC notification (no response expected).
	Notify(ctx context.Context, notif *Notification) error
	// Close shuts down the transport.
	Close() error
}

// notificationBufferSize is the capacity of the notification channel.
const notificationBufferSize = 64

// Client manages a connection to an MCP server.
type Client struct {
	transport     Transport
	requestID     atomic.Int64
	initialized   bool
	mu            sync.Mutex
	spec          *models.ToolSpec
	notifications chan *Notification
	serverCaps    Capability // capabilities reported by the server
}

// NewClient creates a new MCP client from a tool spec.
func NewClient(spec *models.ToolSpec) (*Client, error) {
	if spec.Server == nil {
		return nil, fmt.Errorf("spec %q has no server config", spec.Name)
	}

	var transport Transport
	var err error

	switch spec.Server.Type {
	case "stdio":
		transport, err = newStdioTransport(spec)
	case "http":
		transport, err = newHTTPTransport(spec)
	default:
		return nil, fmt.Errorf("unsupported server type: %q", spec.Server.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("creating %s transport: %w", spec.Server.Type, err)
	}

	c := &Client{
		transport:     transport,
		spec:          spec,
		notifications: make(chan *Notification, notificationBufferSize),
	}

	// Wire notification handler for stdio transports
	if st, ok := transport.(*StdioTransport); ok {
		st.onNotify = c.queueNotification
	}

	return c, nil
}

// Initialize performs the MCP handshake with the server.
func (c *Client) Initialize(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		return nil
	}

	req := c.newRequest("initialize", InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    Capability{},
		ClientInfo: Info{
			Name:    "clictl",
			Version: "1.0.0",
		},
	})

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s", resp.Error.Message)
	}

	// Parse server capabilities
	if resp.Result != nil {
		var initResult InitializeResult
		if data, mErr := json.Marshal(resp.Result); mErr == nil {
			if json.Unmarshal(data, &initResult) == nil {
				c.serverCaps = initResult.Capabilities
			}
		}
	}

	// Send initialized notification
	if err := c.transport.Notify(ctx, &Notification{
		JSONRPC: "2.0",
		Method:  "initialized",
	}); err != nil {
		return fmt.Errorf("sending initialized notification: %w", err)
	}

	c.initialized = true
	return nil
}

// requestTimeout returns a context with the per-request timeout from the spec,
// or the parent context if no timeout is configured.
func (c *Client) requestTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.spec != nil && c.spec.Server != nil && c.spec.Server.Timeout != "" {
		d := c.spec.Server.TimeoutDuration()
		return context.WithTimeout(ctx, d)
	}
	return ctx, func() {}
}

// sendRequest sends an MCP request with per-request timeout from spec.Server.Timeout.
func (c *Client) sendRequest(ctx context.Context, method string, params any) (*Response, error) {
	reqCtx, cancel := c.requestTimeout(ctx)
	defer cancel()

	req := c.newRequest(method, params)
	resp, err := c.transport.Send(reqCtx, req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%s error: %s", method, resp.Error.Message)
	}
	return resp, nil
}

// decodeResult marshals then unmarshals a response result into the target type.
func decodeResult(resp *Response, target any) error {
	data, err := json.Marshal(resp.Result)
	if err != nil {
		return fmt.Errorf("marshaling result: %w", err)
	}
	return json.Unmarshal(data, target)
}

// paginatedList calls a list method repeatedly until there is no nextCursor,
// accumulating results via the provided callback. The callback receives the
// raw response result and should append to its accumulator, returning the
// nextCursor value (empty string means done).
func (c *Client) paginatedList(ctx context.Context, method string, handler func(json.RawMessage) (string, error)) error {
	var cursor string
	for {
		var params any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}

		resp, err := c.sendRequest(ctx, method, params)
		if err != nil {
			return err
		}

		data, err := json.Marshal(resp.Result)
		if err != nil {
			return fmt.Errorf("marshaling %s result: %w", method, err)
		}

		nextCursor, err := handler(data)
		if err != nil {
			return fmt.Errorf("parsing %s result: %w", method, err)
		}

		if nextCursor == "" {
			return nil
		}
		cursor = nextCursor
	}
}

// ListTools returns the tools available from the MCP server.
// Automatically paginates if the server returns a nextCursor.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}

	var tools []Tool
	err := c.paginatedList(ctx, "tools/list", func(data json.RawMessage) (string, error) {
		var result ToolsListResult
		if err := json.Unmarshal(data, &result); err != nil {
			return "", err
		}
		tools = append(tools, result.Tools...)
		return result.NextCursor, nil
	})
	if err != nil {
		return nil, err
	}
	return tools, nil
}

// HasResources returns true if the server advertised resources capability.
func (c *Client) HasResources() bool {
	return c.serverCaps.Resources != nil
}

// HasPrompts returns true if the server advertised prompts capability.
func (c *Client) HasPrompts() bool {
	return c.serverCaps.Prompts != nil
}

// ListResources returns the resources available from the MCP server.
// Automatically paginates if the server returns a nextCursor.
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	if !c.HasResources() {
		return nil, nil // server doesn't support resources
	}

	var resources []Resource
	err := c.paginatedList(ctx, "resources/list", func(data json.RawMessage) (string, error) {
		var result ResourcesListResult
		if err := json.Unmarshal(data, &result); err != nil {
			return "", err
		}
		resources = append(resources, result.Resources...)
		return result.NextCursor, nil
	})
	if err != nil {
		return nil, err
	}
	return resources, nil
}

// ReadResource reads the contents of a resource by URI.
func (c *Client) ReadResource(ctx context.Context, uri string) (*ResourceReadResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, "resources/read", map[string]any{"uri": uri})
	if err != nil {
		return nil, err
	}

	var result ResourceReadResult
	if err := decodeResult(resp, &result); err != nil {
		return nil, fmt.Errorf("parsing resources/read result: %w", err)
	}
	return &result, nil
}

// ListResourceTemplates returns the resource templates available from the MCP server.
// Automatically paginates if the server returns a nextCursor.
func (c *Client) ListResourceTemplates(ctx context.Context) ([]ResourceTemplate, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}

	var templates []ResourceTemplate
	err := c.paginatedList(ctx, "resources/templates/list", func(data json.RawMessage) (string, error) {
		var result ResourceTemplatesListResult
		if err := json.Unmarshal(data, &result); err != nil {
			return "", err
		}
		templates = append(templates, result.ResourceTemplates...)
		return result.NextCursor, nil
	})
	if err != nil {
		return nil, err
	}
	return templates, nil
}

// ListPrompts returns the prompts available from the MCP server.
// Automatically paginates if the server returns a nextCursor.
func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	if !c.HasPrompts() {
		return nil, nil // server doesn't support prompts
	}

	var prompts []Prompt
	err := c.paginatedList(ctx, "prompts/list", func(data json.RawMessage) (string, error) {
		var result PromptsListResult
		if err := json.Unmarshal(data, &result); err != nil {
			return "", err
		}
		prompts = append(prompts, result.Prompts...)
		return result.NextCursor, nil
	})
	if err != nil {
		return nil, err
	}
	return prompts, nil
}

// GetPrompt retrieves a prompt by name with optional arguments.
func (c *Client) GetPrompt(ctx context.Context, name string, args map[string]string) (*PromptGetResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}

	params := map[string]any{"name": name}
	if len(args) > 0 {
		params["arguments"] = args
	}

	resp, err := c.sendRequest(ctx, "prompts/get", params)
	if err != nil {
		return nil, err
	}

	var result PromptGetResult
	if err := decodeResult(resp, &result); err != nil {
		return nil, fmt.Errorf("parsing prompts/get result: %w", err)
	}
	return &result, nil
}

// Notifications returns a read-only channel of notifications received from the server.
// Notifications are queued in a buffered channel. If the buffer is full, new
// notifications are dropped.
func (c *Client) Notifications() <-chan *Notification {
	return c.notifications
}

// queueNotification adds a notification to the buffer. If the buffer is full,
// the notification is dropped silently to avoid blocking the transport reader.
func (c *Client) queueNotification(notif *Notification) {
	select {
	case c.notifications <- notif:
	default:
		// Buffer full, drop the notification
	}
}

// CallTool invokes a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*CallToolResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}

	req := c.newRequest("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("tools/call %s: %w", name, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("tools/call %s error: %s", name, resp.Error.Message)
	}

	data, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("marshaling tools/call result: %w", err)
	}

	var result CallToolResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing tools/call result: %w", err)
	}

	return &result, nil
}

// Ping sends a ping request to the MCP server and returns an error if
// the server does not respond within the given context deadline.
func (c *Client) Ping(ctx context.Context) error {
	req := c.newRequest("ping", nil)
	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("ping error: %s", resp.Error.Message)
	}
	return nil
}

// Close shuts down the MCP client and its transport.
func (c *Client) Close() error {
	return c.transport.Close()
}

// Spec returns the tool spec this client was created from.
func (c *Client) Spec() *models.ToolSpec {
	return c.spec
}

func (c *Client) newRequest(method string, params any) *Request {
	id := c.requestID.Add(1)
	return &Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
}

// NotificationHandler is called when a notification is received from the server.
type NotificationHandler func(*Notification)

// StdioTransport manages a child process communicating via stdio.
type StdioTransport struct {
	cmd        *osexec.Cmd
	stdin      io.WriteCloser
	reader     *bufio.Scanner
	mu         sync.Mutex
	onNotify   NotificationHandler
}

// sandboxEnabled controls whether MCP server subprocesses are sandboxed.
// Set to false via --no-sandbox flag or sandbox: false in config.
var sandboxEnabled = true

// strictSandbox controls whether sandbox setup failures abort the process.
// Defaults to true (fail-closed).
var strictSandbox = true

// SetSandboxEnabled controls process sandboxing for MCP servers.
func SetSandboxEnabled(enabled bool) {
	sandboxEnabled = enabled
}

// SetStrictSandbox controls whether sandbox setup failures are fatal.
func SetStrictSandbox(strict bool) {
	strictSandbox = strict
}

// newStdioCommand wraps os/exec.Command for testability.
var newStdioCommand = func(name string, args ...string) *osexec.Cmd {
	return osexec.Command(name, args...)
}

func newStdioTransport(spec *models.ToolSpec) (*StdioTransport, error) {
	srv := spec.Server
	if srv.Command == "" {
		return nil, fmt.Errorf("stdio transport requires a command")
	}

	cmd := newStdioCommand(srv.Command, srv.Args...)
	cmd.Stderr = os.Stderr

	// Build sandbox policy for process isolation
	policy := sandbox.NewPolicy(spec, sandboxEnabled)
	policy.Strict = strictSandbox

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	// ApplyAndStart sets env vars (scrubbed allowlist) and applies
	// platform-specific filesystem isolation before starting the process.
	if err := sandbox.ApplyAndStart(context.Background(), cmd, policy); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("starting MCP server %q: %w", srv.Command, err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		reader: scanner,
	}, nil
}

// Send transmits a JSON-RPC request and returns the response.
func (t *StdioTransport) Send(ctx context.Context, req *Request) (*Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	if _, err := t.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("writing request: %w", err)
	}

	// Read response lines until we get one matching our request ID
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if !t.reader.Scan() {
			if err := t.reader.Err(); err != nil {
				return nil, fmt.Errorf("reading response: %w", err)
			}
			return nil, fmt.Errorf("EOF from MCP server")
		}

		line := t.reader.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Try to parse as a JSON-RPC message. Could be a response or notification.
		var msg struct {
			JSONRPC string      `json:"jsonrpc"`
			ID      interface{} `json:"id,omitempty"`
			Method  string      `json:"method,omitempty"`
			Params  interface{} `json:"params,omitempty"`
			Result  interface{} `json:"result,omitempty"`
			Error   *Error      `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue // skip non-JSON lines (e.g., server logging)
		}

		// If it has a method but no ID, it is a notification. Queue it.
		if msg.Method != "" && msg.ID == nil {
			if t.onNotify != nil {
				t.onNotify(&Notification{
					JSONRPC: msg.JSONRPC,
					Method:  msg.Method,
					Params:  msg.Params,
				})
			}
			continue
		}

		// Match response to request by ID
		if msg.ID != nil && req.ID != nil {
			respID, _ := json.Marshal(msg.ID)
			reqID, _ := json.Marshal(req.ID)
			if string(respID) == string(reqID) {
				return &Response{
					JSONRPC: msg.JSONRPC,
					ID:      msg.ID,
					Result:  msg.Result,
					Error:   msg.Error,
				}, nil
			}
		}
	}
}

// Notify sends a JSON-RPC notification (no response expected).
func (t *StdioTransport) Notify(_ context.Context, notif *Notification) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshaling notification: %w", err)
	}

	if _, err := t.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing notification: %w", err)
	}
	return nil
}

// Close shuts down the transport connection.
func (t *StdioTransport) Close() error {
	t.stdin.Close()
	done := make(chan error, 1)
	go func() {
		done <- t.cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.cmd.Process.Kill()
		return fmt.Errorf("MCP server did not exit within 5s, killed")
	}
}

// HTTPTransport communicates with an HTTP-based MCP server.
type HTTPTransport struct {
	url     string
	headers map[string]string
	client  *http.Client
	spec    *models.ToolSpec
}

func newHTTPTransport(spec *models.ToolSpec) (*HTTPTransport, error) {
	srv := spec.Server
	if srv.URL == "" {
		return nil, fmt.Errorf("http transport requires a URL")
	}

	headers := make(map[string]string)
	for k, v := range srv.Headers {
		headers[k] = v
	}

	// Inject auth credentials as headers for HTTP MCP transports.
	// 1.0 format: auth.header is "HeaderName: ${ENV_VAR}" template.
	if spec.Auth != nil && spec.Auth.Header != "" {
		resolved := spec.Auth.Header
		for _, envName := range spec.Auth.Env {
			val := os.Getenv(envName)
			resolved = strings.ReplaceAll(resolved, "${"+envName+"}", val)
		}
		if idx := strings.Index(resolved, ": "); idx > 0 {
			headers[resolved[:idx]] = resolved[idx+2:]
		}
	}

	return &HTTPTransport{
		url:     srv.URL,
		headers: headers,
		client:  &http.Client{Timeout: srv.TimeoutDuration()},
		spec:    spec,
	}, nil
}

// Send transmits a JSON-RPC request and returns the response.
func (t *HTTPTransport) Send(ctx context.Context, req *Request) (*Response, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	httpResp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending HTTP request: %w", err)
	}
	defer httpResp.Body.Close()

	// Limit response body to 10 MiB to prevent memory exhaustion from malicious servers
	body, err := io.ReadAll(io.LimitReader(httpResp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("reading HTTP response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP server returned %d: %s", httpResp.StatusCode, string(body))
	}

	var resp Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing HTTP response: %w", err)
	}

	return &resp, nil
}

// Notify sends a JSON-RPC notification (no response expected).
func (t *HTTPTransport) Notify(ctx context.Context, notif *Notification) error {
	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshaling notification: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Close shuts down the transport connection.
func (t *HTTPTransport) Close() error {
	return nil
}

// AdHocSpec is a minimal spec for ad-hoc MCP server connections (e.g., discover command).
type AdHocSpec struct {
	URL string
}

// NewAdHocHTTPClient creates an MCP client for an ad-hoc HTTP URL (not from registry).
func NewAdHocHTTPClient(spec *AdHocSpec) (*Client, error) {
	transport := &HTTPTransport{
		url:     spec.URL,
		headers: map[string]string{},
		client:  &http.Client{Timeout: 30 * time.Second},
	}
	return &Client{
		transport:     transport,
		notifications: make(chan *Notification, notificationBufferSize),
	}, nil
}
