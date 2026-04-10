// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package transform provides a pipeline for processing API responses.
//
// Transforms are chainable steps that extract, reshape, filter, and format
// data before it reaches the agent. Each step receives the output of the
// previous step.
package transform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"regexp"
	"strings"
	"text/template"

	"github.com/clictl/cli/internal/logger"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Step represents a single transform operation.
type Step struct {
	// Type field for the new typed format dispatch
	Type string `yaml:"type,omitempty" json:"type,omitempty"`

	Extract        string            `yaml:"extract,omitempty" json:"extract,omitempty"`
	Select         []string          `yaml:"select,omitempty" json:"select,omitempty"`
	Template       string            `yaml:"template,omitempty" json:"template,omitempty"`
	Truncate       *TruncateConfig   `yaml:"truncate,omitempty" json:"truncate,omitempty"`
	Rename         map[string]string `yaml:"rename,omitempty" json:"rename,omitempty"`
	HTMLToMarkdown *HTMLToMDConfig   `yaml:"html_to_markdown,omitempty" json:"html_to_markdown,omitempty"`
	JS             string            `yaml:"js,omitempty" json:"js,omitempty"`

	// DAG fields
	ID          string   `yaml:"id,omitempty" json:"id,omitempty"`
	Input       string   `yaml:"input,omitempty" json:"input,omitempty"`
	DependsOn   []string `yaml:"depends,omitempty" json:"depends,omitempty"`
	Each        bool     `yaml:"each,omitempty" json:"each,omitempty"`
	When        string   `yaml:"when,omitempty" json:"when,omitempty"`
	Concurrency int      `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`

	// Merge fields
	Sources  []string `yaml:"sources,omitempty" json:"sources,omitempty"`
	Strategy string   `yaml:"strategy,omitempty" json:"strategy,omitempty"`
	JoinOn   string   `yaml:"join_on,omitempty" json:"join_on,omitempty"`

	// MCP-aware transform types
	Prefix  string            `yaml:"prefix,omitempty" json:"prefix,omitempty"`   // Namespace tool names
	Only    []string          `yaml:"only,omitempty" json:"only,omitempty"`       // Whitelist filter
	Inject  map[string]any    `yaml:"inject,omitempty" json:"inject,omitempty"`   // Default arg injection
	Redact  []RedactPattern   `yaml:"redact,omitempty" json:"redact,omitempty"`   // Pattern scrubbing
	Cost    *CostConfig       `yaml:"cost,omitempty" json:"cost,omitempty"`       // Token budget

	// Phase 2 typed fields
	Field      string `yaml:"field,omitempty" json:"field,omitempty"`           // sort, unique, group, date_format, base64_decode
	Order      string `yaml:"order,omitempty" json:"order,omitempty"`           // sort: asc/desc
	Filter     string `yaml:"filter,omitempty" json:"filter,omitempty"`         // filter expression
	Separator  string `yaml:"separator,omitempty" json:"separator,omitempty"`   // join, split
	From       string `yaml:"from,omitempty" json:"from,omitempty"`             // date_format: source layout
	To         string `yaml:"to,omitempty" json:"to,omitempty"`                 // date_format: target layout
	CSVHeaders bool   `yaml:"headers,omitempty" json:"headers,omitempty"`       // csv_to_json
	Value      string `yaml:"value,omitempty" json:"value,omitempty"`           // prompt

	// type: pipe (route data through another clictl tool)
	PipeTool   string            `yaml:"pipe_tool,omitempty" json:"pipe_tool,omitempty"`
	PipeAction string            `yaml:"pipe_action,omitempty" json:"pipe_action,omitempty"`
	PipeParams map[string]string `yaml:"pipe_params,omitempty" json:"pipe_params,omitempty"`
	PipeRun    string            `yaml:"pipe_run,omitempty" json:"pipe_run,omitempty"`

	// Flags for json type sub-operations
	FlattenFlag bool `yaml:"flatten,omitempty" json:"flatten,omitempty"`
	UnwrapFlag  bool `yaml:"unwrap,omitempty" json:"unwrap,omitempty"`
}

// RedactPattern describes a single redaction rule.
type RedactPattern struct {
	Match   string `yaml:"match" json:"match"`
	Replace string `yaml:"replace" json:"replace"`
	Type    string `yaml:"type,omitempty" json:"type,omitempty"` // "literal" (default) or "regex"
}

// CostConfig configures the token budget transform.
type CostConfig struct {
	MaxTokens int `yaml:"max_tokens" json:"max_tokens"`
}

// TruncateConfig configures the truncate transform.
type TruncateConfig struct {
	MaxItems  int `yaml:"max_items,omitempty" json:"max_items,omitempty"`
	MaxLength int `yaml:"max_length,omitempty" json:"max_length,omitempty"`
}

