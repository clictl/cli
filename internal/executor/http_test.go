// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/models"
)

func TestMain(m *testing.M) {
	// Allow private/loopback base URLs in tests since httptest.NewServer
	// binds to 127.0.0.1.
	allowPrivateBaseURL = true
	os.Exit(m.Run())
}

func TestBuildURL_SimpleQuery(t *testing.T) {
	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: "https://api.example.com"},
	}
	action := &models.Action{
		Path: "/data",
		Params: []models.Param{
			{Name: "q", Type: "string", In: "query", Required: true},
			{Name: "limit", Type: "int", In: "query", Default: "10"},
		},
	}
	params := map[string]string{"q": "test"}

	got, err := buildURL(spec.Server.URL, action, params)
	if err != nil {
		t.Fatalf("buildURL: %v", err)
	}
	want := "https://api.example.com/data?q=test&limit=10"
	if got != want {
		t.Errorf("buildURL: got %q, want %q", got, want)
	}
}

func TestBuildURL_PathParams(t *testing.T) {
	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: "https://api.example.com"},
	}
	action := &models.Action{
		Path: "/repos/{owner}/{repo}",
		Params: []models.Param{
			{Name: "owner", Type: "string", In: "path", Required: true},
			{Name: "repo", Type: "string", In: "path", Required: true},
		},
	}
	params := map[string]string{"owner": "clictl", "repo": "cli"}

	got, err := buildURL(spec.Server.URL, action, params)
	if err != nil {
		t.Fatalf("buildURL: %v", err)
	}
	want := "https://api.example.com/repos/clictl/cli"
	if got != want {
		t.Errorf("buildURL: got %q, want %q", got, want)
	}
}

func TestBuildURL_MissingRequiredParam(t *testing.T) {
	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: "https://api.example.com"},
	}
	action := &models.Action{
		Path: "/data",
		Params: []models.Param{
			{Name: "q", Type: "string", In: "query", Required: true},
		},
	}

	_, err := buildURL(spec.Server.URL, action, map[string]string{})
	if err == nil {
		t.Fatal("buildURL missing required: expected error")
	}
}

func TestBuildURL_TrailingSlashOnBase(t *testing.T) {
	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: "https://api.example.com/"},
	}
	action := &models.Action{Path: "/data"}

	got, err := buildURL(spec.Server.URL, action, nil)
	if err != nil {
		t.Fatalf("buildURL: %v", err)
	}
	want := "https://api.example.com/data"
	if got != want {
		t.Errorf("buildURL trailing slash: got %q, want %q", got, want)
	}
}

func TestBuildURL_OptionalParamOmitted(t *testing.T) {
	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: "https://api.example.com"},
	}
	action := &models.Action{
		Path: "/data",
		Params: []models.Param{
			{Name: "optional", Type: "string", In: "query"},
		},
	}

	got, err := buildURL(spec.Server.URL, action, map[string]string{})
	if err != nil {
		t.Fatalf("buildURL: %v", err)
	}
	if got != "https://api.example.com/data" {
		t.Errorf("buildURL optional omitted: got %q", got)
	}
}

func TestApplyTransform_SimpleField(t *testing.T) {
	body := []byte(`{"main": {"temp": 20}, "wind": {"speed": 5}}`)
	got, err := applyTransform(body, "$.main")
	if err != nil {
		t.Fatalf("applyTransform: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Expected non-empty result")
	}
}

func TestApplyTransform_NestedField(t *testing.T) {
	body := []byte(`{"data": {"items": [1, 2, 3]}}`)
	got, err := applyTransform(body, "$.data.items")
	if err != nil {
		t.Fatalf("applyTransform: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Expected non-empty result")
	}
}

func TestApplyTransform_MissingField(t *testing.T) {
	body := []byte(`{"foo": "bar"}`)
	_, err := applyTransform(body, "$.nonexistent")
	if err == nil {
		t.Fatal("applyTransform missing field: expected error")
	}
}

func TestApplyTransform_NoOp(t *testing.T) {
	body := []byte(`{"foo": "bar"}`)
	got, err := applyTransform(body, "$")
	if err != nil {
		t.Fatalf("applyTransform noop: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("applyTransform noop: got %q, want %q", string(got), string(body))
	}
}

func TestHTTPExecutor_Execute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hello" {
			t.Errorf("Path: got %q, want /hello", r.URL.Path)
		}
		if r.URL.Query().Get("name") != "world" {
			t.Errorf("Query name: got %q, want world", r.URL.Query().Get("name"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message": "hello world"}`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name:    "hello",
		Method: "GET", Path: "/hello",
		Params: []models.Param{
			{Name: "name", Type: "string", In: "query", Required: true},
		},
	}

	exec := &HTTPExecutor{}
	result, err := exec.Execute(context.Background(), spec, action, map[string]string{"name": "world"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("Expected non-empty result")
	}
}

func TestHTTPExecutor_ExecuteWithTransform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data": {"value": 42}, "meta": {"count": 1}}`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name:    "get-data",
		Method: "GET", Path: "/data",
		Transform: []models.TransformStep{
			{Type: "json", Extract: "$.data"},
		},
	}

	exec := &HTTPExecutor{}
	result, err := exec.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Should contain "value": 42 but not "meta"
	s := string(result)
	if !contains(s, "42") {
		t.Errorf("Expected result to contain 42, got %q", s)
	}
}

