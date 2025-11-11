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
