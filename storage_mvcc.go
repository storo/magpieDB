package magpie

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// MVCCStorage manages versioned vector storage on disk
// It extends the base Storage to support multiple versions per vector ID
type MVCCStorage struct {
	storage      *Storage
	versionIndex map[string][]uint64 // VectorID → [pageNums] (newest first)
	mu           sync.RWMutex
}

// NewMVCCStorage creates a new MVCC storage manager
func NewMVCCStorage(storage *Storage) *MVCCStorage {
	return &MVCCStorage{
		storage:      storage,
		versionIndex: make(map[string][]uint64),
	}
}

// StoreVersion stores a vector version to disk
// Allocates a new page, serializes the version, and updates the in-memory index
func (ms *MVCCStorage) StoreVersion(v *MVCCVersion) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Allocate page for version
	pageNum, err := ms.storage.AllocatePage()
	if err != nil {
		return fmt.Errorf("failed to allocate page: %w", err)
	}

	// Serialize version to page
	pageData, err := ms.serializeVersionToPage(v)
	if err != nil {
		return fmt.Errorf("failed to serialize version: %w", err)
	}

	// Write page
	if err := ms.storage.WritePage(pageNum, pageData); err != nil {
		return fmt.Errorf("failed to write version page: %w", err)
	}

	// Update index (insert at front for newest-first ordering)
	ms.versionIndex[v.ID] = append([]uint64{pageNum}, ms.versionIndex[v.ID]...)

	// Re-sort to maintain newest-first order
	ms.sortVersionChain(v.ID)

	return nil
}

// GetAllVersions retrieves all versions for a vector ID
// Returns versions sorted newest first
func (ms *MVCCStorage) GetAllVersions(id string) []*MVCCVersion {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	pageNums, exists := ms.versionIndex[id]
	if !exists {
		return nil
	}

	versions := make([]*VectorVersion, 0, len(pageNums))

	for _, pageNum := range pageNums {
		pageData, err := ms.storage.ReadPage(pageNum)
		if err != nil {
			continue
		}

		version, err := ms.deserializeVersionFromPage(pageData)
		if err != nil {
			continue
		}

		versions = append(versions, version)
	}

	return versions
}

// GetVersion retrieves a specific version by version number
func (ms *MVCCStorage) GetVersion(id string, versionNum uint64) *MVCCVersion {
	versions := ms.GetAllVersions(id)
	for _, v := range versions {
		if v.Version == versionNum {
			return v
		}
	}
	return nil
}

// GetLatestVersion retrieves the newest version for a vector ID
func (ms *MVCCStorage) GetLatestVersion(id string) *MVCCVersion {
	versions := ms.GetAllVersions(id)
	if len(versions) == 0 {
		return nil
	}
	return versions[0] // First is newest
}

// GCVersionsOlderThan removes versions created before the given TxID
// Returns the number of versions removed
// Guarantees to keep at least one version per vector ID
func (ms *MVCCStorage) GCVersionsOlderThan(oldestActiveTx uint64) int {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	removed := 0

	for id, pageNums := range ms.versionIndex {
		newPageNums := make([]uint64, 0, len(pageNums))
		keptCount := 0

		for _, pageNum := range pageNums {
			pageData, err := ms.storage.ReadPage(pageNum)
			if err != nil {
				continue
			}

			version, err := ms.deserializeVersionFromPage(pageData)
			if err != nil {
				continue
			}

			// Keep if:
			// 1. Created after oldest active tx, OR
			// 2. Not deleted yet (might be needed), OR
			// 3. Is the first/only version we're keeping (always keep at least one)
			shouldKeep := version.CreatedByTx >= oldestActiveTx ||
				version.DeletedByTx == 0 ||
				keptCount == 0

			if shouldKeep {
				newPageNums = append(newPageNums, pageNum)
				keptCount++
			} else {
				// Can be GC'd
				_ = ms.storage.FreePage(pageNum)
				removed++
			}
		}

		if len(newPageNums) > 0 {
			ms.versionIndex[id] = newPageNums
		} else {
			delete(ms.versionIndex, id)
		}
	}

	return removed
}

