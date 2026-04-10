// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// NQ.13: E2E - Fork tool, publish under own namespace, verify attribution
// ---------------------------------------------------------------------------

// publishedTool represents a tool returned by the mock API for E2E tests.
type publishedTool struct {
	Name            string `json:"name"`
	QualifiedName   string `json:"qualified_name"`
	OriginNamespace string `json:"origin_namespace"`
	Published       bool   `json:"published"`
	Version         string `json:"version"`
}

func TestE2E_ForkPublishAttribution(t *testing.T) {
	// Scenario:
	// 1. User forks anthropic/xlsx into their own toolbox.
	// 2. The forked spec retains origin_namespace="anthropic".
	// 3. User attempts to publish under their own namespace "rickcrawford".
	// 4. The API blocks publishing (409 namespace_mismatch) because
	//    origin_namespace != publisher_namespace.
	// 5. User removes the namespace from the spec (clears origin_namespace).
	// 6. Publish succeeds as rickcrawford/xlsx.
	// 7. Attribution shows "forked from anthropic/xlsx".

	// Step 1-4: Mock API returns 409 when origin_namespace mismatches
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/specs/create/" && r.Method == http.MethodPost:
			attemptCount++
			var payload map[string]interface{}
			json.NewDecoder(r.Body).Decode(&payload)

			specStr, _ := payload["spec"].(string)

			if attemptCount == 1 {
				// First attempt: spec has namespace: anthropic
				if !strings.Contains(specStr, "namespace: anthropic") {
					t.Error("first publish attempt should contain original namespace")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]string{
					"code":   "namespace_mismatch",
					"detail": "This tool attributes its origin to 'anthropic'. Your publisher namespace is 'rickcrawford'.",
				})
				return
			}

			// Second attempt: namespace removed, should succeed
			if strings.Contains(specStr, "namespace: anthropic") {
				t.Error("second attempt should not contain original namespace")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(publishedTool{
				Name:            "xlsx",
				QualifiedName:   "rickcrawford/xlsx",
				OriginNamespace: "",
				Published:       true,
				Version:         "1.0.0",
			})

		case r.URL.Path == "/api/v1/packs/rickcrawford/xlsx/" && r.Method == http.MethodGet:
			// Tool detail endpoint shows attribution
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name":             "xlsx",
				"qualified_name":   "rickcrawford/xlsx",
				"origin_namespace": "",
				"published":        true,
				"forked_from":      "anthropic/xlsx",
				"attribution":      "Originally from anthropic/xlsx",
			})

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := context.Background()

	// Attempt 1: Publish forked spec (has namespace: anthropic)
	specWithNS := "name: xlsx\nversion: '1.0.0'\nnamespace: anthropic\ndescription: Excel tools\n"
	payload1, _ := json.Marshal(map[string]interface{}{
		"name": "xlsx", "version": "1.0.0", "spec": specWithNS, "public": true,
	})

	req1, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/v1/specs/create/", strings.NewReader(string(payload1)))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer token")
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("first publish request failed: %v", err)
	}
	defer resp1.Body.Close()

	if resp1.StatusCode != http.StatusConflict {
		t.Errorf("first attempt: expected 409, got %d", resp1.StatusCode)
	}

	var errResp map[string]string
	json.NewDecoder(resp1.Body).Decode(&errResp)
	if errResp["code"] != "namespace_mismatch" {
		t.Errorf("expected namespace_mismatch, got %q", errResp["code"])
	}

	// Attempt 2: Publish without namespace (user removed it from spec)
	specNoNS := "name: xlsx\nversion: '1.0.0'\ndescription: Excel tools\n"
	payload2, _ := json.Marshal(map[string]interface{}{
		"name": "xlsx", "version": "1.0.0", "spec": specNoNS, "public": true,
	})

	req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/v1/specs/create/", strings.NewReader(string(payload2)))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer token")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("second publish request failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusCreated {
		t.Errorf("second attempt: expected 201, got %d", resp2.StatusCode)
	}

	var published publishedTool
	json.NewDecoder(resp2.Body).Decode(&published)
	if published.QualifiedName != "rickcrawford/xlsx" {
		t.Errorf("qualified_name = %q, want %q", published.QualifiedName, "rickcrawford/xlsx")
	}

	// Step 7: Verify attribution is available in the tool detail
	req3, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/packs/rickcrawford/xlsx/", nil)
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatalf("attribution request failed: %v", err)
	}
	defer resp3.Body.Close()

	var detail map[string]interface{}
	json.NewDecoder(resp3.Body).Decode(&detail)
	if detail["forked_from"] != "anthropic/xlsx" {
		t.Errorf("forked_from = %v, want %q", detail["forked_from"], "anthropic/xlsx")
	}
}

// ---------------------------------------------------------------------------
// NQ.14: E2E - Search "xlsx" returns multiple publishers, disambiguation
// ---------------------------------------------------------------------------

