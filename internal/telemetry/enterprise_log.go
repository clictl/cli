// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/clictl/cli/internal/config"
)

// InvocationRecord is a single tool call log entry for enterprise logging.
type InvocationRecord struct {
	ToolName     string `json:"tool_name"`
	Action       string `json:"action,omitempty"`
	Client       string `json:"client,omitempty"`
	LatencyMs    *int   `json:"latency_ms,omitempty"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
	ParamsHash   string `json:"params_hash,omitempty"`
	ResponseSize *int   `json:"response_size,omitempty"`
}

const (
	maxEnterpriseQueue = 500
	flushInterval      = 30 * time.Second
)

var (
	enterpriseEnabled bool
	enterpriseAPIURL  string
	enterpriseToken   string
	enterpriseWS      string
	enterpriseQueue   = &invocationQueue{}
	enterpriseOnce    sync.Once
	enterpriseDone    chan struct{}
)

// InitEnterprise configures the enterprise log submitter.
// If the user is logged into an enterprise workspace with logging enabled,
// this starts a background flush loop.
func InitEnterprise(cfg *config.Config, cliVer string) {
	enterpriseOnce.Do(func() {
		if cfg == nil {
			return
		}
		// Check opt-out via logging: false in config.yaml
		if !cfg.LoggingEnabled() {
			return
		}
		// Check workspace
		token := config.ResolveAuthToken("", cfg)
		ws := cfg.Auth.ActiveWorkspace
		if token == "" || ws == "" {
			return
		}
		apiURL := config.ResolveAPIURL("", cfg)

		enterpriseEnabled = true
		enterpriseAPIURL = apiURL
		enterpriseToken = token
		enterpriseWS = ws
		enterpriseDone = make(chan struct{})

		go enterpriseFlushLoop()
	})
}

// EnterpriseEnabled reports whether enterprise log submission is active.
func EnterpriseEnabled() bool {
	return enterpriseEnabled
}

// TrackInvocation enqueues an invocation record for enterprise log submission.
// Non-blocking; drops oldest records when the queue is full.
func TrackInvocation(rec InvocationRecord) {
	if !enterpriseEnabled {
		return
	}
	enterpriseQueue.Enqueue(rec)
}

// FlushEnterprise sends all queued enterprise invocation records to the API.
func FlushEnterprise() {
	records := enterpriseQueue.Drain()
	if len(records) == 0 {
		return
	}

	body, err := json.Marshal(map[string]interface{}{
		"records": records,
	})
	if err != nil {
		return
	}

	u := fmt.Sprintf("%s/api/v1/workspaces/%s/logs/ingest/",
		enterpriseAPIURL, url.PathEscape(enterpriseWS))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+enterpriseToken)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// StopEnterprise stops the background flush loop and flushes remaining records.
func StopEnterprise() {
	if !enterpriseEnabled || enterpriseDone == nil {
		return
	}
	close(enterpriseDone)
	FlushEnterprise()
}

func enterpriseFlushLoop() {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			FlushEnterprise()
		case <-enterpriseDone:
			return
		}
	}
}

// invocationQueue is a bounded, thread-safe queue for InvocationRecord.
type invocationQueue struct {
	mu      sync.Mutex
	records []InvocationRecord
}

// Enqueue adds an audit event to the buffer for batch flush.
func (q *invocationQueue) Enqueue(r InvocationRecord) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.records) >= maxEnterpriseQueue {
		q.records = q.records[1:]
	}
	q.records = append(q.records, r)
}

// Drain flushes all buffered audit events to the API.
func (q *invocationQueue) Drain() []InvocationRecord {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.records) == 0 {
		return nil
	}
	out := q.records
	q.records = nil
	return out
}
