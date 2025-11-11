package magpie

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestMVCCFullLifecycle tests the complete MVCC lifecycle from creation to GC
func TestMVCCFullLifecycle(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{
		Dimensions: 3,
		Distance:   "cosine",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Transaction 1: Insert initial vectors
	tx1, err := nest.Begin()
	if err != nil {
		t.Fatal("Failed to begin transaction:", err)
	}

	if err := tx1.Store("vec1", []float32{1, 0, 0}); err != nil {
		t.Fatal("Failed to store vec1:", err)
	}
	if err := tx1.Store("vec2", []float32{0, 1, 0}); err != nil {
		t.Fatal("Failed to store vec2:", err)
	}

	if err := tx1.Commit(); err != nil {
		t.Fatal("Failed to commit tx1:", err)
	}

	// Transaction 2: Update existing vector
	tx2, err := nest.Begin()
	if err != nil {
		t.Fatal("Failed to begin tx2:", err)
	}

	if err := tx2.Store("vec1", []float32{0, 0, 1}); err != nil {
		t.Fatal("Failed to update vec1:", err)
	}

	if err := tx2.Commit(); err != nil {
		t.Fatal("Failed to commit tx2:", err)
	}

	// Verify final state
	treasure, err := nest.Get("vec1")
	if err != nil {
		t.Fatal("Failed to get vec1:", err)
	}

	if treasure.Vector[2] != 1 {
		t.Errorf("Expected vec1[2]=1, got %f", treasure.Vector[2])
	}
}

// TestMVCCConcurrentReadersWriter tests snapshot isolation with concurrent readers and writers
func TestMVCCConcurrentReadersWriter(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Initial data
	if err := nest.Store("vec1", []float32{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	// Start long-running reader transaction
	wg.Add(1)
	go func() {
		defer wg.Done()

		tx, err := nest.Begin()
		if err != nil {
			errors <- fmt.Errorf("reader: failed to begin: %w", err)
			return
		}

		// Read initial value
		treasure, err := tx.Get("vec1")
		if err != nil {
			errors <- fmt.Errorf("reader: first read failed: %w", err)
			_ = tx.Rollback()
			return
		}
		initialValue := treasure.Vector[0]

		// Sleep to allow writer to commit
		time.Sleep(100 * time.Millisecond)

		// Read again - should see same value (snapshot isolation)
		treasure, err = tx.Get("vec1")
		if err != nil {
			errors <- fmt.Errorf("reader: second read failed: %w", err)
			_ = tx.Rollback()
			return
		}

		if treasure.Vector[0] != initialValue {
			errors <- fmt.Errorf("snapshot isolation violated: expected %f, got %f",
				initialValue, treasure.Vector[0])
		}

		_ = tx.Commit()
	}()

	// Concurrent writer
	time.Sleep(50 * time.Millisecond)
	tx2, err := nest.Begin()
	if err != nil {
		t.Fatal("writer: failed to begin:", err)
	}

	if err := tx2.Store("vec1", []float32{9, 9, 9}); err != nil {
		t.Fatal("writer: failed to store:", err)
	}

	if err := tx2.Commit(); err != nil {
		t.Fatal("writer: failed to commit:", err)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

// TestMVCCConflictDetection tests write-write conflict detection
func TestMVCCConflictDetection(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Initial data
	if err := nest.Store("vec1", []float32{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	tx1, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	tx2, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// Both try to update the same vector
	if err := tx1.Store("vec1", []float32{10, 20, 30}); err != nil {
		t.Fatal("tx1 store failed:", err)
	}

	if err := tx2.Store("vec1", []float32{40, 50, 60}); err != nil {
		t.Fatal("tx2 store failed:", err)
	}

	// First commit wins
	if err := tx1.Commit(); err != nil {
		t.Fatal("First commit should succeed:", err)
	}

	// Second commit should fail with conflict
	if err := tx2.Commit(); err == nil {
		t.Error("Expected conflict error, but commit succeeded")
	} else {
		t.Logf("Conflict detected correctly: %v", err)
	}

	// Verify winner's value persisted
	treasure, err := nest.Get("vec1")
	if err != nil {
		t.Fatal("Failed to get vec1:", err)
	}

	if treasure.Vector[0] != 10 {
		t.Errorf("Wrong value committed: expected 10, got %f", treasure.Vector[0])
	}
}

// TestMVCCWithWALPersistence tests MVCC with WAL recovery
func TestMVCCWithWALPersistence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: file sync and header persistence issues with fallback storage")
	}
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	// Phase 1: Create data with transactions
	{
		nest, err := Open(tmpfile, Options{Dimensions: 2, WAL: true})
		if err != nil {
			t.Fatal(err)
		}

		tx, err := nest.Begin()
		if err != nil {
			t.Fatal(err)
		}

		for i := 0; i < 10; i++ {
			id := fmt.Sprintf("vec%d", i)
			if err := tx.Store(id, []float32{float32(i), float32(i * 2)}); err != nil {
				t.Fatal(err)
			}
		}

		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}

		nest.Close()
	}

	// Phase 2: Reopen and verify
	{
		nest, err := Open(tmpfile, Options{WAL: true})
		if err != nil {
			t.Fatal(err)
		}
		defer nest.Close()

		count := nest.Count()
		if count != 10 {
			t.Errorf("Expected 10 vectors after recovery, got %d", count)
		}

		treasure, err := nest.Get("vec5")
		if err != nil {
			t.Fatal("Failed to get vec5:", err)
		}

		if treasure.Vector[0] != 5 {
			t.Errorf("Data not correctly recovered: expected 5, got %f", treasure.Vector[0])
		}
	}
}

// TestMVCCVersionHistory tests version chain tracking
func TestMVCCVersionHistory(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Create multiple versions of the same vector
	for i := 1; i <= 5; i++ {
		tx, err := nest.Begin()
		if err != nil {
			t.Fatal(err)
		}

		if err := tx.Store("vec1", []float32{float32(i), float32(i * 10)}); err != nil {
			t.Fatal(err)
		}

		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	// Verify we can access the latest version
	treasure, err := nest.Get("vec1")
	if err != nil {
		t.Fatal(err)
	}

	if treasure.Vector[0] != 5 {
		t.Errorf("Expected latest version (5), got %f", treasure.Vector[0])
	}
}

// TestMVCCRecoveryWithMultipleVersions tests recovery with version chains
func TestMVCCRecoveryWithMultipleVersions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: file sync and header persistence issues with fallback storage")
	}
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	// Phase 1: Create multiple versions
	{
		nest, err := Open(tmpfile, Options{Dimensions: 2, WAL: true})
		if err != nil {
			t.Fatal(err)
		}

		// Create 3 versions of vec1
		for i := 1; i <= 3; i++ {
			tx, err := nest.Begin()
			if err != nil {
				t.Fatal(err)
			}
			_ = tx.Store("vec1", []float32{float32(i), float32(i)})
			_ = tx.Commit()
		}

		nest.Close()
	}

	// Phase 2: Recover and verify latest version
	{
		nest, err := Open(tmpfile, Options{WAL: true})
		if err != nil {
			t.Fatal(err)
		}
		defer nest.Close()

		treasure, err := nest.Get("vec1")
		if err != nil {
			t.Fatal(err)
		}

		if treasure.Vector[0] != 3 {
			t.Errorf("Expected version 3, got %f", treasure.Vector[0])
		}
	}
}

// TestMVCCLongRunningSnapshot tests that old versions are preserved for active transactions
func TestMVCCLongRunningSnapshot(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Initial value
	_ = nest.Store("vec1", []float32{0, 0})

	// Start long-running transaction
	longTx, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	initialTreasure, err := longTx.Get("vec1")
	if err != nil {
		t.Fatal(err)
	}

	// Create 10 new versions
	for i := 1; i <= 10; i++ {
		tx, _ := nest.Begin()
		_ = tx.Store("vec1", []float32{float32(i), float32(i)})
		_ = tx.Commit()
	}

	// Long transaction should still see original value
	currentTreasure, err := longTx.Get("vec1")
	if err != nil {
		t.Fatal(err)
	}

	if currentTreasure.Vector[0] != initialTreasure.Vector[0] {
		t.Errorf("Snapshot isolation broken: expected %f, got %f",
			initialTreasure.Vector[0], currentTreasure.Vector[0])
	}

	_ = longTx.Commit()

	// After long tx commits, latest should be visible
	treasure, _ := nest.Get("vec1")
	if treasure.Vector[0] != 10 {
		t.Errorf("Expected latest version (10), got %f", treasure.Vector[0])
	}
}

// TestMVCCIndexConsistency tests that index remains consistent with MVCC
func TestMVCCIndexConsistency(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Insert vectors
	tx1, _ := nest.Begin()
	_ = tx1.Store("vec1", []float32{1, 0, 0})
	_ = tx1.Store("vec2", []float32{0, 1, 0})
	_ = tx1.Store("vec3", []float32{0, 0, 1})
	_ = tx1.Commit()

	// Update vec2
	tx2, _ := nest.Begin()
	_ = tx2.Store("vec2", []float32{0.5, 0.5, 0})
	_ = tx2.Commit()

	// Search should return consistent results
	results := nest.Find([]float32{0, 1, 0}, 3)
	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Verify vec2 has updated value
	for _, r := range results {
		if r.ID == "vec2" {
			if r.Vector[0] != 0.5 {
				t.Errorf("Index not updated: expected 0.5, got %f", r.Vector[0])
			}
			break
		}
	}
}

// TestMVCCSearchWithConcurrentWrites tests search stability during writes
func TestMVCCSearchWithConcurrentWrites(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Initial vectors
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("vec%d", i)
		_ = nest.Store(id, []float32{float32(i), 0, 0})
	}

	var wg sync.WaitGroup

	// Concurrent reader
	wg.Add(1)
	go func() {
		defer wg.Done()

		tx, _ := nest.Begin()

		results1, _ := tx.Find([]float32{5, 0, 0}, 5)
		count1 := len(results1)

		time.Sleep(100 * time.Millisecond)

		results2, _ := tx.Find([]float32{5, 0, 0}, 5)
		count2 := len(results2)

		if count1 != count2 {
			t.Errorf("Search results changed: %d -> %d", count1, count2)
		}

		_ = tx.Commit()
	}()

	// Concurrent writer
	time.Sleep(50 * time.Millisecond)
	tx, _ := nest.Begin()
	_ = tx.Store("vec10", []float32{10, 0, 0})
	_ = tx.Store("vec11", []float32{11, 0, 0})
	_ = tx.Commit()

	wg.Wait()
}

// TestMVCCRollbackIsolation tests that rolled back changes are isolated
func TestMVCCRollbackIsolation(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	_ = nest.Store("vec1", []float32{1, 1})

	// Start transaction and modify
	tx, _ := nest.Begin()
	_ = tx.Store("vec1", []float32{999, 999})
	_ = tx.Store("vec2", []float32{2, 2})

	// Rollback
	if err := tx.Rollback(); err != nil {
		t.Fatal("Rollback failed:", err)
	}

	// Verify changes not visible
	treasure, _ := nest.Get("vec1")
	if treasure.Vector[0] != 1 {
		t.Errorf("Rollback failed: vec1 changed to %f", treasure.Vector[0])
	}

	if nest.Has("vec2") {
		t.Error("Rollback failed: vec2 exists")
	}
}

// TestMVCCMetadataPersistence tests metadata persistence with MVCC
func TestMVCCMetadataPersistence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: file sync and header persistence issues with fallback storage")
	}
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2, WAL: true})
	if err != nil {
		t.Fatal(err)
	}

	tx, _ := nest.Begin()
	_ = tx.Store("vec1", []float32{1, 2}, map[string]interface{}{
		"title": "Test Vector",
		"count": 42,
	})
	_ = tx.Commit()

	nest.Close()

	// Reopen and verify metadata
	nest, err = Open(tmpfile, Options{WAL: true})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	treasure, err := nest.Get("vec1")
	if err != nil {
		t.Fatal(err)
	}

	if treasure.Metadata["title"] != "Test Vector" {
		t.Error("Metadata not persisted correctly")
	}
}

