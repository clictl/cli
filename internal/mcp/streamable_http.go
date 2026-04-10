// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// maxResponseBytes is the maximum response body size (10 MiB).
const maxResponseBytes = 10 * 1024 * 1024

// StreamableHTTPTransport implements the MCP Streamable HTTP transport.
// It POSTs JSON-RPC messages to a single endpoint and handles both
// direct JSON and SSE response types. Session management, protocol
// version negotiation, and security checks are built in.
type StreamableHTTPTransport struct {
	endpoint    string
	sessionID   string
	protocolVer string
	httpClient  *http.Client
	headers     map[string]string
	closed      bool
	insecure    bool // allow http:// URLs
	mu          sync.Mutex
}

// StreamableHTTPOption configures a StreamableHTTPTransport.
type StreamableHTTPOption func(*StreamableHTTPTransport)

// WithInsecure allows non-TLS (http://) connections.
func WithInsecure(insecure bool) StreamableHTTPOption {
	return func(t *StreamableHTTPTransport) {
		t.insecure = insecure
	}
}

// WithHeaders sets additional HTTP headers on all requests.
func WithHeaders(headers map[string]string) StreamableHTTPOption {
	return func(t *StreamableHTTPTransport) {
		for k, v := range headers {
			t.headers[k] = v
		}
	}
}

// NewStreamableHTTPTransport creates a Streamable HTTP transport for the given
// endpoint URL. By default, http:// URLs are rejected unless WithInsecure(true)
// is passed. The resolved IP is checked against private ranges to prevent SSRF.
func NewStreamableHTTPTransport(endpoint string, opts ...StreamableHTTPOption) (*StreamableHTTPTransport, error) {
	t := &StreamableHTTPTransport{
		endpoint: endpoint,
		headers:  make(map[string]string),
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				DialContext: ssrfSafeDialer,
			},
		},
	}

	for _, opt := range opts {
		opt(t)
	}

	// M3.7: reject non-TLS by default
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing endpoint URL: %w", err)
	}
	if parsed.Scheme == "http" && !t.insecure {
		return nil, fmt.Errorf("refusing non-TLS endpoint %q (use --insecure to override)", endpoint)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme %q, expected http or https", parsed.Scheme)
	}

	return t, nil
}

// Send posts a JSON-RPC request to the endpoint and returns the response.
// It handles both application/json and text/event-stream response types.
// On 404, it attempts session re-initialization before retrying once.
func (t *StreamableHTTPTransport) Send(ctx context.Context, req *Request) (*Response, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport is closed")
	}
	t.mu.Unlock()

	resp, err := t.doPost(ctx, req)
	if err != nil {
		return nil, err
	}

	// M3.3: detect expired session (404) and re-initialize
	if resp.Error != nil && resp.Error.Code == -32000 {
		t.mu.Lock()
		t.sessionID = ""
		t.protocolVer = ""
		t.mu.Unlock()

		// Re-initialize: send a fresh initialize request
		initReq := &Request{
			JSONRPC: "2.0",
			ID:      req.ID,
			Method:  "initialize",
			Params: InitializeParams{
				ProtocolVersion: "2025-03-26",
				Capabilities:    Capability{},
				ClientInfo: Info{
					Name:    "clictl",
					Version: "1.0.0",
				},
			},
		}
		initResp, initErr := t.doPost(ctx, initReq)
		if initErr != nil {
			return nil, fmt.Errorf("session re-initialization failed: %w", initErr)
		}
		// Store new session info from init response
		_ = initResp

		// Retry the original request
		return t.doPost(ctx, req)
	}

	return resp, nil
}

// doPost performs the actual HTTP POST and parses the response.
func (t *StreamableHTTPTransport) doPost(ctx context.Context, req *Request) (*Response, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	// M3.4: send protocol version on all requests after init
	t.mu.Lock()
	if t.protocolVer != "" {
		httpReq.Header.Set("Mcp-Protocol-Version", t.protocolVer)
	}
	if t.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	t.mu.Unlock()

	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	httpResp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending HTTP request: %w", err)
	}
	defer httpResp.Body.Close()

	// M3.3: store session ID from response header
	if sid := httpResp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		t.sessionID = sid
		t.mu.Unlock()
	}

	// Store protocol version from initialize response
	if pv := httpResp.Header.Get("Mcp-Protocol-Version"); pv != "" {
		t.mu.Lock()
		t.protocolVer = pv
		t.mu.Unlock()
	}

	// M3.6: fallback on 404/405 to legacy SSE
	if httpResp.StatusCode == http.StatusNotFound || httpResp.StatusCode == http.StatusMethodNotAllowed {
		return nil, &FallbackError{StatusCode: httpResp.StatusCode}
	}

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBytes))
		return nil, fmt.Errorf("MCP server returned %d: %s", httpResp.StatusCode, string(body))
	}

	contentType := httpResp.Header.Get("Content-Type")

	// Handle SSE response
	if strings.HasPrefix(contentType, "text/event-stream") {
		return t.readSSEResponse(httpResp.Body, req)
	}

	// Handle JSON response (default)
	body, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("reading HTTP response: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing HTTP response: %w", err)
	}

	return &resp, nil
}

