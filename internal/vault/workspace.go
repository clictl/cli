// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package vault

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	// workspaceCacheTTL is the default time-to-live for cached workspace secrets.
	workspaceCacheTTL = 5 * time.Minute
	// maxStaleTTL is the maximum age past expiration that a stale cached secret
	// will be served. Beyond this limit, the secret is rejected to ensure revoked
	// credentials do not persist indefinitely.
	maxStaleTTL = 1 * time.Hour
	// workspaceCachePrefix is the key prefix for cached workspace secrets in the local vault.
	workspaceCachePrefix = "ws:"
)

// workspaceCacheEntry holds a cached workspace secret with metadata.
type workspaceCacheEntry struct {
	Value     string    `json:"value"`
	ETag      string    `json:"etag"`
	ExpiresAt time.Time `json:"expires_at"`
}

// WorkspaceVaultResolver resolves secrets from the workspace API with local caching.
// It fetches secrets from /api/v1/workspaces/{slug}/secrets/{name}/ and caches
// them in the local user vault with a "ws:{slug}:{name}" key prefix.
type WorkspaceVaultResolver struct {
	APIURL string
	Token  string
	Slug   string
	Cache  *Vault
}

// NewWorkspaceVaultResolver creates a new resolver for the given workspace.
func NewWorkspaceVaultResolver(apiURL, token, slug string, cache *Vault) *WorkspaceVaultResolver {
	return &WorkspaceVaultResolver{
		APIURL: apiURL,
		Token:  token,
		Slug:   slug,
		Cache:  cache,
	}
}

// Resolve fetches a secret value from the workspace, using local cache when possible.
// On cache hit (within TTL): returns cached value.
// On cache miss or expired: calls the API with If-None-Match ETag header.
// On 304 Not Modified: extends cache TTL and returns cached value.
// On 200 OK: updates cache with new value and ETag.
// On error with cached value: returns stale cached value with a warning to stderr.
// On error with no cache: returns empty string with a warning to stderr.
func (w *WorkspaceVaultResolver) Resolve(name string) string {
	if w.Cache == nil || w.Slug == "" || w.Token == "" {
		return ""
	}

	cacheKey := workspaceCachePrefix + w.Slug + ":" + name

	// Check local cache
	cached, err := w.loadCacheEntry(cacheKey)
	if err == nil && cached != nil && time.Now().Before(cached.ExpiresAt) {
		return cached.Value
	}

	// Cache miss or expired, call API
	url := strings.TrimRight(w.APIURL, "/") + "/api/v1/workspaces/" + w.Slug + "/secrets/" + name + "/"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return w.fallbackCached(cached, name, err)
	}
	req.Header.Set("Authorization", "Bearer "+w.Token)
	if cached != nil && cached.ETag != "" {
		req.Header.Set("If-None-Match", cached.ETag)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return w.fallbackCached(cached, name, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		// Extend cache TTL
		if cached != nil {
			cached.ExpiresAt = time.Now().Add(workspaceCacheTTL)
			w.saveCacheEntry(cacheKey, cached)
			return cached.Value
		}
		return ""

	case http.StatusOK:
		var result struct {
			Value string `json:"value"`
		}
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return w.fallbackCached(cached, name, readErr)
		}
		if jsonErr := json.Unmarshal(body, &result); jsonErr != nil {
			return w.fallbackCached(cached, name, jsonErr)
		}
		etag := resp.Header.Get("ETag")
		entry := &workspaceCacheEntry{
			Value:     result.Value,
			ETag:      etag,
			ExpiresAt: time.Now().Add(workspaceCacheTTL),
		}
		w.saveCacheEntry(cacheKey, entry)
		return result.Value

	default:
		body, _ := io.ReadAll(resp.Body)
		apiErr := fmt.Errorf("workspace API returned %d: %s", resp.StatusCode, string(body))
		return w.fallbackCached(cached, name, apiErr)
	}
}

// fallbackCached returns a stale cached value if available, otherwise empty string.
// In both cases a warning is printed to stderr.
func (w *WorkspaceVaultResolver) fallbackCached(cached *workspaceCacheEntry, name string, err error) string {
	if cached != nil && cached.Value != "" {
		// Reject secrets that have been stale for too long
		if time.Since(cached.ExpiresAt) > maxStaleTTL {
			fmt.Fprintf(os.Stderr, "Warning: cached secret %q expired %v ago, rejecting (max stale: %v)\n", name, time.Since(cached.ExpiresAt).Round(time.Second), maxStaleTTL)
			return ""
		}
		fmt.Fprintf(os.Stderr, "Warning: using stale cached workspace secret %q: %v\n", name, err)
		return cached.Value
	}
	fmt.Fprintf(os.Stderr, "Warning: could not resolve workspace secret %q: %v\n", name, err)
	return ""
}

// loadCacheEntry reads and parses a cached workspace secret from the local vault.
func (w *WorkspaceVaultResolver) loadCacheEntry(cacheKey string) (*workspaceCacheEntry, error) {
	raw, err := w.Cache.Get(cacheKey)
	if err != nil {
		return nil, err
	}
	var entry workspaceCacheEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// saveCacheEntry stores a workspace secret cache entry in the local vault.
func (w *WorkspaceVaultResolver) saveCacheEntry(cacheKey string, entry *workspaceCacheEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = w.Cache.Set(cacheKey, string(data))
}
