// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/clictl/cli/internal/models"
)

// DefaultKeepAlive is the default duration to keep idle MCP connections alive.
const DefaultKeepAlive = 60 * time.Second

// Pool manages reusable MCP client connections keyed by spec name.
type Pool struct {
	mu      sync.Mutex
	clients map[string]*poolEntry
	done    chan struct{} // closed to stop background reaper
}

type poolEntry struct {
	client    *Client
	mu        sync.Mutex // serializes send-receive cycles per client
	lastUsed  time.Time
	keepAlive time.Duration
}

// NewPool creates a new connection pool and starts a background reaper
// that cleans up idle connections every 30 seconds.
func NewPool() *Pool {
	p := &Pool{
		clients: make(map[string]*poolEntry),
		done:    make(chan struct{}),
	}
	go p.reapLoop()
	return p
}

// reapLoop runs Reap every 30 seconds until the pool is closed.
func (p *Pool) reapLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.Reap()
		case <-p.done:
			return
		}
	}
}

// Get returns an existing client for the spec or creates a new one.
// Cached clients are health-checked with a 1s ping before reuse.
func (p *Pool) Get(ctx context.Context, spec *models.ToolSpec) (*Client, error) {
	p.mu.Lock()
	entry, ok := p.clients[spec.Name]
	if ok {
		entry.lastUsed = time.Now()
		p.mu.Unlock()

		// Health check: ping with 1s timeout
		pingCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		defer cancel()
		if err := entry.client.Ping(pingCtx); err != nil {
			// Stale connection - remove from pool and create new
			p.mu.Lock()
			if current, exists := p.clients[spec.Name]; exists && current == entry {
				delete(p.clients, spec.Name)
			}
			p.mu.Unlock()
			entry.client.Close()
		} else {
			return entry.client, nil
		}
	} else {
		p.mu.Unlock()
	}

	// Create new client outside the lock
	client, err := NewClient(spec)
	if err != nil {
		return nil, fmt.Errorf("creating MCP client for %s: %w", spec.Name, err)
	}

	if err := client.Initialize(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("initializing MCP client for %s: %w", spec.Name, err)
	}

	keepAlive := DefaultKeepAlive
	if spec.Server != nil {
		keepAlive = spec.Server.KeepAliveDuration()
	}

	p.mu.Lock()
	// Double-check another goroutine didn't create one while we were connecting
	if existing, ok := p.clients[spec.Name]; ok {
		p.mu.Unlock()
		client.Close()
		existing.lastUsed = time.Now()
		return existing.client, nil
	}
	p.clients[spec.Name] = &poolEntry{
		client:    client,
		lastUsed:  time.Now(),
		keepAlive: keepAlive,
	}
	p.mu.Unlock()

	return client, nil
}

// Release marks a client as available for reuse (updates last-used time).
func (p *Pool) Release(specName string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.clients[specName]; ok {
		entry.lastUsed = time.Now()
	}
}

// Reap closes and removes idle clients that have exceeded their keep-alive.
func (p *Pool) Reap() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for name, entry := range p.clients {
		if now.Sub(entry.lastUsed) > entry.keepAlive {
			entry.client.Close()
			delete(p.clients, name)
		}
	}
}

// CloseAll closes all pooled clients and stops the background reaper.
func (p *Pool) CloseAll() {
	// Signal the reaper to stop
	select {
	case <-p.done:
		// already closed
	default:
		close(p.done)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for name, entry := range p.clients {
		entry.client.Close()
		delete(p.clients, name)
	}
}

// Size returns the number of pooled connections.
func (p *Pool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.clients)
}

// Lock acquires the per-client mutex for the named entry, serializing
// send-receive cycles. Returns false if the entry does not exist.
func (p *Pool) Lock(specName string) bool {
	p.mu.Lock()
	entry, ok := p.clients[specName]
	p.mu.Unlock()
	if !ok {
		return false
	}
	entry.mu.Lock()
	return true
}

// Unlock releases the per-client mutex for the named entry.
func (p *Pool) Unlock(specName string) {
	p.mu.Lock()
	entry, ok := p.clients[specName]
	p.mu.Unlock()
	if ok {
		entry.mu.Unlock()
	}
}