// HTMLToMDConfig configures HTML to markdown conversion.
type HTMLToMDConfig struct {
	StripTags []string `yaml:"strip_tags,omitempty" json:"strip_tags,omitempty"`
	BaseURL   string   `yaml:"base_url,omitempty" json:"base_url,omitempty"`
}

// Pipeline is an ordered list of transform steps.
type Pipeline []Step

// Apply runs transforms in sequence. Each step's output feeds into the next.
// If any step has DAG fields (ID, Input, DependsOn), execution switches to the DAG executor
// for parallel branch execution. Most specs use linear pipelines.
func (p Pipeline) Apply(data any) (any, error) {
	if len(p) == 0 {
		return data, nil
	}

	dag, err := NewDAGExecutor(p)
	if err != nil {
		return nil, err
	}
	if dag != nil {
		return dag.Execute(context.Background(), data)
	}

	// Linear execution for pipelines without DAG fields
	current := data
	for i, step := range p {
		stepType := step.Type
		if stepType == "" {
			stepType = inferLegacyStepType(step)
		}
		logger.Debug("transform step start", logger.F("step", i+1), logger.F("type", stepType), logger.F("input_type", fmt.Sprintf("%T", current)))
		var err error
		current, err = applyStep(step, current)
		if err != nil {
			logger.Error("transform step failed", logger.F("step", i+1), logger.F("type", stepType), logger.F("error", err.Error()))
			return nil, fmt.Errorf("transform step %d: %w", i+1, err)
		}
		logger.Debug("transform step done", logger.F("step", i+1), logger.F("type", stepType), logger.F("output_type", fmt.Sprintf("%T", current)))
	}
	return current, nil
}

// inferLegacyStepType returns a descriptive name for legacy (untyped) steps.
func inferLegacyStepType(step Step) string {
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
	default:
		return "unknown"
	}
}

func applyStep(step Step, data any) (any, error) {
	// New typed dispatch: if Type is set, use it for routing
	if step.Type != "" {
		return applyTypedStep(step, data)
	}

	// Legacy flat format dispatch
	if step.Extract != "" {
		return applyExtract(step.Extract, data)
	}
	if len(step.Select) > 0 {
		return applySelect(step.Select, data)
	}
	if step.Template != "" {
		return applyTemplate(step.Template, data)
	}
	if step.Truncate != nil {
		return applyTruncate(step.Truncate, data)
	}
	if len(step.Rename) > 0 {
		return applyRename(step.Rename, data)
	}
	if step.HTMLToMarkdown != nil {
		return applyHTMLToMarkdown(step.HTMLToMarkdown, data)
	}
	if step.JS != "" {
		return applyJS(step.JS, data)
	}
	if step.Prefix != "" {
		return applyPrefix(step.Prefix, data)
	}
	if len(step.Only) > 0 {
		return applyOnly(step.Only, data)
	}
	if len(step.Inject) > 0 {
		return applyInject(step.Inject, data)
	}
	if len(step.Redact) > 0 {
		return applyRedact(step.Redact, data)
	}
	if step.Cost != nil {
		return applyCost(step.Cost, data)
	}
	return data, nil
}

// applyTypedStep dispatches based on step.Type for the new typed format.
func applyTypedStep(step Step, data any) (any, error) {
	switch step.Type {
	case "json":
		// JSON type can have extract, select, rename, only, inject, flatten, unwrap
		if step.Extract != "" {
			data2, err := applyExtract(step.Extract, data)
			if err != nil {
				return nil, err
			}
			data = data2
		}
		if len(step.Select) > 0 {
			data2, err := applySelect(step.Select, data)
			if err != nil {
				return nil, err
			}
			data = data2
		}
		if len(step.Rename) > 0 {
			data2, err := applyRename(step.Rename, data)
			if err != nil {
				return nil, err
			}
			data = data2
		}
		if len(step.Only) > 0 {
			data2, err := applyOnly(step.Only, data)
			if err != nil {
				return nil, err
			}
			data = data2
		}
		if len(step.Inject) > 0 {
			data2, err := applyInject(step.Inject, data)
			if err != nil {
				return nil, err
			}
			data = data2
		}
		if step.FlattenFlag {
			data2, err := applyFlatten(data)
			if err != nil {
				return nil, err
			}
			data = data2
		}
		if step.UnwrapFlag {
			data2, err := applyUnwrap(data)
			if err != nil {
				return nil, err
			}
			data = data2
		}
		return data, nil
	case "truncate":
		return applyTruncate(step.Truncate, data)
	case "template":
		return applyTemplate(step.Template, data)
	case "html_to_markdown":
		return applyHTMLToMarkdown(step.HTMLToMarkdown, data)
	case "js":
		return applyJS(step.JS, data)
	case "prefix":
		return applyPrefix(step.Prefix, data)
	case "redact":
		return applyRedact(step.Redact, data)
	case "cost":
		return applyCost(step.Cost, data)
	case "sort":
		return applySort(step.Field, step.Order, data)
	case "filter":
		return applyFilter(step.Filter, data)
	case "unique":
		return applyUnique(step.Field, data)
	case "group":
		return applyGroup(step.Field, data)
	case "count":
		return applyCount(data)
	case "join":
		return applyJoin(step.Separator, data)
	case "split":
		return applySplit(step.Separator, data)
	case "flatten":
		return applyFlatten(data)
	case "unwrap":
		return applyUnwrap(data)
	case "format":
		return applyFormat(step.Template, data)
	case "prompt":
		return applyPrompt(step.Value, data)
	case "date_format":
		return applyDateFormat(step.Field, step.From, step.To, data)
	case "xml_to_json":
		return applyXMLToJSON(data)
	case "csv_to_json":
		return applyCSVToJSON(step.CSVHeaders, data)
	case "base64_decode":
		return applyBase64Decode(step.Field, data)
	case "markdown_to_text":
		return applyMarkdownToText(data)
	case "pipe":
		return applyPipe(step, data)
	default:
		return nil, fmt.Errorf("unknown transform type: %q", step.Type)
	}
}

