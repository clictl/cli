// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/clictl/cli/internal/archive"
	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/signing"
	"gopkg.in/yaml.v3"
)

// decodeJSONBody decodes a JSON response body into the target struct.
func decodeJSONBody(body io.Reader, target interface{}) error {
	return json.NewDecoder(body).Decode(target)
}

// installFromRelease attempts to install a tool from a signed GitHub Release
// pack. It checks authentication, downloads the pack and signature, verifies
// both, then extracts and installs the content.
//
// Returns (installed path, nil) on success, or ("", error) on failure.
// The caller should fall back to the local toolbox flow on error.
func installFromRelease(ctx context.Context, cfg *config.Config, toolName, version, target string) (string, error) {
	token := config.ResolveAuthToken(flagAPIKey, cfg)
	if token == "" {
		return "", fmt.Errorf("not authenticated")
	}

	apiURL := config.ResolveAPIURL(flagAPIURL, cfg)

	// Step 1: Fetch the release metadata from the API
	release, err := fetchReleaseInfo(ctx, apiURL, token, toolName, version)
	if err != nil {
		return "", fmt.Errorf("fetching release info: %w", err)
	}

	// Step 2: Download the pack archive to a temp directory
	tmpDir, err := os.MkdirTemp("", "clictl-pack-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	packPath := filepath.Join(tmpDir, fmt.Sprintf("%s-%s.tar.gz", toolName, version))
	if err := downloadFile(ctx, token, release.PackURL, packPath); err != nil {
		return "", fmt.Errorf("downloading pack: %w", err)
	}

	// Step 3: Download the signature file
	sigPath := filepath.Join(tmpDir, fmt.Sprintf("%s-%s.sig", toolName, version))
	if err := downloadFile(ctx, token, release.SigURL, sigPath); err != nil {
		return "", fmt.Errorf("downloading signature: %w", err)
	}

	// Step 4: Verify the pack hash
	packData, err := os.ReadFile(packPath)
	if err != nil {
		return "", fmt.Errorf("reading pack: %w", err)
	}
	actualHash := sha256Hex(packData)
	if release.SHA256 != "" && actualHash != release.SHA256 {
		return "", fmt.Errorf("pack hash mismatch: expected %s, got %s", release.SHA256, actualHash)
	}

	// Step 5: Read signature and verify against manifest
	sigData, err := os.ReadFile(sigPath)
	if err != nil {
		return "", fmt.Errorf("reading signature: %w", err)
	}
	signatureB64 := strings.TrimSpace(string(sigData))

	// Step 6: Extract the pack to a staging directory
	stagingDir := filepath.Join(tmpDir, "staging")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return "", fmt.Errorf("creating staging dir: %w", err)
	}

	manifest, err := archive.Unpack(packPath, stagingDir)
	if err != nil {
		return "", fmt.Errorf("unpacking archive: %w", err)
	}

	// Step 7: Verify the Ed25519 signature on the manifest
	manifestYAML, err := yaml.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("marshaling manifest: %w", err)
	}
	ed25519Err := signing.VerifyManifest(string(manifestYAML), signatureB64)

	// Step 8: Sigstore bundle verification (additive, not blocking for backward compat)
	sigstoreVerified := false
	var sigstoreResult *signing.SigstoreResult
	if release.SigstoreBundleURL != "" {
		bundlePath := filepath.Join(tmpDir, fmt.Sprintf("%s-%s.sigstore.json", toolName, version))
		bundleDownloadErr := downloadFile(ctx, token, release.SigstoreBundleURL, bundlePath)
		if bundleDownloadErr == nil {
			bundleData, readErr := os.ReadFile(bundlePath)
			if readErr == nil {
				bundle, parseErr := signing.ParseSigstoreBundle(bundleData)
				if parseErr == nil {
					expectedIdentity := signing.ResolveSigstoreIdentity(ctx, apiURL)
					result, verifyErr := signing.VerifySigstoreBundle(string(manifestYAML), *bundle, expectedIdentity)
					if verifyErr == nil {
						sigstoreVerified = true
						sigstoreResult = result
					} else {
						fmt.Fprintf(os.Stderr, "Warning: Sigstore verification failed: %v\n", verifyErr)
					}
				}
			}
		}
	}

	// Determine verification outcome
	if ed25519Err != nil && !sigstoreVerified {
		return "", fmt.Errorf("signature verification failed: %w", ed25519Err)
	}
	if ed25519Err != nil && sigstoreVerified {
		fmt.Fprintf(os.Stderr, "Warning: Ed25519 signature invalid, but Sigstore verification passed\n")
	}

	// Print verification status
	if sigstoreVerified && sigstoreResult != nil {
		logTime := sigstoreResult.IntegratedTime.UTC().Format(time.RFC3339)
		fmt.Fprintf(os.Stderr, "Verified: registry signature + transparency log (index #%d, %s)\n", sigstoreResult.LogIndex, logTime)
	} else {
		fmt.Fprintf(os.Stderr, "Verified: registry signature only\n")
	}

	// Step 10: Verify content hash from manifest matches extracted content
	contentHash, _ := manifest["content_sha256"].(string)
	if contentHash != "" {
		if err := archive.VerifyPackContent(stagingDir, contentHash); err != nil {
			return "", fmt.Errorf("content verification: %w", err)
		}
	}

	// Step 11: Install the extracted content to the target directory
	installDir := skillTargetDir(toolName, target)
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return "", fmt.Errorf("creating install dir: %w", err)
	}

	contentDir := filepath.Join(stagingDir, "content")
	if err := copyDir(contentDir, installDir); err != nil {
		return "", fmt.Errorf("installing content: %w", err)
	}

	return installDir, nil
}

