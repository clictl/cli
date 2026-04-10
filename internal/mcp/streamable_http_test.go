// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewStreamableHTTPTransport_RejectHTTP(t *testing.T) {
	_, err := NewStreamableHTTPTransport("http://example.com/mcp")
	if err == nil {
		t.Fatal("expected error for http:// URL")
	}
	if !strings.Contains(err.Error(), "non-TLS") {
		t.Errorf("expected non-TLS error, got: %v", err)
	}
}

func TestNewStreamableHTTPTransport_AllowHTTPInsecure(t *testing.T) {
	_, err := NewStreamableHTTPTransport("http://example.com/mcp", WithInsecure(true))
	if err != nil {
		t.Fatalf("unexpected error with insecure: %v", err)
	}
}

func TestNewStreamableHTTPTransport_RejectBadScheme(t *testing.T) {
	_, err := NewStreamableHTTPTransport("ftp://example.com/mcp")
	if err == nil {
		t.Fatal("expected error for ftp:// URL")
	}
}

func TestStreamableHTTPTransport_SendJSON(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type")
		}

		var req Request
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "test-session-123")
		json.NewEncoder(w).Encode(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]string{"status": "ok"},
		})
	}))
	defer srv.Close()

	transport := newTestTLSTransport(srv)

	resp, err := transport.Send(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error in response: %v", resp.Error)
	}

	// Verify session was stored
	transport.mu.Lock()
	sid := transport.sessionID
	transport.mu.Unlock()
	if sid != "test-session-123" {
		t.Errorf("expected session ID 'test-session-123', got %q", sid)
	}
}

func TestStreamableHTTPTransport_SendSSE(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		json.NewDecoder(r.Body).Decode(&req)

		resp := Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]string{"from": "sse"},
		}
		respBytes, _ := json.Marshal(resp)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(respBytes))
	}))
	defer srv.Close()

	transport := newTestTLSTransport(srv)

	resp, err := transport.Send(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error in response: %v", resp.Error)
	}
}

func TestStreamableHTTPTransport_FallbackOn404(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	transport := newTestTLSTransport(srv)

	_, err := transport.Send(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "test",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsFallbackError(err) {
		t.Errorf("expected FallbackError, got: %v", err)
	}
}

func TestStreamableHTTPTransport_FallbackOn405(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer srv.Close()

	transport := newTestTLSTransport(srv)

	_, err := transport.Send(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "test",
	})
	if !IsFallbackError(err) {
		t.Errorf("expected FallbackError, got: %v", err)
	}
}

func TestStreamableHTTPTransport_ProtocolVersionHeader(t *testing.T) {
	var receivedHeaders http.Header
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", ID: float64(1)})
	}))
	defer srv.Close()

	transport := newTestTLSTransport(srv)
	transport.protocolVer = "2025-03-26"
	transport.sessionID = "sess-42"

	transport.Send(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "test",
	})

	if receivedHeaders.Get("Mcp-Protocol-Version") != "2025-03-26" {
		t.Errorf("expected Mcp-Protocol-Version header, got %q", receivedHeaders.Get("Mcp-Protocol-Version"))
	}
	if receivedHeaders.Get("Mcp-Session-Id") != "sess-42" {
		t.Errorf("expected Mcp-Session-Id header, got %q", receivedHeaders.Get("Mcp-Session-Id"))
	}
}

func TestStreamableHTTPTransport_Close(t *testing.T) {
	var deleteReceived bool
	var deleteSessionID string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteReceived = true
			deleteSessionID = r.Header.Get("Mcp-Session-Id")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", ID: float64(1)})
	}))
	defer srv.Close()

	transport := newTestTLSTransport(srv)
	transport.sessionID = "cleanup-session"

	transport.Close()

	if !deleteReceived {
		t.Error("expected DELETE request for session cleanup")
	}
	if deleteSessionID != "cleanup-session" {
		t.Errorf("expected session ID 'cleanup-session', got %q", deleteSessionID)
	}

	// Verify transport is marked closed
	transport.mu.Lock()
	closed := transport.closed
	transport.mu.Unlock()
	if !closed {
		t.Error("expected transport to be marked closed")
	}
}

func TestStreamableHTTPTransport_ClosedRejectsSend(t *testing.T) {
	transport := &StreamableHTTPTransport{
		endpoint: "https://example.com/mcp",
		headers:  make(map[string]string),
		closed:   true,
	}

	_, err := transport.Send(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "test",
	})
	if err == nil {
		t.Fatal("expected error from closed transport")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("expected closed error, got: %v", err)
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"172.32.0.1", false},
		{"192.168.1.1", true},
		{"192.168.0.0", true},
		{"169.254.1.1", true},
		{"127.0.0.1", true},
		{"127.0.0.2", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"::1", true},
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("failed to parse IP: %s", tt.ip)
		}
		got := isPrivateIP(ip)
		if got != tt.private {
			t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
		}
	}
}

