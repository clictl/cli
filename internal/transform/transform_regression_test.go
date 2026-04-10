// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package transform

import (
	"strings"
	"testing"
)

// Regression tests for bugs found in the transform pipeline.
// These test the full path from mapToStep (via ParseSteps) through Apply,
// simulating what happens when transformStepToMap in run.go passes Go-typed
// values ([]string, map[string]string) instead of JSON-unmarshaled types
// ([]any, map[string]any).

// Bug #1: type: truncate with max_items at top level caused nil pointer
// because mapToStep did not populate Truncate config from top-level fields.
func TestRegression_TruncateTopLevelMaxItems(t *testing.T) {
	raw := []any{
		map[string]any{
			"type":      "truncate",
			"max_items": 2,
		},
	}
	pipeline, err := ParseSteps(raw)
	if err != nil {
		t.Fatal(err)
	}

	data := parseJSON(t, `[{"a": 1}, {"a": 2}, {"a": 3}, {"a": 4}]`)
	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 2 {
		t.Errorf("expected 2 items after truncate, got %d", len(arr))
	}
}

// Bug #2: type: json with select passed as []string was not handled.
// transformStepToMap passes []string from the Go struct, but mapToStep
// originally only handled []any.
func TestRegression_JSONSelectAsStringSlice(t *testing.T) {
	raw := []any{
		map[string]any{
			"type":   "json",
			"select": []string{"name", "age"},
		},
	}
	pipeline, err := ParseSteps(raw)
	if err != nil {
		t.Fatal(err)
	}

	data := parseJSON(t, `{"name": "alice", "age": 30, "email": "a@b.com"}`)
	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}
	obj := result.(map[string]any)
	if obj["name"] != "alice" {
		t.Errorf("expected name 'alice', got %v", obj["name"])
	}
	if obj["age"] != float64(30) {
		t.Errorf("expected age 30, got %v", obj["age"])
	}
	if _, ok := obj["email"]; ok {
		t.Error("email should have been filtered out by select")
	}
}

// Bug #3: rename passed as map[string]string from transformStepToMap
// but mapToStep only handled map[string]any.
func TestRegression_RenameAsStringMap(t *testing.T) {
	raw := []any{
		map[string]any{
			"type":   "json",
			"rename": map[string]string{"full_name": "name", "years": "age"},
		},
	}
	pipeline, err := ParseSteps(raw)
	if err != nil {
		t.Fatal(err)
	}

	data := parseJSON(t, `[{"full_name": "alice", "years": 30}, {"full_name": "bob", "years": 25}]`)
	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	first := arr[0].(map[string]any)
	if first["name"] != "alice" {
		t.Errorf("expected renamed 'name' = 'alice', got %v", first["name"])
	}
	if first["age"] != float64(30) {
		t.Errorf("expected renamed 'age' = 30, got %v", first["age"])
	}
	if _, ok := first["full_name"]; ok {
		t.Error("original key 'full_name' should be renamed")
	}
}

// Bug #4: type: truncate with nil Truncate config should be a no-op, not panic.
func TestRegression_TruncateNilConfig(t *testing.T) {
	pipeline := Pipeline{
		{Type: "truncate"},
	}
	data := parseJSON(t, `[1, 2, 3]`)
	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 3 {
		t.Errorf("nil truncate config should be no-op, got %d items", len(arr))
	}
}

// Bug #5: type: html_to_markdown with nil HTMLToMarkdown config should
// still work (uses defaults).
func TestRegression_HTMLToMarkdownNilConfig(t *testing.T) {
	pipeline := Pipeline{
		{Type: "html_to_markdown"},
	}
	result, err := pipeline.Apply("<h1>Hello</h1><p>World</p>")
	if err != nil {
		t.Fatal(err)
	}
	s := result.(string)
	if !strings.Contains(s, "Hello") {
		t.Errorf("expected 'Hello' in output, got %q", s)
	}
	if !strings.Contains(s, "World") {
		t.Errorf("expected 'World' in output, got %q", s)
	}
}

// Bug #6: type: cost with nil Cost config should be a no-op, not panic.
func TestRegression_CostNilConfig(t *testing.T) {
	pipeline := Pipeline{
		{Type: "cost"},
	}
	result, err := pipeline.Apply("some data")
	if err != nil {
		t.Fatal(err)
	}
	if result != "some data" {
		t.Errorf("nil cost config should be no-op, got %v", result)
	}
}

// Bug #7: only passed as []string from transformStepToMap
// but mapToStep only handled []any.
func TestRegression_OnlyAsStringSlice(t *testing.T) {
	raw := []any{
		map[string]any{
			"type": "json",
			"only": []string{"search", "lookup"},
		},
	}
	pipeline, err := ParseSteps(raw)
	if err != nil {
		t.Fatal(err)
	}

	data := parseJSON(t, `[
		{"name": "search", "desc": "find"},
		{"name": "delete", "desc": "remove"},
		{"name": "lookup", "desc": "get"}
	]`)
	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 2 {
		t.Fatalf("expected 2 items after only filter, got %d", len(arr))
	}
	first := arr[0].(map[string]any)
	if first["name"] != "search" {
		t.Errorf("expected 'search', got %v", first["name"])
	}
}

