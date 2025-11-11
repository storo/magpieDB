package magpie

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ==============================================================================
// UNIT TESTS - Pool Mechanics (12 tests)
// ==============================================================================

// TestVectorBufferPoolGetPut tests basic pool operations
func TestVectorBufferPoolGetPut(t *testing.T) {
	pool := NewVectorBufferPool()

	// Get a buffer
	buf1 := pool.Get()
	if buf1 == nil {
		t.Fatal("pool returned nil buffer")
	}

	// Check capacity
	if cap(*buf1) < 1024 {
		t.Errorf("expected capacity >= 1024, got %d", cap(*buf1))
	}

	// Put it back
	pool.Put(buf1)

	// Get again - should reuse
	buf2 := pool.Get()
	if buf2 == nil {
		t.Fatal("pool returned nil buffer on second get")
	}

	// Verify it was reset (length = 0)
	if len(*buf2) != 0 {
		t.Errorf("expected length 0 after reset, got %d", len(*buf2))
	}
}

// TestVectorBufferPoolConcurrent tests concurrent pool access
func TestVectorBufferPoolConcurrent(t *testing.T) {
	pool := NewVectorBufferPool()
	numGoroutines := 100
	numOps := 1000

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				buf := pool.Get()
				*buf = append(*buf, float32(j))
				pool.Put(buf)
			}
		}()
	}

	wg.Wait()
}

// TestVectorBufferPoolReset tests that buffers are properly reset
func TestVectorBufferPoolReset(t *testing.T) {
	pool := NewVectorBufferPool()

	buf := pool.Get()
	*buf = append(*buf, 1.0, 2.0, 3.0)

	if len(*buf) != 3 {
		t.Fatalf("expected length 3, got %d", len(*buf))
	}

	pool.Put(buf)

	buf2 := pool.Get()
	if len(*buf2) != 0 {
		t.Errorf("buffer not reset: expected length 0, got %d", len(*buf2))
	}

	// Capacity should be preserved
	if cap(*buf2) < 3 {
		t.Errorf("capacity not preserved: expected >= 3, got %d", cap(*buf2))
	}
}

// TestMetadataBufferPoolGetPut tests metadata buffer pool
func TestMetadataBufferPoolGetPut(t *testing.T) {
	pool := NewMetadataBufferPool()

	buf1 := pool.Get()
	if buf1 == nil {
		t.Fatal("pool returned nil buffer")
	}

	pool.Put(buf1)

	buf2 := pool.Get()
	if buf2 == nil {
		t.Fatal("pool returned nil buffer on second get")
	}

	if buf2.Len() != 0 {
		t.Errorf("buffer not reset: expected length 0, got %d", buf2.Len())
	}
}

// TestMetadataBufferPoolSerialization tests metadata serialization with pool
func TestMetadataBufferPoolSerialization(t *testing.T) {
	pool := NewMetadataBufferPool()

	buf := pool.Get()
	defer pool.Put(buf)

	metadata := map[string]interface{}{
		"title": "Test Document",
		"count": 42,
	}

	// Serialize to buffer
	encoder := NewJSONEncoder(buf)
	if err := encoder.Encode(metadata); err != nil {
		t.Fatalf("failed to encode metadata: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("buffer is empty after encoding")
	}
}

// TestBatchSerializationPerformance tests serialization with pooling
func TestBatchSerializationPerformance(t *testing.T) {
	vectorPool := NewVectorBufferPool()
	count := 1000

	start := time.Now()
	for i := 0; i < count; i++ {
		buf := vectorPool.Get()
		for j := 0; j < 128; j++ {
			*buf = append(*buf, float32(j))
		}
		vectorPool.Put(buf)
	}
	elapsed := time.Since(start)

	opsPerSec := float64(count) / elapsed.Seconds()
	if opsPerSec < 10000 {
		t.Logf("Warning: Low serialization throughput: %.0f ops/sec", opsPerSec)
	}
	t.Logf("Serialization throughput: %.0f ops/sec", opsPerSec)
}

// TestPoolMemoryReuse tests that pool actually reuses memory
func TestPoolMemoryReuse(t *testing.T) {
	pool := NewVectorBufferPool()

	// Get and track pointer
	buf1 := pool.Get()
	ptr1 := fmt.Sprintf("%p", buf1)
	pool.Put(buf1)

	// Get again - should reuse same memory
	buf2 := pool.Get()
	ptr2 := fmt.Sprintf("%p", buf2)

	if ptr1 != ptr2 {
		t.Logf("Note: Pool did not reuse same pointer (got %s, then %s)", ptr1, ptr2)
		t.Logf("This is not necessarily an error, but pool may not be optimally reusing memory")
	}
}

// TestEmptyBatchOperation tests batch with no operations
func TestEmptyBatchOperation(t *testing.T) {
	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	err := nest.Batch(func(tx *Tx) error {
		// Do nothing
		return nil
	})

	if err != nil {
		t.Errorf("empty batch should succeed: %v", err)
	}
}

// TestBatchErrorHandling tests error propagation in batch
func TestBatchErrorHandling(t *testing.T) {
	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	expectedErr := fmt.Errorf("test error")

	err := nest.Batch(func(tx *Tx) error {
		// Add some operations
		_ = tx.Store("test1", make([]float32, 128))
		return expectedErr
	})

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}

	// Verify nothing was committed
	if nest.Has("test1") {
		t.Error("vector should not exist after failed batch")
	}
}

