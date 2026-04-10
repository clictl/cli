// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package transform

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Assertion validates a response before transforms run.
type Assertion struct {
	Status   []int  `yaml:"status,omitempty" json:"status,omitempty"`
	Exists   string `yaml:"exists,omitempty" json:"exists,omitempty"`
	NotEmpty string `yaml:"not_empty,omitempty" json:"not_empty,omitempty"`
	Equals   *EqualsAssertion `yaml:"equals,omitempty" json:"equals,omitempty"`
	Contains string `yaml:"contains,omitempty" json:"contains,omitempty"`
	JS       string `yaml:"js,omitempty" json:"js,omitempty"`
}

// EqualsAssertion checks that a JSONPath value equals an expected value.
type EqualsAssertion struct {
	Path  string `yaml:"path" json:"path"`
	Value any    `yaml:"value" json:"value"`
}

// Assertions is a list of assertions to check.
type Assertions []Assertion

// Check runs all assertions against the response. Returns nil if all pass.
func (a Assertions) Check(statusCode int, body []byte) error {
	if len(a) == 0 {
		return nil
	}

	var parsed any
	isJSON := json.Unmarshal(body, &parsed) == nil

	for i, assertion := range a {
		if err := checkAssertion(assertion, statusCode, body, parsed, isJSON); err != nil {
			return fmt.Errorf("assertion %d failed: %w", i+1, err)
		}
	}
	return nil
}

func checkAssertion(a Assertion, statusCode int, body []byte, parsed any, isJSON bool) error {
	if len(a.Status) > 0 {
		found := false
		for _, s := range a.Status {
			if s == statusCode {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("expected status %v, got %d", a.Status, statusCode)
		}
	}

	if a.Exists != "" {
		if !isJSON {
			return fmt.Errorf("exists check requires JSON response")
		}
		_, err := applyExtract(a.Exists, parsed)
		if err != nil {
			return fmt.Errorf("path %s does not exist: %w", a.Exists, err)
		}
	}

	if a.NotEmpty != "" {
		if !isJSON {
			return fmt.Errorf("not_empty check requires JSON response")
		}
		val, err := applyExtract(a.NotEmpty, parsed)
		if err != nil {
			return fmt.Errorf("path %s does not exist: %w", a.NotEmpty, err)
		}
		if isEmpty(val) {
			return fmt.Errorf("path %s is empty", a.NotEmpty)
		}
	}

	if a.Equals != nil {
		if !isJSON {
			return fmt.Errorf("equals check requires JSON response")
		}
		val, err := applyExtract(a.Equals.Path, parsed)
		if err != nil {
			return fmt.Errorf("path %s does not exist: %w", a.Equals.Path, err)
		}
		if !valuesEqual(val, a.Equals.Value) {
			return fmt.Errorf("path %s: expected %v, got %v", a.Equals.Path, a.Equals.Value, val)
		}
	}

	if a.Contains != "" {
		if !strings.Contains(string(body), a.Contains) {
			return fmt.Errorf("response does not contain %q", a.Contains)
		}
	}

	if a.JS != "" {
		if err := checkJSAssertion(a.JS, statusCode, body, parsed, isJSON); err != nil {
			return err
		}
	}

	return nil
}

func isEmpty(val any) bool {
	if val == nil {
		return true
	}
	switch v := val.(type) {
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	case bool:
		return !v
	case float64:
		return v == 0
	}
	return false
}

func valuesEqual(got, expected any) bool {
	// Normalize numbers for comparison (JSON numbers are float64)
	switch e := expected.(type) {
	case int:
		if g, ok := got.(float64); ok {
			return g == float64(e)
		}
	case float64:
		if g, ok := got.(float64); ok {
			return g == e
		}
	}
	return fmt.Sprintf("%v", got) == fmt.Sprintf("%v", expected)
}

func checkJSAssertion(script string, statusCode int, body []byte, parsed any, isJSON bool) error {
	if err := validateScript(script); err != nil {
		return err
	}

	vm := sandboxVM()

	input := map[string]any{
		"status_code": statusCode,
		"body_raw":    string(body),
	}
	if isJSON {
		input["body"] = parsed
	}

	jsonBytes, _ := json.Marshal(input)
	vm.Set("__raw_data__", string(jsonBytes))

	fullScript := fmt.Sprintf(`
		var __data__ = JSON.parse(__raw_data__);
		delete __raw_data__;
		%s
		if (typeof assert !== 'function') {
			throw new Error('Script must define an assert(response) function');
		}
		var __result__ = assert(__data__);
		JSON.stringify(__result__);
	`, script)

	val, err := vm.RunString(fullScript)
	if err != nil {
		return fmt.Errorf("JS assertion failed: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(val.String()), &result); err != nil {
		// If it returned a simple true/false
		if val.String() == "true" || val.String() == `"true"` {
			return nil
		}
		return fmt.Errorf("assertion returned: %s", val.String())
	}

	pass, _ := result["pass"].(bool)
	if !pass {
		reason, _ := result["reason"].(string)
		if reason == "" {
			reason = "assertion failed"
		}
		return fmt.Errorf("%s", reason)
	}
	return nil
}

// ParseAssertions converts raw assert config into Assertions.
func ParseAssertions(raw any) (Assertions, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case []any:
		var assertions Assertions
		for i, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("assert step %d: expected object", i+1)
			}
			a, err := mapToAssertion(m)
			if err != nil {
				return nil, fmt.Errorf("assert step %d: %w", i+1, err)
			}
			assertions = append(assertions, a)
		}
		return assertions, nil
	default:
		return nil, fmt.Errorf("assert: expected array, got %T", raw)
	}
}

func mapToAssertion(m map[string]any) (Assertion, error) {
	var a Assertion

	if v, ok := m["status"]; ok {
		switch s := v.(type) {
		case []any:
			for _, code := range s {
				if n, ok := code.(float64); ok {
					a.Status = append(a.Status, int(n))
				}
			}
		case float64:
			a.Status = []int{int(s)}
		}
	}
	if v, ok := m["exists"].(string); ok {
		a.Exists = v
	}
	if v, ok := m["not_empty"].(string); ok {
		a.NotEmpty = v
	}
	if v, ok := m["contains"].(string); ok {
		a.Contains = v
	}
	if v, ok := m["js"].(string); ok {
		a.JS = v
	}
	if v, ok := m["equals"].(map[string]any); ok {
		a.Equals = &EqualsAssertion{
			Path:  v["path"].(string),
			Value: v["value"],
		}
	}

	return a, nil
}
