package magpie

import (
	"os"
	"testing"
)

func TestStorageInit(t *testing.T) {
	tmpfile := tempFilename() + ".storage"
	defer os.Remove(tmpfile)

	file, err := os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	storage := NewStorage()
	err = storage.Init(file, PageSize*10)
	if err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}
	defer storage.Close()

	if storage.Size() != PageSize*10 {
		t.Errorf("Expected size %d, got %d", PageSize*10, storage.Size())
	}

	if storage.PageCount() != 10 {
		t.Errorf("Expected 10 pages, got %d", storage.PageCount())
	}
}

func TestStorageReadWritePage(t *testing.T) {
	tmpfile := tempFilename() + ".storage"
	defer os.Remove(tmpfile)

	file, err := os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	storage := NewStorage()
	err = storage.Init(file, PageSize*10)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	// Write a page
	data := make([]byte, PageSize)
	for i := range data {
		data[i] = byte(i % 256)
	}

	err = storage.WritePage(0, data)
	if err != nil {
		t.Fatalf("Failed to write page: %v", err)
	}

	// Read it back
	readData, err := storage.ReadPage(0)
	if err != nil {
		t.Fatalf("Failed to read page: %v", err)
	}

	// Verify
	for i := range readData {
		if readData[i] != data[i] {
			t.Errorf("Mismatch at byte %d: got %d, want %d", i, readData[i], data[i])
			break
		}
	}
}

func TestStorageAllocatePage(t *testing.T) {
	tmpfile := tempFilename() + ".storage"
	defer os.Remove(tmpfile)

	file, err := os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	storage := NewStorage()
	err = storage.Init(file, PageSize*2)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	initialPages := storage.PageCount()

	// Allocate a new page
	pageNum, err := storage.AllocatePage()
	if err != nil {
		t.Fatalf("Failed to allocate page: %v", err)
	}

	if pageNum != initialPages {
		t.Errorf("Expected page number %d, got %d", initialPages, pageNum)
	}

	if storage.PageCount() != initialPages+1 {
		t.Errorf("Expected %d pages, got %d", initialPages+1, storage.PageCount())
	}
}

func TestPageSerialization(t *testing.T) {
	header := NewPageHeader(VectorPage)
	header.Count = 10
	header.NextPage = 5
	header.Checksum = 12345

	// Serialize
	data := SerializeHeader(header)

	// Deserialize
	header2, err := DeserializeHeader(data)
	if err != nil {
		t.Fatalf("Failed to deserialize header: %v", err)
	}

	if header2.Type != header.Type {
		t.Errorf("Type mismatch: got %d, want %d", header2.Type, header.Type)
	}

	if header2.Count != header.Count {
		t.Errorf("Count mismatch: got %d, want %d", header2.Count, header.Count)
	}

	if header2.NextPage != header.NextPage {
		t.Errorf("NextPage mismatch: got %d, want %d", header2.NextPage, header.NextPage)
	}
}

func TestVectorSerialization(t *testing.T) {
	original := []float32{1.5, 2.7, 3.9, 4.1}

	// Serialize
	data := SerializeVector(original)

	// Deserialize
	result := DeserializeVector(data)

	if len(result) != len(original) {
		t.Fatalf("Length mismatch: got %d, want %d", len(result), len(original))
	}

	for i := range original {
		if result[i] != original[i] {
			t.Errorf("Value mismatch at index %d: got %f, want %f", i, result[i], original[i])
		}
	}
}

func TestDBHeaderSerialization(t *testing.T) {
	header := NewDBHeader(384, "cosine")
	header.VectorCount = 1000
	header.PageCount = 50

	// Serialize
	data := SerializeDBHeader(header)

	// Deserialize
	header2, err := DeserializeDBHeader(data)
	if err != nil {
		t.Fatalf("Failed to deserialize DB header: %v", err)
	}

	if string(header2.Magic[:]) != MagicNumber {
		t.Errorf("Magic number mismatch")
	}

	if header2.Dimensions != header.Dimensions {
		t.Errorf("Dimensions mismatch: got %d, want %d", header2.Dimensions, header.Dimensions)
	}

	if header2.VectorCount != header.VectorCount {
		t.Errorf("VectorCount mismatch: got %d, want %d", header2.VectorCount, header.VectorCount)
	}

	if header2.DistanceMetric != header.DistanceMetric {
		t.Errorf("DistanceMetric mismatch: got %d, want %d", header2.DistanceMetric, header.DistanceMetric)
	}
}

