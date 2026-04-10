// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package registry

import (
	"testing"

	"github.com/clictl/cli/internal/models"
)

func setupTestIndex(t *testing.T) *LocalIndex {
	t.Helper()
	dir := t.TempDir()
	li := NewLocalIndex(dir, "test")

	idx := &models.Index{
		SchemaVersion: 1,
		Specs: map[string]models.IndexEntry{
			"openweathermap": {
				Version:     "2.5",
				Description: "Weather data and forecasts",
				Category:    "weather",
				Tags:        []string{"weather", "forecast", "temperature"},
			},
			"github": {
				Version:     "3.0",
				Description: "GitHub REST API",
				Category:    "developer",
				Tags:        []string{"github", "git", "developer"},
			},
			"coingecko": {
				Version:     "3.0",
				Description: "Cryptocurrency prices and market data",
				Category:    "crypto",
				Tags:        []string{"crypto", "bitcoin", "prices"},
			},
		},
	}
	if err := li.Save(idx); err != nil {
		t.Fatalf("Save index: %v", err)
	}
	return li
}

func TestLocalIndex_Search_ExactNameMatch(t *testing.T) {
	li := setupTestIndex(t)
	results, err := li.Search("github")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Expected results for 'github'")
	}
	if results[0].Name != "github" {
		t.Errorf("First result: got %q, want %q", results[0].Name, "github")
	}
}

func TestLocalIndex_Search_TagMatch(t *testing.T) {
	li := setupTestIndex(t)
	results, err := li.Search("bitcoin")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Expected results for 'bitcoin'")
	}
	if results[0].Name != "coingecko" {
		t.Errorf("First result: got %q, want %q", results[0].Name, "coingecko")
	}
}

func TestLocalIndex_Search_DescriptionMatch(t *testing.T) {
	li := setupTestIndex(t)
	results, err := li.Search("forecasts")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Expected results for 'forecasts'")
	}
	if results[0].Name != "openweathermap" {
		t.Errorf("First result: got %q, want %q", results[0].Name, "openweathermap")
	}
}

func TestLocalIndex_Search_NoResults(t *testing.T) {
	li := setupTestIndex(t)
	results, err := li.Search("nonexistent")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Expected 0 results, got %d", len(results))
	}
}

func TestLocalIndex_Search_EmptyQuery(t *testing.T) {
	li := setupTestIndex(t)
	results, err := li.Search("")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results != nil {
		t.Errorf("Expected nil results for empty query, got %d", len(results))
	}
}

func TestLocalIndex_List_All(t *testing.T) {
	li := setupTestIndex(t)
	results, err := li.List("")
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("List all: got %d, want 3", len(results))
	}
}

func TestLocalIndex_List_FilterByCategory(t *testing.T) {
	li := setupTestIndex(t)
	results, err := li.List("weather")
	if err != nil {
		t.Fatalf("List weather: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("List weather: got %d, want 1", len(results))
	}
	if results[0].Name != "openweathermap" {
		t.Errorf("List weather: got %q, want %q", results[0].Name, "openweathermap")
	}
}

func TestLocalIndex_GetEntry_Found(t *testing.T) {
	li := setupTestIndex(t)
	entry, err := li.GetEntry("github")
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if entry.Category != "developer" {
		t.Errorf("Category: got %q, want %q", entry.Category, "developer")
	}
}

func TestLocalIndex_GetEntry_NotFound(t *testing.T) {
	li := setupTestIndex(t)
	_, err := li.GetEntry("nonexistent")
	if err == nil {
		t.Fatal("GetEntry nonexistent: expected error")
	}
}

func TestLocalIndex_Categories(t *testing.T) {
	li := setupTestIndex(t)
	cats, err := li.Categories()
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(cats) != 3 {
		t.Errorf("Categories count: got %d, want 3", len(cats))
	}
	if cats["weather"] != 1 {
		t.Errorf("weather count: got %d, want 1", cats["weather"])
	}
}

func TestLocalIndex_Search_ActionNameMatch(t *testing.T) {
	dir := t.TempDir()
	li := NewLocalIndex(dir, "test")

	idx := &models.Index{
		SchemaVersion: 1,
		Specs: map[string]models.IndexEntry{
			"email-tool": {
				Version:            "1.0",
				Description:        "Email management",
				Category:           "communication",
				ActionNames:        []string{"send", "receive", "list"},
				ActionDescriptions: []string{"Send an email", "Receive emails", "List inbox"},
			},
			"chat-tool": {
				Version:     "1.0",
				Description: "Chat application",
				Category:    "communication",
			},
		},
	}
	if err := li.Save(idx); err != nil {
		t.Fatalf("Save: %v", err)
	}

	results, err := li.Search("send")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Expected results for 'send'")
	}
	if results[0].Name != "email-tool" {
		t.Errorf("First result: got %q, want %q", results[0].Name, "email-tool")
	}
}

func TestLocalIndex_Search_ActionDescriptionMatch(t *testing.T) {
	dir := t.TempDir()
	li := NewLocalIndex(dir, "test")

	idx := &models.Index{
		SchemaVersion: 1,
		Specs: map[string]models.IndexEntry{
			"file-tool": {
				Version:            "1.0",
				Description:        "File operations",
				Category:           "utility",
				ActionDescriptions: []string{"Upload a file to cloud storage"},
			},
		},
	}
	if err := li.Save(idx); err != nil {
		t.Fatalf("Save: %v", err)
	}

	results, err := li.Search("upload")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Expected results for 'upload'")
	}
	if results[0].Name != "file-tool" {
		t.Errorf("First result: got %q, want %q", results[0].Name, "file-tool")
	}
}

func TestLocalIndex_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	li := NewLocalIndex(dir, "roundtrip")

	idx := &models.Index{
		SchemaVersion: 1,
		Specs: map[string]models.IndexEntry{
			"test": {Version: "1.0", Category: "testing"},
		},
	}
	if err := li.Save(idx); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := li.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Specs["test"].Version != "1.0" {
		t.Errorf("Roundtrip version: got %q, want %q", loaded.Specs["test"].Version, "1.0")
	}
}