// Extract handles JSONPath-like extraction from parsed data.
// Exported for reuse in the test command's assert engine.
func Extract(path string, data any) (any, error) {
	return applyExtract(path, data)
}

// applyExtract handles JSONPath-like extraction.
// Supports: $.field, $.field.nested, $.field[0], $.array[*].field
func applyExtract(path string, data any) (any, error) {
	if !strings.HasPrefix(path, "$") {
		return nil, fmt.Errorf("extract path must start with $, got %q", path)
	}

	// Remove leading "$" and optional "."
	path = strings.TrimPrefix(path, "$")
	path = strings.TrimPrefix(path, ".")

	if path == "" {
		return data, nil
	}

	return navigatePath(path, data)
}

func navigatePath(path string, data any) (any, error) {
	parts := splitPath(path)
	current := data

	for _, part := range parts {
		var err error
		current, err = accessField(part, current)
		if err != nil {
			return nil, err
		}
	}
	return current, nil
}

// splitPath splits a dotted path, respecting array indices.
// "data.items[0].name" -> ["data", "items[0]", "name"]
func splitPath(path string) []string {
	var parts []string
	var current strings.Builder

	for i := 0; i < len(path); i++ {
		ch := path[i]
		if ch == '.' && current.Len() > 0 {
			parts = append(parts, current.String())
			current.Reset()
		} else if ch != '.' {
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func accessField(field string, data any) (any, error) {
	// Check for array index: field[0] or field[*]
	if idx := strings.Index(field, "["); idx >= 0 {
		name := field[:idx]
		bracket := field[idx:]

		// First navigate to the field
		var target any
		if name == "" {
			target = data
		} else {
			obj, ok := data.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("expected object for field %q, got %T", name, data)
			}
			v, exists := obj[name]
			if !exists {
				return nil, fmt.Errorf("field %q not found", name)
			}
			target = v
		}

		arr, ok := target.([]any)
		if !ok {
			return nil, fmt.Errorf("expected array for %q, got %T", field, target)
		}

		// Handle [*] wildcard
		if bracket == "[*]" {
			return arr, nil
		}

		// Handle [N] numeric index
		var index int
		if _, err := fmt.Sscanf(bracket, "[%d]", &index); err != nil {
			return nil, fmt.Errorf("invalid array index in %q", field)
		}
		if index < 0 || index >= len(arr) {
			return nil, fmt.Errorf("array index %d out of range (length %d)", index, len(arr))
		}
		return arr[index], nil
	}

	obj, ok := data.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected object for field %q, got %T", field, data)
	}
	v, exists := obj[field]
	if !exists {
		return nil, fmt.Errorf("field %q not found", field)
	}
	return v, nil
}

// applySelect keeps only specified fields from objects or arrays of objects.
func applySelect(fields []string, data any) (any, error) {
	switch v := data.(type) {
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			obj, ok := item.(map[string]any)
			if !ok {
				result[i] = item
				continue
			}
			result[i] = pickFields(obj, fields)
		}
		return result, nil
	case map[string]any:
		return pickFields(v, fields), nil
	default:
		return data, nil
	}
}

func pickFields(obj map[string]any, fields []string) map[string]any {
	result := make(map[string]any, len(fields))
	for _, f := range fields {
		// Support dotted fields: "user.login"
		if strings.Contains(f, ".") {
			parts := strings.SplitN(f, ".", 2)
			if sub, ok := obj[parts[0]]; ok {
				if subObj, ok := sub.(map[string]any); ok {
					if val, ok := subObj[parts[1]]; ok {
						result[f] = val
					}
				}
			}
		} else if val, ok := obj[f]; ok {
			result[f] = val
		}
	}
	return result
}

