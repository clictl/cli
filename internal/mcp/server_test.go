// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/clictl/cli/internal/models"
)

func testSpec() *models.ToolSpec {
	return &models.ToolSpec{
		Name:        "testool",
		Description: "A test tool",
		Server:      &models.Server{Type: "http", URL: "https://api.example.com"},
		Actions: []models.Action{
			{
				Name:        "greet",
				Description: "Say hello",
				Method: "GET", Path: "/greet",
				Params: []models.Param{
					{Name: "name", Type: "string", Required: true, In: "query", Description: "Who to greet"},
					{Name: "lang", Type: "string", In: "query", Default: "en", Description: "Language"},
				},
			},
			{
				Name:        "count",
				Description: "Count items",
				Method: "GET", Path: "/count",
				Params: []models.Param{
					{Name: "limit", Type: "int", Required: false, In: "query"},
				},
			},
		},
	}
}

func sendRequest(t *testing.T, s *Server, method string, id interface{}, params interface{}) Response {
	t.Helper()
	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	line, _ := json.Marshal(req)

	var buf bytes.Buffer
	s.writer = &buf

	s.handleRequest(context.Background(), &req)
	_ = line // used for construction

	var resp Response
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal response: %v\nraw: %s", err, buf.String())
	}
	return resp
}

func TestServer_Initialize(t *testing.T) {
	s := NewServer([]*models.ToolSpec{testSpec()})
	resp := sendRequest(t, s, "initialize", 1, nil)

	if resp.Error != nil {
		t.Fatalf("Initialize error: %v", resp.Error.Message)
	}

	resultJSON, _ := json.Marshal(resp.Result)
	var result InitializeResult
	json.Unmarshal(resultJSON, &result)

	if result.ServerInfo.Name != "clictl" {
		t.Errorf("ServerInfo.Name: got %q, want %q", result.ServerInfo.Name, "clictl")
	}
	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("ProtocolVersion: got %q", result.ProtocolVersion)
	}
}

func TestServer_ToolsList(t *testing.T) {
	s := NewServer([]*models.ToolSpec{testSpec()})
	resp := sendRequest(t, s, "tools/list", 2, nil)

	if resp.Error != nil {
		t.Fatalf("tools/list error: %v", resp.Error.Message)
	}

	resultJSON, _ := json.Marshal(resp.Result)
	var result ToolsListResult
	json.Unmarshal(resultJSON, &result)

	if len(result.Tools) != 2 {
		t.Fatalf("Tools count: got %d, want 2", len(result.Tools))
	}

	// Verify tool names follow specName_actionName convention
	if result.Tools[0].Name != "testool_greet" {
		t.Errorf("Tool[0] name: got %q, want %q", result.Tools[0].Name, "testool_greet")
	}
	if result.Tools[1].Name != "testool_count" {
		t.Errorf("Tool[1] name: got %q, want %q", result.Tools[1].Name, "testool_count")
	}

	// Verify input schema
	greetSchema := result.Tools[0].InputSchema
	if greetSchema.Type != "object" {
		t.Errorf("Schema type: got %q, want %q", greetSchema.Type, "object")
	}
	if _, ok := greetSchema.Properties["name"]; !ok {
		t.Error("Missing 'name' property in schema")
	}
	if len(greetSchema.Required) != 1 || greetSchema.Required[0] != "name" {
		t.Errorf("Required: got %v, want [name]", greetSchema.Required)
	}
}

func TestServer_Ping(t *testing.T) {
	s := NewServer([]*models.ToolSpec{testSpec()})
	resp := sendRequest(t, s, "ping", 3, nil)

	if resp.Error != nil {
		t.Fatalf("ping error: %v", resp.Error.Message)
	}
}

func TestServer_UnknownMethod(t *testing.T) {
	s := NewServer([]*models.ToolSpec{testSpec()})
	resp := sendRequest(t, s, "unknown/method", 4, nil)

	if resp.Error == nil {
		t.Fatal("Expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("Error code: got %d, want -32601", resp.Error.Code)
	}
}

func TestBuildInputSchema(t *testing.T) {
	action := models.Action{
		Params: []models.Param{
			{Name: "query", Type: "string", Required: true, Description: "Search query"},
			{Name: "count", Type: "int", Required: false, Description: "Result count"},
			{Name: "score", Type: "float", Required: false, Description: "Min score"},
			{Name: "verbose", Type: "bool", Required: false, Description: "Verbose output"},
		},
	}

	schema := buildInputSchema(action)

	if schema.Type != "object" {
		t.Errorf("Schema type: got %q, want object", schema.Type)
	}
	if len(schema.Properties) != 4 {
		t.Errorf("Properties count: got %d, want 4", len(schema.Properties))
	}

	// Check type mapping
	if schema.Properties["query"].Type != "string" {
		t.Errorf("query type: got %q", schema.Properties["query"].Type)
	}
	if schema.Properties["count"].Type != "number" {
		t.Errorf("count type: got %q", schema.Properties["count"].Type)
	}
	if schema.Properties["score"].Type != "number" {
		t.Errorf("score type: got %q", schema.Properties["score"].Type)
	}
	if schema.Properties["verbose"].Type != "boolean" {
		t.Errorf("verbose type: got %q", schema.Properties["verbose"].Type)
	}

	// Check required
	if len(schema.Required) != 1 || schema.Required[0] != "query" {
		t.Errorf("Required: got %v, want [query]", schema.Required)
	}
}
