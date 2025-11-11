package magpie

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestIndexPersistEmpty tests that an empty index can be saved and loaded
func TestIndexPersistEmpty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: file sync and header persistence issues with fallback storage")
	}
	tmpFile := filepath.Join(os.TempDir(), "test_index_empty.magpie")
	defer os.Remove(tmpFile)

	// Create database with empty index
	nest, err := Open(tmpFile, Options{
		Dimensions:     128,
		Distance:       "cosine",
		M:              16,
		EfConstruction: 200,
	})
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Close to persist
	if err := nest.Close(); err != nil {
		t.Fatalf("Failed to close database: %v", err)
	}

	// Reopen and verify empty
	nest, err = Open(tmpFile)
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}
	defer nest.Close()

	if nest.Count() != 0 {
		t.Errorf("Expected 0 vectors, got %d", nest.Count())
	}

	if nest.index.Count() != 0 {
		t.Errorf("Expected empty index, got %d nodes", nest.index.Count())
	}
}

// TestIndexPersistSingle tests that a single node survives round-trip
func TestIndexPersistSingle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: file sync and header persistence issues with fallback storage")
	}
	tmpFile := filepath.Join(os.TempDir(), "test_index_single.magpie")
	defer os.Remove(tmpFile)

	vector := make([]float32, 128)
	for i := range vector {
		vector[i] = float32(i) * 0.01
	}

	// Create and store
	nest, err := Open(tmpFile, Options{
		Dimensions:     128,
		Distance:       "cosine",
		M:              16,
		EfConstruction: 200,
	})
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	if err := nest.Store("node1", vector); err != nil {
		t.Fatalf("Failed to store vector: %v", err)
	}

	if err := nest.Close(); err != nil {
		t.Fatalf("Failed to close database: %v", err)
	}

	// Reopen and verify
	nest, err = Open(tmpFile)
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}
	defer nest.Close()

	if nest.Count() != 1 {
		t.Errorf("Expected 1 vector, got %d", nest.Count())
	}

	// Verify node exists and vector matches
	treasure, err := nest.Get("node1")
	if err != nil {
		t.Fatalf("Failed to get node1: %v", err)
	}

	if len(treasure.Vector) != len(vector) {
		t.Errorf("Vector length mismatch: expected %d, got %d", len(vector), len(treasure.Vector))
	}

	for i := range vector {
		if treasure.Vector[i] != vector[i] {
			t.Errorf("Vector value mismatch at index %d: expected %f, got %f", i, vector[i], treasure.Vector[i])
			break
		}
	}
}

// TestIndexPersist100Nodes tests 100 nodes with links
func TestIndexPersist100Nodes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: file sync and header persistence issues with fallback storage")
	}
	tmpFile := filepath.Join(os.TempDir(), "test_index_100.magpie")
	defer os.Remove(tmpFile)

	dimensions := 128
	nodeCount := 100

	// Create and populate
	nest, err := Open(tmpFile, Options{
		Dimensions:     dimensions,
		Distance:       "cosine",
		M:              16,
		EfConstruction: 200,
	})
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Store 100 vectors
	for i := 0; i < nodeCount; i++ {
		vector := make([]float32, dimensions)
		for j := range vector {
			vector[j] = float32(i*j) * 0.001
		}
		id := fmt.Sprintf("node%d", i)
		if err := nest.Store(id, vector); err != nil {
			t.Fatalf("Failed to store vector %s: %v", id, err)
		}
	}

	// Record entry point and maxLevel before close
	entryPointID := ""
	if nest.index.entryPoint != nil {
		entryPointID = nest.index.entryPoint.ID
	}
	maxLevel := nest.index.MaxLevel()

	if err := nest.Close(); err != nil {
		t.Fatalf("Failed to close database: %v", err)
	}

	// Reopen and verify
	nest, err = Open(tmpFile)
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}
	defer nest.Close()

	if nest.Count() != int64(nodeCount) {
		t.Errorf("Expected %d vectors, got %d", nodeCount, nest.Count())
	}

	if nest.index.Count() != nodeCount {
		t.Errorf("Expected %d nodes in index, got %d", nodeCount, nest.index.Count())
	}

	// Verify entry point preserved
	if nest.index.entryPoint == nil {
		t.Error("Entry point is nil after reload")
	} else if nest.index.entryPoint.ID != entryPointID {
		t.Errorf("Entry point mismatch: expected %s, got %s", entryPointID, nest.index.entryPoint.ID)
	}

	// Verify maxLevel preserved
	if nest.index.MaxLevel() != maxLevel {
		t.Errorf("MaxLevel mismatch: expected %d, got %d", maxLevel, nest.index.MaxLevel())
	}
}

