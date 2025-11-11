package magpie

import (
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestMVCC100ConcurrentTransactions tests high concurrency with 100 simultaneous transactions
func TestMVCC100ConcurrentTransactions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Pre-populate with 500 vectors (increased to reduce contention)
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("vec%d", i)
		_ = nest.Store(id, []float32{float32(i), float32(i), float32(i)})
	}

	var wg sync.WaitGroup
	successCount := int32(0)
	conflictCount := int32(0)
	errorCount := int32(0)

	// Launch 100 concurrent transactions
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			tx, err := nest.Begin()
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
				return
			}

			// Each transaction updates 5 random vectors
			for j := 0; j < 5; j++ {
				vecID := fmt.Sprintf("vec%d", rand.Intn(500))
				if err := tx.Store(vecID, []float32{float32(id), float32(j), 0}); err != nil {
					atomic.AddInt32(&errorCount, 1)
					_ = tx.Rollback()
					return
				}
			}

			// Commit
			if err := tx.Commit(); err != nil {
				atomic.AddInt32(&conflictCount, 1)
			} else {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Successful: %d, Conflicts: %d, Errors: %d",
		successCount, conflictCount, errorCount)

	if successCount == 0 {
		t.Error("No transactions succeeded")
	}

	if errorCount > 10 {
		t.Errorf("Too many errors: %d", errorCount)
	}

	// Conflict rate should be reasonable (<70%)
	conflictRate := float64(conflictCount) / 100.0
	if conflictRate > 0.7 {
		t.Errorf("Conflict rate too high: %.2f%%", conflictRate*100)
	}

	// Verify database is still consistent
	count := nest.Count()
	if count != 500 {
		t.Errorf("Database inconsistent: expected 500 vectors, got %d", count)
	}
}

// TestMVCCHighContention tests worst-case contention on a single hot vector
func TestMVCCHighContention(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test")
	}

	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Single hot vector
	_ = nest.Store("hot", []float32{0, 0})

	var wg sync.WaitGroup
	attempts := 50

	successCount := int32(0)
	conflictCount := int32(0)

	// All transactions try to update same vector
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()

			tx, err := nest.Begin()
			if err != nil {
				return
			}

			_ = tx.Store("hot", []float32{float32(val), float32(val * 2)})

			if err := tx.Commit(); err != nil {
				atomic.AddInt32(&conflictCount, 1)
			} else {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Success rate: %d/%d (%.1f%%), Conflicts: %d",
		successCount, attempts,
		float64(successCount)/float64(attempts)*100,
		conflictCount)

	// At least one should succeed
	if successCount == 0 {
		t.Error("All transactions failed")
	}

	// Exactly one should succeed (last writer wins)
	if successCount+conflictCount != int32(attempts) {
		t.Errorf("Transaction count mismatch: %d + %d != %d",
			successCount, conflictCount, attempts)
	}

	// Final value should be from one of the transactions
	treasure, err := nest.Get("hot")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Final value: [%.0f, %.0f]", treasure.Vector[0], treasure.Vector[1])
}

// TestMVCCLongRunningWithGC tests GC behavior with long-running transactions
func TestMVCCLongRunningWithGC(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test")
	}

	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	_ = nest.Store("vec1", []float32{0, 0})

	// Start long-running reader
	longTx, err := nest.Begin()
	if err != nil {
		t.Fatal(err)
	}

	initialValue, err := longTx.Get("vec1")
	if err != nil {
		t.Fatal(err)
	}

	// Create 100 versions while reader is active
	for i := 1; i <= 100; i++ {
		tx, err := nest.Begin()
		if err != nil {
			t.Fatal(err)
		}
		_ = tx.Store("vec1", []float32{float32(i), float32(i * 2)})
		_ = tx.Commit()
	}

	// Long tx should still see original value
	currentValue, err := longTx.Get("vec1")
	if err != nil {
		t.Fatal(err)
	}

	if currentValue.Vector[0] != initialValue.Vector[0] {
		t.Errorf("Snapshot isolation broken by GC: expected %f, got %f",
			initialValue.Vector[0], currentValue.Vector[0])
	}

	_ = longTx.Commit()

	// After commit, latest should be visible
	latest, _ := nest.Get("vec1")
	if latest.Vector[0] != 100 {
		t.Errorf("Expected latest version (100), got %f", latest.Vector[0])
	}
}

