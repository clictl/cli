// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package sandbox

import (
	"fmt"
	"path/filepath"
	"strings"
)

// shellOperators contains shell operators that indicate a compound command.
// When these are present, only an exact match against the full raw command is allowed.
var shellOperators = []string{"|", ";", "&&", "||", "`", "$("}

// MatchBashCommand checks if a command matches any pattern in the allowlist.
// Patterns support glob matching:
//   - "*" matches any arguments
//   - Exact match for commands without wildcards
//
// Commands containing shell operators (|, ;, &&, ||, backticks, $()) are rejected
// unless the entire piped command matches a pattern exactly.
func MatchBashCommand(command string, patterns []string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}

	if len(patterns) == 0 {
		return false
	}

	// Check for shell operators
	if containsShellOperator(command) {
		// Security: only allow exact match on the full raw command
		for _, p := range patterns {
			if command == p {
				return true
			}
		}
		return false
	}

	// Simple command (no operators): match against each pattern
	for _, pattern := range patterns {
		if matchPattern(command, pattern) {
			return true
		}
	}

	return false
}

// containsShellOperator checks if the command string contains any shell operators.
func containsShellOperator(command string) bool {
	for _, op := range shellOperators {
		if strings.Contains(command, op) {
			return true
		}
	}
	return false
}

// matchPattern matches a command against a single allowlist pattern.
// - If pattern has no "*", exact match is required.
// - If pattern ends with " *", the command must start with the prefix before " *".
// - If pattern ends with "*" (no space before), prefix match on the command.
// - If pattern has "*" in the middle, use filepath.Match for glob matching.
func matchPattern(command, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}

	// No wildcard: exact match
	if !strings.Contains(pattern, "*") {
		return command == pattern
	}

	// Pattern is just "*": matches everything
	if pattern == "*" {
		return true
	}

	// Trailing wildcard: "cmd *" or "cmd*"
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		// "npm run *" -> prefix is "npm run ", command must start with that prefix
		if strings.HasPrefix(command, prefix) {
			return true
		}
		// Also try: pattern "npm *" should match "npm install"
		// The prefix already includes the trailing part before *, so HasPrefix is sufficient.
		return false
	}

	// Wildcard in the middle: use filepath.Match on the full command
	matched, err := filepath.Match(pattern, command)
	if err != nil {
		return false
	}
	return matched
}

// ValidateBashAllowPatterns checks that patterns are syntactically valid.
// It returns an error if any pattern is empty or contains an invalid glob.
func ValidateBashAllowPatterns(patterns []string) error {
	for i, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			return fmt.Errorf("pattern at index %d is empty", i)
		}

		// Check for valid glob syntax by attempting a match
		if strings.Contains(p, "*") || strings.Contains(p, "?") || strings.Contains(p, "[") {
			// filepath.Match returns an error for invalid patterns like "[invalid"
			_, err := filepath.Match(p, "test")
			if err != nil {
				return fmt.Errorf("pattern %q at index %d has invalid glob syntax: %w", p, i, err)
			}
		}
	}
	return nil
}
