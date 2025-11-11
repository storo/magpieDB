package magpie

import (
	"os"
	"sync"
	"testing"
	"time"
)

// TestMVCCNoPhantomReads tests that phantom reads don't occur with snapshot isolation
func TestMVCCNoPhantomReads(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Initial data
	nest.Store("vec1", []float32{1, 1})
	nest.Store("vec2", []float32{2, 2})

	tx, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// First scan
	count1 := 0
	if tx.Has("vec1") {
		count1++
	}
	if tx.Has("vec2") {
		count1++
	}
	if tx.Has("vec3") {
		count1++
	}

	// Concurrent insert (in different transaction)
	tx2, _ := nest.Begin()
	tx2.Store("vec3", []float32{3, 3})
	tx2.Commit()

	// Second scan - should see same count (no phantom)
	count2 := 0
	if tx.Has("vec1") {
		count2++
	}
	if tx.Has("vec2") {
		count2++
	}
	if tx.Has("vec3") {
		count2++
	}

	if count1 != count2 {
		t.Errorf("Phantom read detected: first scan=%d, second scan=%d", count1, count2)
	}

	tx.Commit()

	// After commit, new transaction should see vec3
	if !nest.Has("vec3") {
		t.Error("vec3 not visible after concurrent insert committed")
	}
}

// TestMVCCNoLostUpdate tests prevention of lost updates
func TestMVCCNoLostUpdate(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	nest.Store("counter", []float32{0, 0})

	tx1, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	tx2, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// Both read same value
	v1, err := tx1.Get("counter")
	if err != nil {
		t.Fatal(err)
	}

	v2, err := tx2.Get("counter")
	if err != nil {
		t.Fatal(err)
	}

	// Both increment (read-modify-write)
	tx1.Store("counter", []float32{v1.Vector[0] + 1, 0})
	tx2.Store("counter", []float32{v2.Vector[0] + 1, 0})

	// First commits
	if err := tx1.Commit(); err != nil {
		t.Fatal("First commit should succeed:", err)
	}

	// Second should conflict (preventing lost update)
	if err := tx2.Commit(); err == nil {
		t.Error("Expected lost update to be prevented via conflict detection")
	} else {
		t.Logf("Lost update prevented: %v", err)
	}

	// Final value should be 1 (only tx1's increment)
	treasure, _ := nest.Get("counter")
	if treasure.Vector[0] != 1 {
		t.Errorf("Expected counter=1, got %f", treasure.Vector[0])
	}
}

// TestMVCCWriteSkew tests write skew anomaly detection
func TestMVCCWriteSkew(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Initial state: both x and y are 1
	nest.Store("x", []float32{1, 0})
	nest.Store("y", []float32{1, 0})

	tx1, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	tx2, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	// tx1 reads x, writes y
	x1, _ := tx1.Get("x")
	tx1.Store("y", []float32{x1.Vector[0] + 1, 0})

	// tx2 reads y, writes x
	y2, _ := tx2.Get("y")
	tx2.Store("x", []float32{y2.Vector[0] + 1, 0})

	// Both commit (write skew possible with SI, but should be handled)
	err1 := tx1.Commit()
	err2 := tx2.Commit()

	t.Logf("Tx1 commit: %v, Tx2 commit: %v", err1, err2)

	// At least one should succeed
	if err1 != nil && err2 != nil {
		t.Error("Both transactions failed - deadlock or over-conservative locking")
	}

	// Note: Pure snapshot isolation allows write skew. This test documents
	// the behavior. Serializable isolation would require predicate locking.
}

// TestMVCCEmptyTransaction tests committing an empty transaction
func TestMVCCEmptyTransaction(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Begin and immediately commit
	tx, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	if err := tx.Commit(); err != nil {
		t.Error("Empty transaction commit failed:", err)
	}

	// Should be idempotent
	if nest.Count() != 0 {
		t.Errorf("Expected 0 vectors, got %d", nest.Count())
	}
}

// TestMVCCDoubleCommit tests committing a transaction twice
func TestMVCCDoubleCommit(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	tx, _ := nest.Begin()
	tx.Store("vec1", []float32{1, 1})

	// First commit
	if err := tx.Commit(); err != nil {
		t.Fatal("First commit failed:", err)
	}

	// Second commit should fail
	if err := tx.Commit(); err == nil {
		t.Error("Expected error on double commit")
	} else {
		t.Logf("Double commit prevented: %v", err)
	}
}

