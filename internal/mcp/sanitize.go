// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import "regexp"

// ansiRegex matches ANSI escape sequences including:
// - CSI sequences: ESC [ ... <letter>
// - OSC sequences: ESC ] ... BEL
// - Other escape sequences: ESC <char> ...
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[^[\]()][^\x1b]*`)

// StripANSI removes all ANSI escape sequences from a string.
// This prevents MCP servers from injecting terminal control sequences
// that could manipulate the host terminal or confuse LLM clients.
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}
