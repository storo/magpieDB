package magpie

import (
	"bytes"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// TestStorageConcurrentReadDuringRemap tests that concurrent reads during remap don't cause data races
// This test demonstrates the CRITICAL bug: ReadPage returns direct mmap slices that become invalid
// when AllocatePage remaps the memory
func TestStorageConcurrentReadDuringRemap(t *testing.T) {
	// Create temp file
	tmpFile, err := os.CreateTemp("", "magpie_remap_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Initialize storage
	storage := NewStorage()
	if err := storage.Init(tmpFile, PageSize*10); err != nil {
		t.Fatalf("Failed to init storage: %v", err)
	}
	defer storage.Close()

	// Write test data to first few pages
	testData := make([]byte, PageSize)
	for i := 0; i < PageSize; i++ {
		testData[i] = byte(i % 256)
	}

	for pageNum := uint64(0); pageNum < 5; pageNum++ {
		if err := storage.WritePage(pageNum, testData); err != nil {
			t.Fatalf("Failed to write page %d: %v", pageNum, err)
		}
	}

	// Start concurrent readers and allocators
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// 10 concurrent readers - each reads a fixed number of times
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				// Read page and verify data
				pageData, err := storage.ReadPage(0)
				if err != nil {
					select {
					case errors <- fmt.Errorf("reader %d read error: %w", readerID, err):
					default:
					}
					return
				}

				// Try to access the data (this may segfault if remap happens)
				if len(pageData) != PageSize {
					select {
					case errors <- fmt.Errorf("reader %d got wrong page size: %d", readerID, len(pageData)):
					default:
					}
					return
				}

				// Verify first few bytes
				for k := 0; k < 100; k++ {
					if pageData[k] != byte(k%256) {
						select {
						case errors <- fmt.Errorf("reader %d data corruption at byte %d: got %d, want %d",
							readerID, k, pageData[k], byte(k%256)):
						default:
						}
						return
					}
				}

				time.Sleep(time.Microsecond * 10)
			}
		}(i)
	}

	// 5 concurrent allocators (cause remaps)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(allocatorID int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_, err := storage.AllocatePage()
				if err != nil {
					select {
					case errors <- fmt.Errorf("allocator %d error: %w", allocatorID, err):
					default:
					}
					return
				}
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	done := make(chan bool)
	go func() {
		wg.Wait()
		close(done)
	}()

	// Wait for completion or error
	select {
	case err := <-errors:
		t.Fatalf("Concurrent access error: %v", err)
	case <-done:
		// Success - all goroutines completed without errors
	case <-time.After(15 * time.Second):
		t.Fatal("Test timeout - possible deadlock or race condition")
	}
}

// TestStorageSliceInvalidationAfterRemap tests that slices obtained before remap become invalid
// This demonstrates the core issue: returned slices point to old mmap that gets unmapped
func TestStorageSliceInvalidationAfterRemap(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "magpie_slice_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	storage := NewStorage()
	if err := storage.Init(tmpFile, PageSize*2); err != nil {
		t.Fatalf("Failed to init storage: %v", err)
	}
	defer storage.Close()

	// Write recognizable pattern
	testData := make([]byte, PageSize)
	for i := 0; i < PageSize; i++ {
		testData[i] = 0xAB
	}
	if err := storage.WritePage(0, testData); err != nil {
		t.Fatalf("Failed to write page: %v", err)
	}

	// Get slice BEFORE remap
	pageSlice, err := storage.ReadPage(0)
	if err != nil {
		t.Fatalf("Failed to read page: %v", err)
	}

	// Verify slice is correct initially
	if len(pageSlice) != PageSize {
		t.Fatalf("Wrong page size: got %d, want %d", len(pageSlice), PageSize)
	}
	if pageSlice[0] != 0xAB {
		t.Fatalf("Wrong initial data: got %x, want 0xAB", pageSlice[0])
	}

	// Force multiple remaps by allocating many pages
	for i := 0; i < 100; i++ {
		_, err := storage.AllocatePage()
		if err != nil {
			t.Fatalf("Failed to allocate page: %v", err)
		}
	}

	// Try to access the old slice (THIS SHOULD NOT SEGFAULT with the fix)
	// With the bug, this could cause a segfault or return garbage data
	// With the fix (copy), the slice is independent and still valid
	for i := 0; i < 100; i++ {
		_ = pageSlice[i] // Access memory
	}

	// The old slice should still have the original data if it was copied
	// If it's a direct mmap slice, it may have garbage or cause segfault
	if pageSlice[0] != 0xAB {
		t.Errorf("Slice data corrupted after remap: got %x, want 0xAB", pageSlice[0])
	}
}