// TestMVCCCommitAfterRollback tests committing after rollback
func TestMVCCCommitAfterRollback(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	tx, _ := nest.Begin()
	tx.Store("vec1", []float32{1, 1})

	// Rollback
	if err := tx.Rollback(); err != nil {
		t.Fatal("Rollback failed:", err)
	}

	// Commit should fail
	if err := tx.Commit(); err == nil {
		t.Error("Expected error when committing after rollback")
	} else {
		t.Logf("Commit after rollback prevented: %v", err)
	}

	// Vector should not exist
	if nest.Has("vec1") {
		t.Error("Vector exists after rollback")
	}
}

// TestMVCCReadOnlyTransaction tests read-only transactions
func TestMVCCReadOnlyTransaction(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Setup data
	nest.Store("vec1", []float32{1, 1})
	nest.Store("vec2", []float32{2, 2})

	// Read-only transaction
	tx, _ := nest.Begin()

	v1, err := tx.Get("vec1")
	if err != nil {
		t.Fatal(err)
	}

	v2, err := tx.Get("vec2")
	if err != nil {
		t.Fatal(err)
	}

	if v1.Vector[0] != 1 || v2.Vector[0] != 2 {
		t.Error("Read-only transaction returned wrong values")
	}

	// Commit should succeed (no writes)
	if err := tx.Commit(); err != nil {
		t.Error("Read-only transaction commit failed:", err)
	}
}

// TestMVCCVersionOverflow tests behavior with extreme version numbers
func TestMVCCVersionOverflow(t *testing.T) {
	// This is a design test - documents expected behavior
	t.Skip("Version overflow handling not yet implemented")

	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// In a real implementation, we'd need to test:
	// 1. What happens when version counter approaches uint64 max?
	// 2. Does the system gracefully handle wraparound?
	// 3. Are there safeguards against version collision?
}

// TestMVCCConcurrentGC tests garbage collection with active transactions
func TestMVCCConcurrentGC(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Create initial version
	nest.Store("vec1", []float32{0, 0})

	// Start long transaction
	longTx, _ := nest.Begin()
	longTx.Get("vec1")

	// Create many versions
	for i := 1; i <= 50; i++ {
		tx, _ := nest.Begin()
		tx.Store("vec1", []float32{float32(i), 0})
		tx.Commit()
	}

	// Trigger GC (implementation-specific)
	// This is a placeholder for when GC is implemented
	t.Log("GC would run here in full implementation")

	// Long transaction should still work
	v, err := longTx.Get("vec1")
	if err != nil {
		t.Error("Long transaction broken by GC:", err)
	}

	if v.Vector[0] != 0 {
		t.Errorf("GC removed visible version: expected 0, got %f", v.Vector[0])
	}

	longTx.Commit()
}

// TestMVCCRecoveryCorruption tests recovery from corrupted MVCC data
func TestMVCCRecoveryCorruption(t *testing.T) {
	t.Skip("Corruption recovery test requires full MVCC implementation")

	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	// Test scenario:
	// 1. Create database with MVCC data
	// 2. Corrupt version chain
	// 3. Attempt recovery
	// 4. Verify graceful degradation or error
}

// TestMVCCTransactionTimeout tests transaction timeout behavior
func TestMVCCTransactionTimeout(t *testing.T) {
	t.Skip("Transaction timeout not yet implemented")

	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	tx, _ := nest.Begin()

	// Hold transaction open for extended period
	time.Sleep(5 * time.Second)

	// Should timeout or succeed depending on policy
	err = tx.Commit()
	t.Logf("Long-running transaction result: %v", err)
}

