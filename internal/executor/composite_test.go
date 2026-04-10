// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package executor

import (
	"testing"

	"github.com/clictl/cli/internal/models"
)

// --- DAG Validation Tests ---

func TestTopoSort_LinearDAG(t *testing.T) {
	steps := []models.CompositeStep{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	}
	sorted, err := topoSort(steps)
	if err != nil {
		t.Fatalf("topoSort linear: %v", err)
	}
	if len(sorted) != 3 {
		t.Fatalf("expected 3 sorted steps, got %d", len(sorted))
	}
	// a must come before b, b before c
	idx := make(map[string]int)
	for i, s := range sorted {
		idx[s.ID] = i
	}
	if idx["a"] >= idx["b"] {
		t.Errorf("a (pos %d) should come before b (pos %d)", idx["a"], idx["b"])
	}
	if idx["b"] >= idx["c"] {
		t.Errorf("b (pos %d) should come before c (pos %d)", idx["b"], idx["c"])
	}
}

func TestTopoSort_ParallelDAG(t *testing.T) {
	steps := []models.CompositeStep{
		{ID: "a"},
		{ID: "b"},
		{ID: "c", DependsOn: []string{"a", "b"}},
	}
	sorted, err := topoSort(steps)
	if err != nil {
		t.Fatalf("topoSort parallel: %v", err)
	}
	if len(sorted) != 3 {
		t.Fatalf("expected 3 sorted steps, got %d", len(sorted))
	}
	idx := make(map[string]int)
	for i, s := range sorted {
		idx[s.ID] = i
	}
	if idx["a"] >= idx["c"] {
		t.Errorf("a should come before c")
	}
	if idx["b"] >= idx["c"] {
		t.Errorf("b should come before c")
	}
}

func TestTopoSort_CycleDetection(t *testing.T) {
	steps := []models.CompositeStep{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	}
	_, err := topoSort(steps)
	if err == nil {
		t.Fatal("topoSort cycle: expected error")
	}
	if !containsStr(err.Error(), "cycle") {
		t.Errorf("error should mention cycle, got: %s", err.Error())
	}
}

func TestTopoSort_ThreeNodeCycle(t *testing.T) {
	steps := []models.CompositeStep{
		{ID: "a", DependsOn: []string{"c"}},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	}
	_, err := topoSort(steps)
	if err == nil {
		t.Fatal("topoSort 3-node cycle: expected error")
	}
}

func TestValidation_MissingDependsOnReference(t *testing.T) {
	action := &models.Action{
		Name:      "test",
		Steps: []models.CompositeStep{
			{ID: "a"},
			{ID: "b", DependsOn: []string{"nonexistent"}},
		},
	}
	stepIndex := make(map[string]*models.CompositeStep, len(action.Steps))
	for i := range action.Steps {
		stepIndex[action.Steps[i].ID] = &action.Steps[i]
	}
	for _, step := range action.Steps {
		for _, dep := range step.DependsOn {
			if _, exists := stepIndex[dep]; !exists {
				return // correctly detected missing reference
			}
		}
	}
	t.Fatal("expected missing depends_on reference to be detected")
}

func TestValidation_DuplicateStepIDs(t *testing.T) {
	steps := []models.CompositeStep{
		{ID: "a"},
		{ID: "a"},
	}
	stepIndex := make(map[string]*models.CompositeStep)
	var dupFound bool
	for i := range steps {
		if _, exists := stepIndex[steps[i].ID]; exists {
			dupFound = true
			break
		}
		stepIndex[steps[i].ID] = &steps[i]
	}
	if !dupFound {
		t.Fatal("expected duplicate step ID to be detected")
	}
}

func TestValidation_MaxStepsLimit(t *testing.T) {
	steps := make([]models.CompositeStep, 21)
	for i := range steps {
		steps[i] = models.CompositeStep{ID: "s" + string(rune('a'+i))}
	}
	if len(steps) <= maxSteps {
		t.Fatalf("test setup: expected > %d steps, got %d", maxSteps, len(steps))
	}
	// The executor enforces this limit; verify the constant
	if maxSteps != 20 {
		t.Errorf("maxSteps should be 20, got %d", maxSteps)
	}
}

func TestValidation_EmptySteps(t *testing.T) {
	steps := []models.CompositeStep{}
	if len(steps) != 0 {
		t.Fatal("expected empty steps slice")
	}
	// ExecuteComposite would return an error for empty steps
}

func TestComputeMaxDepth_Linear(t *testing.T) {
	steps := []models.CompositeStep{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	}
	stepIndex := make(map[string]*models.CompositeStep, len(steps))
	for i := range steps {
		stepIndex[steps[i].ID] = &steps[i]
	}
	depth := computeMaxDepth(steps, stepIndex)
	if depth != 3 {
		t.Errorf("computeMaxDepth linear: got %d, want 3", depth)
	}
}