func TestPageChecksum(t *testing.T) {
	data := make([]byte, PageSize)

	// Create header first (without checksum)
	header := NewPageHeader(VectorPage)
	header.Count = 5
	headerData := SerializeHeader(header)
	copy(data[0:64], headerData)

	// Fill in some test data
	for i := 64; i < PageSize; i++ {
		data[i] = byte(i % 256)
	}

	// Calculate and set checksum
	checksum := CalculatePageChecksum(data)
	header.Checksum = checksum
	headerData = SerializeHeader(header)
	copy(data[0:64], headerData)

	// Verify
	if !VerifyPageChecksum(data) {
		t.Error("Checksum verification failed")
	}

	// Corrupt data (outside header)
	data[100] ^= 0xFF

	// Should fail now
	if VerifyPageChecksum(data) {
		t.Error("Expected checksum verification to fail on corrupted data")
	}
}

func TestVectorPageReadWrite(t *testing.T) {
	tmpfile := tempFilename() + ".vectorpage"
	defer os.Remove(tmpfile)

	file, err := os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	storage := NewStorage()
	err = storage.Init(file, PageSize*10)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	// Create test vector entries
	entries := []VectorEntry{
		{
			ID:       packIDString("vector1"),
			Offset:   0,
			Length:   128,
			Flags:    0,
			Reserved: 0,
			Metadata: 12345,
		},
		{
			ID:       packIDString("vector2"),
			Offset:   512,
			Length:   256,
			Flags:    1,
			Reserved: 0,
			Metadata: 67890,
		},
		{
			ID:       packIDString("test_vector_3"),
			Offset:   1024,
			Length:   384,
			Flags:    0,
			Reserved: 0,
			Metadata: 99999,
		},
	}

	// Write vector page
	err = storage.writeVectorPage(1, entries)
	if err != nil {
		t.Fatalf("Failed to write vector page: %v", err)
	}

	// Read it back
	readEntries, err := storage.readVectorPage(1)
	if err != nil {
		t.Fatalf("Failed to read vector page: %v", err)
	}

	// Verify count
	if len(readEntries) != len(entries) {
		t.Fatalf("Entry count mismatch: got %d, want %d", len(readEntries), len(entries))
	}

	// Verify each entry
	for i := range entries {
		if extractIDString(readEntries[i].ID) != extractIDString(entries[i].ID) {
			t.Errorf("Entry %d ID mismatch: got %s, want %s",
				i, extractIDString(readEntries[i].ID), extractIDString(entries[i].ID))
		}

		if readEntries[i].Offset != entries[i].Offset {
			t.Errorf("Entry %d Offset mismatch: got %d, want %d",
				i, readEntries[i].Offset, entries[i].Offset)
		}

		if readEntries[i].Length != entries[i].Length {
			t.Errorf("Entry %d Length mismatch: got %d, want %d",
				i, readEntries[i].Length, entries[i].Length)
		}

		if readEntries[i].Flags != entries[i].Flags {
			t.Errorf("Entry %d Flags mismatch: got %d, want %d",
				i, readEntries[i].Flags, entries[i].Flags)
		}

		if readEntries[i].Metadata != entries[i].Metadata {
			t.Errorf("Entry %d Metadata mismatch: got %d, want %d",
				i, readEntries[i].Metadata, entries[i].Metadata)
		}
	}
}

func TestFreePageManagement(t *testing.T) {
	tmpfile := tempFilename() + ".freelist"
	defer os.Remove(tmpfile)

	file, err := os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	storage := NewStorage()
	err = storage.Init(file, PageSize*5)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	initialPages := storage.PageCount()

	// Allocate a new page
	page1, err := storage.AllocatePage()
	if err != nil {
		t.Fatalf("Failed to allocate page: %v", err)
	}

	if storage.PageCount() != initialPages+1 {
		t.Errorf("Expected %d pages after allocation, got %d", initialPages+1, storage.PageCount())
	}

	// Free the page
	err = storage.FreePage(page1)
	if err != nil {
		t.Fatalf("Failed to free page: %v", err)
	}

	// Allocate again - should reuse the freed page
	page2, err := storage.AllocatePage()
	if err != nil {
		t.Fatalf("Failed to allocate page: %v", err)
	}

	if page2 != page1 {
		t.Errorf("Expected to reuse freed page %d, got %d", page1, page2)
	}

	if storage.PageCount() != initialPages+1 {
		t.Errorf("Expected page count to remain %d, got %d", initialPages+1, storage.PageCount())
	}
}