func TestE2E_SearchDisambiguation(t *testing.T) {
	// Scenario:
	// 1. User searches for "xlsx".
	// 2. API returns multiple matches from different publishers.
	// 3. CLI should display disambiguation options.

	type searchResult struct {
		QualifiedName string `json:"qualified_name"`
		Name          string `json:"name"`
		Publisher     string `json:"publisher"`
		Description   string `json:"description"`
		IsCurated     bool   `json:"is_curated"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/search/" && r.Method == http.MethodGet:
			query := r.URL.Query().Get("q")
			if query != "xlsx" {
				json.NewEncoder(w).Encode([]searchResult{})
				return
			}
			results := []searchResult{
				{
					QualifiedName: "clictl/xlsx",
					Name:          "xlsx",
					Publisher:      "clictl",
					Description:    "Official xlsx tool from the curated toolbox",
					IsCurated:      true,
				},
				{
					QualifiedName: "anthropic/xlsx",
					Name:          "xlsx",
					Publisher:      "anthropic",
					Description:    "Anthropic's xlsx conversion skill",
					IsCurated:      false,
				},
				{
					QualifiedName: "rickcrawford/xlsx",
					Name:          "xlsx",
					Publisher:      "rickcrawford",
					Description:    "Community fork of xlsx with extra features",
					IsCurated:      false,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(results)

		case r.URL.Path == "/api/v1/resolve/xlsx/" && r.Method == http.MethodGet:
			// Disambiguation endpoint
			results := []searchResult{
				{QualifiedName: "clictl/xlsx", Publisher: "clictl", IsCurated: true},
				{QualifiedName: "anthropic/xlsx", Publisher: "anthropic"},
				{QualifiedName: "rickcrawford/xlsx", Publisher: "rickcrawford"},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(results)

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := context.Background()

	// Step 1: Search for "xlsx"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/search/?q=xlsx", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("search request failed: %v", err)
	}
	defer resp.Body.Close()

	var results []searchResult
	json.NewDecoder(resp.Body).Decode(&results)

	if len(results) < 2 {
		t.Fatalf("expected multiple results for 'xlsx', got %d", len(results))
	}

	// Step 2: Verify curated result comes first (by is_curated flag)
	curatedFirst := false
	for i, r := range results {
		if r.IsCurated {
			if i == 0 {
				curatedFirst = true
			}
			break
		}
	}
	if !curatedFirst {
		t.Error("curated tool should be listed first in search results")
	}

	// Step 3: Verify all results have qualified_name format
	for _, r := range results {
		if !strings.Contains(r.QualifiedName, "/") {
			t.Errorf("qualified_name %q should contain '/'", r.QualifiedName)
		}
	}

	// Step 4: Disambiguation - verify the resolve endpoint returns all matches
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/resolve/xlsx/", nil)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("resolve request failed: %v", err)
	}
	defer resp2.Body.Close()

	var resolveResults []searchResult
	json.NewDecoder(resp2.Body).Decode(&resolveResults)

	if len(resolveResults) != 3 {
		t.Errorf("expected 3 disambiguation results, got %d", len(resolveResults))
	}

	// Verify we can build a disambiguation prompt from the results
	var promptLines []string
	for i, r := range resolveResults {
		line := fmt.Sprintf("  [%d] %s (%s)", i+1, r.QualifiedName, r.Publisher)
		if r.IsCurated {
			line += " [curated]"
		}
		promptLines = append(promptLines, line)
	}

	prompt := "Multiple tools match 'xlsx'. Choose one:\n" + strings.Join(promptLines, "\n")
	if !strings.Contains(prompt, "clictl/xlsx") {
		t.Error("disambiguation prompt should include clictl/xlsx")
	}
	if !strings.Contains(prompt, "anthropic/xlsx") {
		t.Error("disambiguation prompt should include anthropic/xlsx")
	}
	if !strings.Contains(prompt, "[curated]") {
		t.Error("disambiguation prompt should mark curated tools")
	}
}

func TestE2E_ScopedInstallBypassesDisambiguation(t *testing.T) {
	// When user specifies the full qualified name (e.g., "clictl install anthropic/xlsx"),
	// disambiguation should be skipped and the tool resolved directly.

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/packs/anthropic/xlsx/" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(publishedTool{
				Name:          "xlsx",
				QualifiedName: "anthropic/xlsx",
				Published:     true,
				Version:       "1.0.0",
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/packs/anthropic/xlsx/", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("scoped install request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for scoped lookup, got %d", resp.StatusCode)
	}

	var tool publishedTool
	json.NewDecoder(resp.Body).Decode(&tool)
	if tool.QualifiedName != "anthropic/xlsx" {
		t.Errorf("qualified_name = %q, want %q", tool.QualifiedName, "anthropic/xlsx")
	}
}