// applyTemplate renders a Go template with the data.
func applyTemplate(tmplStr string, data any) (any, error) {
	tmpl, err := template.New("transform").Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}
	return strings.TrimSpace(buf.String()), nil
}

// applyTruncate limits array length or string length.
func applyTruncate(cfg *TruncateConfig, data any) (any, error) {
	if cfg == nil {
		return data, nil
	}
	if cfg.MaxItems > 0 {
		if arr, ok := data.([]any); ok {
			if len(arr) > cfg.MaxItems {
				return arr[:cfg.MaxItems], nil
			}
			return arr, nil
		}
	}
	if cfg.MaxLength > 0 {
		if s, ok := data.(string); ok {
			if len(s) > cfg.MaxLength {
				return s[:cfg.MaxLength] + "...", nil
			}
			return s, nil
		}
	}
	return data, nil
}

// applyRename renames fields in objects or arrays of objects.
func applyRename(mapping map[string]string, data any) (any, error) {
	switch v := data.(type) {
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			obj, ok := item.(map[string]any)
			if !ok {
				result[i] = item
				continue
			}
			result[i] = renameFields(obj, mapping)
		}
		return result, nil
	case map[string]any:
		return renameFields(v, mapping), nil
	default:
		return data, nil
	}
}

func renameFields(obj map[string]any, mapping map[string]string) map[string]any {
	result := make(map[string]any, len(obj))
	for k, v := range obj {
		if newName, ok := mapping[k]; ok {
			result[newName] = v
		} else {
			result[k] = v
		}
	}
	return result
}

// applyHTMLToMarkdown converts HTML to simple markdown.
// This is a basic implementation that handles common elements.
func applyHTMLToMarkdown(cfg *HTMLToMDConfig, data any) (any, error) {
	s, ok := data.(string)
	if !ok {
		// Try to extract string from JSON
		if b, err := json.Marshal(data); err == nil {
			s = string(b)
		} else {
			return data, nil
		}
	}

	baseURL := ""
	if cfg != nil {
		baseURL = cfg.BaseURL
	}
	result := htmlToMDWithBase(s, baseURL)
	return strings.TrimSpace(result), nil
}

// htmlToMD converts HTML to markdown using a proper HTML parser.
// Handles attributes, entities, nested tags, links, images, tables, and lists.
func htmlToMD(rawHTML string) string {
	return htmlToMDWithBase(rawHTML, "")
}

// htmlToMDWithBase converts HTML to markdown, resolving relative URLs against baseURL.
func htmlToMDWithBase(rawHTML, baseURL string) string {
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return stripTags(rawHTML)
	}
	var parsedBase *neturl.URL
	if baseURL != "" {
		parsedBase, _ = neturl.Parse(baseURL)
	}
	var buf strings.Builder
	renderNode(&buf, doc, renderState{baseURL: parsedBase})
	return collapseBlankLines(buf.String())
}

// renderState tracks context during recursive HTML-to-markdown rendering.
type renderState struct {
	inPre     bool
	inLink    bool
	listDepth int
	ordered   bool
	olIndex   int
	baseURL   *neturl.URL // for resolving relative URLs
}

