// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package permissions provides permission checking for tool access in
// workspace-enabled CLI sessions. It wraps API calls with an in-memory
// cache to reduce latency on repeated checks.
package permissions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// checkResponse is the JSON shape returned by the permissions check API.
type checkResponse struct {
	Allowed    bool   `json:"allowed"`
	CanRequest bool   `json:"can_request"`
	Reason     string `json:"reason"`
}

// Checker performs permission checks against the clictl API and caches results.
type Checker struct {
	baseURL   string
	authToken string
	client    *http.Client
	cache     *cache
}

// NewChecker creates a Checker that talks to the given API base URL.
func NewChecker(baseURL, authToken string) *Checker {
	return &Checker{
		baseURL:   baseURL,
		authToken: authToken,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache: newCache(),
	}
}

// Check verifies whether the current user may use tool/action in the given
// workspace. Results are cached for 5 minutes.
func (c *Checker) Check(ctx context.Context, workspace, tool, action string) (allowed bool, canRequest bool, reason string, err error) {
	// Try cache first.
	if allowed, canRequest, reason, ok := c.cache.get(workspace, tool, action); ok {
		return allowed, canRequest, reason, nil
	}

	// Build the API request.
	u := fmt.Sprintf("%s/api/v1/permissions/check/?tool=%s&action=%s&workspace=%s",
		c.baseURL,
		url.QueryEscape(tool),
		url.QueryEscape(action),
		url.QueryEscape(workspace),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, false, "", fmt.Errorf("creating permission check request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "clictl/1.0")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return false, false, "", fmt.Errorf("permission check request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, false, "", fmt.Errorf("permission check API returned %d: %s", resp.StatusCode, string(body))
	}

	var result checkResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, false, "", fmt.Errorf("decoding permission check response: %w", err)
	}

	// Cache the result.
	c.cache.set(workspace, tool, action, result.Allowed, result.CanRequest, result.Reason)

	return result.Allowed, result.CanRequest, result.Reason, nil
}
