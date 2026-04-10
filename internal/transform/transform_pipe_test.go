// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package transform

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- parsePipeRun ---

func TestParsePipeRun_ToolActionParams(t *testing.T) {
	tool, action, params, err := parsePipeRun("jq filter --expr=.name --pretty=true")
	if err != nil {
		t.Fatal(err)
	}
	if tool != "jq" {
		t.Errorf("tool: got %q, want %q", tool, "jq")
	}
	if action != "filter" {
		t.Errorf("action: got %q, want %q", action, "filter")
	}
	if params["expr"] != ".name" {
		t.Errorf("params[expr]: got %q, want %q", params["expr"], ".name")
	}
	if params["pretty"] != "true" {
		t.Errorf("params[pretty]: got %q, want %q", params["pretty"], "true")
	}
}

func TestParsePipeRun_ToolOnly(t *testing.T) {
	tool, action, params, err := parsePipeRun("formatter")
	if err != nil {
		t.Fatal(err)
	}
	if tool != "formatter" {
		t.Errorf("tool: got %q, want %q", tool, "formatter")
	}
	if action != "" {
		t.Errorf("action: got %q, want empty", action)
	}
	if len(params) != 0 {
		t.Errorf("params: got %v, want empty", params)
	}
}

func TestParsePipeRun_ToolWithParams(t *testing.T) {
	tool, action, params, err := parsePipeRun("mytool --format=csv")
	if err != nil {
		t.Fatal(err)
	}
	if tool != "mytool" {
		t.Errorf("tool: got %q, want %q", tool, "mytool")
	}
	if action != "" {
		t.Errorf("action should be empty when second arg starts with --, got %q", action)
	}
	if params["format"] != "csv" {
		t.Errorf("params[format]: got %q, want %q", params["format"], "csv")
	}
}

func TestParsePipeRun_Empty(t *testing.T) {
	_, _, _, err := parsePipeRun("")
	if err == nil {
		t.Fatal("expected error for empty run string")
	}
}

func TestParsePipeRun_BooleanFlag(t *testing.T) {
	_, _, params, err := parsePipeRun("tool action --verbose")
	if err != nil {
		t.Fatal(err)
	}
	if params["verbose"] != "true" {
		t.Errorf("boolean flag: got %q, want %q", params["verbose"], "true")
	}
}

// --- parsePipeStep ---

func TestParsePipeStep_Structured(t *testing.T) {
	step := Step{
		PipeTool:   "converter",
		PipeAction: "to-json",
		PipeParams: map[string]string{"indent": "2"},
	}
	tool, action, params, err := parsePipeStep(step)
	if err != nil {
		t.Fatal(err)
	}
	if tool != "converter" {
		t.Errorf("tool: got %q, want %q", tool, "converter")
	}
	if action != "to-json" {
		t.Errorf("action: got %q, want %q", action, "to-json")
	}
	if params["indent"] != "2" {
		t.Errorf("params[indent]: got %q, want %q", params["indent"], "2")
	}
}

func TestParsePipeStep_RunString(t *testing.T) {
	step := Step{
		PipeRun: "jq filter --expr=.data",
	}
	tool, action, _, err := parsePipeStep(step)
	if err != nil {
		t.Fatal(err)
	}
	if tool != "jq" {
		t.Errorf("tool: got %q, want %q", tool, "jq")
	}
	if action != "filter" {
		t.Errorf("action: got %q, want %q", action, "filter")
	}
}

func TestParsePipeStep_Neither(t *testing.T) {
	step := Step{}
	_, _, _, err := parsePipeStep(step)
	if err == nil {
		t.Fatal("expected error when neither tool nor run is set")
	}
}

// --- marshalPipeInput ---

func TestMarshalPipeInput_String(t *testing.T) {
	got, err := marshalPipeInput("hello")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `"hello"` {
		t.Errorf("got %q, want %q", string(got), `"hello"`)
	}
}

