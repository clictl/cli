// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package transform

import (
	"encoding/base64"
	"strings"
	"testing"
)

// --- Sort ---

func TestSortAsc(t *testing.T) {
	data := parseJSON(t, `[{"name": "charlie", "age": 30}, {"name": "alice", "age": 25}, {"name": "bob", "age": 28}]`)
	result, err := applySort("age", "asc", data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 3 {
		t.Fatalf("expected 3 items, got %d", len(arr))
	}
	first := arr[0].(map[string]any)
	if first["name"] != "alice" {
		t.Errorf("expected alice first, got %v", first["name"])
	}
	last := arr[2].(map[string]any)
	if last["name"] != "charlie" {
		t.Errorf("expected charlie last, got %v", last["name"])
	}
}

func TestSortDesc(t *testing.T) {
	data := parseJSON(t, `[{"name": "a", "stars": 10}, {"name": "b", "stars": 50}, {"name": "c", "stars": 30}]`)
	result, err := applySort("stars", "desc", data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	first := arr[0].(map[string]any)
	if first["name"] != "b" {
		t.Errorf("expected b first (50 stars), got %v", first["name"])
	}
}

func TestSortByString(t *testing.T) {
	data := parseJSON(t, `[{"name": "charlie"}, {"name": "alice"}, {"name": "bob"}]`)
	result, err := applySort("name", "asc", data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	first := arr[0].(map[string]any)
	if first["name"] != "alice" {
		t.Errorf("expected alice first, got %v", first["name"])
	}
}

func TestSortDefaultOrder(t *testing.T) {
	data := parseJSON(t, `[{"v": 3}, {"v": 1}, {"v": 2}]`)
	result, err := applySort("v", "", data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	first := arr[0].(map[string]any)
	if first["v"] != float64(1) {
		t.Errorf("default order should be asc, got %v first", first["v"])
	}
}

// --- Filter ---

func TestFilterGreaterThan(t *testing.T) {
	data := parseJSON(t, `[{"name": "a", "stars": 50}, {"name": "b", "stars": 150}, {"name": "c", "stars": 200}]`)
	result, err := applyFilter(".stars > 100", data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 2 {
		t.Fatalf("expected 2 items, got %d", len(arr))
	}
}

func TestFilterEquals(t *testing.T) {
	data := parseJSON(t, `[{"status": "active"}, {"status": "done"}, {"status": "active"}]`)
	result, err := applyFilter(`.status == "active"`, data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 2 {
		t.Fatalf("expected 2, got %d", len(arr))
	}
}

func TestFilterNotEquals(t *testing.T) {
	data := parseJSON(t, `[{"status": "active"}, {"status": "done"}, {"status": "active"}]`)
	result, err := applyFilter(`.status != "active"`, data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 1 {
		t.Fatalf("expected 1, got %d", len(arr))
	}
}

func TestFilterLessThan(t *testing.T) {
	data := parseJSON(t, `[{"v": 1}, {"v": 5}, {"v": 10}]`)
	result, err := applyFilter(".v < 5", data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 1 {
		t.Fatalf("expected 1, got %d", len(arr))
	}
}

func TestFilterEmptyResult(t *testing.T) {
	data := parseJSON(t, `[{"v": 1}, {"v": 2}]`)
	result, err := applyFilter(".v > 100", data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 0 {
		t.Fatalf("expected 0, got %d", len(arr))
	}
}

func TestFilterInvalidExpr(t *testing.T) {
	data := parseJSON(t, `[{"v": 1}]`)
	_, err := applyFilter("bad expression", data)
	if err == nil {
		t.Fatal("expected error for invalid expression")
	}
}

// --- Unique ---

func TestUnique(t *testing.T) {
	data := parseJSON(t, `[{"lang": "go"}, {"lang": "python"}, {"lang": "go"}, {"lang": "rust"}, {"lang": "python"}]`)
	result, err := applyUnique("lang", data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 3 {
		t.Fatalf("expected 3 unique, got %d", len(arr))
	}
	// First occurrence kept
	first := arr[0].(map[string]any)
	if first["lang"] != "go" {
		t.Errorf("expected go first, got %v", first["lang"])
	}
}

func TestUniqueEmpty(t *testing.T) {
	data := parseJSON(t, `[]`)
	result, err := applyUnique("x", data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 0 {
		t.Fatalf("expected 0, got %d", len(arr))
	}
}

// --- Group ---

func TestGroup(t *testing.T) {
	data := parseJSON(t, `[
		{"name": "a", "type": "fruit"},
		{"name": "b", "type": "veg"},
		{"name": "c", "type": "fruit"},
		{"name": "d", "type": "veg"}
	]`)
	result, err := applyGroup("type", data)
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	fruits := m["fruit"].([]any)
	vegs := m["veg"].([]any)
	if len(fruits) != 2 {
		t.Errorf("expected 2 fruits, got %d", len(fruits))
	}
	if len(vegs) != 2 {
		t.Errorf("expected 2 vegs, got %d", len(vegs))
	}
}

// --- Count ---

func TestCount(t *testing.T) {
	data := parseJSON(t, `[1, 2, 3, 4, 5]`)
	result, err := applyCount(data)
	if err != nil {
		t.Fatal(err)
	}
	if result != float64(5) {
		t.Errorf("expected 5, got %v", result)
	}
}

func TestCountEmpty(t *testing.T) {
	data := parseJSON(t, `[]`)
	result, err := applyCount(data)
	if err != nil {
		t.Fatal(err)
	}
	if result != float64(0) {
		t.Errorf("expected 0, got %v", result)
	}
}

func TestCountNonArray(t *testing.T) {
	result, err := applyCount("not an array")
	if err != nil {
		t.Fatal(err)
	}
	if result != "not an array" {
		t.Error("non-array should pass through")
	}
}

// --- Join ---

func TestJoinDefault(t *testing.T) {
	data := parseJSON(t, `["a", "b", "c"]`)
	result, err := applyJoin("", data)
	if err != nil {
		t.Fatal(err)
	}
	if result != "a, b, c" {
		t.Errorf("expected 'a, b, c', got %q", result)
	}
}

func TestJoinCustomSeparator(t *testing.T) {
	data := parseJSON(t, `["x", "y", "z"]`)
	result, err := applyJoin(" | ", data)
	if err != nil {
		t.Fatal(err)
	}
	if result != "x | y | z" {
		t.Errorf("expected 'x | y | z', got %q", result)
	}
}

func TestJoinNumbers(t *testing.T) {
	data := parseJSON(t, `[1, 2, 3]`)
	result, err := applyJoin("-", data)
	if err != nil {
		t.Fatal(err)
	}
	if result != "1-2-3" {
		t.Errorf("expected '1-2-3', got %q", result)
	}
}

// --- Split ---

func TestSplit(t *testing.T) {
	result, err := applySplit(",", "a,b,c")
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 3 {
		t.Fatalf("expected 3, got %d", len(arr))
	}
	if arr[0] != "a" || arr[1] != "b" || arr[2] != "c" {
		t.Errorf("unexpected split result: %v", arr)
	}
}

func TestSplitNewline(t *testing.T) {
	result, err := applySplit("\n", "line1\nline2\nline3")
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 3 {
		t.Fatalf("expected 3, got %d", len(arr))
	}
}

// --- Flatten ---

func TestFlatten(t *testing.T) {
	data := parseJSON(t, `[[1, 2], [3, 4], [5]]`)
	result, err := applyFlatten(data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 5 {
		t.Fatalf("expected 5, got %d", len(arr))
	}
}

func TestFlattenMixed(t *testing.T) {
	data := parseJSON(t, `[[1, 2], 3, [4, 5]]`)
	result, err := applyFlatten(data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 5 {
		t.Fatalf("expected 5, got %d", len(arr))
	}
}

func TestFlattenEmpty(t *testing.T) {
	data := parseJSON(t, `[]`)
	result, err := applyFlatten(data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 0 {
		t.Fatalf("expected 0, got %d", len(arr))
	}
}

// --- Unwrap ---

func TestUnwrapSingle(t *testing.T) {
	data := parseJSON(t, `[{"name": "only"}]`)
	result, err := applyUnwrap(data)
	if err != nil {
		t.Fatal(err)
	}
	obj := result.(map[string]any)
	if obj["name"] != "only" {
		t.Errorf("expected unwrapped object, got %v", result)
	}
}

func TestUnwrapMultiple(t *testing.T) {
	data := parseJSON(t, `[1, 2]`)
	result, err := applyUnwrap(data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 2 {
		t.Error("multi-item array should not be unwrapped")
	}
}

func TestUnwrapEmpty(t *testing.T) {
	data := parseJSON(t, `[]`)
	result, err := applyUnwrap(data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 0 {
		t.Error("empty array should not be unwrapped")
	}
}

// --- Format ---

func TestFormatArray(t *testing.T) {
	data := parseJSON(t, `[{"name": "alice", "role": "admin"}, {"name": "bob", "role": "member"}]`)
	result, err := applyFormat("{name} ({role})", data)
	if err != nil {
		t.Fatal(err)
	}
	s := result.(string)
	if !strings.Contains(s, "alice (admin)") {
		t.Errorf("expected 'alice (admin)', got %q", s)
	}
	if !strings.Contains(s, "bob (member)") {
		t.Errorf("expected 'bob (member)', got %q", s)
	}
}

func TestFormatSingleObject(t *testing.T) {
	data := parseJSON(t, `{"city": "NYC", "temp": 72}`)
	result, err := applyFormat("Weather in {city}: {temp}F", data)
	if err != nil {
		t.Fatal(err)
	}
	s := result.(string)
	if s != "Weather in NYC: 72F" {
		t.Errorf("expected 'Weather in NYC: 72F', got %q", s)
	}
}

// --- Prompt ---

func TestPrompt(t *testing.T) {
	result, err := applyPrompt("Use this data carefully.", "some data")
	if err != nil {
		t.Fatal(err)
	}
	s := result.(string)
	if !strings.Contains(s, "some data") {
		t.Error("prompt should contain original data")
	}
	if !strings.Contains(s, "Use this data carefully.") {
		t.Error("prompt should contain guidance text")
	}
}

func TestPromptEmpty(t *testing.T) {
	result, err := applyPrompt("", "data")
	if err != nil {
		t.Fatal(err)
	}
	if result != "data" {
		t.Error("empty prompt should pass through")
	}
}

func TestPromptWithJSON(t *testing.T) {
	data := parseJSON(t, `{"key": "val"}`)
	result, err := applyPrompt("Note: check the key field.", data)
	if err != nil {
		t.Fatal(err)
	}
	s := result.(string)
	if !strings.Contains(s, "Note: check the key field.") {
		t.Error("should contain prompt text")
	}
}

// --- DateFormat ---

func TestDateFormat(t *testing.T) {
	data := parseJSON(t, `{"created_at": "2024-01-15T10:30:00Z", "name": "test"}`)
	result, err := applyDateFormat("created_at", "2006-01-02T15:04:05Z", "Jan 2, 2006", data)
	if err != nil {
		t.Fatal(err)
	}
	obj := result.(map[string]any)
	if obj["created_at"] != "Jan 15, 2024" {
		t.Errorf("expected 'Jan 15, 2024', got %v", obj["created_at"])
	}
	if obj["name"] != "test" {
		t.Error("other fields should be preserved")
	}
}

func TestDateFormatArray(t *testing.T) {
	data := parseJSON(t, `[
		{"date": "2024-01-15", "name": "a"},
		{"date": "2024-06-20", "name": "b"}
	]`)
	result, err := applyDateFormat("date", "2006-01-02", "01/02/2006", data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	first := arr[0].(map[string]any)
	if first["date"] != "01/15/2024" {
		t.Errorf("expected '01/15/2024', got %v", first["date"])
	}
}

func TestDateFormatMissingField(t *testing.T) {
	data := parseJSON(t, `{"name": "test"}`)
	result, err := applyDateFormat("created_at", "2006-01-02", "Jan 2, 2006", data)
	if err != nil {
		t.Fatal(err)
	}
	obj := result.(map[string]any)
	if obj["name"] != "test" {
		t.Error("should pass through unchanged")
	}
}

// --- XMLToJSON ---

func TestXMLToJSON(t *testing.T) {
	xml := `<root><name>test</name><value>42</value></root>`
	result, err := applyXMLToJSON(xml)
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	root, ok := m["root"].(map[string]any)
	if !ok {
		t.Fatalf("expected root object, got %T: %v", m["root"], m)
	}
	if root["name"] != "test" {
		t.Errorf("expected name 'test', got %v", root["name"])
	}
	if root["value"] != "42" {
		t.Errorf("expected value '42', got %v", root["value"])
	}
}

func TestXMLToJSONWithAttributes(t *testing.T) {
	xml := `<item id="123" type="book"><title>Go Programming</title></item>`
	result, err := applyXMLToJSON(xml)
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	item := m["item"].(map[string]any)
	if item["@id"] != "123" {
		t.Errorf("expected @id '123', got %v", item["@id"])
	}
	if item["title"] != "Go Programming" {
		t.Errorf("expected title, got %v", item["title"])
	}
}

func TestXMLToJSONRepeatedElements(t *testing.T) {
	xml := `<list><item>a</item><item>b</item><item>c</item></list>`
	result, err := applyXMLToJSON(xml)
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	list := m["list"].(map[string]any)
	items := list["item"].([]any)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
}

// --- CSVToJSON ---

func TestCSVToJSONWithHeaders(t *testing.T) {
	csvData := "name,age,city\nalice,30,NYC\nbob,25,LA"
	result, err := applyCSVToJSON(true, csvData)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(arr))
	}
	first := arr[0].(map[string]any)
	if first["name"] != "alice" {
		t.Errorf("expected 'alice', got %v", first["name"])
	}
	if first["age"] != "30" {
		t.Errorf("expected '30', got %v", first["age"])
	}
}

func TestCSVToJSONWithoutHeaders(t *testing.T) {
	csvData := "a,b,c\nd,e,f"
	result, err := applyCSVToJSON(false, csvData)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(arr))
	}
	firstRow := arr[0].([]any)
	if firstRow[0] != "a" {
		t.Errorf("expected 'a', got %v", firstRow[0])
	}
}

func TestCSVToJSONEmpty(t *testing.T) {
	result, err := applyCSVToJSON(true, "")
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(arr))
	}
}

// --- Base64Decode ---

func TestBase64Decode(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("hello world"))
	result, err := applyBase64Decode("", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %v", result)
	}
}

func TestBase64DecodeField(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("secret"))
	data := map[string]any{"content": encoded, "name": "test"}
	result, err := applyBase64Decode("content", data)
	if err != nil {
		t.Fatal(err)
	}
	obj := result.(map[string]any)
	if obj["content"] != "secret" {
		t.Errorf("expected 'secret', got %v", obj["content"])
	}
	if obj["name"] != "test" {
		t.Error("other fields should be preserved")
	}
}

func TestBase64DecodeArray(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("decoded"))
	data := []any{
		map[string]any{"data": encoded},
	}
	result, err := applyBase64Decode("data", data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	obj := arr[0].(map[string]any)
	if obj["data"] != "decoded" {
		t.Errorf("expected 'decoded', got %v", obj["data"])
	}
}

// --- MarkdownToText ---

func TestMarkdownToText(t *testing.T) {
	md := "# Hello World\n\nThis is **bold** and *italic*.\n\n- item 1\n- item 2\n\n[link](http://example.com)"
	result, err := applyMarkdownToText(md)
	if err != nil {
		t.Fatal(err)
	}
	s := result.(string)
	if strings.Contains(s, "#") {
		t.Errorf("should not contain # headers: %q", s)
	}
	if strings.Contains(s, "**") {
		t.Errorf("should not contain ** bold: %q", s)
	}
	if strings.Contains(s, "*italic*") {
		t.Errorf("should not contain *italic*: %q", s)
	}
	if strings.Contains(s, "](") {
		t.Errorf("should not contain markdown links: %q", s)
	}
	if !strings.Contains(s, "Hello World") {
		t.Errorf("should contain header text: %q", s)
	}
	if !strings.Contains(s, "bold") {
		t.Errorf("should contain bold text: %q", s)
	}
	if !strings.Contains(s, "link") {
		t.Errorf("should contain link text: %q", s)
	}
}

func TestMarkdownToTextCode(t *testing.T) {
	md := "Use `fmt.Println` to print.\n\n```go\nfmt.Println(\"hello\")\n```"
	result, err := applyMarkdownToText(md)
	if err != nil {
		t.Fatal(err)
	}
	s := result.(string)
	if strings.Contains(s, "`") {
		t.Errorf("should not contain backticks: %q", s)
	}
	if !strings.Contains(s, "fmt.Println") {
		t.Errorf("should contain code text: %q", s)
	}
}

// --- Typed Pipeline Integration ---

func TestTypedPipelineSortFilter(t *testing.T) {
	data := parseJSON(t, `[
		{"name": "a", "stars": 50},
		{"name": "b", "stars": 200},
		{"name": "c", "stars": 150},
		{"name": "d", "stars": 10}
	]`)

	pipeline := Pipeline{
		{Type: "filter", Filter: ".stars > 100"},
		{Type: "sort", Field: "stars", Order: "desc"},
	}

	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}
	arr := result.([]any)
	if len(arr) != 2 {
		t.Fatalf("expected 2, got %d", len(arr))
	}
	first := arr[0].(map[string]any)
	if first["name"] != "b" {
		t.Errorf("expected b (200 stars) first, got %v", first["name"])
	}
}

func TestTypedPipelineGroupCount(t *testing.T) {
	data := parseJSON(t, `[
		{"type": "bug"}, {"type": "feature"}, {"type": "bug"}, {"type": "bug"}
	]`)

	// Group then check structure
	pipeline := Pipeline{
		{Type: "group", Field: "type"},
	}
	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	bugs := m["bug"].([]any)
	if len(bugs) != 3 {
		t.Errorf("expected 3 bugs, got %d", len(bugs))
	}
}

func TestTypedPipelineJoinSplit(t *testing.T) {
	// Split then join with different separator
	pipeline := Pipeline{
		{Type: "split", Separator: ","},
		{Type: "join", Separator: " | "},
	}
	result, err := pipeline.Apply("a,b,c")
	if err != nil {
		t.Fatal(err)
	}
	if result != "a | b | c" {
		t.Errorf("expected 'a | b | c', got %v", result)
	}
}

func TestTypedPipelineFlattenCount(t *testing.T) {
	data := parseJSON(t, `[[1, 2], [3], [4, 5, 6]]`)
	pipeline := Pipeline{
		{Type: "flatten"},
		{Type: "count"},
	}
	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}
	if result != float64(6) {
		t.Errorf("expected 6, got %v", result)
	}
}

func TestTypedPipelineUnwrap(t *testing.T) {
	data := parseJSON(t, `[{"id": 42}]`)
	pipeline := Pipeline{
		{Type: "unwrap"},
	}
	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}
	obj := result.(map[string]any)
	if obj["id"] != float64(42) {
		t.Errorf("expected id 42, got %v", obj["id"])
	}
}

func TestTypedPipelineFormatPrompt(t *testing.T) {
	data := parseJSON(t, `[{"name": "go", "stars": 120000}]`)
	pipeline := Pipeline{
		{Type: "format", Template: "{name}: {stars} stars"},
		{Type: "prompt", Value: "These are popular repos."},
	}
	result, err := pipeline.Apply(data)
	if err != nil {
		t.Fatal(err)
	}
	s := result.(string)
	if !strings.Contains(s, "go: 120,000 stars") {
		t.Errorf("format output missing: %q", s)
	}
	if !strings.Contains(s, "These are popular repos.") {
		t.Errorf("prompt missing: %q", s)
	}
}

// --- ParseSteps with typed format ---

func TestParseStepsTypedFormat(t *testing.T) {
	raw := []any{
		map[string]any{"type": "filter", "filter": ".stars > 100"},
		map[string]any{"type": "sort", "field": "stars", "order": "desc"},
		map[string]any{"type": "count"},
	}
	pipeline, err := ParseSteps(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pipeline) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(pipeline))
	}
	if pipeline[0].Type != "filter" {
		t.Error("first step should be filter")
	}
	if pipeline[0].Filter != ".stars > 100" {
		t.Error("filter expression not parsed")
	}
	if pipeline[1].Type != "sort" {
		t.Error("second step should be sort")
	}
	if pipeline[1].Field != "stars" {
		t.Error("field not parsed")
	}
	if pipeline[2].Type != "count" {
		t.Error("third step should be count")
	}
}

func TestParseStepsMixedFormat(t *testing.T) {
	// Mix old format and new typed format
	raw := []any{
		map[string]any{"extract": "$.data.items"},
		map[string]any{"type": "sort", "field": "name", "order": "asc"},
		map[string]any{"select": []any{"name", "status"}},
	}
	pipeline, err := ParseSteps(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pipeline) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(pipeline))
	}
	if pipeline[0].Extract != "$.data.items" {
		t.Error("old format extract should work")
	}
	if pipeline[1].Type != "sort" {
		t.Error("typed sort should work")
	}
	if len(pipeline[2].Select) != 2 {
		t.Error("old format select should work")
	}
}

func TestUnknownTypeError(t *testing.T) {
	pipeline := Pipeline{
		{Type: "nonexistent"},
	}
	_, err := pipeline.Apply("data")
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	if !strings.Contains(err.Error(), "unknown transform type") {
		t.Errorf("unexpected error: %v", err)
	}
}