func renderNode(buf *strings.Builder, n *html.Node, st renderState) {
	switch n.Type {
	case html.TextNode:
		text := n.Data
		if !st.inPre {
			// Collapse whitespace outside <pre>
			text = collapseWhitespace(text)
		}
		buf.WriteString(text)
		return

	case html.CommentNode:
		// Skip HTML comments
		return

	case html.ElementNode:
		// Skip invisible elements
		switch n.DataAtom {
		case atom.Script, atom.Style, atom.Head, atom.Meta, atom.Link, atom.Noscript:
			return
		// Skip embedded objects and iframes
		case atom.Iframe, atom.Object, atom.Embed:
			return
		// Skip SVG and MathML inline content
		case atom.Svg, atom.Math:
			return
		// Skip form controls (not meaningful as text)
		case atom.Input, atom.Textarea, atom.Select, atom.Option:
			return
		}

		// Skip elements hidden via inline style
		if isStyleHidden(n) {
			return
		}

		// Skip elements with aria-hidden="true"
		if getAttr(n, "aria-hidden") == "true" {
			return
		}

		// Skip elements with hidden attribute
		if hasAttr(n, "hidden") {
			return
		}

		switch n.DataAtom {
		case atom.H1:
			buf.WriteString("\n\n# ")
			renderChildren(buf, n, st)
			buf.WriteString("\n\n")
			return
		case atom.H2:
			buf.WriteString("\n\n## ")
			renderChildren(buf, n, st)
			buf.WriteString("\n\n")
			return
		case atom.H3:
			buf.WriteString("\n\n### ")
			renderChildren(buf, n, st)
			buf.WriteString("\n\n")
			return
		case atom.H4:
			buf.WriteString("\n\n#### ")
			renderChildren(buf, n, st)
			buf.WriteString("\n\n")
			return
		case atom.H5:
			buf.WriteString("\n\n##### ")
			renderChildren(buf, n, st)
			buf.WriteString("\n\n")
			return
		case atom.H6:
			buf.WriteString("\n\n###### ")
			renderChildren(buf, n, st)
			buf.WriteString("\n\n")
			return

		case atom.P:
			buf.WriteString("\n\n")
			renderChildren(buf, n, st)
			buf.WriteString("\n\n")
			return

		case atom.Br:
			buf.WriteString("\n")
			return

		case atom.Hr:
			buf.WriteString("\n\n---\n\n")
			return

		case atom.Strong, atom.B:
			buf.WriteString("**")
			renderChildren(buf, n, st)
			buf.WriteString("**")
			return

		case atom.Em, atom.I:
			buf.WriteString("*")
			renderChildren(buf, n, st)
			buf.WriteString("*")
			return

		case atom.Code:
			if !st.inPre {
				buf.WriteString("`")
				renderChildren(buf, n, st)
				buf.WriteString("`")
			} else {
				renderChildren(buf, n, st)
			}
			return

		case atom.Pre:
			buf.WriteString("\n\n```\n")
			st.inPre = true
			renderChildren(buf, n, st)
			buf.WriteString("\n```\n\n")
			return

		case atom.Blockquote:
			buf.WriteString("\n\n> ")
			renderChildren(buf, n, st)
			buf.WriteString("\n\n")
			return

		case atom.A:
			href := getAttr(n, "href")
			if href == "" || st.inLink {
				renderChildren(buf, n, st)
				return
			}
			href = resolveURL(href, st.baseURL)
			// Render children to check if link has visible text
			var linkBuf strings.Builder
			st.inLink = true
			renderChildren(&linkBuf, n, st)
			linkText := strings.TrimSpace(linkBuf.String())
			if linkText == "" {
				// Skip links with no visible text (e.g. vote arrows)
				return
			}
			buf.WriteString("[")
			buf.WriteString(linkText)
			buf.WriteString("](")
			buf.WriteString(href)
			buf.WriteString(")")
			return

		case atom.Img:
			alt := getAttr(n, "alt")
			src := getAttr(n, "src")
			// Fallback to data-src for lazy-loaded images
			if src == "" || isLikelyPlaceholder(src) {
				if dataSrc := getAttr(n, "data-src"); dataSrc != "" {
					src = dataSrc
				}
			}
			// Skip spacer/tracking images (common in layout-table HTML)
			if src == "" || strings.HasSuffix(src, "s.gif") || (alt == "" && isLikelySpacerSrc(src)) {
				return
			}
			src = resolveURL(src, st.baseURL)
			buf.WriteString("![")
			buf.WriteString(alt)
			buf.WriteString("](")
			buf.WriteString(src)
			buf.WriteString(")")
			return

		case atom.Ul:
			buf.WriteString("\n")
			st.listDepth++
			st.ordered = false
			renderChildren(buf, n, st)
			buf.WriteString("\n")
			return

		case atom.Ol:
			buf.WriteString("\n")
			childSt := st
			childSt.listDepth++
			childSt.ordered = true
			idx := 1
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.DataAtom == atom.Li {
					indent := strings.Repeat("  ", childSt.listDepth-1)
					buf.WriteString(fmt.Sprintf("\n%s%d. ", indent, idx))
					idx++
					renderChildren(buf, c, childSt)
				} else {
					renderNode(buf, c, childSt)
				}
			}
			buf.WriteString("\n")
			return

		case atom.Li:
			indent := strings.Repeat("  ", st.listDepth-1)
			buf.WriteString("\n" + indent + "- ")
			renderChildren(buf, n, st)
			return

		case atom.Table, atom.Tbody, atom.Thead, atom.Tfoot:
			// Treat tables as transparent containers. HTML tables are often
			// used for layout (e.g. Hacker News), not data. Producing markdown
			// table syntax from layout tables creates invalid output.
			buf.WriteString("\n")
			renderChildren(buf, n, st)
			buf.WriteString("\n")
			return

		case atom.Tr:
			renderChildren(buf, n, st)
			buf.WriteString("\n")
			return

		case atom.Th, atom.Td:
			renderChildren(buf, n, st)
			buf.WriteString(" ")
			return

		// Semantic HTML5: del/ins/mark/abbr
		case atom.Del, atom.S:
			buf.WriteString("~~")
			renderChildren(buf, n, st)
			buf.WriteString("~~")
			return

		case atom.Ins:
			renderChildren(buf, n, st)
			return

		case atom.Mark:
			buf.WriteString("==")
			renderChildren(buf, n, st)
			buf.WriteString("==")
			return

		case atom.Abbr:
			renderChildren(buf, n, st)
			title := getAttr(n, "title")
			if title != "" {
				buf.WriteString(" (")
				buf.WriteString(title)
				buf.WriteString(")")
			}
			return

		// Semantic HTML5: figure/figcaption
		case atom.Figure:
			buf.WriteString("\n\n")
			renderChildren(buf, n, st)
			buf.WriteString("\n\n")
			return

		case atom.Figcaption:
			buf.WriteString("\n*")
			renderChildren(buf, n, st)
			buf.WriteString("*\n")
			return

		// Semantic HTML5: details/summary
		case atom.Details:
			buf.WriteString("\n\n")
			renderChildren(buf, n, st)
			buf.WriteString("\n\n")
			return

		case atom.Summary:
			buf.WriteString("**")
			renderChildren(buf, n, st)
			buf.WriteString("**\n\n")
			return

		// Semantic HTML5: time
		case atom.Time:
			renderChildren(buf, n, st)
			return

		// Semantic HTML5: address
		case atom.Address:
			buf.WriteString("\n")
			renderChildren(buf, n, st)
			buf.WriteString("\n")
			return

		// Button: render text content
		case atom.Button:
			buf.WriteString("[")
			renderChildren(buf, n, st)
			buf.WriteString("]")
			return

		// Sup/Sub
		case atom.Sup:
			buf.WriteString("^(")
			renderChildren(buf, n, st)
			buf.WriteString(")")
			return

		case atom.Sub:
			buf.WriteString("~(")
			renderChildren(buf, n, st)
			buf.WriteString(")")
			return

		case atom.Div, atom.Section, atom.Article, atom.Main, atom.Header, atom.Footer, atom.Nav, atom.Aside, atom.Span:
			renderChildren(buf, n, st)
			return
		}
	}

	// Default: render children
	renderChildren(buf, n, st)
}

