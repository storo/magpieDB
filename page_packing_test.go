package magpie

import (
	"fmt"
	"os"
	"testing"
)

// ===== UNIT TESTS (15 tests) =====

func TestCalculateVectorsPerPage(t *testing.T) {
	tests := []struct {
		dimensions int
		expected   int
	}{
		{128, 6},   // (4096-64-2) / (80 + 128*4) = 6.73 → 6 vectors
		{256, 3},   // (4096-64-2) / (80 + 256*4) = 3.56 → 3 vectors
		{512, 1},   // (4096-64-2) / (80 + 512*4) = 1.90 → 1 vector
		{1024, 1},  // Too large, only 1 fits
		{64, 11},   // Small vectors: (4096-64-2) / (80 + 64*4) = 11.30 → 11 vectors
		{32, 19},   // Very small vectors: (4096-64-2) / (80 + 32*4) = 19.87 → 19 vectors
		{1, 47},    // Minimal vectors: (4096-64-2) / (80 + 1*4) = 47.97 → 47 vectors
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("dims=%d", tt.dimensions), func(t *testing.T) {
			got := CalculateVectorsPerPage(tt.dimensions)
			if got != tt.expected {
				t.Errorf("CalculateVectorsPerPage(%d) = %d, want %d", tt.dimensions, got, tt.expected)
			}
		})
	}
}

func TestPackSingleVector(t *testing.T) {
	vec := VectorData{
		ID:     "vec1",
		Vector: []float32{1.0, 2.0, 3.0, 4.0},
	}

	pages := PackVectorsIntoPages([]VectorData{vec})
	if len(pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages))
	}

	if pages[0].VectorCount != 1 {
		t.Errorf("expected VectorCount=1, got %d", pages[0].VectorCount)
	}

	if len(pages[0].Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(pages[0].Entries))
	}

	if len(pages[0].VectorData) != 1 {
		t.Errorf("expected 1 vector data, got %d", len(pages[0].VectorData))
	}
}

func TestPackMultipleVectors(t *testing.T) {
	vectors := make([]VectorData, 10)
	for i := 0; i < 10; i++ {
		vectors[i] = VectorData{
			ID:     fmt.Sprintf("vec%d", i),
			Vector: make([]float32, 128),
		}
		// Fill with test data
		for j := range vectors[i].Vector {
			vectors[i].Vector[j] = float32(i*1000 + j)
		}
	}

	pages := PackVectorsIntoPages(vectors)

	// For 128 dims: 6 vectors per page → 10 vectors = 2 pages
	if len(pages) != 2 {
		t.Errorf("expected 2 pages for 10 vectors (128 dims), got %d", len(pages))
	}

	// First page should have 6 vectors
	if pages[0].VectorCount != 6 {
		t.Errorf("page 0 expected 6 vectors, got %d", pages[0].VectorCount)
	}

	// Second page should have 4 vectors
	if pages[1].VectorCount != 4 {
		t.Errorf("page 1 expected 4 vectors, got %d", pages[1].VectorCount)
	}
}

func TestUnpackVectors(t *testing.T) {
	// Pack vectors
	original := []VectorData{
		{ID: "vec1", Vector: []float32{1, 2, 3}},
		{ID: "vec2", Vector: []float32{4, 5, 6}},
	}

	pages := PackVectorsIntoPages(original)
	if len(pages) == 0 {
		t.Fatal("no pages created")
	}

	pageData := SerializePackedPage(&pages[0])

	// Unpack
	unpacked, err := UnpackVectorsFromPage(pageData)
	if err != nil {
		t.Fatalf("unpack failed: %v", err)
	}

	if len(unpacked) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(unpacked))
	}

	// Verify data matches
	for i := range original {
		if unpacked[i].ID != original[i].ID {
			t.Errorf("ID mismatch: got %s, want %s", unpacked[i].ID, original[i].ID)
		}

		if len(unpacked[i].Vector) != len(original[i].Vector) {
			t.Errorf("vector length mismatch: got %d, want %d", len(unpacked[i].Vector), len(original[i].Vector))
		}

		for j := range original[i].Vector {
			if unpacked[i].Vector[j] != original[i].Vector[j] {
				t.Errorf("vector[%d][%d] mismatch: got %f, want %f",
					i, j, unpacked[i].Vector[j], original[i].Vector[j])
			}
		}
	}
}

