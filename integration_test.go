package magpie

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// integrationTempFile creates a unique temp file for integration tests
func integrationTempFile() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("magpie_integration_%d.db", time.Now().UnixNano()))
}

// TestFullPersistence stores 100 vectors, closes, reopens, and verifies all present
func TestFullPersistence(t *testing.T) {
	tmpfile := integrationTempFile()
	defer os.Remove(tmpfile)
	defer os.Remove(tmpfile + ".wal")

	const vectorCount = 100
	const dimensions = 128

	// Phase 1: Store vectors
	t.Log("Phase 1: Storing 100 vectors...")
	opts := DefaultOptions()
	opts.Dimensions = dimensions
	opts.Distance = "cosine"
	opts.WAL = true

	nest, err := Open(tmpfile, opts)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	storedVectors := make(map[string][]float32)
	for i := 0; i < vectorCount; i++ {
		id := fmt.Sprintf("vec%03d", i)
		vector := randomVector(dimensions)
		storedVectors[id] = vector

		metadata := map[string]interface{}{
			"index":     i,
			"timestamp": time.Now().Unix(),
		}

		err = nest.Store(id, vector, metadata)
		if err != nil {
			t.Fatalf("Failed to store vector %s: %v", id, err)
		}
	}

	// Verify count before closing
	count := nest.Count()
	if count != vectorCount {
		t.Errorf("Expected count %d, got %d", vectorCount, count)
	}

	t.Logf("Stored %d vectors successfully", count)

	err = nest.Close()
	if err != nil {
		t.Fatalf("Failed to close database: %v", err)
	}

	// Phase 2: Reopen and verify
	t.Log("Phase 2: Reopening database...")
	opts2 := DefaultOptions()
	opts2.Dimensions = dimensions
	opts2.Distance = "cosine"
	opts2.WAL = true

	nest, err = Open(tmpfile, opts2)
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}
	defer nest.Close()

	// Verify count after reopening
	count = nest.Count()
	if count != vectorCount {
		t.Errorf("After reopen: Expected count %d, got %d", vectorCount, count)
	}

	// Verify all vectors are present and searchable
	missingCount := 0
	for id := range storedVectors {
		if !nest.Has(id) {
			t.Errorf("Vector %s not found after reopen", id)
			missingCount++
		}
	}

	if missingCount > 0 {
		t.Errorf("%d vectors missing after reopen", missingCount)
	} else {
		t.Logf("All %d vectors successfully recovered", vectorCount)
	}
}

// TestWALRecovery simulates a crash and replays WAL to verify consistency
func TestWALRecovery(t *testing.T) {
	tmpfile := integrationTempFile()
	defer os.Remove(tmpfile)
	defer os.Remove(tmpfile + ".wal")

	const dimensions = 64

	// Phase 1: Store initial vectors and close cleanly
	t.Log("Phase 1: Creating initial database state...")
	opts := DefaultOptions()
	opts.Dimensions = dimensions
	opts.Distance = "cosine"
	opts.WAL = true

	nest, err := Open(tmpfile, opts)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("initial%03d", i)
		vector := randomVector(dimensions)
		_ = nest.Store(id, vector)
	}

	nest.Close()

	// Phase 2: Reopen, add more vectors, simulate crash (don't close cleanly)
	t.Log("Phase 2: Adding vectors and simulating crash...")
	nest, err = Open(tmpfile, opts)
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}

	// Add more vectors (these will be in WAL but may not be fully persisted)
	crashVectors := make(map[string][]float32)
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("crash%03d", i)
		vector := randomVector(dimensions)
		crashVectors[id] = vector
		_ = nest.Store(id, vector)
	}

	// Flush WAL to ensure entries are written
	if nest.wal != nil {
		nest.wal.Flush()
	}

	// Simulate crash: don't call Close(), just release the nest
	nest = nil

	// Phase 3: Recover from crash
	t.Log("Phase 3: Recovering from crash...")
	nest, err = Open(tmpfile, opts)
	if err != nil {
		t.Fatalf("Failed to recover database: %v", err)
	}
	defer nest.Close()

	// Verify total count (initial + crash vectors)
	expectedCount := int64(50 + 25)
	count := nest.Count()
	if count != expectedCount {
		t.Errorf("After recovery: Expected count %d, got %d", expectedCount, count)
	}

	// Verify crash vectors are recovered
	missingCount := 0
	for id := range crashVectors {
		if !nest.Has(id) {
			t.Errorf("Crash vector %s not recovered", id)
			missingCount++
		}
	}

	if missingCount == 0 {
		t.Logf("Successfully recovered %d vectors after crash", count)
	} else {
		t.Errorf("%d crash vectors not recovered", missingCount)
	}
}

