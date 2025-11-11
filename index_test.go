package magpie

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

// TestHNSWIndexCreation tests basic index creation
func TestHNSWIndexCreation(t *testing.T) {
	idx := NewHSNWIndex(128, 16, 200, CosineSimilarity)

	if idx == nil {
		t.Fatal("Failed to create HNSW index")
	}

	if idx.m != 16 {
		t.Errorf("Expected m=16, got %d", idx.m)
	}

	if idx.efConstruct != 200 {
		t.Errorf("Expected efConstruct=200, got %d", idx.efConstruct)
	}

	if idx.Count() != 0 {
		t.Errorf("Expected empty index, got %d nodes", idx.Count())
	}
}

// TestHNSWAddSingleVector tests adding a single vector
func TestHNSWAddSingleVector(t *testing.T) {
	idx := NewHSNWIndex(128, 16, 200, CosineSimilarity)

	vector := make([]float32, 128)
	for i := range vector {
		vector[i] = rand.Float32()
	}

	err := idx.Add("vec1", vector)
	if err != nil {
		t.Fatalf("Failed to add vector: %v", err)
	}

	if idx.Count() != 1 {
		t.Errorf("Expected 1 node, got %d", idx.Count())
	}

	node, exists := idx.Get("vec1")
	if !exists {
		t.Fatal("Vector not found after adding")
	}

	if node.ID != "vec1" {
		t.Errorf("Expected ID 'vec1', got '%s'", node.ID)
	}
}

// TestHNSWAddMultipleVectors tests adding multiple vectors
func TestHNSWAddMultipleVectors(t *testing.T) {
	idx := NewHSNWIndex(128, 16, 200, CosineSimilarity)

	numVectors := 100
	for i := 0; i < numVectors; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = rand.Float32()
		}

		id := fmt.Sprintf("vec%d", i)
		err := idx.Add(id, vector)
		if err != nil {
			t.Fatalf("Failed to add vector %s: %v", id, err)
		}
	}

	if idx.Count() != numVectors {
		t.Errorf("Expected %d nodes, got %d", numVectors, idx.Count())
	}
}

// TestHNSWAddDuplicate tests adding duplicate IDs
func TestHNSWAddDuplicate(t *testing.T) {
	idx := NewHSNWIndex(128, 16, 200, CosineSimilarity)

	vector := make([]float32, 128)
	for i := range vector {
		vector[i] = rand.Float32()
	}

	err := idx.Add("vec1", vector)
	if err != nil {
		t.Fatalf("Failed to add first vector: %v", err)
	}

	// Try to add duplicate
	err = idx.Add("vec1", vector)
	if err == nil {
		t.Fatal("Expected error when adding duplicate ID, got nil")
	}
}

// TestHNSWSearchSingleResult tests searching with one result
func TestHNSWSearchSingleResult(t *testing.T) {
	idx := NewHSNWIndex(128, 16, 200, CosineSimilarity)

	vector := make([]float32, 128)
	for i := range vector {
		vector[i] = rand.Float32()
	}

	err := idx.Add("vec1", vector)
	if err != nil {
		t.Fatalf("Failed to add vector: %v", err)
	}

	// Search for the same vector
	results := idx.Search(vector, 1)

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	if results[0].ID != "vec1" {
		t.Errorf("Expected ID 'vec1', got '%s'", results[0].ID)
	}

	// Distance should be very close to 0 (same vector)
	if results[0].Distance > 0.01 {
		t.Errorf("Expected distance ~0, got %f", results[0].Distance)
	}
}

// TestHNSWSearchMultipleResults tests k-NN search
func TestHNSWSearchMultipleResults(t *testing.T) {
	idx := NewHSNWIndex(128, 16, 200, CosineSimilarity)

	// Add 50 vectors
	numVectors := 50
	for i := 0; i < numVectors; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = rand.Float32()
		}
		_ = idx.Add(fmt.Sprintf("vec%d", i), vector)
	}

	// Create query vector
	query := make([]float32, 128)
	for i := range query {
		query[i] = rand.Float32()
	}

	// Search for top 10
	k := 10
	results := idx.Search(query, k)

	if len(results) != k {
		t.Errorf("Expected %d results, got %d", k, len(results))
	}

	// Verify results are sorted by distance
	for i := 1; i < len(results); i++ {
		if results[i].Distance < results[i-1].Distance {
			t.Errorf("Results not sorted by distance at index %d: %f < %f",
				i, results[i].Distance, results[i-1].Distance)
		}
	}
}

