// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package updater handles automatic registry index syncing, CLI version
// checking, and self-update functionality. It runs checks on a configurable
// interval (default weekly) and can optionally auto-update the binary.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
)

const (
	// GitHubRepo is the GitHub repository for version checks.
	GitHubRepo = "clictl/cli"
	// InstallScriptURL is the URL for the install script used in self-update.
	InstallScriptURL = "https://download.clictl.dev/install.sh"
	// VersionManifestURL is the primary URL for checking the latest version.
	VersionManifestURL = "https://download.clictl.dev/version.json"
	// DownloadBaseURL is the base URL for downloading release assets.
	DownloadBaseURL = "https://download.clictl.dev"
	// VersionCheckTimeout is the HTTP timeout for background version checks.
	VersionCheckTimeout = 3 * time.Second
	// ForceCheckTimeout is the HTTP timeout for explicit version checks (clictl update).
	ForceCheckTimeout = 10 * time.Second
)

// VersionManifest represents the version.json file hosted at download.clictl.dev.
type VersionManifest struct {
	Version  string            `json:"version"`
	Commit   string            `json:"commit"`
	Date     string            `json:"date"`
	Assets   map[string]string `json:"assets"`
	Checksum string            `json:"checksums_url"`
}

// CurrentVersion is set at build time via ldflags. Matches command.Version.
var CurrentVersion = "dev"

// overrideManifestURL and overrideGitHubURL allow tests to redirect HTTP calls.
var overrideManifestURL = VersionManifestURL
var overrideGitHubURL = ""

// SetVersion allows the command package to pass its version for comparison.
func SetVersion(v string) {
	CurrentVersion = v
}

// AutoCheck runs periodic background checks for index sync and version updates.
// Called from root PersistentPreRun. Runs silently, never blocks, never errors fatally.
//
// Version notifications use a two-tier approach:
//  1. If a cached LatestVersion exists and is newer, notify immediately (no network).
//  2. If the cache is stale, fetch in a background goroutine with a fast timeout.
func AutoCheck(cfg *config.Config) {
	now := time.Now()

	// Check if first run (no last sync recorded) - do initial sync
	if cfg.Update.LastSyncAt == "" {
		go syncRegistries(cfg, now)
	} else if needsSync(cfg, now) {
		go syncRegistries(cfg, now)
	}

	// Show notification from cache immediately (no network hit)
	if cfg.Update.LatestVersion != "" {
		notifyIfNewer(cfg, cfg.Update.LatestVersion)
	}

	// Refresh cache in background if stale
	if needsVersionCheck(cfg, now) {
		go checkVersion(cfg, now)
	}
}

// needsSync returns true if the registry index should be re-synced.
func needsSync(cfg *config.Config, now time.Time) bool {
	if cfg.Update.LastSyncAt == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, cfg.Update.LastSyncAt)
	if err != nil {
		return true
	}
	return now.Sub(last) >= cfg.Update.SyncIntervalDuration()
}

// needsVersionCheck returns true if a version check is due.
func needsVersionCheck(cfg *config.Config, now time.Time) bool {
	if cfg.Update.LastVersionCheckAt == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, cfg.Update.LastVersionCheckAt)
	if err != nil {
		return true
	}
	return now.Sub(last) >= cfg.Update.VersionCheckIntervalDuration()
}

// syncRegistries syncs all configured toolboxes and updates the last sync timestamp.
func syncRegistries(cfg *config.Config, now time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	bucketsDir := config.ToolboxesDir()

	for _, reg := range cfg.Toolboxes {
		regDir := filepath.Join(bucketsDir, reg.Name)
		switch reg.Type {
		case "api":
			apiURL := reg.URL
			if apiURL == "" || apiURL == config.DefaultAPIURL {
				apiURL = cfg.APIURL
			}
			_ = registry.SyncAPI(ctx, apiURL, regDir)
		case "git":
			_ = registry.SyncGit(ctx, reg.URL, reg.Branch, regDir)
		}
	}

	cfg.Update.LastSyncAt = now.Format(time.RFC3339)
	_ = config.Save(cfg)
}

// isNewer returns true if remote is a different (presumably newer) version than current.
func isNewer(remote string) bool {
	if CurrentVersion == "dev" || CurrentVersion == remote {
		return false
	}
	return strings.TrimPrefix(CurrentVersion, "v") != strings.TrimPrefix(remote, "v")
}

