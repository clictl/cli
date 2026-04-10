// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package logger provides structured logging for the clictl CLI.
//
// Logging is off by default. Enable it in ~/.clictl/config.yaml:
//
//	log:
//	  enabled: true
//	  level: debug    # debug, info, warn, error
//	  format: json    # text or json
//
// Or via environment variables:
//
//	CLICTL_LOG=1              # enable
//	CLICTL_LOG_LEVEL=debug    # level
//	CLICTL_LOG_FORMAT=json    # format
package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Level represents a log severity level.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelOff
)

var levelNames = map[Level]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
}

var levelFromString = map[string]Level{
	"debug": LevelDebug,
	"info":  LevelInfo,
	"warn":  LevelWarn,
	"error": LevelError,
	"off":   LevelOff,
}

// Logger is a structured logger with level filtering and format support.
type Logger struct {
	mu      sync.Mutex
	level   Level
	format  string // "text" or "json"
	enabled bool
	output  *os.File // where logs are written (stderr or a file)
	ownFile bool     // true if we opened the file and should close it
}

var global = &Logger{
	level:   LevelInfo,
	format:  "text",
	enabled: false,
	output:  os.Stderr,
}

// Init configures the global logger. Call once at startup.
func Init(enabled bool, level string, format string, file string) {
	global.mu.Lock()
	defer global.mu.Unlock()

	// Close previous file if we opened one
	if global.ownFile && global.output != nil {
		global.output.Close()
		global.ownFile = false
	}
	global.output = os.Stderr
	global.enabled = enabled

	// Environment overrides
	if os.Getenv("CLICTL_LOG") == "1" {
		global.enabled = true
	}
	if envLevel := os.Getenv("CLICTL_LOG_LEVEL"); envLevel != "" {
		level = envLevel
	}
	if envFormat := os.Getenv("CLICTL_LOG_FORMAT"); envFormat != "" {
		format = envFormat
	}
	if envFile := os.Getenv("CLICTL_LOG_FILE"); envFile != "" {
		file = envFile
	}

	if l, ok := levelFromString[strings.ToLower(level)]; ok {
		global.level = l
	}
	if format == "json" || format == "text" {
		global.format = format
	}

	// Open log file if specified
	if file != "" {
		f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not open log file %s: %v\n", file, err)
		} else {
			global.output = f
			global.ownFile = true
		}
	}
}

// Close cleans up the logger (closes file if one was opened).
func Close() {
	global.mu.Lock()
	defer global.mu.Unlock()
	if global.ownFile && global.output != nil {
		global.output.Close()
		global.ownFile = false
		global.output = os.Stderr
	}
}

// IsEnabled returns true if the logger is active.
func IsEnabled() bool {
	return global.enabled
}

// Debug logs a debug message.
func Debug(msg string, fields ...Field) {
	global.log(LevelDebug, msg, fields)
}

// Info logs an info message.
func Info(msg string, fields ...Field) {
	global.log(LevelInfo, msg, fields)
}

// Warn logs a warning message.
func Warn(msg string, fields ...Field) {
	global.log(LevelWarn, msg, fields)
}

// Error logs an error message.
func Error(msg string, fields ...Field) {
	global.log(LevelError, msg, fields)
}

// Field is a key-value pair for structured logging.
type Field struct {
	Key   string
	Value any
}

// F creates a log field.
func F(key string, value any) Field {
	return Field{Key: key, Value: value}
}

func (l *Logger) log(level Level, msg string, fields []Field) {
	if !l.enabled || level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.format == "json" {
		l.logJSON(level, msg, fields)
	} else {
		l.logText(level, msg, fields)
	}
}

func (l *Logger) logText(level Level, msg string, fields []Field) {
	ts := time.Now().Format("15:04:05.000")
	levelStr := levelNames[level]

	if len(fields) == 0 {
		fmt.Fprintf(l.output, "%s [%s] %s\n", ts, levelStr, msg)
		return
	}

	var parts []string
	for _, f := range fields {
		parts = append(parts, fmt.Sprintf("%s=%v", f.Key, f.Value))
	}
	fmt.Fprintf(l.output, "%s [%s] %s %s\n", ts, levelStr, msg, strings.Join(parts, " "))
}

func (l *Logger) logJSON(level Level, msg string, fields []Field) {
	entry := map[string]any{
		"time":  time.Now().UTC().Format(time.RFC3339Nano),
		"level": strings.ToLower(levelNames[level]),
		"msg":   msg,
	}
	for _, f := range fields {
		entry[f.Key] = f.Value
	}
	data, _ := json.Marshal(entry)
	fmt.Fprintf(l.output, "%s\n", data)
}
