// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
//go:build windows

package sandbox

import (
	"context"
	"fmt"
	osexec "os/exec"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// applyAndStart creates a Job Object to contain the MCP server process tree
// on Windows.
//
// The Job Object ensures:
// - Child processes cannot outlive the parent (KILL_ON_JOB_CLOSE)
// - The process tree is contained
//
// Combined with env scrubbing from Phase 1, this provides meaningful
// protection against supply chain attacks on Windows.
func applyAndStart(ctx context.Context, cmd *osexec.Cmd, policy *Policy) error {
	// Create a Job Object
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("CreateJobObject: %w", err)
	}

	// Configure: kill all processes when the job handle is closed
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}

	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("SetInformationJobObject: %w", err)
	}

	// Start the process normally
	if err := cmd.Start(); err != nil {
		windows.CloseHandle(job)
		return err
	}

	// Open the process handle and assign to Job Object
	pid := uint32(cmd.Process.Pid)
	procHandle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		pid,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clictl: warning: could not open process for Job Object: %v\n", err)
	} else {
		if err := windows.AssignProcessToJobObject(job, procHandle); err != nil {
			fmt.Fprintf(os.Stderr, "clictl: warning: could not assign process to Job Object: %v\n", err)
		}
		windows.CloseHandle(procHandle)
	}

	// Keep the job handle alive until the process exits
	go func() {
		cmd.Wait()
		windows.CloseHandle(job)
	}()

	return nil
}
