package magpie

import (
	"sync"
	"testing"
	"time"
)

// TestMVCCGetVisibleVersionRaceCondition tests for race conditions
// when reading from version chains while they're being modified.
// This test should fail with -race flag before the fix.
// Related to Issue #1: https://github.com/storo/magpieDB/issues/1
func TestMVCCGetVisibleVersionRaceCondition(t *testing.T) {
	manager := NewMVCCManager()

	// Create initial version
	tx1 := manager.BeginTx()
	version1 := manager.CreateVersion(tx1.ID, "vec1", []float32{1.0, 2.0, 3.0}, map[string]interface{}{"tag": "v1"})
	manager.AddVersion("vec1", version1)
	_ = manager.CommitTx(tx1)

	// Create multiple concurrent readers and writers
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Readers: continuously call GetVisibleVersion
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()

			for j := 0; j < 100; j++ {
				tx := manager.BeginTx()

				// Get visible version - this returns a pointer to the chain
				version := manager.GetVisibleVersion("vec1", tx)
				if version != nil {
					// Access version data - potential race if chain is modified
					_ = version.Vector
					_ = version.Metadata
					_ = version.NextVersion  // Accessing next pointer - race condition!
				}

				_ = manager.CommitTx(tx)
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	// Writers: continuously add new versions to the chain
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()

			for j := 0; j < 50; j++ {
				tx := manager.BeginTx()

				// Create and add new version - modifies the chain
				newVersion := manager.CreateVersion(tx.ID, "vec1",
					[]float32{float32(writerID), float32(j), 3.0},
					map[string]interface{}{"writer": writerID, "iter": j})

				manager.AddVersion("vec1", newVersion)
				_ = manager.CommitTx(tx)

				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		if err != nil {
			t.Errorf("Concurrent access error: %v", err)
		}
	}
}

// TestMVCCGetVisibleVersionReturnsDeepCopy tests that GetVisibleVersion
// returns a deep copy, not a pointer to the live chain.
// This ensures modifications to returned version don't affect the chain.
// Related to Issue #1: https://github.com/storo/magpieDB/issues/1
func TestMVCCGetVisibleVersionReturnsDeepCopy(t *testing.T) {
	manager := NewMVCCManager()

	// Create initial version
	tx1 := manager.BeginTx()
	originalVector := []float32{1.0, 2.0, 3.0}
	originalMetadata := map[string]interface{}{"tag": "original"}
	version1 := manager.CreateVersion(tx1.ID, "vec1", originalVector, originalMetadata)
	manager.AddVersion("vec1", version1)
	_ = manager.CommitTx(tx1)

	// Get visible version
	tx2 := manager.BeginTx()
	retrieved := manager.GetVisibleVersion("vec1", tx2)

	if retrieved == nil {
		t.Fatal("Expected to retrieve version, got nil")
	}

	// Test 1: Verify NextVersion is nil (deep copy shouldn't have chain pointer)
	if retrieved.NextVersion != nil {
		t.Error("Expected NextVersion to be nil in returned copy, but it points to live chain")
	}

	// Test 2: Modify returned version's vector
	if len(retrieved.Vector) > 0 {
		retrieved.Vector[0] = 999.0
	}

	// Test 3: Modify returned version's metadata
	if retrieved.Metadata != nil {
		retrieved.Metadata["modified"] = true
	}

	// Get the version again - should be unchanged
	retrieved2 := manager.GetVisibleVersion("vec1", tx2)
	if retrieved2 == nil {
		t.Fatal("Expected to retrieve version again, got nil")
	}

	// Verify original version data is unchanged
	if len(retrieved2.Vector) > 0 && retrieved2.Vector[0] != 1.0 {
		t.Errorf("Original version was modified! Expected vector[0]=1.0, got %f", retrieved2.Vector[0])
	}

	if retrieved2.Metadata != nil {
		if _, exists := retrieved2.Metadata["modified"]; exists {
			t.Error("Original version metadata was modified!")
		}
		if tag, ok := retrieved2.Metadata["tag"]; !ok || tag != "original" {
			t.Error("Original version metadata was corrupted")
		}
	}

	_ = manager.CommitTx(tx2)
}

// TestMVCCGetVisibleVersionWithConcurrentGC tests GetVisibleVersion
// during garbage collection to ensure no use-after-free issues.
// Related to Issue #1: https://github.com/storo/magpieDB/issues/1
func TestMVCCGetVisibleVersionWithConcurrentGC(t *testing.T) {
	manager := NewMVCCManager()

	// Create multiple versions
	for i := 0; i < 20; i++ {
		tx := manager.BeginTx()
		version := manager.CreateVersion(tx.ID, "vec1",
			[]float32{float32(i), float32(i+1), float32(i+2)},
			map[string]interface{}{"version": i})
		manager.AddVersion("vec1", version)
		_ = manager.CommitTx(tx)
	}

	var wg sync.WaitGroup
	errors := make(chan string, 100)

	// Readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < 100; j++ {
				tx := manager.BeginTx()
				version := manager.GetVisibleVersion("vec1", tx)

				if version != nil {
					// Use the version data
					sum := float32(0)
					for _, v := range version.Vector {
						sum += v
					}
					_ = sum

					// Try to access metadata
					if version.Metadata != nil {
						_ = version.Metadata["version"]
					}
				}

				_ = manager.CommitTx(tx)
			}
		}()
	}

	// Concurrent GC
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < 20; j++ {
				manager.GarbageCollect()
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Error(err)
	}
}

// TestMVCCVersionIsolationAfterModification verifies that a version
// returned by GetVisibleVersion remains valid even after the chain is modified.
// Related to Issue #1: https://github.com/storo/magpieDB/issues/1
func TestMVCCVersionIsolationAfterModification(t *testing.T) {
	manager := NewMVCCManager()

	// Create initial version
	tx1 := manager.BeginTx()
	version1 := manager.CreateVersion(tx1.ID, "vec1", []float32{1.0, 2.0}, map[string]interface{}{"v": 1})
	manager.AddVersion("vec1", version1)
	_ = manager.CommitTx(tx1)

	// Start transaction and get version
	tx2 := manager.BeginTx()
	retrieved := manager.GetVisibleVersion("vec1", tx2)

	if retrieved == nil {
		t.Fatal("Expected to get version")
	}

	// Store original values
	originalVector := make([]float32, len(retrieved.Vector))
	copy(originalVector, retrieved.Vector)

	// Modify the chain by adding new version
	tx3 := manager.BeginTx()
	version2 := manager.CreateVersion(tx3.ID, "vec1", []float32{99.0, 99.0}, map[string]interface{}{"v": 2})
	manager.AddVersion("vec1", version2)
	_ = manager.CommitTx(tx3)

	// The retrieved version should still be valid and unchanged
	for i, v := range retrieved.Vector {
		if v != originalVector[i] {
			t.Errorf("Retrieved version was modified after chain update! Index %d: expected %f, got %f",
				i, originalVector[i], v)
		}
	}

	// Verify the data is still accessible
	if retrieved.Metadata == nil {
		t.Error("Metadata became nil")
	} else if retrieved.Metadata["v"] != 1 {
		t.Error("Metadata was corrupted")
	}

	_ = manager.CommitTx(tx2)
}
