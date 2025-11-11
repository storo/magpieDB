package magpie

import (
	"fmt"
	"os"
)

// Recovery handles crash recovery from WAL replay.

// Recover performs crash recovery by replaying the WAL.
func (n *Nest) Recover() error {
	// 1. Read and validate header
	header, err := n.readHeader()
	if err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	// 2. Validate header
	if !header.Valid() {
		return ErrCorruptedData
	}

	n.header = header

	// 3. Initialize storage
	n.storage = NewStorage()
	if err := n.storage.Init(n.file, int64(header.PageCount*PageSize)); err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	// 3.5. Initialize vectorPages map
	n.vectorPages = make(map[string]uint64)

	// 4. Load index from pages if exists, otherwise rebuild from vectors
	dimensions := int(header.Dimensions)
	distanceMetric := codeToMetric(header.DistanceMetric)
	distanceFunc := GetDistanceFunc(distanceMetric)

	if n.header.IndexRootPage > 0 {
		// Index is persisted, load it from pages
		idx, err := n.loadIndex(n.header.IndexRootPage)
		if err != nil {
			return fmt.Errorf("failed to load index: %w", err)
		}
		n.index = idx

		// Build vectorPages map by scanning vector pages
		// This is needed because the persisted index doesn't include page mappings
		if err := n.buildVectorPageMap(); err != nil {
			return fmt.Errorf("failed to build vector page map: %w", err)
		}
	} else {
		// No persisted index, create empty one
		n.index = NewHSNWIndex(dimensions, 16, 200, distanceFunc)

		// Rebuild from vector pages if any exist
		if err := n.rebuildIndexFromPages(); err != nil {
			return fmt.Errorf("failed to rebuild index: %w", err)
		}
	}

	// 4.5. Load MVCC version chains from disk (BEFORE WAL replay)
	// This ensures version history is available for snapshot isolation
	if n.mvccStorage != nil && n.mvcc != nil {
		// Load version index from disk
		if err := n.mvccStorage.LoadVersionChains(); err != nil {
			return fmt.Errorf("failed to load version chains: %w", err)
		}

		// Populate MVCC manager with loaded versions
		for _, id := range n.mvccStorage.GetAllVectorIDs() {
			versions := n.mvccStorage.GetAllVersions(id)
			for _, version := range versions {
				n.mvcc.AddVersion(id, version)
			}
		}
	}

	// 5. Replay WAL if it exists
	walPath := n.path + ".wal"
	if _, err := os.Stat(walPath); err == nil {
		if err := n.replayWAL(); err != nil {
			return fmt.Errorf("failed to replay WAL: %w", err)
		}
	}

	// 6. Initialize WAL for future writes (if not read-only)
	// Note: WAL initialization will be done in init() when we know the options
	// For now, just replay existing WAL if present

	// 7. MVCC version chains will be rebuilt from WAL replay
	// The MVCC manager is already initialized and will handle version tracking

	// 8. Update header VectorCount to match actual loaded count
	// This is important because the persisted header may be stale if database crashed
	n.header.VectorCount = uint64(n.index.Count())

	return nil
}

// readHeader reads the database header from the file.
func (n *Nest) readHeader() (*Header, error) {
	// Read first page
	headerData := make([]byte, PageSize)
	if _, err := n.file.ReadAt(headerData, 0); err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	// Deserialize header
	header, err := DeserializeDBHeader(headerData)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize header: %w", err)
	}

	return header, nil
}

// writeHeader writes the database header to the file.
func (n *Nest) writeHeader() error {
	// Update PageCount to reflect current storage size
	if n.storage != nil {
		n.header.PageCount = n.storage.PageCount()
	}

	headerData := SerializeDBHeader(n.header)

	if _, err := n.file.WriteAt(headerData, 0); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Sync to disk
	if err := n.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync header: %w", err)
	}

	return nil
}

