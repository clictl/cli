// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package httpcache

import (
	"net/http"
	"os"
	"testing"
	"time"
)

func openTestCache(t *testing.T, maxMB int) *Cache {
	t.Helper()
	dir := t.TempDir()
	c, err := Open(Options{Dir: dir, MaxSizeMB: maxMB})
	if err != nil {
		t.Fatalf("Open cache: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestPutAndGet(t *testing.T) {
	c := openTestCache(t, 0)

	entry := &Entry{
		URL:        "https://api.example.com/data",
		Method:     "GET",
		StatusCode: 200,
		Body:       []byte(`{"result": "ok"}`),
		StoredAt:   time.Now(),
		ExpiresAt:  time.Now().Add(1 * time.Hour),
		ETag:       `"abc123"`,
	}

	if err := c.Put(entry); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := c.Get("GET", "https://api.example.com/data")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Expected non-nil entry")
	}
	if string(got.Body) != `{"result": "ok"}` {
		t.Errorf("Body: got %q", string(got.Body))
	}
	if got.ETag != `"abc123"` {
		t.Errorf("ETag: got %q", got.ETag)
	}
}

func TestGetMissing(t *testing.T) {
	c := openTestCache(t, 0)

	got, err := c.Get("GET", "https://missing.example.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("Expected nil for missing entry")
	}
}

func TestEntry_IsExpired(t *testing.T) {
	fresh := &Entry{ExpiresAt: time.Now().Add(1 * time.Hour)}
	if fresh.IsExpired() {
		t.Error("Fresh entry should not be expired")
	}

	stale := &Entry{ExpiresAt: time.Now().Add(-1 * time.Hour)}
	if !stale.IsExpired() {
		t.Error("Stale entry should be expired")
	}

	noExpiry := &Entry{}
	if noExpiry.IsExpired() {
		t.Error("Entry with zero expiry should not be expired")
	}
}

func TestEntry_HasValidator(t *testing.T) {
	e1 := &Entry{ETag: `"abc"`}
	if !e1.HasValidator() {
		t.Error("Entry with ETag should have validator")
	}

	e2 := &Entry{LastModified: "Mon, 01 Jan 2024 00:00:00 GMT"}
	if !e2.HasValidator() {
		t.Error("Entry with Last-Modified should have validator")
	}

	e3 := &Entry{}
	if e3.HasValidator() {
		t.Error("Entry without validators should not have validator")
	}
}

func TestDelete(t *testing.T) {
	c := openTestCache(t, 0)

	entry := &Entry{
		URL:      "https://api.example.com/delete-me",
		Method:   "GET",
		Body:     []byte("data"),
		StoredAt: time.Now(),
	}
	c.Put(entry)

	if err := c.Delete("GET", "https://api.example.com/delete-me"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, _ := c.Get("GET", "https://api.example.com/delete-me")
	if got != nil {
		t.Error("Expected nil after delete")
	}
}

func TestClear(t *testing.T) {
	c := openTestCache(t, 0)

	for i := 0; i < 5; i++ {
		c.Put(&Entry{
			URL: "https://api.example.com/" + string(rune('a'+i)),
			Method: "GET", Body: []byte("x"), StoredAt: time.Now(),
		})
	}

	if err := c.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	count, _, _ := c.Stats()
	if count != 0 {
		t.Errorf("After clear: got %d entries, want 0", count)
	}
}

func TestStats(t *testing.T) {
	c := openTestCache(t, 0)

	c.Put(&Entry{URL: "https://a.com", Method: "GET", Body: []byte("aaa"), StoredAt: time.Now()})
	c.Put(&Entry{URL: "https://b.com", Method: "GET", Body: []byte("bbb"), StoredAt: time.Now()})

	count, size, err := c.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if count != 2 {
		t.Errorf("Count: got %d, want 2", count)
	}
	if size <= 0 {
		t.Errorf("Size: got %d, want > 0", size)
	}
}

func TestEviction(t *testing.T) {
	// Use a 1KB limit
	dir := t.TempDir()
	c, err := Open(Options{Dir: dir, MaxSizeMB: 0})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()

	// Set a very small limit directly for testing
	c.maxSize = 500

	// Fill with entries
	for i := 0; i < 10; i++ {
		c.Put(&Entry{
			URL:      "https://api.example.com/" + string(rune('a'+i)),
			Method:   "GET",
			Body:     make([]byte, 100), // 100 bytes per entry
			StoredAt: time.Now().Add(time.Duration(i) * time.Second),
		})
	}

	// After eviction, should be under the limit
	_, size, _ := c.Stats()
	if size > 500 {
		t.Errorf("Size after eviction: %d, want <= 500", size)
	}
}

func TestIsCacheable(t *testing.T) {
	tests := []struct {
		method string
		status int
		cc     string
		want   bool
	}{
		{"GET", 200, "", true},
		{"POST", 200, "", false},
		{"GET", 404, "", false},
		{"GET", 200, "no-store", false},
		{"GET", 200, "max-age=3600", true},
		{"GET", 200, "public, max-age=86400", true},
	}

	for _, tt := range tests {
		h := http.Header{}
		if tt.cc != "" {
			h.Set("Cache-Control", tt.cc)
		}
		got := IsCacheable(tt.method, tt.status, h)
		if got != tt.want {
			t.Errorf("IsCacheable(%q, %d, %q): got %v, want %v", tt.method, tt.status, tt.cc, got, tt.want)
		}
	}
}

func TestParseExpiry(t *testing.T) {
	// max-age
	h := http.Header{}
	h.Set("Cache-Control", "max-age=3600")
	exp := ParseExpiry(h)
	if exp.IsZero() {
		t.Error("Expected non-zero expiry for max-age=3600")
	}
	if time.Until(exp) < 3500*time.Second || time.Until(exp) > 3700*time.Second {
		t.Errorf("Expected expiry ~3600s from now, got %v", time.Until(exp))
	}

	// no-cache
	h2 := http.Header{}
	h2.Set("Cache-Control", "no-cache")
	exp2 := ParseExpiry(h2)
	if !exp2.IsZero() {
		t.Error("Expected zero expiry for no-cache")
	}

	// No headers
	h3 := http.Header{}
	exp3 := ParseExpiry(h3)
	if !exp3.IsZero() {
		t.Error("Expected zero expiry with no headers")
	}
}

func TestCorruptedDB_Recovery(t *testing.T) {
	dir := t.TempDir()

	// Write corrupt data to the db file
	dbPath := dir + "/responses.db"
	if err := writeTestFile(dbPath, []byte("not a valid bbolt database")); err != nil {
		t.Fatal(err)
	}

	// Open should recover by deleting and recreating
	c, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Open with corrupt DB: %v", err)
	}
	defer c.Close()

	// Should work after recovery
	err = c.Put(&Entry{URL: "https://test.com", Method: "GET", Body: []byte("ok"), StoredAt: time.Now()})
	if err != nil {
		t.Fatalf("Put after recovery: %v", err)
	}
}

func writeTestFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
