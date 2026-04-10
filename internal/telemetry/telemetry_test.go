// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package telemetry

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"
)

func resetState() {
	enabled = true
	queue = &eventQueue{}
	cliVersion = "v1.0.0-test"
	initOnce = sync.Once{}
}

func resetEnterpriseState() {
	enterpriseEnabled = false
	enterpriseAPIURL = ""
	enterpriseToken = ""
	enterpriseWS = ""
	enterpriseQueue = &invocationQueue{}
	enterpriseOnce = sync.Once{}
	enterpriseDone = nil
}

func TestEnqueueDrain(t *testing.T) {
	q := &eventQueue{}

	if got := q.Drain(); got != nil {
		t.Fatalf("expected nil from empty drain, got %d events", len(got))
	}

	q.Enqueue(Event{Event: "a"})
	q.Enqueue(Event{Event: "b"})
	q.Enqueue(Event{Event: "c"})

	if q.Len() != 3 {
		t.Fatalf("expected len 3, got %d", q.Len())
	}

	events := q.Drain()
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Event != "a" || events[1].Event != "b" || events[2].Event != "c" {
		t.Fatalf("unexpected event order: %v", events)
	}

	// After drain, queue should be empty
	if q.Len() != 0 {
		t.Fatalf("expected len 0 after drain, got %d", q.Len())
	}
	if got := q.Drain(); got != nil {
		t.Fatalf("expected nil after second drain, got %d events", len(got))
	}
}

func TestQueueCapAt1000(t *testing.T) {
	q := &eventQueue{}

	for i := 0; i < 1200; i++ {
		q.Enqueue(Event{Event: "event"})
	}

	if q.Len() != maxQueueSize {
		t.Fatalf("expected queue capped at %d, got %d", maxQueueSize, q.Len())
	}

	events := q.Drain()
	if len(events) != maxQueueSize {
		t.Fatalf("expected %d events from drain, got %d", maxQueueSize, len(events))
	}
}

func TestQueueDropsOldest(t *testing.T) {
	q := &eventQueue{}

	// Fill to capacity
	for i := 0; i < maxQueueSize; i++ {
		q.Enqueue(Event{Event: "old", Tool: "old"})
	}

	// Add one more - should drop the oldest
	q.Enqueue(Event{Event: "new", Tool: "new"})

	events := q.Drain()
	if len(events) != maxQueueSize {
		t.Fatalf("expected %d events, got %d", maxQueueSize, len(events))
	}

	// Last event should be the new one
	last := events[len(events)-1]
	if last.Tool != "new" {
		t.Fatalf("expected last event tool=new, got %q", last.Tool)
	}

	// First event should be old (the second oldest, since the first was dropped)
	first := events[0]
	if first.Tool != "old" {
		t.Fatalf("expected first event tool=old, got %q", first.Tool)
	}
}

func TestTrackWhenDisabled(t *testing.T) {
	resetState()
	enabled = false

	Track(Event{Event: "should.not.track"})

	events := queue.Drain()
	if events != nil {
		t.Fatalf("expected no events when disabled, got %d", len(events))
	}
}

func TestTrackWhenEnabled(t *testing.T) {
	resetState()
	enabled = true

	Track(Event{Event: "run", Tool: "github"})

	events := queue.Drain()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.Event != "run" {
		t.Fatalf("expected event=tool.run, got %q", e.Event)
	}
	if e.Tool != "github" {
		t.Fatalf("expected tool=github, got %q", e.Tool)
	}
	// Common fields should be filled in
	if e.CLIVersion != "v1.0.0-test" {
		t.Fatalf("expected cli_version=v1.0.0-test, got %q", e.CLIVersion)
	}
	if e.OS != runtime.GOOS {
		t.Fatalf("expected os=%s, got %q", runtime.GOOS, e.OS)
	}
	if e.Arch != runtime.GOARCH {
		t.Fatalf("expected arch=%s, got %q", runtime.GOARCH, e.Arch)
	}
	if e.Timestamp == "" {
		t.Fatal("expected timestamp to be set")
	}
	// Verify timestamp is valid RFC3339
	if _, err := time.Parse(time.RFC3339, e.Timestamp); err != nil {
		t.Fatalf("invalid timestamp format: %v", err)
	}
}

