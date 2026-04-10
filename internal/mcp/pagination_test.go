// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"encoding/json"
	"testing"
)

// TestPaginationLoopCollectsAllPages simulates iterating through paginated
// results by checking NextCursor across multiple ResourcesListResult pages.
func TestPaginationLoopCollectsAllPages(t *testing.T) {
	// Simulate 3 pages of resources
	pages := []ResourcesListResult{
		{
			Resources:  []Resource{{URI: "file:///a", Name: "a"}, {URI: "file:///b", Name: "b"}},
			NextCursor: "cursor-page2",
		},
		{
			Resources:  []Resource{{URI: "file:///c", Name: "c"}},
			NextCursor: "cursor-page3",
		},
		{
			Resources:  []Resource{{URI: "file:///d", Name: "d"}},
			NextCursor: "", // last page
		},
	}

	var allResources []Resource
	for i, page := range pages {
		// Simulate JSON wire format
		data, err := json.Marshal(page)
		if err != nil {
			t.Fatalf("page %d marshal: %v", i, err)
		}

		var result ResourcesListResult
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("page %d unmarshal: %v", i, err)
		}

		allResources = append(allResources, result.Resources...)

		if result.NextCursor == "" {
			break
		}
	}

	if len(allResources) != 4 {
		t.Errorf("total resources: got %d, want 4", len(allResources))
	}

	expectedURIs := []string{"file:///a", "file:///b", "file:///c", "file:///d"}
	for i, want := range expectedURIs {
		if allResources[i].URI != want {
			t.Errorf("resource[%d].URI: got %q, want %q", i, allResources[i].URI, want)
		}
	}
}

// TestPaginationTerminatesOnEmptyCursor verifies that an empty NextCursor
// signals the final page for all paginated result types.
func TestPaginationTerminatesOnEmptyCursor(t *testing.T) {
	tests := []struct {
		name       string
		jsonResult string
		wantEmpty  bool
	}{
		{
			name:       "ResourcesListResult with cursor",
			jsonResult: `{"resources":[],"nextCursor":"abc"}`,
			wantEmpty:  false,
		},
		{
			name:       "ResourcesListResult without cursor",
			jsonResult: `{"resources":[]}`,
			wantEmpty:  true,
		},
		{
			name:       "PromptsListResult with cursor",
			jsonResult: `{"prompts":[],"nextCursor":"xyz"}`,
			wantEmpty:  false,
		},
		{
			name:       "PromptsListResult without cursor",
			jsonResult: `{"prompts":[]}`,
			wantEmpty:  true,
		},
		{
			name:       "ResourceTemplatesListResult with cursor",
			jsonResult: `{"resourceTemplates":[],"nextCursor":"tmpl-cursor"}`,
			wantEmpty:  false,
		},
		{
			name:       "ResourceTemplatesListResult without cursor",
			jsonResult: `{"resourceTemplates":[]}`,
			wantEmpty:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Try each paginated type and check NextCursor
			var cursor string

			// Attempt ResourcesListResult
			var rlr ResourcesListResult
			if err := json.Unmarshal([]byte(tt.jsonResult), &rlr); err == nil && rlr.NextCursor != "" {
				cursor = rlr.NextCursor
			}

			// Attempt PromptsListResult
			var plr PromptsListResult
			if err := json.Unmarshal([]byte(tt.jsonResult), &plr); err == nil && plr.NextCursor != "" {
				cursor = plr.NextCursor
			}

			// Attempt ResourceTemplatesListResult
			var rtlr ResourceTemplatesListResult
			if err := json.Unmarshal([]byte(tt.jsonResult), &rtlr); err == nil && rtlr.NextCursor != "" {
				cursor = rtlr.NextCursor
			}

			isEmpty := cursor == ""
			if isEmpty != tt.wantEmpty {
				t.Errorf("cursor empty: got %v, want %v (cursor=%q)", isEmpty, tt.wantEmpty, cursor)
			}
		})
	}
}

// TestPaginationSinglePage verifies that a single-page result has no cursor
// and contains all expected items.
func TestPaginationSinglePage(t *testing.T) {
	result := ResourcesListResult{
		Resources: []Resource{
			{URI: "file:///only", Name: "only-file"},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Verify NextCursor is omitted from JSON
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}

	if _, hasCursor := raw["nextCursor"]; hasCursor {
		t.Error("single-page result should not include nextCursor in JSON")
	}

	// Verify roundtrip
	var roundtrip ResourcesListResult
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if roundtrip.NextCursor != "" {
		t.Errorf("NextCursor should be empty, got %q", roundtrip.NextCursor)
	}
	if len(roundtrip.Resources) != 1 {
		t.Fatalf("Resources count: got %d, want 1", len(roundtrip.Resources))
	}
	if roundtrip.Resources[0].Name != "only-file" {
		t.Errorf("Resource name: got %q", roundtrip.Resources[0].Name)
	}
}
