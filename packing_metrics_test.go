package magpie

import (
	"fmt"
	"os"
	"testing"
)

// TestPackingSpaceSavings measures actual file size reduction from packing
func TestPackingSpaceSavings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping space savings test in short mode")
	}

	// Test with 1000 vectors at 128 dimensions
	const numVectors = 1000
	const dimensions = 128

	// Create test database
	tempDir := t.TempDir()
	path := tempDir + "/packing_test.db"

	opts := DefaultOptions()
	opts.Dimensions = dimensions
	opts.WAL = false

	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}

	// Store vectors
	for i := 0; i < numVectors; i++ {
		id := fmt.Sprintf("vec_%04d", i)
		vec := make([]float32, dimensions)
		for j := range vec {
			vec[j] = float32(i*1000 + j)
		}
		if err := nest.Store(id, vec); err != nil {
			t.Fatalf("failed to store vector %d: %v", i, err)
		}
	}

	// Get size before compaction
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	sizeBeforeCompact := fileInfo.Size()

	// Compact with packing
	if err := nest.Compact(); err != nil {
		t.Fatalf("compaction failed: %v", err)
	}

	// Get size after compaction
	fileInfo, err = os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat file after compaction: %v", err)
	}
	sizeAfterCompact := fileInfo.Size()

	// Verify all vectors still accessible
	for i := 0; i < numVectors; i++ {
		id := fmt.Sprintf("vec_%04d", i)
		treasure, err := nest.Get(id)
		if err != nil {
			t.Errorf("failed to get vector %d after compaction: %v", i, err)
		}
		if treasure.ID != id {
			t.Errorf("ID mismatch after compaction: got %s, want %s", treasure.ID, id)
		}
	}

	nest.Close()

	// Calculate savings
	savings := float64(sizeBeforeCompact-sizeAfterCompact) / float64(sizeBeforeCompact) * 100

	// Calculate theoretical sizes
	headerSize := int64(PageSize)

	// Without packing: 1 vector per page
	// Each vector: 80 bytes (entry) + 128*4 bytes (vector) = 592 bytes
	// But uses full 4096 byte page = 84% waste
	theoreticalUnpacked := headerSize + (numVectors * PageSize)

	// With packing: 6 vectors per page (for 128 dims)
	// 1000 vectors / 6 = 167 pages
	vectorsPerPage := CalculateVectorsPerPage(dimensions)
	pagesNeeded := (numVectors + vectorsPerPage - 1) / vectorsPerPage
	theoreticalPacked := headerSize + (int64(pagesNeeded) * PageSize) + PageSize // +1 for index

	theoreticalSavings := float64(theoreticalUnpacked-theoreticalPacked) / float64(theoreticalUnpacked) * 100

	t.Logf("=== PACKING SPACE SAVINGS REPORT ===")
	t.Logf("Vectors: %d, Dimensions: %d", numVectors, dimensions)
	t.Logf("Vectors per page: %d", vectorsPerPage)
	t.Logf("")
	t.Logf("Size before compact: %d bytes (%.1f KB)", sizeBeforeCompact, float64(sizeBeforeCompact)/1024)
	t.Logf("Size after compact:  %d bytes (%.1f KB)", sizeAfterCompact, float64(sizeAfterCompact)/1024)
	t.Logf("Space saved:         %d bytes (%.1f KB)", sizeBeforeCompact-sizeAfterCompact, float64(sizeBeforeCompact-sizeAfterCompact)/1024)
	t.Logf("Savings:             %.1f%%", savings)
	t.Logf("")
	t.Logf("Theoretical unpacked: %d bytes (%.1f KB)", theoreticalUnpacked, float64(theoreticalUnpacked)/1024)
	t.Logf("Theoretical packed:   %d bytes (%.1f KB)", theoreticalPacked, float64(theoreticalPacked)/1024)
	t.Logf("Theoretical savings:  %.1f%%", theoreticalSavings)

	// Verify we achieved significant savings (at least 40%)
	if savings < 40.0 {
		t.Errorf("Expected at least 40%% savings, got %.1f%%", savings)
	}
}