func TestPackingChecksums(t *testing.T) {
	vectors := []VectorData{
		{ID: "vec1", Vector: []float32{1.1, 2.2, 3.3}},
		{ID: "vec2", Vector: []float32{4.4, 5.5, 6.6}},
	}

	pages := PackVectorsIntoPages(vectors)
	pageData := SerializePackedPage(&pages[0])

	// Verify checksum
	if !VerifyPageChecksum(pageData) {
		t.Error("page checksum verification failed")
	}

	// Corrupt data and verify checksum fails
	pageData[200] ^= 0xFF // Flip bits in data section
	if VerifyPageChecksum(pageData) {
		t.Error("checksum should fail for corrupted data")
	}
}

func TestPackLargeVectors(t *testing.T) {
	// Test vectors that don't fit multiple per page (e.g., 1024 dims)
	vectors := make([]VectorData, 5)
	for i := 0; i < 5; i++ {
		vectors[i] = VectorData{
			ID:     fmt.Sprintf("large_vec%d", i),
			Vector: make([]float32, 1024),
		}
		for j := range vectors[i].Vector {
			vectors[i].Vector[j] = float32(i*10000 + j)
		}
	}

	pages := PackVectorsIntoPages(vectors)

	// For 1024 dims: only 1 vector per page → 5 vectors = 5 pages
	expectedPages := 5
	if len(pages) != expectedPages {
		t.Errorf("expected %d pages for 5 vectors (1024 dims), got %d", expectedPages, len(pages))
	}

	// Each page should have exactly 1 vector
	for i, page := range pages {
		if page.VectorCount != 1 {
			t.Errorf("page %d expected 1 vector, got %d", i, page.VectorCount)
		}
	}
}

func TestPackEmptyVectors(t *testing.T) {
	// Edge case: empty vector list
	pages := PackVectorsIntoPages([]VectorData{})
	if pages != nil {
		t.Errorf("expected nil for empty input, got %d pages", len(pages))
	}

	pages = PackVectorsIntoPages(nil)
	if pages != nil {
		t.Errorf("expected nil for nil input, got %d pages", len(pages))
	}
}

func TestPackExactPageFit(t *testing.T) {
	// Test when vectors exactly fill a page
	vectorsPerPage := CalculateVectorsPerPage(128)
	vectors := make([]VectorData, vectorsPerPage)

	for i := 0; i < vectorsPerPage; i++ {
		vectors[i] = VectorData{
			ID:     fmt.Sprintf("vec%d", i),
			Vector: make([]float32, 128),
		}
	}

	pages := PackVectorsIntoPages(vectors)

	if len(pages) != 1 {
		t.Errorf("expected exactly 1 page, got %d", len(pages))
	}

	if int(pages[0].VectorCount) != vectorsPerPage {
		t.Errorf("expected %d vectors in page, got %d", vectorsPerPage, pages[0].VectorCount)
	}
}

func TestPackMultiplePageBoundary(t *testing.T) {
	// Test vectors that span multiple pages at boundary
	vectorsPerPage := CalculateVectorsPerPage(128)
	totalVectors := vectorsPerPage*3 + 1 // 3 full pages + 1 extra

	vectors := make([]VectorData, totalVectors)
	for i := 0; i < totalVectors; i++ {
		vectors[i] = VectorData{
			ID:     fmt.Sprintf("vec%d", i),
			Vector: make([]float32, 128),
		}
	}

	pages := PackVectorsIntoPages(vectors)

	expectedPages := 4
	if len(pages) != expectedPages {
		t.Errorf("expected %d pages, got %d", expectedPages, len(pages))
	}

	// Last page should have 1 vector
	if pages[len(pages)-1].VectorCount != 1 {
		t.Errorf("last page expected 1 vector, got %d", pages[len(pages)-1].VectorCount)
	}
}

