// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateSpecCache(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, ".clictl", "cache")
	os.MkdirAll(cacheDir, 0o755)
	t.Setenv("HOME", tmpDir)

	cache, err := OpenPrivateSpecCache()
	if err != nil {
		t.Fatalf("OpenPrivateSpecCache: %v", err)
	}
	defer cache.Close()

	// Test Put and Get
	err = cache.Put("my-tool", "name: my-tool\nversion: 1.0", "source-123", 5, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	yaml, ok := cache.Get("my-tool", "source-123", 5, "2026-01-01T00:00:00Z")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if yaml != "name: my-tool\nversion: 1.0" {
		t.Errorf("unexpected yaml: %s", yaml)
	}

	// Test cache miss on different source ID
	_, ok = cache.Get("my-tool", "different-source", 5, "2026-01-01T00:00:00Z")
	if ok {
		t.Error("expected cache miss for different source")
	}

	// Test cache invalidation on spec count change
	_, ok = cache.Get("my-tool", "source-123", 6, "2026-01-01T00:00:00Z")
	if ok {
		t.Error("expected cache miss for different spec count")
	}

	// Test cache invalidation on last synced change
	_, ok = cache.Get("my-tool", "source-123", 5, "2026-01-02T00:00:00Z")
	if ok {
		t.Error("expected cache miss for different last synced")
	}

	// Test cache miss for non-existent key
	_, ok = cache.Get("nonexistent", "source-123", 5, "2026-01-01T00:00:00Z")
	if ok {
		t.Error("expected cache miss for nonexistent key")
	}
}
