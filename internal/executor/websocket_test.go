// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/clictl/cli/internal/models"
)

func TestWebSocketExecutor_BasicEcho(t *testing.T) {
	// Create a test WebSocket server that echoes messages
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			conn.WriteMessage(mt, msg)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "websocket", URL: wsURL},
	}
	action := &models.Action{
		Name:    "echo",
		Message: `{"hello": "world"}`,
		Wait:    "2s",
		Collect: 1,
	}

	result, err := DispatchWithFullOptions(
		context.Background(), spec, action,
		map[string]any{},
		&DispatchOptions{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(result), "hello") {
		t.Errorf("expected echo response, got: %s", string(result))
	}
}

func TestWebSocketExecutor_ParamTemplating(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		conn.WriteMessage(mt, msg)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	spec := &models.ToolSpec{
		Server: &models.Server{Type: "websocket", URL: wsURL},
	}
	action := &models.Action{
		Name:    "subscribe",
		Message: `{"channel": "${symbol}"}`,
		Wait:    "2s",
		Collect: 1,
	}

	result, err := DispatchWithFullOptions(
		context.Background(), spec, action,
		map[string]any{"symbol": "btcusdt"},
		&DispatchOptions{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(result), "btcusdt") {
		t.Errorf("expected templated message, got: %s", string(result))
	}
}

func TestWebSocketExecutor_NoURL(t *testing.T) {
	spec := &models.ToolSpec{}
	action := &models.Action{Name: "test", Message: "hello"}

	_, err := DispatchWithFullOptions(
		context.Background(), spec, action,
		map[string]any{},
		&DispatchOptions{},
	)
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
	if !strings.Contains(err.Error(), "not yet implemented") && !strings.Contains(err.Error(), "no WebSocket URL") {
		t.Errorf("unexpected error: %v", err)
	}
}
