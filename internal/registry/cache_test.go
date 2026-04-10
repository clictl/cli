// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package registry

import (
	"testing"
	"time"
)

func TestCache_PutAndGet(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)

	data := []byte("name: test\nprotocol: http\nactions:\n  - name: a\n")
	etag := "abc123"

	if err := cache.Put("testool", data, etag); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, gotEtag, fresh, err := cache.Get("testool")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("Data mismatch: got %q", string(got))
	}
	if gotEtag != etag {
		t.Errorf("ETag: got %q, want %q", gotEtag, etag)
	}
	if !fresh {
		t.Error("Expected fresh entry")
	}
}

func TestCache_GetMissing(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)

	data, etag, fresh, err := cache.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get missing: %v", err)
	}
	if data != nil {
		t.Errorf("Expected nil data, got %q", string(data))
	}
	if etag != "" {
		t.Errorf("Expected empty etag, got %q", etag)
	}
	if fresh {
		t.Error("Expected not fresh")
	}
}

func TestCache_Overwrite(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)

	if err := cache.Put("tool", []byte("v1"), "etag1"); err != nil {
		t.Fatalf("Put v1: %v", err)
	}
	if err := cache.Put("tool", []byte("v2"), "etag2"); err != nil {
		t.Fatalf("Put v2: %v", err)
	}

	got, gotEtag, _, err := cache.Get("tool")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "v2" {
		t.Errorf("Data: got %q, want %q", string(got), "v2")
	}
	if gotEtag != "etag2" {
		t.Errorf("ETag: got %q, want %q", gotEtag, "etag2")
	}
}

func TestCache_SoftTTL(t *testing.T) {
	// Verify that the softTTL constant is 1 hour
	if softTTL != 1*time.Hour {
		t.Errorf("softTTL: got %v, want 1h", softTTL)
	}
}

func TestCache_MultipleSpecs(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir)

	if err := cache.Put("tool-a", []byte("a"), "etag-a"); err != nil {
		t.Fatalf("Put a: %v", err)
	}
	if err := cache.Put("tool-b", []byte("b"), "etag-b"); err != nil {
		t.Fatalf("Put b: %v", err)
	}

	gotA, etagA, _, _ := cache.Get("tool-a")
	gotB, etagB, _, _ := cache.Get("tool-b")

	if string(gotA) != "a" || etagA != "etag-a" {
		t.Errorf("tool-a: data=%q etag=%q", string(gotA), etagA)
	}
	if string(gotB) != "b" || etagB != "etag-b" {
		t.Errorf("tool-b: data=%q etag=%q", string(gotB), etagB)
	}
}