// Bug #8: inject passed as map[string]string from transformStepToMap
// but mapToStep only handled map[string]any.
func TestRegression_InjectAsStringMap(t *testing.T) {
	raw := []any{
		map[string]any{
			"type":   "json",
			"inject": map[string]string{"source": "api", "version": "v2"},
		},
	}
	pipeline, err := ParseSteps(raw)
	if err != nil {
		t.Fatal(err)
	}

	data := parseJSON(t, `{"name": "test"}`)
	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}
	obj := result.(map[string]any)
	if obj["source"] != "api" {
		t.Errorf("expected injected 'source' = 'api', got %v", obj["source"])
	}
	if obj["name"] != "test" {
		t.Errorf("original field should be preserved, got %v", obj["name"])
	}
}

// Integration: Full pipeline from transformStepToMap-style maps through
// ParseSteps and Apply, simulating a nominatim-like spec.
func TestRegression_NominatimLikePipeline(t *testing.T) {
	// Simulate what transformStepToMap produces for a geocoding tool:
	// 1. Extract results array
	// 2. Select specific fields (passed as []string)
	// 3. Rename fields (passed as map[string]string)
	// 4. Truncate to top 3
	raw := []any{
		map[string]any{
			"type":    "json",
			"extract": "$.results",
			"select":  []string{"display_name", "lat", "lon", "type"},
			"rename":  map[string]string{"display_name": "name", "lat": "latitude", "lon": "longitude"},
		},
		map[string]any{
			"type":      "truncate",
			"max_items": 3,
		},
	}
	pipeline, err := ParseSteps(raw)
	if err != nil {
		t.Fatal(err)
	}

	data := parseJSON(t, `{
		"results": [
			{"display_name": "Paris, France", "lat": "48.8566", "lon": "2.3522", "type": "city", "importance": 0.9},
			{"display_name": "Paris, TX", "lat": "33.6609", "lon": "-95.5555", "type": "city", "importance": 0.5},
			{"display_name": "Paris, TN", "lat": "36.3020", "lon": "-88.3267", "type": "city", "importance": 0.3},
			{"display_name": "Paris, KY", "lat": "38.2098", "lon": "-84.2530", "type": "city", "importance": 0.2},
			{"display_name": "Paris, IL", "lat": "39.6112", "lon": "-87.6961", "type": "city", "importance": 0.1}
		]
	}`)
	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}

	arr := result.([]any)
	if len(arr) != 3 {
		t.Fatalf("expected 3 results after truncate, got %d", len(arr))
	}

	first := arr[0].(map[string]any)
	// Check rename worked
	if first["name"] != "Paris, France" {
		t.Errorf("expected renamed 'name' = 'Paris, France', got %v", first["name"])
	}
	if first["latitude"] != "48.8566" {
		t.Errorf("expected renamed 'latitude', got %v", first["latitude"])
	}
	if first["longitude"] != "2.3522" {
		t.Errorf("expected renamed 'longitude', got %v", first["longitude"])
	}
	// Check select filtered out 'importance'
	if _, ok := first["importance"]; ok {
		t.Error("importance should have been filtered out by select")
	}
	// Check original keys are gone
	if _, ok := first["display_name"]; ok {
		t.Error("display_name should have been renamed to name")
	}
}

// Verify that max_items passed as int (not float64) works in truncate.
// YAML unmarshaling may produce int, while JSON produces float64.
func TestRegression_TruncateMaxItemsAsInt(t *testing.T) {
	raw := []any{
		map[string]any{
			"type":      "truncate",
			"max_items": int(2), // YAML produces int
		},
	}
	pipeline, err := ParseSteps(raw)
	if err != nil {
		t.Fatal(err)
	}
	data := parseJSON(t, `[1, 2, 3, 4]`)
	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 2 {
		t.Errorf("expected 2, got %d", len(arr))
	}
}

// Verify that max_items as float64 also works (JSON unmarshaling).
func TestRegression_TruncateMaxItemsAsFloat64(t *testing.T) {
	raw := []any{
		map[string]any{
			"type":      "truncate",
			"max_items": float64(2),
		},
	}
	pipeline, err := ParseSteps(raw)
	if err != nil {
		t.Fatal(err)
	}
	data := parseJSON(t, `[1, 2, 3, 4]`)
	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 2 {
		t.Errorf("expected 2, got %d", len(arr))
	}
}

// ---------------------------------------------------------------------------
// htmlToMD comprehensive tests
// ---------------------------------------------------------------------------

func TestHTMLToMD_Headings(t *testing.T) {
	result := htmlToMD("<h1>Title</h1><h2>Subtitle</h2><h3>Section</h3>")
	if !strings.Contains(result, "# Title") {
		t.Errorf("missing h1 heading in %q", result)
	}
	if !strings.Contains(result, "## Subtitle") {
		t.Errorf("missing h2 heading in %q", result)
	}
	if !strings.Contains(result, "### Section") {
		t.Errorf("missing h3 heading in %q", result)
	}
}

