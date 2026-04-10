// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package transform

import (
	"context"
	"strings"
	"testing"
)

func TestNewDAGExecutor_LinearStepsReturnsNil(t *testing.T) {
	steps := []Step{
		{Type: "json", Extract: "$.data"},
		{Type: "truncate", Truncate: &TruncateConfig{MaxItems: 5}},
	}
	dag, err := NewDAGExecutor(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dag != nil {
		t.Fatal("expected nil executor for linear steps")
	}
}

func TestNewDAGExecutor_DuplicateIDError(t *testing.T) {
	steps := []Step{
		{ID: "a", Type: "json", Extract: "$.x"},
		{ID: "a", Type: "json", Extract: "$.y"},
	}
	_, err := NewDAGExecutor(steps)
	if err == nil {
		t.Fatal("expected error for duplicate IDs")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got: %v", err)
	}
}

func TestNewDAGExecutor_UnknownInputError(t *testing.T) {
	steps := []Step{
		{ID: "a", Type: "json", Extract: "$.x", Input: "nonexistent"},
	}
	_, err := NewDAGExecutor(steps)
	if err == nil {
		t.Fatal("expected error for unknown input reference")
	}
	if !strings.Contains(err.Error(), "unknown input") {
		t.Fatalf("expected unknown input error, got: %v", err)
	}
}

func TestDAGExecutor_CycleDetection(t *testing.T) {
	steps := []Step{
		{ID: "a", Type: "json", Extract: "$.x", DependsOn: []string{"c"}},
		{ID: "b", Type: "json", Extract: "$.y", Input: "a"},
		{ID: "c", Type: "json", Extract: "$.z", Input: "b"},
	}
	_, err := NewDAGExecutor(steps)
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got: %v", err)
	}
}

func TestDAGExecutor_TwoParallelBranchesMerge(t *testing.T) {
	// Two branches extract different fields, then merge them.
	//
	//   _root -> extractNames (select name)
	//   _root -> extractAges  (select age)
	//   extractNames + extractAges -> merged (merge object)
	steps := []Step{
		{
			ID:   "extractNames",
			Type: "json",
			Select: []string{"name"},
		},
		{
			ID:   "extractAges",
			Type: "json",
			Select: []string{"age"},
		},
		{
			ID:       "merged",
			Type:     "merge",
			Sources:  []string{"extractNames", "extractAges"},
			Strategy: "object",
		},
	}

	dag, err := NewDAGExecutor(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dag == nil {
		t.Fatal("expected non-nil DAG executor")
	}

	input := map[string]any{
		"name": "Alice",
		"age":  30.0,
		"city": "NYC",
	}
	result, err := dag.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	obj, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T: %v", result, result)
	}

	// extractNames should have produced {"name": "Alice"}
	names, ok := obj["extractNames"]
	if !ok {
		t.Fatal("missing extractNames in merged result")
	}
	namesMap, ok := names.(map[string]any)
	if !ok {
		t.Fatalf("extractNames not a map: %T", names)
	}
	if namesMap["name"] != "Alice" {
		t.Errorf("expected name=Alice, got %v", namesMap["name"])
	}

	// extractAges should have produced {"age": 30}
	ages, ok := obj["extractAges"]
	if !ok {
		t.Fatal("missing extractAges in merged result")
	}
	agesMap, ok := ages.(map[string]any)
	if !ok {
		t.Fatalf("extractAges not a map: %T", ages)
	}
	if agesMap["age"] != 30.0 {
		t.Errorf("expected age=30, got %v", agesMap["age"])
	}
}

func TestDAGExecutor_EachFanOut(t *testing.T) {
	steps := []Step{
		{
			ID:          "fanout",
			Type:        "json",
			Each:        true,
			Concurrency: 2,
			Select:      []string{"x"},
		},
	}

	dag, err := NewDAGExecutor(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	input := []any{
		map[string]any{"x": 1, "y": 2},
		map[string]any{"x": 3, "y": 4},
		map[string]any{"x": 5, "y": 6},
	}
	result, err := dag.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected array result, got %T", result)
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 results, got %d", len(arr))
	}
	for i, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			t.Errorf("result[%d]: expected map, got %T", i, item)
			continue
		}
		if _, ok := m["y"]; ok {
			t.Errorf("result[%d]: field y should have been filtered out", i)
		}
	}
}