func TestHTTPExecutor_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "not found"}`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{Name: "test", Method: "GET", Path: "/missing"}

	exec := &HTTPExecutor{}
	_, err := exec.Execute(context.Background(), spec, action, nil)
	if err == nil {
		t.Fatal("Expected error for 404")
	}
}

func TestDispatch_UnsupportedServerType(t *testing.T) {
	spec := &models.ToolSpec{Server: &models.Server{Type: "websocket"}}
	action := &models.Action{Name: "test"}
	_, err := Dispatch(context.Background(), spec, action, nil)
	if err == nil {
		t.Fatal("Expected error for unsupported server type")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- S2: Auth Tests ---

func TestApplyAuth_Nil(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	err := applyAuth(req, nil)
	if err != nil {
		t.Fatalf("applyAuth nil: %v", err)
	}
}

func TestApplyAuth_HeaderInjection(t *testing.T) {
	t.Setenv("MY_API_KEY", "secret123")
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	auth := &models.Auth{
		Env:    models.StringOrSlice{"MY_API_KEY"},
		Header: "Authorization: Bearer ${MY_API_KEY}",
	}
	err := applyAuth(req, auth)
	if err != nil {
		t.Fatalf("applyAuth header: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer secret123" {
		t.Errorf("Expected Authorization header %q, got %q", "Bearer secret123", got)
	}
}

func TestApplyAuth_QueryInjection(t *testing.T) {
	t.Setenv("MY_API_KEY", "key456")
	req, _ := http.NewRequest("GET", "https://example.com/api", nil)
	auth := &models.Auth{
		Env:   models.StringOrSlice{"MY_API_KEY"},
		Param: "api_key",
	}
	err := applyAuth(req, auth)
	if err != nil {
		t.Fatalf("applyAuth query: %v", err)
	}
	if got := req.URL.Query().Get("api_key"); got != "key456" {
		t.Errorf("Expected query param api_key=%q, got %q", "key456", got)
	}
}

func TestApplyAuth_HeaderTemplate(t *testing.T) {
	t.Setenv("MY_KEY", "abc")
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	auth := &models.Auth{
		Env:    models.StringOrSlice{"MY_KEY"},
		Header: "X-Api-Key: ${MY_KEY}",
	}
	err := applyAuth(req, auth)
	if err != nil {
		t.Fatalf("applyAuth header template: %v", err)
	}
	if got := req.Header.Get("X-Api-Key"); got != "abc" {
		t.Errorf("Expected X-Api-Key %q, got %q", "abc", got)
	}
}

func TestApplyAuth_BearerTemplate(t *testing.T) {
	t.Setenv("TOKEN", "sk-123")
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	auth := &models.Auth{
		Env:    models.StringOrSlice{"TOKEN"},
		Header: "Authorization: Bearer ${TOKEN}",
	}
	err := applyAuth(req, auth)
	if err != nil {
		t.Fatalf("applyAuth bearer: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-123" {
		t.Errorf("Expected 'Bearer sk-123', got %q", got)
	}
}

func TestApplyAuth_EnvNotSet(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	auth := &models.Auth{
		Env:    models.StringOrSlice{"NONEXISTENT_KEY_XYZ"},
		Header: "Authorization: Bearer ${NONEXISTENT_KEY_XYZ}",
	}
	err := applyAuth(req, auth)
	if err == nil {
		t.Fatal("applyAuth env not set: expected error")
	}
	if !contains(err.Error(), "NONEXISTENT_KEY_XYZ") {
		t.Errorf("Error should reference env var name, got: %s", err.Error())
	}
}

func TestApplyAuth_NilAuth(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	err := applyAuth(req, nil)
	if err != nil {
		t.Fatalf("applyAuth nil: %v", err)
	}
}

// --- S3: Safety Runtime Tests ---

func TestConfirmMutableAction_SafeMethods(t *testing.T) {
	for _, method := range []string{"GET", "HEAD", "OPTIONS", "get", "head", "options"} {
		action := &models.Action{Name: "test", Path: "/data"}
		confirmed := map[string]bool{}
		err := confirmMutableAction(method, action, confirmed)
		if err != nil {
			t.Errorf("confirmMutableAction(%s): expected nil, got %v", method, err)
		}
	}
}

func TestConfirmMutableAction_NotMutable(t *testing.T) {
	action := &models.Action{Name: "create", Path: "/items", Mutable: false}
	confirmed := map[string]bool{}
	err := confirmMutableAction("POST", action, confirmed)
	if err != nil {
		t.Errorf("confirmMutableAction POST mutable:false: expected nil, got %v", err)
	}
}

func TestConfirmMutableAction_AlreadyConfirmedPost(t *testing.T) {
	action := &models.Action{Name: "update", Path: "/items", Mutable: true}
	confirmed := map[string]bool{"update:POST": true}
	err := confirmMutableAction("POST", action, confirmed)
	if err != nil {
		t.Errorf("confirmMutableAction already confirmed POST: expected nil, got %v", err)
	}
}

func TestConfirmMutableAction_DeleteAlwaysRePrompts(t *testing.T) {
	action := &models.Action{Name: "remove", Path: "/items/{id}", Mutable: true}
	confirmed := map[string]bool{"remove:DELETE": true}
	// DELETE should re-prompt even if previously confirmed.
	// Since we cannot provide stdin input in tests, the Scanln will read "" and
	// the function should return an error (user did not confirm).
	err := confirmMutableAction("DELETE", action, confirmed)
	if err == nil {
		t.Error("confirmMutableAction DELETE re-prompt: expected error (no stdin), got nil")
	}
}

func TestConfirmMutableAction_PutNotConfirmed(t *testing.T) {
	action := &models.Action{Name: "update", Path: "/items", Mutable: true}
	confirmed := map[string]bool{}
	// No stdin, so Scanln reads "" and function returns error
	err := confirmMutableAction("PUT", action, confirmed)
	if err == nil {
		t.Error("confirmMutableAction PUT not confirmed: expected error, got nil")
	}
}

// --- S4: User-Agent and Disabled Tool Tests ---

func TestHTTPExecutor_UserAgentHeader(t *testing.T) {
	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	SetVersion("1.2.3")
	defer SetVersion("dev")

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{Name: "test", Method: "GET", Path: "/test"}

	exec := &HTTPExecutor{}
	_, err := exec.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := "clictl/1.2.3 (https://clictl.dev)"
	if gotUserAgent != want {
		t.Errorf("User-Agent: got %q, want %q", gotUserAgent, want)
	}
}

func TestHTTPExecutor_UserAgentDefault(t *testing.T) {
	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	SetVersion("dev")

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{Name: "test", Method: "GET", Path: "/test"}

	exec := &HTTPExecutor{}
	_, err := exec.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.HasPrefix(gotUserAgent, "clictl/dev") {
		t.Errorf("User-Agent should start with clictl/dev, got %q", gotUserAgent)
	}
}

func TestHTTPExecutor_UserAgentSpecOverride(t *testing.T) {
	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Server: &models.Server{
			Type:    "http",
			URL:     server.URL,
			Headers: map[string]string{"User-Agent": "custom-agent/1.0"},
		},
	}
	action := &models.Action{Name: "test", Method: "GET", Path: "/test"}

	exec := &HTTPExecutor{}
	_, err := exec.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotUserAgent != "custom-agent/1.0" {
		t.Errorf("User-Agent should be custom override, got %q", gotUserAgent)
	}
}

func TestHTTPExecutor_DisabledTool(t *testing.T) {
	cfg := &config.Config{
		DisabledTools: []string{"blocked-tool"},
	}

	spec := &models.ToolSpec{
		Name:   "blocked-tool",
		Server: &models.Server{Type: "http", URL: "https://example.com"},
	}
	action := &models.Action{Name: "test", Method: "GET", Path: "/test"}

	exec := &HTTPExecutor{Config: cfg}
	_, err := exec.Execute(context.Background(), spec, action, nil)
	if err == nil {
		t.Fatal("expected error for disabled tool")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("error should mention disabled, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "clictl enable") {
		t.Errorf("error should mention 'clictl enable', got: %s", err.Error())
	}
}

func TestHTTPExecutor_EnabledToolNotBlocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		DisabledTools: []string{"other-tool"},
	}

	spec := &models.ToolSpec{
		Name:   "allowed-tool",
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{Name: "test", Method: "GET", Path: "/test"}

	exec := &HTTPExecutor{Config: cfg}
	_, err := exec.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute should succeed for non-disabled tool: %v", err)
	}
}

func TestVerifyRequirements_Installed(t *testing.T) {
	spec := &models.ToolSpec{
		Name: "test-tool",
		Server: &models.Server{
			Type: "http",
			Requires: []models.Requirement{
				{Name: "ls", Check: "ls --version"},
			},
		},
	}
	err := verifyRequirements(spec)
	if err != nil {
		t.Errorf("verifyRequirements should pass for 'ls': %v", err)
	}
}

func TestVerifyRequirements_NotInstalled(t *testing.T) {
	spec := &models.ToolSpec{
		Name: "test-tool",
		Server: &models.Server{
			Type: "http",
			Requires: []models.Requirement{
				{Name: "nonexistent-binary-xyz", URL: "https://example.com/install"},
			},
		},
	}
	err := verifyRequirements(spec)
	if err == nil {
		t.Error("verifyRequirements should fail for missing binary")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error should mention 'not installed', got: %v", err)
	}
	if !strings.Contains(err.Error(), "https://example.com/install") {
		t.Errorf("error should include install URL, got: %v", err)
	}
}

func TestVerifyRequirements_Empty(t *testing.T) {
	spec := &models.ToolSpec{
		Name: "test-tool",
	}
	err := verifyRequirements(spec)
	if err != nil {
		t.Errorf("verifyRequirements should pass with no requirements: %v", err)
	}
}

func TestVerifyRequirements_InferFromRun(t *testing.T) {
	spec := &models.ToolSpec{
		Name:   "test-cli",
		Server: &models.Server{Type: "command"},
		Actions: []models.Action{
			{Name: "status", Run: "git status"},
		},
	}
	err := verifyRequirements(spec)
	if err != nil {
		t.Errorf("verifyRequirements should pass for 'git': %v", err)
	}
}

func TestVerifyRequirements_InferFromRunMissing(t *testing.T) {
	spec := &models.ToolSpec{
		Name:   "test-cli",
		Server: &models.Server{Type: "command"},
		Actions: []models.Action{
			{Name: "run", Run: "nonexistent-tool-xyz --version"},
		},
	}
	err := verifyRequirements(spec)
	if err == nil {
		t.Error("verifyRequirements should fail for missing binary inferred from run")
	}
	if !strings.Contains(err.Error(), "nonexistent-tool-xyz") {
		t.Errorf("error should mention the binary name, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Retry Tests
// ---------------------------------------------------------------------------

func TestHTTPExecutor_RetryOn429ThenSuccess(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error": "rate limited"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name:    "test",
		Method: "GET", Path: "/data",
		Retry: &models.Retry{
			On:          []int{429},
			MaxAttempts: 3,
			Backoff:     "fixed",
			Delay:       "10ms",
		},
	}

	exec := &HTTPExecutor{}
	result, err := exec.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	if !strings.Contains(string(result), "ok") {
		t.Errorf("expected success response, got %q", string(result))
	}
}

func TestHTTPExecutor_RetryExhausted(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error": "unavailable"}`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name:    "test",
		Method: "GET", Path: "/data",
		Retry: &models.Retry{
			On:          []int{503},
			MaxAttempts: 2,
			Backoff:     "fixed",
			Delay:       "10ms",
		},
	}

	exec := &HTTPExecutor{}
	_, err := exec.Execute(context.Background(), spec, action, nil)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error should mention 503, got: %s", err.Error())
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestHTTPExecutor_RetryDefaultConfig(t *testing.T) {
	// With nil Retry on action, defaults should apply (retry on 429, 500, 502, 503)
	rr := resolveRetryConfig(nil)
	if rr.maxAttempts != 3 {
		t.Errorf("default maxAttempts: got %d, want 3", rr.maxAttempts)
	}
	if rr.backoff != "exponential" {
		t.Errorf("default backoff: got %q, want exponential", rr.backoff)
	}
	if rr.delay != 1*time.Second {
		t.Errorf("default delay: got %v, want 1s", rr.delay)
	}
	if !rr.shouldRetry(429) || !rr.shouldRetry(500) || !rr.shouldRetry(502) || !rr.shouldRetry(503) {
		t.Error("default should retry on 429, 500, 502, 503")
	}
	if rr.shouldRetry(404) {
		t.Error("default should NOT retry on 404")
	}
}