// releaseInfo holds the URLs and metadata returned by the pack release API.
type releaseInfo struct {
	PackURL          string
	SigURL           string
	SHA256           string
	SigstoreBundleURL string
	Version          string
}

// fetchReleaseInfo queries the API for pack release URLs and hash.
func fetchReleaseInfo(ctx context.Context, apiURL, token, toolName, version string) (*releaseInfo, error) {
	u := fmt.Sprintf("%s/api/v1/packs/%s/releases/latest", apiURL, toolName)
	if version != "" {
		u = fmt.Sprintf("%s/api/v1/packs/%s/releases/%s", apiURL, toolName, version)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "clictl/1.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("release API returned %d: %s", resp.StatusCode, string(body))
	}

	var release struct {
		PackURL           string `json:"pack_url"`
		SigURL            string `json:"sig_url"`
		SHA256            string `json:"sha256"`
		SigstoreBundleURL string `json:"sigstore_bundle_url"`
		Version           string `json:"version"`
	}

	if err := decodeJSONBody(resp.Body, &release); err != nil {
		return nil, fmt.Errorf("parsing release response: %w", err)
	}

	if release.PackURL == "" {
		return nil, fmt.Errorf("release has no pack URL")
	}

	return &releaseInfo{
		PackURL:           release.PackURL,
		SigURL:            release.SigURL,
		SHA256:            release.SHA256,
		SigstoreBundleURL: release.SigstoreBundleURL,
		Version:           release.Version,
	}, nil
}

// downloadFile downloads a URL to a local file path, using the auth token if provided.
func downloadFile(ctx context.Context, token, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("User-Agent", "clictl/1.0")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}

// sha256Hex returns the hex-encoded SHA256 hash of data.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// skillTargetDir returns the directory where a skill should be installed for the given target.
func skillTargetDir(toolName, target string) string {
	t, ok := skillTargets[target]
	if ok {
		return t.dir(toolName)
	}
	return filepath.Join(".claude", "skills", toolName)
}

// copyDir copies all files from src to dst recursively.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(destPath, data, info.Mode())
	})
}

// tryInstallFromRelease attempts to install from a signed pack release.
// If authentication is missing or the release is not found, it returns
// false and the caller should fall back to the local toolbox flow.
func tryInstallFromRelease(ctx context.Context, cfg *config.Config, toolName, version, target string) (string, bool) {
	token := config.ResolveAuthToken(flagAPIKey, cfg)
	if token == "" {
		// C1.8: Not logged in, skip signed pack and show info message
		fmt.Fprintf(os.Stderr, "Info: signed pack install requires authentication. Using local toolbox.\n")
		fmt.Fprintf(os.Stderr, "  Run 'clictl login' for signed, verified installs.\n")
		fmt.Fprintf(os.Stderr, "  Some tools require secrets (API keys). Use 'clictl vault set <name> <value>' after install.\n")
		return "", false
	}

	path, err := installFromRelease(ctx, cfg, toolName, version, target)
	if err != nil {
		// Release not available or verification failed, fall back to existing flow
		fmt.Fprintf(os.Stderr, "Info: signed pack not available for %s. Using local toolbox.\n", toolName)
		fmt.Fprintf(os.Stderr, "  Reason: %v\n", err)
		fmt.Fprintf(os.Stderr, "  This may happen if:\n")
		fmt.Fprintf(os.Stderr, "    - The publisher has not built a signed pack for this version\n")
		fmt.Fprintf(os.Stderr, "    - The pack has been delisted or revoked\n")
		fmt.Fprintf(os.Stderr, "    - Your CLI is too old to verify this pack format (try: clictl self-update)\n")
		return "", false
	}

	return path, true
}
