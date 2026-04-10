// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package transform

import (
	"encoding/json"
	"strings"
	"testing"
)

func parseJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("parseJSON: %v", err)
	}
	return v
}

func TestExtractSimple(t *testing.T) {
	data := parseJSON(t, `{"data": {"name": "test"}}`)
	result, err := applyExtract("$.data.name", data)
	if err != nil {
		t.Fatal(err)
	}
	if result != "test" {
		t.Errorf("expected 'test', got %v", result)
	}
}

func TestExtractArray(t *testing.T) {
	data := parseJSON(t, `{"items": [{"name": "a"}, {"name": "b"}]}`)
	result, err := applyExtract("$.items[0].name", data)
	if err != nil {
		t.Fatal(err)
	}
	if result != "a" {
		t.Errorf("expected 'a', got %v", result)
	}
}

func TestExtractRoot(t *testing.T) {
	data := parseJSON(t, `{"name": "test"}`)
	result, err := applyExtract("$", data)
	if err != nil {
		t.Fatal(err)
	}
	obj := result.(map[string]any)
	if obj["name"] != "test" {
		t.Errorf("expected root object")
	}
}

func TestExtractNotFound(t *testing.T) {
	data := parseJSON(t, `{"data": {}}`)
	_, err := applyExtract("$.data.missing", data)
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestSelectFields(t *testing.T) {
	data := parseJSON(t, `[
		{"name": "a", "status": "active", "internal_id": "123"},
		{"name": "b", "status": "done", "internal_id": "456"}
	]`)
	result, err := applySelect([]string{"name", "status"}, data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 2 {
		t.Fatalf("expected 2 items, got %d", len(arr))
	}
	first := arr[0].(map[string]any)
	if first["name"] != "a" {
		t.Errorf("expected name 'a'")
	}
	if _, exists := first["internal_id"]; exists {
		t.Errorf("internal_id should have been filtered out")
	}
}

func TestSelectNestedField(t *testing.T) {
	data := parseJSON(t, `[{"user": {"login": "rick"}, "title": "test"}]`)
	result, err := applySelect([]string{"title", "user.login"}, data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	first := arr[0].(map[string]any)
	if first["title"] != "test" {
		t.Errorf("expected title 'test'")
	}
	if first["user.login"] != "rick" {
		t.Errorf("expected user.login 'rick', got %v", first["user.login"])
	}
}

func TestTemplate(t *testing.T) {
	data := parseJSON(t, `[{"name": "a"}, {"name": "b"}]`)
	tmpl := `{{range .}}- {{.name}}
{{end}}`
	result, err := applyTemplate(tmpl, data)
	if err != nil {
		t.Fatal(err)
	}
	s := result.(string)
	if !strings.Contains(s, "- a") || !strings.Contains(s, "- b") {
		t.Errorf("unexpected template output: %q", s)
	}
}

func TestTemplateObject(t *testing.T) {
	data := parseJSON(t, `{"name": "test", "temp": 15.2}`)
	tmpl := `Name: {{.name}}, Temp: {{.temp}}`
	result, err := applyTemplate(tmpl, data)
	if err != nil {
		t.Fatal(err)
	}
	s := result.(string)
	if s != "Name: test, Temp: 15.2" {
		t.Errorf("unexpected: %q", s)
	}
}

func TestTruncateArray(t *testing.T) {
	data := parseJSON(t, `[1, 2, 3, 4, 5]`)
	result, err := applyTruncate(&TruncateConfig{MaxItems: 3}, data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 3 {
		t.Errorf("expected 3 items, got %d", len(arr))
	}
}

func TestTruncateString(t *testing.T) {
	result, err := applyTruncate(&TruncateConfig{MaxLength: 10}, "hello world this is long")
	if err != nil {
		t.Fatal(err)
	}
	s := result.(string)
	if len(s) > 13 { // 10 + "..."
		t.Errorf("expected truncated string, got %q (len %d)", s, len(s))
	}
	if !strings.HasSuffix(s, "...") {
		t.Errorf("expected '...' suffix, got %q", s)
	}
}

func TestTruncateNoOp(t *testing.T) {
	data := parseJSON(t, `[1, 2]`)
	result, err := applyTruncate(&TruncateConfig{MaxItems: 5}, data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 2 {
		t.Errorf("should not truncate below limit")
	}
}

func TestRename(t *testing.T) {
	data := parseJSON(t, `{"dt": 12345, "temp_max": 20}`)
	mapping := map[string]string{"dt": "date", "temp_max": "high"}
	result, err := applyRename(mapping, data)
	if err != nil {
		t.Fatal(err)
	}
	obj := result.(map[string]any)
	if _, exists := obj["dt"]; exists {
		t.Error("old key 'dt' should be renamed")
	}
	if obj["date"] != float64(12345) {
		t.Errorf("expected date=12345, got %v", obj["date"])
	}
	if obj["high"] != float64(20) {
		t.Errorf("expected high=20, got %v", obj["high"])
	}
}

func TestRenameArray(t *testing.T) {
	data := parseJSON(t, `[{"old": "val1"}, {"old": "val2"}]`)
	mapping := map[string]string{"old": "new"}
	result, err := applyRename(mapping, data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	first := arr[0].(map[string]any)
	if first["new"] != "val1" {
		t.Errorf("expected 'val1', got %v", first["new"])
	}
}

func TestHTMLToMarkdown(t *testing.T) {
	html := "<h1>Title</h1><p>This is <strong>bold</strong> and <em>italic</em>.</p>"
	result, err := applyHTMLToMarkdown(&HTMLToMDConfig{}, html)
	if err != nil {
		t.Fatal(err)
	}
	s := result.(string)
	if !strings.Contains(s, "# Title") {
		t.Errorf("expected markdown heading, got %q", s)
	}
	if !strings.Contains(s, "**bold**") {
		t.Errorf("expected bold markdown, got %q", s)
	}
	if !strings.Contains(s, "*italic*") {
		t.Errorf("expected italic markdown, got %q", s)
	}
}

func TestPipelineChain(t *testing.T) {
	data := parseJSON(t, `{
		"response": {
			"data": [
				{"name": "alice", "role": "admin", "internal_id": "x"},
				{"name": "bob", "role": "member", "internal_id": "y"},
				{"name": "carol", "role": "member", "internal_id": "z"}
			]
		}
	}`)

	pipeline := Pipeline{
		{Extract: "$.response.data"},
		{Select: []string{"name", "role"}},
		{Truncate: &TruncateConfig{MaxItems: 2}},
	}

	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}

	arr := result.([]any)
	if len(arr) != 2 {
		t.Fatalf("expected 2 items, got %d", len(arr))
	}

	first := arr[0].(map[string]any)
	if first["name"] != "alice" {
		t.Errorf("expected alice, got %v", first["name"])
	}
	if _, exists := first["internal_id"]; exists {
		t.Error("internal_id should have been filtered out by select")
	}
}

func TestPipelineWithTemplate(t *testing.T) {
	data := parseJSON(t, `{"items": [{"name": "a"}, {"name": "b"}]}`)

	pipeline := Pipeline{
		{Extract: "$.items"},
		{Template: "{{range .}}- {{.name}}\n{{end}}"},
	}

	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}

	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if !strings.Contains(s, "- a") || !strings.Contains(s, "- b") {
		t.Errorf("unexpected: %q", s)
	}
}

func TestPipelineEmpty(t *testing.T) {
	data := parseJSON(t, `{"x": 1}`)
	result, err := Pipeline{}.Apply(data)
	if err != nil {
		t.Fatal(err)
	}
	obj := result.(map[string]any)
	if obj["x"] != float64(1) {
		t.Error("empty pipeline should pass through")
	}
}

func TestParseStepsSingleMap(t *testing.T) {
	raw := map[string]any{"extract": "$.data"}
	pipeline, err := ParseSteps(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pipeline) != 1 {
		t.Fatalf("expected 1 step, got %d", len(pipeline))
	}
	if pipeline[0].Extract != "$.data" {
		t.Errorf("expected extract '$.data', got %q", pipeline[0].Extract)
	}
}

func TestParseStepsList(t *testing.T) {
	raw := []any{
		map[string]any{"extract": "$.items"},
		map[string]any{"truncate": map[string]any{"max_items": float64(5)}},
	}
	pipeline, err := ParseSteps(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pipeline) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(pipeline))
	}
	if pipeline[0].Extract != "$.items" {
		t.Error("first step should be extract")
	}
	if pipeline[1].Truncate == nil || pipeline[1].Truncate.MaxItems != 5 {
		t.Error("second step should be truncate with max_items=5")
	}
}

func TestParseStepsNil(t *testing.T) {
	pipeline, err := ParseSteps(nil)
	if err != nil {
		t.Fatal(err)
	}
	if pipeline != nil {
		t.Error("nil input should return nil pipeline")
	}
}

func TestJSTransform(t *testing.T) {
	data := parseJSON(t, `[{"name": "alice", "age": 30}, {"name": "bob", "age": 25}]`)
	script := `function transform(data) { return data.filter(function(d) { return d.age > 28; }); }`
	result, err := applyJS(script, data)
	if err != nil {
		t.Fatal(err)
	}
	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", result)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 result, got %d", len(arr))
	}
}