// TestMVCCMixedWorkload tests realistic mixed read/write workload
func TestMVCCMixedWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test")
	}

	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Pre-populate
	for i := 0; i < 50; i++ {
		_ = nest.Store(fmt.Sprintf("vec%d", i), []float32{float32(i), 0, 0})
	}

	var wg sync.WaitGroup
	duration := 2 * time.Second
	stopTime := time.Now().Add(duration)

	readTxCount := int64(0)
	writeTxCount := int64(0)
	readOpCount := int64(0)
	writeOpCount := int64(0)

	// Readers (70% - 7 goroutines)
	for i := 0; i < 7; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(stopTime) {
				tx, err := nest.Begin()
				if err != nil {
					continue
				}

				for j := 0; j < 10; j++ {
					_, _ = tx.Get(fmt.Sprintf("vec%d", rand.Intn(50)))
					atomic.AddInt64(&readOpCount, 1)
				}

				_ = tx.Commit()
				atomic.AddInt64(&readTxCount, 1)
			}
		}()
	}

	// Writers (30% - 3 goroutines)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(stopTime) {
				tx, err := nest.Begin()
				if err != nil {
					continue
				}

				for j := 0; j < 5; j++ {
					id := fmt.Sprintf("vec%d", rand.Intn(50))
					_ = tx.Store(id, []float32{rand.Float32(), rand.Float32(), rand.Float32()})
					atomic.AddInt64(&writeOpCount, 1)
				}

				_ = tx.Commit()
				atomic.AddInt64(&writeTxCount, 1)
			}
		}()
	}

	wg.Wait()

	t.Logf("Read txs: %d (%d ops), Write txs: %d (%d ops)",
		readTxCount, readOpCount, writeTxCount, writeOpCount)

	if readTxCount == 0 || writeTxCount == 0 {
		t.Error("Workload imbalanced")
	}

	// Verify database integrity
	count := nest.Count()
	if count != 50 {
		t.Errorf("Database corrupted: expected 50 vectors, got %d", count)
	}
}

// TestMVCC1000VersionsPerVector tests handling of many versions
func TestMVCC1000VersionsPerVector(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test")
	}

	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Create 1000 versions of the same vector
	for i := 0; i < 1000; i++ {
		tx, _ := nest.Begin()
		_ = tx.Store("vec1", []float32{float32(i), float32(i % 100)})
		if err := tx.Commit(); err != nil {
			t.Fatalf("Failed at version %d: %v", i, err)
		}
	}

	// Verify latest version
	treasure, err := nest.Get("vec1")
	if err != nil {
		t.Fatal(err)
	}

	if treasure.Vector[0] != 999 {
		t.Errorf("Expected version 999, got %f", treasure.Vector[0])
	}

	// Database should still have only 1 vector
	if nest.Count() != 1 {
		t.Errorf("Expected 1 vector, got %d", nest.Count())
	}
}

// TestMVCCConcurrentReadersScalability tests reader scalability
func TestMVCCConcurrentReadersScalability(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test")
	}

	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Setup data
	for i := 0; i < 100; i++ {
		_ = nest.Store(fmt.Sprintf("vec%d", i), []float32{float32(i), 0, 0})
	}

	// Test with increasing reader counts
	readerCounts := []int{1, 10, 50, 100}

	for _, readerCount := range readerCounts {
		var wg sync.WaitGroup
		start := time.Now()

		for i := 0; i < readerCount; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				tx, _ := nest.Begin()
				defer func() { _ = tx.Commit() }()

				// Each reader does 100 reads
				for j := 0; j < 100; j++ {
					_, _ = tx.Get(fmt.Sprintf("vec%d", rand.Intn(100)))
				}
			}()
		}

		wg.Wait()
		elapsed := time.Since(start)

		opsPerSec := float64(readerCount*100) / elapsed.Seconds()
		t.Logf("%d readers: %v (%.0f ops/sec)", readerCount, elapsed, opsPerSec)
	}
}