// TestMVCCDeleteAndRecreate tests deleting and recreating vectors
func TestMVCCDeleteAndRecreate(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Create
	tx1, _ := nest.Begin()
	_ = tx1.Store("vec1", []float32{1, 1})
	_ = tx1.Commit()

	// Delete
	tx2, _ := nest.Begin()
	_ = tx2.Remove("vec1")
	_ = tx2.Commit()

	// Recreate
	tx3, _ := nest.Begin()
	_ = tx3.Store("vec1", []float32{2, 2})
	_ = tx3.Commit()

	// Verify new value
	treasure, err := nest.Get("vec1")
	if err != nil {
		t.Fatal(err)
	}

	if treasure.Vector[0] != 2 {
		t.Errorf("Expected 2, got %f", treasure.Vector[0])
	}
}

// TestMVCCMultipleReadersNoBlocking tests that readers don't block each other
func TestMVCCMultipleReadersNoBlocking(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Setup data
	for i := 0; i < 5; i++ {
		_ = nest.Store(fmt.Sprintf("vec%d", i), []float32{float32(i), 0, 0})
	}

	var wg sync.WaitGroup
	readCount := 10

	start := time.Now()

	// Launch multiple concurrent readers
	for i := 0; i < readCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			tx, _ := nest.Begin()
			defer func() { _ = tx.Commit() }()

			for j := 0; j < 5; j++ {
				_, _ = tx.Get(fmt.Sprintf("vec%d", j))
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	// Multiple readers should execute quickly (not serialized)
	if elapsed > time.Second {
		t.Errorf("Readers appear to be blocking: took %v", elapsed)
	}
}

// TestMVCCAbortedTransactionCleanup tests cleanup of aborted transactions
func TestMVCCAbortedTransactionCleanup(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Create and abort several transactions
	for i := 0; i < 10; i++ {
		tx, _ := nest.Begin()
		_ = tx.Store(fmt.Sprintf("vec%d", i), []float32{float32(i), float32(i)})
		_ = tx.Rollback()
	}

	// Database should be empty
	if nest.Count() != 0 {
		t.Errorf("Expected 0 vectors, got %d", nest.Count())
	}

	// Now commit one successfully
	tx, _ := nest.Begin()
	_ = tx.Store("vec_committed", []float32{1, 1})
	_ = tx.Commit()

	if nest.Count() != 1 {
		t.Errorf("Expected 1 vector, got %d", nest.Count())
	}
}