func TestHTTPExecutor_RetryRespectsRetryAfterHeader(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0") // 0 seconds
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error": "rate limited"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name:    "test",
		Method: "GET", Path: "/data",
		Retry: &models.Retry{
			On:          []int{429},
			MaxAttempts: 2,
			Backoff:     "fixed",
			Delay:       "10ms",
		},
	}

	exec := &HTTPExecutor{}
	result, err := exec.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(result), "ok") {
		t.Errorf("expected success response, got %q", string(result))
	}
}

func TestRetryDelayForAttempt_Exponential(t *testing.T) {
	rr := resolvedRetry{delay: 1 * time.Second, backoff: "exponential"}
	if d := rr.delayForAttempt(1); d != 1*time.Second {
		t.Errorf("attempt 1: got %v, want 1s", d)
	}
	if d := rr.delayForAttempt(2); d != 2*time.Second {
		t.Errorf("attempt 2: got %v, want 2s", d)
	}
	if d := rr.delayForAttempt(3); d != 4*time.Second {
		t.Errorf("attempt 3: got %v, want 4s", d)
	}
}

func TestRetryDelayForAttempt_Linear(t *testing.T) {
	rr := resolvedRetry{delay: 500 * time.Millisecond, backoff: "linear"}
	if d := rr.delayForAttempt(1); d != 500*time.Millisecond {
		t.Errorf("attempt 1: got %v, want 500ms", d)
	}
	if d := rr.delayForAttempt(3); d != 500*time.Millisecond {
		t.Errorf("attempt 3: got %v, want 500ms", d)
	}
}

