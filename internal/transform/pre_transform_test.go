// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package transform

import (
	"testing"
)

func TestDefaultParams(t *testing.T) {
	data := &RequestData{Params: map[string]string{"q": "London"}}
	pipeline := PrePipeline{{DefaultParams: map[string]string{"units": "metric", "q": "ignored"}}}

	if err := pipeline.Apply(data); err != nil {
		t.Fatal(err)
	}
	if data.Params["units"] != "metric" {
		t.Errorf("expected metric, got %s", data.Params["units"])
	}
	if data.Params["q"] != "London" {
		t.Error("default should not overwrite existing param")
	}
}

func TestRenameParams(t *testing.T) {
	data := &RequestData{Params: map[string]string{"key": "abc123"}}
	pipeline := PrePipeline{{RenameParams: map[string]string{"key": "apiKey"}}}

	if err := pipeline.Apply(data); err != nil {
		t.Fatal(err)
	}
	if data.Params["apiKey"] != "abc123" {
		t.Errorf("expected abc123, got %s", data.Params["apiKey"])
	}
	if _, exists := data.Params["key"]; exists {
		t.Error("old param name should be removed")
	}
}

func TestTemplateBody(t *testing.T) {
	data := &RequestData{Params: map[string]string{"q": "test query"}}
	pipeline := PrePipeline{{TemplateBody: `{"query": "{ search(q: \"{{.q}}\") { id } }"}`}}

	if err := pipeline.Apply(data); err != nil {
		t.Fatal(err)
	}
	expected := `{"query": "{ search(q: \"test query\") { id } }"}`
	if data.Body != expected {
		t.Errorf("expected %q, got %q", expected, data.Body)
	}
}

func TestPreChain(t *testing.T) {
	data := &RequestData{Params: map[string]string{"city": "London"}}
	pipeline := PrePipeline{
		{DefaultParams: map[string]string{"format": "json"}},
		{RenameParams: map[string]string{"city": "q"}},
	}

	if err := pipeline.Apply(data); err != nil {
		t.Fatal(err)
	}
	if data.Params["q"] != "London" {
		t.Error("rename should have worked")
	}
	if data.Params["format"] != "json" {
		t.Error("default should have been injected")
	}
}

func TestPreJSTransform(t *testing.T) {
	data := &RequestData{Params: map[string]string{"lat": "51.5", "lon": "-0.1"}}
	pipeline := PrePipeline{{JS: `function transform(data) {
		data.params.coordinates = data.params.lat + "," + data.params.lon;
		delete data.params.lat;
		delete data.params.lon;
		return data;
	}`}}

	if err := pipeline.Apply(data); err != nil {
		t.Fatal(err)
	}
	if data.Params["coordinates"] != "51.5,-0.1" {
		t.Errorf("expected combined coords, got %s", data.Params["coordinates"])
	}
	if _, exists := data.Params["lat"]; exists {
		t.Error("lat should be removed")
	}
}

func TestEmptyPrePipeline(t *testing.T) {
	data := &RequestData{Params: map[string]string{"x": "1"}}
	pipeline := PrePipeline{}
	if err := pipeline.Apply(data); err != nil {
		t.Fatal(err)
	}
	if data.Params["x"] != "1" {
		t.Error("empty pipeline should not change data")
	}
}