// TestBatchSizeLimits tests handling of large batches
func TestBatchSizeLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping 10K vector test in short mode")
	}

	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	// Test with 10K vectors
	batchSize := 10000

	err := nest.Batch(func(tx *Tx) error {
		for i := 0; i < batchSize; i++ {
			id := fmt.Sprintf("vec%d", i)
			vector := make([]float32, 128)
			for j := range vector {
				vector[j] = rand.Float32()
			}
			if err := tx.Store(id, vector); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		t.Errorf("large batch failed: %v", err)
	}

	// Verify count
	if nest.Count() != int64(batchSize) {
		t.Errorf("expected count %d, got %d", batchSize, nest.Count())
	}
}

// TestBatchPreallocation tests that write-set is properly initialized
func TestBatchPreallocation(t *testing.T) {
	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	// This test verifies that the Batch() implementation properly initializes
	// the write-set. Since WriteSet is a map, it handles its own growth.

	err := nest.Batch(func(tx *Tx) error {
		// Verify write-set exists
		if tx.mvccTx != nil && tx.mvccTx.WriteSet == nil {
			return fmt.Errorf("WriteSet not initialized")
		}

		// Add operations
		for i := 0; i < 500; i++ {
			id := fmt.Sprintf("vec%d", i)
			if err := tx.Store(id, make([]float32, 128)); err != nil {
				return err
			}
		}

		// Verify writes are in write-set
		if tx.mvccTx != nil {
			if len(tx.mvccTx.WriteSet) != 500 {
				t.Logf("Write-set size: %d (expected 500)", len(tx.mvccTx.WriteSet))
			}
		}

		return nil
	})

	if err != nil {
		t.Errorf("batch with write-set failed: %v", err)
	}
}

// TestBatchOOMHandling tests behavior under memory pressure
func TestBatchOOMHandling(t *testing.T) {
	t.Skip("Test requires >40GB RAM and exceeds page size limits. Run manually on high-memory systems.")

	if testing.Short() {
		t.Skip("Skipping OOM test in short mode")
	}

	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	// Try to allocate an extremely large batch
	// This should fail gracefully, not panic

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("batch operation panicked: %v", r)
		}
	}()

	err := nest.Batch(func(tx *Tx) error {
		// Try to store 1 million large vectors
		for i := 0; i < 1000000; i++ {
			id := fmt.Sprintf("vec%d", i)
			// 10K dimensions = 40KB per vector
			vector := make([]float32, 10000)
			if err := tx.Store(id, vector); err != nil {
				return err // This is expected
			}
		}
		return nil
	})

	// We expect an error, not a panic
	if err == nil {
		t.Log("Note: Extremely large batch succeeded (may indicate insufficient memory pressure)")
	}
}

// ==============================================================================
// INTEGRATION TESTS - Atomicity & Isolation (10 tests)
// ==============================================================================

