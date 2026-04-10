// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/clictl/cli/internal/transform"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var transformCmd = &cobra.Command{
	Use:   "transform",
	Short: "Test transforms on JSON data",
	Long: `Apply a transform pipeline to JSON input from stdin.

Useful for developing and testing transforms before adding them to a tool spec.

Examples:
  echo '{"data": [1, 2, 3]}' | clictl transform --extract '$.data'
  echo '{"items": [{"name": "a"}, {"name": "b"}]}' | clictl transform --extract '$.items' --select 'name'
  cat response.json | clictl transform --file transforms.yaml
  clictl run my-tool get-data --raw | clictl transform --extract '$.results' --truncate 5`,
	RunE: func(cmd *cobra.Command, args []string) error {
		visualize, _ := cmd.Flags().GetBool("visualize")

		// --visualize mode: output Mermaid diagram, no stdin needed
		if visualize {
			file, _ := cmd.Flags().GetString("file")
			if file == "" {
				return fmt.Errorf("--visualize requires --file")
			}
			return visualizeTransforms(cmd, file)
		}

		// Read stdin
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}

		var data any
		if err := json.Unmarshal(input, &data); err != nil {
			return fmt.Errorf("parsing JSON input: %w", err)
		}

		pipeline, err := buildPipelineFromFlags(cmd)
		if err != nil {
			return err
		}

		result, err := pipeline.Apply(data)
		if err != nil {
			return fmt.Errorf("transform failed: %w", err)
		}

		// Output
		switch v := result.(type) {
		case string:
			fmt.Fprintln(cmd.OutOrStdout(), v)
		default:
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			enc.Encode(result)
		}

		return nil
	},
}

func buildPipelineFromFlags(cmd *cobra.Command) (transform.Pipeline, error) {
	// Check for --file first (YAML file with full pipeline)
	file, _ := cmd.Flags().GetString("file")
	if file != "" {
		return loadPipelineFromFile(file)
	}

	// Build pipeline from individual flags
	var pipeline transform.Pipeline

	if extract, _ := cmd.Flags().GetString("extract"); extract != "" {
		pipeline = append(pipeline, transform.Step{Extract: extract})
	}
	if selectFields, _ := cmd.Flags().GetStringSlice("select"); len(selectFields) > 0 {
		pipeline = append(pipeline, transform.Step{Select: selectFields})
	}
	if rename, _ := cmd.Flags().GetStringToString("rename"); len(rename) > 0 {
		pipeline = append(pipeline, transform.Step{Rename: rename})
	}
	if maxItems, _ := cmd.Flags().GetInt("truncate"); maxItems > 0 {
		pipeline = append(pipeline, transform.Step{Truncate: &transform.TruncateConfig{MaxItems: maxItems}})
	}
	if tmpl, _ := cmd.Flags().GetString("template"); tmpl != "" {
		pipeline = append(pipeline, transform.Step{Template: tmpl})
	}
	if htmlToMD, _ := cmd.Flags().GetBool("html-to-markdown"); htmlToMD {
		pipeline = append(pipeline, transform.Step{HTMLToMarkdown: &transform.HTMLToMDConfig{}})
	}
	if jsCode, _ := cmd.Flags().GetString("js"); jsCode != "" {
		pipeline = append(pipeline, transform.Step{JS: jsCode})
	}

	if len(pipeline) == 0 {
		return nil, fmt.Errorf("no transforms specified. Use --extract, --select, --template, --truncate, --rename, --html-to-markdown, --js, or --file")
	}

	return pipeline, nil
}

func loadPipelineFromFile(path string) (transform.Pipeline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading transform file: %w", err)
	}

	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing transform YAML: %w", err)
	}

	return transform.ParseSteps(raw)
}

// visualizeTransforms outputs a Mermaid diagram of the transform pipeline from a YAML file.
func visualizeTransforms(cmd *cobra.Command, file string) error {
	pipeline, err := loadPipelineFromFile(file)
	if err != nil {
		return err
	}

	if len(pipeline) == 0 {
		return fmt.Errorf("no transforms found in %s", file)
	}

	// Try to build a DAG executor (returns nil if linear)
	dag, err := transform.NewDAGExecutor(pipeline)
	if err != nil {
		return fmt.Errorf("building DAG: %w", err)
	}

	if dag != nil {
		fmt.Fprintln(cmd.OutOrStdout(), "```mermaid")
		fmt.Fprint(cmd.OutOrStdout(), dag.Mermaid())
		fmt.Fprintln(cmd.OutOrStdout(), "```")
		return nil
	}

	// Linear pipeline: generate a simple flow diagram
	var sb strings.Builder
	sb.WriteString("```mermaid\ngraph TD\n")
	sb.WriteString("  input([input])\n")
	for i, step := range pipeline {
		typeName := step.Type
		if typeName == "" {
			typeName = inferStepType(step)
		}
		id := fmt.Sprintf("step_%d", i)
		sb.WriteString(fmt.Sprintf("  %s[%s]\n", id, typeName))
		if i == 0 {
			sb.WriteString(fmt.Sprintf("  input --> %s\n", id))
		} else {
			sb.WriteString(fmt.Sprintf("  step_%d --> %s\n", i-1, id))
		}
	}
	lastID := fmt.Sprintf("step_%d", len(pipeline)-1)
	sb.WriteString("  output([output])\n")
	sb.WriteString(fmt.Sprintf("  %s --> output\n", lastID))
	sb.WriteString("```\n")
	fmt.Fprint(cmd.OutOrStdout(), sb.String())
	return nil
}

// inferStepType returns a human-readable type name for a Step without an explicit Type.
func inferStepType(step transform.Step) string {
	switch {
	case step.Extract != "":
		return "extract"
	case len(step.Select) > 0:
		return "select"
	case step.Template != "":
		return "template"
	case step.Truncate != nil:
		return "truncate"
	case len(step.Rename) > 0:
		return "rename"
	case step.HTMLToMarkdown != nil:
		return "html_to_markdown"
	case step.JS != "":
		return "js"
	case step.Prefix != "":
		return "prefix"
	case len(step.Only) > 0:
		return "only"
	case len(step.Redact) > 0:
		return "redact"
	case step.Cost != nil:
		return "cost"
	default:
		return "step"
	}
}

func init() {
	transformCmd.Flags().String("extract", "", "JSONPath expression (e.g., $.data.items)")
	transformCmd.Flags().StringSlice("select", nil, "Fields to keep (e.g., name,status)")
	transformCmd.Flags().StringToString("rename", nil, "Rename fields (e.g., dt=date,temp_max=high)")
	transformCmd.Flags().Int("truncate", 0, "Max array items to keep")
	transformCmd.Flags().String("template", "", "Go template string")
	transformCmd.Flags().Bool("html-to-markdown", false, "Convert HTML to markdown")
	transformCmd.Flags().String("file", "", "YAML file with transform pipeline")
	transformCmd.Flags().String("js", "", "JavaScript transform function")
	transformCmd.Flags().Bool("visualize", false, "Output a Mermaid diagram of the transform DAG")

	rootCmd.AddCommand(transformCmd)
}