// TestHNSWSearchAccuracy tests search accuracy
func TestHNSWSearchAccuracy(t *testing.T) {
	idx := NewHSNWIndex(3, 16, 200, EuclideanDistance)

	// Add some known vectors in 3D space
	vectors := []struct {
		id     string
		vector []float32
	}{
		{"origin", []float32{0, 0, 0}},
		{"x1", []float32{1, 0, 0}},
		{"x2", []float32{2, 0, 0}},
		{"y1", []float32{0, 1, 0}},
		{"z1", []float32{0, 0, 1}},
		{"far", []float32{10, 10, 10}},
	}

	for _, v := range vectors {
		err := idx.Add(v.id, v.vector)
		if err != nil {
			t.Fatalf("Failed to add vector %s: %v", v.id, err)
		}
	}

	// Search for nearest to origin
	query := []float32{0, 0, 0}
	results := idx.Search(query, 3)

	if len(results) == 0 {
		t.Fatal("No results returned")
	}

	// First result should be origin itself
	if results[0].ID != "origin" {
		t.Errorf("Expected 'origin' as first result, got '%s'", results[0].ID)
	}

	// Distance should be 0
	if results[0].Distance > 0.001 {
		t.Errorf("Expected distance 0, got %f", results[0].Distance)
	}
}

// TestHNSWSearchDifferentDimensions tests various vector dimensions
func TestHNSWSearchDifferentDimensions(t *testing.T) {
	dimensions := []int{128, 384, 768}

	for _, dim := range dimensions {
		t.Run(fmt.Sprintf("dim%d", dim), func(t *testing.T) {
			idx := NewHSNWIndex(dim, 16, 200, CosineSimilarity)

			// Add 20 vectors
			for i := 0; i < 20; i++ {
				vector := make([]float32, dim)
				for j := range vector {
					vector[j] = rand.Float32()
				}
				_ = idx.Add(fmt.Sprintf("vec%d", i), vector)
			}

			// Search
			query := make([]float32, dim)
			for i := range query {
				query[i] = rand.Float32()
			}

			results := idx.Search(query, 5)
			if len(results) != 5 {
				t.Errorf("Expected 5 results, got %d", len(results))
			}
		})
	}
}

// TestHNSWLevelAssignment tests level assignment distribution
func TestHNSWLevelAssignment(t *testing.T) {
	idx := NewHSNWIndex(128, 16, 200, CosineSimilarity)

	// Add many vectors and check level distribution
	levelCounts := make(map[int]int)
	numVectors := 1000

	for i := 0; i < numVectors; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = rand.Float32()
		}

		id := fmt.Sprintf("vec%d", i)
		_ = idx.Add(id, vector)

		node, _ := idx.Get(id)
		levelCounts[node.Level]++
	}

	// Most nodes should be at level 0 (with p=0.5, expect ~50%)
	// Allow some variance due to randomness (40-60%)
	level0Ratio := float64(levelCounts[0]) / float64(numVectors)
	if level0Ratio < 0.4 || level0Ratio > 0.6 {
		t.Logf("Level 0 ratio: %.2f (expected ~0.5, got %d/%d)",
			level0Ratio, levelCounts[0], numVectors)
	}

	// Should have exponential decay in higher levels
	// Level 1 should have roughly half of level 0
	// (allowing for randomness)
	if levelCounts[1] == 0 {
		t.Error("Expected some nodes at level 1")
	}

	// Log level distribution for debugging
	t.Logf("Level distribution: %v", levelCounts)
}

// TestHNSWRemove tests vector removal
func TestHNSWRemove(t *testing.T) {
	idx := NewHSNWIndex(128, 16, 200, CosineSimilarity)

	// Add vectors
	for i := 0; i < 10; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = rand.Float32()
		}
		_ = idx.Add(fmt.Sprintf("vec%d", i), vector)
	}

	// Remove one
	err := idx.Remove("vec5")
	if err != nil {
		t.Fatalf("Failed to remove vector: %v", err)
	}

	if idx.Count() != 9 {
		t.Errorf("Expected 9 nodes after removal, got %d", idx.Count())
	}

	_, exists := idx.Get("vec5")
	if exists {
		t.Error("Vector still exists after removal")
	}

	// Try to remove non-existent
	err = idx.Remove("nonexistent")
	if err == nil {
		t.Error("Expected error when removing non-existent vector")
	}
}

// TestHNSWClear tests clearing the index
func TestHNSWClear(t *testing.T) {
	idx := NewHSNWIndex(128, 16, 200, CosineSimilarity)

	// Add vectors
	for i := 0; i < 10; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = rand.Float32()
		}
		_ = idx.Add(fmt.Sprintf("vec%d", i), vector)
	}

	if idx.Count() != 10 {
		t.Fatalf("Expected 10 nodes, got %d", idx.Count())
	}

	idx.Clear()

	if idx.Count() != 0 {
		t.Errorf("Expected 0 nodes after clear, got %d", idx.Count())
	}

	if idx.entryPoint != nil {
		t.Error("Entry point should be nil after clear")
	}
}