// buildVectorPageMap scans vector pages to build the ID → page number mapping.
// This is called after loading a persisted index to restore the vectorPages map.
func (n *Nest) buildVectorPageMap() error {
	// Scan all pages looking for vector pages
	pageCount := n.storage.PageCount()

	for pageNum := uint64(1); pageNum < pageCount; pageNum++ {
		// Try to read page
		pageData, err := n.storage.ReadPage(pageNum)
		if err != nil {
			continue // Skip unreadable pages
		}

		// Parse header
		header, err := DeserializeHeader(pageData)
		if err != nil {
			continue // Skip corrupted pages
		}

		// Only process vector pages
		if header.Type != VectorPage {
			continue
		}

		// Read vector entries from page
		entries, err := n.storage.readVectorPage(pageNum)
		if err != nil {
			continue
		}

		// Build mapping for each vector
		for _, entry := range entries {
			id := extractIDString(entry.ID)

			// Skip deleted entries
			if entry.Flags&0x01 != 0 {
				continue
			}

			// Track page mapping
			n.vectorPages[id] = pageNum
		}
	}

	return nil
}

// rebuildIndexFromPages rebuilds the HNSW index from stored vector pages.
func (n *Nest) rebuildIndexFromPages() error {
	// Initialize vectorPages map (will be filled as we scan)
	n.vectorPages = make(map[string]uint64)

	// Scan all pages looking for vector pages
	pageCount := n.storage.PageCount()

	for pageNum := uint64(1); pageNum < pageCount; pageNum++ {
		// Try to read page
		pageData, err := n.storage.ReadPage(pageNum)
		if err != nil {
			continue // Skip unreadable pages
		}

		// Parse header
		header, err := DeserializeHeader(pageData)
		if err != nil {
			continue // Skip corrupted pages
		}

		// Only process vector pages
		if header.Type != VectorPage {
			continue
		}

		// Verify checksum
		if !VerifyPageChecksum(pageData) {
			continue // Skip corrupted pages
		}

		// Read vector entries from page
		entries, err := n.storage.readVectorPage(pageNum)
		if err != nil {
			continue
		}

		// Process each entry
		for _, entry := range entries {
			id := extractIDString(entry.ID)

			// Skip deleted entries (check flags)
			if entry.Flags&0x01 != 0 {
				continue
			}

			// Read vector data
			vectorData, err := n.readVectorData(pageNum, entry.Offset, int(entry.Length)*4)
			if err != nil {
				continue
			}

			// Deserialize vector
			vector := DeserializeVector(vectorData)

			// Add to index
			if err := n.index.Add(id, vector); err != nil {
				// Ignore duplicate errors (vector might be in multiple pages)
				if err.Error() != fmt.Sprintf("vector with ID %s already exists", id) {
					return fmt.Errorf("failed to add vector %s to index: %w", id, err)
				}
			}

			// Track page mapping
			n.vectorPages[id] = pageNum
		}
	}

	return nil
}

// loadIndex loads the HNSW index from disk pages.
func (n *Nest) loadIndex(rootPageNum uint64) (*HSNWIndex, error) {
	// 1. Read all index pages starting from root, following chain
	var data []byte
	pageNum := rootPageNum

	for pageNum > 0 {
		pageData, err := n.storage.ReadPage(pageNum)
		if err != nil {
			return nil, fmt.Errorf("failed to read index page %d: %w", pageNum, err)
		}

		// Verify checksum
		if !VerifyPageChecksum(pageData) {
			return nil, fmt.Errorf("index page %d corrupted (checksum failed)", pageNum)
		}

		// Parse header
		header, err := DeserializeHeader(pageData)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize index page header: %w", err)
		}

		// Verify page type
		if header.Type != IndexPage {
			return nil, fmt.Errorf("expected index page, got type %d", header.Type)
		}

		// Append index data (skip 64-byte header)
		data = append(data, pageData[64:]...)

		// Follow chain to next page
		pageNum = header.NextPage
	}

	// 2. Trim trailing zeros from last page
	// Find the last non-zero byte
	trimmed := len(data)
	for trimmed > 0 && data[trimmed-1] == 0 {
		trimmed--
	}
	data = data[:trimmed]

	// 3. Create empty index with placeholder parameters (will be overwritten by Deserialize)
	dimensions := int(n.header.Dimensions)
	distanceFunc := GetDistanceFunc(codeToMetric(n.header.DistanceMetric))
	idx := NewHSNWIndex(dimensions, 16, 200, distanceFunc)

	// 4. Deserialize index from data
	if err := idx.Deserialize(data); err != nil {
		return nil, fmt.Errorf("failed to deserialize index: %w", err)
	}

	// 5. Restore distance function (not serialized)
	idx.distance = distanceFunc

	return idx, nil
}

