// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package sandbox

import (
	osexec "os/exec"
)

// exitCodeFromError extracts the exit code from an exec error.
// Returns 0 for nil errors, the actual exit code for ExitErrors,
// and 1 for other error types.
func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*osexec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return 1
}