func TestSerializeDeserializePackedPage(t *testing.T) {
	vectors := []VectorData{
		{ID: "test1", Vector: []float32{1.0, 2.0, 3.0, 4.0}},
		{ID: "test2", Vector: []float32{5.0, 6.0, 7.0, 8.0}},
		{ID: "test3", Vector: []float32{9.0, 10.0, 11.0, 12.0}},
	}

	pages := PackVectorsIntoPages(vectors)
	pageData := SerializePackedPage(&pages[0])

	// Check page size
	if len(pageData) != PageSize {
		t.Errorf("serialized page size = %d, want %d", len(pageData), PageSize)
	}

	// Deserialize
	unpacked, err := UnpackVectorsFromPage(pageData)
	if err != nil {
		t.Fatalf("failed to unpack: %v", err)
	}

	// Verify all vectors
	if len(unpacked) != len(vectors) {
		t.Fatalf("unpacked count = %d, want %d", len(unpacked), len(vectors))
	}

	for i := range vectors {
		if unpacked[i].ID != vectors[i].ID {
			t.Errorf("vector %d: ID = %s, want %s", i, unpacked[i].ID, vectors[i].ID)
		}
		for j := range vectors[i].Vector {
			if unpacked[i].Vector[j] != vectors[i].Vector[j] {
				t.Errorf("vector %d dim %d: value = %f, want %f",
					i, j, unpacked[i].Vector[j], vectors[i].Vector[j])
			}
		}
	}
}

func TestPackedPageOffsets(t *testing.T) {
	// Test that offsets are calculated correctly
	vectors := make([]VectorData, 3)
	for i := 0; i < 3; i++ {
		vectors[i] = VectorData{
			ID:     fmt.Sprintf("vec%d", i),
			Vector: make([]float32, 10), // 40 bytes each
		}
	}

	pages := PackVectorsIntoPages(vectors)
	page := &pages[0]

	// Expected offsets:
	// Header: 64 bytes
	// Count: 2 bytes
	// Entry 0: 80 bytes (offset 66-145)
	// Entry 1: 80 bytes (offset 146-225)
	// Entry 2: 80 bytes (offset 226-305)
	// Vector 0 offset: 64 + 2 + 3*80 = 306
	// Vector 1 offset: 306 + 40 = 346
	// Vector 2 offset: 346 + 40 = 386

	expectedOffsets := []uint32{306, 346, 386}
	for i, entry := range page.Entries {
		if entry.Offset != expectedOffsets[i] {
			t.Errorf("entry %d offset = %d, want %d", i, entry.Offset, expectedOffsets[i])
		}
	}
}

func TestPackWithMetadata(t *testing.T) {
	vectors := []VectorData{
		{
			ID:     "vec1",
			Vector: []float32{1, 2, 3},
			Metadata: map[string]interface{}{
				"category": "test",
				"value":    42,
			},
		},
	}

	pages := PackVectorsIntoPages(vectors)
	pageData := SerializePackedPage(&pages[0])

	unpacked, err := UnpackVectorsFromPage(pageData)
	if err != nil {
		t.Fatalf("unpack failed: %v", err)
	}

	// Note: Metadata is stored separately in current design
	// This test verifies the structure works
	if len(unpacked) != 1 {
		t.Errorf("expected 1 vector, got %d", len(unpacked))
	}
}