func renderChildren(buf *strings.Builder, n *html.Node, st renderState) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		renderNode(buf, c, st)
	}
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// resolveURL resolves a potentially relative URL against the base URL.
// If base is nil or href is already absolute, returns href unchanged.
func resolveURL(href string, base *neturl.URL) string {
	if base == nil || href == "" {
		return href
	}
	// Already absolute
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "//") {
		return href
	}
	// Skip non-http schemes (mailto:, javascript:, data:, etc.)
	if strings.Contains(href, ":") && !strings.HasPrefix(href, "/") {
		return href
	}
	ref, err := neturl.Parse(href)
	if err != nil {
		return href
	}
	return base.ResolveReference(ref).String()
}

// hasAttr returns true if the element has the given attribute (any value).
func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

// isStyleHidden checks if an element has inline CSS that hides it.
func isStyleHidden(n *html.Node) bool {
	style := getAttr(n, "style")
	if style == "" {
		return false
	}
	lower := strings.ToLower(style)
	// display: none
	if strings.Contains(lower, "display") && strings.Contains(lower, "none") {
		return true
	}
	// visibility: hidden
	if strings.Contains(lower, "visibility") && strings.Contains(lower, "hidden") {
		return true
	}
	return false
}

// isLikelyPlaceholder returns true if src looks like a placeholder/loading image.
func isLikelyPlaceholder(src string) bool {
	lower := strings.ToLower(src)
	return strings.Contains(lower, "placeholder") ||
		strings.Contains(lower, "loading") ||
		strings.Contains(lower, "lazy") ||
		strings.HasPrefix(lower, "data:image/") // data URI placeholders
}

// isLikelySpacerSrc returns true if the image src looks like a spacer/tracking pixel.
func isLikelySpacerSrc(src string) bool {
	lower := strings.ToLower(src)
	return strings.Contains(lower, "spacer") ||
		strings.Contains(lower, "blank") ||
		strings.Contains(lower, "1x1") ||
		strings.Contains(lower, "pixel") ||
		strings.Contains(lower, "clear.gif") ||
		strings.Contains(lower, "shim.gif")
}

// collapseWhitespace replaces runs of whitespace with a single space.
func collapseWhitespace(s string) string {
	var buf strings.Builder
	lastWS := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !lastWS {
				buf.WriteByte(' ')
				lastWS = true
			}
		} else {
			buf.WriteRune(r)
			lastWS = false
		}
	}
	return buf.String()
}

// collapseBlankLines reduces 3+ consecutive newlines to 2.
func collapseBlankLines(s string) string {
	re := regexp.MustCompile(`\n{3,}`)
	return re.ReplaceAllString(s, "\n\n")
}

// stripTags is a fallback that removes HTML tags.
func stripTags(s string) string {
	var buf strings.Builder
	inTag := false
	for _, ch := range s {
		if ch == '<' {
			inTag = true
		} else if ch == '>' {
			inTag = false
		} else if !inTag {
			buf.WriteRune(ch)
		}
	}
	return buf.String()
}