func TestRetryAfterDelay_Seconds(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "5")
	d := retryAfterDelay(h)
	if d != 5*time.Second {
		t.Errorf("got %v, want 5s", d)
	}
}

func TestRetryAfterDelay_Empty(t *testing.T) {
	h := http.Header{}
	d := retryAfterDelay(h)
	if d != 0 {
		t.Errorf("got %v, want 0", d)
	}
}

// ---------------------------------------------------------------------------
// Pagination Tests
// ---------------------------------------------------------------------------

func TestHTTPExecutor_PaginationPage(t *testing.T) {
	pageCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pageCount++
		switch page {
		case "1", "":
			w.Write([]byte(`{"data": [{"id": 1}, {"id": 2}], "has_more": true}`))
		case "2":
			w.Write([]byte(`{"data": [{"id": 3}, {"id": 4}], "has_more": true}`))
		case "3":
			w.Write([]byte(`{"data": [{"id": 5}], "has_more": false}`))
		default:
			w.Write([]byte(`{"data": [], "has_more": false}`))
		}
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name:    "list",
		Method: "GET", Path: "/items",
		Params: []models.Param{
			{Name: "page", In: "query"},
		},
		Pagination: &models.Pagination{
			Type:        "page",
			Param:       "page",
			HasMorePath: "$.has_more",
			MaxPages:    10,
		},
		// No retry delays for test speed
		Retry: &models.Retry{MaxAttempts: 1},
	}

	exec := &HTTPExecutor{PaginateAll: true}
	result, err := exec.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Should have fetched 3 pages
	if pageCount != 3 {
		t.Errorf("expected 3 page requests, got %d", pageCount)
	}

	// Result should be a JSON array with 5 items
	var items []map[string]any
	if err := json.Unmarshal(result, &items); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(items) != 5 {
		t.Errorf("expected 5 items, got %d", len(items))
	}
}

func TestHTTPExecutor_PaginationCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")
		switch cursor {
		case "", "start":
			w.Write([]byte(`{"items": [{"id": 1}], "next_cursor": "abc123"}`))
		case "abc123":
			w.Write([]byte(`{"items": [{"id": 2}], "next_cursor": "def456"}`))
		case "def456":
			w.Write([]byte(`{"items": [{"id": 3}], "next_cursor": ""}`))
		default:
			w.Write([]byte(`{"items": []}`))
		}
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name:    "list",
		Method: "GET", Path: "/items",
		Params: []models.Param{
			{Name: "cursor", In: "query"},
		},
		Pagination: &models.Pagination{
			Type:       "cursor",
			Param:      "cursor",
			CursorPath: "$.next_cursor",
			MaxPages:   10,
		},
		Retry: &models.Retry{MaxAttempts: 1},
	}

	exec := &HTTPExecutor{PaginateAll: true}
	result, err := exec.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var items []map[string]any
	if err := json.Unmarshal(result, &items); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