func TestDAGExecutor_EachNonArrayError(t *testing.T) {
	steps := []Step{
		{
			ID:   "fanout",
			Type: "json",
			Each: true,
		},
	}

	dag, err := NewDAGExecutor(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = dag.Execute(context.Background(), "not an array")
	if err == nil {
		t.Fatal("expected error for non-array Each input")
	}
	if !strings.Contains(err.Error(), "must be an array") {
		t.Fatalf("expected array error, got: %v", err)
	}
}

func TestDAGExecutor_WhenConditionalSkip(t *testing.T) {
	steps := []Step{
		{
			ID:   "guarded",
			Type: "json",
			When: "size(data) > 5",
			Select: []string{"name"},
		},
	}

	dag, err := NewDAGExecutor(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Input is a small array - should be skipped
	input := []any{
		map[string]any{"name": "Alice"},
	}
	result, err := dag.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil (skipped), got %v", result)
	}
}

func TestDAGExecutor_WhenConditionalRun(t *testing.T) {
	steps := []Step{
		{
			ID:   "guarded",
			Type: "json",
			When: "size(data) > 0",
			Select: []string{"name"},
		},
	}

	dag, err := NewDAGExecutor(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	input := []any{
		map[string]any{"name": "Alice", "age": 30},
	}
	result, err := dag.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", result)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 result, got %d", len(arr))
	}
}

func TestDAGExecutor_WhenFieldCondition(t *testing.T) {
	steps := []Step{
		{
			ID:   "check",
			Type: "json",
			When: "data.status == 'active'",
			Select: []string{"name"},
		},
	}

	dag, err := NewDAGExecutor(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should run
	input := map[string]any{"name": "Alice", "status": "active"}
	result, err := dag.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}

	// Should skip
	input2 := map[string]any{"name": "Bob", "status": "inactive"}
	result2, err := dag.Execute(context.Background(), input2)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result2 != nil {
		t.Fatalf("expected nil (skipped), got %v", result2)
	}
}

func TestDAGExecutor_MergeConcat(t *testing.T) {
	steps := []Step{
		{ID: "a", Type: "json", Extract: "$.fruits"},
		{ID: "b", Type: "json", Extract: "$.vegetables"},
		{ID: "all", Type: "merge", Sources: []string{"a", "b"}, Strategy: "concat"},
	}

	dag, err := NewDAGExecutor(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	input := map[string]any{
		"fruits":     []any{"apple", "banana"},
		"vegetables": []any{"carrot", "pea"},
	}
	result, err := dag.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", result)
	}
	if len(arr) != 4 {
		t.Fatalf("expected 4 items, got %d: %v", len(arr), arr)
	}
}

func TestDAGExecutor_MergeZip(t *testing.T) {
	steps := []Step{
		{ID: "names", Type: "json", Extract: "$.names"},
		{ID: "scores", Type: "json", Extract: "$.scores"},
		{ID: "zipped", Type: "merge", Sources: []string{"names", "scores"}, Strategy: "zip"},
	}

	dag, err := NewDAGExecutor(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	input := map[string]any{
		"names":  []any{"Alice", "Bob"},
		"scores": []any{95.0, 87.0},
	}
	result, err := dag.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", result)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(arr))
	}

	pair0, ok := arr[0].([]any)
	if !ok || len(pair0) != 2 {
		t.Fatalf("expected pair at index 0, got %v", arr[0])
	}
	if pair0[0] != "Alice" || pair0[1] != 95.0 {
		t.Errorf("unexpected pair[0]: %v", pair0)
	}
}

func TestDAGExecutor_MergeFirst(t *testing.T) {
	steps := []Step{
		{ID: "a", Type: "json", When: "size(data) == 0", Select: []string{"x"}},
		{ID: "b", Type: "json", Select: []string{"name"}},
		{ID: "pick", Type: "merge", Sources: []string{"a", "b"}, Strategy: "first"},
	}

	dag, err := NewDAGExecutor(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	input := map[string]any{"name": "Alice", "age": 30.0}
	result, err := dag.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// Step "a" should be skipped (When: size(data) == 0 fails for a map with 2 keys).
	// Step "b" produces {"name": "Alice"}.
	// mergeFirst should return the first non-nil, which is b's output.
	obj, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T: %v", result, result)
	}
	if obj["name"] != "Alice" {
		t.Errorf("expected name=Alice, got %v", obj["name"])
	}
}

