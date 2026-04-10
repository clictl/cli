// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package codegen generates typed SDK code from tool specifications.
// Supports TypeScript and Python output for use in code mode, where LLM agents
// write code against typed APIs instead of making individual tool calls.
package codegen

import (
	"strings"
	"unicode"
)

// ToCamelCase converts a kebab-case or snake_case name to camelCase.
// Examples: "list-repos" -> "listRepos", "get_user" -> "getUser"
func ToCamelCase(s string) string {
	parts := splitName(s)
	if len(parts) == 0 {
		return s
	}
	result := strings.ToLower(parts[0])
	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		result += strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return result
}

// ToPascalCase converts a kebab-case or snake_case name to PascalCase.
// Examples: "list-repos" -> "ListRepos", "get_user" -> "GetUser"
func ToPascalCase(s string) string {
	parts := splitName(s)
	var result string
	for _, p := range parts {
		if p == "" {
			continue
		}
		result += strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return result
}

// ToSnakeCase converts a kebab-case name to snake_case.
// Examples: "list-repos" -> "list_repos"
func ToSnakeCase(s string) string {
	return strings.Join(splitName(s), "_")
}

// splitName splits on hyphens, underscores, and camelCase boundaries.
func splitName(s string) []string {
	var parts []string
	var current strings.Builder
	for i, r := range s {
		if r == '-' || r == '_' {
			if current.Len() > 0 {
				parts = append(parts, strings.ToLower(current.String()))
				current.Reset()
			}
			continue
		}
		if unicode.IsUpper(r) && i > 0 {
			if current.Len() > 0 {
				parts = append(parts, strings.ToLower(current.String()))
				current.Reset()
			}
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		parts = append(parts, strings.ToLower(current.String()))
	}
	return parts
}

// TSType maps a spec param type to a TypeScript type string.
func TSType(specType string) string {
	switch strings.ToLower(specType) {
	case "int", "integer", "float", "number":
		return "number"
	case "bool", "boolean":
		return "boolean"
	case "array":
		return "string[]"
	case "object":
		return "Record<string, any>"
	default:
		return "string"
	}
}

// PyType maps a spec param type to a Python type hint string.
func PyType(specType string) string {
	switch strings.ToLower(specType) {
	case "int", "integer":
		return "int"
	case "float", "number":
		return "float"
	case "bool", "boolean":
		return "bool"
	case "array":
		return "list[str]"
	case "object":
		return "dict[str, Any]"
	default:
		return "str"
	}
}
