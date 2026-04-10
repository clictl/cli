// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package telemetry provides anonymous, fire-and-forget usage tracking.
// It collects tool install/run/uninstall events and CLI version checks,
// batching them in-memory before flushing to the telemetry API.
// No IP addresses, usernames, or workspace identifiers are collected.
package telemetry

import "sync"

const maxQueueSize = 1000

// eventQueue is a thread-safe, bounded in-memory event buffer.
type eventQueue struct {
	mu     sync.Mutex
	events []Event
}

// Enqueue adds an event to the queue. If the queue is at capacity,
// the oldest event is dropped to make room.
func (q *eventQueue) Enqueue(e Event) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.events) >= maxQueueSize {
		// Drop the oldest event
		q.events = q.events[1:]
	}
	q.events = append(q.events, e)
}

// Drain returns all queued events and resets the queue to empty.
func (q *eventQueue) Drain() []Event {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.events) == 0 {
		return nil
	}

	out := q.events
	q.events = nil
	return out
}

// Len returns the current number of queued events.
func (q *eventQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.events)
}