func TestMarshalPipeInput_JSONString(t *testing.T) {
	got, err := marshalPipeInput(`{"key": "value"}`)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"key": "value"}` {
		t.Errorf("got %q, want %q", string(got), `{"key": "value"}`)
	}
}

func TestMarshalPipeInput_Map(t *testing.T) {
	got, err := marshalPipeInput(map[string]any{"a": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if parsed["a"] != float64(1) {
		t.Errorf("expected a=1, got %v", parsed["a"])
	}
}

// --- buildPipeArgs ---

func TestBuildPipeArgs_Full(t *testing.T) {
	args := buildPipeArgs("mytool", "convert", map[string]string{"format": "csv"})
	// Should contain: run, mytool, convert, --format=csv, --stdin
	if args[0] != "run" {
		t.Errorf("args[0]: got %q, want %q", args[0], "run")
	}
	if args[1] != "mytool" {
		t.Errorf("args[1]: got %q, want %q", args[1], "mytool")
	}
	if args[2] != "convert" {
		t.Errorf("args[2]: got %q, want %q", args[2], "convert")
	}
	// Last arg should be --stdin
	if args[len(args)-1] != "--stdin" {
		t.Errorf("last arg: got %q, want %q", args[len(args)-1], "--stdin")
	}
	// Check that --format=csv is present somewhere
	found := false
	for _, a := range args {
		if a == "--format=csv" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --format=csv in args: %v", args)
	}
}

func TestBuildPipeArgs_NoAction(t *testing.T) {
	args := buildPipeArgs("mytool", "", nil)
	if len(args) != 3 { // run, mytool, --stdin
		t.Errorf("expected 3 args, got %d: %v", len(args), args)
	}
}

// --- parsePipeOutput ---

func TestParsePipeOutput_JSON(t *testing.T) {
	result := parsePipeOutput([]byte(`{"status": "ok"}`))
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["status"] != "ok" {
		t.Errorf("status: got %v, want %q", m["status"], "ok")
	}
}

func TestParsePipeOutput_JSONArray(t *testing.T) {
	result := parsePipeOutput([]byte(`[1, 2, 3]`))
	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", result)
	}
	if len(arr) != 3 {
		t.Errorf("expected 3 items, got %d", len(arr))
	}
}

func TestParsePipeOutput_RawString(t *testing.T) {
	result := parsePipeOutput([]byte("not json at all"))
	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if s != "not json at all" {
		t.Errorf("got %q, want %q", s, "not json at all")
	}
}

func TestParsePipeOutput_Empty(t *testing.T) {
	result := parsePipeOutput([]byte(""))
	if result != "" {
		t.Errorf("expected empty string, got %v", result)
	}
}

func TestParsePipeOutput_Whitespace(t *testing.T) {
	result := parsePipeOutput([]byte("  \n  "))
	if result != "" {
		t.Errorf("expected empty string for whitespace, got %v", result)
	}
}

// --- applyPipe with mock ---

func TestApplyPipe_MockSubprocess(t *testing.T) {
	// Replace the command function with a mock
	original := pipeCommandFunc
	defer func() { pipeCommandFunc = original }()

	pipeCommandFunc = func(args []string, input []byte) ([]byte, error) {
		// Verify args
		if args[0] != "run" {
			t.Errorf("expected args[0]='run', got %q", args[0])
		}
		if args[1] != "uppercase" {
			t.Errorf("expected args[1]='uppercase', got %q", args[1])
		}

		// Parse the input and return transformed output
		var s string
		if err := json.Unmarshal(input, &s); err != nil {
			// Input might be a JSON object
			return []byte(`{"result": "TRANSFORMED"}`), nil
		}
		return []byte(`"` + strings.ToUpper(s) + `"`), nil
	}

	step := Step{
		Type:     "pipe",
		PipeTool: "uppercase",
	}

	result, err := applyPipe(step, "hello")
	if err != nil {
		t.Fatal(err)
	}

	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T: %v", result, result)
	}
	if s != "HELLO" {
		t.Errorf("got %q, want %q", s, "HELLO")
	}
}

func TestApplyPipe_MockWithJSONData(t *testing.T) {
	original := pipeCommandFunc
	defer func() { pipeCommandFunc = original }()

	pipeCommandFunc = func(args []string, input []byte) ([]byte, error) {
		// Parse input, extract a field, return it
		var data map[string]any
		if err := json.Unmarshal(input, &data); err != nil {
			return nil, err
		}
		items := data["items"]
		out, _ := json.Marshal(items)
		return out, nil
	}

	step := Step{
		Type:       "pipe",
		PipeTool:   "extractor",
		PipeAction: "items",
	}

	input := map[string]any{
		"items": []any{"a", "b", "c"},
		"meta":  "ignore",
	}

	result, err := applyPipe(step, input)
	if err != nil {
		t.Fatal(err)
	}

	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", result)
	}
	if len(arr) != 3 {
		t.Errorf("expected 3 items, got %d", len(arr))
	}
}

func TestApplyPipe_MockRunSyntax(t *testing.T) {
	original := pipeCommandFunc
	defer func() { pipeCommandFunc = original }()

	pipeCommandFunc = func(args []string, input []byte) ([]byte, error) {
		// Return the tool and action as confirmation
		return []byte(`{"tool": "` + args[1] + `", "action": "` + args[2] + `"}`), nil
	}

	step := Step{
		Type:    "pipe",
		PipeRun: "formatter pretty --indent=2",
	}

	result, err := applyPipe(step, "data")
	if err != nil {
		t.Fatal(err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["tool"] != "formatter" {
		t.Errorf("tool: got %v, want %q", m["tool"], "formatter")
	}
	if m["action"] != "pretty" {
		t.Errorf("action: got %v, want %q", m["action"], "pretty")
	}
}

// --- Pipeline integration ---

func TestPipelinePipeType(t *testing.T) {
	original := pipeCommandFunc
	defer func() { pipeCommandFunc = original }()

	pipeCommandFunc = func(args []string, input []byte) ([]byte, error) {
		// Return a count of input array items
		var arr []any
		if err := json.Unmarshal(input, &arr); err != nil {
			return []byte(`0`), nil
		}
		return []byte(json.Number(strings.Repeat("0", len(arr))[:0] + string(rune('0'+len(arr))))), nil
	}

	// Simpler test: just verify the pipe type works in a pipeline
	pipeCommandFunc = func(args []string, input []byte) ([]byte, error) {
		return []byte(`{"piped": true}`), nil
	}

	pipeline := Pipeline{
		{Type: "pipe", PipeTool: "test-tool", PipeAction: "process"},
	}

	result, err := pipeline.Apply(map[string]any{"data": "test"})
	if err != nil {
		t.Fatal(err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["piped"] != true {
		t.Errorf("expected piped=true, got %v", m["piped"])
	}
}
