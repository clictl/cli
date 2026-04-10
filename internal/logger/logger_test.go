// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package logger

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	// Point logger output at the pipe too
	global.mu.Lock()
	global.output = w
	global.mu.Unlock()

	fn()

	w.Close()
	os.Stderr = old
	global.mu.Lock()
	global.output = os.Stderr
	global.mu.Unlock()

	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

func TestLoggerDisabledByDefault(t *testing.T) {
	Init(false, "info", "text", "")
	output := captureStderr(func() {
		Info("should not appear")
	})
	if output != "" {
		t.Errorf("expected no output when disabled, got %q", output)
	}
}

func TestLoggerTextFormat(t *testing.T) {
	Init(true, "debug", "text", "")
	defer Init(false, "info", "text", "")

	output := captureStderr(func() {
		Info("hello world", F("key", "value"))
	})
	if !strings.Contains(output, "[INFO]") {
		t.Errorf("expected [INFO], got %q", output)
	}
	if !strings.Contains(output, "hello world") {
		t.Errorf("expected 'hello world', got %q", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("expected 'key=value', got %q", output)
	}
}

func TestLoggerJSONFormat(t *testing.T) {
	Init(true, "debug", "json", "")
	defer Init(false, "info", "text", "")

	output := captureStderr(func() {
		Warn("something bad", F("code", 42))
	})

	var entry map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &entry); err != nil {
		t.Fatalf("expected valid JSON, got %q: %v", output, err)
	}
	if entry["level"] != "warn" {
		t.Errorf("expected level 'warn', got %v", entry["level"])
	}
	if entry["msg"] != "something bad" {
		t.Errorf("expected msg 'something bad', got %v", entry["msg"])
	}
	if entry["code"] != float64(42) {
		t.Errorf("expected code 42, got %v", entry["code"])
	}
}

func TestLevelFiltering(t *testing.T) {
	Init(true, "warn", "text", "")
	defer Init(false, "info", "text", "")

	output := captureStderr(func() {
		Debug("debug msg")
		Info("info msg")
		Warn("warn msg")
		Error("error msg")
	})
	if strings.Contains(output, "debug msg") {
		t.Error("debug should be filtered")
	}
	if strings.Contains(output, "info msg") {
		t.Error("info should be filtered")
	}
	if !strings.Contains(output, "warn msg") {
		t.Error("warn should appear")
	}
	if !strings.Contains(output, "error msg") {
		t.Error("error should appear")
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("CLICTL_LOG", "1")
	t.Setenv("CLICTL_LOG_LEVEL", "error")
	t.Setenv("CLICTL_LOG_FORMAT", "json")

	Init(false, "info", "text", "")
	defer Init(false, "info", "text", "")

	if !global.enabled {
		t.Error("CLICTL_LOG=1 should enable logging")
	}
	if global.level != LevelError {
		t.Errorf("expected error level, got %d", global.level)
	}
	if global.format != "json" {
		t.Errorf("expected json format, got %s", global.format)
	}
}

func TestNoFieldsText(t *testing.T) {
	Init(true, "debug", "text", "")
	defer Init(false, "info", "text", "")

	output := captureStderr(func() {
		Info("simple message")
	})
	if !strings.Contains(output, "simple message") {
		t.Errorf("expected message, got %q", output)
	}
}

func TestLogToFile(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "test.log")

	Init(true, "debug", "text", logFile)
	defer func() {
		Close()
		Init(false, "info", "text", "")
	}()

	Info("file log message", F("key", "val"))
	Close()

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "file log message") {
		t.Errorf("expected message in file, got %q", content)
	}
	if !strings.Contains(content, "key=val") {
		t.Errorf("expected field in file, got %q", content)
	}
}

func TestLogToFileJSON(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "test.json")

	Init(true, "info", "json", logFile)
	defer func() {
		Close()
		Init(false, "info", "text", "")
	}()

	Error("json file entry", F("code", 500))
	Close()

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &entry); err != nil {
		t.Fatalf("expected valid JSON in file, got %q: %v", data, err)
	}
	if entry["msg"] != "json file entry" {
		t.Errorf("expected msg, got %v", entry["msg"])
	}
}