// TestStorageCopyOnReadSafety verifies that ReadPage returns safe copies
// After the fix, this should pass. Before the fix, it may race.
func TestStorageCopyOnReadSafety(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "magpie_copy_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	storage := NewStorage()
	if err := storage.Init(tmpFile, PageSize*5); err != nil {
		t.Fatalf("Failed to init storage: %v", err)
	}
	defer storage.Close()

	// Write pattern
	pattern := make([]byte, PageSize)
	for i := 0; i < PageSize; i++ {
		pattern[i] = byte(i % 256)
	}
	if err := storage.WritePage(0, pattern); err != nil {
		t.Fatalf("Failed to write page: %v", err)
	}

	// Read page twice
	slice1, err := storage.ReadPage(0)
	if err != nil {
		t.Fatalf("Failed first read: %v", err)
	}

	slice2, err := storage.ReadPage(0)
	if err != nil {
		t.Fatalf("Failed second read: %v", err)
	}

	// If returning copies, modifying slice1 should NOT affect slice2
	slice1[0] = 0xFF

	if slice2[0] == 0xFF {
		t.Error("Slices share memory - not returning copies! This is unsafe.")
	}

	// Both should have original data
	if slice2[0] != pattern[0] {
		t.Errorf("slice2 data corrupted: got %x, want %x", slice2[0], pattern[0])
	}

	// After remap, original slices should still be valid (if copies)
	for i := 0; i < 50; i++ {
		_, err := storage.AllocatePage()
		if err != nil {
			t.Fatalf("Failed to allocate: %v", err)
		}
	}

	// Should still be able to access both slices
	_ = slice1[100]
	_ = slice2[100]

	// slice2 should still have original data (not modified by slice1)
	if slice2[100] != pattern[100] {
		t.Errorf("slice2 data corrupted after remap: got %x, want %x", slice2[100], pattern[100])
	}
}

// TestStorageHighConcurrencyRemap stress tests with many readers and allocators
func TestStorageHighConcurrencyRemap(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "magpie_stress_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	storage := NewStorage()
	if err := storage.Init(tmpFile, PageSize*20); err != nil {
		t.Fatalf("Failed to init storage: %v", err)
	}
	defer storage.Close()

	// Write test patterns to pages
	for pageNum := uint64(0); pageNum < 10; pageNum++ {
		testData := make([]byte, PageSize)
		marker := byte(pageNum)
		for i := 0; i < PageSize; i++ {
			testData[i] = marker
		}
		if err := storage.WritePage(pageNum, testData); err != nil {
			t.Fatalf("Failed to write page %d: %v", pageNum, err)
		}
	}

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// 50 concurrent readers (high contention) - fixed iteration count
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for readCount := 0; readCount < 100; readCount++ {
				pageNum := uint64(readerID % 10)
				pageData, err := storage.ReadPage(pageNum)
				if err != nil {
					select {
					case errors <- fmt.Errorf("reader %d error: %w", readerID, err):
					default:
					}
					return
				}

				// Verify data integrity
				expectedMarker := byte(pageNum)
				for j := 0; j < 50; j++ {
					if pageData[j] != expectedMarker {
						select {
						case errors <- fmt.Errorf("reader %d data corruption: page %d byte %d got %x want %x",
							readerID, pageNum, j, pageData[j], expectedMarker):
						default:
						}
						return
					}
				}

				time.Sleep(time.Microsecond * 10)
			}
		}(i)
	}

	// 10 aggressive allocators
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(allocatorID int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, err := storage.AllocatePage()
				if err != nil {
					select {
					case errors <- fmt.Errorf("allocator %d error: %w", allocatorID, err):
					default:
					}
					return
				}
				time.Sleep(time.Millisecond * 2)
			}
		}(i)
	}

	// Wait for completion
	done := make(chan bool)
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case err := <-errors:
		t.Fatalf("High concurrency error: %v", err)
	case <-done:
		// Success
	case <-time.After(30 * time.Second):
		t.Fatal("Stress test timeout")
	}
}