// TestPackingEfficiencyComparison compares packing efficiency across different dimensions
func TestPackingEfficiencyComparison(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping efficiency comparison in short mode")
	}

	testCases := []struct {
		dims     int
		numVecs  int
		minSave  float64 // minimum expected savings %
	}{
		{32, 500, 70},   // Very efficient: 19 per page
		{64, 500, 65},   // Efficient: 11 per page
		{128, 500, 55},  // Good: 6 per page
		{256, 200, 20},  // Limited: 3 per page (index overhead significant)
		{512, 100, -100}, // No packing benefit: 1 per page (index overhead dominates)
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("dims=%d", tc.dims), func(t *testing.T) {
			tempDir := t.TempDir()
			path := tempDir + "/efficiency_test.db"

			opts := DefaultOptions()
			opts.Dimensions = tc.dims
			opts.WAL = false

			nest, err := Open(path, opts)
			if err != nil {
				t.Fatalf("failed to open nest: %v", err)
			}

			// Store vectors
			for i := 0; i < tc.numVecs; i++ {
				id := fmt.Sprintf("v%d", i)
				vec := make([]float32, tc.dims)
				for j := range vec {
					vec[j] = float32(i + j)
				}
				if err := nest.Store(id, vec); err != nil {
					t.Fatalf("failed to store: %v", err)
				}
			}

			// Measure before
			info1, _ := os.Stat(path)
			sizeBefore := info1.Size()

			// Compact
			if err := nest.Compact(); err != nil {
				t.Fatalf("compact failed: %v", err)
			}

			// Measure after
			info2, _ := os.Stat(path)
			sizeAfter := info2.Size()

			nest.Close()

			savings := float64(sizeBefore-sizeAfter) / float64(sizeBefore) * 100
			vectorsPerPage := CalculateVectorsPerPage(tc.dims)

			t.Logf("Dims=%d: Before=%dKB, After=%dKB, Savings=%.1f%%, VecsPerPage=%d",
				tc.dims, sizeBefore/1024, sizeAfter/1024, savings, vectorsPerPage)

			if savings < tc.minSave {
				t.Errorf("Expected at least %.1f%% savings, got %.1f%%", tc.minSave, savings)
			}
		})
	}
}

// TestPackingWastePercentage verifies waste is below threshold
func TestPackingWastePercentage(t *testing.T) {
	testCases := []struct {
		dims      int
		maxWaste  float64 // maximum acceptable waste %
	}{
		{32, 30},   // 19 per page
		{64, 35},   // 11 per page
		{128, 45},  // 6 per page
		{256, 50},  // 3 per page
		{512, 85},  // 1 per page (high waste acceptable)
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("dims=%d", tc.dims), func(t *testing.T) {
			numVecs := 100
			vectors := make([]VectorData, numVecs)
			for i := 0; i < numVecs; i++ {
				vectors[i] = VectorData{
					ID:     fmt.Sprintf("vec%d", i),
					Vector: make([]float32, tc.dims),
				}
			}

			pages := PackVectorsIntoPages(vectors)

			// Calculate waste
			entrySize := 80
			vectorSize := tc.dims * 4
			usedSpace := numVecs * (entrySize + vectorSize)
			totalSpace := len(pages) * PageSize

			waste := float64(totalSpace-usedSpace) / float64(totalSpace) * 100

			t.Logf("Dims=%d: Pages=%d, Used=%dKB, Total=%dKB, Waste=%.1f%%",
				tc.dims, len(pages), usedSpace/1024, totalSpace/1024, waste)

			if waste > tc.maxWaste {
				t.Errorf("Waste %.1f%% exceeds maximum %.1f%%", waste, tc.maxWaste)
			}
		})
	}
}
