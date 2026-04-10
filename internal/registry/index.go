// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/clictl/cli/internal/models"
)

// LocalIndex manages the locally synced index for a registry.
type LocalIndex struct {
	dir  string
	name string
}

// NewLocalIndex creates a new LocalIndex rooted at dir for the named registry.
func NewLocalIndex(dir string, name string) *LocalIndex {
	return &LocalIndex{
		dir:  dir,
		name: name,
	}
}

// indexPath returns the path to the index.json file.
// Checks for toolbox/ directory first; if it exists, reads index.json from there.
// Falls back to root directory for backward compatibility.
func (li *LocalIndex) indexPath() string {
	toolboxDir := filepath.Join(li.dir, "toolbox")
	toolboxIndex := filepath.Join(toolboxDir, "index.json")
	if info, err := os.Stat(toolboxDir); err == nil && info.IsDir() {
		if _, err := os.Stat(toolboxIndex); err == nil {
			return toolboxIndex
		}
	}
	return filepath.Join(li.dir, "index.json")
}

// Load reads index.json from the local registry dir.
func (li *LocalIndex) Load() (*models.Index, error) {
	data, err := os.ReadFile(li.indexPath())
	if err != nil {
		return nil, fmt.Errorf("reading index for registry %q: %w", li.name, err)
	}

	var idx models.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parsing index for registry %q: %w", li.name, err)
	}

	return &idx, nil
}

// Save writes index.json to the local registry dir.
func (li *LocalIndex) Save(idx *models.Index) error {
	if err := os.MkdirAll(li.dir, 0o755); err != nil {
		return fmt.Errorf("creating registry dir %q: %w", li.dir, err)
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling index for registry %q: %w", li.name, err)
	}

	if err := os.WriteFile(li.indexPath(), data, 0o644); err != nil {
		return fmt.Errorf("writing index for registry %q: %w", li.name, err)
	}

	return nil
}

// scoredResult holds a search result paired with its relevance score.
type scoredResult struct {
	result models.SearchResult
	score  int
}

// Search searches the local index by query string.
// Matches against name, description, tags, and category.
// Returns results sorted by relevance (name match > tag match > description match).
func (li *LocalIndex) Search(query string) ([]models.SearchResult, error) {
	idx, err := li.Load()
	if err != nil {
		return nil, err
	}

	queryLower := strings.ToLower(query)
	terms := strings.Fields(queryLower)
	if len(terms) == 0 {
		return nil, nil
	}

	var scored []scoredResult

	for specName, entry := range idx.Specs {
		nameLower := strings.ToLower(specName)
		descLower := strings.ToLower(entry.Description)
		catLower := strings.ToLower(entry.Category)

		totalScore := 0

		for _, term := range terms {
			if nameLower == term {
				totalScore += 100
			} else if strings.Contains(nameLower, term) {
				totalScore += 50
			}

			for _, tag := range entry.Tags {
				if strings.ToLower(tag) == term {
					totalScore += 30
					break
				}
			}

			// Action name exact match
			for _, an := range entry.ActionNames {
				if strings.ToLower(an) == term {
					totalScore += 25
					break
				}
			}

			if catLower == term {
				totalScore += 20
			}

			if strings.Contains(descLower, term) {
				totalScore += 10
			}

			// Action description contains
			for _, ad := range entry.ActionDescriptions {
				if strings.Contains(strings.ToLower(ad), term) {
					totalScore += 8
					break
				}
			}
		}

		if totalScore > 0 {
			scored = append(scored, scoredResult{
				result: models.SearchResult{
					Name:        specName,
					Description: entry.Description,
					Category:    entry.Category,
					Version:     entry.Version,
					Source:      li.name,
				},
				score: totalScore,
			})
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].result.Name < scored[j].result.Name
	})

	results := make([]models.SearchResult, len(scored))
	for i, s := range scored {
		results[i] = s.result
	}

	return results, nil
}

// List returns all specs, optionally filtered by category.
func (li *LocalIndex) List(category string) ([]models.SearchResult, error) {
	idx, err := li.Load()
	if err != nil {
		return nil, err
	}

	catLower := strings.ToLower(category)
	var results []models.SearchResult

	for specName, entry := range idx.Specs {
		if catLower != "" && strings.ToLower(entry.Category) != catLower {
			continue
		}
		results = append(results, models.SearchResult{
			Name:        specName,
			Description: entry.Description,
			Category:    entry.Category,
			Version:     entry.Version,
			Source:      li.name,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	return results, nil
}

// GetEntry returns the index entry for a named spec.
func (li *LocalIndex) GetEntry(name string) (*models.IndexEntry, error) {
	idx, err := li.Load()
	if err != nil {
		return nil, err
	}

	entry, ok := idx.Specs[name]
	if !ok {
		return nil, fmt.Errorf("spec %q not found in registry %q", name, li.name)
	}

	return &entry, nil
}

// Categories returns a list of unique categories with counts.
func (li *LocalIndex) Categories() (map[string]int, error) {
	idx, err := li.Load()
	if err != nil {
		return nil, err
	}

	cats := make(map[string]int)
	for _, entry := range idx.Specs {
		if entry.Category != "" {
			cats[entry.Category]++
		}
	}

	return cats, nil
}