// TestStorageRemapDataIntegrity verifies data remains intact through multiple remaps
func TestStorageRemapDataIntegrity(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "magpie_integrity_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	storage := NewStorage()
	if err := storage.Init(tmpFile, PageSize*5); err != nil {
		t.Fatalf("Failed to init storage: %v", err)
	}
	defer storage.Close()

	// Write unique patterns to first 5 pages
	patterns := make([][]byte, 5)
	for pageNum := 0; pageNum < 5; pageNum++ {
		patterns[pageNum] = make([]byte, PageSize)
		for i := 0; i < PageSize; i++ {
			patterns[pageNum][i] = byte((pageNum*256 + i) % 256)
		}
		if err := storage.WritePage(uint64(pageNum), patterns[pageNum]); err != nil {
			t.Fatalf("Failed to write page %d: %v", pageNum, err)
		}
	}

	// Perform multiple remaps
	for i := 0; i < 100; i++ {
		_, err := storage.AllocatePage()
		if err != nil {
			t.Fatalf("Failed to allocate page %d: %v", i, err)
		}

		// Periodically verify original pages still have correct data
		if i%10 == 0 {
			for pageNum := 0; pageNum < 5; pageNum++ {
				pageData, err := storage.ReadPage(uint64(pageNum))
				if err != nil {
					t.Fatalf("Failed to read page %d after %d remaps: %v", pageNum, i, err)
				}

				if !bytes.Equal(pageData, patterns[pageNum]) {
					// Find first mismatch
					for j := 0; j < PageSize; j++ {
						if pageData[j] != patterns[pageNum][j] {
							t.Fatalf("Data corruption in page %d byte %d after %d remaps: got %x want %x",
								pageNum, j, i, pageData[j], patterns[pageNum][j])
						}
					}
				}
			}
		}
	}

	// Final verification
	for pageNum := 0; pageNum < 5; pageNum++ {
		pageData, err := storage.ReadPage(uint64(pageNum))
		if err != nil {
			t.Fatalf("Failed final read of page %d: %v", pageNum, err)
		}

		if !bytes.Equal(pageData, patterns[pageNum]) {
			t.Errorf("Final data corruption in page %d", pageNum)
		}
	}
}

// TestStorageReadPagesRemap tests batch read operation during remaps
func TestStorageReadPagesRemap(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "magpie_batch_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	storage := NewStorage()
	if err := storage.Init(tmpFile, PageSize*10); err != nil {
		t.Fatalf("Failed to init storage: %v", err)
	}
	defer storage.Close()

	// Write test data
	for pageNum := uint64(0); pageNum < 5; pageNum++ {
		testData := make([]byte, PageSize)
		for i := 0; i < PageSize; i++ {
			testData[i] = byte(pageNum)
		}
		if err := storage.WritePage(pageNum, testData); err != nil {
			t.Fatalf("Failed to write page %d: %v", pageNum, err)
		}
	}

	var wg sync.WaitGroup
	errors := make(chan error, 50)

	// Concurrent batch readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				pages, err := storage.ReadPages([]uint64{0, 1, 2, 3, 4})
				if err != nil {
					select {
					case errors <- fmt.Errorf("reader %d batch read error: %w", readerID, err):
					default:
					}
					return
				}

				// Verify each page
				for pageIdx, pageData := range pages {
					if len(pageData) != PageSize {
						select {
						case errors <- fmt.Errorf("reader %d wrong page size: %d", readerID, len(pageData)):
						default:
						}
						return
					}
					expectedByte := byte(pageIdx)
					if pageData[0] != expectedByte {
						select {
						case errors <- fmt.Errorf("reader %d page %d corrupted: got %x want %x",
							readerID, pageIdx, pageData[0], expectedByte):
						default:
						}
						return
					}
				}
				time.Sleep(time.Microsecond * 100)
			}
		}(i)
	}

	// Concurrent allocators causing remaps
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(allocatorID int) {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				_, err := storage.AllocatePage()
				if err != nil {
					select {
					case errors <- fmt.Errorf("allocator %d error: %w", allocatorID, err):
					default:
					}
					return
				}
				time.Sleep(time.Millisecond * 3)
			}
		}(i)
	}

	done := make(chan bool)
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case err := <-errors:
		t.Fatalf("Batch read remap error: %v", err)
	case <-done:
		// Success
	case <-time.After(20 * time.Second):
		t.Fatal("Batch read test timeout")
	}
}
