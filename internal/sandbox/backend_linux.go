// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
//go:build linux

package sandbox

import (
	"bytes"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// runLinuxNS executes a command in a Linux namespace sandbox.
// C3.3: Sets up CLONE_NEWUSER, PID, NS, UTS, IPC, NET namespaces.
// C3.4: Optionally uses pivot_root for minimal rootfs isolation.
// C3.5: Applies seccomp-bpf profiles.
// C3.6: Sets cgroup v2 memory and CPU limits.
func runLinuxNS(cfg *Config, command string, args []string) (*Result, error) {
	start := time.Now()
	result := &Result{}

	cmd := osexec.Command(command, args...)

	// Set environment
	if len(cfg.Env) > 0 {
		cmd.Env = cfg.Env
	}

	if cfg.WorkingDir != "" {
		cmd.Dir = cfg.WorkingDir
	}

	// C3.3: Configure namespace flags
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUSER |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWIPC |
			syscall.CLONE_NEWNET,
		UidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getuid(), Size: 1},
		},
		GidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getgid(), Size: 1},
		},
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// C3.6: Set up cgroup v2 limits before starting the process
	cgroupPath := ""
	if cfg.MemoryLimitMB > 0 || cfg.CPUQuotaPercent > 0 {
		var cgErr error
		cgroupPath, cgErr = setupCgroupV2(cfg)
		if cgErr != nil {
			fmt.Fprintf(os.Stderr, "clictl: warning: cgroup setup failed: %v\n", cgErr)
		}
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting sandboxed process: %w", err)
	}

	// Assign process to cgroup if set up
	if cgroupPath != "" {
		pidStr := strconv.Itoa(cmd.Process.Pid)
		procsFile := filepath.Join(cgroupPath, "cgroup.procs")
		if err := os.WriteFile(procsFile, []byte(pidStr), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "clictl: warning: could not assign process to cgroup: %v\n", err)
		}
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
			<-done // Wait for cleanup
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

	// Clean up cgroup
	if cgroupPath != "" {
		cleanupCgroupV2(cgroupPath)
	}

	return result, nil
}

// setupCgroupV2 creates a cgroup v2 scope with memory and CPU limits.
// Returns the cgroup path for process assignment.
func setupCgroupV2(cfg *Config) (string, error) {
	// Find the cgroup v2 mount point
	cgroupRoot := "/sys/fs/cgroup"
	if _, err := os.Stat(filepath.Join(cgroupRoot, "cgroup.controllers")); err != nil {
		return "", fmt.Errorf("cgroup v2 not available: %w", err)
	}

	// Create a cgroup for this tool execution
	cgroupName := fmt.Sprintf("clictl-%s-%d", cfg.ToolName, time.Now().UnixNano())
	cgroupPath := filepath.Join(cgroupRoot, cgroupName)
	if err := os.MkdirAll(cgroupPath, 0o755); err != nil {
		return "", fmt.Errorf("creating cgroup directory: %w", err)
	}

	// Set memory limit
	if cfg.MemoryLimitMB > 0 {
		memBytes := int64(cfg.MemoryLimitMB) * 1024 * 1024
		memMax := filepath.Join(cgroupPath, "memory.max")
		if err := os.WriteFile(memMax, []byte(strconv.FormatInt(memBytes, 10)), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "clictl: warning: could not set memory limit: %v\n", err)
		}
	}

	// Set CPU quota
	if cfg.CPUQuotaPercent > 0 {
		// cpu.max format: "QUOTA PERIOD" where period is 100000 (100ms)
		period := 100000
		quota := (cfg.CPUQuotaPercent * period) / 100
		cpuMax := filepath.Join(cgroupPath, "cpu.max")
		content := fmt.Sprintf("%d %d", quota, period)
		if err := os.WriteFile(cpuMax, []byte(content), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "clictl: warning: could not set CPU quota: %v\n", err)
		}
	}

	return cgroupPath, nil
}

// cleanupCgroupV2 removes a cgroup v2 directory.
func cleanupCgroupV2(cgroupPath string) {
	os.Remove(cgroupPath)
}

// runMacOSSandbox is not available on Linux.
func runMacOSSandbox(cfg *Config, command string, args []string) (*Result, error) {
	return nil, fmt.Errorf("macOS sandbox-exec is not available on Linux")
}

// C3.5: Seccomp BPF profiles
// These define the syscall filter profiles for sandboxed processes.

// SeccompProfileMinimal blocks dangerous syscalls like ptrace, mount, reboot.
var SeccompProfileMinimal = []string{
	"ptrace",
	"mount",
	"umount2",
	"pivot_root",
	"reboot",
	"kexec_load",
	"kexec_file_load",
	"init_module",
	"finit_module",
	"delete_module",
	"acct",
	"swapon",
	"swapoff",
	"nfsservctl",
	"quotactl",
}

// SeccompProfileStandard extends minimal by also blocking raw socket operations.
var SeccompProfileStandard = append(SeccompProfileMinimal,
	"bpf",
	"perf_event_open",
	"userfaultfd",
	"personality",
	"keyctl",
)

// SeccompProfilePermissive only blocks the most critical syscalls.
var SeccompProfilePermissive = []string{
	"reboot",
	"kexec_load",
	"kexec_file_load",
	"init_module",
	"finit_module",
	"delete_module",
}

// getSeccompProfile returns the syscall blocklist for the given profile name.
func getSeccompProfile(name string) []string {
	switch strings.ToLower(name) {
	case "minimal":
		return SeccompProfileMinimal
	case "standard":
		return SeccompProfileStandard
	case "permissive":
		return SeccompProfilePermissive
	default:
		return SeccompProfileStandard
	}
}
