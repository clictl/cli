// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helper: setupHomeWithAuth creates an isolated HOME with api_url and auth config.
// ---------------------------------------------------------------------------

func setupHomeWithAuth(t *testing.T, apiURL string) (string, map[string]string) {
	t.Helper()
	home, env := setupHomeWithAPI(t, apiURL)

	cliDir := filepath.Join(home, ".clictl")
	cacheDir := filepath.Join(cliDir, "cache")

	configYAML := fmt.Sprintf(`api_url: "%s"
output: text
cache_dir: "%s"
first_run_done: true
update:
  auto_update: false
  last_sync_at: "2099-01-01T00:00:00Z"
  last_version_check_at: "2099-01-01T00:00:00Z"
auth:
  access_token: "test-token"
  active_workspace: "test-ws"
toolboxes:
  - name: test
    type: git
    url: "file:///dev/null"
`, apiURL, cacheDir)
	os.WriteFile(filepath.Join(cliDir, "config.yaml"), []byte(configYAML), 0o600)

	return home, env
}

// ---------------------------------------------------------------------------
// Star / Unstar / Stars
// ---------------------------------------------------------------------------

func TestStar(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/favorites/") {
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"tool_name":"echo-test"}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	_, env := setupHomeWithAuth(t, srv.URL)
	out := mustRun(t, env, "star", "echo-test")
	if !strings.Contains(out, "Starred echo-test") {
		t.Errorf("expected 'Starred echo-test', got: %s", out)
	}
}

func TestUnstar(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/favorites/echo-test/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	_, env := setupHomeWithAuth(t, srv.URL)
	out := mustRun(t, env, "unstar", "echo-test")
	if !strings.Contains(out, "Unstarred echo-test") {
		t.Errorf("expected 'Unstarred echo-test', got: %s", out)
	}
}