func TestHTMLToMD_Links(t *testing.T) {
	result := htmlToMD(`<a href="https://example.com">Click here</a>`)
	if !strings.Contains(result, "[Click here](https://example.com)") {
		t.Errorf("expected markdown link, got %q", result)
	}
}

func TestHTMLToMD_Bold_Italic(t *testing.T) {
	result := htmlToMD("<strong>bold</strong> and <em>italic</em>")
	if !strings.Contains(result, "**bold**") {
		t.Errorf("expected bold, got %q", result)
	}
	if !strings.Contains(result, "*italic*") {
		t.Errorf("expected italic, got %q", result)
	}
}

func TestHTMLToMD_UnorderedList(t *testing.T) {
	result := htmlToMD("<ul><li>First</li><li>Second</li></ul>")
	if !strings.Contains(result, "- First") {
		t.Errorf("expected list item, got %q", result)
	}
	if !strings.Contains(result, "- Second") {
		t.Errorf("expected second list item, got %q", result)
	}
}

func TestHTMLToMD_OrderedList(t *testing.T) {
	result := htmlToMD("<ol><li>First</li><li>Second</li></ol>")
	if !strings.Contains(result, "1. First") {
		t.Errorf("expected numbered list, got %q", result)
	}
	if !strings.Contains(result, "2. Second") {
		t.Errorf("expected second numbered item, got %q", result)
	}
}

func TestHTMLToMD_HTMLEntities(t *testing.T) {
	result := htmlToMD("<p>Tom &amp; Jerry &lt;3&gt; &#x27;hello&#x27;</p>")
	if !strings.Contains(result, "Tom & Jerry") {
		t.Errorf("expected decoded &amp;, got %q", result)
	}
	if !strings.Contains(result, "<3>") {
		t.Errorf("expected decoded &lt;&gt;, got %q", result)
	}
	if !strings.Contains(result, "'hello'") {
		t.Errorf("expected decoded &#x27;, got %q", result)
	}
}

func TestHTMLToMD_NBSP(t *testing.T) {
	result := htmlToMD("<p>word1&nbsp;word2</p>")
	if !strings.Contains(result, "word1") || !strings.Contains(result, "word2") {
		t.Errorf("expected words, got %q", result)
	}
}

func TestHTMLToMD_StripsScriptAndStyle(t *testing.T) {
	result := htmlToMD("<p>visible</p><script>alert('xss')</script><style>.x{color:red}</style><p>also visible</p>")
	if strings.Contains(result, "alert") {
		t.Errorf("script content should be stripped, got %q", result)
	}
	if strings.Contains(result, "color") {
		t.Errorf("style content should be stripped, got %q", result)
	}
	if !strings.Contains(result, "visible") {
		t.Errorf("visible content missing, got %q", result)
	}
}

func TestHTMLToMD_PreCodeBlock(t *testing.T) {
	result := htmlToMD("<pre><code>func main() {\n  fmt.Println()\n}</code></pre>")
	if !strings.Contains(result, "```") {
		t.Errorf("expected code fence, got %q", result)
	}
	if !strings.Contains(result, "func main()") {
		t.Errorf("expected code content, got %q", result)
	}
}

func TestHTMLToMD_Image(t *testing.T) {
	result := htmlToMD(`<img src="photo.jpg" alt="A photo">`)
	if !strings.Contains(result, "![A photo](photo.jpg)") {
		t.Errorf("expected markdown image, got %q", result)
	}
}

func TestHTMLToMD_AttributesOnTags(t *testing.T) {
	result := htmlToMD(`<h1 class="title" id="main">Title</h1><p style="color:red">Text</p>`)
	if !strings.Contains(result, "# Title") {
		t.Errorf("expected heading despite attributes, got %q", result)
	}
	if !strings.Contains(result, "Text") {
		t.Errorf("expected paragraph text despite attributes, got %q", result)
	}
}

func TestHTMLToMD_NestedTags(t *testing.T) {
	result := htmlToMD("<p>This is <strong>very <em>important</em> text</strong>.</p>")
	if !strings.Contains(result, "**very *important* text**") {
		t.Errorf("expected nested formatting, got %q", result)
	}
}

func TestHTMLToMD_ComplexPage(t *testing.T) {
	input := `<html><head><title>Test</title></head><body>
		<table><tr><td class="title"><span class="titleline"><a href="https://example.com">Story Title</a></span></td></tr></table>
		<p>325&nbsp;comments</p>
	</body></html>`
	result := htmlToMD(input)
	if !strings.Contains(result, "[Story Title](https://example.com)") {
		t.Errorf("expected link with text, got %q", result)
	}
	if !strings.Contains(result, "comments") {
		t.Errorf("expected comments text, got %q", result)
	}
}