func TestEventSerialization(t *testing.T) {
	success := true
	e := Event{
		Event:      "run",
		Tool:       "stripe",
		Action:     "charges.list",
		Version:    "1.2.0",
		Protocol:   "rest",
		Category:   "payments",
		Success:    &success,
		CLIVersion: "v1.0.0",
		OS:         "darwin",
		Arch:       "arm64",
		Timestamp:  "2026-03-27T12:00:00Z",
	}

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Event != e.Event {
		t.Fatalf("event mismatch: %q vs %q", decoded.Event, e.Event)
	}
	if decoded.Tool != e.Tool {
		t.Fatalf("tool mismatch: %q vs %q", decoded.Tool, e.Tool)
	}
	if decoded.Action != e.Action {
		t.Fatalf("action mismatch: %q vs %q", decoded.Action, e.Action)
	}
	if decoded.Success == nil || *decoded.Success != true {
		t.Fatalf("expected success=true, got %v", decoded.Success)
	}
	if decoded.CLIVersion != "v1.0.0" {
		t.Fatalf("cli_version mismatch: %q", decoded.CLIVersion)
	}
	if decoded.OS != "darwin" {
		t.Fatalf("os mismatch: %q", decoded.OS)
	}
}

func TestEventOmitsEmptyFields(t *testing.T) {
	e := Event{
		Event:      "version_check",
		CLIVersion: "v1.0.0",
		OS:         "linux",
		Arch:       "amd64",
		Timestamp:  "2026-03-27T12:00:00Z",
	}

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// These fields should be omitted when empty
	for _, key := range []string{"tool", "action", "version", "protocol", "category", "success"} {
		if _, exists := raw[key]; exists {
			t.Errorf("expected %q to be omitted, but it was present", key)
		}
	}

	// These fields should always be present
	for _, key := range []string{"event", "cli_version", "os", "arch", "timestamp"} {
		if _, exists := raw[key]; !exists {
			t.Errorf("expected %q to be present, but it was missing", key)
		}
	}
}

func TestTrackInstall(t *testing.T) {
	resetState()

	TrackInstall("github", "2.0.0", "rest", "developer-tools")

	events := queue.Drain()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.Event != "installed" {
		t.Fatalf("expected event=tool.installed, got %q", e.Event)
	}
	if e.Tool != "github" {
		t.Fatalf("expected tool=github, got %q", e.Tool)
	}
	if e.Version != "2.0.0" {
		t.Fatalf("expected version=2.0.0, got %q", e.Version)
	}
	if e.Protocol != "rest" {
		t.Fatalf("expected protocol=rest, got %q", e.Protocol)
	}
	if e.Category != "developer-tools" {
		t.Fatalf("expected category=developer-tools, got %q", e.Category)
	}
	if e.OS != runtime.GOOS {
		t.Fatalf("expected os=%s, got %q", runtime.GOOS, e.OS)
	}
	if e.Arch != runtime.GOARCH {
		t.Fatalf("expected arch=%s, got %q", runtime.GOARCH, e.Arch)
	}
}

func TestTrackRun(t *testing.T) {
	resetState()

	TrackRun("stripe", "charges.list", true)

	events := queue.Drain()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.Event != "run" {
		t.Fatalf("expected event=tool.run, got %q", e.Event)
	}
	if e.Tool != "stripe" {
		t.Fatalf("expected tool=stripe, got %q", e.Tool)
	}
	if e.Action != "charges.list" {
		t.Fatalf("expected action=charges.list, got %q", e.Action)
	}
	if e.Success == nil || *e.Success != true {
		t.Fatalf("expected success=true, got %v", e.Success)
	}
}