func TestHTTPExecutor_PaginationOffset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		switch offset {
		case "0", "":
			w.Write([]byte(`[{"id": 1}, {"id": 2}]`))
		case "2":
			w.Write([]byte(`[{"id": 3}]`))
		case "3":
			w.Write([]byte(`[]`))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name:    "list",
		Method: "GET", Path: "/items",
		Params: []models.Param{
			{Name: "offset", In: "query"},
			{Name: "limit", In: "query"},
		},
		Pagination: &models.Pagination{
			Type:         "offset",
			Param:        "offset",
			LimitParam:   "limit",
			LimitDefault: 2,
			MaxPages:     10,
		},
		Retry: &models.Retry{MaxAttempts: 1},
	}

	exec := &HTTPExecutor{PaginateAll: true}
	result, err := exec.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var items []map[string]any
	if err := json.Unmarshal(result, &items); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

func TestHTTPExecutor_PaginationMaxPages(t *testing.T) {
	pageCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageCount++
		// Always return data and has_more=true
		w.Write([]byte(`{"data": [{"id": 1}], "has_more": true}`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name:    "list",
		Method: "GET", Path: "/items",
		Params: []models.Param{
			{Name: "page", In: "query"},
		},
		Pagination: &models.Pagination{
			Type:        "page",
			Param:       "page",
			HasMorePath: "$.has_more",
			MaxPages:    3,
		},
		Retry: &models.Retry{MaxAttempts: 1},
	}

	exec := &HTTPExecutor{PaginateAll: true}
	result, err := exec.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Should stop at max_pages=3
	if pageCount != 3 {
		t.Errorf("expected 3 page requests (max_pages), got %d", pageCount)
	}

	var items []map[string]any
	if err := json.Unmarshal(result, &items); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items (1 per page x 3 pages), got %d", len(items))
	}
}

func TestHTTPExecutor_NoPaginationWithoutAll(t *testing.T) {
	pageCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageCount++
		w.Write([]byte(`{"data": [{"id": 1}], "has_more": true}`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name:    "list",
		Method: "GET", Path: "/items",
		Pagination: &models.Pagination{
			Type:  "page",
			Param: "page",
		},
		Retry: &models.Retry{MaxAttempts: 1},
	}

	// PaginateAll is false - should only fetch one page
	exec := &HTTPExecutor{PaginateAll: false}
	_, err := exec.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if pageCount != 1 {
		t.Errorf("expected 1 page request without --all, got %d", pageCount)
	}
}

// ---------------------------------------------------------------------------
// Streaming Tests
// ---------------------------------------------------------------------------

func TestHTTPExecutor_StreamBasic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, "line %d\n", i)
			if ok {
				flusher.Flush()
			}
		}
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name:          "stream-test",
		Method: "GET", Path: "/stream",
		Stream:        true,
		StreamTimeout: "5s",
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exec := &HTTPExecutor{}
	result, err := exec.Execute(context.Background(), spec, action, nil)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Result should contain all lines
	if !strings.Contains(string(result), "line 0") {
		t.Errorf("result should contain line 0, got: %q", string(result))
	}

	// Stdout should have received the lines
	stdout := buf.String()
	if !strings.Contains(stdout, "line 0") {
		t.Errorf("stdout should contain line 0, got: %q", stdout)
	}
	if !strings.Contains(stdout, "line 2") {
		t.Errorf("stdout should contain line 2, got: %q", stdout)
	}
}

// ---------------------------------------------------------------------------
// JSONPath helper tests
// ---------------------------------------------------------------------------

func TestExtractJSONPathBool(t *testing.T) {
	body := []byte(`{"has_more": true, "count": 0}`)
	if !extractJSONPathBool(body, "$.has_more") {
		t.Error("expected true for has_more")
	}
	if extractJSONPathBool(body, "$.count") {
		t.Error("expected false for count=0")
	}
	if extractJSONPathBool(body, "$.nonexistent") {
		t.Error("expected false for nonexistent field")
	}
}

func TestExtractJSONPathString(t *testing.T) {
	body := []byte(`{"cursor": "abc123", "empty": ""}`)
	if got := extractJSONPathString(body, "$.cursor"); got != "abc123" {
		t.Errorf("expected abc123, got %q", got)
	}
	if got := extractJSONPathString(body, "$.empty"); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
	if got := extractJSONPathString(body, "$.missing"); got != "" {
		t.Errorf("expected empty for missing field, got %q", got)
	}
}

func TestValidateBaseURL_BlocksPrivate(t *testing.T) {
	// Temporarily disable the test bypass
	allowPrivateBaseURL = false
	defer func() { allowPrivateBaseURL = true }()

	cases := []struct {
		url     string
		wantErr bool
	}{
		{"http://127.0.0.1:8080/api", true},
		{"http://10.0.0.1/api", true},
		{"http://192.168.1.1/api", true},
		{"http://[::1]/api", true},
		{"https://api.example.com/v1", false},
		{"https://8.8.8.8/dns-query", false},
	}
	for _, tc := range cases {
		err := validateBaseURL(tc.url)
		if tc.wantErr && err == nil {
			t.Errorf("validateBaseURL(%q) = nil, want error", tc.url)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("validateBaseURL(%q) = %v, want nil", tc.url, err)
		}
	}
}

// ---------------------------------------------------------------------------
// applyActionTransform tests - verifies the full transform pipeline
// ---------------------------------------------------------------------------

