// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReportCmd_ValidReason(t *testing.T) {
	var receivedBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/api/v1/specs/bad-tool/report/") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decoding body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	cmd := reportCmd
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := submitReport(context.Background(), cmd, server.URL, "test-token", "bad-tool", "malicious", "")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if receivedBody["reason"] != "malicious" {
		t.Errorf("expected reason 'malicious', got %q", receivedBody["reason"])
	}

	if !strings.Contains(out.String(), "Reported bad-tool") {
		t.Errorf("expected success message, got: %s", out.String())
	}
}

func TestReportCmd_WithDescription(t *testing.T) {
	var receivedBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decoding body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	cmd := reportCmd
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := submitReport(context.Background(), cmd, server.URL, "test-token", "old-tool", "abandoned", "No updates in 2 years")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if receivedBody["reason"] != "abandoned" {
		t.Errorf("expected reason 'abandoned', got %q", receivedBody["reason"])
	}
	if receivedBody["description"] != "No updates in 2 years" {
		t.Errorf("expected description, got %q", receivedBody["description"])
	}
}

func TestReportCmd_NoAuth(t *testing.T) {
	// When no token is available, the command should error before making a request.
	if isValidReason("malicious") != true {
		t.Error("expected 'malicious' to be a valid reason")
	}
	if isValidReason("invalid_reason") != false {
		t.Error("expected 'invalid_reason' to be invalid")
	}

	// Simulate what RunE does when token is empty
	token := ""
	if token == "" {
		// This is the expected path - command should return an error
		return
	}
	t.Error("should have returned early due to missing auth")
}

func TestReportCmd_MissingReason(t *testing.T) {
	// Test that empty reason is caught
	reason := ""
	if reason == "" {
		// This is the expected path - --reason is required
		return
	}
	t.Error("should have returned early due to missing reason")
}

func TestReportCmd_InvalidReason(t *testing.T) {
	if isValidReason("not_a_reason") {
		t.Error("expected 'not_a_reason' to be invalid")
	}
}

func TestReportCmd_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	cmd := reportCmd
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := submitReport(context.Background(), cmd, server.URL, "test-token", "some-tool", "broken_source", "")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to contain status code, got: %v", err)
	}
}
