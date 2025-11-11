package magpie

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestMVCCCopyVersionPreservesData verifies that copyVersion creates a proper deep copy
// that preserves all data integrity without sharing memory with the original version.
// Related to Issue #1: https://github.com/storo/magpieDB/issues/1
func TestMVCCCopyVersionPreservesData(t *testing.T) {
	manager := NewMVCCManager()

	// Create a version with various data types in metadata
	originalVector := []float32{1.0, 2.0, 3.0, 4.0, 5.0}
	originalMetadata := map[string]interface{}{
		"string": "test",
		"int":    42,
		"float":  3.14,
		"bool":   true,
		"nested": map[string]interface{}{
			"inner": "value",
			"count": 10,
		},
		"slice": []string{"a", "b", "c"},
	}

	tx := manager.BeginTx()
	version := manager.CreateVersion(tx.ID, "vec1", originalVector, originalMetadata)
	manager.AddVersion("vec1", version)
	_ = manager.CommitTx(tx)

	// Get the version back through GetVisibleVersion
	tx2 := manager.BeginTx()
	copied := manager.GetVisibleVersion("vec1", tx2)
	_ = manager.CommitTx(tx2)

	if copied == nil {
		t.Fatal("Expected to get a version, got nil")
	}

	// Test 1: Verify vector data is copied correctly
	if len(copied.Vector) != len(originalVector) {
		t.Errorf("Vector length mismatch: expected %d, got %d", len(originalVector), len(copied.Vector))
	}
	for i, v := range originalVector {
		if copied.Vector[i] != v {
			t.Errorf("Vector[%d] mismatch: expected %f, got %f", i, v, copied.Vector[i])
		}
	}

	// Test 2: Verify vector is truly a deep copy (different backing array)
	copied.Vector[0] = 999.0
	tx3 := manager.BeginTx()
	reread := manager.GetVisibleVersion("vec1", tx3)
	_ = manager.CommitTx(tx3)
	if reread.Vector[0] != 1.0 {
		t.Errorf("Original vector was modified! Expected 1.0, got %f", reread.Vector[0])
	}

	// Test 3: Verify metadata is copied correctly
	if copied.Metadata["string"] != "test" {
		t.Error("String metadata not preserved")
	}
	if copied.Metadata["int"] != 42 {
		t.Error("Int metadata not preserved")
	}
	if copied.Metadata["bool"] != true {
		t.Error("Bool metadata not preserved")
	}

	// Test 4: Verify NextVersion is nil (isolated from chain)
	if copied.NextVersion != nil {
		t.Error("Expected NextVersion to be nil (isolated copy), but it points to chain")
	}

	// Test 5: Verify transaction IDs are preserved
	if copied.CreatedByTx != tx.ID {
		t.Errorf("CreatedByTx not preserved: expected %d, got %d", tx.ID, copied.CreatedByTx)
	}
	if copied.DeletedByTx != 0 {
		t.Errorf("DeletedByTx should be 0, got %d", copied.DeletedByTx)
	}

	// Test 6: Verify ID is preserved
	if copied.ID != "vec1" {
		t.Errorf("ID not preserved: expected 'vec1', got '%s'", copied.ID)
	}
}

// TestMVCCConcurrentReadsDifferentVersions tests that multiple concurrent transactions
// can read different versions of the same vector without race conditions or data corruption.
// This simulates real-world MVCC behavior with snapshot isolation.
// Related to Issue #1: https://github.com/storo/magpieDB/issues/1
func TestMVCCConcurrentReadsDifferentVersions(t *testing.T) {
	manager := NewMVCCManager()

	// Create initial version
	tx0 := manager.BeginTx()
	v0 := manager.CreateVersion(tx0.ID, "vec1", []float32{0.0}, map[string]interface{}{"version": 0})
	manager.AddVersion("vec1", v0)
	_ = manager.CommitTx(tx0)

	// Create multiple versions sequentially
	numVersions := 20
	for i := 1; i <= numVersions; i++ {
		tx := manager.BeginTx()
		version := manager.CreateVersion(tx.ID, "vec1",
			[]float32{float32(i), float32(i * 2)},
			map[string]interface{}{
				"version": i,
				"data":    fmt.Sprintf("v%d", i),
			})
		manager.AddVersion("vec1", version)
		_ = manager.CommitTx(tx)
		time.Sleep(time.Microsecond * 10) // Small delay to ensure distinct snapshots
	}

	// Now create many concurrent readers at different snapshot points
	var wg sync.WaitGroup
	numReaders := 50
	results := make(chan int, numReaders)
	errors := make(chan error, numReaders)

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()

			// Each reader gets its own transaction/snapshot
			tx := manager.BeginTx()
			defer manager.CommitTx(tx)

			// Read the same vector multiple times - should always get same version (snapshot isolation)
			var firstVersion *MVCCVersion
			for attempt := 0; attempt < 10; attempt++ {
				version := manager.GetVisibleVersion("vec1", tx)
				if version == nil {
					errors <- fmt.Errorf("reader %d: got nil version on attempt %d", readerID, attempt)
					return
				}

				if firstVersion == nil {
					firstVersion = version
				} else {
					// Verify we always see the same snapshot
					if len(version.Vector) != len(firstVersion.Vector) {
						errors <- fmt.Errorf("reader %d: vector length changed within transaction", readerID)
						return
					}
					for j := range version.Vector {
						if version.Vector[j] != firstVersion.Vector[j] {
							errors <- fmt.Errorf("reader %d: vector data changed within transaction at index %d", readerID, j)
							return
						}
					}
					if version.Version != firstVersion.Version {
						errors <- fmt.Errorf("reader %d: version number changed within transaction", readerID)
						return
					}
				}

				// Verify data integrity
				if version.Metadata == nil {
					errors <- fmt.Errorf("reader %d: metadata is nil", readerID)
					return
				}

				// Access all fields to trigger potential race conditions
				_ = version.ID
				_ = version.Vector
				_ = version.Metadata
				_ = version.Version
				_ = version.CreatedByTx
				_ = version.DeletedByTx
				_ = version.NextVersion

				time.Sleep(time.Microsecond)
			}

			// Report which version this reader saw
			if firstVersion != nil && firstVersion.Metadata != nil {
				if verNum, ok := firstVersion.Metadata["version"].(int); ok {
					results <- verNum
				}
			}
		}(i)
	}

	// Concurrent writers adding even more versions
	numWriters := 10
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()

			for j := 0; j < 5; j++ {
				tx := manager.BeginTx()
				version := manager.CreateVersion(tx.ID, "vec1",
					[]float32{float32(1000 + writerID*10 + j)},
					map[string]interface{}{
						"version": 1000 + writerID*10 + j,
						"writer":  writerID,
					})
				manager.AddVersion("vec1", version)
				_ = manager.CommitTx(tx)
				time.Sleep(time.Microsecond * 50)
			}
		}(i)
	}

	wg.Wait()
	close(results)
	close(errors)

	// Check for any errors
	errorCount := 0
	for err := range errors {
		t.Error(err)
		errorCount++
	}

	if errorCount > 0 {
		t.Fatalf("Found %d errors during concurrent reads", errorCount)
	}

	// Verify we got results from all readers
	resultCount := 0
	seenVersions := make(map[int]int)
	for verNum := range results {
		resultCount++
		seenVersions[verNum]++
	}

	if resultCount != numReaders {
		t.Errorf("Expected %d results, got %d", numReaders, resultCount)
	}

	t.Logf("Concurrent readers saw %d different versions", len(seenVersions))
	for ver, count := range seenVersions {
		t.Logf("  Version %d: seen by %d readers", ver, count)
	}
}

