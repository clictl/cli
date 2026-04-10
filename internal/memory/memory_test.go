// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupTestDir(t *testing.T) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".clictl", "memory")
	t.Setenv("HOME", filepath.Dir(filepath.Dir(dir)))
}

func fixedTime() time.Time {
	return time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
}

func TestAddAndLoad(t *testing.T) {
	setupTestDir(t)

	if err := Add("test-tool", "first note", TypeNote); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := Add("test-tool", "second note", TypeGotcha); err != nil {
		t.Fatalf("Add second: %v", err)
	}

	entries, err := Load("test-tool")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Note != "first note" || entries[0].Type != TypeNote {
		t.Errorf("entry 0: %+v", entries[0])
	}
	if entries[1].Note != "second note" || entries[1].Type != TypeGotcha {
		t.Errorf("entry 1: %+v", entries[1])
	}
}

func TestLoadEmpty(t *testing.T) {
	setupTestDir(t)
	entries, err := Load("nonexistent")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if entries != nil {
		t.Fatalf("expected nil, got %v", entries)
	}
}

func TestRemove(t *testing.T) {
	setupTestDir(t)
	Add("tool", "a", TypeNote)
	Add("tool", "b", TypeTip)
	Add("tool", "c", TypeNote)

	if err := Remove("tool", 1); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	entries, _ := Load("tool")
	if len(entries) != 2 {
		t.Fatalf("expected 2, got %d", len(entries))
	}
	if entries[0].Note != "a" || entries[1].Note != "c" {
		t.Errorf("unexpected: %v", entries)
	}
}

func TestRemoveInvalidIndex(t *testing.T) {
	setupTestDir(t)
	Add("tool", "only one", TypeNote)
	if err := Remove("tool", 5); err == nil {
		t.Fatal("expected error")
	}
}

func TestClear(t *testing.T) {
	setupTestDir(t)
	Add("tool", "note", TypeNote)
	if err := Clear("tool"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	entries, _ := Load("tool")
	if entries != nil {
		t.Fatalf("expected nil, got %v", entries)
	}
}

func TestClearNonexistent(t *testing.T) {
	setupTestDir(t)
	if err := Clear("nonexistent"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
}

func TestListTools(t *testing.T) {
	setupTestDir(t)
	Add("alpha", "note", TypeNote)
	Add("beta", "note", TypeGotcha)

	tools, err := ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2, got %d", len(tools))
	}
}

func TestFilePermissions(t *testing.T) {
	setupTestDir(t)
	Add("secure-tool", "secret", TypeNote)

	dir, _ := Dir()
	info, err := os.Stat(filepath.Join(dir, "secure-tool.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected 0600, got %o", perm)
	}
}

func TestParseType(t *testing.T) {
	if ParseType("gotcha") != TypeGotcha {
		t.Error("gotcha")
	}
	if ParseType("GOTCHA") != TypeGotcha {
		t.Error("case insensitive")
	}
	if ParseType("invalid") != TypeNote {
		t.Error("invalid -> note")
	}
	if ParseType("") != TypeNote {
		t.Error("empty -> note")
	}
}

func TestFormatText(t *testing.T) {
	entries := []Entry{
		{Note: "first", Type: TypeGotcha, CreatedAt: fixedTime()},
		{Note: "second", Type: TypeTip, CreatedAt: fixedTime()},
	}
	out := FormatText("test", entries)
	if !strings.Contains(out, "[gotcha]") || !strings.Contains(out, "[tip]") {
		t.Errorf("missing types: %s", out)
	}
}

func TestFormatJSON(t *testing.T) {
	entries := []Entry{{Note: "test", Type: TypeNote, CreatedAt: fixedTime()}}
	out, err := FormatJSON("tool", entries)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["tool"] != "tool" || result["count"] != float64(1) {
		t.Error("wrong JSON output")
	}
}

func TestFormatMarkdown(t *testing.T) {
	entries := []Entry{
		{Note: "watch out", Type: TypeGotcha, CreatedAt: fixedTime()},
		{Note: "try this", Type: TypeTip, CreatedAt: fixedTime()},
		{Note: "general", Type: TypeNote, CreatedAt: fixedTime()},
	}
	out := FormatMarkdown("test", entries)
	if !strings.Contains(out, "### Gotchas") {
		t.Error("missing Gotchas")
	}
	if !strings.Contains(out, "### Tips") {
		t.Error("missing Tips")
	}
	if !strings.Contains(out, "### Notes") {
		t.Error("missing Notes")
	}
}

func TestFormatMarkdownEmpty(t *testing.T) {
	if out := FormatMarkdown("test", nil); out != "" {
		t.Errorf("expected empty, got %q", out)
	}
}

func TestDefaultTypeBackfill(t *testing.T) {
	setupTestDir(t)
	dir, _ := Dir()
	path := filepath.Join(dir, "old-tool.json")
	os.WriteFile(path, []byte(`[{"note": "old entry", "created_at": "2026-01-01T00:00:00Z"}]`), 0600)

	entries, err := Load("old-tool")
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Type != TypeNote {
		t.Errorf("expected backfilled note, got %s", entries[0].Type)
	}
}