// notifyIfNewer prints an update notice to stderr if remote is newer than current.
// If auto-update is enabled, it triggers a self-update instead.
func notifyIfNewer(cfg *config.Config, remote string) {
	if !isNewer(remote) {
		return
	}

	if cfg.Update.AutoUpdate {
		fmt.Fprintf(os.Stderr, "Updating clictl %s -> %s...\n", CurrentVersion, remote)
		if err := SelfUpdate(false); err != nil {
			fmt.Fprintf(os.Stderr, "Auto-update failed: %v\n", err)
			fmt.Fprintf(os.Stderr, "Run 'clictl update' to update manually.\n")
		} else {
			fmt.Fprintf(os.Stderr, "Updated to clictl %s. Restart to use the new version.\n", remote)
		}
	} else {
		fmt.Fprintf(os.Stderr, "A new version of clictl is available: %s (current: %s)\n", remote, CurrentVersion)
		fmt.Fprintf(os.Stderr, "Run 'clictl update' to update, or enable auto-update:\n")
		fmt.Fprintf(os.Stderr, "  clictl update --enable-auto\n")
	}
}

// checkVersion fetches the latest version in the background and updates the cache.
// Notification is handled by AutoCheck from the cache, so this only updates the
// stored value for the next run.
func checkVersion(cfg *config.Config, now time.Time) {
	latest, err := fetchLatestVersionFast()
	if err != nil {
		return
	}

	cfg.Update.LastVersionCheckAt = now.Format(time.RFC3339)
	cfg.Update.LatestVersion = latest
	_ = config.Save(cfg)
}

// ForceSyncRegistries syncs all toolboxes immediately (called by 'clictl update').
func ForceSyncRegistries(cfg *config.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	bucketsDir := config.ToolboxesDir()
	var lastErr error

	for _, reg := range cfg.Toolboxes {
		regDir := filepath.Join(bucketsDir, reg.Name)
		switch reg.Type {
		case "api":
			apiURL := reg.URL
			if apiURL == "" || apiURL == config.DefaultAPIURL {
				apiURL = cfg.APIURL
			}
			fmt.Printf("Syncing toolbox %q from %s...\n", reg.Name, apiURL)
			if err := registry.SyncAPI(ctx, apiURL, regDir); err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: %v\n", err)
				lastErr = err
			} else {
				fmt.Println("  Done.")
			}
		case "git":
			fmt.Printf("Syncing toolbox %q from %s...\n", reg.Name, reg.URL)
			if err := registry.SyncGit(ctx, reg.URL, reg.Branch, regDir); err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: %v\n", err)
				lastErr = err
			} else {
				fmt.Println("  Done.")
			}
		}
	}

	cfg.Update.LastSyncAt = time.Now().Format(time.RFC3339)
	_ = config.Save(cfg)
	return lastErr
}

// ForceVersionCheck checks for the latest version and returns it.
func ForceVersionCheck(cfg *config.Config) (string, error) {
	latest, err := fetchLatestVersion()
	if err != nil {
		return "", err
	}

	cfg.Update.LastVersionCheckAt = time.Now().Format(time.RFC3339)
	cfg.Update.LatestVersion = latest
	_ = config.Save(cfg)

	return latest, nil
}

// fetchLatestVersionFast checks only the version manifest with a fast timeout.
// Used by background AutoCheck where we prefer to silently fail over blocking.
func fetchLatestVersionFast() (string, error) {
	return fetchVersionFromManifestWithTimeout(VersionCheckTimeout)
}

// fetchLatestVersion checks the version manifest first, then falls back to GitHub.
// Used by ForceVersionCheck and SelfUpdate where reliability matters more than speed.
func fetchLatestVersion() (string, error) {
	if v, err := fetchVersionFromManifestWithTimeout(ForceCheckTimeout); err == nil {
		return v, nil
	}
	return fetchVersionFromGitHub()
}

// fetchVersionManifest fetches the full manifest using the default force timeout.
func fetchVersionManifest() (*VersionManifest, error) {
	return fetchVersionManifestWithTimeout(ForceCheckTimeout)
}

// fetchVersionFromManifest gets the latest version using the default force timeout.
func fetchVersionFromManifest() (string, error) {
	return fetchVersionFromManifestWithTimeout(ForceCheckTimeout)
}

// fetchVersionManifestWithTimeout fetches and parses the version manifest with a given timeout.
func fetchVersionManifestWithTimeout(timeout time.Duration) (*VersionManifest, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest("GET", overrideManifestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "clictl/"+CurrentVersion)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching version manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("version manifest returned %d", resp.StatusCode)
	}

	var manifest VersionManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parsing version manifest: %w", err)
	}

	return &manifest, nil
}

// fetchVersionFromManifestWithTimeout gets the latest version string with a given timeout.
func fetchVersionFromManifestWithTimeout(timeout time.Duration) (string, error) {
	manifest, err := fetchVersionManifestWithTimeout(timeout)
	if err != nil {
		return "", err
	}
	if manifest.Version == "" {
		return "", fmt.Errorf("version manifest has empty version")
	}
	return manifest.Version, nil
}

