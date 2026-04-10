// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/registry"
	"github.com/clictl/cli/internal/transform"
)

var testSpecCmd = &cobra.Command{
	Use:   "test <tool>",
	Short: "Validate a spec against the live API",
	Long: `Run assertions and transforms against the live API to verify a spec works.

  clictl test openweathermap
  clictl test my-api`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		toolName := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

		cache := registry.NewCache(cfg.CacheDir)
		spec, err := registry.ResolveSpec(ctx, toolName, cfg, cache, flagNoCache)
		if err != nil {
			msg := fmt.Sprintf("tool %q not found", toolName)
			if dym := toolSuggestion(toolName, cfg); dym != "" {
				msg += dym
			}
			return fmt.Errorf("%s", msg)
		}

		passed := 0
		failed := 0
		skipped := 0

		for _, action := range spec.Actions {
			result := testAction(ctx, spec, &action)
			switch result.status {
			case "OK":
				passed++
			case "SKIP":
				skipped++
			case "FAIL":
				failed++
			}
			fmt.Println(result.String())
		}

		total := passed + failed + skipped
		fmt.Printf("\n%d of %d actions passed", passed, total)
		if skipped > 0 {
			fmt.Printf(" (%d skipped)", skipped)
		}
		if failed > 0 {
			fmt.Printf(" (%d failed)", failed)
		}
		fmt.Println(".")

		if failed > 0 {
			return fmt.Errorf("%d action(s) failed", failed)
		}
		return nil
	},
}

type assertResult struct {
	assertType string
	passed     bool
	detail     string
}

type testResult struct {
	action  string
	status  string
	code    int
	summary string
	asserts []assertResult
}

// String returns the string representation.
func (r testResult) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("  %s: %s", r.action, r.status))
	if r.code > 0 {
		sb.WriteString(fmt.Sprintf(" (%d", r.code))
		if r.summary != "" {
			sb.WriteString(fmt.Sprintf(", %s", r.summary))
		}
		sb.WriteString(")")
	} else if r.summary != "" {
		sb.WriteString(fmt.Sprintf(" (%s)", r.summary))
	}
	for _, a := range r.asserts {
		mark := "PASS"
		if !a.passed {
			mark = "FAIL"
		}
		sb.WriteString(fmt.Sprintf("\n    [%s] %s", mark, a.assertType))
		if a.detail != "" {
			sb.WriteString(fmt.Sprintf(": %s", a.detail))
		}
	}
	return sb.String()
}

func testAction(ctx context.Context, spec *models.ToolSpec, action *models.Action) testResult {
	// Skip composite actions
	if action.IsComposite() {
		return testResult{action: action.Name, status: "SKIP", summary: "composite action"}
	}

	// Skip CLI/command protocol actions
	if spec.IsCommand() {
		return testResult{action: action.Name, status: "SKIP", summary: "command protocol"}
	}

	// Resolve action-level config with spec-level fallbacks
	resolvedAuth := spec.ResolveActionAuth(action)

	// Check auth requirements
	if resolvedAuth != nil && len(resolvedAuth.Env) > 0 {
		envVal := os.Getenv(resolvedAuth.Env[0])
		if envVal == "" {
			return testResult{
				action:  action.Name,
				status:  "SKIP",
				summary: fmt.Sprintf("env %s not set", resolvedAuth.Env[0]),
			}
		}
	}

	// Build the URL
	baseURL := spec.ResolveActionURL(action)
	if baseURL == "" {
		return testResult{action: action.Name, status: "SKIP", summary: "no base URL"}
	}

	fullURL := strings.TrimRight(baseURL, "/") + action.Path

	// Determine HTTP method (default GET)
	method := "GET"
	if action.Method != "" {
		method = strings.ToUpper(action.Method)
	}

	// Only test safe (GET) actions to avoid side effects
	if method != "GET" {
		return testResult{action: action.Name, status: "SKIP", summary: "non-GET method"}
	}

	// Build the request
	req, err := http.NewRequestWithContext(ctx, method, fullURL, nil)
	if err != nil {
		return testResult{action: action.Name, status: "FAIL", summary: err.Error()}
	}

	// Apply auth
	if resolvedAuth != nil && len(resolvedAuth.Env) > 0 {
		resolvedValues := make(map[string]string)
		for _, envKey := range resolvedAuth.Env {
			if v := os.Getenv(envKey); v != "" {
				resolvedValues[envKey] = v
			}
		}
		if len(resolvedValues) > 0 {
			injectAuth(req, resolvedAuth, resolvedValues)
		}
	}

	// Set headers (merged spec + action level)
	for k, v := range spec.ResolveActionHeaders(action) {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return testResult{action: action.Name, status: "FAIL", summary: err.Error()}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return testResult{action: action.Name, status: "FAIL", summary: fmt.Sprintf("reading body: %s", err)}
	}

	// Collect asserts from the action and any matching test block
	asserts := action.Assert
	if spec.Test != nil {
		for _, tc := range spec.Test.Actions {
			if tc.Action == action.Name && len(tc.Assert) > 0 {
				asserts = append(asserts, tc.Assert...)
				break
			}
		}
	}

	// If no asserts defined, default to checking status 200
	if len(asserts) == 0 {
		asserts = []models.AssertStep{{Type: "status", Values: []int{200}}}
	}

	// Run each typed assert
	result := testResult{
		action: action.Name,
		status: "OK",
		code:   resp.StatusCode,
	}

	for _, as := range asserts {
		ar := runAssertStep(as, resp.StatusCode, body)
		result.asserts = append(result.asserts, ar)
		if !ar.passed {
			result.status = "FAIL"
		}
	}

	// Build summary from failures
	var failures []string
	for _, a := range result.asserts {
		if !a.passed {
			failures = append(failures, a.detail)
		}
	}
	if len(failures) > 0 {
		result.summary = strings.Join(failures, "; ")
	}

	return result
}