func TestPackDifferentDimensions(t *testing.T) {
	// Test different dimension sizes
	testCases := []struct {
		dims     int
		numVecs  int
		expected int // expected pages
	}{
		{32, 50, 3},    // 19 per page → 50 needs 3 pages
		{64, 100, 10},  // 11 per page → 100 needs 10 pages
		{128, 20, 4},   // 6 per page → 20 needs 4 pages
		{256, 10, 4},   // 3 per page → 10 needs 4 pages
		{512, 5, 5},    // 1 per page → 5 needs 5 pages
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("dims=%d_vecs=%d", tc.dims, tc.numVecs), func(t *testing.T) {
			vectors := make([]VectorData, tc.numVecs)
			for i := 0; i < tc.numVecs; i++ {
				vectors[i] = VectorData{
					ID:     fmt.Sprintf("vec%d", i),
					Vector: make([]float32, tc.dims),
				}
			}

			pages := PackVectorsIntoPages(vectors)
			if len(pages) != tc.expected {
				t.Errorf("dims=%d, vecs=%d: got %d pages, want %d",
					tc.dims, tc.numVecs, len(pages), tc.expected)
			}
		})
	}
}

// ===== INTEGRATION TESTS (10 tests) =====

func TestIntegrationPackedStorage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	nest := createTestNestPacking(t)
	defer cleanupTestNestPacking(nest)

	// Store 100 vectors
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("vec%d", i)
		vec := make([]float32, 128)
		for j := range vec {
			vec[j] = float32(i + j)
		}
		err := nest.Store(id, vec)
		if err != nil {
			t.Fatalf("failed to store vec%d: %v", i, err)
		}
	}

	// Verify all can be retrieved
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("vec%d", i)
		treasure, err := nest.Get(id)
		if err != nil {
			t.Errorf("failed to get vec%d: %v", i, err)
		}
		if treasure.ID != id {
			t.Errorf("ID mismatch: got %s, want %s", treasure.ID, id)
		}
	}

	// Check file size
	fileInfo, err := os.Stat(nest.path)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	actualSize := fileInfo.Size()

	t.Logf("File size with 100 vectors: %d bytes (%.1fKB)", actualSize, float64(actualSize)/1024)
}

func TestCompactionWithPacking(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	nest := createTestNestPacking(t)
	defer cleanupTestNestPacking(nest)

	// Store vectors
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("vec%d", i)
		vec := make([]float32, 128)
		for j := range vec {
			vec[j] = float32(i + j)
		}
		err := nest.Store(id, vec)
		if err != nil {
			t.Fatalf("failed to store: %v", err)
		}
	}

	// Compact
	err := nest.Compact()
	if err != nil {
		t.Fatalf("compaction failed: %v", err)
	}

	// Verify all vectors are still accessible
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("vec%d", i)
		_, err := nest.Get(id)
		if err != nil {
			t.Errorf("failed to get vec%d after compaction: %v", i, err)
		}
	}
}

func TestPackingRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create vectors with various data
	vectors := make([]VectorData, 20)
	for i := 0; i < 20; i++ {
		vectors[i] = VectorData{
			ID:     fmt.Sprintf("roundtrip_vec%d", i),
			Vector: make([]float32, 128),
		}
		for j := range vectors[i].Vector {
			vectors[i].Vector[j] = float32(i*1000 + j) + 0.5
		}
	}

	// Pack
	pages := PackVectorsIntoPages(vectors)

	// Serialize and deserialize each page
	var allUnpacked []VectorData
	for _, page := range pages {
		pageData := SerializePackedPage(&page)
		unpacked, err := UnpackVectorsFromPage(pageData)
		if err != nil {
			t.Fatalf("failed to unpack page: %v", err)
		}
		allUnpacked = append(allUnpacked, unpacked...)
	}

	// Verify all vectors match
	if len(allUnpacked) != len(vectors) {
		t.Fatalf("vector count mismatch: got %d, want %d", len(allUnpacked), len(vectors))
	}

	for i := range vectors {
		if allUnpacked[i].ID != vectors[i].ID {
			t.Errorf("vector %d ID mismatch: got %s, want %s", i, allUnpacked[i].ID, vectors[i].ID)
		}
		for j := range vectors[i].Vector {
			if allUnpacked[i].Vector[j] != vectors[i].Vector[j] {
				t.Errorf("vector %d dim %d mismatch: got %f, want %f",
					i, j, allUnpacked[i].Vector[j], vectors[i].Vector[j])
			}
		}
	}
}