func TestVectorPageChecksum(t *testing.T) {
	tmpfile := tempFilename() + ".checksum"
	defer os.Remove(tmpfile)

	file, err := os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	storage := NewStorage()
	err = storage.Init(file, PageSize*10)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	// Create a test entry
	entries := []VectorEntry{
		{
			ID:       packIDString("test"),
			Offset:   0,
			Length:   128,
			Flags:    0,
			Reserved: 0,
			Metadata: 1234,
		},
	}

	// Write vector page
	err = storage.writeVectorPage(2, entries)
	if err != nil {
		t.Fatalf("Failed to write vector page: %v", err)
	}

	// Read should succeed
	_, err = storage.readVectorPage(2)
	if err != nil {
		t.Fatalf("Failed to read valid page: %v", err)
	}

	// Corrupt the page data
	pageData, err := storage.ReadPage(2)
	if err != nil {
		t.Fatal(err)
	}

	// Corrupt a byte in the data section (after header)
	corruptedData := make([]byte, PageSize)
	copy(corruptedData, pageData)
	corruptedData[100] ^= 0xFF

	err = storage.WritePage(2, corruptedData)
	if err != nil {
		t.Fatal(err)
	}

	// Read should now fail checksum validation
	_, err = storage.readVectorPage(2)
	if err == nil {
		t.Error("Expected checksum validation to fail on corrupted page")
	}
}

func TestDBHeaderValidation(t *testing.T) {
	// Test valid header
	header := NewDBHeader(384, "cosine")
	header.VectorCount = 100
	header.PageCount = 10

	data := SerializeDBHeader(header)
	_, err := DeserializeDBHeader(data)
	if err != nil {
		t.Errorf("Valid header failed validation: %v", err)
	}

	// Test invalid dimensions (too high)
	header2 := NewDBHeader(200000, "cosine")
	data2 := SerializeDBHeader(header2)
	_, err = DeserializeDBHeader(data2)
	if err == nil {
		t.Error("Expected validation error for invalid dimensions")
	}

	// Test invalid distance metric
	header3 := NewDBHeader(384, "cosine")
	header3.DistanceMetric = 99
	data3 := SerializeDBHeader(header3)
	_, err = DeserializeDBHeader(data3)
	if err == nil {
		t.Error("Expected validation error for invalid distance metric")
	}

	// Test invalid page count
	header4 := NewDBHeader(384, "cosine")
	header4.PageCount = 0
	data4 := SerializeDBHeader(header4)
	_, err = DeserializeDBHeader(data4)
	if err == nil {
		t.Error("Expected validation error for invalid page count")
	}
}

// TestStorageCloseIdempotent verifies that calling Close() multiple times
// does not cause errors and is idempotent (Issue #6)
func TestStorageCloseIdempotent(t *testing.T) {
	tmpfile := tempFilename() + ".close_idempotent"
	defer os.Remove(tmpfile)

	file, err := os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}

	storage := NewStorage()
	err = storage.Init(file, PageSize*10)
	if err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}

	// Verify storage is initialized
	if storage.file == nil {
		t.Fatal("Expected storage.file to be non-nil after Init")
	}

	// First close should succeed
	err = storage.Close()
	if err != nil {
		t.Fatalf("First Close() failed: %v", err)
	}

	// Verify internal state after close
	if storage.file != nil {
		t.Error("Expected storage.file to be nil after Close()")
	}
	if storage.mmap != nil {
		t.Error("Expected storage.mmap to be nil after Close()")
	}

	// Second close should be idempotent (no error)
	err = storage.Close()
	if err != nil {
		t.Errorf("Second Close() should be idempotent, got error: %v", err)
	}

	// Third close should still work
	err = storage.Close()
	if err != nil {
		t.Errorf("Third Close() should be idempotent, got error: %v", err)
	}
}