func TestJSTransformString(t *testing.T) {
	data := parseJSON(t, `{"items": [{"name": "a"}, {"name": "b"}]}`)
	script := `function transform(data) { return data.items.map(function(i) { return i.name; }).join(", "); }`
	result, err := applyJS(script, data)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T: %v", result, result)
	}
	if s != "a, b" {
		t.Errorf("expected 'a, b', got %q", s)
	}
}

func TestJSTransformInPipeline(t *testing.T) {
	data := parseJSON(t, `{"data": [1, 2, 3, 4, 5]}`)
	pipeline := Pipeline{
		{Extract: "$.data"},
		{JS: `function transform(data) { return data.filter(function(n) { return n > 3; }); }`},
	}
	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 2 {
		t.Fatalf("expected 2 items, got %d", len(arr))
	}
}

// --- JS Sandbox Security Tests ---

func TestJSBlocksFetch(t *testing.T) {
	data := parseJSON(t, `{}`)
	script := `function transform(data) { return fetch("https://evil.com"); }`
	_, err := applyJS(script, data)
	if err == nil {
		t.Fatal("expected error when calling fetch")
	}
}

func TestJSBlocksXMLHttpRequest(t *testing.T) {
	data := parseJSON(t, `{}`)
	script := `function transform(data) { var x = new XMLHttpRequest(); return data; }`
	_, err := applyJS(script, data)
	if err == nil {
		t.Fatal("expected error when using XMLHttpRequest")
	}
}

