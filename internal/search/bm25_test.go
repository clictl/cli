// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"spaces", "hello world", []string{"hello", "world"}},
		{"hyphens", "my-cool-tool", []string{"my", "cool", "tool"}},
		{"underscores", "my_cool_tool", []string{"my", "cool", "tool"}},
		{"mixed case", "Hello World", []string{"hello", "world"}},
		{"punctuation", "hello, world! foo.", []string{"hello", "world", "foo"}},
		{"mixed delimiters", "hello-world_foo bar.baz", []string{"hello", "world", "foo", "bar", "baz"}},
		{"empty string", "", nil},
		{"only delimiters", "---___...", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Tokenize(tt.input)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("Tokenize(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("Tokenize(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTokenizeField(t *testing.T) {
	freq := TokenizeField("hello world hello")
	if freq["hello"] != 2 {
		t.Errorf("expected hello freq 2, got %d", freq["hello"])
	}
	if freq["world"] != 1 {
		t.Errorf("expected world freq 1, got %d", freq["world"])
	}
}

func TestScoreExactNameHigherThanDescription(t *testing.T) {
	now := time.Now()
	nameDoc := Document{
		Name:        "weather",
		Description: "a tool for various tasks",
		UpdatedAt:   now,
	}
	descDoc := Document{
		Name:        "utility",
		Description: "fetches weather data from the internet",
		UpdatedAt:   now,
	}

	idx := BuildIndex([]Document{nameDoc, descDoc})

	nameScore := Score("weather", nameDoc, idx.IDF, idx.AvgLengths, len(idx.Documents))
	descScore := Score("weather", descDoc, idx.IDF, idx.AvgLengths, len(idx.Documents))

	if nameScore <= descScore {
		t.Errorf("name match score (%.4f) should be > description match score (%.4f)", nameScore, descScore)
	}
}

func TestFieldWeightOrdering(t *testing.T) {
	now := time.Now()
	docs := []Document{
		{Name: "github", Description: "version control", Category: "developer", Tags: []string{"git"}, UpdatedAt: now},
		{Name: "utility", Description: "a developer tool", Category: "github", Tags: []string{"code"}, UpdatedAt: now},
		{Name: "fetcher", Description: "a tool", Category: "network", Tags: []string{"github"}, UpdatedAt: now},
		{Name: "runner", Description: "runs github actions locally", Category: "ci", Tags: []string{"automation"}, UpdatedAt: now},
	}

	idx := BuildIndex(docs)

	nameScore := Score("github", docs[0], idx.IDF, idx.AvgLengths, len(idx.Documents))
	catScore := Score("github", docs[1], idx.IDF, idx.AvgLengths, len(idx.Documents))
	tagScore := Score("github", docs[2], idx.IDF, idx.AvgLengths, len(idx.Documents))
	descScore := Score("github", docs[3], idx.IDF, idx.AvgLengths, len(idx.Documents))

	if nameScore <= tagScore {
		t.Errorf("name match (%.4f) should be > tag match (%.4f)", nameScore, tagScore)
	}
	if tagScore <= catScore {
		t.Errorf("tag match (%.4f) should be > category match (%.4f)", tagScore, catScore)
	}
	if catScore <= descScore {
		t.Errorf("category match (%.4f) should be > description match (%.4f)", catScore, descScore)
	}
}

func TestFreshnessBoost(t *testing.T) {
	recent := Document{
		Name:      "tool-a",
		UpdatedAt: time.Now(),
	}
	old := Document{
		Name:      "tool-a",
		UpdatedAt: time.Now().AddDate(-2, 0, 0),
	}

	idx := BuildIndex([]Document{recent, old})

	recentScore := Score("tool", recent, idx.IDF, idx.AvgLengths, len(idx.Documents))
	oldScore := Score("tool", old, idx.IDF, idx.AvgLengths, len(idx.Documents))

	if recentScore <= oldScore {
		t.Errorf("recent doc score (%.4f) should be > old doc score (%.4f)", recentScore, oldScore)
	}
}

func TestBuildIndexAndSearch(t *testing.T) {
	now := time.Now()
	docs := []Document{
		{Name: "weather-api", Description: "get weather forecasts", Category: "data", Tags: []string{"weather", "api"}, UpdatedAt: now},
		{Name: "github-cli", Description: "interact with github repos", Category: "developer", Tags: []string{"git", "github"}, UpdatedAt: now},
		{Name: "slack-bot", Description: "send messages to slack", Category: "communication", Tags: []string{"chat", "slack"}, UpdatedAt: now},
	}

	idx := BuildIndex(docs)

	results := idx.Search("weather", 10)
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'weather'")
	}
	if results[0].Document.Name != "weather-api" {
		t.Errorf("expected first result to be 'weather-api', got %q", results[0].Document.Name)
	}

	results = idx.Search("github", 10)
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'github'")
	}
	if results[0].Document.Name != "github-cli" {
		t.Errorf("expected first result to be 'github-cli', got %q", results[0].Document.Name)
	}

	// Test limit
	results = idx.Search("weather", 1)
	if len(results) != 1 {
		t.Errorf("expected 1 result with limit=1, got %d", len(results))
	}
}

func TestSaveAndLoadIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bm25")

	now := time.Now().Truncate(time.Second)
	docs := []Document{
		{Name: "test-tool", Description: "a test tool", Category: "testing", Tags: []string{"test"}, Version: "1.0", UpdatedAt: now},
	}

	idx := BuildIndex(docs)
	if err := idx.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := LoadIndex(path)
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	if len(loaded.Documents) != 1 {
		t.Fatalf("expected 1 document, got %d", len(loaded.Documents))
	}
	if loaded.Documents[0].Name != "test-tool" {
		t.Errorf("expected name 'test-tool', got %q", loaded.Documents[0].Name)
	}
	if loaded.Documents[0].Version != "1.0" {
		t.Errorf("expected version '1.0', got %q", loaded.Documents[0].Version)
	}

	// Verify IDF was preserved.
	if len(loaded.IDF) == 0 {
		t.Error("expected IDF to be non-empty after load")
	}

	// Search should still work after round-trip.
	results := loaded.Search("test", 10)
	if len(results) == 0 {
		t.Error("expected results from loaded index")
	}
}

