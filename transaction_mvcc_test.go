package magpie

import (
	"fmt"
	"os"
	"sync"
	"testing"
)

// Test 1: Begin Transaction
func TestMVCCTransactionBegin(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	tx, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if tx == nil {
		t.Error("Expected transaction")
	}
	if !tx.active {
		t.Error("Transaction should be active")
	}
}

// Test 2: Read from Snapshot
func TestMVCCReadFromSnapshot(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Store initial version
	err = nest.Store("vec1", []float32{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}

	// Begin transaction (creates snapshot)
	tx, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// Concurrent update (should not be visible to tx)
	err = nest.Store("vec1", []float32{4, 5, 6})
	if err != nil {
		t.Fatal(err)
	}

	// Transaction should see old version
	treasure, err := tx.Get("vec1")
	if err != nil {
		t.Fatal(err)
	}
	if treasure.Vector[0] != 1 {
		t.Errorf("Expected snapshot version with value 1, got %.2f", treasure.Vector[0])
	}

	tx.Rollback()
}

// Test 3: Write Buffering
func TestMVCCWriteBuffering(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	tx, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// Write should buffer, not visible outside
	err = tx.Store("vec1", []float32{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}

	// Should not be visible outside transaction
	if nest.Has("vec1") {
		t.Error("Uncommitted write visible outside transaction")
	}

	// Should be visible inside transaction
	treasure, err := tx.Get("vec1")
	if err != nil {
		t.Error("Should see own writes")
	}
	if treasure == nil {
		t.Error("Expected treasure from own write")
	}

	tx.Rollback()
}

// Test 4: Conflict Detection
func TestMVCCWriteConflict(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Store initial version
	err = nest.Store("vec1", []float32{1, 2, 3})
	if err != nil {
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

	// Both try to update same vector
	err = tx1.Store("vec1", []float32{10, 20, 30})
	if err != nil {
		t.Fatal(err)
	}
	err = tx2.Store("vec1", []float32{40, 50, 60})
	if err != nil {
		t.Fatal(err)
	}

	// First commit succeeds
	if err := tx1.Commit(); err != nil {
		t.Fatal(err)
	}

	// Second commit should fail (write-write conflict)
	if err := tx2.Commit(); err == nil {
		t.Error("Expected conflict error for concurrent write")
	}
}

// Test 5: Commit Atomicity
func TestMVCCCommitAtomicity(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	tx, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// Multiple writes in transaction
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("vec%d", i)
		err = tx.Store(id, []float32{float32(i)})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Before commit, nothing visible
	if nest.Count() != 0 {
		t.Error("Uncommitted writes visible outside transaction")
	}

	// Commit
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// After commit, all visible
	if nest.Count() != 10 {
		t.Errorf("Expected 10 vectors, got %d", nest.Count())
	}
}

// Test 6: Rollback
func TestMVCCRollback(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	tx, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// Write some data
	err = tx.Store("vec1", []float32{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}

	// Rollback
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// Data should not be visible
	if nest.Has("vec1") {
		t.Error("Rolled back write is visible")
	}
}

// Test 7: Read Your Own Writes
func TestReadYourOwnWrites(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	tx, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// Write
	err = tx.Store("vec1", []float32{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}

	// Read back
	treasure, err := tx.Get("vec1")
	if err != nil {
		t.Fatal(err)
	}
	if treasure.Vector[0] != 1 {
		t.Errorf("Expected 1, got %.2f", treasure.Vector[0])
	}

	// Update
	err = tx.Store("vec1", []float32{4, 5, 6})
	if err != nil {
		t.Fatal(err)
	}

	// Read updated value
	treasure, err = tx.Get("vec1")
	if err != nil {
		t.Fatal(err)
	}
	if treasure.Vector[0] != 4 {
		t.Errorf("Expected 4, got %.2f", treasure.Vector[0])
	}

	tx.Rollback()
}

// Test 8: Concurrent Readers
func TestConcurrentReaders(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Store some data
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("vec%d", i)
		err = nest.Store(id, []float32{float32(i)})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Multiple concurrent readers
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()

			tx, err := nest.Begin()
			if err != nil {
				errors <- err
				return
			}
			defer tx.Rollback()

			// Read multiple vectors
			for j := 0; j < 100; j++ {
				id := fmt.Sprintf("vec%d", j)
				_, err := tx.Get(id)
				if err != nil {
					errors <- fmt.Errorf("reader %d failed to get %s: %w", readerID, id, err)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

// Test 9: Concurrent Writers
func TestConcurrentWriters(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// Multiple writers to different keys
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()

			tx, err := nest.Begin()
			if err != nil {
				return
			}

			// Each writer writes to its own key
			id := fmt.Sprintf("vec%d", writerID)
			err = tx.Store(id, []float32{float32(writerID)})
			if err != nil {
				tx.Rollback()
				return
			}

			if err := tx.Commit(); err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	if successCount != 10 {
		t.Errorf("Expected 10 successful commits, got %d", successCount)
	}
	if nest.Count() != 10 {
		t.Errorf("Expected 10 vectors, got %d", nest.Count())
	}
}

// Test 10: Snapshot Isolation
func TestSnapshotIsolation(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Initial data
	err = nest.Store("vec1", []float32{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}

	// Start transaction 1
	tx1, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// Transaction 2 updates and commits
	tx2, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}
	err = tx2.Store("vec1", []float32{4, 5, 6})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatal(err)
	}

	// Transaction 1 should still see old version
	treasure, err := tx1.Get("vec1")
	if err != nil {
		t.Fatal(err)
	}
	if treasure.Vector[0] != 1 {
		t.Errorf("Snapshot isolation violated: expected 1, got %.2f", treasure.Vector[0])
	}

	tx1.Rollback()
}

// Test 11: Phantom Reads (should NOT occur with SI)
func TestNoPhantomReads(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Initial data
	err = nest.Store("vec1", []float32{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}

	// Start transaction
	tx, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// Read count
	initialCount := nest.Count()

	// Another transaction adds data
	tx2, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}
	err = tx2.Store("vec2", []float32{4, 5, 6})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatal(err)
	}

	// Original transaction should not see new vector (snapshot isolation)
	_, err = tx.Get("vec2")
	if err == nil {
		t.Error("Phantom read detected: new vector visible in old snapshot")
	}

	if initialCount != 1 {
		t.Errorf("Expected count to remain 1, got %d", initialCount)
	}

	tx.Rollback()
}

// Test 12: Lost Update (should NOT occur with SI)
func TestNoLostUpdate(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Initial value
	err = nest.Store("counter", []float32{0})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// Two transactions try to increment counter
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			tx, err := nest.Begin()
			if err != nil {
				return
			}

			// Read current value
			treasure, err := tx.Get("counter")
			if err != nil {
				tx.Rollback()
				return
			}

			// Increment
			newValue := treasure.Vector[0] + 1
			err = tx.Store("counter", []float32{newValue})
			if err != nil {
				tx.Rollback()
				return
			}

			// Commit (one should fail due to conflict)
			if err := tx.Commit(); err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Only one should succeed
	if successCount != 1 {
		t.Errorf("Expected 1 successful update, got %d (lost update vulnerability)", successCount)
	}
}

// Test 13: Commit Ordering
func TestCommitOrdering(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Create transactions in order
	tx1, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}
	tx2, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// Write to different keys
	err = tx1.Store("vec1", []float32{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	err = tx2.Store("vec2", []float32{4, 5, 6})
	if err != nil {
		t.Fatal(err)
	}

	// Commit in reverse order (tx2 before tx1)
	if err := tx2.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := tx1.Commit(); err != nil {
		t.Fatal(err)
	}

	// Both should be visible
	if !nest.Has("vec1") || !nest.Has("vec2") {
		t.Error("Both writes should be visible")
	}
}

// Test 14: Transaction Abort
func TestTransactionAbort(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	tx, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// Write some data
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("vec%d", i)
		err = tx.Store(id, []float32{float32(i)})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Abort by rolling back
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// Nothing should be visible
	if nest.Count() != 0 {
		t.Error("Aborted transaction writes are visible")
	}

	// Transaction should not be reusable
	err = tx.Store("vec99", []float32{99})
	if err == nil {
		t.Error("Expected error when using aborted transaction")
	}
}

// Test 15: Long Running Transaction
func TestLongRunningTransaction(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Initial data
	err = nest.Store("vec1", []float32{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}

	// Start long-running transaction
	tx, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// Many other transactions update data
	for i := 0; i < 10; i++ {
		tx2, err := nest.Begin()
		if err != nil {
			t.Fatal(err)
		}
		err = tx2.Store("vec1", []float32{float32(i), float32(i), float32(i)})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx2.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	// Long-running transaction should still see original version
	treasure, err := tx.Get("vec1")
	if err != nil {
		t.Fatal(err)
	}
	if treasure.Vector[0] != 1 {
		t.Errorf("Expected original version 1, got %.2f", treasure.Vector[0])
	}

	tx.Rollback()
}

// Test 16: High Contention
func TestHighContention(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Initial data
	err = nest.Store("hotspot", []float32{0})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	successCount := 0
	conflictCount := 0
	var mu sync.Mutex

	// Many transactions try to update the same key
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			tx, err := nest.Begin()
			if err != nil {
				return
			}

			err = tx.Store("hotspot", []float32{float32(id)})
			if err != nil {
				tx.Rollback()
				return
			}

			if err := tx.Commit(); err != nil {
				mu.Lock()
				conflictCount++
				mu.Unlock()
			} else {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Only one should succeed, rest should conflict
	if successCount != 1 {
		t.Errorf("Expected 1 success under high contention, got %d", successCount)
	}
	if conflictCount != 19 {
		t.Errorf("Expected 19 conflicts, got %d", conflictCount)
	}
}

// Test 17: Empty Transaction
func TestEmptyTransaction(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	tx, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// Commit without doing anything
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Should succeed
	if nest.Count() != 0 {
		t.Error("Empty transaction should not modify database")
	}
}

// Benchmark 1: Transaction Throughput
func BenchmarkTransactionThroughput(b *testing.B) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		b.Fatal(err)
	}
	defer nest.Close()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tx, err := nest.Begin()
		if err != nil {
			b.Fatal(err)
		}

		id := fmt.Sprintf("vec%d", i)
		err = tx.Store(id, []float32{float32(i), float32(i), float32(i)})
		if err != nil {
			b.Fatal(err)
		}

		if err := tx.Commit(); err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark 2: Conflict Detection
func BenchmarkConflictDetection(b *testing.B) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		b.Fatal(err)
	}
	defer nest.Close()

	// Pre-populate
	err = nest.Store("hotspot", []float32{0, 0, 0})
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tx1, _ := nest.Begin()
		tx2, _ := nest.Begin()

		tx1.Store("hotspot", []float32{1, 1, 1})
		tx2.Store("hotspot", []float32{2, 2, 2})

		tx1.Commit()
		tx2.Commit() // Should detect conflict
	}
}

// Benchmark 3: Parallel Transactions
func BenchmarkParallelTransactions(b *testing.B) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile)
	if err != nil {
		b.Fatal(err)
	}
	defer nest.Close()

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			tx, err := nest.Begin()
			if err != nil {
				continue
			}

			id := fmt.Sprintf("vec%d_%d", b.N, i)
			tx.Store(id, []float32{float32(i)})
			tx.Commit()
			i++
		}
	})
}
