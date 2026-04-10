// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package telemetry

import (
	"bytes"
	"encoding/json"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/clictl/cli/internal/config"
)

const telemetryEndpoint = "https://api.clictl.dev/telemetry/events"

// Event represents a single telemetry event. All fields are anonymous;
// no IP, username, or workspace data is included.
type Event struct {
	Event      string `json:"event"`
	Tool       string `json:"tool,omitempty"`
	Action     string `json:"action,omitempty"`
	Version    string `json:"version,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	Category   string `json:"category,omitempty"`
	Success    *bool  `json:"success,omitempty"`
	CLIVersion string `json:"cli_version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Timestamp  string `json:"timestamp"`
}

var (
	enabled    = true
	queue      = &eventQueue{}
	cliVersion = "dev"
	initOnce   sync.Once
)

// Init reads the telemetry preference from config and sets the enabled flag.
// It also captures the CLI version for stamping on events.
func Init(cfg *config.Config, version string) {
	initOnce.Do(func() {
		cliVersion = version
		if cfg != nil && !cfg.Telemetry {
			enabled = false
		}
	})
}

// Enabled reports whether telemetry collection is active.
func Enabled() bool {
	return enabled
}

// Track enqueues an event if telemetry is enabled.
// This never blocks the caller; events are buffered in memory.
func Track(e Event) {
	if !enabled {
		return
	}

	// Fill in common fields if the caller left them empty.
	if e.CLIVersion == "" {
		e.CLIVersion = cliVersion
	}
	if e.OS == "" {
		e.OS = runtime.GOOS
	}
	if e.Arch == "" {
		e.Arch = runtime.GOARCH
	}
	if e.Timestamp == "" {
		e.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	queue.Enqueue(e)
}

// Flush sends all queued events to the telemetry API in a single batch.
// Failures are silently ignored. The HTTP POST has a 1-second timeout
// and no retries.
func Flush() {
	events := queue.Drain()
	if len(events) == 0 {
		return
	}

	body, err := json.Marshal(events)
	if err != nil {
		return
	}

	client := &http.Client{Timeout: 1 * time.Second}
	req, err := http.NewRequest(http.MethodPost, telemetryEndpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// --- Helper functions for common events ---

// TrackInstall records an installed event.
func TrackInstall(toolName, version, protocol, category string) {
	Track(Event{
		Event:    "installed",
		Tool:     toolName,
		Version:  version,
		Protocol: protocol,
		Category: category,
	})
}

// TrackRun records a run event with success or failure.
func TrackRun(toolName, action string, success bool) {
	Track(Event{
		Event:   "run",
		Tool:    toolName,
		Action:  action,
		Success: &success,
	})
}

// TrackUninstall records an uninstalled event.
func TrackUninstall(toolName string) {
	Track(Event{
		Event: "uninstalled",
		Tool:  toolName,
	})
}

// TrackVersionCheck records a version_check event.
func TrackVersionCheck() {
	Track(Event{
		Event: "version_check",
	})
}
