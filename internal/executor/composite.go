// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/registry"
)

const (
	maxSteps    = 20
	maxDepth    = 3
	maxCycleLen = 100
)

// templateExprRegex matches template expressions like {{params.X}}, {{steps.Y.output}}, {{env.VAR}}.
var templateExprRegex = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// dollarExprRegex matches spec 1.0 template expressions like ${params.city}, ${steps.geocode.results[0].lat}.
var dollarExprRegex = regexp.MustCompile(`\$\{([^}]+)\}`)

// ExecuteComposite runs a composite action by executing its steps in dependency order.
// Steps form a DAG (directed acyclic graph) resolved via topological sort.
// Each step's output is stored and can be referenced by subsequent steps.
func ExecuteComposite(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]string, cfg *config.Config) ([]byte, error) {
	if len(action.Steps) == 0 {
		return nil, fmt.Errorf("composite action %q has no steps", action.Name)
	}

	if len(action.Steps) > maxSteps {
		return nil, fmt.Errorf("composite action %q exceeds maximum of %d steps (has %d)", action.Name, maxSteps, len(action.Steps))
	}

	// Build step index and validate IDs are unique
	stepIndex := make(map[string]*models.CompositeStep, len(action.Steps))
	for i := range action.Steps {
		step := &action.Steps[i]
		if step.ID == "" {
			return nil, fmt.Errorf("composite action %q: step %d is missing an ID", action.Name, i+1)
		}
		if _, exists := stepIndex[step.ID]; exists {
			return nil, fmt.Errorf("composite action %q: duplicate step ID %q", action.Name, step.ID)
		}
		stepIndex[step.ID] = step
	}

	// Validate depends_on references exist
	for _, step := range action.Steps {
		for _, dep := range step.DependsOn {
			if _, exists := stepIndex[dep]; !exists {
				return nil, fmt.Errorf("composite action %q: step %q depends on unknown step %q", action.Name, step.ID, dep)
			}
		}
	}

	// Topological sort with cycle detection
	sorted, err := topoSort(action.Steps)
	if err != nil {
		return nil, fmt.Errorf("composite action %q: %w", action.Name, err)
	}

	// Validate max depth
	depth := computeMaxDepth(action.Steps, stepIndex)
	if depth > maxDepth {
		return nil, fmt.Errorf("composite action %q: dependency depth %d exceeds maximum of %d", action.Name, depth, maxDepth)
	}

	// Execute steps in topological order
	results := make(map[string][]byte)
	var lastStepID string

	for _, step := range sorted {
		lastStepID = step.ID

		// Evaluate condition if present
		if step.Condition != "" {
			condResult := resolveTemplate(step.Condition, params, results)
			if condResult == "false" || condResult == "" {
				continue
			}
		}

		// Resolve parameters with template expressions
		stepParams := make(map[string]string)
		for k, v := range step.Params {
			stepParams[k] = resolveTemplate(v, params, results)
		}

		// Execute the step with retry support
		var output []byte
		var execErr error

		maxAttempts := 1
		var retryDelay time.Duration
		if step.Retry != nil && step.Retry.MaxAttempts > 1 {
			maxAttempts = step.Retry.MaxAttempts
			if step.Retry.Delay != "" {
				if d, err := time.ParseDuration(step.Retry.Delay); err == nil {
					retryDelay = d
				}
			}
			if retryDelay == 0 {
				retryDelay = 1 * time.Second
			}
		}

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			output, execErr = executeStep(ctx, spec, &step, stepParams, cfg)
			if execErr == nil {
				break
			}
			if attempt < maxAttempts && retryDelay > 0 {
				time.Sleep(retryDelay)
			}
		}

		if execErr != nil {
			switch step.OnError {
			case "skip":
				continue
			case "continue":
				results[step.ID] = []byte("{}")
				continue
			default: // "fail" or empty
				return nil, fmt.Errorf("step %q failed: %w", step.ID, execErr)
			}
		}

		results[step.ID] = output
	}

	// Find terminal step (no other step depends on it), or use the last executed step
	terminalID := findTerminalStep(action.Steps, lastStepID)
	var finalOutput []byte
	if result, ok := results[terminalID]; ok {
		finalOutput = result
	} else {
		finalOutput = []byte("{}")
	}

	// If the action has a template transform, resolve step/param references first
	if len(action.Transform) > 0 {
		resolved := resolveCompositeTransform(action.Transform, params, results)
		if resolved != "" {
			return []byte(resolved), nil
		}
	}

	return finalOutput, nil
}

// resolveCompositeTransform resolves {{params.*}}, {{steps.*}}, ${params.*}, and ${steps.*}
// expressions in a composite action's transform template and returns the result as a string.
func resolveCompositeTransform(steps []models.TransformStep, params map[string]string, results map[string][]byte) string {
	for _, step := range steps {
		if step.Template != "" {
			return resolveTemplate(step.Template, params, results)
		}
	}
	return ""
}