// TestMVCCMetadataDeepCopy specifically tests that nested metadata structures
// are properly deep-copied to prevent shared memory issues.
// Related to Issue #1: https://github.com/storo/magpieDB/issues/1
func TestMVCCMetadataDeepCopy(t *testing.T) {
	manager := NewMVCCManager()

	// Create version with deeply nested metadata
	nestedMap := map[string]interface{}{
		"level1": map[string]interface{}{
			"level2": map[string]interface{}{
				"level3": "deep_value",
			},
		},
	}

	nestedSlice := []interface{}{
		map[string]interface{}{"item": 1},
		map[string]interface{}{"item": 2},
	}

	metadata := map[string]interface{}{
		"simple":       "value",
		"nested_map":   nestedMap,
		"nested_slice": nestedSlice,
		"number":       42,
	}

	tx1 := manager.BeginTx()
	version := manager.CreateVersion(tx1.ID, "vec1", []float32{1.0}, metadata)
	manager.AddVersion("vec1", version)
	_ = manager.CommitTx(tx1)

	// Get version through GetVisibleVersion
	tx2 := manager.BeginTx()
	copied := manager.GetVisibleVersion("vec1", tx2)
	_ = manager.CommitTx(tx2)

	if copied == nil {
		t.Fatal("Expected to get version, got nil")
	}

	// Test 1: Verify simple values are copied
	if copied.Metadata["simple"] != "value" {
		t.Error("Simple metadata value not preserved")
	}
	if copied.Metadata["number"] != 42 {
		t.Error("Number metadata value not preserved")
	}

	// Test 2: Modify the returned metadata
	if copiedMeta, ok := copied.Metadata["nested_map"].(map[string]interface{}); ok {
		copiedMeta["modified"] = true
	}

	// WARNING: The current implementation does shallow copy of metadata values
	// This test will FAIL if nested structures share memory!
	// Get the version again and check if original is affected
	tx3 := manager.BeginTx()
	reread := manager.GetVisibleVersion("vec1", tx3)
	_ = manager.CommitTx(tx3)

	if reread == nil {
		t.Fatal("Expected to get version on reread, got nil")
	}

	// Test 3: Check if original nested structure was modified
	if rereadMeta, ok := reread.Metadata["nested_map"].(map[string]interface{}); ok {
		if _, exists := rereadMeta["modified"]; exists {
			t.Error("RACE CONDITION DETECTED: Nested metadata structure is shared (shallow copy)!")
			t.Log("The copyVersion function needs to implement deep copy for nested structures")
		}
	}

	// Test 4: Verify we can access nested values
	if nestedMapCopy, ok := reread.Metadata["nested_map"].(map[string]interface{}); ok {
		if level1, ok := nestedMapCopy["level1"].(map[string]interface{}); ok {
			if level2, ok := level1["level2"].(map[string]interface{}); ok {
				if level3, ok := level2["level3"].(string); !ok || level3 != "deep_value" {
					t.Error("Nested value not preserved correctly")
				}
			} else {
				t.Error("Level 2 nested map not accessible")
			}
		} else {
			t.Error("Level 1 nested map not accessible")
		}
	} else {
		t.Error("Nested map not preserved")
	}

	// Test 5: Test concurrent modifications to metadata
	// This should expose any race conditions with -race flag
	var wg sync.WaitGroup
	numGoroutines := 20

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			tx := manager.BeginTx()
			v := manager.GetVisibleVersion("vec1", tx)
			_ = manager.CommitTx(tx)

			if v != nil && v.Metadata != nil {
				// Try to access nested metadata
				if nm, ok := v.Metadata["nested_map"].(map[string]interface{}); ok {
					_ = nm["level1"]
				}

				// Try to read simple values
				_ = v.Metadata["simple"]
				_ = v.Metadata["number"]
			}
		}(i)
	}

	wg.Wait()
}
