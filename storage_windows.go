//go:build windows
// +build windows

package magpie

import (
	"syscall"
	"unsafe"
)

// msync syncs a memory-mapped region to disk on Windows.
func msync(b []byte) error {
	if len(b) == 0 {
		return nil
	}

	// On Windows, use FlushViewOfFile
	err := syscall.FlushViewOfFile(uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	if err != nil {
		return err
	}
	return nil
}
