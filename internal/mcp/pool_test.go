// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"context"
	"testing"
	"time"
)

// noopTransport satisfies the Transport interface without doing anything.
type noopTransport struct{}

func (noopTransport) Send(context.Context, *Request) (*Response, error) { return nil, nil }
func (noopTransport) Notify(context.Context, *Notification) error       { return nil }
func (noopTransport) Close() error                                      { return nil }

// newTestClient returns a Client with a no-op transport so Close() is safe.
func newTestClient() *Client {
	return &Client{transport: noopTransport{}}
}

func TestNewPool_Empty(t *testing.T) {
	p := NewPool()
	if p == nil {
		t.Fatal("expected non-nil pool")
	}
	if p.Size() != 0 {
		t.Errorf("expected empty pool, got size %d", p.Size())
	}
}

func TestPool_Size(t *testing.T) {
	p := NewPool()

	p.mu.Lock()
	p.clients["a"] = &poolEntry{
		client:    newTestClient(),
		lastUsed:  time.Now(),
		keepAlive: DefaultKeepAlive,
	}
	p.clients["b"] = &poolEntry{
		client:    newTestClient(),
		lastUsed:  time.Now(),
		keepAlive: DefaultKeepAlive,
	}
	p.mu.Unlock()

	if got := p.Size(); got != 2 {
		t.Errorf("expected size 2, got %d", got)
	}
}

func TestPool_CloseAll(t *testing.T) {
	p := NewPool()

	p.mu.Lock()
	p.clients["a"] = &poolEntry{
		client:    newTestClient(),
		lastUsed:  time.Now(),
		keepAlive: DefaultKeepAlive,
	}
	p.clients["b"] = &poolEntry{
		client:    newTestClient(),
		lastUsed:  time.Now(),
		keepAlive: DefaultKeepAlive,
	}
	p.mu.Unlock()

	p.CloseAll()

	if got := p.Size(); got != 0 {
		t.Errorf("expected size 0 after CloseAll, got %d", got)
	}
}

func TestPool_Reap_RemovesExpired(t *testing.T) {
	p := NewPool()

	p.mu.Lock()
	// "expired" entry: lastUsed well in the past with a tiny keep-alive
	p.clients["expired"] = &poolEntry{
		client:    newTestClient(),
		lastUsed:  time.Now().Add(-2 * time.Second),
		keepAlive: 1 * time.Millisecond,
	}
	// "fresh" entry: should survive reaping
	p.clients["fresh"] = &poolEntry{
		client:    newTestClient(),
		lastUsed:  time.Now(),
		keepAlive: 10 * time.Minute,
	}
	p.mu.Unlock()

	p.Reap()

	if got := p.Size(); got != 1 {
		t.Errorf("expected size 1 after Reap, got %d", got)
	}

	p.mu.Lock()
	_, hasExpired := p.clients["expired"]
	_, hasFresh := p.clients["fresh"]
	p.mu.Unlock()

	if hasExpired {
		t.Error("expected expired entry to be reaped")
	}
	if !hasFresh {
		t.Error("expected fresh entry to survive reaping")
	}
}

func TestPool_Release_UpdatesLastUsed(t *testing.T) {
	p := NewPool()

	past := time.Now().Add(-5 * time.Minute)
	p.mu.Lock()
	p.clients["a"] = &poolEntry{
		client:    newTestClient(),
		lastUsed:  past,
		keepAlive: DefaultKeepAlive,
	}
	p.mu.Unlock()

	p.Release("a")

	p.mu.Lock()
	entry := p.clients["a"]
	p.mu.Unlock()

	if !entry.lastUsed.After(past) {
		t.Error("expected Release to update lastUsed to a more recent time")
	}
}
