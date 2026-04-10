// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package search

import (
	"encoding/gob"
	"os"
	"sort"
	"strings"
	"time"
)

// Index holds precomputed data for BM25 search.
type Index struct {
	Documents  []Document
	IDF        map[string]float64 // term -> inverse document frequency
	AvgLengths map[string]float64 // field -> average token length
	BuiltAt    time.Time
}

// BuildIndex creates an index from a list of documents. It computes IDF for
// all terms across all fields and average field lengths.
func BuildIndex(docs []Document) *Index {
	idx := &Index{
		Documents:  docs,
		IDF:        make(map[string]float64),
		AvgLengths: make(map[string]float64),
		BuiltAt:    time.Now(),
	}

	if len(docs) == 0 {
		return idx
	}

	N := len(docs)

	// Track which documents contain each term (across all fields).
	termDocCount := make(map[string]int)
	// Track total token counts per field for averaging.
	var totalName, totalDesc, totalCat, totalTags, totalActNames, totalActDescs float64

	for _, doc := range docs {
		seen := make(map[string]bool)

		nameTokens := Tokenize(doc.Name)
		descTokens := Tokenize(doc.Description)
		catTokens := Tokenize(doc.Category)
		tagTokens := Tokenize(strings.Join(doc.Tags, " "))
		actNameTokens := Tokenize(doc.ActionNames)
		actDescTokens := Tokenize(doc.ActionDescriptions)

		totalName += float64(len(nameTokens))
		totalDesc += float64(len(descTokens))
		totalCat += float64(len(catTokens))
		totalTags += float64(len(tagTokens))
		totalActNames += float64(len(actNameTokens))
		totalActDescs += float64(len(actDescTokens))

		allTokens := make([]string, 0, len(nameTokens)+len(descTokens)+len(catTokens)+len(tagTokens)+len(actNameTokens)+len(actDescTokens))
		allTokens = append(allTokens, nameTokens...)
		allTokens = append(allTokens, descTokens...)
		allTokens = append(allTokens, catTokens...)
		allTokens = append(allTokens, tagTokens...)
		allTokens = append(allTokens, actNameTokens...)
		allTokens = append(allTokens, actDescTokens...)

		for _, t := range allTokens {
			if !seen[t] {
				seen[t] = true
				termDocCount[t]++
			}
		}
	}

	nf := float64(N)
	idx.AvgLengths["name"] = totalName / nf
	idx.AvgLengths["description"] = totalDesc / nf
	idx.AvgLengths["category"] = totalCat / nf
	idx.AvgLengths["tags"] = totalTags / nf
	idx.AvgLengths["action_names"] = totalActNames / nf
	idx.AvgLengths["action_descriptions"] = totalActDescs / nf

	for term, count := range termDocCount {
		idx.IDF[term] = computeIDF(N, count)
	}

	return idx
}

// Search returns the top N results for a query, scored by BM25. Results are
// sorted by descending score. If query is empty, all documents are returned
// sorted by UpdatedAt (most recent first).
func (idx *Index) Search(query string, limit int) []SearchResult {
	if len(idx.Documents) == 0 {
		return nil
	}

	queryTerms := Tokenize(query)

	// Empty query: return all docs sorted by freshness.
	if len(queryTerms) == 0 {
		results := make([]SearchResult, len(idx.Documents))
		for i, doc := range idx.Documents {
			results[i] = SearchResult{Document: doc, Score: freshnessBoost(doc.UpdatedAt)}
		}
		sort.Slice(results, func(i, j int) bool {
			return results[i].Score > results[j].Score
		})
		if limit > 0 && len(results) > limit {
			results = results[:limit]
		}
		return results
	}

	docCount := len(idx.Documents)
	var results []SearchResult
	for _, doc := range idx.Documents {
		s := Score(query, doc, idx.IDF, idx.AvgLengths, docCount)
		if s > 0 {
			results = append(results, SearchResult{Document: doc, Score: s})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results
}

// SearchWithBoosts returns the top N results with source and trust tier boosts applied.
// canonicalPrefix is the path prefix that identifies the canonical toolbox.
func (idx *Index) SearchWithBoosts(query string, limit int, boosts SearchBoosts, canonicalPrefix string) []SearchResult {
	if len(idx.Documents) == 0 {
		return nil
	}

	queryTerms := Tokenize(query)

	// Empty query: return all docs sorted by freshness.
	if len(queryTerms) == 0 {
		results := make([]SearchResult, len(idx.Documents))
		for i, doc := range idx.Documents {
			results[i] = SearchResult{Document: doc, Score: freshnessBoost(doc.UpdatedAt)}
		}
		sort.Slice(results, func(i, j int) bool {
			return results[i].Score > results[j].Score
		})
		if limit > 0 && len(results) > limit {
			results = results[:limit]
		}
		return results
	}

	docCount := len(idx.Documents)
	var results []SearchResult
	for _, doc := range idx.Documents {
		isCanonical := canonicalPrefix != "" && strings.HasPrefix(doc.SourcePath, canonicalPrefix)
		s := ScoreWithBoosts(query, doc, idx.IDF, idx.AvgLengths, docCount, boosts, isCanonical)
		if s > 0 {
			results = append(results, SearchResult{Document: doc, Score: s})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results
}

// Save writes the index to a file using gob encoding.
func (idx *Index) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return gob.NewEncoder(f).Encode(idx)
}

// LoadIndex reads an index from a gob-encoded file.
func LoadIndex(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var idx Index
	if err := gob.NewDecoder(f).Decode(&idx); err != nil {
		return nil, err
	}
	return &idx, nil
}
