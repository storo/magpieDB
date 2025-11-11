package magpie

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestWALOpen tests opening and creating a WAL file.
func TestWALOpen(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal := NewWAL()
	defer wal.Close()

	// Test creating a new WAL file
	if err := wal.Open(walPath); err != nil {
		t.Fatalf("Failed to open new WAL: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(walPath); os.IsNotExist(err) {
		t.Errorf("WAL file was not created")
	}

	// Verify WAL can be reopened
	wal.Close()
	wal2 := NewWAL()
	defer wal2.Close()
	if err := wal2.Open(walPath); err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}
}

// TestWALAppend tests appending entries to the WAL.
func TestWALAppend(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal := NewWAL()
	defer wal.Close()

	if err := wal.Open(walPath); err != nil {
		t.Fatalf("Failed to open WAL: %v", err)
	}

	// Append entries
	entries := []WALEntry{
		{
			Type:     WALInsert,
			ID:       "vec1",
			Vector:   []float32{1.0, 2.0, 3.0},
			Metadata: map[string]interface{}{"key": "value1"},
		},
		{
			Type:     WALUpdate,
			ID:       "vec2",
			Vector:   []float32{4.0, 5.0, 6.0},
			Metadata: map[string]interface{}{"key": "value2"},
		},
		{
			Type:   WALDelete,
			ID:     "vec3",
			Vector: []float32{7.0, 8.0, 9.0},
		},
	}

	for _, entry := range entries {
		if err := wal.Append(entry); err != nil {
			t.Errorf("Failed to append entry: %v", err)
		}
	}

	// Flush to disk
	if err := wal.Flush(); err != nil {
		t.Errorf("Failed to flush WAL: %v", err)
	}

	// Verify sequence numbers are monotonic
	readEntries, err := wal.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read WAL: %v", err)
	}

	if len(readEntries) != len(entries) {
		t.Fatalf("Expected %d entries, got %d", len(entries), len(readEntries))
	}

	for i := 0; i < len(readEntries); i++ {
		if readEntries[i].Sequence != uint64(i+1) {
			t.Errorf("Entry %d: expected sequence %d, got %d", i, i+1, readEntries[i].Sequence)
		}
		if readEntries[i].Type != entries[i].Type {
			t.Errorf("Entry %d: expected type %d, got %d", i, entries[i].Type, readEntries[i].Type)
		}
		if readEntries[i].ID != entries[i].ID {
			t.Errorf("Entry %d: expected ID %s, got %s", i, entries[i].ID, readEntries[i].ID)
		}
	}
}

// TestWALFlush tests explicit flushing of buffered entries.
func TestWALFlush(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal := NewWAL()
	defer wal.Close()

	if err := wal.Open(walPath); err != nil {
		t.Fatalf("Failed to open WAL: %v", err)
	}

	// Append entry (buffered, not flushed yet)
	entry := WALEntry{
		Type:     WALInsert,
		ID:       "vec1",
		Vector:   []float32{1.0, 2.0, 3.0},
		Metadata: map[string]interface{}{"key": "value"},
	}

	if err := wal.Append(entry); err != nil {
		t.Fatalf("Failed to append entry: %v", err)
	}

	// Explicit flush
	if err := wal.Flush(); err != nil {
		t.Fatalf("Failed to flush WAL: %v", err)
	}

	// Verify data was written to disk by reopening
	wal.Close()
	wal2 := NewWAL()
	defer wal2.Close()

	if err := wal2.Open(walPath); err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}

	entries, err := wal2.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read WAL: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if entries[0].ID != "vec1" {
		t.Errorf("Expected ID 'vec1', got '%s'", entries[0].ID)
	}
}

