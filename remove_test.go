package magpie

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// TestRemoveBasic verifies basic removal functionality
func TestRemoveBasic(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.db"

	opts := DefaultOptions()
	opts.Dimensions = 128
	opts.WAL = true

	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}
	defer nest.Close()

	// Store a vector
	vector := make([]float32, 128)
	for i := range vector {
		vector[i] = float32(i) * 0.1
	}

	if err := nest.Store("vec1", vector); err != nil {
		t.Fatalf("failed to store vector: %v", err)
	}

	// Verify it exists
	if !nest.Has("vec1") {
		t.Fatal("vector should exist before removal")
	}

	// Remove the vector
	if err := nest.Remove("vec1"); err != nil {
		t.Fatalf("failed to remove vector: %v", err)
	}

	// Verify it no longer exists
	if nest.Has("vec1") {
		t.Error("vector should not exist after removal")
	}

	// Verify Search doesn't return it
	results := nest.Find(vector, 10)
	for _, result := range results {
		if result.ID == "vec1" {
			t.Error("removed vector should not appear in search results")
		}
	}
}

// TestRemoveNonExistent verifies error handling for non-existent vectors
func TestRemoveNonExistent(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.db"

	opts := DefaultOptions()
	opts.Dimensions = 128
	opts.WAL = true

	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}
	defer nest.Close()

	// Try to remove a vector that doesn't exist
	err = nest.Remove("nonexistent")
	if err == nil {
		t.Fatal("expected error when removing non-existent vector")
	}

	// Verify error message
	expected := "vector nonexistent not found"
	if err.Error() != expected {
		t.Errorf("expected error '%s', got '%s'", expected, err.Error())
	}
}

// TestRemoveAndSearch verifies Search doesn't return deleted vectors
func TestRemoveAndSearch(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.db"

	opts := DefaultOptions()
	opts.Dimensions = 128
	opts.WAL = true

	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}
	defer nest.Close()

	// Store multiple vectors
	for i := 0; i < 10; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = float32(i*j) * 0.01
		}
		if err := nest.Store(fmt.Sprintf("vec%d", i), vector); err != nil {
			t.Fatalf("failed to store vector %d: %v", i, err)
		}
	}

	// Remove vector 5
	if err := nest.Remove("vec5"); err != nil {
		t.Fatalf("failed to remove vector: %v", err)
	}

	// Search and verify vec5 is not returned
	query := make([]float32, 128)
	for i := range query {
		query[i] = float32(5*i) * 0.01
	}

	results := nest.Find(query, 10)
	for _, result := range results {
		if result.ID == "vec5" {
			t.Error("removed vector vec5 should not appear in search results")
		}
	}

	// Verify we still get other vectors
	if len(results) == 0 {
		t.Error("should still have other vectors in search results")
	}
}

// TestRemoveInvalidatesCache verifies query cache is invalidated
func TestRemoveInvalidatesCache(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.db"

	opts := DefaultOptions()
	opts.Dimensions = 128
	opts.WAL = true
	opts.EnableQueryCache = true

	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}
	defer nest.Close()

	// Store vectors
	for i := 0; i < 5; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = float32(i*j) * 0.01
		}
		if err := nest.Store(fmt.Sprintf("vec%d", i), vector); err != nil {
			t.Fatalf("failed to store vector %d: %v", i, err)
		}
	}

	// Perform search to populate cache
	query := make([]float32, 128)
	results1 := nest.Find(query, 5)
	count1 := len(results1)

	// Remove a vector
	if err := nest.Remove("vec2"); err != nil {
		t.Fatalf("failed to remove vector: %v", err)
	}

	// Search again - cache should be invalidated
	results2 := nest.Find(query, 5)
	count2 := len(results2)

	// Should have one less result
	if count2 >= count1 {
		t.Errorf("expected fewer results after removal, got %d before and %d after", count1, count2)
	}

	// Verify vec2 is not in results
	for _, result := range results2 {
		if result.ID == "vec2" {
			t.Error("removed vector should not appear in cached results")
		}
	}
}

// TestRemoveWithWAL verifies WAL entry is created correctly
// Note: This test verifies that WAL delete entries work by testing persistence
// The WAL is truncated on Close(), so we verify replay instead
func TestRemoveWithWAL(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.db"

	// First session: Store vector and remove it
	{
		opts := DefaultOptions()
		opts.Dimensions = 128
		opts.WAL = true

		nest, err := Open(path, opts)
		if err != nil {
			t.Fatalf("failed to open nest: %v", err)
		}

		// Store a vector
		vector := make([]float32, 128)
		for i := range vector {
			vector[i] = float32(i) * 0.1
		}

		if err := nest.Store("vec1", vector); err != nil {
			t.Fatalf("failed to store vector: %v", err)
		}

		// Force sync WAL
		if err := nest.Sync(); err != nil {
			t.Fatalf("failed to sync: %v", err)
		}

		// Remove the vector (this writes WALDelete)
		if err := nest.Remove("vec1"); err != nil {
			t.Fatalf("failed to remove vector: %v", err)
		}

		// Force sync WAL again
		if err := nest.Sync(); err != nil {
			t.Fatalf("failed to sync: %v", err)
		}

		// Verify vector is removed in current session
		if nest.Has("vec1") {
			t.Error("vector should be removed in current session")
		}

		// Debug: Check count before close
		t.Logf("Vector count before close: %d", nest.Count())

		// Close without crashing - this will truncate WAL after persisting to pages
		if err := nest.Close(); err != nil {
			t.Fatalf("failed to close nest: %v", err)
		}
	}

	// Second session: Verify removal persisted (WAL was replayed correctly)
	{
		opts := DefaultOptions()
		opts.WAL = true

		nest, err := Open(path, opts)
		if err != nil {
			t.Fatalf("failed to reopen nest: %v", err)
		}
		defer nest.Close()

		// Debug: Check vector count
		t.Logf("Vector count after restart: %d", nest.Count())

		// Verify vector is still removed (WAL delete was applied)
		if nest.Has("vec1") {
			t.Error("vector should still be removed after restart")
		}
	}
}

