package magpie

import (
	"fmt"
	"os"
	"sync"
	"syscall"
	_ "unsafe"
)

// Storage manages the memory-mapped database file for zero-copy reads.
type Storage struct {
	file     *os.File
	mmap     []byte
	size     int64
	freeList []uint64 // List of free page numbers available for reuse
	mu       sync.RWMutex
}

// NewStorage creates a new storage engine.
func NewStorage() *Storage {
	return &Storage{
		freeList: make([]uint64, 0),
	}
}

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

// ReadPage reads a page at the given page number.
// Returns a slice of the memory-mapped region (zero-copy).
func (s *Storage) ReadPage(pageNum uint64) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	offset := pageNum * PageSize
	if offset+PageSize > uint64(s.size) {
		return nil, fmt.Errorf("page %d out of bounds", pageNum)
	}

	// Return a slice of the mmap (zero-copy)
	return s.mmap[offset : offset+PageSize], nil
}

// WritePage writes data to a page at the given page number.
// The data must be exactly PageSize bytes.
func (s *Storage) WritePage(pageNum uint64, data []byte) error {
	if len(data) != PageSize {
		return fmt.Errorf("data must be exactly %d bytes", PageSize)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	offset := pageNum * PageSize
	if offset+PageSize > uint64(s.size) {
		return fmt.Errorf("page %d out of bounds", pageNum)
	}

	// Copy directly to mmap
	copy(s.mmap[offset:offset+PageSize], data)

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

// FreePage marks a page as free for reuse.
// The page is added to the free list and can be allocated later.
func (s *Storage) FreePage(pageNum uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate page number
	if pageNum >= uint64(s.size)/PageSize {
		return fmt.Errorf("invalid page number %d", pageNum)
	}

	// Add to free list
	s.freeList = append(s.freeList, pageNum)

	// Optionally zero out the page for security
	// This is commented out for performance, but can be enabled if needed
	// offset := pageNum * PageSize
	// for i := uint64(0); i < PageSize; i++ {
	//     s.mmap[offset+i] = 0
	// }

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

// Close unmaps the memory and closes the file.
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

// Size returns the current size of the storage in bytes.
func (s *Storage) Size() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.size
}

// PageCount returns the total number of pages in the storage.
func (s *Storage) PageCount() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return uint64(s.size) / PageSize
}

// readVectorPage reads vectors from a vector page.
func (s *Storage) readVectorPage(pageNum uint64) ([]VectorEntry, error) {
	pageData, err := s.ReadPage(pageNum)
	if err != nil {
		return nil, err
	}

	// Validate checksum first
	if !VerifyPageChecksum(pageData) {
		return nil, fmt.Errorf("page %d checksum validation failed", pageNum)
	}

	// Parse page header
	header, err := DeserializeHeader(pageData)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize header: %w", err)
	}

	// Verify page type
	if header.Type != VectorPage {
		return nil, fmt.Errorf("expected vector page, got type %d", header.Type)
	}

	// Parse vector entries
	entries := make([]VectorEntry, header.Count)
	offset := 64 // Start after page header

	for i := uint16(0); i < header.Count; i++ {
		entry, err := unpackVectorEntry(pageData[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to unpack entry %d: %w", i, err)
		}
		entries[i] = *entry
		offset += 80 // Size of packed VectorEntry
	}

	return entries, nil
}

// writeVectorPage writes vectors to a vector page.
func (s *Storage) writeVectorPage(pageNum uint64, entries []VectorEntry) error {
	// Create page buffer
	pageData := make([]byte, PageSize)

	// Build page header
	header := NewPageHeader(VectorPage)
	header.Count = uint16(len(entries))
	header.NextPage = 0 // Can be set by caller if needed

	// Pack vector entries
	offset := 64 // Start after page header

	for i := range entries {
		if offset+80 > PageSize {
			return fmt.Errorf("too many entries for page (max offset exceeded)")
		}

		entryData := packVectorEntry(&entries[i])
		copy(pageData[offset:], entryData)
		offset += 80 // Size of packed VectorEntry

		// Note: Actual vector data would be stored separately or in continuation pages
		// This implementation stores only the metadata entries
	}

	// Calculate and set checksum
	headerData := SerializeHeader(header)
	copy(pageData[0:64], headerData)

	// Calculate checksum over the entire page
	checksum := CalculatePageChecksum(pageData)
	header.Checksum = checksum

	// Update header with checksum
	headerData = SerializeHeader(header)
	copy(pageData[0:64], headerData)

	// Write page to storage
	return s.WritePage(pageNum, pageData)
}

// ReadPages reads multiple pages in a single operation for better performance
// OPTIMIZATION: Batch reading reduces syscalls and improves cache locality
func (s *Storage) ReadPages(pageNums []uint64) ([][]byte, error) {
	if len(pageNums) == 0 {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([][]byte, len(pageNums))

	// Read each page (zero-copy from mmap)
	for i, pageNum := range pageNums {
		offset := pageNum * PageSize
		if offset+PageSize > uint64(s.size) {
			return nil, fmt.Errorf("page %d out of bounds", pageNum)
		}

		// Return slice of mmap (zero-copy)
		results[i] = s.mmap[offset : offset+PageSize]
	}

	return results, nil
}

// persistIndex persists the HNSW index to disk pages.
func (s *Storage) persistIndex(idx *HSNWIndex, rootPageNum uint64) error {
	// 1. Serialize index using idx.Serialize()
	data, err := idx.Serialize()
	if err != nil {
		return fmt.Errorf("failed to serialize index: %w", err)
	}

	// 2. Calculate how many pages we need (4KB pages, 64-byte header per page)
	dataPerPage := PageSize - 64
	numPages := (len(data) + dataPerPage - 1) / dataPerPage

	// 3. Allocate pages for index
	pages := make([]uint64, numPages)
	pages[0] = rootPageNum // Use provided root page
	for i := 1; i < numPages; i++ {
		pageNum, err := s.AllocatePage()
		if err != nil {
			return fmt.Errorf("failed to allocate index page %d: %w", i, err)
		}
		pages[i] = pageNum
	}

	// 4. Write data to pages
	for i := 0; i < numPages; i++ {
		// Create index page
		header := NewPageHeader(IndexPage)
		pageData := make([]byte, PageSize)

		// Set next page pointer
		if i < numPages-1 {
			header.NextPage = pages[i+1]
		} else {
			header.NextPage = 0
		}

		// Copy index data chunk (after header)
		start := i * dataPerPage
		end := start + dataPerPage
		if end > len(data) {
			end = len(data)
		}
		copy(pageData[64:], data[start:end])

		// Calculate checksum
		headerData := SerializeHeader(header)
		copy(pageData[0:64], headerData)
		checksum := CalculatePageChecksum(pageData)
		header.Checksum = checksum

		// Write final header with checksum
		headerData = SerializeHeader(header)
		copy(pageData[0:64], headerData)

		// Write page
		if err := s.WritePage(pages[i], pageData); err != nil {
			return fmt.Errorf("failed to write index page %d: %w", i, err)
		}
	}

	return nil
}