// TestWALReadAll tests reading all entries from the WAL.
func TestWALReadAll(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal := NewWAL()
	defer wal.Close()

	if err := wal.Open(walPath); err != nil {
		t.Fatalf("Failed to open WAL: %v", err)
	}

	// Write multiple entries
	expectedCount := 50
	for i := 0; i < expectedCount; i++ {
		entry := WALEntry{
			Type:     WALInsert,
			ID:       fmt.Sprintf("vec%d", i),
			Vector:   []float32{float32(i), float32(i + 1), float32(i + 2)},
			Metadata: map[string]interface{}{"index": i},
		}
		if err := wal.Append(entry); err != nil {
			t.Fatalf("Failed to append entry %d: %v", i, err)
		}
	}

	// Flush all entries
	if err := wal.Flush(); err != nil {
		t.Fatalf("Failed to flush WAL: %v", err)
	}

	// Read all entries
	entries, err := wal.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read WAL: %v", err)
	}

	if len(entries) != expectedCount {
		t.Fatalf("Expected %d entries, got %d", expectedCount, len(entries))
	}

	// Verify content
	for i, entry := range entries {
		expectedID := fmt.Sprintf("vec%d", i)
		if entry.ID != expectedID {
			t.Errorf("Entry %d: expected ID %s, got %s", i, expectedID, entry.ID)
		}
		if entry.Vector[0] != float32(i) {
			t.Errorf("Entry %d: expected vector[0] = %f, got %f", i, float32(i), entry.Vector[0])
		}
	}
}

// TestWALChecksum verifies CRC32 checksum validation.
func TestWALChecksum(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal := NewWAL()
	defer wal.Close()

	if err := wal.Open(walPath); err != nil {
		t.Fatalf("Failed to open WAL: %v", err)
	}

	// Append entry
	entry := WALEntry{
		Type:     WALInsert,
		ID:       "vec1",
		Vector:   []float32{1.0, 2.0, 3.0},
		Metadata: map[string]interface{}{"key": "value"},
	}

	if err := wal.Append(entry); err != nil {
		t.Fatalf("Failed to append entry: %v", err)
	}

	if err := wal.Flush(); err != nil {
		t.Fatalf("Failed to flush WAL: %v", err)
	}

	// Read back and verify checksum is set
	entries, err := wal.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read WAL: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if entries[0].Checksum == 0 {
		t.Error("Checksum was not set on entry")
	}

	// Verify checksum is valid (ReadAll verifies checksums internally)
	// If we got here without error, checksum is valid
}

// TestWALCorruption tests detection of corrupted entries.
func TestWALCorruption(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal := NewWAL()
	if err := wal.Open(walPath); err != nil {
		t.Fatalf("Failed to open WAL: %v", err)
	}

	// Write valid entry
	entry := WALEntry{
		Type:     WALInsert,
		ID:       "vec1",
		Vector:   []float32{1.0, 2.0, 3.0},
		Metadata: map[string]interface{}{"key": "value"},
	}

	if err := wal.Append(entry); err != nil {
		t.Fatalf("Failed to append entry: %v", err)
	}

	if err := wal.Flush(); err != nil {
		t.Fatalf("Failed to flush WAL: %v", err)
	}

	wal.Close()

	// Corrupt the WAL file by modifying a byte
	file, err := os.OpenFile(walPath, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("Failed to open WAL for corruption: %v", err)
	}

	// Seek to middle of file and corrupt a byte
	stat, _ := file.Stat()
	if stat.Size() > 20 {
		file.Seek(20, 0)
		file.Write([]byte{0xFF})
	}
	file.Close()

	// Try to read corrupted WAL
	wal2 := NewWAL()
	defer wal2.Close()

	if err := wal2.Open(walPath); err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}

	_, err = wal2.ReadAll()
	if err == nil {
		t.Error("Expected error when reading corrupted WAL, got nil")
	}
}

