//go:build windows
// +build windows

package magpie

import (
	"fmt"
	"io"
	"os"
)

// Init initializes the storage engine with the given file and size.
// On Windows, we use a fallback approach with a memory buffer instead of mmap.
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

	// Allocate memory buffer (simulating mmap)
	mmap := make([]byte, size)

	// Read existing file content into buffer
	if info.Size() > 0 {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("failed to seek file: %w", err)
		}
		if _, err := io.ReadFull(file, mmap[:info.Size()]); err != nil && err != io.EOF {
			return fmt.Errorf("failed to read file: %w", err)
		}
	}

	s.file = file
	s.mmap = mmap
	s.size = size

	return nil
}

// AllocatePage allocates a new page and returns its page number.
// On Windows, we extend the memory buffer instead of remapping.
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

	// Extend memory buffer
	newMmap := make([]byte, newSize)
	copy(newMmap, s.mmap)

	s.mmap = newMmap
	s.size = newSize

	return pageNum, nil
}

// Close flushes the buffer and closes the file.
func (s *Storage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error

	// Flush buffer to disk
	if s.mmap != nil && s.file != nil {
		if _, err := s.file.Seek(0, io.SeekStart); err != nil {
			errs = append(errs, fmt.Errorf("failed to seek file: %w", err))
		} else if _, err := s.file.Write(s.mmap); err != nil {
			errs = append(errs, fmt.Errorf("failed to write file: %w", err))
		} else if err := s.file.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("failed to sync file: %w", err))
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

// msync syncs the memory buffer to disk on Windows.
func msync(b []byte) error {
	// On Windows with our fallback implementation, msync is handled in Sync()
	// This is a no-op here since we don't use actual memory mapping
	return nil
}