// executeStep runs a single composite step, resolving cross-tool references as needed.
// Supports two styles:
//   - Tool/Action reference: step has tool + action fields pointing to another spec
//   - Inline URL: step has method + url fields for direct HTTP calls
func executeStep(ctx context.Context, currentSpec *models.ToolSpec, step *models.CompositeStep, params map[string]string, cfg *config.Config) ([]byte, error) {
	// Inline URL style: step has url field, make a direct HTTP call
	if step.URL != "" {
		return executeInlineStep(ctx, step, params)
	}

	var targetSpec *models.ToolSpec

	if step.Tool != "" && step.Tool != currentSpec.Name {
		// Cross-tool step: resolve the other tool's spec
		cache := registry.NewCache(cfg.CacheDir)
		resolved, err := registry.ResolveSpec(ctx, step.Tool, cfg, cache, false)
		if err != nil {
			return nil, fmt.Errorf("resolving tool %q for step %q: %w", step.Tool, step.ID, err)
		}
		targetSpec = resolved
	} else {
		targetSpec = currentSpec
	}

	targetAction, err := registry.FindAction(targetSpec, step.Action)
	if err != nil {
		return nil, fmt.Errorf("finding action for step %q: %w", step.ID, err)
	}

	// If the target action is itself composite, prevent excessive nesting
	if targetAction.IsComposite() {
		return nil, fmt.Errorf("step %q references composite action %q (nested composites are not supported)", step.ID, step.Action)
	}

	anyParams := make(map[string]any, len(params))
	for k, v := range params {
		anyParams[k] = v
	}
	return DispatchWithOptions(ctx, targetSpec, targetAction, anyParams, nil, cfg)
}

// executeInlineStep makes a direct HTTP call using the step's request fields.
func executeInlineStep(ctx context.Context, step *models.CompositeStep, params map[string]string) ([]byte, error) {
	method := "GET"
	if step.Method != "" {
		method = strings.ToUpper(step.Method)
	}

	// Build URL with query params
	stepURL := step.URL
	queryParts := []string{}
	for k, v := range params {
		queryParts = append(queryParts, fmt.Sprintf("%s=%s", url.QueryEscape(k), url.QueryEscape(v)))
	}
	if len(queryParts) > 0 {
		separator := "?"
		if strings.Contains(stepURL, "?") {
			separator = "&"
		}
		stepURL += separator + strings.Join(queryParts, "&")
	}

	req, err := http.NewRequestWithContext(ctx, method, stepURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for step %q: %w", step.ID, err)
	}

	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "clictl/1.0 (https://clictl.dev)")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request for step %q: %w", step.ID, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response for step %q: %w", step.ID, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// topoSort performs a topological sort of steps using Kahn's algorithm.
func topoSort(steps []models.CompositeStep) ([]models.CompositeStep, error) {
	inDegree := make(map[string]int)
	dependents := make(map[string][]string)
	stepMap := make(map[string]models.CompositeStep)

	for _, step := range steps {
		stepMap[step.ID] = step
		if _, exists := inDegree[step.ID]; !exists {
			inDegree[step.ID] = 0
		}
		for _, dep := range step.DependsOn {
			dependents[dep] = append(dependents[dep], step.ID)
			inDegree[step.ID]++
		}
	}

	// Start with steps that have no dependencies
	var queue []string
	for _, step := range steps {
		if inDegree[step.ID] == 0 {
			queue = append(queue, step.ID)
		}
	}

	var sorted []models.CompositeStep
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]

		sorted = append(sorted, stepMap[id])

		for _, dep := range dependents[id] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(sorted) != len(steps) {
		return nil, fmt.Errorf("dependency cycle detected among steps")
	}

	return sorted, nil
}

// computeMaxDepth computes the longest dependency chain in the step DAG.
func computeMaxDepth(steps []models.CompositeStep, stepIndex map[string]*models.CompositeStep) int {
	cache := make(map[string]int)

	var depth func(id string) int
	depth = func(id string) int {
		if d, ok := cache[id]; ok {
			return d
		}
		step := stepIndex[id]
		if step == nil || len(step.DependsOn) == 0 {
			cache[id] = 1
			return 1
		}
		maxDep := 0
		for _, dep := range step.DependsOn {
			d := depth(dep)
			if d > maxDep {
				maxDep = d
			}
		}
		cache[id] = maxDep + 1
		return maxDep + 1
	}

	maxD := 0
	for _, step := range steps {
		d := depth(step.ID)
		if d > maxD {
			maxD = d
		}
	}
	return maxD
}

// findTerminalStep returns the ID of a step that no other step depends on.
// If multiple terminal steps exist, returns the last one in the original order.
// Falls back to lastStepID if no unique terminal is found.
func findTerminalStep(steps []models.CompositeStep, lastStepID string) string {
	depended := make(map[string]bool)
	for _, step := range steps {
		for _, dep := range step.DependsOn {
			depended[dep] = true
		}
	}

	var terminal string
	for _, step := range steps {
		if !depended[step.ID] {
			terminal = step.ID
		}
	}
	if terminal != "" {
		return terminal
	}
	return lastStepID
}

