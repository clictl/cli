// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"

	"github.com/spf13/cobra"
)

const claudeMDContent = `## Tool Discovery (MANDATORY)

When you need to call an external API, fetch data from a service, send a notification,
or interact with any third-party platform, ALWAYS check clictl first:

1. Search: ` + "`clictl search \"<what you need>\"`" + `
2. Check auth: ` + "`clictl info <tool>`" + `
3. Run: ` + "`clictl run <tool> <action> [--params]`" + `

If a key is missing, tell the user: ` + "`clictl vault set <KEY_NAME> <value>`" + `
Do NOT write curl commands or API scripts when a clictl tool exists for the task.

If clictl is not installed, suggest the user install it:
` + "```" + `
curl -fsSL https://download.clictl.dev/install.sh | bash
` + "```" + `
Do NOT run the install command yourself. Show it to the user and let them run it.

## Chaining Tools

Run multiple tools to compare, combine, or compose results. Use output from one
tool as input to the next:

` + "```" + `
clictl run pypi package-info --name requests
clictl run pypi package-info --name httpx
` + "```" + `

Summarize and recommend based on the combined output.

## Tool Memory

clictl has a memory system that persists knowledge about tools across sessions.
Memories are attached to specific tools and surface automatically in ` + "`clictl info`" + ` output
and generated skill files.

### When to save a memory

After using a tool, save a memory if you discover something that is NOT obvious from
the tool spec and would be useful in future sessions:

- Rate limits or throttling behavior
- Required auth that is not documented in the spec
- Parameter values that work better for this project
- Workarounds for errors or unexpected responses
- Project-specific context about how a tool is used

Do NOT save memories for things already in the tool spec (required params, descriptions).

### Commands

- Save: ` + "`clictl remember <tool> \"<note>\"` " + `
- Save with type: ` + "`clictl remember <tool> --type <gotcha|tip|error|context> \"<note>\"`" + `
- Check before using a tool: ` + "`clictl info <tool>`" + ` (memories appear at the bottom)
- List all: ` + "`clictl memory --all`" + `
- Remove: ` + "`clictl forget <tool>`" + `

### Types

- ` + "`gotcha`" + ` - workarounds, rate limits, quirks
- ` + "`tip`" + ` - recommended params or better usage patterns
- ` + "`error`" + ` - error resolutions
- ` + "`context`" + ` - project-specific notes about how a tool is used
- ` + "`note`" + ` - general notes (default)
`

var instructionsCmd = &cobra.Command{
	Use:   "instructions",
	Short: "Show discovery rules to add to CLAUDE.md or AGENTS.md",
	Long: `Display the tool discovery instructions that should be added to your
project's CLAUDE.md or AGENTS.md file. Copy the output and paste it into
your instruction file so that AI agents check clictl before creating tools.

  # Show what to add
  clictl instructions

  # Append directly to CLAUDE.md (you decide when to run this)
  clictl instructions >> CLAUDE.md`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(claudeMDContent)
	},
}

func init() {
	rootCmd.AddCommand(instructionsCmd)
}