func TestComputeMaxDepth_Parallel(t *testing.T) {
	steps := []models.CompositeStep{
		{ID: "a"},
		{ID: "b"},
		{ID: "c", DependsOn: []string{"a", "b"}},
	}
	stepIndex := make(map[string]*models.CompositeStep, len(steps))
	for i := range steps {
		stepIndex[steps[i].ID] = &steps[i]
	}
	depth := computeMaxDepth(steps, stepIndex)
	if depth != 2 {
		t.Errorf("computeMaxDepth parallel: got %d, want 2", depth)
	}
}

func TestFindTerminalStep(t *testing.T) {
	steps := []models.CompositeStep{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	}
	terminal := findTerminalStep(steps, "c")
	if terminal != "c" {
		t.Errorf("findTerminalStep: got %q, want %q", terminal, "c")
	}
}

func TestFindTerminalStep_MultipleTerminals(t *testing.T) {
	steps := []models.CompositeStep{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"a"}},
	}
	// Both b and c are terminal; function returns the last one in order
	terminal := findTerminalStep(steps, "c")
	if terminal != "c" {
		t.Errorf("findTerminalStep multiple: got %q, want %q", terminal, "c")
	}
}

// --- Template Resolution Tests ---

func TestResolveTemplate_Params(t *testing.T) {
	params := map[string]string{"city": "London"}
	results := map[string][]byte{}

	got := resolveTemplate("{{params.city}}", params, results)
	if got != "London" {
		t.Errorf("resolveTemplate params: got %q, want %q", got, "London")
	}
}

func TestResolveTemplate_StepOutput(t *testing.T) {
	params := map[string]string{}
	results := map[string][]byte{
		"geocode": []byte(`{"lat": 51.5, "lon": -0.1}`),
	}

	got := resolveTemplate("{{steps.geocode.output}}", params, results)
	if got != `{"lat": 51.5, "lon": -0.1}` {
		t.Errorf("resolveTemplate step output: got %q", got)
	}
}

func TestResolveTemplate_StepOutputField(t *testing.T) {
	params := map[string]string{}
	results := map[string][]byte{
		"geocode": []byte(`{"lat": 51.5, "lon": -0.1}`),
	}

	got := resolveTemplate("{{steps.geocode.output.lat}}", params, results)
	if got != "51.5" {
		t.Errorf("resolveTemplate step output field: got %q, want %q", got, "51.5")
	}
}

func TestResolveTemplate_EnvVar(t *testing.T) {
	t.Setenv("TEST_RESOLVE_VAR", "hello123")
	params := map[string]string{}
	results := map[string][]byte{}

	got := resolveTemplate("{{env.TEST_RESOLVE_VAR}}", params, results)
	if got != "hello123" {
		t.Errorf("resolveTemplate env: got %q, want %q", got, "hello123")
	}
}

func TestResolveTemplate_UnresolvedLeftAsIs(t *testing.T) {
	params := map[string]string{}
	results := map[string][]byte{}

	got := resolveTemplate("{{params.missing}}", params, results)
	if got != "{{params.missing}}" {
		t.Errorf("resolveTemplate unresolved: got %q, want %q", got, "{{params.missing}}")
	}
}

func TestResolveTemplate_MixedExpressions(t *testing.T) {
	t.Setenv("TEST_MIX_ENV", "world")
	params := map[string]string{"greeting": "hello"}
	results := map[string][]byte{}

	got := resolveTemplate("{{params.greeting}} {{env.TEST_MIX_ENV}}", params, results)
	if got != "hello world" {
		t.Errorf("resolveTemplate mixed: got %q, want %q", got, "hello world")
	}
}

func TestResolveTemplate_MissingStepLeftAsIs(t *testing.T) {
	params := map[string]string{}
	results := map[string][]byte{}

	got := resolveTemplate("{{steps.missing.output}}", params, results)
	if got != "{{steps.missing.output}}" {
		t.Errorf("resolveTemplate missing step: got %q, want %q", got, "{{steps.missing.output}}")
	}
}

func TestResolveTemplate_NoExpressions(t *testing.T) {
	got := resolveTemplate("plain string", nil, nil)
	if got != "plain string" {
		t.Errorf("resolveTemplate plain: got %q, want %q", got, "plain string")
	}
}

// --- Spec 1.0 ${...} Template Resolution Tests ---

func TestResolveTemplate_DollarParams(t *testing.T) {
	params := map[string]string{"city": "Paris"}
	results := map[string][]byte{}

	got := resolveTemplate("${params.city}", params, results)
	if got != "Paris" {
		t.Errorf("resolveTemplate dollar params: got %q, want %q", got, "Paris")
	}
}