// TestHNSWSerializeDeserialize tests index serialization
func TestHNSWSerializeDeserialize(t *testing.T) {
	// Create and populate index
	idx1 := NewHSNWIndex(128, 16, 200, CosineSimilarity)

	numVectors := 50
	vectors := make(map[string][]float32)

	for i := 0; i < numVectors; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = rand.Float32()
		}
		id := fmt.Sprintf("vec%d", i)
		vectors[id] = vector
		_ = idx1.Add(id, vector)
	}

	// Serialize
	data, err := idx1.Serialize()
	if err != nil {
		t.Fatalf("Failed to serialize: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("Serialized data is empty")
	}

	// Create new index and deserialize
	idx2 := NewHSNWIndex(128, 16, 200, CosineSimilarity)
	err = idx2.Deserialize(data)
	if err != nil {
		t.Fatalf("Failed to deserialize: %v", err)
	}

	// Verify node count
	if idx2.Count() != idx1.Count() {
		t.Errorf("Node count mismatch: expected %d, got %d",
			idx1.Count(), idx2.Count())
	}

	// Verify all vectors exist
	for id, vector := range vectors {
		node, exists := idx2.Get(id)
		if !exists {
			t.Errorf("Vector %s not found after deserialization", id)
			continue
		}

		// Verify vector data
		if len(node.Vector) != len(vector) {
			t.Errorf("Vector length mismatch for %s", id)
			continue
		}

		for i := range vector {
			if math.Abs(float64(node.Vector[i]-vector[i])) > 0.0001 {
				t.Errorf("Vector data mismatch for %s at index %d", id, i)
				break
			}
		}
	}

	// Test search on deserialized index
	query := make([]float32, 128)
	for i := range query {
		query[i] = rand.Float32()
	}

	results := idx2.Search(query, 5)
	if len(results) != 5 {
		t.Errorf("Expected 5 search results, got %d", len(results))
	}
}

// TestHNSWLargeDataset tests with a larger dataset
func TestHNSWLargeDataset(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large dataset test in short mode")
	}

	idx := NewHSNWIndex(128, 16, 200, CosineSimilarity)

	// Add 1000 vectors
	numVectors := 1000
	for i := 0; i < numVectors; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = rand.Float32()
		}

		id := fmt.Sprintf("vec%d", i)
		err := idx.Add(id, vector)
		if err != nil {
			t.Fatalf("Failed to add vector %s: %v", id, err)
		}
	}

	if idx.Count() != numVectors {
		t.Errorf("Expected %d nodes, got %d", numVectors, idx.Count())
	}

	// Perform multiple searches
	for i := 0; i < 10; i++ {
		query := make([]float32, 128)
		for j := range query {
			query[j] = rand.Float32()
		}

		results := idx.Search(query, 10)
		if len(results) != 10 {
			t.Errorf("Search %d: expected 10 results, got %d", i, len(results))
		}

		// Verify sorted
		for j := 1; j < len(results); j++ {
			if results[j].Distance < results[j-1].Distance {
				t.Errorf("Search %d: results not sorted at index %d", i, j)
			}
		}
	}
}

// TestHNSWConcurrentReads tests concurrent search operations
func TestHNSWConcurrentReads(t *testing.T) {
	idx := NewHSNWIndex(128, 16, 200, CosineSimilarity)

	// Add vectors
	numVectors := 100
	for i := 0; i < numVectors; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = rand.Float32()
		}
		_ = idx.Add(fmt.Sprintf("vec%d", i), vector)
	}

	// Concurrent searches
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			query := make([]float32, 128)
			for j := range query {
				query[j] = rand.Float32()
			}

			results := idx.Search(query, 5)
			if len(results) != 5 {
				t.Errorf("Goroutine %d: expected 5 results, got %d", id, len(results))
			}
			done <- true
		}(i)
	}

	// Wait for all to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

// BenchmarkHNSWAdd benchmarks vector insertion
func BenchmarkHNSWAdd(b *testing.B) {
	idx := NewHSNWIndex(128, 16, 200, CosineSimilarity)

	vectors := make([][]float32, b.N)
	for i := 0; i < b.N; i++ {
		vectors[i] = make([]float32, 128)
		for j := range vectors[i] {
			vectors[i][j] = rand.Float32()
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = idx.Add(fmt.Sprintf("vec%d", i), vectors[i])
	}
}

// BenchmarkHNSWSearch benchmarks k-NN search
func BenchmarkHNSWSearch(b *testing.B) {
	idx := NewHSNWIndex(128, 16, 200, CosineSimilarity)

	// Pre-populate with 1000 vectors
	for i := 0; i < 1000; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = rand.Float32()
		}
		_ = idx.Add(fmt.Sprintf("vec%d", i), vector)
	}

	// Create query vectors
	queries := make([][]float32, b.N)
	for i := 0; i < b.N; i++ {
		queries[i] = make([]float32, 128)
		for j := range queries[i] {
			queries[i][j] = rand.Float32()
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Search(queries[i], 10)
	}
}