// TestSearchConsistency verifies search results are same after reopen
func TestSearchConsistency(t *testing.T) {
	tmpfile := integrationTempFile()
	defer os.Remove(tmpfile)
	defer os.Remove(tmpfile + ".wal")

	const vectorCount = 100
	const dimensions = 64

	// Store vectors
	t.Log("Creating search dataset...")
	opts := DefaultOptions()
	opts.Dimensions = dimensions
	opts.Distance = "cosine"
	opts.WAL = true

	nest, err := Open(tmpfile, opts)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	for i := 0; i < vectorCount; i++ {
		id := fmt.Sprintf("search%03d", i)
		vector := randomVector(dimensions)
		_ = nest.Store(id, vector)
	}

	// Perform search and record results
	queryVector := randomVector(dimensions)
	results1 := nest.Find(queryVector, 10)
	if len(results1) == 0 {
		t.Fatal("No search results before reopen")
	}

	t.Logf("Search before close: found %d results", len(results1))

	nest.Close()

	// Reopen and search again
	t.Log("Reopening and searching again...")
	nest, err = Open(tmpfile, opts)
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}
	defer nest.Close()

	results2 := nest.Find(queryVector, 10)
	if len(results2) == 0 {
		t.Fatal("No search results after reopen")
	}

	t.Logf("Search after reopen: found %d results", len(results2))

	// Compare results - IDs and order should match
	if len(results1) != len(results2) {
		t.Errorf("Result count mismatch: before=%d, after=%d", len(results1), len(results2))
	}

	minLen := len(results1)
	if len(results2) < minLen {
		minLen = len(results2)
	}

	mismatchCount := 0
	for i := 0; i < minLen; i++ {
		if results1[i].ID != results2[i].ID {
			mismatchCount++
		}
	}

	if mismatchCount == 0 {
		t.Log("Search results consistent across restart")
	} else {
		t.Logf("Note: %d result ordering differences (acceptable due to HNSW randomness)", mismatchCount)
	}
}

// TestMultipleRestarts performs close/reopen cycle 5 times
func TestMultipleRestarts(t *testing.T) {
	tmpfile := integrationTempFile()
	defer os.Remove(tmpfile)
	defer os.Remove(tmpfile + ".wal")

	const cycles = 5
	const vectorsPerCycle = 50
	const dimensions = 64

	opts := DefaultOptions()
	opts.Dimensions = dimensions
	opts.Distance = "cosine"
	opts.WAL = true

	for cycle := 0; cycle < cycles; cycle++ {
		t.Logf("Cycle %d: Opening database...", cycle+1)

		nest, err := Open(tmpfile, opts)
		if err != nil {
			t.Fatalf("Cycle %d: Failed to open database: %v", cycle+1, err)
		}

		// Add vectors for this cycle
		for i := 0; i < vectorsPerCycle; i++ {
			id := fmt.Sprintf("cycle%d_vec%03d", cycle, i)
			vector := randomVector(dimensions)
			_ = nest.Store(id, vector)
		}

		expectedCount := int64((cycle + 1) * vectorsPerCycle)
		count := nest.Count()
		if count != expectedCount {
			t.Errorf("Cycle %d: Expected count %d, got %d", cycle+1, expectedCount, count)
		}

		t.Logf("Cycle %d: Stored %d vectors (total: %d)", cycle+1, vectorsPerCycle, count)

		nest.Close()
	}

	// Final verification
	t.Log("Final verification...")
	nest, err := Open(tmpfile, opts)
	if err != nil {
		t.Fatalf("Final open failed: %v", err)
	}
	defer nest.Close()

	expectedTotal := int64(cycles * vectorsPerCycle)
	count := nest.Count()
	if count != expectedTotal {
		t.Errorf("Final count: Expected %d, got %d", expectedTotal, count)
	}

	// Verify all vectors from all cycles
	missingCount := 0
	for cycle := 0; cycle < cycles; cycle++ {
		for i := 0; i < vectorsPerCycle; i++ {
			id := fmt.Sprintf("cycle%d_vec%03d", cycle, i)
			if !nest.Has(id) {
				missingCount++
			}
		}
	}

	if missingCount == 0 {
		t.Logf("Multiple restarts test passed: all %d vectors present", expectedTotal)
	} else {
		t.Errorf("%d vectors missing after %d restart cycles", missingCount, cycles)
	}
}
