// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxSpecFileSize = 10 * 1024 * 1024 // 10MB

// ScannedTool holds the metadata extracted from a single tool spec YAML file,
// along with its file path and content hash.
type ScannedTool struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description,omitempty"`
	Category    string   `json:"category,omitempty"`
	Protocol    string   `json:"protocol,omitempty"`
	Auth        string   `json:"auth,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Path        string   `json:"path"`
	SHA256      string   `json:"sha256"`
	SkillMDPath string   `json:"skill_md_path,omitempty"` // Path to SKILL.md alongside this spec
}

// ScanSpecs walks each directory in paths, finds *.yaml files, parses their
// frontmatter, computes SHA256, and returns a slice of ScannedTool. Files
// that lack required fields (name, version) are silently skipped.
//
// When a SKILL.md file is found alongside a spec YAML, it is recorded in
// the ScannedTool.SkillMDPath field for later use during install.
func ScanSpecs(paths []string) ([]ScannedTool, error) {
	var tools []ScannedTool

	for _, root := range paths {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(info.Name(), ".yaml") && !strings.HasSuffix(info.Name(), ".yml") {
				return nil
			}
			// Skip hidden files and meta files.
			if strings.HasPrefix(info.Name(), ".") {
				return nil
			}

			if info.Size() > maxSpecFileSize {
				// Skip oversized files
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading %s: %w", path, err)
			}

			tool, err := ParseSpecFrontmatter(data)
			if err != nil {
				// Skip files that don't have required fields.
				return nil
			}

			tool.Path = path
			tool.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))

			// C1.14: Check for SKILL.md alongside the spec YAML
			dir := filepath.Dir(path)
			skillMDPath := filepath.Join(dir, "SKILL.md")
			if _, statErr := os.Stat(skillMDPath); statErr == nil {
				tool.SkillMDPath = skillMDPath
			}

			tools = append(tools, *tool)
			return nil
		})
		if err != nil {
			// If the path doesn't exist, skip it rather than failing.
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("scanning %s: %w", root, err)
		}
	}

	return tools, nil
}

// ParseSpecFrontmatter parses YAML content and extracts tool metadata.
// Returns an error if the required fields (name, version) are missing.
func ParseSpecFrontmatter(content []byte) (*ScannedTool, error) {
	if len(content) > maxSpecFileSize {
		return nil, fmt.Errorf("spec file too large: %d bytes (max %d)", len(content), maxSpecFileSize)
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	name, _ := raw["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("missing required field: name")
	}

	version := yamlString(raw, "version")
	if version == "" {
		return nil, fmt.Errorf("missing required field: version")
	}

	tool := &ScannedTool{
		Name:        name,
		Version:     version,
		Description: yamlString(raw, "description"),
		Category:    yamlString(raw, "category"),
		Protocol:    yamlString(raw, "protocol"),
		Auth:        yamlString(raw, "auth"),
		Tags:        yamlStringSlice(raw, "tags"),
	}

	return tool, nil
}

// yamlString extracts a string value from a map, returning "" if absent or
// not a string. Numeric values are converted via Sprintf.
func yamlString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	// Handle numeric versions like 1.0
	return fmt.Sprintf("%v", v)
}

// yamlStringSlice extracts a []string from a map value that is a []interface{}.
func yamlStringSlice(m map[string]interface{}, key string) []string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	list, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
