package magpie

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// Type aliases for compatibility
type TxID = uint64
type VersionNum = uint64
type VectorVersion = MVCCVersion

// Transaction states
const (
	TxActive     = 0
	TxCommitting = 1
	TxCommitted  = 2
	TxAborted    = 3
)

// MVCCVersion represents a version of a vector in the MVCC system
type MVCCVersion struct {
	ID          string                 // Vector ID
	Vector      []float32              // Vector data
	Metadata    map[string]interface{} // Metadata
	Version     uint64                 // Version number (transaction ID that created it)
	CreatedByTx uint64                 // Transaction ID that created this version
	DeletedByTx uint64                 // Transaction ID that deleted this version (0 if not deleted)
	NextVersion *MVCCVersion           // Pointer to next older version
}

// MVCCTransaction represents an active MVCC transaction
type MVCCTransaction struct {
	ID           uint64                  // Unique transaction ID
	SnapshotTxID uint64                  // Snapshot timestamp (last committed tx at start)
	State        int                     // Transaction state
	ReadSet      map[string]uint64       // Read set for conflict detection (ID -> version)
	WriteSet     map[string]*MVCCVersion // Write set (uncommitted writes)
	mu           sync.Mutex              // Protects transaction state
}

// MVCCManager manages MVCC transactions and version chains
type MVCCManager struct {
	// Transaction ID counter (monotonically increasing)
	txIDCounter uint64

	// Last committed transaction ID
	lastCommittedTx uint64

	// Version chains: ID -> linked list of versions (newest first)
	versionChains map[string]*MVCCVersion

	// Active transactions
	activeTxs map[uint64]*MVCCTransaction

	// Locks
	mu sync.RWMutex // Protects version chains and active transactions
}

// NewMVCCManager creates a new MVCC manager
func NewMVCCManager() *MVCCManager {
	return &MVCCManager{
		txIDCounter:     0,
		lastCommittedTx: 0,
		versionChains:   make(map[string]*MVCCVersion),
		activeTxs:       make(map[uint64]*MVCCTransaction),
	}
}

// BeginTx starts a new MVCC transaction
func (m *MVCCManager) BeginTx() *MVCCTransaction {
	// Allocate new transaction ID
	txID := atomic.AddUint64(&m.txIDCounter, 1)

	// Get snapshot timestamp (last committed transaction)
	snapshotTxID := atomic.LoadUint64(&m.lastCommittedTx)

	tx := &MVCCTransaction{
		ID:           txID,
		SnapshotTxID: snapshotTxID,
		State:        TxActive,
		ReadSet:      make(map[string]uint64),
		WriteSet:     make(map[string]*MVCCVersion),
	}

	// Register transaction
	m.mu.Lock()
	m.activeTxs[txID] = tx
	m.mu.Unlock()

	return tx
}

// CreateVersion creates a new version (not yet visible)
func (m *MVCCManager) CreateVersion(txID uint64, id string, vector []float32, metadata map[string]interface{}) *MVCCVersion {
	// Make copies to avoid shared data issues
	vectorCopy := make([]float32, len(vector))
	copy(vectorCopy, vector)

	var metaCopy map[string]interface{}
	if metadata != nil {
		metaCopy = make(map[string]interface{})
		for k, v := range metadata {
			metaCopy[k] = v
		}
	}

	return &MVCCVersion{
		ID:          id,
		Vector:      vectorCopy,
		Metadata:    metaCopy,
		Version:     txID,
		CreatedByTx: txID,
		DeletedByTx: 0,
		NextVersion: nil,
	}
}