// applyPrefix adds a prefix to "name" fields in objects or arrays of objects.
// Used for namespacing MCP tool names.
func applyPrefix(prefix string, data any) (any, error) {
	switch v := data.(type) {
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			obj, ok := item.(map[string]any)
			if !ok {
				result[i] = item
				continue
			}
			out := make(map[string]any, len(obj))
			for k, val := range obj {
				out[k] = val
			}
			if name, ok := out["name"].(string); ok {
				out["name"] = prefix + name
			}
			result[i] = out
		}
		return result, nil
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[k] = val
		}
		if name, ok := out["name"].(string); ok {
			out["name"] = prefix + name
		}
		return out, nil
	case string:
		return prefix + v, nil
	default:
		return data, nil
	}
}

// applyOnly filters arrays of objects to only include items whose "name" field
// matches one of the allowed names.
func applyOnly(allowed []string, data any) (any, error) {
	arr, ok := data.([]any)
	if !ok {
		return data, nil
	}
	allowSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowSet[name] = true
	}
	var result []any
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := obj["name"].(string); ok && allowSet[name] {
			result = append(result, item)
		}
	}
	return result, nil
}

// applyInject merges default values into objects. Existing keys are not overwritten.
func applyInject(defaults map[string]any, data any) (any, error) {
	switch v := data.(type) {
	case map[string]any:
		result := make(map[string]any, len(v)+len(defaults))
		for k, val := range defaults {
			result[k] = val
		}
		for k, val := range v {
			result[k] = val // user values override defaults
		}
		return result, nil
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			obj, ok := item.(map[string]any)
			if !ok {
				result[i] = item
				continue
			}
			merged := make(map[string]any, len(obj)+len(defaults))
			for k, val := range defaults {
				merged[k] = val
			}
			for k, val := range obj {
				merged[k] = val
			}
			result[i] = merged
		}
		return result, nil
	default:
		return data, nil
	}
}

// applyRedact applies pattern-based string replacement to the data.
// Supports literal and regex patterns.
func applyRedact(patterns []RedactPattern, data any) (any, error) {
	s, isString := data.(string)
	if !isString {
		b, err := json.Marshal(data)
		if err != nil {
			return data, nil
		}
		s = string(b)
	}

	for _, p := range patterns {
		if p.Type == "regex" {
			re, err := regexp.Compile(p.Match)
			if err != nil {
				return nil, fmt.Errorf("invalid redact regex %q: %w", p.Match, err)
			}
			s = re.ReplaceAllString(s, p.Replace)
		} else {
			s = strings.ReplaceAll(s, p.Match, p.Replace)
		}
	}

	// If the input was not a string, try to parse back as JSON
	if !isString {
		var parsed any
		if json.Unmarshal([]byte(s), &parsed) == nil {
			return parsed, nil
		}
	}
	return s, nil
}

// applyCost truncates data to fit within a token budget.
// Uses chars/4 heuristic for token estimation.
func applyCost(cfg *CostConfig, data any) (any, error) {
	if cfg == nil || cfg.MaxTokens <= 0 {
		return data, nil
	}

	s, ok := data.(string)
	if !ok {
		b, err := json.Marshal(data)
		if err != nil {
			return data, nil
		}
		s = string(b)
	}

	maxChars := cfg.MaxTokens * 4 // chars/4 heuristic
	if len(s) > maxChars {
		removed := (len(s) - maxChars) / 4
		s = s[:maxChars] + fmt.Sprintf("... [truncated, ~%d tokens removed]", removed)
	}

	return s, nil
}

// ParseSteps converts a raw transform config (either a single map or a list)
// into a Pipeline.
func ParseSteps(raw any) (Pipeline, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil

	case map[string]any:
		// Single step in old format: { extract: "$.data" }
		step, err := mapToStep(v)
		if err != nil {
			return nil, err
		}
		return Pipeline{step}, nil

	case []any:
		// List of steps
		var pipeline Pipeline
		for i, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("transform step %d: expected object, got %T", i+1, item)
			}
			step, err := mapToStep(m)
			if err != nil {
				return nil, fmt.Errorf("transform step %d: %w", i+1, err)
			}
			pipeline = append(pipeline, step)
		}
		return pipeline, nil

	default:
		return nil, fmt.Errorf("transform: expected object or array, got %T", raw)
	}
}

