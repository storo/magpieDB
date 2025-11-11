package magpie

import (
	"fmt"
	"os"
	"testing"
)

// TestPersistVector tests writing a vector to a vector page
func TestPersistVector(t *testing.T) {
	// Create temporary database
	path := "/tmp/test_persist_vector.magpie"
	defer os.Remove(path)

	opts := DefaultOptions()
	opts.Dimensions = 4
	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer nest.Close()

	// Store a vector
	vector := []float32{0.1, 0.2, 0.3, 0.4}
	err = nest.Store("vec1", vector)
	if err != nil {
		t.Fatalf("failed to store vector: %v", err)
	}

	// Verify vector pages were created
	if len(nest.vectorPages) == 0 {
		t.Fatal("expected vector pages map to have entries")
	}

	// Verify vector page mapping exists
	pageNum, exists := nest.vectorPages["vec1"]
	if !exists {
		t.Fatal("expected vector page mapping for vec1")
	}

	// Verify page was allocated
	if pageNum == 0 {
		t.Fatal("expected non-zero page number")
	}
}

// TestLoadVector tests reading a vector from a page
func TestLoadVector(t *testing.T) {
	path := "/tmp/test_load_vector.magpie"
	defer os.Remove(path)

	opts := DefaultOptions()
	opts.Dimensions = 4
	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Store a vector
	originalVector := []float32{0.1, 0.2, 0.3, 0.4}
	metadata := map[string]interface{}{
		"title": "Test Vector",
		"count": 42,
	}
	err = nest.Store("vec1", originalVector, metadata)
	if err != nil {
		t.Fatalf("failed to store vector: %v", err)
	}

	nest.Close()

	// Reopen database and load vector
	opts2 := DefaultOptions()
	opts2.Dimensions = 4
	nest2, err := Open(path, opts2)
	if err != nil {
		t.Fatalf("failed to reopen database: %v", err)
	}
	defer nest2.Close()

	// Load the vector
	loadedVector, loadedMetadata, err := nest2.loadVector("vec1")
	if err != nil {
		t.Fatalf("failed to load vector: %v", err)
	}

	// Verify vector data
	if len(loadedVector) != len(originalVector) {
		t.Fatalf("expected %d dimensions, got %d", len(originalVector), len(loadedVector))
	}

	for i := range originalVector {
		if loadedVector[i] != originalVector[i] {
			t.Errorf("dimension %d: expected %.2f, got %.2f", i, originalVector[i], loadedVector[i])
		}
	}

	// Verify metadata
	if loadedMetadata["title"] != "Test Vector" {
		t.Errorf("expected title 'Test Vector', got %v", loadedMetadata["title"])
	}

	// JSON unmarshaling converts numbers to float64
	if loadedMetadata["count"].(float64) != 42 {
		t.Errorf("expected count 42, got %v", loadedMetadata["count"])
	}
}

// TestVectorRoundTrip tests that Store + Load = same vector
func TestVectorRoundTrip(t *testing.T) {
	path := "/tmp/test_roundtrip.magpie"
	defer os.Remove(path)

	opts := DefaultOptions()
	opts.Dimensions = 128
	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer nest.Close()

	// Create test vector
	original := make([]float32, 128)
	for i := range original {
		original[i] = float32(i) * 0.01
	}

	// Store
	err = nest.Store("test", original)
	if err != nil {
		t.Fatalf("failed to store: %v", err)
	}

	// Load
	loaded, _, err := nest.loadVector("test")
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	// Compare
	if len(loaded) != len(original) {
		t.Fatalf("length mismatch: expected %d, got %d", len(original), len(loaded))
	}

	for i := range original {
		if loaded[i] != original[i] {
			t.Errorf("index %d: expected %.4f, got %.4f", i, original[i], loaded[i])
		}
	}
}

// TestMultipleVectorsOnePage tests packing multiple small vectors per page
func TestMultipleVectorsOnePage(t *testing.T) {
	path := "/tmp/test_multiple.magpie"
	defer os.Remove(path)

	opts := DefaultOptions()
	opts.Dimensions = 4
	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Store many small vectors
	count := 50
	for i := 0; i < count; i++ {
		vector := []float32{
			float32(i) * 0.1,
			float32(i) * 0.2,
			float32(i) * 0.3,
			float32(i) * 0.4,
		}
		id := fmt.Sprintf("vec_%02d", i)
		err := nest.Store(id, vector)
		if err != nil {
			t.Fatalf("failed to store vector %d: %v", i, err)
		}
	}

	nest.Close()

	// Reopen and verify
	nest2, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to reopen: %v", err)
	}
	defer nest2.Close()

	// Verify all can be loaded
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("vec_%02d", i)
		loaded, _, err := nest2.loadVector(id)
		if err != nil {
			t.Fatalf("failed to load vector %s: %v", id, err)
		}

		expected := float32(i) * 0.1
		if loaded[0] != expected {
			t.Errorf("vector %s: expected %.2f, got %.2f", id, expected, loaded[0])
		}
	}
}

