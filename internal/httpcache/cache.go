// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package httpcache provides an RFC 7234 compliant HTTP response cache backed
// by bbolt. It supports concurrent access from multiple CLI instances, automatic
// size-based eviction, and graceful recovery from database corruption.
//
// The cache stores response bodies keyed by request URL + method. Cache-Control,
// Expires, ETag, and Last-Modified headers are respected for freshness checks
// and conditional requests.
package httpcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	// bucketName is the bbolt bucket for cached entries.
	bucketName = []byte("responses")
)

// Entry is a cached HTTP response.
type Entry struct {
	URL          string            `json:"url"`
	Method       string            `json:"method"`
	StatusCode   int               `json:"status_code"`
	Headers      map[string]string `json:"headers"`
	Body         []byte            `json:"body"`
	StoredAt     time.Time         `json:"stored_at"`
	ExpiresAt    time.Time         `json:"expires_at"`
	ETag         string            `json:"etag"`
	LastModified string            `json:"last_modified"`
}

// IsExpired returns true if the entry has passed its expiration time.
func (e *Entry) IsExpired() bool {
	if e.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(e.ExpiresAt)
}

// HasValidator returns true if the entry has an ETag or Last-Modified for conditional requests.
func (e *Entry) HasValidator() bool {
	return e.ETag != "" || e.LastModified != ""
}

// Cache is an RFC 7234 HTTP response cache backed by bbolt.
type Cache struct {
	db      *bolt.DB
	maxSize int64 // max cache size in bytes, 0 = unlimited
}

// Options configures the cache.
type Options struct {
	// Dir is the directory for the cache database file.
	Dir string
	// MaxSizeMB is the maximum cache size in megabytes. 0 means unlimited.
	MaxSizeMB int
}

// Open creates or opens a response cache at the given directory.
// If the database is corrupted, it is removed and recreated.
func Open(opts Options) (*Cache, error) {
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache dir: %w", err)
	}

	dbPath := filepath.Join(opts.Dir, "responses.db")

	db, err := openDB(dbPath)
	if err != nil {
		// Corrupted database - remove and retry
		os.Remove(dbPath)
		os.Remove(dbPath + ".lock")
		db, err = openDB(dbPath)
		if err != nil {
			return nil, fmt.Errorf("opening cache database: %w", err)
		}
	}

	// Ensure bucket exists
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketName)
		return err
	}); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating cache bucket: %w", err)
	}

	return &Cache{
		db:      db,
		maxSize: int64(opts.MaxSizeMB) * 1024 * 1024,
	}, nil
}

// openDB opens the bbolt database with safe settings for multi-instance access.
func openDB(path string) (*bolt.DB, error) {
	return bolt.Open(path, 0o644, &bolt.Options{
		Timeout:      1 * time.Second,     // Wait up to 1s for file lock
		NoGrowSync:   false,               // Sync on grow for durability
		FreelistType: bolt.FreelistMapType, // Better for concurrent access patterns
	})
}

// Close closes the cache database.
func (c *Cache) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// cacheKey generates a unique key for a request URL and method.
func cacheKey(method, url string) string {
	h := sha256.Sum256([]byte(method + "\x00" + url))
	return hex.EncodeToString(h[:])
}

// Get retrieves a cached response for the given URL and method.
// Returns nil if not cached.
func (c *Cache) Get(method, url string) (*Entry, error) {
	key := cacheKey(method, url)
	var entry *Entry

	err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		if b == nil {
			return nil
		}
		data := b.Get([]byte(key))
		if data == nil {
			return nil
		}
		entry = &Entry{}
		return json.Unmarshal(data, entry)
	})
	if err != nil {
		return nil, err
	}

	return entry, nil
}

// Put stores a response in the cache, evicting old entries if over the size limit.
func (c *Cache) Put(entry *Entry) error {
	key := cacheKey(entry.Method, entry.URL)
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling cache entry: %w", err)
	}

	return c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		if b == nil {
			return nil
		}

		if err := b.Put([]byte(key), data); err != nil {
			return err
		}

		// Evict if over size limit
		if c.maxSize > 0 {
			return c.evict(b)
		}
		return nil
	})
}

