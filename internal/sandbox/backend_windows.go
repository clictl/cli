// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
//go:build windows

package sandbox

import (
	"fmt"
)

// runLinuxNS is not available on Windows.
func runLinuxNS(cfg *Config, command string, args []string) (*Result, error) {
	return nil, fmt.Errorf("Linux namespace sandbox is not available on Windows")
}

// runMacOSSandbox is not available on Windows.
func runMacOSSandbox(cfg *Config, command string, args []string) (*Result, error) {
	return nil, fmt.Errorf("macOS sandbox-exec is not available on Windows")
}
