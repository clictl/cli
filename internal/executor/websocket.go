// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/models"
)

// WebSocketExecutor handles websocket server type specs.
type WebSocketExecutor struct {
	Config *config.Config
}

// Execute connects to a WebSocket server, sends a message, collects responses,
// and returns the result as JSON bytes.
func (e *WebSocketExecutor) Execute(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]string) ([]byte, error) {
	if e.Config != nil && e.Config.IsToolDisabled(spec.Name) {
		return nil, fmt.Errorf("tool %q is disabled. Run 'clictl enable %s' to re-enable it", spec.Name, spec.Name)
	}

	// Resolve the WebSocket URL
	wsURL := action.URL
	if wsURL == "" && spec.Server != nil {
		wsURL = spec.Server.URL
	}
	if wsURL == "" {
		return nil, fmt.Errorf("no WebSocket URL configured")
	}

	// Apply path if present
	if action.Path != "" {
		path := action.Path
		for k, v := range params {
			path = strings.ReplaceAll(path, "{"+k+"}", v)
			path = strings.ReplaceAll(path, "${"+k+"}", v)
		}
		wsURL = strings.TrimRight(wsURL, "/") + "/" + strings.TrimLeft(path, "/")
	}

	// Template the message
	message := action.Message
	for k, v := range params {
		message = strings.ReplaceAll(message, "${"+k+"}", v)
		message = strings.ReplaceAll(message, "{{"+k+"}}", v)
	}

	// Build headers
	headers := make(http.Header)
	if spec.Server != nil {
		for k, v := range spec.Server.Headers {
			headers.Set(k, v)
		}
	}
	for k, v := range action.Headers {
		headers.Set(k, v)
	}

	// Inject auth headers
	auth := spec.ResolveActionAuth(action)
	if auth != nil {
		wsApplyAuth(headers, auth)
	}

	// Connect
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return nil, fmt.Errorf("websocket connect failed: %w", err)
	}
	defer conn.Close()

	// Send message if present
	if message != "" {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
			return nil, fmt.Errorf("websocket send failed: %w", err)
		}
	}

	// Collect responses
	waitDuration := action.WaitDuration()
	maxCollect := action.Collect
	if maxCollect <= 0 {
		maxCollect = 1 // Default: collect 1 message
	}

	var messages []json.RawMessage
	deadline := time.After(waitDuration)

	for len(messages) < maxCollect {
		select {
		case <-ctx.Done():
			goto done
		case <-deadline:
			goto done
		default:
		}

		conn.SetReadDeadline(time.Now().Add(waitDuration))
		_, msg, readErr := conn.ReadMessage()
		if readErr != nil {
			if websocket.IsCloseError(readErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				break
			}
			if len(messages) > 0 {
				// We got some messages; treat timeout as normal end
				break
			}
			return nil, fmt.Errorf("websocket read failed: %w", readErr)
		}

		// Try to parse as JSON, otherwise wrap as a JSON string
		var raw json.RawMessage
		if json.Valid(msg) {
			raw = msg
		} else {
			raw, _ = json.Marshal(string(msg))
		}
		messages = append(messages, raw)
	}

done:
	// Format output
	var result []byte
	if len(messages) == 1 {
		result = messages[0]
	} else if len(messages) > 1 {
		result, _ = json.Marshal(messages)
	} else {
		result = []byte("[]")
	}

	// Apply output transforms
	transformed, err := applyActionTransform(result, action)
	if err != nil {
		return result, nil // Return untransformed on error
	}

	return transformed, nil
}

// wsApplyAuth injects authentication credentials into WebSocket handshake headers.
func wsApplyAuth(headers http.Header, auth *models.Auth) {
	if auth.Header == "" {
		return
	}
	resolved := auth.Header
	for _, envName := range auth.Env {
		val := os.Getenv(envName)
		resolved = strings.ReplaceAll(resolved, "${"+envName+"}", val)
	}
	if idx := strings.Index(resolved, ": "); idx > 0 {
		headers.Set(resolved[:idx], resolved[idx+2:])
	}
}