// Delete removes a cached entry.
func (c *Cache) Delete(method, url string) error {
	key := cacheKey(method, url)
	return c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		if b == nil {
			return nil
		}
		return b.Delete([]byte(key))
	})
}

// Clear removes all cached entries.
func (c *Cache) Clear() error {
	return c.db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket(bucketName); err != nil {
			return err
		}
		_, err := tx.CreateBucket(bucketName)
		return err
	})
}

// Stats returns the number of entries and total size in bytes.
func (c *Cache) Stats() (count int, sizeBytes int64, err error) {
	err = c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			count++
			sizeBytes += int64(len(k) + len(v))
			return nil
		})
	})
	return
}

// evict removes the oldest entries until the total size is under the limit.
func (c *Cache) evict(b *bolt.Bucket) error {
	type entryMeta struct {
		key      string
		size     int64
		storedAt time.Time
	}

	var entries []entryMeta
	var totalSize int64

	// Collect all entries with their metadata
	if err := b.ForEach(func(k, v []byte) error {
		totalSize += int64(len(k) + len(v))
		var e Entry
		if json.Unmarshal(v, &e) == nil {
			entries = append(entries, entryMeta{
				key:      string(k),
				size:     int64(len(k) + len(v)),
				storedAt: e.StoredAt,
			})
		}
		return nil
	}); err != nil {
		return err
	}

	if totalSize <= c.maxSize {
		return nil
	}

	// Sort by stored time, oldest first
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].storedAt.Before(entries[j].storedAt)
	})

	// Delete oldest until under limit
	for _, e := range entries {
		if totalSize <= c.maxSize {
			break
		}
		if err := b.Delete([]byte(e.key)); err != nil {
			return err
		}
		totalSize -= e.size
	}

	return nil
}

// IsCacheable returns true if the response should be cached per RFC 7234.
// Only GET responses with 200 status and no Cache-Control: no-store are cached.
func IsCacheable(method string, statusCode int, respHeaders http.Header) bool {
	if method != http.MethodGet {
		return false
	}
	if statusCode != http.StatusOK {
		return false
	}
	cc := respHeaders.Get("Cache-Control")
	if strings.Contains(strings.ToLower(cc), "no-store") {
		return false
	}
	return true
}

// ParseExpiry determines the expiry time from Cache-Control and Expires headers
// per RFC 7234. Returns zero time if no expiry can be determined.
func ParseExpiry(respHeaders http.Header) time.Time {
	cc := respHeaders.Get("Cache-Control")
	if cc != "" {
		for _, directive := range strings.Split(cc, ",") {
			directive = strings.TrimSpace(strings.ToLower(directive))
			if strings.HasPrefix(directive, "max-age=") {
				ageStr := strings.TrimPrefix(directive, "max-age=")
				var seconds int
				if _, err := fmt.Sscanf(ageStr, "%d", &seconds); err == nil && seconds > 0 {
					return time.Now().Add(time.Duration(seconds) * time.Second)
				}
			}
			if directive == "no-cache" || directive == "no-store" {
				return time.Time{}
			}
		}
	}

	expires := respHeaders.Get("Expires")
	if expires != "" {
		if t, err := http.ParseTime(expires); err == nil {
			return t
		}
	}

	return time.Time{}
}

// ConditionalHeaders adds If-None-Match and If-Modified-Since headers to a
// request based on a cached entry's validators.
func ConditionalHeaders(req *http.Request, entry *Entry) {
	if entry.ETag != "" {
		req.Header.Set("If-None-Match", entry.ETag)
	}
	if entry.LastModified != "" {
		req.Header.Set("If-Modified-Since", entry.LastModified)
	}
}

// EntryFromResponse creates a cache Entry from an HTTP response.
func EntryFromResponse(method, url string, resp *http.Response, body []byte) *Entry {
	headers := make(map[string]string)
	for k := range resp.Header {
		headers[k] = resp.Header.Get(k)
	}

	return &Entry{
		URL:          url,
		Method:       method,
		StatusCode:   resp.StatusCode,
		Headers:      headers,
		Body:         body,
		StoredAt:     time.Now(),
		ExpiresAt:    ParseExpiry(resp.Header),
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}
}