// LoadVersionChains loads version index from disk during recovery
// Scans all pages looking for version pages and rebuilds the in-memory index
func (ms *MVCCStorage) LoadVersionChains() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Clear existing index
	ms.versionIndex = make(map[string][]uint64)

	// Scan all pages looking for version pages
	pageCount := ms.storage.PageCount()

	for pageNum := uint64(1); pageNum < pageCount; pageNum++ {
		pageData, err := ms.storage.ReadPage(pageNum)
		if err != nil {
			continue
		}

		// Check if this is a version page
		header, err := DeserializeHeader(pageData[:64])
		if err != nil {
			continue
		}

		if header.Type != VectorPage {
			continue
		}

		// Verify this is an MVCC version page (has Count=1)
		if header.Count != 1 {
			continue
		}

		// Deserialize version
		version, err := ms.deserializeVersionFromPage(pageData)
		if err != nil {
			continue
		}

		// Add to index
		ms.versionIndex[version.ID] = append(ms.versionIndex[version.ID], pageNum)
	}

	// Sort each chain (newest first)
	for id := range ms.versionIndex {
		ms.sortVersionChain(id)
	}

	return nil
}

// serializeVersionToPage serializes a version to a 4KB page
func (ms *MVCCStorage) serializeVersionToPage(v *MVCCVersion) ([]byte, error) {
	pageData := make([]byte, PageSize)

	// Page header
	header := NewPageHeader(VectorPage)
	header.Count = 1

	offset := 64

	// Version metadata
	binary.LittleEndian.PutUint64(pageData[offset:], uint64(v.Version))
	offset += 8
	binary.LittleEndian.PutUint64(pageData[offset:], uint64(v.CreatedByTx))
	offset += 8
	binary.LittleEndian.PutUint64(pageData[offset:], uint64(v.DeletedByTx))
	offset += 8

	// ID length and data
	idLen := len(v.ID)
	if idLen > 255 {
		return nil, fmt.Errorf("ID too long: %d (max 255)", idLen)
	}
	binary.LittleEndian.PutUint32(pageData[offset:], uint32(idLen))
	offset += 4
	copy(pageData[offset:], []byte(v.ID))
	offset += idLen

	// Vector length and data
	vectorData := SerializeVector(v.Vector)
	vectorLen := len(vectorData)
	if offset+4+vectorLen > PageSize-100 { // Reserve space for metadata and checksum
		return nil, fmt.Errorf("vector too large for page")
	}
	binary.LittleEndian.PutUint32(pageData[offset:], uint32(vectorLen))
	offset += 4
	copy(pageData[offset:], vectorData)
	offset += vectorLen

	// Metadata (JSON)
	if v.Metadata != nil {
		metaBytes, err := json.Marshal(v.Metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal metadata: %w", err)
		}
		if offset+4+len(metaBytes) > PageSize-20 {
			return nil, fmt.Errorf("metadata too large for page")
		}
		binary.LittleEndian.PutUint32(pageData[offset:], uint32(len(metaBytes)))
		offset += 4
		copy(pageData[offset:], metaBytes)
		// offset += len(metaBytes)  // Not used after this
	} else {
		binary.LittleEndian.PutUint32(pageData[offset:], 0)
		// offset += 4  // Not used after this
	}

	// Write header with checksum
	headerData := SerializeHeader(header)
	copy(pageData[0:64], headerData)

	// Calculate and set checksum
	checksum := CalculatePageChecksum(pageData)
	header.Checksum = checksum
	headerData = SerializeHeader(header)
	copy(pageData[0:64], headerData)

	return pageData, nil
}

