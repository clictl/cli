// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"strings"
	"testing"
)

func TestParseSSESlice_BasicEvent(t *testing.T) {
	input := "event: message\ndata: hello world\n\n"
	events, err := ParseSSESlice(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Event != "message" {
		t.Errorf("expected event 'message', got %q", events[0].Event)
	}
	if events[0].Data != "hello world" {
		t.Errorf("expected data 'hello world', got %q", events[0].Data)
	}
}

func TestParseSSESlice_MultipleDataLines(t *testing.T) {
	input := "data: line1\ndata: line2\ndata: line3\n\n"
	events, err := ParseSSESlice(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != "line1\nline2\nline3" {
		t.Errorf("expected multiline data, got %q", events[0].Data)
	}
}

func TestParseSSESlice_WithIDAndComment(t *testing.T) {
	input := ": this is a comment\nevent: update\ndata: payload\nid: 42\n\n"
	events, err := ParseSSESlice(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ID != "42" {
		t.Errorf("expected id '42', got %q", events[0].ID)
	}
	if events[0].Event != "update" {
		t.Errorf("expected event 'update', got %q", events[0].Event)
	}
}

func TestParseSSESlice_MultipleEvents(t *testing.T) {
	input := "data: first\n\ndata: second\n\n"
	events, err := ParseSSESlice(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Data != "first" {
		t.Errorf("expected 'first', got %q", events[0].Data)
	}
	if events[1].Data != "second" {
		t.Errorf("expected 'second', got %q", events[1].Data)
	}
}

func TestParseSSESlice_EmptyDataLines(t *testing.T) {
	// An event with data fields but no actual content should still dispatch.
	input := "data:\n\n"
	events, err := ParseSSESlice(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != "" {
		t.Errorf("expected empty data, got %q", events[0].Data)
	}
}

func TestParseSSESlice_TrailingEventWithoutBlankLine(t *testing.T) {
	input := "data: trailing"
	events, err := ParseSSESlice(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != "trailing" {
		t.Errorf("expected 'trailing', got %q", events[0].Data)
	}
}

func TestParseSSEField_NoColon(t *testing.T) {
	field, value := parseSSEField("fieldonly")
	if field != "fieldonly" {
		t.Errorf("expected field 'fieldonly', got %q", field)
	}
	if value != "" {
		t.Errorf("expected empty value, got %q", value)
	}
}

func TestParseSSEField_LeadingSpace(t *testing.T) {
	field, value := parseSSEField("data: hello")
	if field != "data" {
		t.Errorf("expected field 'data', got %q", field)
	}
	if value != "hello" {
		t.Errorf("expected value 'hello', got %q", value)
	}
}

func TestParseSSEField_NoLeadingSpace(t *testing.T) {
	field, value := parseSSEField("data:hello")
	if field != "data" {
		t.Errorf("expected field 'data', got %q", field)
	}
	if value != "hello" {
		t.Errorf("expected value 'hello', got %q", value)
	}
}

func TestFormatSSEError(t *testing.T) {
	ev := SSEEvent{Event: "error", Data: "session expired"}
	err := formatSSEError(ev)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "session expired") {
		t.Errorf("expected error to contain 'session expired', got %q", err.Error())
	}

	ev2 := SSEEvent{Event: "message", Data: "ok"}
	if formatSSEError(ev2) != nil {
		t.Error("expected nil for non-error event")
	}
}

func TestParseSSESlice_CommentsIgnored(t *testing.T) {
	input := ": keep-alive\n: ping\ndata: actual-data\n\n"
	events, err := ParseSSESlice(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != "actual-data" {
		t.Errorf("expected 'actual-data', got %q", events[0].Data)
	}
}

func TestParseSSESlice_RetryFieldIgnored(t *testing.T) {
	input := "retry: 3000\ndata: payload\n\n"
	events, err := ParseSSESlice(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != "payload" {
		t.Errorf("expected 'payload', got %q", events[0].Data)
	}
}

func TestParseSSESlice_JSONDataPreserved(t *testing.T) {
	jsonPayload := `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`
	input := "event: message\ndata: " + jsonPayload + "\n\n"
	events, err := ParseSSESlice(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != jsonPayload {
		t.Errorf("JSON data not preserved.\ngot:  %q\nwant: %q", events[0].Data, jsonPayload)
	}
}

func TestParseSSESlice_MixedEventsAndNotifications(t *testing.T) {
	input := "event: notification\ndata: {\"method\":\"tools/changed\"}\n\n" +
		"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n" +
		"event: error\ndata: something went wrong\n\n"

	events, err := ParseSSESlice(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	expected := []struct {
		event string
		data  string
	}{
		{"notification", `{"method":"tools/changed"}`},
		{"message", `{"jsonrpc":"2.0","id":1,"result":{}}`},
		{"error", "something went wrong"},
	}

	for i, want := range expected {
		if events[i].Event != want.event {
			t.Errorf("event[%d].Event = %q, want %q", i, events[i].Event, want.event)
		}
		if events[i].Data != want.data {
			t.Errorf("event[%d].Data = %q, want %q", i, events[i].Data, want.data)
		}
	}
}

func TestParseSSESlice_EmptyStream(t *testing.T) {
	events, err := ParseSSESlice(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events from empty stream, got %d", len(events))
	}
}

func TestParseSSESlice_OnlyComments(t *testing.T) {
	input := ": comment 1\n: comment 2\n"
	events, err := ParseSSESlice(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events from comment-only stream, got %d", len(events))
	}
}

func TestParseSSEField_EmptyValue(t *testing.T) {
	field, value := parseSSEField("event:")
	if field != "event" {
		t.Errorf("expected field 'event', got %q", field)
	}
	if value != "" {
		t.Errorf("expected empty value, got %q", value)
	}
}

func TestParseSSESlice_MultilineJSONData(t *testing.T) {
	// Multi-line data is joined with newlines
	input := "data: {\"a\":\n" +
		"data: \"b\"}\n\n"
	events, err := ParseSSESlice(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != "{\"a\":\n\"b\"}" {
		t.Errorf("expected multiline data, got %q", events[0].Data)
	}
}
