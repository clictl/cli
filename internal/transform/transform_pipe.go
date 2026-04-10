// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package transform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// applyPipe routes the current data through another clictl tool via subprocess.
// It serializes data to JSON, pipes it to stdin of "clictl run <tool> <action>",
// and parses stdout as the step output.
func applyPipe(step Step, data any) (any, error) {
	tool, action, params, err := parsePipeStep(step)
	if err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}

	// Serialize input data to JSON for stdin
	inputJSON, err := marshalPipeInput(data)
	if err != nil {
		return nil, fmt.Errorf("pipe: serializing input: %w", err)
	}

	// Build the clictl command
	args := buildPipeArgs(tool, action, params)

	// Execute the subprocess
	output, err := executePipeCommand(args, inputJSON)
	if err != nil {
		return nil, fmt.Errorf("pipe: executing %s %s: %w", tool, action, err)
	}

	// Parse output as JSON, fall back to raw string
	return parsePipeOutput(output), nil
}

// PipeStep holds the parsed fields for a pipe transform. Exported for testing.
type PipeStep struct {
	Tool   string
	Action string
	Params map[string]string
}

// parsePipeStep extracts tool, action, and params from a Step.
// Supports two syntaxes:
//   - Structured: step.PipeTool + step.PipeAction + step.PipeParams
//   - String: step.PipeRun = "tool action --key=value ..."
func parsePipeStep(step Step) (tool, action string, params map[string]string, err error) {
	if step.PipeTool != "" {
		tool = step.PipeTool
		action = step.PipeAction
		params = step.PipeParams
		if tool == "" {
			return "", "", nil, fmt.Errorf("pipe step missing tool name")
		}
		return tool, action, params, nil
	}

	if step.PipeRun != "" {
		return parsePipeRun(step.PipeRun)
	}

	return "", "", nil, fmt.Errorf("pipe step has neither tool nor run field")
}

// parsePipeRun parses the shorthand "tool action --key=value" syntax.
func parsePipeRun(run string) (tool, action string, params map[string]string, err error) {
	parts := strings.Fields(run)
	if len(parts) == 0 {
		return "", "", nil, fmt.Errorf("empty run string")
	}

	tool = parts[0]
	if len(parts) > 1 && !strings.HasPrefix(parts[1], "--") {
		action = parts[1]
		parts = parts[2:]
	} else {
		parts = parts[1:]
	}

	params = make(map[string]string)
	for _, p := range parts {
		if !strings.HasPrefix(p, "--") {
			continue
		}
		kv := strings.TrimPrefix(p, "--")
		if idx := strings.Index(kv, "="); idx > 0 {
			params[kv[:idx]] = kv[idx+1:]
		} else {
			params[kv] = "true"
		}
	}

	return tool, action, params, nil
}

// marshalPipeInput converts data to JSON bytes for piping to stdin.
func marshalPipeInput(data any) ([]byte, error) {
	switch v := data.(type) {
	case string:
		// If it is already valid JSON, pass through; otherwise wrap as a JSON string
		if json.Valid([]byte(v)) {
			return []byte(v), nil
		}
		return json.Marshal(v)
	case []byte:
		if json.Valid(v) {
			return v, nil
		}
		return json.Marshal(string(v))
	default:
		return json.Marshal(v)
	}
}

// buildPipeArgs constructs the argument list for "clictl run <tool> <action> [--params]".
func buildPipeArgs(tool, action string, params map[string]string) []string {
	args := []string{"run", tool}
	if action != "" {
		args = append(args, action)
	}
	for k, v := range params {
		args = append(args, fmt.Sprintf("--%s=%s", k, v))
	}
	// Signal that input is coming from stdin
	args = append(args, "--stdin")
	return args
}

// pipeCommandFunc is the function used to execute the pipe subprocess.
// It can be replaced in tests to avoid spawning real processes.
var pipeCommandFunc = defaultPipeCommand

// defaultPipeCommand runs "clictl" with the given args, piping inputJSON to stdin.
func defaultPipeCommand(args []string, inputJSON []byte) ([]byte, error) {
	cmd := exec.Command("clictl", args...)
	cmd.Stdin = bytes.NewReader(inputJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return nil, fmt.Errorf("%w: %s", err, errMsg)
		}
		return nil, err
	}

	return stdout.Bytes(), nil
}

// executePipeCommand runs the pipe subprocess using the pluggable command function.
func executePipeCommand(args []string, inputJSON []byte) ([]byte, error) {
	return pipeCommandFunc(args, inputJSON)
}

// parsePipeOutput tries to parse output as JSON. Falls back to the raw string.
func parsePipeOutput(output []byte) any {
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 {
		return ""
	}

	var parsed any
	if err := json.Unmarshal(trimmed, &parsed); err == nil {
		return parsed
	}

	return string(trimmed)
}
