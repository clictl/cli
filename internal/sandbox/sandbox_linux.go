// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
//go:build linux

package sandbox

import (
	"context"
	"fmt"
	osexec "os/exec"
	"runtime"

	"github.com/landlock-lsm/go-landlock/landlock"
)

// applyAndStart applies Landlock filesystem restrictions and starts the process.
//
// Landlock is an allowlist-based Linux security module (kernel 5.13+). We only
// grant access to declared paths; everything else (including ~/.ssh, ~/.aws,
// browser profiles) is implicitly denied.
//
// Because Landlock restrictions are per-thread and permanent, we:
// 1. Lock the goroutine to an OS thread (runtime.LockOSThread)
// 2. Apply Landlock restrictions to that thread
// 3. Fork+exec the child (inherits restrictions)
// 4. The restricted thread is sacrificed when the goroutine exits
func applyAndStart(ctx context.Context, cmd *osexec.Cmd, policy *Policy) error {
	readPaths := AllowedReadPaths(policy)
	writePaths := AllowedWritePaths(policy)

	// Build Landlock path rules
	var rules []landlock.Rule
	for _, p := range readPaths {
		rules = append(rules, landlock.RODirs(p))
	}
	for _, p := range writePaths {
		rules = append(rules, landlock.RWDirs(p))
	}

	// Channel to receive the result of Start
	type startResult struct {
		err error
	}
	ch := make(chan startResult, 1)

	go func() {
		// Lock this goroutine to a dedicated OS thread.
		// Landlock restrictions applied here will only affect this thread
		// (and the child process forked from it).
		runtime.LockOSThread()
		// Do NOT unlock - the thread is permanently restricted and will be
		// destroyed when this goroutine returns.

		// Apply Landlock restrictions
		err := landlock.V5.BestEffort().Restrict(rules...)
		if err != nil {
			// Landlock not available or failed - start without sandbox
			ch <- startResult{err: fmt.Errorf("landlock restrict: %w", err)}
			return
		}

		// Start the process on this restricted thread
		ch <- startResult{err: cmd.Start()}
	}()

	result := <-ch

	// If Landlock failed, fall back to unsandboxed start
	if result.err != nil {
		// Check if it was a Landlock error (not a Start error)
		if cmd.Process == nil {
			// Landlock failed, try starting without sandbox
			return fmt.Errorf("landlock: %w", result.err)
		}
	}

	return result.err
}