func TestApplyActionTransform_NoTransforms(t *testing.T) {
	body := []byte(`{"data": "hello"}`)
	action := &models.Action{}
	got, err := applyActionTransform(body, action)
	if err != nil {
		t.Fatalf("applyActionTransform: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("got %q, want %q", string(got), string(body))
	}
}

func TestApplyActionTransform_JSONExtract(t *testing.T) {
	body := []byte(`{"data": {"name": "test"}, "meta": "ignore"}`)
	action := &models.Action{
		Transform: []models.TransformStep{
			{Type: "json", Extract: "$.data"},
		},
	}
	got, err := applyActionTransform(body, action)
	if err != nil {
		t.Fatalf("applyActionTransform json extract: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["name"] != "test" {
		t.Errorf("expected name=test, got %v", result["name"])
	}
}

func TestApplyActionTransform_HTMLToMarkdown(t *testing.T) {
	body := []byte(`"<h1>Title</h1><p>Hello <strong>world</strong></p>"`)
	action := &models.Action{
		Transform: []models.TransformStep{
			{Type: "html_to_markdown"},
		},
	}
	got, err := applyActionTransform(body, action)
	if err != nil {
		t.Fatalf("applyActionTransform html_to_markdown: %v", err)
	}
	result := string(got)
	if !strings.Contains(result, "# Title") {
		t.Errorf("expected markdown heading, got %q", result)
	}
	if !strings.Contains(result, "**world**") {
		t.Errorf("expected bold text, got %q", result)
	}
}

func TestApplyActionTransform_Truncate(t *testing.T) {
	body := []byte(`[1, 2, 3, 4, 5, 6, 7, 8, 9, 10]`)
	action := &models.Action{
		Transform: []models.TransformStep{
			{Type: "truncate", MaxItems: 3},
		},
	}
	got, err := applyActionTransform(body, action)
	if err != nil {
		t.Fatalf("applyActionTransform truncate: %v", err)
	}
	var result []any
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 items, got %d", len(result))
	}
}

func TestApplyActionTransform_SkipsRequestPhase(t *testing.T) {
	body := []byte(`{"data": "hello"}`)
	action := &models.Action{
		Transform: []models.TransformStep{
			{Type: "json", Extract: "$.data", On: "request"},
		},
	}
	got, err := applyActionTransform(body, action)
	if err != nil {
		t.Fatalf("applyActionTransform: %v", err)
	}
	// Request-phase transforms should be skipped, body unchanged
	if string(got) != string(body) {
		t.Errorf("request-phase transform should be skipped, got %q", string(got))
	}
}

func TestApplyActionTransform_MultiStepPipeline(t *testing.T) {
	body := []byte(`{"items": [{"name": "a"}, {"name": "b"}, {"name": "c"}, {"name": "d"}]}`)
	action := &models.Action{
		Transform: []models.TransformStep{
			{Type: "json", Extract: "$.items"},
			{Type: "truncate", MaxItems: 2},
		},
	}
	got, err := applyActionTransform(body, action)
	if err != nil {
		t.Fatalf("applyActionTransform multi-step: %v", err)
	}
	var result []any
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 items after extract+truncate, got %d", len(result))
	}
}

func TestApplyActionTransform_HTMLBodyDirect(t *testing.T) {
	// Tests that raw HTML (non-JSON) is handled as a string
	htmlBody := []byte(`<html><body><h1>Hello</h1><p>World</p></body></html>`)
	action := &models.Action{
		Transform: []models.TransformStep{
			{Type: "html_to_markdown"},
		},
	}
	got, err := applyActionTransform(htmlBody, action)
	if err != nil {
		t.Fatalf("applyActionTransform html body: %v", err)
	}
	result := string(got)
	if !strings.Contains(result, "# Hello") {
		t.Errorf("expected markdown heading from raw HTML, got %q", result)
	}
	if !strings.Contains(result, "World") {
		t.Errorf("expected body text, got %q", result)
	}
}

func TestApplyActionTransform_SelectFields(t *testing.T) {
	body := []byte(`[{"name":"a","score":1,"extra":"x"},{"name":"b","score":2,"extra":"y"}]`)
	action := &models.Action{
		Transform: []models.TransformStep{
			{Type: "json", Select: []string{"name", "score"}},
		},
	}
	got, err := applyActionTransform(body, action)
	if err != nil {
		t.Fatalf("applyActionTransform select: %v", err)
	}
	var result []map[string]any
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
	if _, ok := result[0]["extra"]; ok {
		t.Error("expected 'extra' field to be filtered out")
	}
	if result[0]["name"] != "a" {
		t.Errorf("expected name=a, got %v", result[0]["name"])
	}
}

// TestExecute_WithHTMLToMarkdownTransform verifies end-to-end that
// an HTTP response goes through the html_to_markdown transform.
func TestExecute_WithHTMLToMarkdownTransform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><h1>Breaking News</h1><p>Something <strong>important</strong> happened.</p></body></html>`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Name:   "test-scraper",
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name: "scrape",
		Path: "/",
		Transform: []models.TransformStep{
			{Type: "html_to_markdown"},
		},
	}

	executor := &HTTPExecutor{}
	result, err := executor.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute with html_to_markdown: %v", err)
	}

	output := string(result)
	if !strings.Contains(output, "# Breaking News") {
		t.Errorf("expected markdown heading in output, got %q", output)
	}
	if !strings.Contains(output, "**important**") {
		t.Errorf("expected bold markdown in output, got %q", output)
	}
}

// TestExecute_WithMultiStepTransform verifies end-to-end multi-step transforms.
func TestExecute_WithMultiStepTransform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results": [{"title":"A"},{"title":"B"},{"title":"C"},{"title":"D"},{"title":"E"}]}`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Name:   "test-api",
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name: "list",
		Path: "/",
		Transform: []models.TransformStep{
			{Type: "json", Extract: "$.results"},
			{Type: "truncate", MaxItems: 3},
		},
	}

	executor := &HTTPExecutor{}
	result, err := executor.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute with multi-step: %v", err)
	}

	var items []map[string]any
	if err := json.Unmarshal(result, &items); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