func TestEnvFileOverride(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "env.log")
	t.Setenv("CLICTL_LOG", "1")
	t.Setenv("CLICTL_LOG_FILE", logFile)

	Init(false, "info", "text", "")
	defer func() {
		Close()
		Init(false, "info", "text", "")
	}()

	Info("env file test")
	Close()

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if !strings.Contains(string(data), "env file test") {
		t.Errorf("expected message in env file, got %q", data)
	}
}

// ---------------------------------------------------------------------------
// URL sanitization tests (merged from sanitize_test.go)
// ---------------------------------------------------------------------------

func TestSanitizeURL_RedactsSensitiveParams(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string // param that should be redacted
	}{
		{"token", "https://api.example.com/data?token=abc123", "token=%5BREDACTED%5D"},
		{"api_key", "https://api.example.com/data?api_key=secret", "api_key=%5BREDACTED%5D"},
		{"access_token", "https://api.example.com/cb?access_token=xyz", "access_token=%5BREDACTED%5D"},
		{"password", "https://api.example.com/?password=hunter2", "password=%5BREDACTED%5D"},
		{"key", "https://api.example.com/?key=k123", "key=%5BREDACTED%5D"},
		{"secret", "https://api.example.com/?secret=s456", "secret=%5BREDACTED%5D"},
		{"auth", "https://api.example.com/?auth=bearer-token", "auth=%5BREDACTED%5D"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeURL(tt.input)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("SanitizeURL(%q) = %q, expected it to contain %q", tt.input, result, tt.contains)
			}
			// The original secret value should not appear
			if strings.Contains(result, "abc123") || strings.Contains(result, "secret") && tt.name != "secret" {
				t.Errorf("SanitizeURL(%q) = %q, still contains original secret value", tt.input, result)
			}
		})
	}
}

func TestSanitizeURL_PreservesNonSensitiveParams(t *testing.T) {
	input := "https://api.example.com/search?q=hello&page=2&format=json"
	result := SanitizeURL(input)

	if !strings.Contains(result, "q=hello") {
		t.Errorf("expected q=hello preserved, got %q", result)
	}
	if !strings.Contains(result, "page=2") {
		t.Errorf("expected page=2 preserved, got %q", result)
	}
	if !strings.Contains(result, "format=json") {
		t.Errorf("expected format=json preserved, got %q", result)
	}
}

func TestSanitizeURL_MixedParams(t *testing.T) {
	input := "https://api.example.com/data?q=search&token=secret123&page=1"
	result := SanitizeURL(input)

	if !strings.Contains(result, "q=search") {
		t.Errorf("expected q=search preserved, got %q", result)
	}
	if !strings.Contains(result, "page=1") {
		t.Errorf("expected page=1 preserved, got %q", result)
	}
	if strings.Contains(result, "secret123") {
		t.Errorf("expected secret123 to be redacted, got %q", result)
	}
	if !strings.Contains(result, "token=%5BREDACTED%5D") {
		t.Errorf("expected token to be redacted, got %q", result)
	}
}

func TestSanitizeURL_InvalidURL(t *testing.T) {
	result := SanitizeURL("://not-a-url")
	if result != "[invalid-url]" {
		t.Errorf("SanitizeURL with invalid URL = %q, want %q", result, "[invalid-url]")
	}
}

func TestSanitizeURL_RedactsUserinfo(t *testing.T) {
	input := "https://admin:password123@api.example.com/data"
	result := SanitizeURL(input)

	if strings.Contains(result, "admin") {
		t.Errorf("expected username to be redacted, got %q", result)
	}
	if strings.Contains(result, "password123") {
		t.Errorf("expected password to be redacted, got %q", result)
	}
	// Userinfo is URL-encoded in the output
	if !strings.Contains(result, "REDACTED") {
		t.Errorf("expected REDACTED in output, got %q", result)
	}
	if !strings.Contains(result, "api.example.com") {
		t.Errorf("expected host preserved, got %q", result)
	}
}

func TestSanitizeURL_NoParams(t *testing.T) {
	input := "https://api.example.com/data"
	result := SanitizeURL(input)

	if result != input {
		t.Errorf("SanitizeURL(%q) = %q, expected unchanged", input, result)
	}
}

func TestSanitizeURL_EmptyString(t *testing.T) {
	result := SanitizeURL("")
	// Empty string is a valid URL (relative reference), should not error
	if result == "[invalid-url]" {
		t.Errorf("empty string should not be treated as invalid")
	}
}