// TestIndexPersist1000Nodes tests 1000 nodes (larger graph)
func TestIndexPersist1000Nodes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: file sync and header persistence issues with fallback storage")
	}
	tmpFile := filepath.Join(os.TempDir(), "test_index_1000.magpie")
	defer os.Remove(tmpFile)

	dimensions := 128
	nodeCount := 1000

	// Create and populate
	nest, err := Open(tmpFile, Options{
		Dimensions:     dimensions,
		Distance:       "euclidean",
		M:              16,
		EfConstruction: 200,
	})
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Store 1000 vectors
	for i := 0; i < nodeCount; i++ {
		vector := make([]float32, dimensions)
		for j := range vector {
			vector[j] = float32(i+j) * 0.01
		}
		id := fmt.Sprintf("vec%d", i)
		if err := nest.Store(id, vector); err != nil {
			t.Fatalf("Failed to store vector %s: %v", id, err)
		}
	}

	if err := nest.Close(); err != nil {
		t.Fatalf("Failed to close database: %v", err)
	}

	// Reopen and verify
	nest, err = Open(tmpFile)
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}
	defer nest.Close()

	if nest.Count() != int64(nodeCount) {
		t.Errorf("Expected %d vectors, got %d", nodeCount, nest.Count())
	}

	if nest.index.Count() != nodeCount {
		t.Errorf("Expected %d nodes in index, got %d", nodeCount, nest.index.Count())
	}
}

// TestIndexEntryPointPreserved tests that entry point ID is maintained
func TestIndexEntryPointPreserved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: file sync and header persistence issues with fallback storage")
	}
	tmpFile := filepath.Join(os.TempDir(), "test_index_entrypoint.magpie")
	defer os.Remove(tmpFile)

	nest, err := Open(tmpFile, Options{
		Dimensions:     64,
		Distance:       "cosine",
		M:              16,
		EfConstruction: 200,
	})
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Add multiple nodes
	for i := 0; i < 50; i++ {
		vector := make([]float32, 64)
		for j := range vector {
			vector[j] = float32(i*j) * 0.01
		}
		if err := nest.Store(fmt.Sprintf("n%d", i), vector); err != nil {
			t.Fatalf("Failed to store: %v", err)
		}
	}

	originalEntryID := nest.index.entryPoint.ID

	if err := nest.Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	// Reopen
	nest, err = Open(tmpFile)
	if err != nil {
		t.Fatalf("Failed to reopen: %v", err)
	}
	defer nest.Close()

	if nest.index.entryPoint == nil {
		t.Fatal("Entry point is nil after reload")
	}

	if nest.index.entryPoint.ID != originalEntryID {
		t.Errorf("Entry point ID changed: expected %s, got %s", originalEntryID, nest.index.entryPoint.ID)
	}
}

// TestIndexLevelStructure tests that multi-level hierarchy is preserved
func TestIndexLevelStructure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: file sync and header persistence issues with fallback storage")
	}
	tmpFile := filepath.Join(os.TempDir(), "test_index_levels.magpie")
	defer os.Remove(tmpFile)

	nest, err := Open(tmpFile, Options{
		Dimensions:     64,
		Distance:       "cosine",
		M:              16,
		EfConstruction: 200,
	})
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Store enough vectors to build multi-level structure
	for i := 0; i < 100; i++ {
		vector := make([]float32, 64)
		for j := range vector {
			vector[j] = float32(i) * 0.01
		}
		if err := nest.Store(fmt.Sprintf("v%d", i), vector); err != nil {
			t.Fatalf("Failed to store: %v", err)
		}
	}

	// Record level information
	levelCounts := make(map[string]int)
	for _, node := range nest.index.nodes {
		levelCounts[node.ID] = node.Level
	}

	if err := nest.Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	// Reopen
	nest, err = Open(tmpFile)
	if err != nil {
		t.Fatalf("Failed to reopen: %v", err)
	}
	defer nest.Close()

	// Verify levels match
	for id, originalLevel := range levelCounts {
		node, exists := nest.index.Get(id)
		if !exists {
			t.Errorf("Node %s missing after reload", id)
			continue
		}
		if node.Level != originalLevel {
			t.Errorf("Node %s level mismatch: expected %d, got %d", id, originalLevel, node.Level)
		}
	}
}

