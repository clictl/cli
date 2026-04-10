// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package search

import (
	"math"
	"strings"
	"time"
)

const (
	// K1 is the BM25 term frequency saturation parameter.
	K1 = 1.2
	// B is the BM25 length normalization parameter.
	B = 0.75
)

// Field weight constants for BM25 scoring.
const (
	WeightName               = 10.0
	WeightTags               = 5.0
	WeightActionNames        = 4.0
	WeightCategory           = 3.0
	WeightActionDescriptions = 1.5
	WeightDescription        = 1.0
)

// SearchBoosts holds configurable multipliers for source and trust tier ranking.
type SearchBoosts struct {
	CanonicalSource float64
	TierOfficial    float64
	TierCertified   float64
	TierVerified    float64
	TierCommunity   float64
}

// DefaultSearchBoosts returns the default boost multipliers.
// Note: certified/verified tiers are only derivable from backend data.
// Local-only search uses "official" (clictl namespace) or "community".
func DefaultSearchBoosts() SearchBoosts {
	return SearchBoosts{
		CanonicalSource: 1.5,
		TierOfficial:    1.4,
		TierCertified:   1.3,
		TierVerified:    1.1,
		TierCommunity:   1.0,
	}
}

// TierBoost returns the boost multiplier for the given trust tier.
func (b SearchBoosts) TierBoost(tier string) float64 {
	switch strings.ToLower(tier) {
	case "official":
		return b.TierOfficial
	case "certified":
		return b.TierCertified
	case "verified":
		return b.TierVerified
	default:
		return b.TierCommunity
	}
}

// Document represents a tool spec for indexing and search.
type Document struct {
	Name               string
	Description        string
	Category           string
	Tags               []string
	ActionNames        string
	ActionDescriptions string
	Namespace          string
	TrustTier          string
	Version            string
	Protocol           string
	Auth               string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	SourcePath         string
}

// SearchResult holds a document and its BM25 score.
type SearchResult struct {
	Document Document
	Score    float64
}

// Score computes the BM25 score for a query against a document with field weights.
// Fields and weights: name=10, tags=5, action_names=4, category=3, action_descriptions=1.5, description=1.
// A freshness boost is applied: 1.0 + 0.1 * recency where recency decays
// linearly from 1.0 (today) to 0.0 over 365 days.
func Score(query string, doc Document, idf map[string]float64, avgLengths map[string]float64, docCount int) float64 {
	queryTerms := Tokenize(query)
	if len(queryTerms) == 0 {
		return 0
	}

	nameFreq := TokenizeField(doc.Name)
	descFreq := TokenizeField(doc.Description)
	catFreq := TokenizeField(doc.Category)
	tagFreq := TokenizeField(strings.Join(doc.Tags, " "))
	actNameFreq := TokenizeField(doc.ActionNames)
	actDescFreq := TokenizeField(doc.ActionDescriptions)

	nameLen := float64(len(Tokenize(doc.Name)))
	descLen := float64(len(Tokenize(doc.Description)))
	catLen := float64(len(Tokenize(doc.Category)))
	tagLen := float64(len(Tokenize(strings.Join(doc.Tags, " "))))
	actNameLen := float64(len(Tokenize(doc.ActionNames)))
	actDescLen := float64(len(Tokenize(doc.ActionDescriptions)))

	var total float64
	for _, term := range queryTerms {
		termIDF := idf[term]
		if termIDF == 0 {
			continue
		}

		total += fieldBM25(nameFreq[term], nameLen, avgLengths["name"], termIDF) * WeightName
		total += fieldBM25(tagFreq[term], tagLen, avgLengths["tags"], termIDF) * WeightTags
		total += fieldBM25(actNameFreq[term], actNameLen, avgLengths["action_names"], termIDF) * WeightActionNames
		total += fieldBM25(catFreq[term], catLen, avgLengths["category"], termIDF) * WeightCategory
		total += fieldBM25(actDescFreq[term], actDescLen, avgLengths["action_descriptions"], termIDF) * WeightActionDescriptions
		total += fieldBM25(descFreq[term], descLen, avgLengths["description"], termIDF) * WeightDescription
	}

	// Freshness boost
	total *= freshnessBoost(doc.UpdatedAt)

	return total
}

// fieldBM25 computes the BM25 score for a single field.
func fieldBM25(tf int, fieldLen, avgFieldLen, idf float64) float64 {
	if tf == 0 {
		return 0
	}
	ftf := float64(tf)
	if avgFieldLen == 0 {
		avgFieldLen = 1
	}
	numerator := ftf * (K1 + 1)
	denominator := ftf + K1*(1-B+B*(fieldLen/avgFieldLen))
	return idf * (numerator / denominator)
}

// freshnessBoost returns a multiplier between 1.0 and 1.1 based on how
// recently the document was updated. A document updated today gets 1.1,
// one updated 365+ days ago gets 1.0.
func freshnessBoost(updatedAt time.Time) float64 {
	if updatedAt.IsZero() {
		return 1.0
	}
	daysSince := time.Since(updatedAt).Hours() / 24
	if daysSince < 0 {
		daysSince = 0
	}
	recency := 1.0 - (daysSince / 365.0)
	if recency < 0 {
		recency = 0
	}
	return 1.0 + 0.1*recency
}

// ScoreWithBoosts computes a BM25 score and applies source and trust tier
// boost multipliers. If the base score is zero, boosts are not applied.
func ScoreWithBoosts(query string, doc Document, idf map[string]float64, avgLengths map[string]float64, docCount int, boosts SearchBoosts, isCanonical bool) float64 {
	score := Score(query, doc, idf, avgLengths, docCount)
	if score == 0 {
		return 0
	}
	if isCanonical {
		score *= boosts.CanonicalSource
	}
	score *= boosts.TierBoost(doc.TrustTier)
	return score
}

// computeIDF computes the inverse document frequency for a term using the
// standard BM25 IDF formula: ln((N - n + 0.5) / (n + 0.5) + 1) where N is
// the total number of documents and n is the number containing the term.
func computeIDF(docCount, termDocCount int) float64 {
	n := float64(termDocCount)
	N := float64(docCount)
	return math.Log((N-n+0.5)/(n+0.5) + 1)
}
