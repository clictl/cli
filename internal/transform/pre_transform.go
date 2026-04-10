// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package transform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"text/template"
)

// PreStep represents a single pre-request transform operation.
type PreStep struct {
	TemplateBody  string            `yaml:"template_body,omitempty" json:"template_body,omitempty"`
	DefaultParams map[string]string `yaml:"default_params,omitempty" json:"default_params,omitempty"`
	RenameParams  map[string]string `yaml:"rename_params,omitempty" json:"rename_params,omitempty"`
	JS            string            `yaml:"js,omitempty" json:"js,omitempty"`
}

// PrePipeline is an ordered list of pre-request transform steps.
type PrePipeline []PreStep

// RequestData holds the mutable request data that pre-transforms operate on.
type RequestData struct {
	Params map[string]string
	Body   string
}

// Apply runs the pre-request pipeline on the request data.
func (p PrePipeline) Apply(data *RequestData) error {
	for i, step := range p {
		if err := applyPreStep(step, data); err != nil {
			return fmt.Errorf("pre_transform step %d: %w", i+1, err)
		}
	}
	return nil
}

func applyPreStep(step PreStep, data *RequestData) error {
	if len(step.DefaultParams) > 0 {
		for k, v := range step.DefaultParams {
			if _, exists := data.Params[k]; !exists {
				data.Params[k] = v
			}
		}
	}

	if len(step.RenameParams) > 0 {
		for oldName, newName := range step.RenameParams {
			if val, exists := data.Params[oldName]; exists {
				data.Params[newName] = val
				delete(data.Params, oldName)
			}
		}
	}

	if step.TemplateBody != "" {
		tmpl, err := template.New("pre").Parse(step.TemplateBody)
		if err != nil {
			return fmt.Errorf("parsing template_body: %w", err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data.Params); err != nil {
			return fmt.Errorf("executing template_body: %w", err)
		}
		data.Body = buf.String()
	}

	if step.JS != "" {
		if err := applyPreJS(step.JS, data); err != nil {
			return err
		}
	}

	return nil
}

func applyPreJS(script string, data *RequestData) error {
	if err := validateScript(script); err != nil {
		return err
	}

	vm := sandboxVM()

	inputMap := map[string]any{
		"params": data.Params,
		"body":   data.Body,
	}
	jsonBytes, err := json.Marshal(inputMap)
	if err != nil {
		return fmt.Errorf("marshaling request data: %w", err)
	}

	vm.Set("__raw_data__", string(jsonBytes))

	fullScript := fmt.Sprintf(`
		var __data__ = JSON.parse(__raw_data__);
		delete __raw_data__;
		%s
		if (typeof transform !== 'function') {
			throw new Error('Script must define a transform(data) function');
		}
		var __result__ = transform(__data__);
		JSON.stringify(__result__);
	`, script)

	val, err := vm.RunString(fullScript)
	if err != nil {
		return fmt.Errorf("JS pre-transform failed: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(val.String()), &result); err != nil {
		return fmt.Errorf("parsing JS result: %w", err)
	}

	if params, ok := result["params"].(map[string]any); ok {
		data.Params = make(map[string]string, len(params))
		for k, v := range params {
			data.Params[k] = fmt.Sprintf("%v", v)
		}
	}
	if body, ok := result["body"].(string); ok {
		data.Body = body
	}

	return nil
}

// ParsePreSteps converts a raw pre_transform config into a PrePipeline.
func ParsePreSteps(raw any) (PrePipeline, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		step, err := mapToPreStep(v)
		if err != nil {
			return nil, err
		}
		return PrePipeline{step}, nil
	case []any:
		var pipeline PrePipeline
		for i, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("pre_transform step %d: expected object", i+1)
			}
			step, err := mapToPreStep(m)
			if err != nil {
				return nil, fmt.Errorf("pre_transform step %d: %w", i+1, err)
			}
			pipeline = append(pipeline, step)
		}
		return pipeline, nil
	default:
		return nil, fmt.Errorf("pre_transform: expected object or array, got %T", raw)
	}
}

func mapToPreStep(m map[string]any) (PreStep, error) {
	var step PreStep
	if v, ok := m["template_body"].(string); ok {
		step.TemplateBody = v
	}
	if v, ok := m["default_params"].(map[string]any); ok {
		step.DefaultParams = make(map[string]string, len(v))
		for k, val := range v {
			step.DefaultParams[k] = fmt.Sprintf("%v", val)
		}
	}
	if v, ok := m["rename_params"].(map[string]any); ok {
		step.RenameParams = make(map[string]string, len(v))
		for k, val := range v {
			if s, ok := val.(string); ok {
				step.RenameParams[k] = s
			}
		}
	}
	if v, ok := m["js"].(string); ok {
		step.JS = v
	}
	return step, nil
}
