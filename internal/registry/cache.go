// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	specSubdir   = "specs"
	etagFile     = "etags.json"
	softTTL      = 1 * time.Hour
)

// Cache provides local file-based caching for tool specs with ETag support.
type Cache struct {
	dir  string
	mu   sync.Mutex
}

// etagEntry stores both the ETag value and the time the spec was last fetched.
type etagEntry struct {
	ETag      string    `json:"etag"`
	FetchedAt time.Time `json:"fetched_at"`
}

// NewCache creates a new Cache rooted at the given directory.
func NewCache(dir string) *Cache {
	return &Cache{dir: dir}
}

// ComputeContentHash returns the SHA256 hex digest of the given data.
func ComputeContentHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// specDir returns the path to the specs subdirectory.
func (c *Cache) specDir() string {
	return filepath.Join(c.dir, specSubdir)
}

// specPath returns the file path for a named spec.
func (c *Cache) specPath(name string) string {
	return filepath.Join(c.specDir(), name+".yaml")
}

// etagPath returns the path to the etags.json file.
func (c *Cache) etagPath() string {
	return filepath.Join(c.dir, etagFile)
}

// loadETags reads the etag map from disk.
func (c *Cache) loadETags() (map[string]etagEntry, error) {
	data, err := os.ReadFile(c.etagPath())
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]etagEntry), nil
		}
		return nil, err
	}
	var entries map[string]etagEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return make(map[string]etagEntry), nil
	}
	return entries, nil
}

// saveETags writes the etag map to disk.
func (c *Cache) saveETags(entries map[string]etagEntry) error {
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.etagPath(), data, 0o600)
}

// Get retrieves a cached spec by name. Returns the YAML bytes, the stored ETag,
// and whether the cache entry is still fresh (within soft TTL).
// Returns nil bytes if no cached entry exists.
func (c *Cache) Get(name string) (data []byte, etag string, fresh bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err = os.ReadFile(c.specPath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", false, nil
		}
		return nil, "", false, fmt.Errorf("reading cached spec %q: %w", name, err)
	}

	entries, err := c.loadETags()
	if err != nil {
		return data, "", false, nil
	}

	entry, ok := entries[name]
	if !ok {
		return data, "", false, nil
	}

	isFresh := time.Since(entry.FetchedAt) < softTTL
	return data, entry.ETag, isFresh, nil
}

// Put stores a spec and its ETag in the cache.
func (c *Cache) Put(name string, data []byte, etag string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.MkdirAll(c.specDir(), 0o700); err != nil {
		return fmt.Errorf("creating cache spec dir: %w", err)
	}

	if err := os.WriteFile(c.specPath(name), data, 0o600); err != nil {
		return fmt.Errorf("writing cached spec %q: %w", name, err)
	}

	entries, err := c.loadETags()
	if err != nil {
		entries = make(map[string]etagEntry)
	}
	entries[name] = etagEntry{
		ETag:      etag,
		FetchedAt: time.Now(),
	}

	if err := c.saveETags(entries); err != nil {
		return fmt.Errorf("saving etag for %q: %w", name, err)
	}

	return nil
}
