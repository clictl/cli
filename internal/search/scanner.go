// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package search

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ScanToolboxDirs walks toolbox cache directories and builds Documents from
// YAML specs. Reads from the provided paths (typically ~/.clictl/toolboxes/
// subdirectories). Files without required fields (name, version) are skipped.
func ScanToolboxDirs(paths []string) ([]Document, error) {
	var docs []Document

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

			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("reading %s: %w", path, readErr)
			}

			doc, parseErr := parseSpecToDocument(data, path, info.ModTime())
			if parseErr != nil {
				// Skip files that don't have required fields.
				return nil
			}

			docs = append(docs, *doc)
			return nil
		})
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("scanning %s: %w", root, err)
		}
	}

	return docs, nil
}

// parseSpecToDocument parses YAML content and builds a Document.
// Returns an error if the required field (name) is missing.
func parseSpecToDocument(content []byte, sourcePath string, modTime time.Time) (*Document, error) {
	var raw map[string]interface{}
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	name := yamlStr(raw, "name")
	if name == "" {
		return nil, fmt.Errorf("missing required field: name")
	}

	namespace := yamlStr(raw, "namespace")

	// Extract action names and descriptions.
	var actionNames, actionDescs []string
	if actions, ok := raw["actions"].([]interface{}); ok {
		for _, a := range actions {
			if am, ok := a.(map[string]interface{}); ok {
				if n := yamlStr(am, "name"); n != "" {
					actionNames = append(actionNames, n)
				}
				if d := yamlStr(am, "description"); d != "" {
					actionDescs = append(actionDescs, d)
				}
			}
		}
	}

	doc := &Document{
		Name:               name,
		Description:        yamlStr(raw, "description"),
		Category:           yamlStr(raw, "category"),
		Version:            yamlStr(raw, "version"),
		Protocol:           yamlStr(raw, "protocol"),
		Auth:               yamlStr(raw, "auth"),
		Tags:               yamlStrSlice(raw, "tags"),
		ActionNames:        strings.Join(actionNames, " "),
		ActionDescriptions: strings.Join(actionDescs, " "),
		Namespace:          namespace,
		TrustTier:          deriveTrustTier(namespace),
		SourcePath:         sourcePath,
		UpdatedAt:          modTime,
		CreatedAt:          modTime,
	}

	return doc, nil
}

// deriveTrustTier returns the trust tier based on the namespace.
// The "clictl" namespace is official; all others are community.
// Verified/certified status requires backend data and is not available locally.
func deriveTrustTier(namespace string) string {
	if namespace == "clictl" {
		return "official"
	}
	return "community"
}

// yamlStr extracts a string value from a map, returning "" if absent or
// not a string. Numeric values are converted via Sprintf.
func yamlStr(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// yamlStrSlice extracts a []string from a map value that is a []interface{}.
func yamlStrSlice(m map[string]interface{}, key string) []string {
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
