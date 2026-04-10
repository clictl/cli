// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package search

import (
	"strings"
	"unicode"
)

// Tokenize splits text into lowercase tokens, splitting on spaces, hyphens,
// underscores, and punctuation. Empty tokens are discarded.
func Tokenize(text string) []string {
	lower := strings.ToLower(text)
	parts := strings.FieldsFunc(lower, func(r rune) bool {
		return unicode.IsSpace(r) || r == '-' || r == '_' || unicode.IsPunct(r)
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// TokenizeField tokenizes and returns a map of term to frequency count.
func TokenizeField(text string) map[string]int {
	tokens := Tokenize(text)
	freq := make(map[string]int, len(tokens))
	for _, t := range tokens {
		freq[t]++
	}
	return freq
}