// ---------------------------------------------------------------------------
// modelTransformToMap wiring tests
// Verify that each models.TransformStep field is correctly mapped.
// ---------------------------------------------------------------------------

func TestModelTransformToMap_JSONFields(t *testing.T) {
	ts := models.TransformStep{
		Type:    "json",
		Extract: "$.data",
		Select:  []string{"name", "id"},
		Rename:  map[string]string{"name": "title"},
		Flatten: true,
		Unwrap:  true,
	}
	m := modelTransformToMap(ts)
	if m["type"] != "json" {
		t.Errorf("type: got %v", m["type"])
	}
	if m["extract"] != "$.data" {
		t.Errorf("extract: got %v", m["extract"])
	}
	if sel, ok := m["select"].([]string); !ok || len(sel) != 2 {
		t.Errorf("select: got %v", m["select"])
	}
	if ren, ok := m["rename"].(map[string]string); !ok || ren["name"] != "title" {
		t.Errorf("rename: got %v", m["rename"])
	}
	if m["flatten"] != true {
		t.Errorf("flatten: got %v", m["flatten"])
	}
	if m["unwrap"] != true {
		t.Errorf("unwrap: got %v", m["unwrap"])
	}
}

func TestModelTransformToMap_Truncate(t *testing.T) {
	ts := models.TransformStep{Type: "truncate", MaxItems: 5, MaxLength: 100}
	m := modelTransformToMap(ts)
	tc, ok := m["truncate"].(map[string]any)
	if !ok {
		t.Fatalf("truncate config missing")
	}
	if tc["max_items"] != 5 {
		t.Errorf("max_items: got %v", tc["max_items"])
	}
	if tc["max_length"] != 100 {
		t.Errorf("max_length: got %v", tc["max_length"])
	}
}

func TestModelTransformToMap_Template(t *testing.T) {
	ts := models.TransformStep{Type: "template", Template: "Name: {{.name}}"}
	m := modelTransformToMap(ts)
	if m["template"] != "Name: {{.name}}" {
		t.Errorf("template: got %v", m["template"])
	}
}

func TestModelTransformToMap_HTMLToMarkdown(t *testing.T) {
	ts := models.TransformStep{Type: "html_to_markdown"}
	m := modelTransformToMap(ts)
	if _, ok := m["html_to_markdown"]; !ok {
		t.Error("html_to_markdown config should be present even without options")
	}
}

func TestModelTransformToMap_HTMLToMarkdownWithOptions(t *testing.T) {
	ts := models.TransformStep{Type: "html_to_markdown", RemoveImages: true, RemoveLinks: true}
	m := modelTransformToMap(ts)
	cfg, ok := m["html_to_markdown"].(map[string]any)
	if !ok {
		t.Fatal("html_to_markdown config missing")
	}
	if cfg["remove_images"] != true {
		t.Error("remove_images not set")
	}
	if cfg["remove_links"] != true {
		t.Error("remove_links not set")
	}
}

func TestModelTransformToMap_Sort(t *testing.T) {
	ts := models.TransformStep{Type: "sort", Field: "name", Order: "desc"}
	m := modelTransformToMap(ts)
	if m["field"] != "name" {
		t.Errorf("field: got %v", m["field"])
	}
	if m["order"] != "desc" {
		t.Errorf("order: got %v", m["order"])
	}
}

func TestModelTransformToMap_Filter(t *testing.T) {
	ts := models.TransformStep{Type: "filter", Filter: "score > 10"}
	m := modelTransformToMap(ts)
	if m["filter"] != "score > 10" {
		t.Errorf("filter: got %v", m["filter"])
	}
}

func TestModelTransformToMap_JoinSplit(t *testing.T) {
	ts := models.TransformStep{Type: "join", Separator: ", "}
	m := modelTransformToMap(ts)
	if m["separator"] != ", " {
		t.Errorf("separator: got %v", m["separator"])
	}
}

func TestModelTransformToMap_DateFormat(t *testing.T) {
	ts := models.TransformStep{Type: "date_format", Field: "created", From: "2006-01-02", To: "Jan 2, 2006"}
	m := modelTransformToMap(ts)
	if m["from"] != "2006-01-02" {
		t.Errorf("from: got %v", m["from"])
	}
	if m["to"] != "Jan 2, 2006" {
		t.Errorf("to: got %v", m["to"])
	}
}

func TestModelTransformToMap_CSVToJSON(t *testing.T) {
	ts := models.TransformStep{Type: "csv_to_json", CSVHeaders: true}
	m := modelTransformToMap(ts)
	if m["headers"] != true {
		t.Errorf("headers: got %v", m["headers"])
	}
}

func TestModelTransformToMap_Prompt(t *testing.T) {
	ts := models.TransformStep{Type: "prompt", Value: "Summarize: {{.text}}"}
	m := modelTransformToMap(ts)
	if m["value"] != "Summarize: {{.text}}" {
		t.Errorf("value: got %v", m["value"])
	}
}

func TestModelTransformToMap_Only(t *testing.T) {
	ts := models.TransformStep{Type: "json", Only: []string{"search", "list"}}
	m := modelTransformToMap(ts)
	only, ok := m["only"].([]string)
	if !ok || len(only) != 2 {
		t.Errorf("only: got %v", m["only"])
	}
}

