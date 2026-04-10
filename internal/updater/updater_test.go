// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package updater

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clictl/cli/internal/config"
)

func TestNeedsSync_NeverSynced(t *testing.T) {
	cfg := &config.Config{}
	if !needsSync(cfg, time.Now()) {
		t.Error("Expected needsSync=true when LastSyncAt is empty")
	}
}

func TestNeedsSync_RecentSync(t *testing.T) {
	cfg := &config.Config{
		Update: config.UpdateConfig{
			LastSyncAt: time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		},
	}
	if needsSync(cfg, time.Now()) {
		t.Error("Expected needsSync=false when synced 1 hour ago (default interval is 1 week)")
	}
}

func TestNeedsSync_StaleSync(t *testing.T) {
	cfg := &config.Config{
		Update: config.UpdateConfig{
			LastSyncAt: time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339),
		},
	}
	if !needsSync(cfg, time.Now()) {
		t.Error("Expected needsSync=true when synced 8 days ago")
	}
}

func TestNeedsSync_CustomInterval(t *testing.T) {
	cfg := &config.Config{
		Update: config.UpdateConfig{
			SyncInterval: "1h",
			LastSyncAt:   time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
		},
	}
	if !needsSync(cfg, time.Now()) {
		t.Error("Expected needsSync=true when custom interval=1h and last sync 2h ago")
	}
}

func TestNeedsSync_CustomIntervalNotDue(t *testing.T) {
	cfg := &config.Config{
		Update: config.UpdateConfig{
			SyncInterval: "24h",
			LastSyncAt:   time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		},
	}
	if needsSync(cfg, time.Now()) {
		t.Error("Expected needsSync=false when custom interval=24h and last sync 1h ago")
	}
}

func TestNeedsVersionCheck_NeverChecked(t *testing.T) {
	cfg := &config.Config{}
	if !needsVersionCheck(cfg, time.Now()) {
		t.Error("Expected needsVersionCheck=true when LastVersionCheckAt is empty")
	}
}

func TestNeedsVersionCheck_RecentCheck(t *testing.T) {
	cfg := &config.Config{
		Update: config.UpdateConfig{
			LastVersionCheckAt: time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		},
	}
	if needsVersionCheck(cfg, time.Now()) {
		t.Error("Expected needsVersionCheck=false when checked 1 hour ago")
	}
}

func TestNeedsVersionCheck_StaleCheck(t *testing.T) {
	cfg := &config.Config{
		Update: config.UpdateConfig{
			LastVersionCheckAt: time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339),
		},
	}
	if !needsVersionCheck(cfg, time.Now()) {
		t.Error("Expected needsVersionCheck=true when checked 8 days ago")
	}
}

func TestNeedsVersionCheck_InvalidTimestamp(t *testing.T) {
	cfg := &config.Config{
		Update: config.UpdateConfig{
			LastVersionCheckAt: "not-a-timestamp",
		},
	}
	if !needsVersionCheck(cfg, time.Now()) {
		t.Error("Expected needsVersionCheck=true when timestamp is invalid")
	}
}

func TestSetVersion(t *testing.T) {
	SetVersion("v1.2.3")
	if CurrentVersion != "v1.2.3" {
		t.Errorf("SetVersion: got %q, want v1.2.3", CurrentVersion)
	}
	// Reset
	CurrentVersion = "dev"
}

func TestFetchVersionFromManifest(t *testing.T) {
	manifest := VersionManifest{
		Version: "v2.0.0",
		Commit:  "abc1234",
		Date:    "2026-03-23T12:00:00Z",
		Assets: map[string]string{
			"linux-amd64":  "releases/v2.0.0/clictl-linux-amd64.tar.gz",
			"darwin-arm64": "releases/v2.0.0/clictl-darwin-arm64.tar.gz",
		},
		Checksum: "releases/v2.0.0/checksums.txt",
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(manifest)
	}))
	defer ts.Close()

	// Override the manifest URL for testing
	origURL := VersionManifestURL
	defer func() { overrideManifestURL = origURL }()
	overrideManifestURL = ts.URL

	v, err := fetchVersionFromManifest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "v2.0.0" {
		t.Errorf("got version %q, want v2.0.0", v)
	}
}