func TestDAGExecutor_InputChaining(t *testing.T) {
	// a extracts $.items, b selects name from a's output
	steps := []Step{
		{ID: "a", Type: "json", Extract: "$.items"},
		{ID: "b", Type: "json", Input: "a", Select: []string{"name"}},
	}

	dag, err := NewDAGExecutor(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	input := map[string]any{
		"items": []any{
			map[string]any{"name": "Alice", "age": 30.0},
			map[string]any{"name": "Bob", "age": 25.0},
		},
	}
	result, err := dag.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", result)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 items, got %d", len(arr))
	}
	item0, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map at index 0, got %T", arr[0])
	}
	if item0["name"] != "Alice" {
		t.Errorf("expected Alice, got %v", item0["name"])
	}
	if _, hasAge := item0["age"]; hasAge {
		t.Error("age should have been filtered out by select")
	}
}

func TestDAGExecutor_Mermaid(t *testing.T) {
	steps := []Step{
		{ID: "fetch", Type: "json", Extract: "$.data"},
		{ID: "filterA", Type: "filter", Input: "fetch", Filter: ".status == \"active\""},
		{ID: "filterB", Type: "filter", Input: "fetch", Filter: ".status == \"inactive\""},
		{ID: "combine", Type: "merge", Sources: []string{"filterA", "filterB"}, Strategy: "concat"},
	}

	dag, err := NewDAGExecutor(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mermaid := dag.Mermaid()

	if !strings.Contains(mermaid, "graph TD") {
		t.Error("missing graph TD header")
	}
	if !strings.Contains(mermaid, "fetch[json: fetch]") {
		t.Error("missing fetch node")
	}
	if !strings.Contains(mermaid, "fetch --> filterA") {
		t.Error("missing fetch -> filterA edge")
	}
	if !strings.Contains(mermaid, "fetch --> filterB") {
		t.Error("missing fetch -> filterB edge")
	}
	if !strings.Contains(mermaid, "filterA --> combine") {
		t.Error("missing filterA -> combine edge")
	}
	if !strings.Contains(mermaid, "filterB --> combine") {
		t.Error("missing filterB -> combine edge")
	}
}

func TestEvaluateWhen_SizeConditions(t *testing.T) {
	tests := []struct {
		when   string
		data   any
		expect bool
	}{
		{"size(data) == 0", []any{}, true},
		{"size(data) == 0", []any{1}, false},
		{"size(data) > 2", []any{1, 2, 3}, true},
		{"size(data) > 2", []any{1}, false},
		{"size(data) < 5", []any{1, 2}, true},
		{"size(data) >= 3", []any{1, 2, 3}, true},
		{"size(data) <= 1", []any{1}, true},
		{"size(data) != 0", []any{1}, true},
		{"size(data) != 0", []any{}, false},
		{"size(data) == 0", nil, true},
	}

	for _, tt := range tests {
		result := evaluateWhen(tt.when, tt.data)
		if result != tt.expect {
			t.Errorf("evaluateWhen(%q, %v) = %v, want %v", tt.when, tt.data, result, tt.expect)
		}
	}
}

func TestEvaluateWhen_FieldConditions(t *testing.T) {
	data := map[string]any{
		"status": "active",
		"nested": map[string]any{
			"level": "high",
		},
	}

	tests := []struct {
		when   string
		expect bool
	}{
		{"data.status == 'active'", true},
		{"data.status == 'inactive'", false},
		{"data.status != 'inactive'", true},
		{"data.nested.level == 'high'", true},
		{"data.nested.level == 'low'", false},
	}

	for _, tt := range tests {
		result := evaluateWhen(tt.when, data)
		if result != tt.expect {
			t.Errorf("evaluateWhen(%q) = %v, want %v", tt.when, result, tt.expect)
		}
	}
}

func TestPipelineApply_DAGDelegation(t *testing.T) {
	// Verify Pipeline.Apply delegates to DAG when steps have IDs
	pipeline := Pipeline{
		{ID: "a", Type: "json", Extract: "$.items"},
		{ID: "b", Type: "json", Input: "a", Select: []string{"name"}},
	}

	input := map[string]any{
		"items": []any{
			map[string]any{"name": "Test", "extra": true},
		},
	}

	result, err := pipeline.Apply(input)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", result)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 item, got %d", len(arr))
	}
	m := arr[0].(map[string]any)
	if m["name"] != "Test" {
		t.Errorf("expected name=Test, got %v", m["name"])
	}
	if _, has := m["extra"]; has {
		t.Error("extra field should have been filtered")
	}
}

func TestPipelineApply_LinearUnchanged(t *testing.T) {
	// Verify linear pipelines still work as before
	pipeline := Pipeline{
		{Type: "json", Extract: "$.name"},
	}

	input := map[string]any{"name": "Alice"}
	result, err := pipeline.Apply(input)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if result != "Alice" {
		t.Errorf("expected Alice, got %v", result)
	}
}