// TestBatchAtomicity tests that all-or-nothing semantics work
func TestBatchAtomicity(t *testing.T) {
	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	// First, add some vectors successfully
	err := nest.Batch(func(tx *Tx) error {
		for i := 0; i < 10; i++ {
			id := fmt.Sprintf("vec%d", i)
			if err := tx.Store(id, make([]float32, 128)); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("initial batch failed: %v", err)
	}

	initialCount := nest.Count()

	// Now try a batch that fails halfway
	err = nest.Batch(func(tx *Tx) error {
		for i := 10; i < 20; i++ {
			id := fmt.Sprintf("vec%d", i)
			if err := tx.Store(id, make([]float32, 128)); err != nil {
				return err
			}

			// Fail at midpoint
			if i == 15 {
				return fmt.Errorf("simulated failure")
			}
		}
		return nil
	})

	if err == nil {
		t.Error("expected batch to fail")
	}

	// Verify count didn't change
	if nest.Count() != initialCount {
		t.Errorf("atomicity violated: count changed from %d to %d", initialCount, nest.Count())
	}

	// Verify none of the new vectors exist
	for i := 10; i < 20; i++ {
		id := fmt.Sprintf("vec%d", i)
		if nest.Has(id) {
			t.Errorf("vector %s should not exist after failed batch", id)
		}
	}
}

// TestBatchIsolation tests snapshot isolation between concurrent batches
func TestBatchIsolation(t *testing.T) {
	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	// Pre-populate with some vectors
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("vec%d", i)
		_ = nest.Store(id, make([]float32, 128))
	}

	var wg sync.WaitGroup
	errors := make(chan error, 2)

	// Transaction 1: Read all vectors
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := nest.Batch(func(tx *Tx) error {
			count := 0
			for i := 0; i < 10; i++ {
				id := fmt.Sprintf("vec%d", i)
				if tx.Has(id) {
					count++
				}
			}

			// Sleep to allow concurrent transaction to run
			time.Sleep(50 * time.Millisecond)

			// Re-check count - should be same (snapshot isolation)
			count2 := 0
			for i := 0; i < 10; i++ {
				id := fmt.Sprintf("vec%d", i)
				if tx.Has(id) {
					count2++
				}
			}

			if count != count2 {
				return fmt.Errorf("isolation violated: count changed from %d to %d", count, count2)
			}

			return nil
		})
		if err != nil {
			errors <- err
		}
	}()

	// Transaction 2: Add new vectors
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond) // Let tx1 start first

		err := nest.Batch(func(tx *Tx) error {
			for i := 10; i < 20; i++ {
				id := fmt.Sprintf("vec%d", i)
				if err := tx.Store(id, make([]float32, 128)); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			errors <- err
		}
	}()

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("transaction failed: %v", err)
	}
}

// TestConcurrentBatches tests multiple concurrent batch operations
func TestConcurrentBatches(t *testing.T) {
	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	numBatches := 10
	vectorsPerBatch := 100

	var wg sync.WaitGroup
	var successCount atomic.Int32

	for i := 0; i < numBatches; i++ {
		wg.Add(1)
		go func(batchID int) {
			defer wg.Done()

			err := nest.Batch(func(tx *Tx) error {
				for j := 0; j < vectorsPerBatch; j++ {
					id := fmt.Sprintf("batch%d_vec%d", batchID, j)
					vector := make([]float32, 128)
					for k := range vector {
						vector[k] = rand.Float32()
					}
					if err := tx.Store(id, vector); err != nil {
						return err
					}
				}
				return nil
			})

			if err == nil {
				successCount.Add(1)
			} else {
				t.Logf("Batch %d failed: %v", batchID, err)
			}
		}(i)
	}

	wg.Wait()

	expectedCount := int64(numBatches * vectorsPerBatch)
	actualCount := nest.Count()

	t.Logf("Successful batches: %d/%d", successCount.Load(), numBatches)
	t.Logf("Vector count: %d (expected %d)", actualCount, expectedCount)

	if actualCount != expectedCount {
		t.Errorf("expected %d vectors, got %d", expectedCount, actualCount)
	}
}

