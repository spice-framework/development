//go:build darwin

package gorelease

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

func commitOutput(staging string, output string) error {
	stagingPointer, err := syscall.BytePtrFromString(staging)
	if err != nil {
		return err
	}
	outputPointer, err := syscall.BytePtrFromString(output)
	if err != nil {
		return err
	}
	// #nosec G103 -- pointers remain live for the fixed renameatx_np syscall.
	_, _, errno := syscall.Syscall6(
		488,
		^uintptr(1),
		uintptr(unsafe.Pointer(stagingPointer)),
		^uintptr(1),
		uintptr(unsafe.Pointer(outputPointer)),
		0x4,
		0,
	)
	runtime.KeepAlive(stagingPointer)
	runtime.KeepAlive(outputPointer)
	if errno != 0 {
		return &os.LinkError{Op: "rename-noreplace", Old: staging, New: output, Err: errno}
	}
	return nil
}
