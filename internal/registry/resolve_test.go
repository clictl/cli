// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package registry

import "testing"

func TestParseToolVersion(t *testing.T) {
	tests := []struct {
		input       string
		wantName    string
		wantVersion string
	}{
		{input: "github", wantName: "github", wantVersion: ""},
		{input: "github@1.2.0", wantName: "github", wantVersion: "1.2.0"},
		{input: "github@beta", wantName: "github", wantVersion: "beta"},
		{input: "@invalid", wantName: "@invalid", wantVersion: ""},
		{input: "tool@", wantName: "tool", wantVersion: ""},
		{input: "org/tool@2.0.0", wantName: "org/tool", wantVersion: "2.0.0"},
		{input: "tool@1.0.0-rc1", wantName: "tool", wantVersion: "1.0.0-rc1"},
		{input: "", wantName: "", wantVersion: ""},
		{input: "simple-tool", wantName: "simple-tool", wantVersion: ""},
		{input: "tool@v1@extra", wantName: "tool@v1", wantVersion: "extra"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name, version := ParseToolVersion(tt.input)
			if name != tt.wantName {
				t.Errorf("ParseToolVersion(%q) name = %q, want %q", tt.input, name, tt.wantName)
			}
			if version != tt.wantVersion {
				t.Errorf("ParseToolVersion(%q) version = %q, want %q", tt.input, version, tt.wantVersion)
			}
		})
	}
}
