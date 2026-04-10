// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package registry provides the API client, local caching, spec resolution,
// and index management for the clictl tool registry.
package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"os"
	"time"

	"github.com/clictl/cli/internal/models"
)

// Client is an API client for the clictl.dev registry service.
type Client struct {
	BaseURL    string
	AuthToken  string
	HTTPClient *http.Client
	Cache      *Cache
	NoCache    bool
}

// NewClient creates a new registry API client.
func NewClient(baseURL string, cache *Cache, noCache bool) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		Cache:   cache,
		NoCache: noCache,
	}
}

// doWithRetry executes an HTTP request with automatic retry on 429 (rate limit).
// It parses the Retry-After header and waits before retrying, up to maxRetries times.
func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	const maxRetries = 3

	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		// On last attempt, return the 429 response
		if attempt == maxRetries {
			return resp, nil
		}

		resp.Body.Close()

		// Parse Retry-After header
		waitSeconds := 1 << uint(attempt) // exponential backoff: 1, 2, 4
		if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
			if parsed, parseErr := time.ParseDuration(retryAfter + "s"); parseErr == nil {
				waitSeconds = int(parsed.Seconds())
			}
		}

		fmt.Fprintf(os.Stderr, "Rate limited. Retrying in %ds...\n", waitSeconds)

		select {
		case <-time.After(time.Duration(waitSeconds) * time.Second):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}

	// Should not reach here, but compile safety
	return c.HTTPClient.Do(req)
}

// newRequest creates an HTTP request with standard headers (Accept, User-Agent, Authorization).
func (c *Client) newRequest(ctx context.Context, method, u string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "clictl/1.0")
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	return req, nil
}

// SearchWithWorkspace queries the registry for tools matching the given query string,
// including private tools for the specified workspace.
func (c *Client) SearchWithWorkspace(ctx context.Context, query, workspace string) (*models.SearchResponse, error) {
	u := fmt.Sprintf("%s/api/v1/search/?q=%s", c.BaseURL, url.QueryEscape(query))
	if workspace != "" {
		u += "&workspace=" + url.QueryEscape(workspace)
	}
	return c.doSearch(ctx, u)
}

// Search queries the registry for tools matching the given query string.
func (c *Client) Search(ctx context.Context, query string) (*models.SearchResponse, error) {
	u := fmt.Sprintf("%s/api/v1/search/?q=%s", c.BaseURL, url.QueryEscape(query))
	return c.doSearch(ctx, u)
}

// doSearch performs the actual search HTTP request.
func (c *Client) doSearch(ctx context.Context, u string) (*models.SearchResponse, error) {

	req, err := c.newRequest(ctx, http.MethodGet, u)
	if err != nil {
		return nil, fmt.Errorf("creating search request: %w", err)
	}

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, fmt.Errorf("executing search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search API returned %d: %s", resp.StatusCode, string(body))
	}

	var result models.SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding search response: %w", err)
	}
	return &result, nil
}

// List retrieves all specs, optionally filtered by category.
func (c *Client) List(ctx context.Context, category string) (*models.ListResponse, error) {
	u := fmt.Sprintf("%s/api/v1/specs/", c.BaseURL)
	if category != "" {
		u += "?category=" + url.QueryEscape(category)
	}

	req, err := c.newRequest(ctx, http.MethodGet, u)
	if err != nil {
		return nil, fmt.Errorf("creating list request: %w", err)
	}

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, fmt.Errorf("executing list request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list API returned %d: %s", resp.StatusCode, string(body))
	}

	var result models.ListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding list response: %w", err)
	}
	return &result, nil
}

// GetSpec retrieves a spec's JSON detail from the registry.
func (c *Client) GetSpec(ctx context.Context, name string) (*models.ToolSpec, error) {
	u := fmt.Sprintf("%s/api/v1/specs/%s/", c.BaseURL, url.PathEscape(name))

	req, err := c.newRequest(ctx, http.MethodGet, u)
	if err != nil {
		return nil, fmt.Errorf("creating get spec request: %w", err)
	}

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, fmt.Errorf("executing get spec request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get spec API returned %d: %s", resp.StatusCode, string(body))
	}

	var spec models.ToolSpec
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		return nil, fmt.Errorf("decoding spec response: %w", err)
	}
	return &spec, nil
}

