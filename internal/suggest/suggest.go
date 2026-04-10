// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package suggest provides "did you mean?" suggestions for tool names.
//
// Uses Levenshtein distance to find close matches from a list of known
// tool names. Same algorithm used by Cobra, git, and kubectl.
package suggest

import (
	"fmt"
	"sort"
	"strings"
)

// match holds a candidate and its edit distance.
type match struct {
	Name     string
	Distance int
}

// Tools returns up to maxResults tool name suggestions for a misspelled input.
// It checks all candidates using Levenshtein distance and prefix matching.
// Returns nil if no close matches are found.
func Tools(input string, candidates []string, maxResults int) []string {
	if len(candidates) == 0 || input == "" {
		return nil
	}

	inputLower := strings.ToLower(input)
	threshold := maxDistance(len(input))

	var matches []match
	for _, c := range candidates {
		cLower := strings.ToLower(c)

		// Exact prefix match gets priority
		if strings.HasPrefix(cLower, inputLower) || strings.HasPrefix(inputLower, cLower) {
			matches = append(matches, match{Name: c, Distance: 0})
			continue
		}

		d := levenshtein(inputLower, cLower)
		if d <= threshold {
			matches = append(matches, match{Name: c, Distance: d})
		}
	}

	if len(matches) == 0 {
		return nil
	}

	// Sort by distance (closest first), then alphabetically
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Distance != matches[j].Distance {
			return matches[i].Distance < matches[j].Distance
		}
		return matches[i].Name < matches[j].Name
	})

	if len(matches) > maxResults {
		matches = matches[:maxResults]
	}

	result := make([]string, len(matches))
	for i, m := range matches {
		result[i] = m.Name
	}
	return result
}

// FormatMessage returns a "Did you mean?" message, or empty string if no suggestions.
func FormatMessage(input string, candidates []string) string {
	suggestions := Tools(input, candidates, 3)
	// Filter out exact matches (tool exists but resolution failed for another reason)
	var filtered []string
	for _, s := range suggestions {
		if !strings.EqualFold(s, input) {
			filtered = append(filtered, s)
		}
	}
	suggestions = filtered
	if len(suggestions) == 0 {
		return ""
	}
	if len(suggestions) == 1 {
		return fmt.Sprintf("\nDid you mean?\n\t%s\n", suggestions[0])
	}
	var b strings.Builder
	b.WriteString("\nDid you mean one of these?\n")
	for _, s := range suggestions {
		b.WriteString("\t")
		b.WriteString(s)
		b.WriteString("\n")
	}
	return b.String()
}

// maxDistance returns the Levenshtein distance threshold based on input length.
// Short names (1-3 chars) allow 1 edit, medium (4-6) allow 2, longer allow 3.
func maxDistance(inputLen int) int {
	if inputLen <= 3 {
		return 1
	}
	if inputLen <= 6 {
		return 2
	}
	return 3
}

// levenshtein computes the Levenshtein edit distance between two strings.
// Same algorithm as cobra's internal ld() function.
func levenshtein(s, t string) int {
	if s == t {
		return 0
	}
	if len(s) == 0 {
		return len(t)
	}
	if len(t) == 0 {
		return len(s)
	}

	sr := []rune(s)
	tr := []rune(t)

	// Two-row optimization
	prev := make([]int, len(tr)+1)
	curr := make([]int, len(tr)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(sr); i++ {
		curr[0] = i
		for j := 1; j <= len(tr); j++ {
			cost := 1
			if sr[i-1] == tr[j-1] {
				cost = 0
			}
			curr[j] = min3(
				curr[j-1]+1,   // insert
				prev[j]+1,     // delete
				prev[j-1]+cost, // substitute
			)
		}
		prev, curr = curr, prev
	}
	return prev[len(tr)]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