// GetVisibleVersion returns the version visible to the given transaction
// Returns a deep copy to prevent race conditions when the version chain is modified.
// Related to Issue #1: https://github.com/storo/magpieDB/issues/1
func (m *MVCCManager) GetVisibleVersion(id string, tx *MVCCTransaction) *MVCCVersion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Get version chain for this ID
	chain := m.versionChains[id]
	if chain == nil {
		return nil
	}

	// Walk chain to find visible version
	for version := chain; version != nil; version = version.NextVersion {
		// Version is visible if:
		// 1. Created before our snapshot
		// 2. Not deleted before our snapshot
		if version.CreatedByTx <= tx.SnapshotTxID {
			if version.DeletedByTx == 0 || version.DeletedByTx > tx.SnapshotTxID {
				// Return a deep copy to avoid race conditions
				return m.copyVersion(version)
			}
		}
	}

	return nil
}

// deepCopyMetadata creates a deep copy of metadata to prevent shared memory issues
// This handles nested maps and slices recursively
func deepCopyMetadata(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}

	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = deepCopyValue(v)
	}
	return dst
}

// deepCopyValue recursively copies a value, handling maps, slices, and primitives
func deepCopyValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case map[string]interface{}:
		// Recursively copy nested maps
		copied := make(map[string]interface{}, len(val))
		for k, v := range val {
			copied[k] = deepCopyValue(v)
		}
		return copied

	case []interface{}:
		// Copy slices
		copied := make([]interface{}, len(val))
		for i, item := range val {
			copied[i] = deepCopyValue(item)
		}
		return copied

	case []string:
		// Copy string slices
		copied := make([]string, len(val))
		copy(copied, val)
		return copied

	case []int:
		// Copy int slices
		copied := make([]int, len(val))
		copy(copied, val)
		return copied

	case []float64:
		// Copy float64 slices
		copied := make([]float64, len(val))
		copy(copied, val)
		return copied

	case []float32:
		// Copy float32 slices
		copied := make([]float32, len(val))
		copy(copied, val)
		return copied

	default:
		// For primitive types (string, int, bool, float64, etc.), direct assignment is safe
		// as they are copied by value in Go
		return v
	}
}

// copyVersion creates a deep copy of an MVCCVersion
// This prevents race conditions when returning versions from the live chain
func (m *MVCCManager) copyVersion(v *MVCCVersion) *MVCCVersion {
	if v == nil {
		return nil
	}

	// Copy vector data
	vectorCopy := make([]float32, len(v.Vector))
	copy(vectorCopy, v.Vector)

	// Deep copy metadata (including nested structures)
	var metadataCopy map[string]interface{}
	if v.Metadata != nil {
		metadataCopy = deepCopyMetadata(v.Metadata)
	}

	// Return copy without NextVersion pointer (isolate from live chain)
	return &MVCCVersion{
		ID:          v.ID,
		Vector:      vectorCopy,
		Metadata:    metadataCopy,
		Version:     v.Version,
		CreatedByTx: v.CreatedByTx,
		DeletedByTx: v.DeletedByTx,
		NextVersion: nil, // Important: don't link to live chain
	}
}

// GetVersionChain returns the version chain for a given ID
func (m *MVCCManager) GetVersionChain(id string) []*MVCCVersion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var versions []*MVCCVersion
	for version := m.versionChains[id]; version != nil; version = version.NextVersion {
		versions = append(versions, version)
	}
	return versions
}

// AddVersion adds a new version to the version chain (makes it visible)
func (m *MVCCManager) AddVersion(id string, version *MVCCVersion) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Add to front of chain (newest first)
	version.NextVersion = m.versionChains[id]
	m.versionChains[id] = version
}

// GetLatestVersion returns the most recent version (for validation)
func (m *MVCCManager) GetLatestVersion(id string) *MVCCVersion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.versionChains[id]
}

// CommitTx commits a transaction and updates lastCommittedTx
func (m *MVCCManager) CommitTx(tx *MVCCTransaction) error {
	tx.mu.Lock()
	tx.State = TxCommitted
	tx.mu.Unlock()

	// Update last committed transaction ID
	for {
		current := atomic.LoadUint64(&m.lastCommittedTx)
		if tx.ID > current {
			if atomic.CompareAndSwapUint64(&m.lastCommittedTx, current, tx.ID) {
				break
			}
		} else {
			break
		}
	}

	// Remove from active transactions
	m.mu.Lock()
	delete(m.activeTxs, tx.ID)
	m.mu.Unlock()

	return nil
}

