// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/clictl/cli/internal/config"
)

var privateSpecBucket = []byte("private_specs")

// PrivateSpecCache caches specs fetched from private repos in a local bbolt database.
type PrivateSpecCache struct {
	db *bolt.DB
}

type cachedSpec struct {
	YAML       string `json:"yaml"`
	FetchedAt  string `json:"fetched_at"`
	SourceID   string `json:"source_id"`
	SpecCount  int    `json:"spec_count"`
	LastSynced string `json:"last_synced"`
}

// OpenPrivateSpecCache opens or creates the bbolt database for private spec caching.
func OpenPrivateSpecCache() (*PrivateSpecCache, error) {
	dir := filepath.Join(config.BaseDir(), "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dir, "private-specs.db")
	db, err := bolt.Open(dbPath, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(privateSpecBucket)
		return err
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &PrivateSpecCache{db: db}, nil
}

// Get retrieves a cached spec YAML by tool name.
// Returns empty string if not cached or if the cache is stale
// (spec_count or last_synced changed since caching).
func (c *PrivateSpecCache) Get(name, sourceID string, currentSpecCount int, currentLastSynced string) (string, bool) {
	var result string
	var found bool
	c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(privateSpecBucket)
		data := b.Get([]byte(name))
		if data == nil {
			return nil
		}
		var entry cachedSpec
		if err := json.Unmarshal(data, &entry); err != nil {
			return nil
		}
		// Invalidate if source metadata changed
		if entry.SourceID != sourceID {
			return nil
		}
		if entry.SpecCount != currentSpecCount || entry.LastSynced != currentLastSynced {
			return nil
		}
		result = entry.YAML
		found = true
		return nil
	})
	return result, found
}

// Put stores a spec YAML in the cache.
func (c *PrivateSpecCache) Put(name, yamlContent, sourceID string, specCount int, lastSynced string) error {
	entry := cachedSpec{
		YAML:       yamlContent,
		FetchedAt:  time.Now().UTC().Format(time.RFC3339),
		SourceID:   sourceID,
		SpecCount:  specCount,
		LastSynced: lastSynced,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(privateSpecBucket)
		return b.Put([]byte(name), data)
	})
}

// Close closes the bbolt database.
func (c *PrivateSpecCache) Close() error {
	return c.db.Close()
}