// readSSEResponse reads a text/event-stream body and extracts the JSON-RPC
// response matching the given request ID.
func (t *StreamableHTTPTransport) readSSEResponse(body io.Reader, req *Request) (*Response, error) {
	limitedBody := io.LimitReader(body, maxResponseBytes)
	done := make(chan struct{})
	defer close(done)

	reqIDBytes, _ := json.Marshal(req.ID)

	for event := range ParseSSE(limitedBody, done) {
		if event.Event == "error" {
			return nil, formatSSEError(event)
		}

		if event.Data == "" {
			continue
		}

		// Try to parse as JSON-RPC response
		var msg struct {
			JSONRPC string      `json:"jsonrpc"`
			ID      interface{} `json:"id,omitempty"`
			Method  string      `json:"method,omitempty"`
			Result  interface{} `json:"result,omitempty"`
			Error   *Error      `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(event.Data), &msg); err != nil {
			continue
		}

		// Skip notifications
		if msg.Method != "" && msg.ID == nil {
			continue
		}

		// Match response to request by ID
		if msg.ID != nil {
			respID, _ := json.Marshal(msg.ID)
			if string(respID) == string(reqIDBytes) {
				return &Response{
					JSONRPC: msg.JSONRPC,
					ID:      msg.ID,
					Result:  msg.Result,
					Error:   msg.Error,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("SSE stream ended without response for request %v", req.ID)
}

// Notify sends a JSON-RPC notification (no response expected).
func (t *StreamableHTTPTransport) Notify(ctx context.Context, notif *Notification) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("transport is closed")
	}
	t.mu.Unlock()

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshaling notification: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	t.mu.Lock()
	if t.protocolVer != "" {
		httpReq.Header.Set("Mcp-Protocol-Version", t.protocolVer)
	}
	if t.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	t.mu.Unlock()

	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("sending notification: %w", err)
	}
	resp.Body.Close()
	return nil
}

// Close sends an HTTP DELETE to the endpoint to clean up the server-side
// session, then marks the transport as closed.
func (t *StreamableHTTPTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	sessionID := t.sessionID
	endpoint := t.endpoint
	protocolVer := t.protocolVer
	t.mu.Unlock()

	// M3.5: send DELETE to clean up session
	if sessionID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
		if err != nil {
			return nil
		}
		req.Header.Set("Mcp-Session-Id", sessionID)
		if protocolVer != "" {
			req.Header.Set("Mcp-Protocol-Version", protocolVer)
		}
		resp, err := t.httpClient.Do(req)
		if err != nil {
			return nil // Best-effort cleanup
		}
		resp.Body.Close()
	}
	return nil
}

// FallbackError indicates that the server does not support Streamable HTTP
// and the caller should try the legacy SSE transport.
type FallbackError struct {
	StatusCode int
}

// Error returns the error string.
func (e *FallbackError) Error() string {
	return fmt.Sprintf("server returned %d, try legacy SSE transport", e.StatusCode)
}

// IsFallbackError returns true if the error indicates a fallback is needed.
func IsFallbackError(err error) bool {
	_, ok := err.(*FallbackError)
	return ok
}

// isPrivateIP returns true if the given IP address is in a private or
// reserved range that should not be accessed by remote MCP servers.
// This blocks SSRF attacks against internal infrastructure.
func isPrivateIP(ip net.IP) bool {
	// Normalize IPv4-mapped IPv6 addresses
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}

	privateRanges := []struct {
		network string
	}{
		{"10.0.0.0/8"},
		{"172.16.0.0/12"},
		{"192.168.0.0/16"},
		{"169.254.0.0/16"},
		{"127.0.0.0/8"},
		{"::1/128"},
		{"fc00::/7"},
		{"fe80::/10"},
	}

	for _, r := range privateRanges {
		_, cidr, err := net.ParseCIDR(r.network)
		if err != nil {
			continue
		}
		if cidr.Contains(ip) {
			return true
		}
	}

	return false
}

// ssrfSafeDialer is a DialContext function that checks resolved IPs against
// private ranges before establishing a connection.
func ssrfSafeDialer(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("splitting host:port: %w", err)
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", host, err)
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses found for %s", host)
	}

	for _, ip := range ips {
		if isPrivateIP(ip.IP) {
			return nil, fmt.Errorf("SSRF protection: refusing to connect to private IP %s (resolved from %s)", ip.IP, host)
		}
	}

	// Use the first non-private IP to connect
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}