func TestTrackRunFailure(t *testing.T) {
	resetState()

	TrackRun("stripe", "charges.list", false)

	events := queue.Drain()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.Success == nil || *e.Success != false {
		t.Fatalf("expected success=false, got %v", e.Success)
	}
}

func TestTrackUninstall(t *testing.T) {
	resetState()

	TrackUninstall("github")

	events := queue.Drain()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.Event != "uninstalled" {
		t.Fatalf("expected event=tool.uninstalled, got %q", e.Event)
	}
	if e.Tool != "github" {
		t.Fatalf("expected tool=github, got %q", e.Tool)
	}
}

func TestTrackVersionCheck(t *testing.T) {
	resetState()

	TrackVersionCheck()

	events := queue.Drain()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.Event != "version_check" {
		t.Fatalf("expected event=cli.version_check, got %q", e.Event)
	}
	if e.CLIVersion != "v1.0.0-test" {
		t.Fatalf("expected cli_version=v1.0.0-test, got %q", e.CLIVersion)
	}
	if e.OS != runtime.GOOS {
		t.Fatalf("expected os=%s, got %q", runtime.GOOS, e.OS)
	}
	if e.Arch != runtime.GOARCH {
		t.Fatalf("expected arch=%s, got %q", runtime.GOARCH, e.Arch)
	}
}

func TestQueueConcurrentAccess(t *testing.T) {
	q := &eventQueue{}
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q.Enqueue(Event{Event: "concurrent"})
		}()
	}
	wg.Wait()

	events := q.Drain()
	if len(events) != 100 {
		t.Fatalf("expected 100 events after concurrent enqueue, got %d", len(events))
	}
}

// ---------------------------------------------------------------------------
// Enterprise logging tests (merged from enterprise_log_test.go)
// ---------------------------------------------------------------------------

func TestInvocationQueueBounded(t *testing.T) {
	q := &invocationQueue{}

	// Fill beyond the cap.
	for i := 0; i < maxEnterpriseQueue+100; i++ {
		q.Enqueue(InvocationRecord{
			ToolName: "overflow-tool",
			Status:   "success",
		})
	}

	records := q.Drain()
	if len(records) != maxEnterpriseQueue {
		t.Fatalf("expected queue capped at %d, got %d", maxEnterpriseQueue, len(records))
	}

	// Oldest records should be dropped; the last record should still be present.
	last := records[len(records)-1]
	if last.ToolName != "overflow-tool" {
		t.Errorf("expected last record tool_name=overflow-tool, got %q", last.ToolName)
	}

	// Queue should be empty after drain.
	if drained := q.Drain(); drained != nil {
		t.Fatalf("expected nil after drain, got %d records", len(drained))
	}
}

func TestFlushEnterprise(t *testing.T) {
	resetEnterpriseState()

	// Set up a test server that captures the ingested records.
	var received []InvocationRecord
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var payload struct {
			Records []InvocationRecord `json:"records"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		received = append(received, payload.Records...)
		mu.Unlock()

		// Verify auth header is present.
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"accepted": ` + string(rune('0'+len(payload.Records))) + `}`))
	}))
	defer srv.Close()

	// Configure enterprise state directly.
	enterpriseEnabled = true
	enterpriseAPIURL = srv.URL
	enterpriseToken = "test-token"
	enterpriseWS = "test-workspace"

	// Enqueue some records.
	latency := 42
	enterpriseQueue.Enqueue(InvocationRecord{
		ToolName:  "anthropic/github",
		Action:    "list_repos",
		Status:    "success",
		LatencyMs: &latency,
	})
	enterpriseQueue.Enqueue(InvocationRecord{
		ToolName: "anthropic/slack",
		Status:   "error",
		Error:    "connection timeout",
	})

	// Flush and verify records were sent.
	FlushEnterprise()

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 2 {
		t.Fatalf("expected 2 flushed records, got %d", len(received))
	}
	if received[0].ToolName != "anthropic/github" {
		t.Errorf("expected first record tool=anthropic/github, got %q", received[0].ToolName)
	}
	if received[0].Action != "list_repos" {
		t.Errorf("expected first record action=list_repos, got %q", received[0].Action)
	}
	if received[0].LatencyMs == nil || *received[0].LatencyMs != 42 {
		t.Errorf("expected first record latency=42, got %v", received[0].LatencyMs)
	}
	if received[1].Status != "error" {
		t.Errorf("expected second record status=error, got %q", received[1].Status)
	}
	if received[1].Error != "connection timeout" {
		t.Errorf("expected second record error=connection timeout, got %q", received[1].Error)
	}
}