func TestStars(t *testing.T) {
	t.Parallel()

	favorites := []map[string]string{
		{"tool_name": "echo-test", "category": "testing", "source": "test-registry", "created_at": "2025-01-01T00:00:00Z"},
		{"tool_name": "auth-test", "category": "testing", "source": "test-registry", "created_at": "2025-01-02T00:00:00Z"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/favorites/") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(favorites)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	_, env := setupHomeWithAuth(t, srv.URL)
	out := mustRun(t, env, "stars")
	if !strings.Contains(out, "FAVORITES") {
		t.Errorf("expected 'FAVORITES' header, got: %s", out)
	}
	if !strings.Contains(out, "echo-test") {
		t.Errorf("expected 'echo-test' in favorites list, got: %s", out)
	}
	if !strings.Contains(out, "auth-test") {
		t.Errorf("expected 'auth-test' in favorites list, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

func TestMetrics(t *testing.T) {
	t.Parallel()

	overviewJSON := `{
		"total_invocations": 1500,
		"active_tools": 12,
		"active_users": 5,
		"error_rate": 0.02,
		"top_tools": [
			{"name": "echo-test", "invocations": 800, "errors": 10, "avg_duration_ms": 120.5}
		]
	}`

	toolJSON := `{
		"invocations_30d": 800,
		"unique_users_30d": 3,
		"error_rate_30d": 0.01,
		"avg_duration_ms": 120.5,
		"by_action": [
			{"action": "get", "invocations": 600, "avg_duration_ms": 110.0},
			{"action": "status", "invocations": 200, "avg_duration_ms": 150.0}
		]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/metrics/overview/") {
			w.Write([]byte(overviewJSON))
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/metrics/tools/echo-test/") {
			w.Write([]byte(toolJSON))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	_, env := setupHomeWithAuth(t, srv.URL)

	t.Run("overview", func(t *testing.T) {
		out := mustRun(t, env, "metrics")
		if !strings.Contains(out, "1500") {
			t.Errorf("expected total invocations '1500' in output, got: %s", out)
		}
		if !strings.Contains(out, "12") {
			t.Errorf("expected active tools '12' in output, got: %s", out)
		}
		if !strings.Contains(out, "echo-test") {
			t.Errorf("expected 'echo-test' in top tools, got: %s", out)
		}
		if !strings.Contains(out, "2%") {
			t.Errorf("expected error rate '2%%' in output, got: %s", out)
		}
	})

	t.Run("tool_detail", func(t *testing.T) {
		out := mustRun(t, env, "metrics", "echo-test")
		if !strings.Contains(out, "800") {
			t.Errorf("expected invocations '800' in output, got: %s", out)
		}
		if !strings.Contains(out, "3") {
			t.Errorf("expected unique users '3' in output, got: %s", out)
		}
		if !strings.Contains(out, "get") {
			t.Errorf("expected action 'get' in output, got: %s", out)
		}
		if !strings.Contains(out, "status") {
			t.Errorf("expected action 'status' in output, got: %s", out)
		}
	})
}

// ---------------------------------------------------------------------------
// Toolbox List - Merged (workspace + local)
// ---------------------------------------------------------------------------

func TestToolboxListMerged(t *testing.T) {
	t.Parallel()

	home, env := setupHome(t)
	cliDir := filepath.Join(home, ".clictl")

	// Write auth config so active_workspace is set
	cacheDir := filepath.Join(cliDir, "cache")
	configYAML := fmt.Sprintf(`api_url: "http://127.0.0.1:1"
output: text
cache_dir: "%s"
first_run_done: true
update:
  auto_update: false
  last_sync_at: "2099-01-01T00:00:00Z"
  last_version_check_at: "2099-01-01T00:00:00Z"
auth:
  access_token: "test-token"
  active_workspace: "test-ws"
toolboxes:
  - name: test
    type: git
    url: "file:///dev/null"
`, cacheDir)
	os.WriteFile(filepath.Join(cliDir, "config.yaml"), []byte(configYAML), 0o600)

	// Create workspace cache file with mock sources
	wsCacheDir := filepath.Join(cliDir, "workspace-cache")
	os.MkdirAll(wsCacheDir, 0o755)

	wsCache := map[string]interface{}{
		"sources": []map[string]interface{}{
			{
				"id":             "src-1",
				"name":           "acme-tools",
				"url":            "https://github.com/acme/tools.git",
				"provider":       "github",
				"type":           "git",
				"branch":         "main",
				"sync_mode":      "full",
				"visibility":     "public",
				"is_private":     false,
				"spec_count":     15,
				"last_synced_at": "2025-06-01T00:00:00Z",
			},
		},
		"favorites":      []string{},
		"disabled_tools": []string{},
	}
	wsCacheData, _ := json.MarshalIndent(wsCache, "", "  ")
	os.WriteFile(filepath.Join(wsCacheDir, "test-ws.json"), wsCacheData, 0o644)

	out := mustRun(t, env, "toolbox", "list")

	if !strings.Contains(out, "Workspace") && !strings.Contains(out, "workspace") {
		t.Errorf("expected 'Workspace' source in output, got: %s", out)
	}
	if !strings.Contains(out, "Personal") && !strings.Contains(out, "local") {
		t.Errorf("expected 'Personal' source in output, got: %s", out)
	}
	if !strings.Contains(out, "acme-tools") {
		t.Errorf("expected 'acme-tools' toolbox in output, got: %s", out)
	}
	if !strings.Contains(out, "test") {
		t.Errorf("expected 'test' local toolbox in output, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Toolbox Update --trigger
// ---------------------------------------------------------------------------

func TestToolboxUpdateTrigger(t *testing.T) {
	t.Parallel()

	var syncRequested bool
	var syncPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// cli-index endpoint (called by triggerSync to find matching sources)
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/registries/cli-index/") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"sources": []map[string]interface{}{
					{
						"id":         "src-abc",
						"name":       "my-tools",
						"url":        "https://github.com/acme/my-tools.git",
						"type":       "git",
						"branch":     "main",
						"sync_mode":  "full",
						"visibility": "public",
						"is_private": false,
						"spec_count": 5,
					},
				},
				"favorites":      []string{},
				"disabled_tools": []string{},
			})
			return
		}

		// sync trigger endpoint
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/registries/") && strings.HasSuffix(r.URL.Path, "/sync/") {
			syncRequested = true
			syncPath = r.URL.Path
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"status":"queued"}`))
			return
		}

		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	_, env := setupHomeWithAuth(t, srv.URL)
	out := mustRun(t, env, "toolbox", "update", "--trigger")

	if !syncRequested {
		t.Error("expected sync endpoint to be called, but it was not")
	}
	if !strings.Contains(syncPath, "/registries/src-abc/sync/") {
		t.Errorf("expected sync path to contain '/registries/src-abc/sync/', got: %s", syncPath)
	}
	if !strings.Contains(strings.ToLower(out), "sync") {
		t.Errorf("expected sync confirmation in output, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Search with favorites boost
// ---------------------------------------------------------------------------

func TestSearchFavoritesBoost(t *testing.T) {
	t.Parallel()

	// We need a mock API server for the permissions check that happens
	// when the user is logged in. The search also tries the API first
	// when logged in, so we need to handle that.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Permission check endpoint - allow everything
		if strings.Contains(r.URL.Path, "/permissions/check") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"allowed":     true,
				"can_request": false,
				"reason":      "",
			})
			return
		}

		// Search API - return empty so it falls back to local index
		if strings.Contains(r.URL.Path, "/search") || strings.Contains(r.URL.Path, "/specs") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"results": []interface{}{},
				"count":   0,
			})
			return
		}

		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	home, env := setupHomeWithAuth(t, srv.URL)
	cliDir := filepath.Join(home, ".clictl")

	// Create workspace cache with echo-test as a favorite
	wsCacheDir := filepath.Join(cliDir, "workspace-cache")
	os.MkdirAll(wsCacheDir, 0o755)

	wsCache := map[string]interface{}{
		"sources":        []interface{}{},
		"favorites":      []string{"echo-test"},
		"disabled_tools": []string{},
	}
	wsCacheData, _ := json.MarshalIndent(wsCache, "", "  ")
	os.WriteFile(filepath.Join(wsCacheDir, "test-ws.json"), wsCacheData, 0o644)

	out := mustRun(t, env, "search", "echo")

	// The star character (U+2605) should appear next to favorited tools
	if !strings.Contains(out, "\u2605") {
		t.Errorf("expected star character (U+2605) next to favorited tool, got: %s", out)
	}
	if !strings.Contains(out, "echo-test") {
		t.Errorf("expected 'echo-test' in search results, got: %s", out)
	}
}