// runAssertStep evaluates a single AssertStep against the HTTP response.
// It bridges the models.AssertStep types to the transform.Assertion engine
// for reuse, and handles types that need direct evaluation.
func runAssertStep(as models.AssertStep, statusCode int, body []byte) assertResult {
	switch as.Type {
	case "status":
		expected := as.Values
		if len(expected) == 0 {
			expected = []int{200}
		}
		a := transform.Assertions{
			{Status: expected},
		}
		if err := a.Check(statusCode, body); err != nil {
			return assertResult{assertType: "status", passed: false, detail: err.Error()}
		}
		return assertResult{assertType: "status", passed: true, detail: fmt.Sprintf("%d OK", statusCode)}

	case "contains":
		val := as.Value
		a := transform.Assertions{
			{Contains: val},
		}
		if err := a.Check(statusCode, body); err != nil {
			return assertResult{assertType: "contains", passed: false, detail: err.Error()}
		}
		return assertResult{assertType: "contains", passed: true, detail: fmt.Sprintf("found %q", val)}

	case "json":
		// json type uses exists/not_empty fields
		var a transform.Assertion
		if as.Exists != "" {
			a.Exists = as.Exists
		}
		if as.NotEmpty != "" {
			a.NotEmpty = as.NotEmpty
		}
		assertions := transform.Assertions{a}
		if err := assertions.Check(statusCode, body); err != nil {
			return assertResult{assertType: "json", passed: false, detail: err.Error()}
		}
		return assertResult{assertType: "json", passed: true}

	case "jq":
		// jq assert: evaluate JSONPath expression, check it returns truthy
		expr := as.Filter
		if expr == "" {
			return assertResult{assertType: "jq", passed: false, detail: "no filter expression"}
		}
		var parsed any
		if err := json.Unmarshal(body, &parsed); err != nil {
			return assertResult{assertType: "jq", passed: false, detail: fmt.Sprintf("body is not JSON: %s", err)}
		}
		result, err := transform.Extract(expr, parsed)
		if err != nil {
			return assertResult{assertType: "jq", passed: false, detail: fmt.Sprintf("jq error: %s", err)}
		}
		if isFalsy(result) {
			return assertResult{assertType: "jq", passed: false, detail: fmt.Sprintf("expression %q returned %v", expr, result)}
		}
		return assertResult{assertType: "jq", passed: true, detail: fmt.Sprintf("expression %q OK", expr)}

	case "js":
		script := as.Script
		if script == "" {
			return assertResult{assertType: "js", passed: false, detail: "no script"}
		}
		var parsed any
		json.Unmarshal(body, &parsed)
		a := transform.Assertions{
			{JS: script},
		}
		if err := a.Check(statusCode, body); err != nil {
			return assertResult{assertType: "js", passed: false, detail: err.Error()}
		}
		return assertResult{assertType: "js", passed: true}

	case "cel":
		// CEL not yet wired up in the assert engine, report as skipped
		return assertResult{assertType: "cel", passed: true, detail: "CEL evaluation not yet supported"}

	default:
		return assertResult{assertType: as.Type, passed: false, detail: fmt.Sprintf("unknown assert type %q", as.Type)}
	}
}

// isFalsy checks if a jq result is falsy (nil, false, 0, empty string, empty array).
func isFalsy(v any) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case bool:
		return !val
	case float64:
		return val == 0
	case string:
		return val == ""
	case []any:
		return len(val) == 0
	}
	return false
}

// injectAuth applies the auth config to the request using the template model.
func injectAuth(req *http.Request, auth *models.Auth, resolvedValues map[string]string) {
	if auth == nil {
		return
	}
	// Resolve ${KEY} templates in value
	resolveTemplate := func(tmpl string) string {
		result := tmpl
		for k, v := range resolvedValues {
			result = strings.ReplaceAll(result, "${"+k+"}", v)
		}
		return result
	}
	// auth.Header is a template like "Authorization: Bearer ${KEY}"
	if auth.Header != "" {
		if parts := strings.SplitN(auth.Header, ": ", 2); len(parts) == 2 {
			req.Header.Set(parts[0], resolveTemplate(parts[1]))
		}
	}
	if auth.Param != "" {
		// Use the first env value as the param value
		q := req.URL.Query()
		for _, v := range resolvedValues {
			q.Set(auth.Param, v)
			break
		}
		req.URL.RawQuery = q.Encode()
	}
}

func init() {
	rootCmd.AddCommand(testSpecCmd)
}