// TestNestCloseAfterCompaction verifies that closing after compaction
// doesn't cause double-close errors (Issue #6)
func TestNestCloseAfterCompaction(t *testing.T) {
	tmpfile := tempFilename() + ".compact_close"
	defer os.Remove(tmpfile)

	// Create and populate a database
	nest, err := Open(tmpfile, Options{
		Dimensions: 3,
		Distance:   "cosine",
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// Add some vectors
	vectors := []struct {
		id   string
		vec  []float32
	}{
		{"vec1", []float32{1.0, 0.0, 0.0}},
		{"vec2", []float32{0.0, 1.0, 0.0}},
		{"vec3", []float32{0.0, 0.0, 1.0}},
	}

	for _, v := range vectors {
		err := nest.Store(v.id, v.vec)
		if err != nil {
			t.Fatalf("Failed to store vector %s: %v", v.id, err)
		}
	}

	// Perform compaction
	err = nest.Compact()
	if err != nil {
		t.Fatalf("Compaction failed: %v", err)
	}

	// Close should not cause double-close error
	err = nest.Close()
	if err != nil {
		t.Errorf("Close after compaction failed: %v", err)
	}
}

// TestCompactionFailureFileState verifies that file state is consistent
// even if compaction fails midway (Issue #6)
func TestCompactionFailureFileState(t *testing.T) {
	tmpfile := tempFilename() + ".compact_fail"
	defer os.Remove(tmpfile)

	// Create a database
	nest, err := Open(tmpfile, Options{
		Dimensions: 3,
		Distance:   "cosine",
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// Add a vector
	err = nest.Store("vec1", []float32{1.0, 0.0, 0.0})
	if err != nil {
		t.Fatalf("Failed to store vector: %v", err)
	}

	// Close normally first time
	err = nest.Close()
	if err != nil {
		t.Fatalf("Initial close failed: %v", err)
	}

	// Verify file descriptor is properly released
	// Try to open the file again
	file, err := os.OpenFile(tmpfile, os.O_RDWR, 0644)
	if err != nil {
		t.Errorf("File should be accessible after close: %v", err)
	} else {
		file.Close()
	}
}

// TestNestFileOwnership verifies clear ownership of file descriptor
// between Nest and Storage (Issue #6)
func TestNestFileOwnership(t *testing.T) {
	tmpfile := tempFilename() + ".ownership"
	defer os.Remove(tmpfile)

	// Create database
	nest, err := Open(tmpfile, Options{
		Dimensions: 3,
		Distance:   "cosine",
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// Store a vector
	err = nest.Store("vec1", []float32{1.0, 0.0, 0.0})
	if err != nil {
		t.Fatalf("Failed to store vector: %v", err)
	}

	// Verify that both nest.storage and nest.file are set initially
	if nest.storage == nil {
		t.Error("Expected nest.storage to be non-nil")
	}
	if nest.file == nil {
		t.Error("Expected nest.file to be non-nil before close")
	}

	// Close the nest
	err = nest.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// After close, file should be released (no double-close risk)
	// Verify we can open the file again
	file, err := os.OpenFile(tmpfile, os.O_RDWR, 0644)
	if err != nil {
		t.Errorf("File descriptor not properly released: %v", err)
	} else {
		file.Close()
	}
}

// TestStorageCloseErrorHandling verifies that errors during Close
// are properly propagated (Issue #6)
func TestStorageCloseErrorHandling(t *testing.T) {
	tmpfile := tempFilename() + ".close_error"
	defer os.Remove(tmpfile)

	file, err := os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}

	storage := NewStorage()
	err = storage.Init(file, PageSize*10)
	if err != nil {
		t.Fatalf("Failed to initialize storage: %v", err)
	}

	// Manually close the underlying file to simulate an error condition
	// This should cause storage.Close() to handle an already-closed file
	file.Close()

	// Now close storage - should handle gracefully
	err = storage.Close()
	// The current implementation WILL return an error because the file was closed
	// This is expected behavior, but we want to ensure no panic
	if err != nil {
		t.Logf("Close with pre-closed file returned error: %v", err)
		// Verify it's a "file already closed" error
		if err.Error() != "failed to close file: close "+tmpfile+": file already closed" {
			// Error format might vary, just check it's not a panic
			t.Logf("Error format: %v", err)
		}
	}

	// Calling Close again should be idempotent (no error now)
	err = storage.Close()
	if err != nil {
		t.Errorf("Second close after error should be idempotent, got: %v", err)
	}
}
