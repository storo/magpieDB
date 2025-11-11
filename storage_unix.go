//go:build !windows
// +build !windows

package magpie

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Init initializes the storage engine with the given file and size.
// If the file is smaller than size, it will be extended.
// The file is memory-mapped for efficient access.
func (s *Storage) Init(file *os.File, size int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if size < PageSize {
		return fmt.Errorf("size must be at least %d bytes", PageSize)
	}

	// Get current file size
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Extend file if needed
	if info.Size() < size {
		if err := file.Truncate(size); err != nil {
			return fmt.Errorf("failed to resize file: %w", err)
		}
	}

	// Memory map the file
	mmap, err := syscall.Mmap(
		int(file.Fd()),
		0,
		int(size),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED,
	)
	if err != nil {
		return fmt.Errorf("failed to mmap file: %w", err)
	}

	s.file = file
	s.mmap = mmap
	s.size = size

	return nil
}

// AllocatePage allocates a new page and returns its page number.
// It first checks the free list before extending the file.
func (s *Storage) AllocatePage() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if there are free pages available
	if len(s.freeList) > 0 {
		// Pop a page from the free list
		pageNum := s.freeList[len(s.freeList)-1]
		s.freeList = s.freeList[:len(s.freeList)-1]
		return pageNum, nil
	}

	// No free pages, need to extend the file
	pageNum := uint64(s.size) / PageSize

	// Extend file by one page
	newSize := s.size + PageSize
	if err := s.file.Truncate(newSize); err != nil {
		return 0, fmt.Errorf("failed to extend file: %w", err)
	}

	// Remap memory with new size
	if err := syscall.Munmap(s.mmap); err != nil {
		return 0, fmt.Errorf("failed to unmap: %w", err)
	}

	mmap, err := syscall.Mmap(
		int(s.file.Fd()),
		0,
		int(newSize),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to remap: %w", err)
	}

	s.mmap = mmap
	s.size = newSize

	return pageNum, nil
}

// Close unmaps the memory and closes the file.
// This method is idempotent and can be called multiple times safely.
// The Storage owns the file descriptor and is responsible for closing it.
func (s *Storage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error

	// Unmap memory
	if s.mmap != nil {
		if err := syscall.Munmap(s.mmap); err != nil {
			errs = append(errs, fmt.Errorf("failed to unmap: %w", err))
		}
		s.mmap = nil
	}

	// Close file
	if s.file != nil {
		if err := s.file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close file: %w", err))
		}
		s.file = nil
	}

	if len(errs) > 0 {
		return errs[0] // Return first error
	}

	return nil
}

// Sync flushes memory-mapped changes to disk.
func (s *Storage) Sync() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.mmap != nil {
		// Sync the mmap to disk
		if err := msync(s.mmap); err != nil {
			return fmt.Errorf("failed to sync mmap: %w", err)
		}
	}

	if s.file != nil {
		if err := s.file.Sync(); err != nil {
			return fmt.Errorf("failed to sync file: %w", err)
		}
	}

	return nil
}

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