func TestModelTransformToMap_Inject(t *testing.T) {
	ts := models.TransformStep{Type: "json", Inject: map[string]any{"source": "api"}}
	m := modelTransformToMap(ts)
	inj, ok := m["inject"].(map[string]any)
	if !ok || inj["source"] != "api" {
		t.Errorf("inject: got %v", m["inject"])
	}
}

func TestModelTransformToMap_JS(t *testing.T) {
	ts := models.TransformStep{Type: "js", Script: "function transform(data) { return data; }"}
	m := modelTransformToMap(ts)
	if m["js"] != "function transform(data) { return data; }" {
		t.Errorf("js: got %v", m["js"])
	}
}

func TestModelTransformToMap_EmptyStep(t *testing.T) {
	ts := models.TransformStep{}
	m := modelTransformToMap(ts)
	if len(m) != 0 {
		t.Errorf("empty step should produce empty map, got %v", m)
	}
}

// ---------------------------------------------------------------------------
// End-to-end executor transform tests for various types
// ---------------------------------------------------------------------------

func TestExecute_WithTemplateTransform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name": "Alice", "age": 30}`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Name:   "test-template",
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name: "get",
		Path: "/",
		Transform: []models.TransformStep{
			{Type: "template", Template: "Name: {{.name}}, Age: {{.age}}"},
		},
	}

	executor := &HTTPExecutor{}
	result, err := executor.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute with template: %v", err)
	}
	output := string(result)
	if !strings.Contains(output, "Name: Alice") {
		t.Errorf("expected template output, got %q", output)
	}
	if !strings.Contains(output, "Age: 30") {
		t.Errorf("expected age in output, got %q", output)
	}
}

func TestExecute_WithSortTransform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"name":"Charlie","score":3},{"name":"Alice","score":1},{"name":"Bob","score":2}]`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Name:   "test-sort",
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name: "list",
		Path: "/",
		Transform: []models.TransformStep{
			{Type: "sort", Field: "score", Order: "asc"},
		},
	}

	executor := &HTTPExecutor{}
	result, err := executor.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute with sort: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(result, &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0]["name"] != "Alice" {
		t.Errorf("expected Alice first (asc), got %v", items[0]["name"])
	}
}

func TestExecute_WithFilterTransform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"name":"A","score":5},{"name":"B","score":15},{"name":"C","score":25}]`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Name:   "test-filter",
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name: "list",
		Path: "/",
		Transform: []models.TransformStep{
			{Type: "filter", Filter: ".score > 10"},
		},
	}

	executor := &HTTPExecutor{}
	result, err := executor.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute with filter: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(result, &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items with score>10, got %d", len(items))
	}
}

func TestExecute_WithRenameTransform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"nm":"test","val":42}]`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Name:   "test-rename",
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name: "list",
		Path: "/",
		Transform: []models.TransformStep{
			{Type: "json", Rename: map[string]string{"nm": "name", "val": "value"}},
		},
	}

	executor := &HTTPExecutor{}
	result, err := executor.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute with rename: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(result, &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if items[0]["name"] != "test" {
		t.Errorf("expected renamed field, got %v", items[0])
	}
}

func TestExecute_WithJoinTransform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`["apple", "banana", "cherry"]`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Name:   "test-join",
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name: "list",
		Path: "/",
		Transform: []models.TransformStep{
			{Type: "join", Separator: ", "},
		},
	}

	executor := &HTTPExecutor{}
	result, err := executor.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute with join: %v", err)
	}
	output := string(result)
	if output != "apple, banana, cherry" {
		t.Errorf("expected joined string, got %q", output)
	}
}

func TestExecute_WithCountTransform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[1, 2, 3, 4, 5]`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Name:   "test-count",
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name: "list",
		Path: "/",
		Transform: []models.TransformStep{
			{Type: "count"},
		},
	}

	executor := &HTTPExecutor{}
	result, err := executor.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute with count: %v", err)
	}
	output := strings.TrimSpace(string(result))
	if output != "5" {
		t.Errorf("expected count 5, got %q", output)
	}
}

func TestExecute_WithFlattenTransform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[[1, 2], [3, 4], [5]]`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Name:   "test-flatten",
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name: "list",
		Path: "/",
		Transform: []models.TransformStep{
			{Type: "flatten"},
		},
	}

	executor := &HTTPExecutor{}
	result, err := executor.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute with flatten: %v", err)
	}
	var items []any
	if err := json.Unmarshal(result, &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 5 {
		t.Errorf("expected 5 flattened items, got %d", len(items))
	}
}

func TestExecute_SkipsRequestPhaseTransforms(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data": "hello"}`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Name:   "test-phase",
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name: "get",
		Path: "/",
		Transform: []models.TransformStep{
			{Type: "json", Extract: "$.nonexistent", On: "request"},
			{Type: "json", Extract: "$.data"},
		},
	}

	executor := &HTTPExecutor{}
	result, err := executor.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	output := strings.TrimSpace(string(result))
	if !strings.Contains(output, "hello") {
		t.Errorf("expected 'hello', got %q", output)
	}
}

func TestExecute_OutputPhaseExplicit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items": [1, 2, 3]}`))
	}))
	defer server.Close()

	spec := &models.ToolSpec{
		Name:   "test-output",
		Server: &models.Server{Type: "http", URL: server.URL},
	}
	action := &models.Action{
		Name: "get",
		Path: "/",
		Transform: []models.TransformStep{
			{Type: "json", Extract: "$.items", On: "output"},
			{Type: "count", On: "output"},
		},
	}

	executor := &HTTPExecutor{}
	result, err := executor.Execute(context.Background(), spec, action, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	output := strings.TrimSpace(string(result))
	if output != "3" {
		t.Errorf("expected count 3 from explicit output phase, got %q", output)
	}
}