// resolveTemplate replaces template expressions in a string.
// Supports both legacy {{...}} and spec 1.0 ${...} syntax.
//
// Legacy patterns ({{...}}):
//   - {{params.X}} - input parameter value
//   - {{steps.X.output}} - raw output of step X
//   - {{steps.X.output.field}} - JSON field from step X output
//   - {{env.VAR}} - environment variable
//
// Spec 1.0 patterns (${...}):
//   - ${params.X} - input parameter value
//   - ${steps.X.field} - JSON field from step X output (no "output." prefix needed)
//   - ${env.VAR} - environment variable
func resolveTemplate(tmpl string, params map[string]string, results map[string][]byte) string {
	// First pass: resolve ${...} (spec 1.0 syntax)
	result := dollarExprRegex.ReplaceAllStringFunc(tmpl, func(match string) string {
		expr := strings.TrimSpace(match[2 : len(match)-1]) // strip ${ and }
		return resolveExpr(expr, params, results, match, true)
	})

	// Second pass: resolve {{...}} (legacy syntax)
	result = templateExprRegex.ReplaceAllStringFunc(result, func(match string) string {
		expr := strings.TrimSpace(match[2 : len(match)-2]) // strip {{ and }}
		return resolveExpr(expr, params, results, match, false)
	})

	return result
}

// resolveExpr resolves a single template expression.
// When dollarSyntax is true, steps references use direct field paths (steps.X.field)
// instead of requiring the "output." prefix (steps.X.output.field).
func resolveExpr(expr string, params map[string]string, results map[string][]byte, original string, dollarSyntax bool) string {
	parts := strings.SplitN(expr, ".", 3)

	switch parts[0] {
	case "params":
		if len(parts) >= 2 {
			if val, ok := params[parts[1]]; ok {
				return val
			}
		}
	case "env":
		if len(parts) >= 2 {
			return os.Getenv(parts[1])
		}
	case "steps":
		if len(parts) >= 2 {
			stepID := parts[1]
			data, ok := results[stepID]
			if !ok {
				return original
			}

			// No field path - return raw output
			if len(parts) < 3 {
				return string(data)
			}

			rest := parts[2]

			if dollarSyntax {
				// ${steps.X.field} - direct field path, no "output." prefix
				val, err := extractJSONField(data, rest)
				if err != nil {
					return original
				}
				return val
			}

			// Legacy: {{steps.X.output}} or {{steps.X.output.field}}
			if rest == "output" {
				return string(data)
			}
			fieldPath := strings.TrimPrefix(rest, "output.")
			if fieldPath == rest {
				// No "output." prefix in legacy mode
				return original
			}
			val, err := extractJSONField(data, fieldPath)
			if err != nil {
				return original
			}
			return val
		}
	}

	return original
}

// extractJSONField extracts a dotted field path from JSON data.
// Supports array indexing with bracket notation: "results[0].latitude"
func extractJSONField(data []byte, fieldPath string) (string, error) {
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("parsing step output as JSON: %w", err)
	}

	// Split path but preserve array indices: "results[0].latitude" -> ["results[0]", "latitude"]
	parts := strings.Split(fieldPath, ".")
	current := parsed

	for _, part := range parts {
		// Check for array index: "results[0]" -> key="results", index=0
		if idx := strings.Index(part, "["); idx != -1 {
			key := part[:idx]
			indexStr := strings.TrimSuffix(part[idx+1:], "]")

			// Traverse into the object field first
			obj, ok := current.(map[string]any)
			if !ok {
				return "", fmt.Errorf("expected object at %q, got %T", key, current)
			}
			arr, exists := obj[key]
			if !exists {
				return "", fmt.Errorf("field %q not found", key)
			}

			// Index into the array
			slice, ok := arr.([]any)
			if !ok {
				return "", fmt.Errorf("expected array at %q, got %T", key, arr)
			}
			var arrayIdx int
			if _, err := fmt.Sscanf(indexStr, "%d", &arrayIdx); err != nil {
				return "", fmt.Errorf("invalid array index %q", indexStr)
			}
			if arrayIdx < 0 || arrayIdx >= len(slice) {
				return "", fmt.Errorf("array index %d out of range (length %d)", arrayIdx, len(slice))
			}
			current = slice[arrayIdx]
		} else {
			obj, ok := current.(map[string]any)
			if !ok {
				return "", fmt.Errorf("expected object at %q, got %T", part, current)
			}
			val, exists := obj[part]
			if !exists {
				return "", fmt.Errorf("field %q not found", part)
			}
			current = val
		}
	}

	switch v := current.(type) {
	case string:
		return v, nil
	case float64:
		// Avoid scientific notation for coordinates
		if v == float64(int64(v)) {
			return fmt.Sprintf("%.0f", v), nil
		}
		return fmt.Sprintf("%g", v), nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v), nil
		}
		return string(b), nil
	}
}