func TestIsFallbackError(t *testing.T) {
	err := &FallbackError{StatusCode: 404}
	if !IsFallbackError(err) {
		t.Error("expected IsFallbackError to return true")
	}

	if IsFallbackError(fmt.Errorf("some other error")) {
		t.Error("expected IsFallbackError to return false for non-FallbackError")
	}
}

func TestSessionExpiry404ReInit(t *testing.T) {
	// First doPost returns a session-expired error (-32000), triggering
	// the Send method to clear the session, re-initialize, and retry.
	var callCount int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var req Request
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			// First call: return session-expired error
			json.NewEncoder(w).Encode(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &Error{Code: -32000, Message: "session expired"},
			})
			return
		}

		if callCount == 2 {
			// Re-init call: return success with new session
			w.Header().Set("Mcp-Session-Id", "new-session-abc")
			w.Header().Set("Mcp-Protocol-Version", "2025-03-26")
			json.NewEncoder(w).Encode(Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"protocolVersion": "2025-03-26",
					"serverInfo":      map[string]string{"name": "test-server"},
				},
			})
			return
		}

		// Third call: retried original request succeeds
		json.NewEncoder(w).Encode(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"retried": true},
		})
	}))
	defer srv.Close()

	transport := newTestTLSTransport(srv)
	transport.sessionID = "expired-session"

	resp, err := transport.Send(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      float64(99),
		Method:  "tools/list",
	})
	if err != nil {
		t.Fatalf("Send failed after re-init: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error in retried response: %v", resp.Error)
	}

	// Should have made 3 HTTP calls: original, re-init, retry
	if callCount != 3 {
		t.Errorf("expected 3 server calls, got %d", callCount)
	}

	// Session ID should be updated from the re-init response
	transport.mu.Lock()
	sid := transport.sessionID
	transport.mu.Unlock()
	if sid != "new-session-abc" {
		t.Errorf("expected updated sessionID 'new-session-abc', got %q", sid)
	}
}

func TestStreamableHTTPTransport_CloseNoSessionSkipsDelete(t *testing.T) {
	var deleteReceived bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteReceived = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport := newTestTLSTransport(srv)

	transport.Close()

	if deleteReceived {
		t.Error("DELETE should not be sent when sessionID is empty")
	}
}

func TestStreamableHTTPTransport_DoubleCloseIdempotent(t *testing.T) {
	transport := &StreamableHTTPTransport{
		endpoint:   "https://example.com/mcp",
		headers:    make(map[string]string),
		httpClient: http.DefaultClient,
	}

	if err := transport.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("second Close should be idempotent, got: %v", err)
	}
}

func TestWithHeaders(t *testing.T) {
	transport := &StreamableHTTPTransport{
		endpoint: "https://example.com/mcp",
		headers:  make(map[string]string),
	}

	opt := WithHeaders(map[string]string{
		"Authorization": "Bearer tok",
		"X-Custom":      "val",
	})
	opt(transport)

	if transport.headers["Authorization"] != "Bearer tok" {
		t.Errorf("expected Authorization 'Bearer tok', got %q", transport.headers["Authorization"])
	}
	if transport.headers["X-Custom"] != "val" {
		t.Errorf("expected X-Custom 'val', got %q", transport.headers["X-Custom"])
	}
}

func TestStreamableHTTPTransport_NotifyOnClosedTransport(t *testing.T) {
	transport := &StreamableHTTPTransport{
		endpoint:   "https://example.com/mcp",
		headers:    make(map[string]string),
		httpClient: http.DefaultClient,
		closed:     true,
	}

	err := transport.Notify(context.Background(), &Notification{
		JSONRPC: "2.0",
		Method:  "initialized",
	})
	if err == nil {
		t.Fatal("expected error from closed transport")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("expected 'closed' in error, got: %v", err)
	}
}

func TestStreamableHTTPTransport_CustomHeaders(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", ID: float64(1)})
	}))
	defer srv.Close()

	transport := newTestTLSTransport(srv)
	transport.headers["Authorization"] = "Bearer secret-token"

	transport.Send(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "test",
	})

	if receivedAuth != "Bearer secret-token" {
		t.Errorf("expected Authorization header, got %q", receivedAuth)
	}
}

func TestStreamableHTTPTransport_ServerError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	transport := newTestTLSTransport(srv)

	_, err := transport.doPost(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "test",
	})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to contain status code, got: %v", err)
	}
}

func TestIsPrivateIP_Extended(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		// IPv6 private ranges
		{"fc00::1", true},
		{"fd00::1", true},
		{"fe80::1", true},

		// IPv6 public
		{"2001:db8::1", false},
		{"2606:4700::1", false},

		// Edge cases: boundary of private ranges
		{"10.0.0.0", true},
		{"9.255.255.255", false},
		{"172.15.255.255", false},
		{"172.16.0.0", true},
		{"192.167.255.255", false},
		{"192.168.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("could not parse IP %q", tt.ip)
			}
			got := isPrivateIP(ip)
			if got != tt.private {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
			}
		})
	}
}