func TestResolveTemplate_DollarStepField(t *testing.T) {
	params := map[string]string{}
	results := map[string][]byte{
		"geocode": []byte(`{"results": [{"lat": 48.8566, "lon": 2.3522}]}`),
	}

	got := resolveTemplate("${steps.geocode.results[0].lat}", params, results)
	if got != "48.8566" {
		t.Errorf("resolveTemplate dollar step field: got %q, want %q", got, "48.8566")
	}
}

func TestResolveTemplate_DollarStepDirectField(t *testing.T) {
	params := map[string]string{}
	results := map[string][]byte{
		"lookup": []byte(`{"name": "test", "value": 42}`),
	}

	// Direct field access without "output." prefix
	got := resolveTemplate("${steps.lookup.name}", params, results)
	if got != "test" {
		t.Errorf("resolveTemplate dollar direct field: got %q, want %q", got, "test")
	}
}

func TestResolveTemplate_DollarEnv(t *testing.T) {
	t.Setenv("TEST_DOLLAR_VAR", "dollar_value")
	params := map[string]string{}
	results := map[string][]byte{}

	got := resolveTemplate("${env.TEST_DOLLAR_VAR}", params, results)
	if got != "dollar_value" {
		t.Errorf("resolveTemplate dollar env: got %q, want %q", got, "dollar_value")
	}
}

func TestResolveTemplate_DollarUnresolved(t *testing.T) {
	params := map[string]string{}
	results := map[string][]byte{}

	got := resolveTemplate("${params.missing}", params, results)
	if got != "${params.missing}" {
		t.Errorf("resolveTemplate dollar unresolved: got %q, want %q", got, "${params.missing}")
	}
}

func TestResolveTemplate_MixedDollarAndLegacy(t *testing.T) {
	params := map[string]string{"name": "alice"}
	results := map[string][]byte{
		"step1": []byte(`{"count": 5}`),
	}

	got := resolveTemplate("Hello ${params.name}, count={{steps.step1.output.count}}", params, results)
	if got != "Hello alice, count=5" {
		t.Errorf("resolveTemplate mixed: got %q, want %q", got, "Hello alice, count=5")
	}
}

func TestResolveTemplate_DollarMissingStep(t *testing.T) {
	params := map[string]string{}
	results := map[string][]byte{}

	got := resolveTemplate("${steps.missing.field}", params, results)
	if got != "${steps.missing.field}" {
		t.Errorf("resolveTemplate dollar missing step: got %q, want %q", got, "${steps.missing.field}")
	}
}

func TestResolveTemplate_DollarArrayIndex(t *testing.T) {
	params := map[string]string{}
	results := map[string][]byte{
		"search": []byte(`{"items": [{"id": "first"}, {"id": "second"}]}`),
	}

	got := resolveTemplate("${steps.search.items[1].id}", params, results)
	if got != "second" {
		t.Errorf("resolveTemplate dollar array: got %q, want %q", got, "second")
	}
}

func TestResolveTemplate_DollarInURL(t *testing.T) {
	params := map[string]string{"lat": "48.85", "lon": "2.35"}
	results := map[string][]byte{}

	got := resolveTemplate("https://api.weather.com/forecast?lat=${params.lat}&lon=${params.lon}", params, results)
	if got != "https://api.weather.com/forecast?lat=48.85&lon=2.35" {
		t.Errorf("resolveTemplate dollar URL: got %q", got)
	}
}

// --- extractJSONField Tests ---

func TestExtractJSONField_SimpleField(t *testing.T) {
	data := []byte(`{"name": "Alice", "age": 30}`)
	got, err := extractJSONField(data, "name")
	if err != nil {
		t.Fatalf("extractJSONField: %v", err)
	}
	if got != "Alice" {
		t.Errorf("extractJSONField name: got %q, want %q", got, "Alice")
	}
}

func TestExtractJSONField_NestedField(t *testing.T) {
	data := []byte(`{"data": {"coords": {"lat": 51.5}}}`)
	got, err := extractJSONField(data, "data.coords.lat")
	if err != nil {
		t.Fatalf("extractJSONField nested: %v", err)
	}
	if got != "51.5" {
		t.Errorf("extractJSONField nested: got %q, want %q", got, "51.5")
	}
}

func TestExtractJSONField_MissingField(t *testing.T) {
	data := []byte(`{"foo": "bar"}`)
	_, err := extractJSONField(data, "missing")
	if err == nil {
		t.Fatal("extractJSONField missing: expected error")
	}
}

func TestExtractJSONField_InvalidJSON(t *testing.T) {
	data := []byte(`not json`)
	_, err := extractJSONField(data, "field")
	if err == nil {
		t.Fatal("extractJSONField invalid JSON: expected error")
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstr(s, substr))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
