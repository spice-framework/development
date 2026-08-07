//go:build linux

package gorelease

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

func commitOutput(staging string, output string) error {
	trap, err := linuxRenameat2Trap()
	if err != nil {
		return err
	}
	stagingPointer, err := syscall.BytePtrFromString(staging)
	if err != nil {
		return err
	}
	outputPointer, err := syscall.BytePtrFromString(output)
	if err != nil {
		return err
	}
	// #nosec G103 -- pointers remain live for the fixed renameat2 syscall.
	_, _, errno := syscall.Syscall6(
		trap,
		^uintptr(99),
		uintptr(unsafe.Pointer(stagingPointer)),
		^uintptr(99),
		uintptr(unsafe.Pointer(outputPointer)),
		1,
		0,
	)
	runtime.KeepAlive(stagingPointer)
	runtime.KeepAlive(outputPointer)
	if errno != 0 {
		return &os.LinkError{Op: "rename-noreplace", Old: staging, New: output, Err: errno}
	}
	return nil
}

func linuxRenameat2Trap() (uintptr, error) {
	switch runtime.GOARCH {
	case "amd64":
		return 316, nil
	case "arm64":
		return 276, nil
	default:
		return 0, fmt.Errorf("atomic no-replace Go release commit is unsupported on linux/%s", runtime.GOARCH)
	}
}