// GetSpecYAML retrieves a spec as raw YAML, using the cache with ETag validation.
// Returns the parsed ToolSpec and the raw YAML bytes.
func (c *Client) GetSpecYAML(ctx context.Context, name string) (*models.ToolSpec, []byte, error) {
	var cachedData []byte
	var storedETag string

	if c.Cache != nil && !c.NoCache {
		data, etag, fresh, err := c.Cache.Get(name)
		if err == nil && data != nil {
			if fresh {
				spec, parseErr := ParseSpec(data)
				if parseErr == nil {
					return spec, data, nil
				}
			}
			cachedData = data
			storedETag = etag
		}
	}

	var u string
	if parts := strings.SplitN(name, "/", 2); len(parts) == 2 {
		// Qualified name: namespace/tool-name
		u = fmt.Sprintf("%s/api/v1/specs/%s/%s/yaml/", c.BaseURL, url.PathEscape(parts[0]), url.PathEscape(parts[1]))
	} else {
		u = fmt.Sprintf("%s/api/v1/specs/%s/yaml/", c.BaseURL, url.PathEscape(name))
	}
	req, err := c.newRequest(ctx, http.MethodGet, u)
	if err != nil {
		return nil, nil, fmt.Errorf("creating get spec YAML request: %w", err)
	}
	req.Header.Set("Accept", "application/x-yaml")

	if storedETag != "" {
		req.Header.Set("If-None-Match", storedETag)
	}

	resp, err := c.doWithRetry(req)
	if err != nil {
		if cachedData != nil {
			spec, parseErr := ParseSpec(cachedData)
			if parseErr == nil {
				return spec, cachedData, nil
			}
		}
		return nil, nil, fmt.Errorf("executing get spec YAML request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified && cachedData != nil {
		if c.Cache != nil {
			newETag := resp.Header.Get("ETag")
			if newETag == "" {
				newETag = storedETag
			}
			_ = c.Cache.Put(name, cachedData, newETag)
		}
		spec, parseErr := ParseSpec(cachedData)
		if parseErr != nil {
			return nil, nil, fmt.Errorf("parsing cached spec after 304: %w", parseErr)
		}
		return spec, cachedData, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("get spec YAML API returned %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("reading spec YAML response body: %w", err)
	}

	spec, err := ParseSpec(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing fetched spec YAML: %w", err)
	}

	if c.Cache != nil {
		etag := resp.Header.Get("ETag")
		_ = c.Cache.Put(name, data, etag)
	}

	return spec, data, nil
}

// GetSpecVersionYAML retrieves a specific version of a spec as raw YAML.
// Returns the parsed ToolSpec and the raw YAML bytes.
func (c *Client) GetSpecVersionYAML(ctx context.Context, name, version string) (*models.ToolSpec, []byte, error) {
	cacheKey := name + "@" + version

	var cachedData []byte
	var storedETag string

	if c.Cache != nil && !c.NoCache {
		data, etag, fresh, err := c.Cache.Get(cacheKey)
		if err == nil && data != nil {
			if fresh {
				spec, parseErr := ParseSpec(data)
				if parseErr == nil {
					return spec, data, nil
				}
			}
			cachedData = data
			storedETag = etag
		}
	}

	u := fmt.Sprintf("%s/api/v1/specs/%s/versions/%s/", c.BaseURL, url.PathEscape(name), url.PathEscape(version))
	req, err := c.newRequest(ctx, http.MethodGet, u)
	if err != nil {
		return nil, nil, fmt.Errorf("creating get spec version YAML request: %w", err)
	}
	req.Header.Set("Accept", "application/x-yaml")

	if storedETag != "" {
		req.Header.Set("If-None-Match", storedETag)
	}

	resp, err := c.doWithRetry(req)
	if err != nil {
		if cachedData != nil {
			spec, parseErr := ParseSpec(cachedData)
			if parseErr == nil {
				return spec, cachedData, nil
			}
		}
		return nil, nil, fmt.Errorf("executing get spec version YAML request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified && cachedData != nil {
		if c.Cache != nil {
			newETag := resp.Header.Get("ETag")
			if newETag == "" {
				newETag = storedETag
			}
			_ = c.Cache.Put(cacheKey, cachedData, newETag)
		}
		spec, parseErr := ParseSpec(cachedData)
		if parseErr != nil {
			return nil, nil, fmt.Errorf("parsing cached spec after 304: %w", parseErr)
		}
		return spec, cachedData, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("get spec version YAML API returned %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("reading spec version YAML response body: %w", err)
	}

	spec, err := ParseSpec(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing fetched spec version YAML: %w", err)
	}

	if c.Cache != nil {
		etag := resp.Header.Get("ETag")
		_ = c.Cache.Put(cacheKey, data, etag)
	}

	return spec, data, nil
}

// GetPackByQualifiedName fetches a spec by its qualified name (namespace/name).
// This calls GET /api/v1/packs/:namespace/:name/ for direct resolution.
func (c *Client) GetPackByQualifiedName(ctx context.Context, namespace, name string) (*models.ToolSpec, []byte, error) {
	u := fmt.Sprintf("%s/api/v1/packs/%s/%s/", c.BaseURL, url.PathEscape(namespace), url.PathEscape(name))
	req, err := c.newRequest(ctx, http.MethodGet, u)
	if err != nil {
		return nil, nil, fmt.Errorf("creating qualified name request: %w", err)
	}
	req.Header.Set("Accept", "application/x-yaml")

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching qualified name spec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("qualified name lookup returned %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("reading qualified name response: %w", err)
	}

	spec, err := ParseSpec(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing qualified name spec: %w", err)
	}

	if c.Cache != nil {
		etag := resp.Header.Get("ETag")
		cacheKey := namespace + "/" + name
		_ = c.Cache.Put(cacheKey, data, etag)
	}

	return spec, data, nil
}

// ResolveToolByName searches for all published tools matching a name.
// Returns multiple results for disambiguation when unscoped.
func (c *Client) ResolveToolByName(ctx context.Context, name string) ([]models.SearchResult, error) {
	u := fmt.Sprintf("%s/api/v1/resolve/%s/", c.BaseURL, url.PathEscape(name))
	req, err := c.newRequest(ctx, http.MethodGet, u)
	if err != nil {
		return nil, fmt.Errorf("creating resolve request: %w", err)
	}

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, fmt.Errorf("resolving tool name: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resolve API returned %d", resp.StatusCode)
	}

	var results []models.SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("decoding resolve response: %w", err)
	}
	return results, nil
}

// GetCurrentUser fetches the authenticated user's profile.
func (c *Client) GetCurrentUser(ctx context.Context) (*models.UserInfo, error) {
	u := fmt.Sprintf("%s/api/v1/me/", c.BaseURL)
	req, err := c.newRequest(ctx, http.MethodGet, u)
	if err != nil {
		return nil, fmt.Errorf("creating user request: %w", err)
	}

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, fmt.Errorf("fetching user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("not logged in. Run 'clictl login' to authenticate")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("user API returned %d: %s", resp.StatusCode, string(body))
	}

	var user models.UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decoding user response: %w", err)
	}
	return &user, nil
}

// WorkspaceInfo represents a workspace from the API.
type WorkspaceInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	IsPersonal bool   `json:"is_personal"`
}