func TestLoadIndexNotFound(t *testing.T) {
	_, err := LoadIndex("/nonexistent/path/index.bm25")
	if err == nil {
		t.Error("expected error loading from nonexistent path")
	}
}

func TestEmptyQueryReturnsAllDocs(t *testing.T) {
	now := time.Now()
	docs := []Document{
		{Name: "a", UpdatedAt: now.Add(-24 * time.Hour)},
		{Name: "b", UpdatedAt: now},
		{Name: "c", UpdatedAt: now.Add(-48 * time.Hour)},
	}

	idx := BuildIndex(docs)
	results := idx.Search("", 10)

	if len(results) != 3 {
		t.Fatalf("expected 3 results for empty query, got %d", len(results))
	}
	// Most recent first.
	if results[0].Document.Name != "b" {
		t.Errorf("expected first result to be 'b' (most recent), got %q", results[0].Document.Name)
	}
}

func TestNoResultsReturnsEmptySlice(t *testing.T) {
	docs := []Document{
		{Name: "weather-api", Description: "get forecasts", UpdatedAt: time.Now()},
	}

	idx := BuildIndex(docs)
	results := idx.Search("xyznonexistent", 10)

	if results != nil && len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestSearchEmptyIndex(t *testing.T) {
	idx := BuildIndex(nil)
	results := idx.Search("anything", 10)
	if results != nil {
		t.Errorf("expected nil results from empty index, got %v", results)
	}
}

func TestActionNameFieldWeightHigherThanDescription(t *testing.T) {
	now := time.Now()
	// Doc with "deploy" in ActionNames only
	actNameDoc := Document{
		Name:        "utility",
		Description: "a generic tool",
		ActionNames: "deploy rollback",
		UpdatedAt:   now,
	}
	// Doc with "deploy" in Description only
	descDoc := Document{
		Name:        "runner",
		Description: "deploy applications to the cloud",
		UpdatedAt:   now,
	}

	idx := BuildIndex([]Document{actNameDoc, descDoc})

	actNameScore := Score("deploy", actNameDoc, idx.IDF, idx.AvgLengths, len(idx.Documents))
	descScore := Score("deploy", descDoc, idx.IDF, idx.AvgLengths, len(idx.Documents))

	if actNameScore <= descScore {
		t.Errorf("action_names match score (%.4f) should be > description match score (%.4f)", actNameScore, descScore)
	}
}

func TestActionDescFieldWeightHigherThanDescription(t *testing.T) {
	now := time.Now()
	// Doc with "deploy" in ActionDescriptions only
	actDescDoc := Document{
		Name:               "utility",
		Description:        "a generic tool",
		ActionDescriptions: "deploy containers to kubernetes",
		UpdatedAt:          now,
	}
	// Doc with "deploy" in Description only
	descDoc := Document{
		Name:        "runner",
		Description: "deploy applications to the cloud",
		UpdatedAt:   now,
	}

	idx := BuildIndex([]Document{actDescDoc, descDoc})

	actDescScore := Score("deploy", actDescDoc, idx.IDF, idx.AvgLengths, len(idx.Documents))
	descScore := Score("deploy", descDoc, idx.IDF, idx.AvgLengths, len(idx.Documents))

	if actDescScore <= descScore {
		t.Errorf("action_descriptions match score (%.4f) should be > description match score (%.4f)", actDescScore, descScore)
	}
}

func TestSourceBoost(t *testing.T) {
	now := time.Now()
	doc := Document{
		Name:      "weather",
		UpdatedAt: now,
	}

	idx := BuildIndex([]Document{doc})
	boosts := DefaultSearchBoosts()

	canonical := ScoreWithBoosts("weather", doc, idx.IDF, idx.AvgLengths, len(idx.Documents), boosts, true)
	nonCanonical := ScoreWithBoosts("weather", doc, idx.IDF, idx.AvgLengths, len(idx.Documents), boosts, false)

	if canonical <= nonCanonical {
		t.Errorf("canonical score (%.4f) should be > non-canonical score (%.4f)", canonical, nonCanonical)
	}
}

func TestTrustTierBoost(t *testing.T) {
	now := time.Now()
	official := Document{
		Name:      "weather",
		TrustTier: "official",
		UpdatedAt: now,
	}
	community := Document{
		Name:      "weather",
		TrustTier: "community",
		UpdatedAt: now,
	}

	idx := BuildIndex([]Document{official, community})
	boosts := DefaultSearchBoosts()

	officialScore := ScoreWithBoosts("weather", official, idx.IDF, idx.AvgLengths, len(idx.Documents), boosts, false)
	communityScore := ScoreWithBoosts("weather", community, idx.IDF, idx.AvgLengths, len(idx.Documents), boosts, false)

	if officialScore <= communityScore {
		t.Errorf("official tier score (%.4f) should be > community tier score (%.4f)", officialScore, communityScore)
	}
}

func TestDefaultSearchBoosts(t *testing.T) {
	b := DefaultSearchBoosts()

	if b.CanonicalSource != 1.5 {
		t.Errorf("expected CanonicalSource 1.5, got %.1f", b.CanonicalSource)
	}
	if b.TierOfficial != 1.4 {
		t.Errorf("expected TierOfficial 1.4, got %.1f", b.TierOfficial)
	}
	if b.TierCertified != 1.3 {
		t.Errorf("expected TierCertified 1.3, got %.1f", b.TierCertified)
	}
	if b.TierVerified != 1.1 {
		t.Errorf("expected TierVerified 1.1, got %.1f", b.TierVerified)
	}
	if b.TierCommunity != 1.0 {
		t.Errorf("expected TierCommunity 1.0, got %.1f", b.TierCommunity)
	}

	// Test TierBoost method
	if b.TierBoost("official") != 1.4 {
		t.Errorf("TierBoost('official') = %.1f, want 1.4", b.TierBoost("official"))
	}
	if b.TierBoost("certified") != 1.3 {
		t.Errorf("TierBoost('certified') = %.1f, want 1.3", b.TierBoost("certified"))
	}
	if b.TierBoost("verified") != 1.1 {
		t.Errorf("TierBoost('verified') = %.1f, want 1.1", b.TierBoost("verified"))
	}
	if b.TierBoost("community") != 1.0 {
		t.Errorf("TierBoost('community') = %.1f, want 1.0", b.TierBoost("community"))
	}
	if b.TierBoost("unknown") != 1.0 {
		t.Errorf("TierBoost('unknown') = %.1f, want 1.0", b.TierBoost("unknown"))
	}
}

func TestScanToolboxDirs(t *testing.T) {
	dir := t.TempDir()
	specDir := filepath.Join(dir, "shelf", "weather")
	if err := os.MkdirAll(specDir, 0755); err != nil {
		t.Fatal(err)
	}

	specContent := `name: weather-api
version: "1.0"
description: Get weather data
category: data
tags:
  - weather
  - api
protocol: http
auth: api_key
`
	if err := os.WriteFile(filepath.Join(specDir, "weather-api.yaml"), []byte(specContent), 0644); err != nil {
		t.Fatal(err)
	}

	docs, err := ScanToolboxDirs([]string{dir})
	if err != nil {
		t.Fatalf("ScanToolboxDirs failed: %v", err)
	}

	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}

	doc := docs[0]
	if doc.Name != "weather-api" {
		t.Errorf("expected name 'weather-api', got %q", doc.Name)
	}
	if doc.Category != "data" {
		t.Errorf("expected category 'data', got %q", doc.Category)
	}
	if len(doc.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(doc.Tags))
	}
	if doc.Auth != "api_key" {
		t.Errorf("expected auth 'api_key', got %q", doc.Auth)
	}
}

