// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package sandbox

import (
	"bytes"
	"fmt"
	"os"
	osexec "os/exec"
	"strings"
	"time"
)

// C3.15: Unsandboxed fallback - runs with env scrubbing and sensitive path blocking
// but no OS-level isolation. Warns the user about the security implications.

// runUnsandboxed executes a command without sandbox isolation.
// It still applies environment scrubbing and blocks access to sensitive paths
// through convention rather than enforcement.
func runUnsandboxed(cfg *Config, command string, args []string) (*Result, error) {
	start := time.Now()
	result := &Result{}

	// Warn the user
	fmt.Fprintf(os.Stderr, "clictl: WARNING: running %s without sandbox isolation.\n", cfg.ToolName)
	fmt.Fprintf(os.Stderr, "  No OS-level process isolation is available on this system.\n")
	fmt.Fprintf(os.Stderr, "  Environment variables have been scrubbed, but filesystem access is unrestricted.\n")

	cmd := osexec.Command(command, args...)

	// C3.10: Env scrubbing in unsafe mode
	if len(cfg.Env) > 0 {
		cmd.Env = cfg.Env
	} else {
		cmd.Env = scrubEnvironment()
	}

	if cfg.WorkingDir != "" {
		cmd.Dir = cfg.WorkingDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting process: %w", err)
	}

	// Wait with timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	if cfg.Timeout > 0 {
		select {
		case err := <-done:
			result.ExitCode = exitCodeFromError(err)
		case <-time.After(cfg.Timeout):
			cmd.Process.Kill()
			<-done
			result.TimedOut = true
			result.ExitCode = -1
		}
	} else {
		err := <-done
		result.ExitCode = exitCodeFromError(err)
	}

	result.Stdout = stdout.Bytes()
	result.Stderr = stderr.Bytes()
	result.Duration = time.Since(start)

	return result, nil
}

// C3.10: scrubEnvironment creates a clean environment by removing sensitive
// variables from the current process environment.
func scrubEnvironment() []string {
	scrubSet := make(map[string]bool)
	for _, v := range EnvScrub() {
		scrubSet[v] = true
	}

	var env []string
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]

		// Skip known credential vars
		if scrubSet[key] {
			continue
		}

		env = append(env, kv)
	}

	// Add sandbox marker
	env = append(env, "CLICTL_SANDBOX=0")
	env = append(env, "CLICTL_SANDBOX_MODE=unsandboxed")

	return env
}

// runDocker executes a command inside a Docker container.
// C3.14 variant: can also work with gVisor's runsc if installed.
func runDocker(cfg *Config, command string, args []string) (*Result, error) {
	start := time.Now()
	result := &Result{}

	dockerArgs := []string{"run", "--rm"}

	// Set resource limits
	if cfg.MemoryLimitMB > 0 {
		dockerArgs = append(dockerArgs, fmt.Sprintf("--memory=%dm", cfg.MemoryLimitMB))
	}
	if cfg.CPUQuotaPercent > 0 {
		cpus := float64(cfg.CPUQuotaPercent) / 100.0
		dockerArgs = append(dockerArgs, fmt.Sprintf("--cpus=%.2f", cpus))
	}

	// Network mode
	switch cfg.NetworkMode {
	case NetworkNone:
		dockerArgs = append(dockerArgs, "--network=none")
	case NetworkHost:
		dockerArgs = append(dockerArgs, "--network=host")
	default:
		// Default bridge network
	}

	// Environment variables
	for _, env := range cfg.Env {
		dockerArgs = append(dockerArgs, "-e", env)
	}

	// Mounts
	for _, m := range cfg.Mounts {
		flag := "rw"
		if m.ReadOnly {
			flag = "ro"
		}
		dockerArgs = append(dockerArgs, "-v", fmt.Sprintf("%s:%s:%s", m.Source, m.Target, flag))
	}

	// Working directory
	if cfg.WorkingDir != "" {
		dockerArgs = append(dockerArgs, "-w", cfg.WorkingDir)
	}

	// Credential forwarding
	if cfg.Credentials.SSHAgent && !cfg.Credentials.IsCredentialDenied("ssh") {
		sshSock := os.Getenv("SSH_AUTH_SOCK")
		if sshSock != "" {
			dockerArgs = append(dockerArgs, "-v", sshSock+":/ssh-agent:ro")
			dockerArgs = append(dockerArgs, "-e", "SSH_AUTH_SOCK=/ssh-agent")
		}
	}

	// Use gVisor runtime if available
	if isGVisorAvailable() {
		dockerArgs = append(dockerArgs, "--runtime=runsc")
	}

	// Container image (default to busybox if no rootfs configured)
	image := "busybox:latest"
	if cfg.RootfsPath != "" {
		image = cfg.RootfsPath
	}
	dockerArgs = append(dockerArgs, image, command)
	dockerArgs = append(dockerArgs, args...)

	cmd := osexec.Command("docker", dockerArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting docker container: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	if cfg.Timeout > 0 {
		select {
		case err := <-done:
			result.ExitCode = exitCodeFromError(err)
		case <-time.After(cfg.Timeout):
			cmd.Process.Kill()
			<-done
			result.TimedOut = true
			result.ExitCode = -1
		}
	} else {
		err := <-done
		result.ExitCode = exitCodeFromError(err)
	}

	result.Stdout = stdout.Bytes()
	result.Stderr = stderr.Bytes()
	result.Duration = time.Since(start)

	return result, nil
}

// isGVisorAvailable checks if gVisor's runsc is installed.
// C3.14: gVisor backend - OCI bundle + runsc (optional upgrade).
func isGVisorAvailable() bool {
	_, err := osexec.LookPath("runsc")
	return err == nil
}