func TestLoggingOptOut(t *testing.T) {
	resetEnterpriseState()

	// When enterprise is not enabled, TrackInvocation should be a no-op.
	enterpriseEnabled = false

	latency := 100
	TrackInvocation(InvocationRecord{
		ToolName:  "should-not-track",
		Status:    "success",
		LatencyMs: &latency,
	})

	records := enterpriseQueue.Drain()
	if records != nil {
		t.Fatalf("expected no records when enterprise logging is disabled, got %d", len(records))
	}
}

func TestFlushEmptyQueue(t *testing.T) {
	resetEnterpriseState()
	enterpriseEnabled = true
	enterpriseAPIURL = "http://localhost:0" // intentionally unreachable

	// Flushing an empty queue should be a safe no-op (no HTTP call made).
	FlushEnterprise()
	// If we get here without panic or hang, the test passes.
}

func TestInvocationQueueConcurrentAccess(t *testing.T) {
	q := &invocationQueue{}
	var wg sync.WaitGroup

	// Concurrent writes from many goroutines.
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			q.Enqueue(InvocationRecord{
				ToolName: "concurrent-tool",
				Status:   "success",
			})
		}(i)
	}
	wg.Wait()

	records := q.Drain()
	if len(records) != 200 {
		t.Fatalf("expected 200 records after concurrent enqueue, got %d", len(records))
	}
}

func TestInvocationRecordSerialization(t *testing.T) {
	latency := 150
	responseSize := 4096
	rec := InvocationRecord{
		ToolName:     "anthropic/github",
		Action:       "create_issue",
		Client:       "clictl/v1.2.0",
		LatencyMs:    &latency,
		Status:       "success",
		ParamsHash:   "abc123def456",
		ResponseSize: &responseSize,
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded InvocationRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.ToolName != rec.ToolName {
		t.Errorf("tool_name: got %q, want %q", decoded.ToolName, rec.ToolName)
	}
	if decoded.Action != rec.Action {
		t.Errorf("action: got %q, want %q", decoded.Action, rec.Action)
	}
	if decoded.Client != rec.Client {
		t.Errorf("client: got %q, want %q", decoded.Client, rec.Client)
	}
	if decoded.LatencyMs == nil || *decoded.LatencyMs != 150 {
		t.Errorf("latency_ms: got %v, want 150", decoded.LatencyMs)
	}
	if decoded.ResponseSize == nil || *decoded.ResponseSize != 4096 {
		t.Errorf("response_size: got %v, want 4096", decoded.ResponseSize)
	}
}

func TestInvocationRecordOmitsEmptyFields(t *testing.T) {
	rec := InvocationRecord{
		ToolName: "minimal-tool",
		Status:   "success",
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map failed: %v", err)
	}

	// Optional fields should be omitted when zero/nil.
	for _, key := range []string{"action", "client", "latency_ms", "error", "params_hash", "response_size"} {
		if _, exists := raw[key]; exists {
			t.Errorf("expected %q to be omitted when empty, but it was present", key)
		}
	}

	// Required fields should always be present.
	for _, key := range []string{"tool_name", "status"} {
		if _, exists := raw[key]; !exists {
			t.Errorf("expected %q to be present, but it was missing", key)
		}
	}
}