// TestRemoveInTransaction verifies Remove works with MVCC
func TestRemoveInTransaction(t *testing.T) {
	t.Skip("Skipping MVCC transaction test - needs MVCC setup")
	// This test will be implemented once MVCC integration is clarified
}

// TestRemovePersistence verifies removal persists across restarts
func TestRemovePersistence(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.db"

	// First session: create and remove
	{
		opts := DefaultOptions()
		opts.Dimensions = 128
		opts.WAL = true

		nest, err := Open(path, opts)
		if err != nil {
			t.Fatalf("failed to open nest: %v", err)
		}

		// Store vector
		vector := make([]float32, 128)
		for i := range vector {
			vector[i] = float32(i) * 0.1
		}

		if err := nest.Store("vec1", vector); err != nil {
			t.Fatalf("failed to store vector: %v", err)
		}

		// Remove vector
		if err := nest.Remove("vec1"); err != nil {
			t.Fatalf("failed to remove vector: %v", err)
		}

		// Close database
		if err := nest.Close(); err != nil {
			t.Fatalf("failed to close nest: %v", err)
		}
	}

	// Second session: verify removal persisted
	{
		opts := DefaultOptions()
		opts.WAL = true

		nest, err := Open(path, opts)
		if err != nil {
			t.Fatalf("failed to reopen nest: %v", err)
		}
		defer nest.Close()

		// Verify vector is still removed
		if nest.Has("vec1") {
			t.Error("removed vector should not exist after restart")
		}

		// Verify Search doesn't return it
		query := make([]float32, 128)
		for i := range query {
			query[i] = float32(i) * 0.1
		}

		results := nest.Find(query, 10)
		for _, result := range results {
			if result.ID == "vec1" {
				t.Error("removed vector should not appear in search after restart")
			}
		}
	}
}

// TestRemoveConcurrentReads verifies reads can happen during removes
func TestRemoveConcurrentReads(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.db"

	opts := DefaultOptions()
	opts.Dimensions = 128
	opts.WAL = true

	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}
	defer nest.Close()

	// Store vectors
	for i := 0; i < 100; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = float32(i*j) * 0.001
		}
		if err := nest.Store(fmt.Sprintf("vec%d", i), vector); err != nil {
			t.Fatalf("failed to store vector %d: %v", i, err)
		}
	}

	// Concurrent operations
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Start readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				query := make([]float32, 128)
				for k := range query {
					query[k] = float32(id*k) * 0.001
				}
				_ = nest.Find(query, 5)
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// Start removers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				vecID := fmt.Sprintf("vec%d", id*10+j)
				if err := nest.Remove(vecID); err != nil {
					errors <- fmt.Errorf("remove %s failed: %w", vecID, err)
				}
				time.Sleep(2 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Error(err)
	}

	// Verify removed vectors are gone
	for i := 0; i < 5; i++ {
		for j := 0; j < 10; j++ {
			vecID := fmt.Sprintf("vec%d", i*10+j)
			if nest.Has(vecID) {
				t.Errorf("vector %s should be removed", vecID)
			}
		}
	}
}

// TestRemoveSpaceReclamation verifies compaction can reclaim space
func TestRemoveSpaceReclamation(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.db"

	opts := DefaultOptions()
	opts.Dimensions = 128
	opts.WAL = true

	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}
	defer nest.Close()

	// Store many vectors
	for i := 0; i < 100; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = float32(i*j) * 0.001
		}
		if err := nest.Store(fmt.Sprintf("vec%d", i), vector); err != nil {
			t.Fatalf("failed to store vector %d: %v", i, err)
		}
	}

	// Get initial file size
	info1, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	size1 := info1.Size()

	// Remove many vectors
	for i := 0; i < 50; i++ {
		if err := nest.Remove(fmt.Sprintf("vec%d", i)); err != nil {
			t.Fatalf("failed to remove vector %d: %v", i, err)
		}
	}

	// TODO: Trigger compaction (when implemented)
	// For now, just verify that Remove() works and marks for reclamation
	// The actual space reclamation will be tested when compaction is implemented

	// Verify removed vectors are gone
	for i := 0; i < 50; i++ {
		vecID := fmt.Sprintf("vec%d", i)
		if nest.Has(vecID) {
			t.Errorf("vector %s should be removed", vecID)
		}
	}

	// Verify remaining vectors still exist
	for i := 50; i < 100; i++ {
		vecID := fmt.Sprintf("vec%d", i)
		if !nest.Has(vecID) {
			t.Errorf("vector %s should still exist", vecID)
		}
	}

	// Get final file size (for informational purposes)
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	size2 := info2.Size()

	t.Logf("File size before removal: %d bytes", size1)
	t.Logf("File size after removal: %d bytes", size2)
	t.Log("Note: Space reclamation will be verified when compaction is implemented")
}
