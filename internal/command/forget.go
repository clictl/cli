// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"strconv"

	"github.com/clictl/cli/internal/memory"
	"github.com/spf13/cobra"
)

var forgetCmd = &cobra.Command{
	Use:   "forget <tool> [index]",
	Short: "Remove memories for a tool",
	Long:  "Remove a specific memory by index, or use --all to clear all memories for a tool.",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		tool := args[0]
		clearAll, _ := cmd.Flags().GetBool("all")

		if clearAll {
			if err := memory.Clear(tool); err != nil {
				return fmt.Errorf("clearing memories: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "All memories cleared for %s\n", tool)
			return nil
		}

		if len(args) == 2 {
			index, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("invalid index: %s (use a number from 'clictl memory %s')", args[1], tool)
			}
			// Convert from 1-based display to 0-based storage
			if err := memory.Remove(tool, index-1); err != nil {
				return fmt.Errorf("removing memory: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Memory %d removed for %s\n", index, tool)
			return nil
		}

		// No index given, show memories so user can pick
		entries, err := memory.Load(tool)
		if err != nil {
			return fmt.Errorf("loading memories: %w", err)
		}
		if len(entries) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "No memories for %s\n", tool)
			return nil
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Memories for %s:\n", tool)
		for i, e := range entries {
			fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s\n", i+1, e.Note)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "\nUse 'clictl forget %s <number>' to remove one, or 'clictl forget %s --all' to clear all.\n", tool, tool)
		return nil
	},
}

func init() {
	forgetCmd.Flags().Bool("all", false, "Remove all memories for the tool")
	rootCmd.AddCommand(forgetCmd)
}