// TestWALConcurrent tests concurrent append operations.
func TestWALConcurrent(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal := NewWAL()
	defer wal.Close()

	if err := wal.Open(walPath); err != nil {
		t.Fatalf("Failed to open WAL: %v", err)
	}

	// Concurrent appends
	numGoroutines := 10
	entriesPerGoroutine := 100
	var wg sync.WaitGroup

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < entriesPerGoroutine; i++ {
				entry := WALEntry{
					Type:     WALInsert,
					ID:       fmt.Sprintf("g%d-vec%d", goroutineID, i),
					Vector:   []float32{float32(goroutineID), float32(i)},
					Metadata: map[string]interface{}{"g": goroutineID, "i": i},
				}
				if err := wal.Append(entry); err != nil {
					t.Errorf("Goroutine %d: Failed to append entry %d: %v", goroutineID, i, err)
				}
			}
		}(g)
	}

	wg.Wait()

	// Flush all entries
	if err := wal.Flush(); err != nil {
		t.Fatalf("Failed to flush WAL: %v", err)
	}

	// Verify all entries were written
	entries, err := wal.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read WAL: %v", err)
	}

	expectedCount := numGoroutines * entriesPerGoroutine
	if len(entries) != expectedCount {
		t.Fatalf("Expected %d entries, got %d", expectedCount, len(entries))
	}

	// Verify sequence numbers are unique and monotonic
	seqMap := make(map[uint64]bool)
	for i, entry := range entries {
		if seqMap[entry.Sequence] {
			t.Errorf("Duplicate sequence number %d at entry %d", entry.Sequence, i)
		}
		seqMap[entry.Sequence] = true

		if i > 0 && entry.Sequence <= entries[i-1].Sequence {
			t.Errorf("Non-monotonic sequence at entry %d: %d <= %d", i, entry.Sequence, entries[i-1].Sequence)
		}
	}
}

// TestWALBatchWrite tests auto-flush after 100 entries.
func TestWALBatchWrite(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal := NewWAL()
	defer wal.Close()

	if err := wal.Open(walPath); err != nil {
		t.Fatalf("Failed to open WAL: %v", err)
	}

	// Write exactly 100 entries (should trigger auto-flush)
	for i := 0; i < 100; i++ {
		entry := WALEntry{
			Type:     WALInsert,
			ID:       fmt.Sprintf("vec%d", i),
			Vector:   []float32{float32(i)},
			Metadata: map[string]interface{}{"index": i},
		}
		if err := wal.Append(entry); err != nil {
			t.Fatalf("Failed to append entry %d: %v", i, err)
		}
	}

	// Auto-flush should have occurred, verify by reading without explicit flush
	entries, err := wal.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read WAL: %v", err)
	}

	if len(entries) != 100 {
		t.Fatalf("Expected 100 entries after auto-flush, got %d", len(entries))
	}

	// Write 50 more entries (should not auto-flush yet)
	for i := 100; i < 150; i++ {
		entry := WALEntry{
			Type:     WALInsert,
			ID:       fmt.Sprintf("vec%d", i),
			Vector:   []float32{float32(i)},
			Metadata: map[string]interface{}{"index": i},
		}
		if err := wal.Append(entry); err != nil {
			t.Fatalf("Failed to append entry %d: %v", i, err)
		}
	}

	// Explicit flush to persist remaining entries
	if err := wal.Flush(); err != nil {
		t.Fatalf("Failed to flush WAL: %v", err)
	}

	// Verify all 150 entries
	entries, err = wal.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read WAL: %v", err)
	}

	if len(entries) != 150 {
		t.Fatalf("Expected 150 entries, got %d", len(entries))
	}
}

