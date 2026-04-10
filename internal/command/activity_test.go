// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestActivityCommand(t *testing.T) {
	// Mock API returning personal activity entries.
	entries := activityResponse{
		Results: []activityEntry{
			{
				ID:        "aaa-111",
				ToolName:  "anthropic/github",
				Action:    "list_repos",
				Status:    "success",
				Timestamp: "2026-03-30T10:00:00Z",
			},
			{
				ID:        "bbb-222",
				ToolName:  "anthropic/slack",
				Action:    "send_message",
				Status:    "error",
				Timestamp: "2026-03-30T09:50:00Z",
			},
		},
		NextCursor: nil,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header.
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}))
	defer srv.Close()

	// Call fetchActivityJSON directly with the mock server URL.
	data, err := fetchActivityJSON(
		t.Context(),
		srv.URL+"/api/v1/me/logs/?page_size=25",
		"test-token",
	)
	if err != nil {
		t.Fatalf("fetchActivityJSON failed: %v", err)
	}

	var resp activityResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
	if resp.Results[0].ToolName != "anthropic/github" {
		t.Errorf("expected first entry tool=anthropic/github, got %q", resp.Results[0].ToolName)
	}
	if resp.Results[1].Status != "error" {
		t.Errorf("expected second entry status=error, got %q", resp.Results[1].Status)
	}
}

func TestActivityWorkspaceFlag(t *testing.T) {
	// Mock API returning workspace-wide activity with user info.
	entries := activityResponse{
		Results: []activityEntry{
			{
				ID:        "ws-001",
				ToolName:  "anthropic/github",
				Action:    "create_issue",
				Status:    "success",
				UserEmail: "alice@example.com",
				Timestamp: "2026-03-30T11:00:00Z",
			},
			{
				ID:        "ws-002",
				ToolName:  "anthropic/slack",
				Action:    "post_message",
				Status:    "success",
				UserEmail: "bob@example.com",
				Timestamp: "2026-03-30T10:55:00Z",
			},
		},
		NextCursor: nil,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the path includes the workspace slug.
		if r.URL.Path != "/api/v1/workspaces/acme/logs/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer ws-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}))
	defer srv.Close()

	data, err := fetchActivityJSON(
		t.Context(),
		srv.URL+"/api/v1/workspaces/acme/logs/?page_size=25",
		"ws-token",
	)
	if err != nil {
		t.Fatalf("fetchActivityJSON for workspace failed: %v", err)
	}

	var resp activityResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}

	// Workspace activity should include user email.
	if resp.Results[0].UserEmail != "alice@example.com" {
		t.Errorf("expected user_email=alice@example.com, got %q", resp.Results[0].UserEmail)
	}
	if resp.Results[1].UserEmail != "bob@example.com" {
		t.Errorf("expected user_email=bob@example.com, got %q", resp.Results[1].UserEmail)
	}
}

func TestActivityResponseParsing(t *testing.T) {
	// Test that activityEntry correctly handles optional fields.
	raw := `{
		"id": "test-123",
		"tool_name": "github-mcp",
		"action": "",
		"status": "success",
		"latency_ms": 42,
		"client": "clictl/v1.0.0",
		"error": "",
		"timestamp": "2026-03-30T12:00:00Z",
		"workspace_slug": "my-ws",
		"user_email": "dev@example.com"
	}`

	var entry activityEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if entry.ToolName != "github-mcp" {
		t.Errorf("tool_name: got %q, want github-mcp", entry.ToolName)
	}
	if entry.LatencyMs == nil || *entry.LatencyMs != 42 {
		t.Errorf("latency_ms: got %v, want 42", entry.LatencyMs)
	}
	if entry.Client != "clictl/v1.0.0" {
		t.Errorf("client: got %q, want clictl/v1.0.0", entry.Client)
	}
	if entry.WorkspaceSlug != "my-ws" {
		t.Errorf("workspace_slug: got %q, want my-ws", entry.WorkspaceSlug)
	}
}

func TestFormatActivityTime(t *testing.T) {
	// Valid RFC3339 should be formatted.
	result := formatActivityTime("2026-03-30T12:00:00Z")
	if result == "2026-03-30T12:00:00Z" {
		t.Error("expected formatted time, got raw RFC3339 string")
	}
	if result == "" {
		t.Error("expected non-empty formatted time")
	}

	// Invalid timestamp should be returned as-is.
	invalid := formatActivityTime("not-a-timestamp")
	if invalid != "not-a-timestamp" {
		t.Errorf("expected raw string back for invalid timestamp, got %q", invalid)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "he..."},
		{"hi", 2, "hi"},
		{"abcdef", 3, "abc"},
		{"abcdefgh", 6, "abc..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d): got %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestActivityUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"detail": "Invalid token"}`))
	}))
	defer srv.Close()

	_, err := fetchActivityJSON(t.Context(), srv.URL+"/api/v1/me/logs/", "bad-token")
	if err == nil {
		t.Fatal("expected error for unauthorized response, got nil")
	}
}