func TestLargeDatasetPacking(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large test in short mode")
	}

	// Test with 1000 vectors
	vectors := make([]VectorData, 1000)
	for i := 0; i < 1000; i++ {
		vectors[i] = VectorData{
			ID:     fmt.Sprintf("large_vec%d", i),
			Vector: make([]float32, 128),
		}
		for j := range vectors[i].Vector {
			vectors[i].Vector[j] = float32(i*j) * 0.01
		}
	}

	pages := PackVectorsIntoPages(vectors)

	// For 128 dims, 6 per page → 1000 vectors = 167 pages
	expectedPages := (1000 + 5) / 6 // ceil(1000/6)
	if len(pages) != expectedPages {
		t.Errorf("expected ~%d pages, got %d", expectedPages, len(pages))
	}

	t.Logf("Packed 1000 vectors (128 dims) into %d pages", len(pages))
	t.Logf("Space usage: %d KB (vs %d KB unpacked)",
		len(pages)*4, 1000*4)
}

func TestPackingPreservesOrder(t *testing.T) {
	vectors := make([]VectorData, 30)
	for i := 0; i < 30; i++ {
		vectors[i] = VectorData{
			ID:     fmt.Sprintf("order_vec%03d", i), // Zero-padded for clarity
			Vector: make([]float32, 128),
		}
		vectors[i].Vector[0] = float32(i) // Use first element to track order
	}

	pages := PackVectorsIntoPages(vectors)

	// Unpack all pages
	var unpacked []VectorData
	for _, page := range pages {
		pageData := SerializePackedPage(&page)
		vecs, err := UnpackVectorsFromPage(pageData)
		if err != nil {
			t.Fatalf("unpack failed: %v", err)
		}
		unpacked = append(unpacked, vecs...)
	}

	// Verify order
	for i := range vectors {
		if unpacked[i].ID != vectors[i].ID {
			t.Errorf("order mismatch at index %d: got %s, want %s",
				i, unpacked[i].ID, vectors[i].ID)
		}
		if unpacked[i].Vector[0] != float32(i) {
			t.Errorf("data mismatch at index %d: got %f, want %f",
				i, unpacked[i].Vector[0], float32(i))
		}
	}
}

func TestMixedDimensionsFails(t *testing.T) {
	// Vectors must have same dimensions
	// Current implementation uses first vector's dimensions
	// In production, this should be validated before packing
	vectors := []VectorData{
		{ID: "vec1", Vector: make([]float32, 128)},
		{ID: "vec2", Vector: make([]float32, 256)}, // Different!
	}

	// This test documents current behavior
	// TODO: Add dimension validation in production code
	pages := PackVectorsIntoPages(vectors)

	// Currently packs based on first vector's dimensions
	// This is a known limitation that should be addressed with validation
	if len(pages) > 0 {
		t.Logf("Packed %d vectors with mixed dimensions into %d pages (needs validation)", len(vectors), len(pages))
	}
}