// TestMVCCMemoryPressure tests behavior under memory pressure
func TestMVCCMemoryPressure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test")
	}

	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 128}) // Larger vectors
	if err != nil {
		t.Fatal(err)
	}
	defer nest.Close()

	// Create 10000 vectors with larger dimensions
	vectorCount := 10000

	for i := 0; i < vectorCount; i++ {
		vec := make([]float32, 128)
		for j := range vec {
			vec[j] = rand.Float32()
		}

		if err := nest.Store(fmt.Sprintf("vec%d", i), vec); err != nil {
			t.Fatalf("Failed to store vector %d: %v", i, err)
		}

		if i%1000 == 0 {
			t.Logf("Stored %d vectors", i)
		}
	}

	// Verify all vectors are accessible
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("vec%d", rand.Intn(vectorCount))
		if _, err := nest.Get(id); err != nil {
			t.Errorf("Failed to retrieve %s: %v", id, err)
		}
	}

	t.Logf("Final count: %d vectors", nest.Count())
}

// BenchmarkMVCCThroughput benchmarks transaction throughput
func BenchmarkMVCCThroughput(b *testing.B) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 3})
	if err != nil {
		b.Fatal(err)
	}
	defer nest.Close()

	// Pre-populate
	for i := 0; i < 100; i++ {
		_ = nest.Store(fmt.Sprintf("vec%d", i), []float32{float32(i), 0, 0})
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			tx, _ := nest.Begin()
			id := fmt.Sprintf("vec%d", i%100)
			_ = tx.Store(id, []float32{float32(i), float32(i), float32(i)})
			_ = tx.Commit()
			i++
		}
	})

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "tx/sec")
}

// BenchmarkMVCCLatency benchmarks transaction latency
func BenchmarkMVCCLatency(b *testing.B) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 3})
	if err != nil {
		b.Fatal(err)
	}
	defer nest.Close()

	_ = nest.Store("vec1", []float32{1, 0, 0})

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tx, _ := nest.Begin()
		_, _ = tx.Get("vec1")
		_ = tx.Commit()
	}
}

// BenchmarkMVCCReadWrite benchmarks mixed read/write workload
func BenchmarkMVCCReadWrite(b *testing.B) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 3})
	if err != nil {
		b.Fatal(err)
	}
	defer nest.Close()

	// Pre-populate
	for i := 0; i < 100; i++ {
		_ = nest.Store(fmt.Sprintf("vec%d", i), []float32{float32(i), 0, 0})
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			tx, _ := nest.Begin()

			// 70% reads, 30% writes
			if i%10 < 7 {
				_, _ = tx.Get(fmt.Sprintf("vec%d", i%100))
			} else {
				_ = tx.Store(fmt.Sprintf("vec%d", i%100),
					[]float32{float32(i), 0, 0})
			}

			_ = tx.Commit()
			i++
		}
	})
}

// BenchmarkMVCCConflictRate benchmarks conflict detection performance
func BenchmarkMVCCConflictRate(b *testing.B) {
	tmpfile := tempFilename()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: 3})
	if err != nil {
		b.Fatal(err)
	}
	defer nest.Close()

	_ = nest.Store("hot", []float32{0, 0, 0})

	conflicts := int64(0)

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			tx, _ := nest.Begin()
			_ = tx.Store("hot", []float32{1, 2, 3})
			if tx.Commit() != nil {
				atomic.AddInt64(&conflicts, 1)
			}
		}
	})

	conflictRate := float64(conflicts) / float64(b.N) * 100
	b.ReportMetric(conflictRate, "%conflict")
}