// GetWorkspaces fetches the user's workspaces.
func (c *Client) GetWorkspaces(ctx context.Context) ([]WorkspaceInfo, error) {
	u := fmt.Sprintf("%s/api/v1/workspaces/", c.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("workspaces API returned %d", resp.StatusCode)
	}

	var workspaces []WorkspaceInfo
	if err := json.NewDecoder(resp.Body).Decode(&workspaces); err != nil {
		return nil, err
	}
	return workspaces, nil
}

// OAuthTokenResponse is the response from the token exchange endpoint.
type OAuthTokenResponse struct {
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token"`
	TokenType     string `json:"token_type"`
	ExpiresIn     int    `json:"expires_in"`
	WorkspaceSlug string `json:"workspace_slug,omitempty"`
	WorkspaceName string `json:"workspace_name,omitempty"`
}

// ExchangeOAuthCode exchanges an authorization code for JWT tokens using PKCE.
func (c *Client) ExchangeOAuthCode(ctx context.Context, code, codeVerifier, redirectURI string) (*OAuthTokenResponse, error) {
	u := fmt.Sprintf("%s/api/v1/oauth/token/", c.BaseURL)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", "clictl")
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "clictl/1.0")

	// Send as JSON instead of form-encoded to match DRF expectations
	body := map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"client_id":     "clictl",
		"redirect_uri":  redirectURI,
		"code_verifier": codeVerifier,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling token request: %w", err)
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "clictl/1.0")

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange returned %d: %s", resp.StatusCode, string(respBody))
	}

	var tokenResp OAuthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	return &tokenResp, nil
}

