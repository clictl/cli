// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package permissions

import (
	"fmt"
	"sync"
	"time"
)

const defaultTTL = 5 * time.Minute

// cacheEntry stores a single permission check result with expiration.
type cacheEntry struct {
	allowed    bool
	canRequest bool
	reason     string
	expiresAt  time.Time
}

// cache is a thread-safe in-memory cache for permission check results.
type cache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	ttl     time.Duration
}

// newCache creates a new permission cache with the default TTL.
func newCache() *cache {
	return &cache{
		entries: make(map[string]cacheEntry),
		ttl:     defaultTTL,
	}
}

// cacheKey builds a lookup key from workspace, tool, and action.
func cacheKey(workspace, tool, action string) string {
	return fmt.Sprintf("%s:%s:%s", workspace, tool, action)
}

// get retrieves a cached permission result. Returns false for ok if the entry
// is missing or expired.
func (c *cache) get(workspace, tool, action string) (allowed bool, canRequest bool, reason string, ok bool) {
	key := cacheKey(workspace, tool, action)

	c.mu.RLock()
	entry, found := c.entries[key]
	c.mu.RUnlock()

	if !found || time.Now().After(entry.expiresAt) {
		return false, false, "", false
	}
	return entry.allowed, entry.canRequest, entry.reason, true
}

// set stores a permission result in the cache.
func (c *cache) set(workspace, tool, action string, allowed, canRequest bool, reason string) {
	key := cacheKey(workspace, tool, action)

	c.mu.Lock()
	c.entries[key] = cacheEntry{
		allowed:    allowed,
		canRequest: canRequest,
		reason:     reason,
		expiresAt:  time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}
