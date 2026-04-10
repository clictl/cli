// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package memory provides local persistent storage for tool notes.
//
// Memories are JSON files stored at ~/.clictl/memory/, one per tool.
// They persist across sessions and are displayed when inspecting tools.
//
// Each memory has a type that categorizes the knowledge:
//   - gotcha: workarounds, rate limits, quirks
//   - tip: recommended params, better usage patterns
//   - context: project-specific notes
//   - error: error resolutions
//   - note: general notes (default)
package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/clictl/cli/internal/config"
)

// Type categorizes what kind of knowledge a memory captures.
type Type string

const (
	TypeNote     Type = "note"
	TypeGotcha   Type = "gotcha"
	TypeTip      Type = "tip"
	TypeContext  Type = "context"
	TypeError    Type = "error"
	TypeFeedback Type = "feedback"
)

// ValidTypes is the set of allowed memory types.
var ValidTypes = map[Type]string{
	TypeNote:     "General note",
	TypeGotcha:   "Workaround, rate limit, or quirk",
	TypeTip:      "Recommended params or usage pattern",
	TypeContext:  "Project-specific context",
	TypeError:    "Error resolution",
	TypeFeedback: "User feedback on tool quality",
}

// ParseType converts a string to a Type, defaulting to TypeNote.
func ParseType(s string) Type {
	t := Type(strings.ToLower(s))
	if _, ok := ValidTypes[t]; ok {
		return t
	}
	return TypeNote
}

// Entry is a single memory note attached to a tool.
type Entry struct {
	Note      string    `json:"note"`
	Type      Type      `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

// Dir returns the memory storage directory, creating it if needed.
func Dir() (string, error) {
	dir := filepath.Join(config.BaseDir(), "memory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("creating memory directory: %w", err)
	}
	return dir, nil
}

func toolPath(tool string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, tool+".json"), nil
}

// Load returns all memories for a tool. Returns nil if none exist.
func Load(tool string) ([]Entry, error) {
	path, err := toolPath(tool)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading memories for %s: %w", tool, err)
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing memories for %s: %w", tool, err)
	}
	// Backfill type for old entries that don't have one
	for i := range entries {
		if entries[i].Type == "" {
			entries[i].Type = TypeNote
		}
	}
	return entries, nil
}

// Add appends a new memory to a tool.
func Add(tool string, note string, memType Type) error {
	entries, err := Load(tool)
	if err != nil {
		return err
	}
	if memType == "" {
		memType = TypeNote
	}
	entries = append(entries, Entry{
		Note:      note,
		Type:      memType,
		CreatedAt: time.Now().UTC(),
	})
	return save(tool, entries)
}

// Remove deletes a specific memory by index (0-based).
func Remove(tool string, index int) error {
	entries, err := Load(tool)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(entries) {
		return fmt.Errorf("invalid memory index %d for %s (has %d memories)", index, tool, len(entries))
	}
	entries = append(entries[:index], entries[index+1:]...)
	if len(entries) == 0 {
		return Clear(tool)
	}
	return save(tool, entries)
}

// Clear removes all memories for a tool.
func Clear(tool string) error {
	path, err := toolPath(tool)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clearing memories for %s: %w", tool, err)
	}
	return nil
}

// ListTools returns the names of all tools that have memories.
func ListTools() ([]string, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing memory directory: %w", err)
	}
	var tools []string
	for _, f := range files {
		if !f.IsDir() && filepath.Ext(f.Name()) == ".json" {
			tools = append(tools, f.Name()[:len(f.Name())-5])
		}
	}
	return tools, nil
}

// FormatText renders memories as human-readable text.
func FormatText(tool string, entries []Entry) string {
	if len(entries) == 0 {
		return fmt.Sprintf("No memories for %s\n", tool)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Memories for %s:\n", tool))
	for i, e := range entries {
		date := e.CreatedAt.Format("2006-01-02")
		sb.WriteString(fmt.Sprintf("  %d. [%s] %s (%s)\n", i+1, e.Type, e.Note, date))
	}
	return sb.String()
}

// FormatJSON renders memories as a JSON string.
func FormatJSON(tool string, entries []Entry) (string, error) {
	out := map[string]any{
		"tool":    tool,
		"count":   len(entries),
		"entries": entries,
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// FormatMarkdown renders memories as markdown for skill files.
func FormatMarkdown(tool string, entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Memories for %s\n\n", tool))

	// Group by type
	grouped := make(map[Type][]Entry)
	for _, e := range entries {
		grouped[e.Type] = append(grouped[e.Type], e)
	}

	typeOrder := []Type{TypeGotcha, TypeError, TypeTip, TypeContext, TypeNote}
	typeLabels := map[Type]string{
		TypeGotcha:  "Gotchas",
		TypeError:   "Error Resolutions",
		TypeTip:     "Tips",
		TypeContext:  "Project Context",
		TypeNote:    "Notes",
	}

	for _, t := range typeOrder {
		entries, ok := grouped[t]
		if !ok || len(entries) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("### %s\n\n", typeLabels[t]))
		for _, e := range entries {
			sb.WriteString(fmt.Sprintf("- %s (%s)\n", e.Note, e.CreatedAt.Format("2006-01-02")))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func save(tool string, entries []Entry) error {
	path, err := toolPath(tool)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding memories for %s: %w", tool, err)
	}
	return os.WriteFile(path, data, 0600)
}
