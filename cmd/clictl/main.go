// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package main is the entry point for the clictl CLI binary.
// It delegates to the command package which defines all CLI commands.
package main

import "github.com/clictl/cli/internal/command"

func main() {
	command.Execute()
}