// LogInvocation posts an invocation record to the registry. Best-effort, errors are ignored.
func (c *Client) LogInvocation(ctx context.Context, name string) {
	c.LogInvocationWithAction(ctx, name, "", "cli")
}

// LogInvocationWithAction records a tool invocation asynchronously for analytics.
func (c *Client) LogInvocationWithAction(ctx context.Context, name, actionName, source string) {
	u := fmt.Sprintf("%s/api/v1/specs/%s/invoke/", c.BaseURL, url.PathEscape(name))
	body := fmt.Sprintf(`{"action_name":%q,"source":%q}`, actionName, source)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// VersionDiff holds the diff between two spec versions.
type VersionDiff struct {
	OldVersion string `json:"old_version"`
	NewVersion string `json:"new_version"`
	Changes    []struct {
		Field string `json:"field"`
		Old   string `json:"old"`
		New   string `json:"new"`
	} `json:"changes"`
	Summary        string   `json:"summary"`
	IsBreaking     bool     `json:"is_breaking"`
	BreakingReasons []string `json:"breaking_reasons"`
}

// GetVersionDiff fetches the diff between two versions of a spec.
// Returns nil without error if the endpoint returns 404 (diff not available).
func (c *Client) GetVersionDiff(ctx context.Context, name, oldVersion, newVersion string) (*VersionDiff, error) {
	u := fmt.Sprintf("%s/api/v1/specs/%s/versions/%s/diff/%s/",
		c.BaseURL,
		url.PathEscape(name),
		url.PathEscape(oldVersion),
		url.PathEscape(newVersion),
	)

	req, err := c.newRequest(ctx, http.MethodGet, u)
	if err != nil {
		return nil, fmt.Errorf("creating version diff request: %w", err)
	}

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, fmt.Errorf("fetching version diff: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("version diff API returned %d: %s", resp.StatusCode, string(body))
	}

	var diff VersionDiff
	if err := json.NewDecoder(resp.Body).Decode(&diff); err != nil {
		return nil, fmt.Errorf("decoding version diff: %w", err)
	}
	return &diff, nil
}

// ToolRedirect represents a redirect from an old tool name to a new one.
type ToolRedirect struct {
	Redirect  bool   `json:"redirect"`
	OldName   string `json:"old_name"`
	NewName   string `json:"new_name"`
	ExpiresAt string `json:"expires_at"`
}

// CheckRedirect checks if a qualified name has been redirected to a new name.
// Returns nil if no redirect exists.
func (c *Client) CheckRedirect(ctx context.Context, namespace, name string) (*ToolRedirect, error) {
	u := fmt.Sprintf("%s/api/v1/packs/%s/%s/",
		c.BaseURL,
		url.PathEscape(namespace),
		url.PathEscape(name),
	)

	req, err := c.newRequest(ctx, http.MethodGet, u)
	if err != nil {
		return nil, fmt.Errorf("creating redirect check request: %w", err)
	}

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusMovedPermanently {
		var redirect ToolRedirect
		if decodeErr := json.NewDecoder(resp.Body).Decode(&redirect); decodeErr != nil {
			return nil, nil
		}
		return &redirect, nil
	}

	return nil, nil
}

// jsonReader wraps a byte slice to implement io.Reader.
func jsonReader(b []byte) io.Reader {
	return &jsonReaderImpl{data: b}
}

type jsonReaderImpl struct {
	data []byte
	pos  int
}

// Read returns the response body bytes.
func (r *jsonReaderImpl) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// GetSigstoreBundle fetches the Sigstore bundle for a tool from the registry API.
// Returns nil, nil if the bundle is not available (404).
func (c *Client) GetSigstoreBundle(ctx context.Context, name string) ([]byte, error) {
	u := fmt.Sprintf("%s/api/v1/packs/%s/sigstore-bundle", c.BaseURL, url.PathEscape(name))

	req, err := c.newRequest(ctx, http.MethodGet, u)
	if err != nil {
		return nil, fmt.Errorf("creating sigstore bundle request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, fmt.Errorf("fetching sigstore bundle: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sigstore bundle API returned %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading sigstore bundle response: %w", err)
	}

	return data, nil
}