func TestFetchVersionFromManifest_EmptyVersion(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(VersionManifest{})
	}))
	defer ts.Close()

	origURL := VersionManifestURL
	defer func() { overrideManifestURL = origURL }()
	overrideManifestURL = ts.URL

	_, err := fetchVersionFromManifest()
	if err == nil {
		t.Error("expected error for empty version, got nil")
	}
}

func TestFetchVersionFromManifest_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	origURL := VersionManifestURL
	defer func() { overrideManifestURL = origURL }()
	overrideManifestURL = ts.URL

	_, err := fetchVersionFromManifest()
	if err == nil {
		t.Error("expected error for 500 response, got nil")
	}
}

func TestFetchVersionManifest_FullParse(t *testing.T) {
	manifest := VersionManifest{
		Version: "v1.5.0",
		Commit:  "def5678",
		Date:    "2026-03-20T10:00:00Z",
		Assets: map[string]string{
			"linux-amd64":   "releases/v1.5.0/clictl-linux-amd64.tar.gz",
			"linux-arm64":   "releases/v1.5.0/clictl-linux-arm64.tar.gz",
			"darwin-amd64":  "releases/v1.5.0/clictl-darwin-amd64.tar.gz",
			"darwin-arm64":  "releases/v1.5.0/clictl-darwin-arm64.tar.gz",
			"windows-amd64": "releases/v1.5.0/clictl-windows-amd64.zip",
		},
		Checksum: "releases/v1.5.0/checksums.txt",
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(manifest)
	}))
	defer ts.Close()

	origURL := VersionManifestURL
	defer func() { overrideManifestURL = origURL }()
	overrideManifestURL = ts.URL

	m, err := fetchVersionManifest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Version != "v1.5.0" {
		t.Errorf("version: got %q, want v1.5.0", m.Version)
	}
	if m.Commit != "def5678" {
		t.Errorf("commit: got %q, want def5678", m.Commit)
	}
	if len(m.Assets) != 5 {
		t.Errorf("assets count: got %d, want 5", len(m.Assets))
	}
}

func TestFetchLatestVersion_FallbackToGitHub(t *testing.T) {
	// Manifest server returns error
	manifestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer manifestServer.Close()

	// GitHub server returns valid version
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			TagName string `json:"tag_name"`
		}{TagName: "v3.0.0"})
	}))
	defer githubServer.Close()

	origManifest := VersionManifestURL
	origGitHub := overrideGitHubURL
	defer func() {
		overrideManifestURL = origManifest
		overrideGitHubURL = origGitHub
	}()
	overrideManifestURL = manifestServer.URL
	overrideGitHubURL = githubServer.URL

	v, err := fetchLatestVersion()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "v3.0.0" {
		t.Errorf("got version %q, want v3.0.0 (from GitHub fallback)", v)
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current, remote string
		want            bool
	}{
		{"dev", "v1.0.0", false},
		{"v1.0.0", "v1.0.0", false},
		{"1.0.0", "v1.0.0", false},
		{"v1.0.0", "v2.0.0", true},
		{"v1.0.0", "v1.0.1", true},
	}
	for _, tt := range tests {
		CurrentVersion = tt.current
		got := isNewer(tt.remote)
		if got != tt.want {
			t.Errorf("isNewer(%q) with current=%q: got %v, want %v", tt.remote, tt.current, got, tt.want)
		}
	}
	CurrentVersion = "dev"
}

func TestFetchLatestVersionFast_ManifestOnly(t *testing.T) {
	// Fast path should only hit manifest, not GitHub
	manifestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(VersionManifest{Version: "v4.0.0"})
	}))
	defer manifestServer.Close()

	origManifest := overrideManifestURL
	defer func() { overrideManifestURL = origManifest }()
	overrideManifestURL = manifestServer.URL

	v, err := fetchLatestVersionFast()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "v4.0.0" {
		t.Errorf("got version %q, want v4.0.0", v)
	}
}

func TestFetchLatestVersionFast_ManifestDown(t *testing.T) {
	// Fast path should fail (no GitHub fallback)
	manifestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer manifestServer.Close()

	origManifest := overrideManifestURL
	defer func() { overrideManifestURL = origManifest }()
	overrideManifestURL = manifestServer.URL

	_, err := fetchLatestVersionFast()
	if err == nil {
		t.Error("expected error when manifest is down in fast path, got nil")
	}
}