// AbortTx aborts a transaction
func (m *MVCCManager) AbortTx(tx *MVCCTransaction) {
	tx.mu.Lock()
	tx.State = TxAborted
	tx.mu.Unlock()

	// Remove from active transactions
	m.mu.Lock()
	delete(m.activeTxs, tx.ID)
	m.mu.Unlock()
}

// ValidateTransaction checks for conflicts using optimistic concurrency control
// Detects read-write conflicts (serializability) and write-write conflicts on same snapshot
func (m *MVCCManager) ValidateTransaction(tx *MVCCTransaction) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check read-write conflicts
	// If we read a version that was later overwritten, we have a conflict
	for id, readVersion := range tx.ReadSet {
		latest := m.versionChains[id]
		if latest != nil && latest.Version != readVersion {
			// The version we read has been superseded
			// Only fail if it was updated after our snapshot
			if latest.CreatedByTx > tx.SnapshotTxID {
				return fmt.Errorf("read-write conflict on %s: read version %d, current version %d",
					id, readVersion, latest.Version)
			}
		}
	}

	// Check write-write conflicts for blind writes
	// Allow concurrent writes to different vectors, but detect conflicts on same vector
	for id := range tx.WriteSet {
		// Skip if we read this vector (already checked above)
		if _, wasRead := tx.ReadSet[id]; wasRead {
			continue
		}

		// Get current latest version
		latest := m.versionChains[id]

		if latest != nil {
			// Check if written by a transaction that committed after we started
			// This prevents lost updates even for blind writes
			if latest.CreatedByTx > tx.SnapshotTxID {
				// Another transaction modified this vector after we started
				// This is a write-write conflict
				return fmt.Errorf("write-write conflict on %s: version %d > snapshot %d",
					id, latest.CreatedByTx, tx.SnapshotTxID)
			}
		}
	}

	return nil
}

// GarbageCollect removes old versions that are no longer visible to any transaction
func (m *MVCCManager) GarbageCollect() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find minimum active snapshot
	minSnapshot := atomic.LoadUint64(&m.lastCommittedTx)
	for _, tx := range m.activeTxs {
		if tx.SnapshotTxID < minSnapshot {
			minSnapshot = tx.SnapshotTxID
		}
	}

	// Remove versions older than minimum snapshot
	for id, chain := range m.versionChains {
		newChain := m.gcVersionChain(chain, minSnapshot)
		if newChain == nil {
			delete(m.versionChains, id)
		} else {
			m.versionChains[id] = newChain
		}
	}
}

// gcVersionChain garbage collects a version chain
func (m *MVCCManager) gcVersionChain(chain *MVCCVersion, minSnapshot uint64) *MVCCVersion {
	if chain == nil {
		return nil
	}

	// Keep versions that might be visible to active transactions
	var head, tail *MVCCVersion

	for version := chain; version != nil; version = version.NextVersion {
		// Keep if created before or at minSnapshot
		if version.CreatedByTx <= minSnapshot {
			newVersion := *version
			newVersion.NextVersion = nil

			if head == nil {
				head = &newVersion
				tail = head
			} else {
				tail.NextVersion = &newVersion
				tail = &newVersion
			}

			// Only keep one old version for each ID
			break
		} else {
			// Keep recent versions
			newVersion := *version
			newVersion.NextVersion = nil

			if head == nil {
				head = &newVersion
				tail = head
			} else {
				tail.NextVersion = &newVersion
				tail = &newVersion
			}
		}
	}

	return head
}

// GetActiveTransactionCount returns the number of active transactions
func (m *MVCCManager) GetActiveTransactionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.activeTxs)
}