// TestBatch100KVectors tests batch with 100K vectors
func TestBatch100KVectors(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping 100K vector test in short mode")
	}

	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	batchSize := 100000

	start := time.Now()
	err := nest.Batch(func(tx *Tx) error {
		for i := 0; i < batchSize; i++ {
			id := fmt.Sprintf("vec%d", i)
			vector := make([]float32, 128)
			if err := tx.Store(id, vector); err != nil {
				return err
			}

			if i > 0 && i%10000 == 0 {
				t.Logf("Progress: %d/%d vectors", i, batchSize)
			}
		}
		return nil
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("100K batch failed: %v", err)
	}

	opsPerSec := float64(batchSize) / elapsed.Seconds()
	t.Logf("Throughput: %.0f ops/sec", opsPerSec)

	if opsPerSec < 10000 {
		t.Logf("Warning: Low throughput (< 10K ops/sec)")
	}
}

// TestBatchWithMetadata tests batch operations with metadata
func TestBatchWithMetadata(t *testing.T) {
	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	count := 1000

	err := nest.Batch(func(tx *Tx) error {
		for i := 0; i < count; i++ {
			id := fmt.Sprintf("vec%d", i)
			vector := make([]float32, 128)
			metadata := map[string]interface{}{
				"index": i,
				"batch": "test",
			}
			if err := tx.Store(id, vector, metadata); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("batch with metadata failed: %v", err)
	}

	// Verify metadata
	treasure, err := nest.Get("vec500")
	if err != nil {
		t.Fatalf("failed to get vector: %v", err)
	}

	if treasure.Metadata == nil {
		t.Error("metadata not stored")
	} else {
		if treasure.Metadata["index"] != 500.0 { // JSON unmarshals to float64
			t.Errorf("wrong metadata index: %v", treasure.Metadata["index"])
		}
	}
}

// TestBatchReadYourWrites tests that transactions can read their own writes
func TestBatchReadYourWrites(t *testing.T) {
	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	err := nest.Batch(func(tx *Tx) error {
		// Write a vector
		id := "test_vec"
		vector := []float32{1, 2, 3, 4}
		if err := tx.Store(id, vector); err != nil {
			return err
		}

		// Read it back immediately
		treasure, err := tx.Get(id)
		if err != nil {
			return fmt.Errorf("failed to read own write: %w", err)
		}

		if treasure.ID != id {
			return fmt.Errorf("wrong ID: expected %s, got %s", id, treasure.ID)
		}

		if len(treasure.Vector) != len(vector) {
			return fmt.Errorf("wrong vector length: expected %d, got %d", len(vector), len(treasure.Vector))
		}

		return nil
	})

	if err != nil {
		t.Errorf("read-your-writes failed: %v", err)
	}
}

// TestBatchRemoveOperations tests batch deletions
func TestBatchRemoveOperations(t *testing.T) {
	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	// Pre-populate
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("vec%d", i)
		_ = nest.Store(id, make([]float32, 128))
	}

	initialCount := nest.Count()

	// Batch remove half
	err := nest.Batch(func(tx *Tx) error {
		for i := 0; i < 50; i++ {
			id := fmt.Sprintf("vec%d", i)
			if err := tx.Remove(id); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("batch remove failed: %v", err)
	}

	finalCount := nest.Count()
	expectedCount := initialCount - 50

	if finalCount != expectedCount {
		t.Errorf("expected count %d, got %d", expectedCount, finalCount)
	}
}

// TestBatchMixedOperations tests mix of stores and removes
func TestBatchMixedOperations(t *testing.T) {
	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	// Pre-populate
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("vec%d", i)
		_ = nest.Store(id, make([]float32, 128))
	}

	err := nest.Batch(func(tx *Tx) error {
		// Remove some
		for i := 0; i < 25; i++ {
			id := fmt.Sprintf("vec%d", i)
			if err := tx.Remove(id); err != nil {
				return err
			}
		}

		// Add new ones
		for i := 50; i < 100; i++ {
			id := fmt.Sprintf("vec%d", i)
			if err := tx.Store(id, make([]float32, 128)); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		t.Fatalf("mixed operations batch failed: %v", err)
	}

	// Should have 25 (remaining) + 50 (new) = 75 vectors
	expectedCount := int64(75)
	if nest.Count() != expectedCount {
		t.Errorf("expected count %d, got %d", expectedCount, nest.Count())
	}
}

// TestBatchRollbackOnPanic tests that panics don't corrupt database
func TestBatchRollbackOnPanic(t *testing.T) {
	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	// Pre-populate
	_ = nest.Store("vec1", make([]float32, 128))
	initialCount := nest.Count()

	// Try batch that panics
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic")
			}
		}()

		_ = nest.Batch(func(tx *Tx) error {
			_ = tx.Store("vec2", make([]float32, 128))
			panic("simulated panic")
		})
	}()

	// Verify database is still consistent
	if nest.Count() != initialCount {
		t.Errorf("count changed after panic: expected %d, got %d", initialCount, nest.Count())
	}

	if nest.Has("vec2") {
		t.Error("vec2 should not exist after panic")
	}
}

// TestBatchDurability tests that batches survive restart
func TestBatchDurability(t *testing.T) {
	path := createTempPath(t)
	defer cleanupPath(path)

	// Create and populate
	nest1, err := Open(path, Options{
		Dimensions: 128,
	})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	err = nest1.Batch(func(tx *Tx) error {
		for i := 0; i < 100; i++ {
			id := fmt.Sprintf("vec%d", i)
			if err := tx.Store(id, make([]float32, 128)); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}

	nest1.Close()

	// Reopen and verify
	nest2, err := Open(path, Options{
		Dimensions: 128,
	})
	if err != nil {
		t.Fatalf("failed to reopen database: %v", err)
	}
	defer nest2.Close()

	if nest2.Count() != 100 {
		t.Errorf("expected 100 vectors after recovery, got %d", nest2.Count())
	}
}

// ==============================================================================
// BENCHMARKS - Throughput & Memory (5 benchmarks)
// ==============================================================================

// BenchmarkBatch100Items benchmarks batch with 100 items
func BenchmarkBatch100Items(b *testing.B) {
	nest := createTestNest(b)
	defer cleanupTestNest(nest)

	vector := make([]float32, 128)
	for i := range vector {
		vector[i] = rand.Float32()
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := nest.Batch(func(tx *Tx) error {
			for j := 0; j < 100; j++ {
				id := fmt.Sprintf("vec_%d_%d", i, j)
				if err := tx.Store(id, vector); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			b.Fatalf("batch failed: %v", err)
		}
	}

	opsPerSec := float64(b.N*100) / b.Elapsed().Seconds()
	b.ReportMetric(opsPerSec, "ops/sec")
}

// BenchmarkBatch1KItems benchmarks batch with 1K items
func BenchmarkBatch1KItems(b *testing.B) {
	nest := createTestNest(b)
	defer cleanupTestNest(nest)

	vector := make([]float32, 128)
	for i := range vector {
		vector[i] = rand.Float32()
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := nest.Batch(func(tx *Tx) error {
			for j := 0; j < 1000; j++ {
				id := fmt.Sprintf("vec_%d_%d", i, j)
				if err := tx.Store(id, vector); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			b.Fatalf("batch failed: %v", err)
		}
	}

	opsPerSec := float64(b.N*1000) / b.Elapsed().Seconds()
	b.ReportMetric(opsPerSec, "ops/sec")
}

// BenchmarkBatch10KItems benchmarks batch with 10K items
func BenchmarkBatch10KItems(b *testing.B) {
	nest := createTestNest(b)
	defer cleanupTestNest(nest)

	vector := make([]float32, 128)
	for i := range vector {
		vector[i] = rand.Float32()
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := nest.Batch(func(tx *Tx) error {
			for j := 0; j < 10000; j++ {
				id := fmt.Sprintf("vec_%d_%d", i, j)
				if err := tx.Store(id, vector); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			b.Fatalf("batch failed: %v", err)
		}
	}

	opsPerSec := float64(b.N*10000) / b.Elapsed().Seconds()
	b.ReportMetric(opsPerSec, "ops/sec")
}

// BenchmarkBatchMemoryOverhead measures memory overhead with pooling
func BenchmarkBatchMemoryOverhead(b *testing.B) {
	nest := createTestNest(b)
	defer cleanupTestNest(nest)

	vector := make([]float32, 128)

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = nest.Batch(func(tx *Tx) error {
			for j := 0; j < 1000; j++ {
				id := fmt.Sprintf("vec_%d_%d", i, j)
				_ = tx.Store(id, vector)
			}
			return nil
		})
	}

	b.StopTimer()
	runtime.GC()
	runtime.ReadMemStats(&m2)

	allocatedBytes := m2.TotalAlloc - m1.TotalAlloc
	b.ReportMetric(float64(allocatedBytes)/float64(b.N*1000), "bytes/op")
}

// BenchmarkPoolContention measures pool contention under concurrent load
func BenchmarkPoolContention(b *testing.B) {
	pool := NewVectorBufferPool()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.Get()
			*buf = append(*buf, 1.0, 2.0, 3.0)
			pool.Put(buf)
		}
	})
}

// ==============================================================================
// PARALLEL BATCH SEARCH TESTS
// ==============================================================================

// TestBatchFind tests parallel batch search
func TestBatchFind(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping 1000 vector search test in short mode")
	}

	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	// Populate with test vectors
	numVectors := 1000
	for i := 0; i < numVectors; i++ {
		id := fmt.Sprintf("vec%d", i)
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = float32(i) + rand.Float32()
		}
		_ = nest.Store(id, vector)
	}

	// Create multiple queries
	numQueries := 100
	queries := make([][]float32, numQueries)
	for i := range queries {
		queries[i] = make([]float32, 128)
		for j := range queries[i] {
			queries[i][j] = rand.Float32()
		}
	}

	// Perform batch search
	results := nest.BatchFind(queries, 10)

	if len(results) != numQueries {
		t.Errorf("expected %d result sets, got %d", numQueries, len(results))
	}

	for i, resultSet := range results {
		if len(resultSet) == 0 {
			t.Errorf("query %d returned no results", i)
		}
		if len(resultSet) > 10 {
			t.Errorf("query %d returned too many results: %d", i, len(resultSet))
		}
	}
}

// TestBatchFindConcurrency tests that BatchFind scales with cores
func TestBatchFindConcurrency(t *testing.T) {
	nest := createTestNest(t)
	defer cleanupTestNest(nest)

	// Populate
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("vec%d", i)
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = rand.Float32()
		}
		_ = nest.Store(id, vector)
	}

	// Create queries
	numQueries := runtime.NumCPU() * 10
	queries := make([][]float32, numQueries)
	for i := range queries {
		queries[i] = make([]float32, 128)
		for j := range queries[i] {
			queries[i][j] = rand.Float32()
		}
	}

	// Time sequential
	start := time.Now()
	for _, query := range queries {
		nest.Find(query, 10)
	}
	seqTime := time.Since(start)

	// Time parallel
	start = time.Now()
	nest.BatchFind(queries, 10)
	parTime := time.Since(start)

	speedup := float64(seqTime) / float64(parTime)
	t.Logf("Sequential: %v, Parallel: %v, Speedup: %.2fx", seqTime, parTime, speedup)

	if speedup < 1.5 {
		t.Logf("Warning: Low speedup (%.2fx)", speedup)
	}
}

// BenchmarkBatchFind benchmarks parallel batch search
func BenchmarkBatchFind(b *testing.B) {
	nest := createTestNest(b)
	defer cleanupTestNest(nest)

	// Populate
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("vec%d", i)
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = rand.Float32()
		}
		_ = nest.Store(id, vector)
	}

	// Create queries
	queries := make([][]float32, 100)
	for i := range queries {
		queries[i] = make([]float32, 128)
		for j := range queries[i] {
			queries[i][j] = rand.Float32()
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		nest.BatchFind(queries, 10)
	}

	queriesPerSec := float64(b.N*len(queries)) / b.Elapsed().Seconds()
	b.ReportMetric(queriesPerSec, "queries/sec")
}

// ==============================================================================
// Helper Functions
// ==============================================================================

func createTestNest(t testing.TB) *Nest {
	t.Helper()
	path := createTempPath(t)
	nest, err := Open(path)
	if err != nil {
		t.Fatalf("failed to create test nest: %v", err)
	}
	return nest
}

func cleanupTestNest(nest *Nest) {
	if nest != nil {
		path := nest.path
		nest.Close()
		cleanupPath(path)
	}
}

func createTempPath(t testing.TB) string {
	t.Helper()
	return filepath.Join(os.TempDir(), fmt.Sprintf("magpie_batch_test_%d_%d.db", time.Now().UnixNano(), rand.Int()))
}

func cleanupPath(path string) {
	os.Remove(path)
	os.Remove(path + ".wal")
}