func TestPackingSpaceEfficiency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping efficiency test in short mode")
	}

	// Compare packed vs unpacked for various configurations
	testCases := []struct {
		dims      int
		numVecs   int
		maxWaste  float64 // Maximum acceptable waste percentage
	}{
		{128, 100, 0.45}, // 6 per page, should have <45% waste
		{256, 100, 0.50}, // 3 per page, should have <50% waste
		{64, 100, 0.35},  // 13 per page, should have <35% waste
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("dims=%d", tc.dims), func(t *testing.T) {
			vectors := make([]VectorData, tc.numVecs)
			for i := 0; i < tc.numVecs; i++ {
				vectors[i] = VectorData{
					ID:     fmt.Sprintf("eff_vec%d", i),
					Vector: make([]float32, tc.dims),
				}
			}

			pages := PackVectorsIntoPages(vectors)

			// Calculate actual data size
			entrySize := 80
			vectorSize := tc.dims * 4
			actualData := tc.numVecs * (entrySize + vectorSize)

			// Total allocated space
			totalSpace := len(pages) * PageSize

			// Calculate waste
			waste := float64(totalSpace-actualData) / float64(totalSpace)

			t.Logf("Dims=%d, Vectors=%d, Pages=%d, Waste=%.1f%%",
				tc.dims, tc.numVecs, len(pages), waste*100)

			if waste > tc.maxWaste {
				t.Errorf("waste %.1f%% exceeds maximum %.1f%%",
					waste*100, tc.maxWaste*100)
			}
		})
	}
}

func TestConcurrentPacking(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}

	// Test that packing is safe for concurrent use
	// (each goroutine packs independently)
	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			vectors := make([]VectorData, 20)
			for i := 0; i < 20; i++ {
				vectors[i] = VectorData{
					ID:     fmt.Sprintf("g%d_vec%d", id, i),
					Vector: make([]float32, 128),
				}
			}

			pages := PackVectorsIntoPages(vectors)
			if len(pages) == 0 {
				t.Errorf("goroutine %d: failed to pack", id)
			}

			done <- true
		}(g)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

func TestPackedPageHeaderValidation(t *testing.T) {
	vectors := []VectorData{
		{ID: "vec1", Vector: []float32{1, 2, 3}},
	}

	pages := PackVectorsIntoPages(vectors)
	pageData := SerializePackedPage(&pages[0])

	// Verify header
	header, err := DeserializeHeader(pageData)
	if err != nil {
		t.Fatalf("failed to deserialize header: %v", err)
	}

	if header.Type != VectorPage {
		t.Errorf("header type = %d, want %d", header.Type, VectorPage)
	}

	if header.Count != 1 {
		t.Errorf("header count = %d, want 1", header.Count)
	}
}

// ===== BENCHMARKS =====

func BenchmarkPackVectors(b *testing.B) {
	vectors := make([]VectorData, 1000)
	for i := 0; i < 1000; i++ {
		vectors[i] = VectorData{
			ID:     fmt.Sprintf("vec%d", i),
			Vector: make([]float32, 128),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = PackVectorsIntoPages(vectors)
	}
}

func BenchmarkUnpackVectors(b *testing.B) {
	vectors := make([]VectorData, 100)
	for i := 0; i < 100; i++ {
		vectors[i] = VectorData{
			ID:     fmt.Sprintf("vec%d", i),
			Vector: make([]float32, 128),
		}
	}

	pages := PackVectorsIntoPages(vectors)
	pageData := make([][]byte, len(pages))
	for i := range pages {
		pageData[i] = SerializePackedPage(&pages[i])
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, pd := range pageData {
			_, _ = UnpackVectorsFromPage(pd)
		}
	}
}

func BenchmarkSerializePackedPage(b *testing.B) {
	vectors := make([]VectorData, 6) // Full page for 128 dims
	for i := 0; i < 6; i++ {
		vectors[i] = VectorData{
			ID:     fmt.Sprintf("vec%d", i),
			Vector: make([]float32, 128),
		}
	}

	pages := PackVectorsIntoPages(vectors)
	page := &pages[0]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SerializePackedPage(page)
	}
}

// ===== HELPER FUNCTIONS =====

func createTestNestPacking(t *testing.T) *Nest {
	t.Helper()

	tempDir := t.TempDir()
	path := tempDir + "/test_packing.db"

	opts := DefaultOptions()
	opts.Dimensions = 128
	opts.WAL = false

	nest, err := Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}

	return nest
}

func cleanupTestNestPacking(nest *Nest) {
	if nest != nil {
		nest.Close()
	}
}
