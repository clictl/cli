// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/codegen"
	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/registry"
)

var codegenLang string
var codegenOut string
var codegenAll bool

var codegenCmd = &cobra.Command{
	Use:   "codegen [tool...]",
	Short: "Generate typed SDK code from tool specs",
	Long: `Generate TypeScript or Python SDK code from tool specifications.

The generated code includes typed interfaces for all action parameters
and function declarations that match the tool's API endpoints. Each
action can target a different URL with different auth.

  # Generate TypeScript to stdout
  clictl codegen github --lang typescript

  # Generate Python to a file
  clictl codegen stripe --lang python --out stripe_sdk.py

  # Generate for all installed tools
  clictl codegen --all --lang typescript --out ./sdk/`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !codegenAll && len(args) == 0 {
			return fmt.Errorf("provide a tool name or use --all")
		}

		cfg, err := config.Load()
		if err != nil {
			cfg = &config.Config{}
		}

		cache := registry.NewCache(cfg.CacheDir)
		client := registry.NewClient(config.ResolveAPIURL("", cfg), cache, false)
		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token != "" {
			client.AuthToken = token
		}

		// Resolve tool names
		toolNames := args
		if codegenAll {
			toolNames = loadInstalled()
			if len(toolNames) == 0 {
				return fmt.Errorf("no installed tools found")
			}
		}

		// Load all specs
		var specs []*models.ToolSpec
		for _, name := range toolNames {
			spec, _, err := client.GetSpecYAML(cmd.Context(), name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not load %s: %v\n", name, err)
				continue
			}
			specs = append(specs, spec)
		}

		if len(specs) == 0 {
			return fmt.Errorf("no valid specs loaded")
		}

		// Generate for each spec
		for _, spec := range specs {
			var output string
			switch codegenLang {
			case "typescript", "ts":
				output = codegen.GenerateTypeScript(spec)
			case "python", "py":
				output = codegen.GeneratePython(spec)
			default:
				return fmt.Errorf("unsupported language %q (use typescript or python)", codegenLang)
			}

			if codegenOut == "" {
				fmt.Print(output)
				continue
			}

			// For --all with --out as directory, generate one file per spec
			outPath := codegenOut
			if len(specs) > 1 {
				ext := ".ts"
				if codegenLang == "python" || codegenLang == "py" {
					ext = ".py"
				}
				outPath = filepath.Join(codegenOut, strings.ReplaceAll(spec.Name, "-", "_")+ext)
			}

			dir := filepath.Dir(outPath)
			if dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("creating output directory: %w", err)
				}
			}
			if err := os.WriteFile(outPath, []byte(output), 0o644); err != nil {
				return fmt.Errorf("writing %s: %w", outPath, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Generated %s SDK for %s -> %s\n", codegenLang, spec.Name, outPath)
		}

		return nil
	},
}

func init() {
	codegenCmd.Flags().StringVar(&codegenLang, "lang", "typescript", "Output language (typescript, python)")
	codegenCmd.Flags().StringVar(&codegenOut, "out", "", "Output file path or directory (default: stdout)")
	codegenCmd.Flags().BoolVar(&codegenAll, "all", false, "Generate for all installed tools")
	rootCmd.AddCommand(codegenCmd)
}