// TestWALTruncate tests truncating the WAL after replay.
func TestWALTruncate(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal := NewWAL()
	defer wal.Close()

	if err := wal.Open(walPath); err != nil {
		t.Fatalf("Failed to open WAL: %v", err)
	}

	// Write entries
	for i := 0; i < 10; i++ {
		entry := WALEntry{
			Type:     WALInsert,
			ID:       fmt.Sprintf("vec%d", i),
			Vector:   []float32{float32(i)},
			Metadata: map[string]interface{}{"index": i},
		}
		if err := wal.Append(entry); err != nil {
			t.Fatalf("Failed to append entry %d: %v", i, err)
		}
	}

	if err := wal.Flush(); err != nil {
		t.Fatalf("Failed to flush WAL: %v", err)
	}

	// Verify entries exist
	entries, err := wal.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read WAL: %v", err)
	}

	if len(entries) != 10 {
		t.Fatalf("Expected 10 entries before truncate, got %d", len(entries))
	}

	// Truncate WAL
	if err := wal.Truncate(); err != nil {
		t.Fatalf("Failed to truncate WAL: %v", err)
	}

	// Verify WAL is empty
	entries, err = wal.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read WAL after truncate: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("Expected 0 entries after truncate, got %d", len(entries))
	}

	// Verify we can still append after truncate
	entry := WALEntry{
		Type:     WALInsert,
		ID:       "vec_after_truncate",
		Vector:   []float32{1.0, 2.0, 3.0},
		Metadata: map[string]interface{}{"key": "value"},
	}

	if err := wal.Append(entry); err != nil {
		t.Fatalf("Failed to append after truncate: %v", err)
	}

	if err := wal.Flush(); err != nil {
		t.Fatalf("Failed to flush after truncate: %v", err)
	}

	entries, err = wal.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read WAL after truncate and append: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry after truncate and append, got %d", len(entries))
	}
}

// TestWALClose tests proper cleanup on close.
func TestWALClose(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal := NewWAL()

	if err := wal.Open(walPath); err != nil {
		t.Fatalf("Failed to open WAL: %v", err)
	}

	// Append entry without explicit flush
	entry := WALEntry{
		Type:     WALInsert,
		ID:       "vec1",
		Vector:   []float32{1.0, 2.0, 3.0},
		Metadata: map[string]interface{}{"key": "value"},
	}

	if err := wal.Append(entry); err != nil {
		t.Fatalf("Failed to append entry: %v", err)
	}

	// Close should flush buffered entries
	if err := wal.Close(); err != nil {
		t.Fatalf("Failed to close WAL: %v", err)
	}

	// Verify entry was persisted by reopening
	wal2 := NewWAL()
	defer wal2.Close()

	if err := wal2.Open(walPath); err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}

	entries, err := wal2.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read WAL: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry after close, got %d", len(entries))
	}

	if entries[0].ID != "vec1" {
		t.Errorf("Expected ID 'vec1', got '%s'", entries[0].ID)
	}
}

// TestWALLargeEntries tests handling of large vector entries.
func TestWALLargeEntries(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal := NewWAL()
	defer wal.Close()

	if err := wal.Open(walPath); err != nil {
		t.Fatalf("Failed to open WAL: %v", err)
	}

	// Create large vector (10000 dimensions)
	largeVector := make([]float32, 10000)
	for i := range largeVector {
		largeVector[i] = rand.Float32()
	}

	entry := WALEntry{
		Type:     WALInsert,
		ID:       "large_vec",
		Vector:   largeVector,
		Metadata: map[string]interface{}{"dimensions": 10000},
	}

	if err := wal.Append(entry); err != nil {
		t.Fatalf("Failed to append large entry: %v", err)
	}

	if err := wal.Flush(); err != nil {
		t.Fatalf("Failed to flush WAL: %v", err)
	}

	// Read back and verify
	entries, err := wal.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read WAL: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if len(entries[0].Vector) != 10000 {
		t.Fatalf("Expected vector with 10000 dimensions, got %d", len(entries[0].Vector))
	}

	// Verify vector content
	for i := 0; i < 10000; i++ {
		if entries[0].Vector[i] != largeVector[i] {
			t.Errorf("Vector mismatch at index %d: expected %f, got %f", i, largeVector[i], entries[0].Vector[i])
			break
		}
	}
}