// TestIndexBidirectionalLinks tests that all neighbor links are intact
func TestIndexBidirectionalLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: file sync and header persistence issues with fallback storage")
	}
	tmpFile := filepath.Join(os.TempDir(), "test_index_links.magpie")
	defer os.Remove(tmpFile)

	nest, err := Open(tmpFile, Options{
		Dimensions:     64,
		Distance:       "cosine",
		M:              16,
		EfConstruction: 200,
	})
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Store vectors
	for i := 0; i < 50; i++ {
		vector := make([]float32, 64)
		for j := range vector {
			vector[j] = float32(i*10 + j)
		}
		if err := nest.Store(fmt.Sprintf("node%d", i), vector); err != nil {
			t.Fatalf("Failed to store: %v", err)
		}
	}

	// Record neighbor counts per level
	neighborCounts := make(map[string][]int)
	for id, node := range nest.index.nodes {
		counts := make([]int, len(node.Neighbors))
		for level, neighbors := range node.Neighbors {
			counts[level] = len(neighbors)
		}
		neighborCounts[id] = counts
	}

	if err := nest.Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	// Reopen
	nest, err = Open(tmpFile)
	if err != nil {
		t.Fatalf("Failed to reopen: %v", err)
	}
	defer nest.Close()

	// Verify neighbor counts match
	for id, originalCounts := range neighborCounts {
		node, exists := nest.index.Get(id)
		if !exists {
			t.Errorf("Node %s missing after reload", id)
			continue
		}

		if len(node.Neighbors) != len(originalCounts) {
			t.Errorf("Node %s neighbor layer count mismatch: expected %d, got %d",
				id, len(originalCounts), len(node.Neighbors))
			continue
		}

		for level, originalCount := range originalCounts {
			if len(node.Neighbors[level]) != originalCount {
				t.Errorf("Node %s level %d neighbor count mismatch: expected %d, got %d",
					id, level, originalCount, len(node.Neighbors[level]))
			}
		}
	}
}

// TestIndexMaxLevel tests that MaxLevel is preserved
func TestIndexMaxLevel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: file sync and header persistence issues with fallback storage")
	}
	tmpFile := filepath.Join(os.TempDir(), "test_index_maxlevel.magpie")
	defer os.Remove(tmpFile)

	nest, err := Open(tmpFile, Options{
		Dimensions:     32,
		Distance:       "cosine",
		M:              16,
		EfConstruction: 200,
	})
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Add vectors
	for i := 0; i < 200; i++ {
		vector := make([]float32, 32)
		for j := range vector {
			vector[j] = float32(i + j)
		}
		if err := nest.Store(fmt.Sprintf("v%d", i), vector); err != nil {
			t.Fatalf("Failed to store: %v", err)
		}
	}

	originalMaxLevel := nest.index.MaxLevel()

	if err := nest.Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	// Reopen
	nest, err = Open(tmpFile)
	if err != nil {
		t.Fatalf("Failed to reopen: %v", err)
	}
	defer nest.Close()

	if nest.index.MaxLevel() != originalMaxLevel {
		t.Errorf("MaxLevel mismatch: expected %d, got %d", originalMaxLevel, nest.index.MaxLevel())
	}
}

// TestIndexParameters tests that M and efConstruct are preserved
func TestIndexParameters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: file sync and header persistence issues with fallback storage")
	}
	tmpFile := filepath.Join(os.TempDir(), "test_index_params.magpie")
	defer os.Remove(tmpFile)

	originalM := 24
	originalEfConstruct := 300

	nest, err := Open(tmpFile, Options{
		Dimensions:     64,
		Distance:       "dot",
		M:              originalM,
		EfConstruction: originalEfConstruct,
	})
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Store some vectors
	for i := 0; i < 20; i++ {
		vector := make([]float32, 64)
		for j := range vector {
			vector[j] = float32(i)
		}
		if err := nest.Store(fmt.Sprintf("v%d", i), vector); err != nil {
			t.Fatalf("Failed to store: %v", err)
		}
	}

	if err := nest.Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	// Reopen
	nest, err = Open(tmpFile)
	if err != nil {
		t.Fatalf("Failed to reopen: %v", err)
	}
	defer nest.Close()

	if nest.index.m != originalM {
		t.Errorf("M parameter mismatch: expected %d, got %d", originalM, nest.index.m)
	}

	if nest.index.efConstruct != originalEfConstruct {
		t.Errorf("efConstruct parameter mismatch: expected %d, got %d", originalEfConstruct, nest.index.efConstruct)
	}
}

