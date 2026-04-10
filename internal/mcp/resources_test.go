// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"encoding/json"
	"testing"
)

func TestResourceSerialization(t *testing.T) {
	tests := []struct {
		name string
		res  Resource
		want map[string]any
	}{
		{
			name: "minimal resource",
			res:  Resource{URI: "file:///tmp/a.txt", Name: "a.txt"},
			want: map[string]any{"uri": "file:///tmp/a.txt", "name": "a.txt"},
		},
		{
			name: "full resource with annotations",
			res: Resource{
				URI:         "file:///project/README.md",
				Name:        "README.md",
				Description: "Project readme",
				MimeType:    "text/markdown",
				Size:        1024,
				Annotations: &Annotations{
					Audience: []string{"user"},
					Priority: 0.8,
				},
			},
			want: map[string]any{
				"uri":         "file:///project/README.md",
				"name":        "README.md",
				"description": "Project readme",
				"mimeType":    "text/markdown",
				"size":        float64(1024),
				"annotations": map[string]any{
					"audience": []any{"user"},
					"priority": float64(0.8),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.res)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			for k, wantVal := range tt.want {
				gotVal, ok := got[k]
				if !ok {
					t.Errorf("missing key %q in serialized output", k)
					continue
				}
				wantJSON, _ := json.Marshal(wantVal)
				gotJSON, _ := json.Marshal(gotVal)
				if string(wantJSON) != string(gotJSON) {
					t.Errorf("key %q: got %s, want %s", k, gotJSON, wantJSON)
				}
			}

			// Roundtrip: unmarshal back into Resource
			var roundtrip Resource
			if err := json.Unmarshal(data, &roundtrip); err != nil {
				t.Fatalf("Roundtrip unmarshal: %v", err)
			}
			if roundtrip.URI != tt.res.URI {
				t.Errorf("URI roundtrip: got %q, want %q", roundtrip.URI, tt.res.URI)
			}
			if roundtrip.Name != tt.res.Name {
				t.Errorf("Name roundtrip: got %q, want %q", roundtrip.Name, tt.res.Name)
			}
		})
	}
}

func TestResourceTemplateSerialization(t *testing.T) {
	tests := []struct {
		name string
		tmpl ResourceTemplate
		key  string
		want string
	}{
		{
			name: "basic template",
			tmpl: ResourceTemplate{
				URITemplate: "file:///project/{path}",
				Name:        "Project File",
			},
			key:  "uriTemplate",
			want: "file:///project/{path}",
		},
		{
			name: "template with description",
			tmpl: ResourceTemplate{
				URITemplate: "db://records/{id}",
				Name:        "DB Record",
				Description: "Fetch a database record by ID",
				MimeType:    "application/json",
			},
			key:  "description",
			want: "Fetch a database record by ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.tmpl)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			val, ok := got[tt.key]
			if !ok {
				t.Fatalf("missing key %q", tt.key)
			}
			if val != tt.want {
				t.Errorf("key %q: got %v, want %v", tt.key, val, tt.want)
			}

			// Roundtrip
			var roundtrip ResourceTemplate
			if err := json.Unmarshal(data, &roundtrip); err != nil {
				t.Fatalf("Roundtrip: %v", err)
			}
			if roundtrip.URITemplate != tt.tmpl.URITemplate {
				t.Errorf("URITemplate roundtrip: got %q, want %q", roundtrip.URITemplate, tt.tmpl.URITemplate)
			}
		})
	}
}

func TestResourceContentTextAndBlob(t *testing.T) {
	tests := []struct {
		name    string
		content ResourceContent
		hasText bool
		hasBlob bool
	}{
		{
			name: "text content",
			content: ResourceContent{
				URI:      "file:///hello.txt",
				MimeType: "text/plain",
				Text:     "Hello, world!",
			},
			hasText: true,
			hasBlob: false,
		},
		{
			name: "blob content",
			content: ResourceContent{
				URI:      "file:///image.png",
				MimeType: "image/png",
				Blob:     "iVBORw0KGgo=",
			},
			hasText: false,
			hasBlob: true,
		},
		{
			name: "both text and blob",
			content: ResourceContent{
				URI:  "file:///mixed",
				Text: "some text",
				Blob: "c29tZSBibG9i",
			},
			hasText: true,
			hasBlob: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.content)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			_, textPresent := got["text"]
			_, blobPresent := got["blob"]

			if tt.hasText && !textPresent {
				t.Error("expected text field in output")
			}
			if tt.hasBlob && !blobPresent {
				t.Error("expected blob field in output")
			}

			var roundtrip ResourceContent
			if err := json.Unmarshal(data, &roundtrip); err != nil {
				t.Fatalf("Roundtrip: %v", err)
			}
			if roundtrip.Text != tt.content.Text {
				t.Errorf("Text roundtrip: got %q, want %q", roundtrip.Text, tt.content.Text)
			}
			if roundtrip.Blob != tt.content.Blob {
				t.Errorf("Blob roundtrip: got %q, want %q", roundtrip.Blob, tt.content.Blob)
			}
		})
	}
}

func TestResourceReadResultMultipleContents(t *testing.T) {
	result := ResourceReadResult{
		Contents: []ResourceContent{
			{URI: "file:///dir/a.txt", MimeType: "text/plain", Text: "file a"},
			{URI: "file:///dir/b.txt", MimeType: "text/plain", Text: "file b"},
			{URI: "file:///dir/c.bin", MimeType: "application/octet-stream", Blob: "AQID"},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var roundtrip ResourceReadResult
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(roundtrip.Contents) != 3 {
		t.Fatalf("Contents count: got %d, want 3", len(roundtrip.Contents))
	}

	if roundtrip.Contents[0].URI != "file:///dir/a.txt" {
		t.Errorf("Contents[0].URI: got %q", roundtrip.Contents[0].URI)
	}
	if roundtrip.Contents[1].Text != "file b" {
		t.Errorf("Contents[1].Text: got %q", roundtrip.Contents[1].Text)
	}
	if roundtrip.Contents[2].Blob != "AQID" {
		t.Errorf("Contents[2].Blob: got %q", roundtrip.Contents[2].Blob)
	}
}

func TestResourcesListResultPagination(t *testing.T) {
	tests := []struct {
		name       string
		result     ResourcesListResult
		wantCount  int
		wantCursor string
	}{
		{
			name: "with cursor",
			result: ResourcesListResult{
				Resources:  []Resource{{URI: "file:///a", Name: "a"}},
				NextCursor: "page2",
			},
			wantCount:  1,
			wantCursor: "page2",
		},
		{
			name: "empty cursor means last page",
			result: ResourcesListResult{
				Resources: []Resource{
					{URI: "file:///x", Name: "x"},
					{URI: "file:///y", Name: "y"},
				},
			},
			wantCount:  2,
			wantCursor: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.result)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var got ResourcesListResult
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			if len(got.Resources) != tt.wantCount {
				t.Errorf("Resources count: got %d, want %d", len(got.Resources), tt.wantCount)
			}
			if got.NextCursor != tt.wantCursor {
				t.Errorf("NextCursor: got %q, want %q", got.NextCursor, tt.wantCursor)
			}
		})
	}
}
