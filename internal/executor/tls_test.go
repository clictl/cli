// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package executor

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildTLSTransport_NilConfig(t *testing.T) {
	_, err := BuildTLSTransport(nil)
	if err == nil {
		t.Fatal("BuildTLSTransport nil config: expected error")
	}
}

func TestBuildTLSTransport_SkipVerify(t *testing.T) {
	cfg := &TLSConfig{
		SkipVerify: true,
	}
	transport, err := BuildTLSTransport(cfg)
	if err != nil {
		t.Fatalf("BuildTLSTransport skip verify: %v", err)
	}
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}
}

func TestBuildTLSTransport_ServerName(t *testing.T) {
	cfg := &TLSConfig{
		ServerName: "custom.example.com",
	}
	transport, err := BuildTLSTransport(cfg)
	if err != nil {
		t.Fatalf("BuildTLSTransport server name: %v", err)
	}
	if transport.TLSClientConfig.ServerName != "custom.example.com" {
		t.Errorf("ServerName: got %q, want %q", transport.TLSClientConfig.ServerName, "custom.example.com")
	}
}

func TestBuildTLSTransport_MinVersion12(t *testing.T) {
	cfg := &TLSConfig{
		MinVersion: "1.2",
	}
	transport, err := BuildTLSTransport(cfg)
	if err != nil {
		t.Fatalf("BuildTLSTransport min version 1.2: %v", err)
	}
	if transport.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion: got %d, want %d (TLS 1.2)", transport.TLSClientConfig.MinVersion, tls.VersionTLS12)
	}
}

func TestBuildTLSTransport_MinVersion13(t *testing.T) {
	cfg := &TLSConfig{
		MinVersion: "1.3",
	}
	transport, err := BuildTLSTransport(cfg)
	if err != nil {
		t.Fatalf("BuildTLSTransport min version 1.3: %v", err)
	}
	if transport.TLSClientConfig.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion: got %d, want %d (TLS 1.3)", transport.TLSClientConfig.MinVersion, tls.VersionTLS13)
	}
}

func TestBuildTLSTransport_InvalidMinVersion(t *testing.T) {
	cfg := &TLSConfig{
		MinVersion: "2.0",
	}
	_, err := BuildTLSTransport(cfg)
	if err == nil {
		t.Fatal("BuildTLSTransport invalid min version: expected error")
	}
}

func TestBuildTLSTransport_EmptyConfig(t *testing.T) {
	cfg := &TLSConfig{}
	transport, err := BuildTLSTransport(cfg)
	if err != nil {
		t.Fatalf("BuildTLSTransport empty config: %v", err)
	}
	if transport == nil {
		t.Fatal("expected non-nil transport for empty config")
	}
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be false by default")
	}
}

func TestParseTLSVersion(t *testing.T) {
	tests := []struct {
		input   string
		want    uint16
		wantErr bool
	}{
		{"1.0", 0, true},
		{"1.1", 0, true},
		{"1.2", tls.VersionTLS12, false},
		{"1.3", tls.VersionTLS13, false},
		{"2.0", 0, true},
		{"", 0, true},
		{"TLS1.2", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseTLSVersion(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTLSVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseTLSVersion(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveFileOrEnv_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cert.pem")
	content := []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	got, err := resolveFileOrEnv(path, "")
	if err != nil {
		t.Fatalf("resolveFileOrEnv file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("resolveFileOrEnv file: got %q, want %q", string(got), string(content))
	}
}

func TestResolveFileOrEnv_Env(t *testing.T) {
	t.Setenv("TEST_CERT_DATA", "cert-data-from-env")

	got, err := resolveFileOrEnv("", "TEST_CERT_DATA")
	if err != nil {
		t.Fatalf("resolveFileOrEnv env: %v", err)
	}
	if string(got) != "cert-data-from-env" {
		t.Errorf("resolveFileOrEnv env: got %q, want %q", string(got), "cert-data-from-env")
	}
}

func TestResolveFileOrEnv_Neither(t *testing.T) {
	got, err := resolveFileOrEnv("", "")
	if err != nil {
		t.Fatalf("resolveFileOrEnv neither: %v", err)
	}
	if got != nil {
		t.Errorf("resolveFileOrEnv neither: expected nil, got %q", string(got))
	}
}

func TestResolveFileOrEnv_MissingFile(t *testing.T) {
	_, err := resolveFileOrEnv("/nonexistent/path/cert.pem", "")
	if err == nil {
		t.Fatal("resolveFileOrEnv missing file: expected error")
	}
}

func TestResolveFileOrEnv_UnsetEnv(t *testing.T) {
	_, err := resolveFileOrEnv("", "DEFINITELY_NOT_SET_ENV_VAR_XYZ")
	if err == nil {
		t.Fatal("resolveFileOrEnv unset env: expected error")
	}
}

func TestResolveFileOrEnv_FileTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cert.pem")
	fileContent := []byte("from-file")
	if err := os.WriteFile(path, fileContent, 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	t.Setenv("TEST_PRECEDENCE_CERT", "from-env")

	got, err := resolveFileOrEnv(path, "TEST_PRECEDENCE_CERT")
	if err != nil {
		t.Fatalf("resolveFileOrEnv precedence: %v", err)
	}
	if string(got) != "from-file" {
		t.Errorf("expected file to take precedence, got %q", string(got))
	}
}