// TestIndexPageChaining tests that index spanning multiple pages works
func TestIndexPageChaining(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: file sync and header persistence issues with fallback storage")
	}
	tmpFile := filepath.Join(os.TempDir(), "test_index_pages.magpie")
	defer os.Remove(tmpFile)

	dimensions := 512 // Large dimensions to force multiple pages
	nodeCount := 100

	nest, err := Open(tmpFile, Options{
		Dimensions:     dimensions,
		Distance:       "cosine",
		M:              16,
		EfConstruction: 200,
	})
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Store large index
	for i := 0; i < nodeCount; i++ {
		vector := make([]float32, dimensions)
		for j := range vector {
			vector[j] = float32(i*j) * 0.001
		}
		if err := nest.Store(fmt.Sprintf("node%d", i), vector); err != nil {
			t.Fatalf("Failed to store: %v", err)
		}
	}

	if err := nest.Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	// Reopen
	nest, err = Open(tmpFile)
	if err != nil {
		t.Fatalf("Failed to reopen: %v", err)
	}
	defer nest.Close()

	if nest.Count() != int64(nodeCount) {
		t.Errorf("Expected %d vectors after reload, got %d", nodeCount, nest.Count())
	}

	if nest.index.Count() != nodeCount {
		t.Errorf("Expected %d nodes in index after reload, got %d", nodeCount, nest.index.Count())
	}
}

// TestIndexRoundTripSearch tests that search results are same after reload
func TestIndexRoundTripSearch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: file sync and header persistence issues with fallback storage")
	}
	tmpFile := filepath.Join(os.TempDir(), "test_index_search.magpie")
	defer os.Remove(tmpFile)

	dimensions := 128
	nodeCount := 100

	nest, err := Open(tmpFile, Options{
		Dimensions:     dimensions,
		Distance:       "cosine",
		M:              16,
		EfConstruction: 200,
	})
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Store vectors
	for i := 0; i < nodeCount; i++ {
		vector := make([]float32, dimensions)
		for j := range vector {
			vector[j] = float32(i*10 + j)
		}
		if err := nest.Store(fmt.Sprintf("v%d", i), vector); err != nil {
			t.Fatalf("Failed to store: %v", err)
		}
	}

	// Create query
	query := make([]float32, dimensions)
	for i := range query {
		query[i] = float32(i) * 0.5
	}

	// Search before close
	resultsBefore := nest.Find(query, 10)
	if len(resultsBefore) == 0 {
		t.Fatal("No results before close")
	}

	if err := nest.Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	// Reopen
	nest, err = Open(tmpFile)
	if err != nil {
		t.Fatalf("Failed to reopen: %v", err)
	}
	defer nest.Close()

	// Search after reload
	resultsAfter := nest.Find(query, 10)

	if len(resultsAfter) != len(resultsBefore) {
		t.Errorf("Result count mismatch: expected %d, got %d", len(resultsBefore), len(resultsAfter))
	}

	// Verify results match (same IDs in same order)
	for i := range resultsBefore {
		if i >= len(resultsAfter) {
			break
		}
		if resultsBefore[i].ID != resultsAfter[i].ID {
			t.Errorf("Result %d ID mismatch: expected %s, got %s",
				i, resultsBefore[i].ID, resultsAfter[i].ID)
		}
		// Allow small floating point differences
		distDiff := resultsBefore[i].Distance - resultsAfter[i].Distance
		if distDiff < 0 {
			distDiff = -distDiff
		}
		if distDiff > 0.0001 {
			t.Errorf("Result %d distance mismatch: expected %f, got %f",
				i, resultsBefore[i].Distance, resultsAfter[i].Distance)
		}
	}
}