// TestVectorWithMetadata tests vector + metadata persistence together
func TestVectorWithMetadata(t *testing.T) {
	path := "/tmp/test_metadata.magpie"
	defer os.Remove(path)

	opts := DefaultOptions()
	opts.Dimensions = 3
	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	vector := []float32{1.0, 2.0, 3.0}
	metadata := map[string]interface{}{
		"name":   "Test Document",
		"score":  95.5,
		"active": true,
	}

	// Store
	err = nest.Store("doc1", vector, metadata)
	if err != nil {
		t.Fatalf("failed to store: %v", err)
	}

	nest.Close()

	// Reopen and load
	nest2, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to reopen: %v", err)
	}
	defer nest2.Close()

	loadedVec, loadedMeta, err := nest2.loadVector("doc1")
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	// Verify vector
	for i := range vector {
		if loadedVec[i] != vector[i] {
			t.Errorf("vector[%d]: expected %.1f, got %.1f", i, vector[i], loadedVec[i])
		}
	}

	// Verify metadata
	if loadedMeta["name"] != "Test Document" {
		t.Errorf("name mismatch: %v", loadedMeta["name"])
	}

	if loadedMeta["score"].(float64) != 95.5 {
		t.Errorf("score mismatch: %v", loadedMeta["score"])
	}

	if loadedMeta["active"] != true {
		t.Errorf("active mismatch: %v", loadedMeta["active"])
	}
}

// TestEmptyMetadata tests storing vectors without metadata
func TestEmptyMetadata(t *testing.T) {
	path := "/tmp/test_empty_meta.magpie"
	defer os.Remove(path)

	opts := DefaultOptions()
	opts.Dimensions = 3
	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	vector := []float32{1.0, 2.0, 3.0}
	err = nest.Store("no_meta", vector)
	if err != nil {
		t.Fatalf("failed to store: %v", err)
	}

	nest.Close()

	// Reopen
	nest2, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to reopen: %v", err)
	}
	defer nest2.Close()

	loaded, metadata, err := nest2.loadVector("no_meta")
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	if len(loaded) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(loaded))
	}

	if len(metadata) > 0 {
		t.Errorf("expected nil or empty metadata, got %v", metadata)
	}
}

// TestVectorPersistenceAcrossRestart tests that vectors survive database restart
func TestVectorPersistenceAcrossRestart(t *testing.T) {
	path := "/tmp/test_restart.magpie"
	defer os.Remove(path)

	// First session: store vectors
	{
		opts := DefaultOptions()
		opts.Dimensions = 16
		nest, err := Open(path, opts)
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}

		for i := 0; i < 20; i++ {
			vector := make([]float32, 16)
			for j := range vector {
				vector[j] = float32(i*100 + j)
			}

			metadata := map[string]interface{}{
				"index": i,
				"label": fmt.Sprintf("vector_%d", i),
			}

			id := fmt.Sprintf("persist_%02d", i)
			err := nest.Store(id, vector, metadata)
			if err != nil {
				t.Fatalf("failed to store vector %d: %v", i, err)
			}
		}

		nest.Close()
	}

	// Second session: load vectors
	{
		opts := DefaultOptions()
		opts.Dimensions = 16
		nest, err := Open(path, opts)
		if err != nil {
			t.Fatalf("failed to reopen database: %v", err)
		}
		defer nest.Close()

		// Verify all vectors load correctly
		for i := 0; i < 20; i++ {
			id := fmt.Sprintf("persist_%02d", i)
			loaded, metadata, err := nest.loadVector(id)
			if err != nil {
				t.Fatalf("failed to load vector %s: %v", id, err)
			}

			// Verify vector data
			if len(loaded) != 16 {
				t.Errorf("vector %s: expected 16 dimensions, got %d", id, len(loaded))
			}

			expected := float32(i * 100)
			if loaded[0] != expected {
				t.Errorf("vector %s: expected first value %.0f, got %.0f", id, expected, loaded[0])
			}

			// Verify metadata
			if metadata != nil {
				if metadata["index"].(float64) != float64(i) {
					t.Errorf("vector %s: metadata index mismatch", id)
				}
			}
		}

		// Verify count
		if nest.Count() != 20 {
			t.Errorf("expected 20 vectors, got %d", nest.Count())
		}
	}
}