func TestScanToolboxDirsSkipsInvalid(t *testing.T) {
	dir := t.TempDir()

	// File without name field should be skipped.
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("description: no name"), 0644); err != nil {
		t.Fatal(err)
	}
	// File with name should be included.
	if err := os.WriteFile(filepath.Join(dir, "good.yaml"), []byte("name: good\nversion: \"1.0\""), 0644); err != nil {
		t.Fatal(err)
	}

	docs, err := ScanToolboxDirs([]string{dir})
	if err != nil {
		t.Fatalf("ScanToolboxDirs failed: %v", err)
	}

	if len(docs) != 1 {
		t.Fatalf("expected 1 document (skipping invalid), got %d", len(docs))
	}
	if docs[0].Name != "good" {
		t.Errorf("expected name 'good', got %q", docs[0].Name)
	}
}

func TestScanToolboxDirsExtractsActions(t *testing.T) {
	dir := t.TempDir()
	specContent := `name: email-tool
namespace: clictl
version: "1.0"
description: Email management
category: communication
actions:
  - name: send
    description: Send an email message to a recipient
  - name: list
    description: List inbox messages
`
	if err := os.WriteFile(filepath.Join(dir, "email-tool.yaml"), []byte(specContent), 0644); err != nil {
		t.Fatal(err)
	}

	docs, err := ScanToolboxDirs([]string{dir})
	if err != nil {
		t.Fatalf("ScanToolboxDirs failed: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}

	doc := docs[0]
	if doc.ActionNames != "send list" {
		t.Errorf("ActionNames: got %q, want %q", doc.ActionNames, "send list")
	}
	if !strings.Contains(doc.ActionDescriptions, "Send an email message") {
		t.Errorf("ActionDescriptions should contain 'Send an email message', got %q", doc.ActionDescriptions)
	}
	if !strings.Contains(doc.ActionDescriptions, "List inbox messages") {
		t.Errorf("ActionDescriptions should contain 'List inbox messages', got %q", doc.ActionDescriptions)
	}
	if doc.Namespace != "clictl" {
		t.Errorf("Namespace: got %q, want %q", doc.Namespace, "clictl")
	}
	if doc.TrustTier != "official" {
		t.Errorf("TrustTier: got %q, want %q", doc.TrustTier, "official")
	}
}

func TestTrustTierDerivation(t *testing.T) {
	tests := []struct {
		namespace string
		wantTier  string
	}{
		{"clictl", "official"},
		{"acme-corp", "community"},
		{"", "community"},
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			got := deriveTrustTier(tt.namespace)
			if got != tt.wantTier {
				t.Errorf("deriveTrustTier(%q) = %q, want %q", tt.namespace, got, tt.wantTier)
			}
		})
	}
}

func TestScanToolboxDirsNonexistentPath(t *testing.T) {
	docs, err := ScanToolboxDirs([]string{"/nonexistent/path/that/does/not/exist"})
	if err != nil {
		t.Fatalf("expected no error for nonexistent path, got: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("expected 0 documents for nonexistent path, got %d", len(docs))
	}
}