func TestJSBlocksEval(t *testing.T) {
	data := parseJSON(t, `{}`)
	script := `function transform(data) { return eval("1+1"); }`
	_, err := applyJS(script, data)
	if err == nil {
		t.Fatal("expected error when calling eval")
	}
}

func TestJSBlocksSetTimeout(t *testing.T) {
	data := parseJSON(t, `{}`)
	script := `function transform(data) { setTimeout(function(){}, 1000); return data; }`
	_, err := applyJS(script, data)
	if err == nil {
		t.Fatal("expected error when calling setTimeout")
	}
}

func TestJSBlocksRequire(t *testing.T) {
	data := parseJSON(t, `{}`)
	script := `function transform(data) { var fs = require("fs"); return data; }`
	_, err := applyJS(script, data)
	if err == nil {
		t.Fatal("expected error when calling require")
	}
}

func TestJSBlocksFunctionConstructor(t *testing.T) {
	data := parseJSON(t, `{}`)
	script := `function transform(data) { var f = new Function("return 1"); return data; }`
	_, err := applyJS(script, data)
	if err != nil {
		// Pattern blocked at validation stage
		if !strings.Contains(err.Error(), "blocked pattern") {
			t.Fatalf("expected blocked pattern error, got: %v", err)
		}
	}
}

func TestJSBlocksConstructorAccess(t *testing.T) {
	data := parseJSON(t, `{}`)
	script := `function transform(data) { return data.constructor("return 1")(); }`
	_, err := applyJS(script, data)
	if err == nil {
		t.Fatal("expected error when using .constructor()")
	}
}

func TestJSMissingTransformFunction(t *testing.T) {
	data := parseJSON(t, `{"x": 1}`)
	script := `var x = 1;`
	_, err := applyJS(script, data)
	if err == nil {
		t.Fatal("expected error for missing transform function")
	}
	if !strings.Contains(err.Error(), "must define a transform") {
		t.Fatalf("expected 'must define' error, got: %v", err)
	}
}

func TestJSSafeOperationsWork(t *testing.T) {
	data := parseJSON(t, `[3, 1, 4, 1, 5, 9, 2, 6]`)
	script := `function transform(data) {
		return data
			.filter(function(n) { return n > 3; })
			.sort(function(a, b) { return b - a; })
			.map(function(n) { return n * 2; });
	}`
	result, err := applyJS(script, data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	// 9, 6, 5, 4 > 3, sorted desc, doubled = 18, 12, 10, 8
	if len(arr) != 4 {
		t.Fatalf("expected 4, got %d", len(arr))
	}
	if arr[0] != float64(18) {
		t.Errorf("expected 18, got %v", arr[0])
	}
}

func TestJSStringOperationsWork(t *testing.T) {
	data := parseJSON(t, `{"text": "Hello World"}`)
	script := `function transform(data) {
		return {
			upper: data.text.toUpperCase(),
			lower: data.text.toLowerCase(),
			len: data.text.length,
			words: data.text.split(" ")
		};
	}`
	result, err := applyJS(script, data)
	if err != nil {
		t.Fatal(err)
	}
	obj := result.(map[string]any)
	if obj["upper"] != "HELLO WORLD" {
		t.Errorf("upper: %v", obj["upper"])
	}
	if obj["len"] != float64(11) {
		t.Errorf("len: %v", obj["len"])
	}
}

func TestJSMathOperationsWork(t *testing.T) {
	data := parseJSON(t, `[10, 20, 30]`)
	script := `function transform(data) {
		var sum = data.reduce(function(a, b) { return a + b; }, 0);
		return { sum: sum, avg: sum / data.length, max: Math.max.apply(null, data) };
	}`
	result, err := applyJS(script, data)
	if err != nil {
		t.Fatal(err)
	}
	obj := result.(map[string]any)
	if obj["sum"] != float64(60) {
		t.Errorf("sum: %v", obj["sum"])
	}
	if obj["avg"] != float64(20) {
		t.Errorf("avg: %v", obj["avg"])
	}
}