// fetchVersionFromGitHub queries GitHub releases API for the latest tag (fallback).
func fetchVersionFromGitHub() (string, error) {
	url := overrideGitHubURL
	if url == "" {
		url = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", GitHubRepo)
	}
	client := &http.Client{Timeout: ForceCheckTimeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "clictl/"+CurrentVersion)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("checking for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("no releases found (repo %s may not have published releases yet)", GitHubRepo)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("parsing release info: %w", err)
	}

	return release.TagName, nil
}

// SelfUpdate downloads and installs the latest version of clictl.
// It downloads the appropriate binary for the current OS/arch and replaces the running binary.
// When skipVerify is true, checksum verification is skipped (for air-gapped environments).
func SelfUpdate(skipVerify bool) error {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	// Determine archive name
	var filename string
	var extractCmd string
	if osName == "windows" {
		filename = fmt.Sprintf("clictl-windows-%s.zip", arch)
		extractCmd = "unzip"
	} else {
		filename = fmt.Sprintf("clictl-%s-%s.tar.gz", osName, arch)
		extractCmd = "tar"
	}

	// Get latest release download URL
	latest, err := fetchLatestVersion()
	if err != nil {
		return err
	}

	downloadURL := fmt.Sprintf("%s/releases/%s/%s", DownloadBaseURL, latest, filename)

	// Download to temp dir
	tmpDir, err := os.MkdirTemp("", "clictl-update-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, filename)
	if err := downloadFile(downloadURL, archivePath); err != nil {
		return fmt.Errorf("downloading %s: %w", filename, err)
	}

	// Verify checksum (mandatory unless explicitly skipped)
	if skipVerify {
		fmt.Fprintf(os.Stderr, "Warning: checksum verification skipped (--skip-verify)\n")
	} else if err := verifyChecksum(latest, filename, archivePath); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	// Extract
	var cmd *exec.Cmd
	if extractCmd == "tar" {
		cmd = exec.Command("tar", "-xzf", archivePath, "-C", tmpDir)
	} else {
		cmd = exec.Command("unzip", "-q", archivePath, "-d", tmpDir)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("extracting archive: %w", err)
	}

	// Find the current binary path
	currentBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding current binary: %w", err)
	}
	currentBin, err = filepath.EvalSymlinks(currentBin)
	if err != nil {
		return fmt.Errorf("resolving binary path: %w", err)
	}

	// New binary path
	newBinName := "clictl"
	if osName == "windows" {
		newBinName = "clictl.exe"
	}
	newBin := filepath.Join(tmpDir, newBinName)

	if _, err := os.Stat(newBin); err != nil {
		return fmt.Errorf("extracted binary not found: %w", err)
	}

	// Replace: rename old, move new, remove old
	backupPath := currentBin + ".old"
	os.Remove(backupPath) // clean up any previous backup

	if err := os.Rename(currentBin, backupPath); err != nil {
		return fmt.Errorf("backing up current binary: %w", err)
	}

	if err := copyFile(newBin, currentBin); err != nil {
		// Restore backup on failure
		os.Rename(backupPath, currentBin)
		return fmt.Errorf("installing new binary: %w", err)
	}

	if err := os.Chmod(currentBin, 0o755); err != nil {
		return fmt.Errorf("setting permissions: %w", err)
	}

	os.Remove(backupPath)
	return nil
}

// secureHTTPClient returns an http.Client with redirect restrictions and timeout.
// Blocks redirects to private/loopback IP ranges to prevent SSRF.
func secureHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			host := req.URL.Hostname()
			if ip := net.ParseIP(host); ip != nil {
				if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
					return fmt.Errorf("redirect to private network blocked: %s", host)
				}
			} else {
				ips, err := net.LookupIP(host)
				if err == nil {
					for _, ip := range ips {
						if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
							return fmt.Errorf("redirect to private network blocked: %s", host)
						}
					}
				}
			}
			return nil
		},
	}
}

// verifyChecksum downloads the checksums file and verifies the archive hash.
func verifyChecksum(version, filename, archivePath string) error {
	checksumsURL := fmt.Sprintf("%s/releases/%s/checksums.txt", DownloadBaseURL, version)
	resp, err := secureHTTPClient().Get(checksumsURL)
	if err != nil {
		return fmt.Errorf("downloading checksums: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("checksums file not available (HTTP %d), aborting update for security", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading checksums: %w", err)
	}

	// Parse checksums file: each line is "hash  filename"
	expectedHash := ""
	for _, line := range strings.Split(string(body), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == filename {
			expectedHash = parts[0]
			break
		}
	}

	if expectedHash == "" {
		return fmt.Errorf("no checksum found for %s in checksums file, aborting update for security", filename)
	}

	// Compute actual hash
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("opening archive for verification: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("computing checksum: %w", err)
	}
	actualHash := hex.EncodeToString(h.Sum(nil))

	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	return nil
}

// downloadFile downloads a URL to a local file.
func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

// copyFile copies src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
