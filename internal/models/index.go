// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package models

// RegistryMeta is the registry.yaml metadata.
type RegistryMeta struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Maintainer  string `yaml:"maintainer" json:"maintainer"`
	Homepage    string `yaml:"homepage" json:"homepage"`
	APIURL      string `yaml:"api_url" json:"api_url"`
	Version     int    `yaml:"version" json:"version"`
}

// Index is the index.json structure.
type Index struct {
	SchemaVersion int                   `json:"schema_version"`
	GeneratedAt   string                `json:"generated_at"`
	Registries    []IndexRegistry       `json:"registries"`
	Specs         map[string]IndexEntry `json:"specs"`
}

// IndexRegistry is a reference to a registry source.
type IndexRegistry struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Maintainer  string `json:"maintainer"`
}

// IndexEntry is a single spec entry in the index.
type IndexEntry struct {
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Type        string   `json:"type"`
	Protocol    string   `json:"protocol"`
	Tags        []string `json:"tags"`
	Path        string   `json:"path"`
	Auth        string   `json:"auth"`
	SHA256             string   `json:"sha256"`
	ActionNames        []string `json:"action_names,omitempty"`
	ActionDescriptions []string `json:"action_descriptions,omitempty"`
}
