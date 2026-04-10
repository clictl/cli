// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package sandbox

import (
	"fmt"
	"os"
	osexec "os/exec"
	"runtime"
	"sync"
)

// backendRegistry holds all registered sandbox backends in priority order.
var (
	registeredBackends []Backend
	backendOnce        sync.Once
)

// initBackends registers all available backends in priority order:
// 1. Linux namespaces (best isolation, Linux only)
// 2. macOS sandbox-exec (macOS only)
// 3. Docker (cross-platform, requires Docker daemon)
// 4. Unsandboxed (always available, warns user)
func initBackends() {
	backendOnce.Do(func() {
		registeredBackends = []Backend{
			&linuxNSBackend{},
			&macOSSandboxBackend{},
			&dockerBackend{},
			&unsandboxedBackend{},
		}
	})
}

// SelectBackend returns the best available sandbox backend for the current system.
// It checks backends in priority order and returns the first one that reports Available().
func SelectBackend() Backend {
	initBackends()
	for _, b := range registeredBackends {
		if b.Available() {
			return b
		}
	}
	// Should never happen since unsandboxed is always available
	return &unsandboxedBackend{}
}

// SelectBackendByName returns a specific sandbox backend by name, or an error
// if the backend is not available on this system.
func SelectBackendByName(name string) (Backend, error) {
	initBackends()
	for _, b := range registeredBackends {
		if b.Name() == name {
			if !b.Available() {
				return nil, fmt.Errorf("sandbox backend %q is not available on this system", name)
			}
			return b, nil
		}
	}
	return nil, fmt.Errorf("unknown sandbox backend %q", name)
}

// ListBackends returns all registered backends with their availability status.
func ListBackends() []BackendStatus {
	initBackends()
	statuses := make([]BackendStatus, len(registeredBackends))
	for i, b := range registeredBackends {
		statuses[i] = BackendStatus{
			Name:      b.Name(),
			Available: b.Available(),
		}
	}
	return statuses
}

// BackendStatus describes a sandbox backend and whether it's usable.
type BackendStatus struct {
	Name      string
	Available bool
}

// linuxNSBackend implements sandboxing via Linux namespaces (CLONE_NEWUSER, PID, etc.).
type linuxNSBackend struct{}

// Name returns the backend identifier.
func (b *linuxNSBackend) Name() string { return "linux-ns" }

// Available reports whether the backend can run on this platform.
func (b *linuxNSBackend) Available() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	// Check if user namespaces are available
	_, err := os.ReadFile("/proc/sys/kernel/unprivileged_userns_clone")
	if err != nil {
		// File might not exist on all kernels, but namespaces may still work
		return true
	}
	return true
}

// Run applies sandbox restrictions and starts the process.
func (b *linuxNSBackend) Run(cfg *Config, command string, args []string) (*Result, error) {
	return runLinuxNS(cfg, command, args)
}

// macOSSandboxBackend implements sandboxing via macOS sandbox-exec.
type macOSSandboxBackend struct{}

// Name returns the backend identifier.
func (b *macOSSandboxBackend) Name() string { return "macos-sandbox" }

// Available reports whether the backend can run on this platform.
func (b *macOSSandboxBackend) Available() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	// Check if sandbox-exec exists
	_, err := osexec.LookPath("sandbox-exec")
	return err == nil
}

// Run applies sandbox restrictions and starts the process.
func (b *macOSSandboxBackend) Run(cfg *Config, command string, args []string) (*Result, error) {
	return runMacOSSandbox(cfg, command, args)
}

// dockerBackend implements sandboxing via Docker containers.
type dockerBackend struct{}

// Name returns the backend identifier.
func (b *dockerBackend) Name() string { return "docker" }

// Available reports whether the backend can run on this platform.
func (b *dockerBackend) Available() bool {
	// Check if docker CLI is available and daemon is running
	path, err := osexec.LookPath("docker")
	if err != nil || path == "" {
		return false
	}
	cmd := osexec.Command("docker", "info")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// Run applies sandbox restrictions and starts the process.
func (b *dockerBackend) Run(cfg *Config, command string, args []string) (*Result, error) {
	return runDocker(cfg, command, args)
}

// unsandboxedBackend runs commands without any sandboxing. It is always available
// but warns the user about the security implications.
type unsandboxedBackend struct{}

// Name returns the backend identifier.
func (b *unsandboxedBackend) Name() string    { return "unsandboxed" }
// Available reports whether the backend can run on this platform.
func (b *unsandboxedBackend) Available() bool { return true }

// Run applies sandbox restrictions and starts the process.
func (b *unsandboxedBackend) Run(cfg *Config, command string, args []string) (*Result, error) {
	return runUnsandboxed(cfg, command, args)
}
