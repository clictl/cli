// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/suggest"
)

// toolSuggestion returns a "Did you mean?" message for a misspelled tool name,
// or an empty string if no close matches are found.
func toolSuggestion(input string, cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	candidates := registry.AllToolNames(cfg)
	return suggest.FormatMessage(input, candidates)
}

// hasMultipleSources returns true if the results come from more than one toolbox.
func hasMultipleSources(results []models.SearchResult) bool {
	if len(results) == 0 {
		return false
	}
	first := results[0].Source
	for _, r := range results[1:] {
		if r.Source != first {
			return true
		}
	}
	return false
}
