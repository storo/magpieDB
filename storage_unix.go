//go:build !windows
// +build !windows

package magpie

import (
	"syscall"
	"unsafe"
)

// msync syncs a memory-mapped region to disk on Unix-like systems.
func msync(b []byte) error {
	if len(b) == 0 {
		return nil
	}

	_, _, errno := syscall.Syscall(
		syscall.SYS_MSYNC,
		uintptr(unsafe.Pointer(&b[0])),
		uintptr(len(b)),
		uintptr(syscall.MS_SYNC),
	)
	if errno != 0 {
		return errno
	}
	return nil
}
