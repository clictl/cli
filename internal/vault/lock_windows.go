// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
//go:build windows

package vault

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	modkernel32    = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx = modkernel32.NewProc("LockFileEx")
	procUnlockFile = modkernel32.NewProc("UnlockFileEx")
)

const (
	lockfileExclusiveLock = 0x00000002
	lockfileFailImmediately = 0x00000001
)

func lockFileExclusive(f *os.File) error {
	// OVERLAPPED struct with all zeros for synchronous operation.
	var ol syscall.Overlapped
	handle := syscall.Handle(f.Fd())
	// Lock the entire file with an exclusive lock (blocking).
	r1, _, err := procLockFileEx.Call(
		uintptr(handle),
		uintptr(lockfileExclusiveLock),
		0,
		1, 0,
		uintptr(unsafe.Pointer(&ol)),
	)
	if r1 == 0 {
		return err
	}
	return nil
}

func unlockFile(f *os.File) error {
	var ol syscall.Overlapped
	handle := syscall.Handle(f.Fd())
	r1, _, err := procUnlockFile.Call(
		uintptr(handle),
		0,
		1, 0,
		uintptr(unsafe.Pointer(&ol)),
	)
	if r1 == 0 {
		return err
	}
	return nil
}