// deserializeVersionFromPage deserializes a version from a page
func (ms *MVCCStorage) deserializeVersionFromPage(pageData []byte) (*MVCCVersion, error) {
	if len(pageData) != PageSize {
		return nil, fmt.Errorf("invalid page size: %d", len(pageData))
	}

	// Verify checksum
	if !VerifyPageChecksum(pageData) {
		return nil, fmt.Errorf("checksum mismatch")
	}

	offset := 64

	v := &MVCCVersion{}

	// Version metadata
	if offset+24 > len(pageData) {
		return nil, fmt.Errorf("truncated version metadata")
	}
	v.Version = binary.LittleEndian.Uint64(pageData[offset:])
	offset += 8
	v.CreatedByTx = binary.LittleEndian.Uint64(pageData[offset:])
	offset += 8
	v.DeletedByTx = binary.LittleEndian.Uint64(pageData[offset:])
	offset += 8

	// ID
	if offset+4 > len(pageData) {
		return nil, fmt.Errorf("truncated ID length")
	}
	idLen := binary.LittleEndian.Uint32(pageData[offset:])
	offset += 4
	if offset+int(idLen) > len(pageData) {
		return nil, fmt.Errorf("truncated ID data")
	}
	v.ID = string(pageData[offset : offset+int(idLen)])
	offset += int(idLen)

	// Vector
	if offset+4 > len(pageData) {
		return nil, fmt.Errorf("truncated vector length")
	}
	vectorLen := binary.LittleEndian.Uint32(pageData[offset:])
	offset += 4
	if offset+int(vectorLen) > len(pageData) {
		return nil, fmt.Errorf("truncated vector data")
	}
	v.Vector = DeserializeVector(pageData[offset : offset+int(vectorLen)])
	offset += int(vectorLen)

	// Metadata
	if offset+4 > len(pageData) {
		return nil, fmt.Errorf("truncated metadata length")
	}
	metaLen := binary.LittleEndian.Uint32(pageData[offset:])
	offset += 4
	if metaLen > 0 {
		if offset+int(metaLen) > len(pageData) {
			return nil, fmt.Errorf("truncated metadata data")
		}
		v.Metadata = make(map[string]interface{})
		if err := json.Unmarshal(pageData[offset:offset+int(metaLen)], &v.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return v, nil
}

// sortVersionChain sorts a version chain newest first
func (ms *MVCCStorage) sortVersionChain(id string) {
	pageNums := ms.versionIndex[id]
	if len(pageNums) <= 1 {
		return
	}

	// Read versions and sort by version number
	type versionPage struct {
		version uint64
		pageNum uint64
	}

	vps := make([]versionPage, 0, len(pageNums))

	for _, pageNum := range pageNums {
		pageData, err := ms.storage.ReadPage(pageNum)
		if err != nil {
			continue
		}

		// Read version number (at offset 64)
		if len(pageData) < 72 {
			continue
		}
		version := binary.LittleEndian.Uint64(pageData[64:72])
		vps = append(vps, versionPage{version, pageNum})
	}

	// Sort descending (newest first)
	sort.Slice(vps, func(i, j int) bool {
		return vps[i].version > vps[j].version
	})

	// Update index
	sorted := make([]uint64, len(vps))
	for i, vp := range vps {
		sorted[i] = vp.pageNum
	}
	ms.versionIndex[id] = sorted
}

// GetVersionCount returns the total number of versions across all vector IDs
func (ms *MVCCStorage) GetVersionCount() int {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	count := 0
	for _, pageNums := range ms.versionIndex {
		count += len(pageNums)
	}
	return count
}

// GetVectorCount returns the number of unique vector IDs
func (ms *MVCCStorage) GetVectorCount() int {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	return len(ms.versionIndex)
}

// GetAllVectorIDs returns all vector IDs in the storage
func (ms *MVCCStorage) GetAllVectorIDs() []string {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	ids := make([]string, 0, len(ms.versionIndex))
	for id := range ms.versionIndex {
		ids = append(ids, id)
	}
	return ids
}