// replayWAL replays the write-ahead log to apply pending operations.
func (n *Nest) replayWAL() error {
	// Open WAL file
	walPath := n.path + ".wal"
	if _, err := os.Stat(walPath); os.IsNotExist(err) {
		// No WAL file, nothing to replay
		return nil
	}

	wal := NewWAL()
	if err := wal.Open(walPath); err != nil {
		return fmt.Errorf("failed to open WAL: %w", err)
	}
	defer wal.Close()

	// Read all entries
	entries, err := wal.ReadAll()
	if err != nil {
		return fmt.Errorf("failed to read WAL: %w", err)
	}

	// Replay each entry
	for _, entry := range entries {
		if err := n.applyWALEntry(&entry); err != nil {
			return fmt.Errorf("failed to apply WAL entry %d: %w", entry.Sequence, err)
		}
	}

	// Truncate WAL after successful replay
	if err := wal.Truncate(); err != nil {
		return fmt.Errorf("failed to truncate WAL: %w", err)
	}

	return nil
}

// applyWALEntry applies a single WAL entry to the database.
func (n *Nest) applyWALEntry(entry *WALEntry) error {
	switch entry.Type {
	case WALInsert:
		// Check if vector already exists (already loaded from pages)
		alreadyExists := false
		if err := n.index.Add(entry.ID, entry.Vector); err != nil {
			// Check if it's a duplicate error
			if err.Error() == fmt.Sprintf("vector with ID %s already exists", entry.ID) {
				alreadyExists = true
			} else {
				return err
			}
		}

		// Only write to storage if this is a new vector (not already in pages)
		if !alreadyExists {
			if err := n.storeVector(entry.ID, entry.Vector, entry.Metadata); err != nil {
				return fmt.Errorf("failed to store vector during WAL replay: %w", err)
			}
			n.header.VectorCount++
		}

	case WALDelete:
		// Remove from index
		if err := n.index.Remove(entry.ID); err != nil {
			// Ignore not found errors during replay
			if err != ErrVectorNotFound {
				return err
			}
		}

		// TODO: Mark as deleted in storage

		n.header.VectorCount--

	case WALUpdate:
		// Remove old version
		n.index.Remove(entry.ID)

		// Add new version
		if err := n.index.Add(entry.ID, entry.Vector); err != nil {
			return err
		}

		// Update storage (overwrite existing)
		if err := n.storeVector(entry.ID, entry.Vector, entry.Metadata); err != nil {
			return fmt.Errorf("failed to update vector during WAL replay: %w", err)
		}

	default:
		return fmt.Errorf("unknown WAL entry type: %d", entry.Type)
	}

	return nil
}

// checkpoint creates a checkpoint by flushing all in-memory state to disk.
func (n *Nest) checkpoint() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// 1. Flush WAL
	if n.wal != nil {
		if err := n.wal.Flush(); err != nil {
			return fmt.Errorf("failed to flush WAL: %w", err)
		}
	}

	// 2. Write all vectors to pages
	// TODO: Implement vector page writing

	// 3. Serialize and write index
	// TODO: Implement index serialization

	// 4. Update and write header
	if err := n.writeHeader(); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// 5. Sync storage
	if n.storage != nil {
		if err := n.storage.Sync(); err != nil {
			return fmt.Errorf("failed to sync storage: %w", err)
		}
	}

	// 6. Truncate WAL
	if n.wal != nil {
		if err := n.wal.Truncate(); err != nil {
			return fmt.Errorf("failed to truncate WAL: %w", err)
		}
	}

	return nil
}

// validateDatabase performs integrity checks on the database.
func (n *Nest) validateDatabase() error {
	// TODO: Verify header checksum
	// TODO: Verify page checksums
	// TODO: Check for orphaned pages
	// TODO: Verify index consistency

	return nil
}