func mapToStep(m map[string]any) (Step, error) {
	var step Step

	// New typed format: "type" field is the primary dispatch key
	if v, ok := m["type"]; ok {
		step.Type, _ = v.(string)
	}

	// Phase 2 typed fields
	if v, ok := m["field"]; ok {
		step.Field, _ = v.(string)
	}
	if v, ok := m["order"]; ok {
		step.Order, _ = v.(string)
	}
	if v, ok := m["filter"]; ok {
		step.Filter, _ = v.(string)
	}
	if v, ok := m["separator"]; ok {
		step.Separator, _ = v.(string)
	}
	if v, ok := m["from"]; ok {
		step.From, _ = v.(string)
	}
	if v, ok := m["to"]; ok {
		step.To, _ = v.(string)
	}
	if v, ok := m["headers"]; ok {
		step.CSVHeaders, _ = v.(bool)
	}
	if v, ok := m["value"]; ok {
		step.Value, _ = v.(string)
	}
	if v, ok := m["flatten"]; ok {
		step.FlattenFlag, _ = v.(bool)
	}
	if v, ok := m["unwrap"]; ok {
		step.UnwrapFlag, _ = v.(bool)
	}

	if v, ok := m["extract"]; ok {
		step.Extract, _ = v.(string)
	}
	if v, ok := m["select"]; ok {
		switch arr := v.(type) {
		case []any:
			for _, item := range arr {
				if s, ok := item.(string); ok {
					step.Select = append(step.Select, s)
				}
			}
		case []string:
			step.Select = arr
		}
	}
	if v, ok := m["template"]; ok {
		step.Template, _ = v.(string)
	}
	if v, ok := m["truncate"]; ok {
		if cfg, ok := v.(map[string]any); ok {
			step.Truncate = &TruncateConfig{}
			if n, ok := cfg["max_items"]; ok {
				step.Truncate.MaxItems = toInt(n)
			}
			if n, ok := cfg["max_length"]; ok {
				step.Truncate.MaxLength = toInt(n)
			}
		}
	}
	// Also handle typed format: type: truncate with max_items/max_length at top level
	if step.Type == "truncate" && step.Truncate == nil {
		mi := toInt(m["max_items"])
		ml := toInt(m["max_length"])
		if mi > 0 || ml > 0 {
			step.Truncate = &TruncateConfig{MaxItems: mi, MaxLength: ml}
		}
	}
	if v, ok := m["rename"]; ok {
		switch cfg := v.(type) {
		case map[string]any:
			step.Rename = make(map[string]string, len(cfg))
			for key, val := range cfg {
				if s, ok := val.(string); ok {
					step.Rename[key] = s
				}
			}
		case map[string]string:
			step.Rename = cfg
		}
	}
	if v, ok := m["html_to_markdown"]; ok {
		step.HTMLToMarkdown = &HTMLToMDConfig{}
		if cfg, ok := v.(map[string]any); ok {
			if tags, ok := cfg["strip_tags"].([]any); ok {
				for _, t := range tags {
					if s, ok := t.(string); ok {
						step.HTMLToMarkdown.StripTags = append(step.HTMLToMarkdown.StripTags, s)
					}
				}
			}
			if url, ok := cfg["base_url"].(string); ok {
				step.HTMLToMarkdown.BaseURL = url
			}
		}
	}

	if v, ok := m["js"]; ok {
		step.JS, _ = v.(string)
	}

	if v, ok := m["prefix"]; ok {
		step.Prefix, _ = v.(string)
	}

	if v, ok := m["only"]; ok {
		switch arr := v.(type) {
		case []any:
			for _, item := range arr {
				if s, ok := item.(string); ok {
					step.Only = append(step.Only, s)
				}
			}
		case []string:
			step.Only = arr
		}
	}

	if v, ok := m["inject"]; ok {
		switch cfg := v.(type) {
		case map[string]any:
			step.Inject = cfg
		case map[string]string:
			step.Inject = make(map[string]any, len(cfg))
			for k, val := range cfg {
				step.Inject[k] = val
			}
		}
	}

	if v, ok := m["redact"]; ok {
		switch rv := v.(type) {
		case []any:
			for _, item := range rv {
				rm, ok := item.(map[string]any)
				if !ok {
					continue
				}
				p := RedactPattern{}
				if match, ok := rm["match"].(string); ok {
					p.Match = match
				}
				if replace, ok := rm["replace"].(string); ok {
					p.Replace = replace
				}
				if ptype, ok := rm["type"].(string); ok {
					p.Type = ptype
				}
				step.Redact = append(step.Redact, p)
			}
		case map[string]any:
			// Simple key-value format: {"pattern": "replacement"}
			for match, replace := range rv {
				if r, ok := replace.(string); ok {
					step.Redact = append(step.Redact, RedactPattern{Match: match, Replace: r})
				}
			}
		}
	}

	if v, ok := m["cost"]; ok {
		if cfg, ok := v.(map[string]any); ok {
			step.Cost = &CostConfig{}
			if n, ok := cfg["max_tokens"]; ok {
				step.Cost.MaxTokens = toInt(n)
			}
		}
	}

	return step, nil
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
	}
}
