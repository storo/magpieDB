package magpie

import (
	"fmt"
	"os"
	"testing"
)

// TestPackingDebug debugs why compaction doesn't reduce size
func TestPackingDebug(t *testing.T) {
	const numVectors = 100
	const dimensions = 128

	tempDir := t.TempDir()
	path := tempDir + "/debug.db"

	opts := DefaultOptions()
	opts.Dimensions = dimensions
	opts.WAL = false

	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open: %v", err)
	}

	// Store vectors
	for i := 0; i < numVectors; i++ {
		id := fmt.Sprintf("vec%d", i)
		vec := make([]float32, dimensions)
		for j := range vec {
			vec[j] = float32(i + j)
		}
		if err := nest.Store(id, vec); err != nil {
			t.Fatalf("failed to store: %v", err)
		}
	}

	// Get stats before compact
	stats := nest.GetCompactionStats()
	t.Logf("Before compact - Stats: %+v", stats)

	info1, _ := os.Stat(path)
	t.Logf("Before compact - File size: %d bytes (%d KB)", info1.Size(), info1.Size()/1024)
	t.Logf("Before compact - Vector count: %d", nest.header.VectorCount)
	t.Logf("Before compact - Page count: %d", nest.header.PageCount)

	// Compact
	t.Logf("Running compaction...")
	if err := nest.Compact(); err != nil {
		t.Fatalf("compaction failed: %v", err)
	}

	// Get stats after compact
	stats2 := nest.GetCompactionStats()
	t.Logf("After compact - Stats: %+v", stats2)

	info2, _ := os.Stat(path)
	t.Logf("After compact - File size: %d bytes (%d KB)", info2.Size(), info2.Size()/1024)
	t.Logf("After compact - Vector count: %d", nest.header.VectorCount)
	t.Logf("After compact - Page count: %d", nest.header.PageCount)

	// Check if vectors are accessible
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("vec%d", i)
		_, err := nest.Get(id)
		if err != nil {
			t.Errorf("failed to get %s after compact: %v", id, err)
		}
	}

	nest.Close()

	// Calculate expected pages with packing
	vectorsPerPage := CalculateVectorsPerPage(dimensions)
	expectedPages := (numVectors + vectorsPerPage - 1) / vectorsPerPage
	t.Logf("Expected pages with packing: %d (vs actual %d)", expectedPages, nest.header.PageCount)
}

// TestManualPacking tests packing without full database
func TestManualPacking(t *testing.T) {
	const numVectors = 100
	const dimensions = 128

	// Create vectors
	vectors := make([]VectorData, numVectors)
	for i := 0; i < numVectors; i++ {
		vectors[i] = VectorData{
			ID:     fmt.Sprintf("vec%d", i),
			Vector: make([]float32, dimensions),
		}
		for j := range vectors[i].Vector {
			vectors[i].Vector[j] = float32(i + j)
		}
	}

	// Pack them
	packedPages := PackVectorsIntoPages(vectors)
	t.Logf("Packed %d vectors into %d pages", numVectors, len(packedPages))

	// Calculate space usage
	vectorsPerPage := CalculateVectorsPerPage(dimensions)
	t.Logf("Vectors per page: %d", vectorsPerPage)

	entrySize := 80
	vectorSize := dimensions * 4
	dataSize := numVectors * (entrySize + vectorSize)
	totalSize := len(packedPages) * PageSize
	waste := float64(totalSize-dataSize) / float64(totalSize) * 100

	t.Logf("Data size: %d bytes (%d KB)", dataSize, dataSize/1024)
	t.Logf("Total size: %d bytes (%d KB)", totalSize, totalSize/1024)
	t.Logf("Waste: %.1f%%", waste)

	// Verify each page
	for i, page := range packedPages {
		t.Logf("Page %d: %d vectors", i, page.VectorCount)
		if i < len(packedPages)-1 {
			// All but last page should be full
			if int(page.VectorCount) != vectorsPerPage {
				t.Errorf("Page %d should have %d vectors, has %d", i, vectorsPerPage, page.VectorCount)
			}
		}
	}
}
