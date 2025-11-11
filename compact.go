package magpie

import (
	// "encoding/binary"  // Unused import
	"encoding/json"
	"fmt"
	"os"
	// "time"  // Unused import
)

// Compaction reclaims space from deleted vectors and optimizes storage layout.

// Compact reorganizes the database file to reclaim space and improve performance.
// This operation creates a new file with only live vectors, then replaces the original.
func (n *Nest) CompactInternal() error {
	// Note: This is called internally by Compact() which holds the lock

	// 1. Create temporary file
	tempPath := n.path + ".tmp"
	tempFile, err := os.OpenFile(tempPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		tempFile.Close()
		os.Remove(tempPath) // Clean up on failure
	}()

	// 2. Create new header
	newHeader := NewDBHeader(n.header.Dimensions, codeToMetric(n.header.DistanceMetric))
	newHeader.VectorCount = 0
	newHeader.PageCount = 1

	// Write header to temp file
	headerData := SerializeDBHeader(newHeader)
	if _, err := tempFile.WriteAt(headerData, 0); err != nil {
		return fmt.Errorf("failed to write temp header: %w", err)
	}

	// 3. Iterate through all live vectors and write to new file
	liveVectors := n.collectLiveVectors()

	// 4. Initialize temporary storage for new file
	tempStorage := NewStorage()
	if err := tempStorage.Init(tempFile, PageSize); err != nil {
		return fmt.Errorf("failed to initialize temp storage: %w", err)
	}

	// 5. Convert live vectors to VectorData format for packing
	vectorData := make([]VectorData, len(liveVectors))
	for i, vec := range liveVectors {
		vectorData[i] = VectorData{
			ID:       vec.ID,
			Vector:   vec.Vector,
			Metadata: vec.Metadata,
		}
	}

	// 6. Pack vectors into pages (NEW: Multi-vector per page optimization)
	packedPages := PackVectorsIntoPages(vectorData)

	// 7. Write packed pages to storage
	vectorPages := make(map[string]uint64)
	for i, packedPage := range packedPages {
		pageNum, err := tempStorage.AllocatePage()
		if err != nil {
			return fmt.Errorf("failed to allocate page %d: %w", i, err)
		}

		pageBytes := SerializePackedPage(&packedPage)
		if err := tempStorage.WritePage(pageNum, pageBytes); err != nil {
			return fmt.Errorf("failed to write packed page %d: %w", i, err)
		}

		// Map each vector ID to its page number
		for _, entry := range packedPage.Entries {
			vectorID := extractIDString(entry.ID)
			vectorPages[vectorID] = pageNum
		}
	}

	// 8. Handle metadata separately (stored in metadata pages)
	for _, vec := range liveVectors {
		if len(vec.Metadata) > 0 {
			metaJSON, err := json.Marshal(vec.Metadata)
			if err != nil {
				return fmt.Errorf("failed to serialize metadata: %w", err)
			}

			// Store metadata in temp storage
			_, err = storeMetadataInStorage(tempStorage, metaJSON)
			if err != nil {
				return fmt.Errorf("failed to store metadata: %w", err)
			}
		}
	}

	// 6. Build new HNSW index
	distanceFunc := GetDistanceFunc(codeToMetric(newHeader.DistanceMetric))
	newIndex := NewHSNWIndex(int(newHeader.Dimensions), n.index.m, n.index.efConstruct, distanceFunc)

	for _, vec := range liveVectors {
		if err := newIndex.Add(vec.ID, vec.Vector); err != nil {
			return fmt.Errorf("failed to add vector to new index: %w", err)
		}
	}

	// 7. Allocate index root page and persist index
	indexRootPage, err := tempStorage.AllocatePage()
	if err != nil {
		return fmt.Errorf("failed to allocate index root page: %w", err)
	}
	newHeader.IndexRootPage = indexRootPage

	if err := tempStorage.persistIndex(newIndex, indexRootPage); err != nil {
		return fmt.Errorf("failed to persist index: %w", err)
	}

	newHeader.VectorCount = uint64(len(liveVectors))
	newHeader.PageCount = uint64(tempStorage.PageCount())

	// 4. Update new header
	headerData = SerializeDBHeader(newHeader)
	if _, err := tempFile.WriteAt(headerData, 0); err != nil {
		return fmt.Errorf("failed to update temp header: %w", err)
	}

	// 5. Sync temp file
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	// 6. Close current storage (this also closes the file)
	if n.storage != nil {
		if err := n.storage.Close(); err != nil {
			return fmt.Errorf("failed to close storage: %w", err)
		}
		n.storage = nil // Clear storage reference after close
		n.file = nil    // File is owned by storage, set to nil to avoid double-close
	}

	// 7. Close temp file
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// 8. Backup original file
	backupPath := n.path + ".bak"
	if err := os.Rename(n.path, backupPath); err != nil {
		return fmt.Errorf("failed to backup original: %w", err)
	}

	// 9. Rename temp file to original
	if err := os.Rename(tempPath, n.path); err != nil {
		// Try to restore backup
		_ = os.Rename(backupPath, n.path)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// 10. Reopen database
	file, err := os.OpenFile(n.path, os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to reopen database: %w", err)
	}

	n.file = file

	// 11. Reinitialize storage
	n.storage = NewStorage()
	if err := n.storage.Init(file, int64(newHeader.PageCount*PageSize)); err != nil {
		return fmt.Errorf("failed to reinit storage: %w", err)
	}

	// 12. Update header reference and internal structures
	n.header = newHeader
	n.index = newIndex
	n.vectorPages = vectorPages

	// 13. Remove backup
	os.Remove(backupPath)

	return nil
}

// collectLiveVectors collects all live (non-deleted) vectors from the database.
func (n *Nest) collectLiveVectors() []struct {
	ID       string
	Vector   []float32
	Metadata map[string]interface{}
} {
	var vectors []struct {
		ID       string
		Vector   []float32
		Metadata map[string]interface{}
	}

	// Iterate through index to get all live vectors
	for id, node := range n.index.nodes {
		// TODO: Load metadata from storage

		vectors = append(vectors, struct {
			ID       string
			Vector   []float32
			Metadata map[string]interface{}
		}{
			ID:       id,
			Vector:   node.Vector,
			Metadata: nil,
		})
	}

	return vectors
}

// estimateSize estimates the size of the database after compaction.
func (n *Nest) estimateSize() int64 {
	// Header page
	size := int64(PageSize)

	// Vector pages
	vectorCount := int64(n.header.VectorCount)
	dimensions := int64(n.header.Dimensions)
	bytesPerVector := 80 + dimensions*4 // Entry header + vector data
	vectorPages := (vectorCount*bytesPerVector + PageSize - 1) / PageSize
	size += vectorPages * PageSize

	// Index pages (rough estimate)
	// HNSW typically uses ~100 bytes per vector for graph structure
	indexPages := (vectorCount*100 + PageSize - 1) / PageSize
	size += indexPages * PageSize

	// Metadata pages (rough estimate)
	// Assume average 200 bytes per metadata entry
	metadataPages := (vectorCount*200 + PageSize - 1) / PageSize
	size += metadataPages * PageSize

	return size
}

// shouldCompact determines if compaction would be beneficial.
func (n *Nest) shouldCompact() bool {
	// Compact if:
	// 1. File size is > 2x the estimated optimal size
	// 2. More than 20% of vectors have been deleted

	currentSize := n.storage.Size()
	estimatedSize := n.estimateSize()

	if currentSize > estimatedSize*2 {
		return true
	}

	// TODO: Track deleted vector count
	// For now, use a simple heuristic based on page count

	return false
}

// AutoCompact performs compaction if beneficial.
func (n *Nest) AutoCompact() error {
	if !n.shouldCompact() {
		return nil
	}

	return n.Compact()
}

// GetCompactionStats returns statistics about database fragmentation.
func (n *Nest) GetCompactionStats() map[string]interface{} {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return map[string]interface{}{
		"current_size":   n.storage.Size(),
		"estimated_size": n.estimateSize(),
		"vector_count":   n.header.VectorCount,
		"page_count":     n.header.PageCount,
		"fragmentation":  float64(n.storage.Size()) / float64(n.estimateSize()),
	}
}

// vacuum is a lighter-weight operation that just removes deleted entries from pages.
// func (n *Nest) vacuum() error {
// 	// TODO: Implement vacuum
// 	// Walk through pages and remove deleted entries without full compaction
// 	return fmt.Errorf("not implemented")
// }

// calculateFragmentation returns the fragmentation ratio (0.0 to 1.0)
// Fragmentation is calculated as: (current_size - estimated_optimal_size) / current_size
// func (n *Nest) calculateFragmentation() float64 {
// 	currentSize := n.storage.Size()
// 	if currentSize == 0 {
// 		return 0.0
// 	}
//
// 	estimatedSize := n.estimateSize()
// 	if estimatedSize >= currentSize {
// 		return 0.0
// 	}
//
// 	// Fragmentation = wasted space / total space
// 	wastedSpace := currentSize - estimatedSize
// 	return float64(wastedSpace) / float64(currentSize)
// }

// storeMetadataInStorage stores metadata in a storage page
// TODO: Implement proper metadata storage
func storeMetadataInStorage(storage *Storage, data []byte) (uint64, error) {
	// Placeholder implementation
	return 0, nil
}

// writeVectorToStoragePage writes a vector entry to a storage page
// TODO: Implement proper vector writing
// func writeVectorToStoragePage(storage *Storage, pageNum uint64, entry *VectorEntry, data []byte) error {
// 	// Placeholder implementation
// 	return nil
// }