// GetVersionCount returns the total number of versions across all chains
func (m *MVCCManager) GetVersionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, chain := range m.versionChains {
		for v := chain; v != nil; v = v.NextVersion {
			count++
		}
	}
	return count
}

// VersionCount is an alias for GetVersionCount
func (m *MVCCManager) VersionCount() int {
	return m.GetVersionCount()
}

// TxManager manages MVCC transactions
type TxManager struct {
	mvcc       *MVCCManager
	nest       *Nest
	commitLock sync.Mutex // Serialize commits to prevent write skew
}

// NewTxManager creates a new transaction manager
func NewTxManager(nest *Nest, mvcc *MVCCManager) *TxManager {
	return &TxManager{
		mvcc: mvcc,
		nest: nest,
	}
}

// Begin starts a new MVCC transaction
func (tm *TxManager) Begin() (*Tx, error) {
	mvccTx := tm.mvcc.BeginTx()

	tx := &Tx{
		nest:     tm.nest,
		mvccTx:   mvccTx,
		writes:   make([]WriteOp, 0),
		snapshot: nil, // Use MVCC visibility instead
		active:   true,
		tm:       tm,
	}

	return tx, nil
}

// IsVisible checks if a version is visible to a transaction (Snapshot Isolation)
func (m *MVCCManager) IsVisible(v *MVCCVersion, tx *MVCCTransaction) bool {
	// Created after snapshot
	if v.CreatedByTx > tx.SnapshotTxID {
		return false
	}
	// Deleted before or at snapshot
	if v.DeletedByTx != 0 && v.DeletedByTx <= tx.SnapshotTxID {
		return false
	}
	return true
}

// CommitVersion marks a version as committed (no-op, version is committed when added)
func (m *MVCCManager) CommitVersion(v *MVCCVersion, txID uint64) {
	// Version is already visible once added to chain
	// This is a no-op for compatibility with tests
}

// DeleteVersion marks the latest version of a vector as deleted
func (m *MVCCManager) DeleteVersion(id string, txID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	chain := m.versionChains[id]
	if chain == nil {
		return ErrVectorNotFound
	}

	// Mark latest version as deleted
	if chain.DeletedByTx != 0 {
		return ErrVersionDeleted
	}

	chain.DeletedByTx = txID
	return nil
}

// AbortTxByID aborts a transaction by TxID
func (m *MVCCManager) AbortTxByID(txID uint64) error {
	m.mu.Lock()
	tx, exists := m.activeTxs[txID]
	m.mu.Unlock()

	if !exists {
		return ErrTxNotActive
	}

	// Call the existing AbortTx method with the transaction
	m.AbortTx(tx)
	return nil
}

// GetTx retrieves a transaction by ID
func (m *MVCCManager) GetTx(txID uint64) (*MVCCTransaction, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tx, exists := m.activeTxs[txID]
	return tx, exists
}

// getMinActiveSnapshot returns the minimum snapshot timestamp across all active transactions
// This is used for garbage collection to ensure we don't delete versions still needed
// MUST be called with lock held (RLock or Lock)
func (m *MVCCManager) getMinActiveSnapshot() uint64 {
	minSnapshot := atomic.LoadUint64(&m.lastCommittedTx)
	for _, tx := range m.activeTxs {
		if tx.SnapshotTxID < minSnapshot {
			minSnapshot = tx.SnapshotTxID
		}
	}
	return minSnapshot
}

// GetGCCandidates returns versions that can be garbage collected
func (m *MVCCManager) GetGCCandidates() []*MVCCVersion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var candidates []*MVCCVersion

	// Find minimum active snapshot (lock already held)
	minSnapshot := m.getMinActiveSnapshot()

	// Find versions older than min snapshot
	for _, chain := range m.versionChains {
		count := 0
		for v := chain; v != nil; v = v.NextVersion {
			count++
			// Keep first visible version, collect older ones
			if count > 1 && v.CreatedByTx < minSnapshot {
				candidates = append(candidates, v)
			}
		}
	}

	return candidates
}