// TestWALSequenceRecovery tests sequence number recovery after reopen.
func TestWALSequenceRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// First session: write 10 entries
	wal1 := NewWAL()
	if err := wal1.Open(walPath); err != nil {
		t.Fatalf("Failed to open WAL: %v", err)
	}

	for i := 0; i < 10; i++ {
		entry := WALEntry{
			Type:     WALInsert,
			ID:       fmt.Sprintf("vec%d", i),
			Vector:   []float32{float32(i)},
			Metadata: map[string]interface{}{"index": i},
		}
		if err := wal1.Append(entry); err != nil {
			t.Fatalf("Failed to append entry %d: %v", i, err)
		}
	}

	if err := wal1.Flush(); err != nil {
		t.Fatalf("Failed to flush WAL: %v", err)
	}

	wal1.Close()

	// Second session: reopen and append more entries
	wal2 := NewWAL()
	defer wal2.Close()

	if err := wal2.Open(walPath); err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}

	// Append 5 more entries - sequence should continue from 11
	for i := 10; i < 15; i++ {
		entry := WALEntry{
			Type:     WALInsert,
			ID:       fmt.Sprintf("vec%d", i),
			Vector:   []float32{float32(i)},
			Metadata: map[string]interface{}{"index": i},
		}
		if err := wal2.Append(entry); err != nil {
			t.Fatalf("Failed to append entry %d: %v", i, err)
		}
	}

	if err := wal2.Flush(); err != nil {
		t.Fatalf("Failed to flush WAL: %v", err)
	}

	// Verify all 15 entries with continuous sequence numbers
	entries, err := wal2.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read WAL: %v", err)
	}

	if len(entries) != 15 {
		t.Fatalf("Expected 15 entries, got %d", len(entries))
	}

	for i, entry := range entries {
		expectedSeq := uint64(i + 1)
		if entry.Sequence != expectedSeq {
			t.Errorf("Entry %d: expected sequence %d, got %d", i, expectedSeq, entry.Sequence)
		}
	}
}

// TestWALEmptyMetadata tests handling entries with nil metadata.
func TestWALEmptyMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal := NewWAL()
	defer wal.Close()

	if err := wal.Open(walPath); err != nil {
		t.Fatalf("Failed to open WAL: %v", err)
	}

	// Entry with nil metadata
	entry := WALEntry{
		Type:     WALInsert,
		ID:       "vec1",
		Vector:   []float32{1.0, 2.0, 3.0},
		Metadata: nil,
	}

	if err := wal.Append(entry); err != nil {
		t.Fatalf("Failed to append entry: %v", err)
	}

	if err := wal.Flush(); err != nil {
		t.Fatalf("Failed to flush WAL: %v", err)
	}

	// Read back and verify
	entries, err := wal.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read WAL: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if entries[0].Metadata != nil && len(entries[0].Metadata) != 0 {
		t.Errorf("Expected nil or empty metadata, got %v", entries[0].Metadata)
	}
}

// TestWAL10KEntries tests writing 10K entries without corruption.
func TestWAL10KEntries(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping 10K entries test in short mode")
	}

	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal := NewWAL()
	defer wal.Close()

	if err := wal.Open(walPath); err != nil {
		t.Fatalf("Failed to open WAL: %v", err)
	}

	// Write 10K entries
	count := 10000
	for i := 0; i < count; i++ {
		entry := WALEntry{
			Type:     WALInsert,
			ID:       fmt.Sprintf("vec%d", i),
			Vector:   []float32{float32(i), float32(i + 1), float32(i + 2)},
			Metadata: map[string]interface{}{"index": i, "batch": i / 1000},
		}
		if err := wal.Append(entry); err != nil {
			t.Fatalf("Failed to append entry %d: %v", i, err)
		}
	}

	// Final flush
	if err := wal.Flush(); err != nil {
		t.Fatalf("Failed to flush WAL: %v", err)
	}

	// Verify all entries
	entries, err := wal.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read WAL: %v", err)
	}

	if len(entries) != count {
		t.Fatalf("Expected %d entries, got %d", count, len(entries))
	}

	// Spot check some entries
	for i := 0; i < count; i += 1000 {
		if entries[i].ID != fmt.Sprintf("vec%d", i) {
			t.Errorf("Entry %d: expected ID vec%d, got %s", i, i, entries[i].ID)
		}
		if entries[i].Sequence != uint64(i+1) {
			t.Errorf("Entry %d: expected sequence %d, got %d", i, i+1, entries[i].Sequence)
		}
	}
}