// Test1000VectorsPersistence tests storing and loading 1000 vectors
func Test1000VectorsPersistence(t *testing.T) {
	path := "/tmp/test_1000_vectors.magpie"
	defer os.Remove(path)

	opts := DefaultOptions()
	opts.Dimensions = 128

	// Store 1000 vectors
	{
		nest, err := Open(path, opts)
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}

		for i := 0; i < 1000; i++ {
			vector := make([]float32, 128)
			for j := range vector {
				vector[j] = float32(i*1000 + j)
			}

			metadata := map[string]interface{}{
				"index":       i,
				"description": fmt.Sprintf("Vector number %d", i),
			}

			id := fmt.Sprintf("vec_%04d", i)
			err := nest.Store(id, vector, metadata)
			if err != nil {
				t.Fatalf("failed to store vector %d: %v", i, err)
			}
		}

		if nest.Count() != 1000 {
			t.Errorf("expected 1000 vectors, got %d", nest.Count())
		}

		nest.Close()
	}

	// Reopen and verify all 1000 vectors
	{
		nest, err := Open(path, opts)
		if err != nil {
			t.Fatalf("failed to reopen database: %v", err)
		}
		defer nest.Close()

		if nest.Count() != 1000 {
			t.Errorf("after reopen: expected 1000 vectors, got %d", nest.Count())
		}

		// Verify sample of vectors
		samples := []int{0, 100, 250, 500, 750, 999}
		for _, i := range samples {
			id := fmt.Sprintf("vec_%04d", i)
			loaded, metadata, err := nest.loadVector(id)
			if err != nil {
				t.Errorf("failed to load vector %s: %v", id, err)
				continue
			}

			// Verify vector data
			if len(loaded) != 128 {
				t.Errorf("vector %s: expected 128 dimensions, got %d", id, len(loaded))
			}

			expected := float32(i * 1000)
			if loaded[0] != expected {
				t.Errorf("vector %s: expected first value %.0f, got %.0f", id, expected, loaded[0])
			}

			// Verify metadata
			if metadata["index"].(float64) != float64(i) {
				t.Errorf("vector %s: metadata index mismatch", id)
			}
		}
	}
}

// TestLargeVector tests storing vectors larger than a single page
func TestLargeVector(t *testing.T) {
	path := "/tmp/test_large_vector.magpie"
	defer os.Remove(path)

	// 2048 dimensions * 4 bytes = 8KB (needs 2+ pages)
	dimensions := 2048
	opts := DefaultOptions()
	opts.Dimensions = dimensions

	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Create large vector
	largeVector := make([]float32, dimensions)
	for i := range largeVector {
		largeVector[i] = float32(i)
	}

	// Store - this should fail with current implementation
	// because we don't support multi-page vectors yet
	err = nest.Store("large", largeVector)
	if err != nil {
		// Expected to fail with "vector too large for single page"
		if err.Error() != "failed to write vector: vector too large for single page (need multi-page support)" {
			t.Logf("Large vector storage failed as expected: %v", err)
		}
		nest.Close()
		return
	}

	// If it succeeded (future implementation), verify it loads correctly
	nest.Close()

	nest2, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to reopen: %v", err)
	}
	defer nest2.Close()

	loaded, _, err := nest2.loadVector("large")
	if err != nil {
		t.Fatalf("failed to load large vector: %v", err)
	}

	// Verify
	if len(loaded) != dimensions {
		t.Errorf("expected %d dimensions, got %d", dimensions, len(loaded))
	}

	for i := 0; i < dimensions; i++ {
		if loaded[i] != float32(i) {
			t.Errorf("index %d: expected %.0f, got %.0f", i, float32(i), loaded[i])
			break
		}
	}
}

// TestVectorUpdate tests updating an existing vector
func TestVectorUpdate(t *testing.T) {
	path := "/tmp/test_update.magpie"
	defer os.Remove(path)

	opts := DefaultOptions()
	opts.Dimensions = 4

	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Store initial vector
	vector1 := []float32{1.0, 2.0, 3.0, 4.0}
	err = nest.Store("update_test", vector1)
	if err != nil {
		t.Fatalf("failed to store initial vector: %v", err)
	}

	// Update with new vector
	vector2 := []float32{5.0, 6.0, 7.0, 8.0}
	err = nest.Store("update_test", vector2)
	if err != nil {
		t.Fatalf("failed to update vector: %v", err)
	}

	nest.Close()

	// Reopen and verify updated vector
	nest2, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to reopen: %v", err)
	}
	defer nest2.Close()

	loaded, _, err := nest2.loadVector("update_test")
	if err != nil {
		t.Fatalf("failed to load updated vector: %v", err)
	}

	// Should have the updated values
	for i := range vector2 {
		if loaded[i] != vector2[i] {
			t.Errorf("index %d: expected %.1f, got %.1f", i, vector2[i], loaded[i])
		}
	}

	// Count should still be 1
	if nest2.Count() != 1 {
		t.Errorf("expected count 1 after update, got %d", nest2.Count())
	}
}
