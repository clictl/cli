// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// SSEEvent represents a single Server-Sent Event parsed from a text/event-stream body.
type SSEEvent struct {
	Event string // event type (from "event:" line)
	Data  string // event data (from "data:" line(s), joined with newlines)
	ID    string // event ID (from "id:" line)
}

// ParseSSE reads a text/event-stream response body and yields SSEEvents on
// the returned channel. The channel is closed when the reader is exhausted or
// an error occurs. If the caller's done channel is closed, parsing stops early.
func ParseSSE(r io.Reader, done <-chan struct{}) <-chan SSEEvent {
	ch := make(chan SSEEvent, 16)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		// Allow lines up to 1 MiB to handle large data payloads.
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)

		var current SSEEvent
		var dataLines []string

		for scanner.Scan() {
			select {
			case <-done:
				return
			default:
			}

			line := scanner.Text()

			// An empty line dispatches the current event.
			if line == "" {
				if len(dataLines) > 0 {
					current.Data = strings.Join(dataLines, "\n")
					select {
					case ch <- current:
					case <-done:
						return
					}
				}
				current = SSEEvent{}
				dataLines = nil
				continue
			}

			// Lines starting with ':' are comments, ignore them.
			if strings.HasPrefix(line, ":") {
				continue
			}

			field, value := parseSSEField(line)
			switch field {
			case "event":
				current.Event = value
			case "data":
				dataLines = append(dataLines, value)
			case "id":
				current.ID = value
			case "retry":
				// Retry is intentionally ignored; the caller can implement reconnect logic.
			}
		}

		// Flush any trailing event without a final blank line.
		if len(dataLines) > 0 {
			current.Data = strings.Join(dataLines, "\n")
			select {
			case ch <- current:
			case <-done:
			}
		}
	}()
	return ch
}

// ParseSSESlice reads all SSE events from r into a slice. This is a convenience
// wrapper for tests and callers that do not need streaming.
func ParseSSESlice(r io.Reader) ([]SSEEvent, error) {
	done := make(chan struct{})
	defer close(done)

	var events []SSEEvent
	for ev := range ParseSSE(r, done) {
		events = append(events, ev)
	}
	return events, nil
}

// parseSSEField splits an SSE line into field name and value.
// Per the spec, the first colon separates field from value. If there is no
// colon, the entire line is the field name with an empty value.
func parseSSEField(line string) (field, value string) {
	idx := strings.IndexByte(line, ':')
	if idx == -1 {
		return line, ""
	}
	field = line[:idx]
	value = line[idx+1:]
	// Strip a single leading space from the value, per SSE spec.
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	return field, value
}

// formatSSEError formats an error from SSE parsing for display.
func formatSSEError(event SSEEvent) error {
	if event.Event == "error" {
		return fmt.Errorf("SSE error event: %s", event.Data)
	}
	return nil
}
