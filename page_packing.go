package magpie

import (
	"encoding/binary"
	"fmt"
)

// VectorData holds a vector with its ID for packing
type VectorData struct {
	ID       string
	Vector   []float32
	Metadata map[string]interface{}
}

// PackedVectorPage represents a page with multiple vectors
type PackedVectorPage struct {
	Header      PageHeader
	VectorCount uint16
	Entries     []VectorEntry
	VectorData  [][]float32
}

// CalculateVectorsPerPage calculates how many vectors fit in one page
func CalculateVectorsPerPage(dimensions int) int {
	headerSize := 64  // PageHeader
	countSize := 2    // VectorCount field (uint16)
	entrySize := 80   // VectorEntry size
	vectorSize := dimensions * 4 // float32 = 4 bytes

	// Available space after header and count field
	availableSpace := PageSize - headerSize - countSize

	// Space needed per vector (entry + data)
	spacePerVector := entrySize + vectorSize

	// Calculate how many fit
	vectorsPerPage := availableSpace / spacePerVector

	if vectorsPerPage < 1 {
		return 1
	}

	return vectorsPerPage
}

// PackVectorsIntoPages packs multiple vectors into minimal number of pages
func PackVectorsIntoPages(vectors []VectorData) []PackedVectorPage {
	if len(vectors) == 0 {
		return nil
	}

	dimensions := len(vectors[0].Vector)
	vectorsPerPage := CalculateVectorsPerPage(dimensions)
	numPages := (len(vectors) + vectorsPerPage - 1) / vectorsPerPage

	pages := make([]PackedVectorPage, numPages)

	for i := 0; i < numPages; i++ {
		start := i * vectorsPerPage
		end := start + vectorsPerPage
		if end > len(vectors) {
			end = len(vectors)
		}

		pages[i] = createPackedPage(vectors[start:end])
	}

	return pages
}

// createPackedPage creates a single packed page from vectors
func createPackedPage(vectors []VectorData) PackedVectorPage {
	page := PackedVectorPage{
		Header:      *NewPageHeader(VectorPage),
		VectorCount: uint16(len(vectors)),
		Entries:     make([]VectorEntry, len(vectors)),
		VectorData:  make([][]float32, len(vectors)),
	}

	// Calculate offsets for vector data
	// Layout: Header (64) + Count (2) + Entries (80 * n) + Vector Data
	currentOffset := 64 + 2 + len(vectors)*80

	for i, vec := range vectors {
		page.Entries[i] = VectorEntry{
			ID:       packIDString(vec.ID),
			Offset:   uint32(currentOffset),
			Length:   uint16(len(vec.Vector)),
			Flags:    0,
			Reserved: 0,
			Metadata: 0, // Metadata stored separately
		}
		page.VectorData[i] = vec.Vector
		currentOffset += len(vec.Vector) * 4
	}

	page.Header.Count = uint16(len(vectors))

	return page
}

// SerializePackedPage converts PackedVectorPage to bytes
func SerializePackedPage(page *PackedVectorPage) []byte {
	pageData := make([]byte, PageSize)
	offset := 0

	// Serialize header (will update checksum later)
	headerBytes := SerializeHeader(&page.Header)
	copy(pageData[offset:], headerBytes)
	offset += 64

	// Write vector count
	binary.LittleEndian.PutUint16(pageData[offset:], page.VectorCount)
	offset += 2

	// Write vector entries
	for i := range page.Entries {
		entryBytes := packVectorEntry(&page.Entries[i])
		copy(pageData[offset:], entryBytes)
		offset += 80
	}

	// Write vector data
	for _, vec := range page.VectorData {
		vecBytes := SerializeVector(vec)
		copy(pageData[offset:], vecBytes)
		offset += len(vecBytes)
	}

	// Calculate and set checksum
	checksum := CalculatePageChecksum(pageData)
	page.Header.Checksum = checksum

	// Update header with checksum
	headerBytes = SerializeHeader(&page.Header)
	copy(pageData[0:64], headerBytes)

	return pageData
}

// UnpackVectorsFromPage extracts all vectors from a packed page
func UnpackVectorsFromPage(pageData []byte) ([]VectorData, error) {
	if len(pageData) < PageSize {
		return nil, fmt.Errorf("invalid page size: %d", len(pageData))
	}

	// Verify checksum
	if !VerifyPageChecksum(pageData) {
		return nil, fmt.Errorf("page checksum verification failed")
	}

	offset := 64 // Skip header

	// Read vector count
	vectorCount := binary.LittleEndian.Uint16(pageData[offset:])
	offset += 2

	if vectorCount == 0 {
		return nil, nil
	}

	vectors := make([]VectorData, vectorCount)

	// Read all entries first
	entries := make([]VectorEntry, vectorCount)
	for i := uint16(0); i < vectorCount; i++ {
		entry, err := unpackVectorEntry(pageData[offset : offset+80])
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize entry %d: %w", i, err)
		}
		entries[i] = *entry
		offset += 80
	}

	// Read vector data using offsets from entries
	for i := uint16(0); i < vectorCount; i++ {
		vectors[i].ID = extractIDString(entries[i].ID)

		vecOffset := int(entries[i].Offset)
		vecLength := int(entries[i].Length)
		vecBytesLen := vecLength * 4

		if vecOffset+vecBytesLen > len(pageData) {
			return nil, fmt.Errorf("vector %d offset %d + length %d exceeds page size",
				i, vecOffset, vecBytesLen)
		}

		vecBytes := pageData[vecOffset : vecOffset+vecBytesLen]
		vectors[i].Vector = DeserializeVector(vecBytes)
	}

	return vectors, nil
}

// Helper functions for packing/unpacking (reusing existing page.go functions)
// These are already defined in page.go but we ensure they work correctly

// Validation helper
// func validatePackedPage(page *PackedVectorPage) error {
// 	if page.VectorCount == 0 {
// 		return fmt.Errorf("page has no vectors")
// 	}
//
// 	if len(page.Entries) != int(page.VectorCount) {
// 		return fmt.Errorf("entry count mismatch: %d vs %d", len(page.Entries), page.VectorCount)
// 	}
//
// 	if len(page.VectorData) != int(page.VectorCount) {
// 		return fmt.Errorf("vector data count mismatch: %d vs %d", len(page.VectorData), page.VectorCount)
// 	}
//
// 	// Verify all vectors have same dimension
// 	if len(page.VectorData) > 0 {
// 		expectedDim := len(page.VectorData[0])
// 		for i, vec := range page.VectorData {
// 			if len(vec) != expectedDim {
// 				return fmt.Errorf("vector %d has dimension %d, expected %d", i, len(vec), expectedDim)
// 			}
// 		}
// 	}
//
// 	return nil
// }