// TestMVCCDeadlockDetection tests deadlock detection and resolution
func TestMVCCDeadlockDetection(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Setup
	nest.Store("x", []float32{1, 0})
	nest.Store("y", []float32{1, 0})

	var wg sync.WaitGroup
	results := make(chan error, 2)

	// Transaction 1: x then y
	wg.Add(1)
	go func() {
		defer wg.Done()

		tx, _ := nest.Begin()
		tx.Store("x", []float32{2, 0})

		time.Sleep(100 * time.Millisecond)

		tx.Store("y", []float32{2, 0})
		results <- tx.Commit()
	}()

	// Transaction 2: y then x
	wg.Add(1)
	go func() {
		defer wg.Done()

		tx, _ := nest.Begin()
		tx.Store("y", []float32{3, 0})

		time.Sleep(100 * time.Millisecond)

		tx.Store("x", []float32{3, 0})
		results <- tx.Commit()
	}()

	wg.Wait()
	close(results)

	// At least one should succeed (MVCC doesn't have deadlocks with OCC)
	successCount := 0
	for err := range results {
		if err == nil {
			successCount++
		}
	}

	if successCount == 0 {
		t.Error("Both transactions failed - possible deadlock")
	}

	t.Logf("%d transaction(s) succeeded", successCount)
}

// TestMVCCReadYourWrites tests read-your-writes consistency
func TestMVCCReadYourWrites(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	tx, _ := nest.Begin()

	// Write
	tx.Store("vec1", []float32{1, 2})

	// Read back immediately
	treasure, err := tx.Get("vec1")
	if err != nil {
		t.Fatal("Failed to read own write:", err)
	}

	if treasure.Vector[0] != 1 {
		t.Errorf("Read-your-writes violated: expected 1, got %f", treasure.Vector[0])
	}

	tx.Commit()
}

// TestMVCCMonotonicReads tests monotonic read consistency
func TestMVCCMonotonicReads(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	nest.Store("vec1", []float32{1, 0})

	tx, _ := nest.Begin()

	// First read
	v1, _ := tx.Get("vec1")

	// Concurrent update
	tx2, _ := nest.Begin()
	tx2.Store("vec1", []float32{100, 0})
	tx2.Commit()

	// Second read should see same value (monotonic reads)
	v2, _ := tx.Get("vec1")

	if v1.Vector[0] != v2.Vector[0] {
		t.Errorf("Monotonic reads violated: %f -> %f", v1.Vector[0], v2.Vector[0])
	}

	tx.Commit()
}

// TestMVCCWriteVisibilityToOthers tests write visibility to concurrent transactions
func TestMVCCWriteVisibilityToOthers(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	nest.Store("vec1", []float32{1, 0})

	tx1, _ := nest.Begin()
	tx2, _ := nest.Begin()

	// tx1 updates
	tx1.Store("vec1", []float32{999, 0})

	// tx2 should NOT see uncommitted write
	v, _ := tx2.Get("vec1")
	if v.Vector[0] != 1 {
		t.Errorf("Read uncommitted data: expected 1, got %f", v.Vector[0])
	}

	// After tx1 commits, tx2 still shouldn't see it (snapshot isolation)
	tx1.Commit()

	v, _ = tx2.Get("vec1")
	if v.Vector[0] != 1 {
		t.Errorf("Snapshot isolation violated: expected 1, got %f", v.Vector[0])
	}

	tx2.Commit()

	// New transaction should see committed write
	tx3, _ := nest.Begin()
	v, _ = tx3.Get("vec1")
	if v.Vector[0] != 999 {
		t.Errorf("Committed write not visible: expected 999, got %f", v.Vector[0])
	}
	tx3.Commit()
}

// TestMVCCRollbackVisibility tests that rolled back changes are never visible
func TestMVCCRollbackVisibility(t *testing.T) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	nest.Store("vec1", []float32{1, 0})

	var wg sync.WaitGroup

	// Transaction that will rollback
	wg.Add(1)
	go func() {
		defer wg.Done()

		tx, _ := nest.Begin()
		tx.Store("vec1", []float32{999, 0})
		time.Sleep(50 * time.Millisecond)
		tx.Rollback()
	}()

	// Concurrent reader
	wg.Add(1)
	go func() {
		defer wg.Done()

		time.Sleep(25 * time.Millisecond)

		tx, _ := nest.Begin()
		v, _ := tx.Get("vec1")

		if v.Vector[0] != 1 {
			t.Errorf("Saw uncommitted data: %f", v.Vector[0])
		}

		tx.Commit()
	}()

	wg.Wait()

	// Final state should be original
	treasure, _ := nest.Get("vec1")
	if treasure.Vector[0] != 1 {
		t.Errorf("Rollback not effective: got %f", treasure.Vector[0])
	}
}
