package magpie

import (
	"fmt"
)

// Transaction support for atomic batch operations.

// BeginInternal starts a new transaction (internal implementation).
// This is kept for backward compatibility but delegates to MVCC manager.
func (n *Nest) BeginInternal() (*Tx, error) {
	// Delegate to MVCC transaction manager if available
	if n.txMgr != nil {
		return n.txMgr.Begin()
	}

	// Fallback to simple snapshot-based transaction
	snapshot := &Snapshot{
		index:   n.index, // TODO: Create actual copy
		version: n.header.VectorCount,
	}

	tx := &Tx{
		nest:     n,
		writes:   make([]WriteOp, 0),
		snapshot: snapshot,
		active:   true,
	}

	return tx, nil
}

// Store adds a vector to the transaction.
func (tx *Tx) Store(id string, vector []float32, metadata ...map[string]interface{}) error {
	// Delegate to MVCC transaction if available
	if tx.tm != nil && tx.mvccTx != nil {
		// Use MVCC-based store from transaction_mvcc.go
		// The method is already defined there, so we skip adding writes here
		return tx.storeToMVCC(id, vector, metadata...)
	}

	// Fallback to simple transaction
	if !tx.active {
		return fmt.Errorf("transaction not active")
	}

	// Validate ID
	if id == "" || len(id) > 63 {
		return ErrInvalidID
	}

	// Validate dimensions
	if tx.nest.header.Dimensions != 0 && len(vector) != int(tx.nest.header.Dimensions) {
		return ErrInvalidDimensions
	}

	// Extract metadata if provided
	var meta map[string]interface{}
	if len(metadata) > 0 {
		meta = metadata[0]
	}

	// Add to write operations
	tx.writes = append(tx.writes, WriteOp{
		Type:     WALInsert,
		ID:       id,
		Vector:   vector,
		Metadata: meta,
	})

	return nil
}

// storeToMVCC is a helper that calls the MVCC Store implementation
func (tx *Tx) storeToMVCC(id string, vector []float32, metadata ...map[string]interface{}) error {
	if !tx.active {
		return fmt.Errorf("transaction not active")
	}

	// Validate ID
	if id == "" || len(id) > 63 {
		return ErrInvalidID
	}

	// Validate dimensions
	if tx.nest.header.Dimensions != 0 && len(vector) != int(tx.nest.header.Dimensions) {
		return ErrInvalidDimensions
	}

	var meta map[string]interface{}
	if len(metadata) > 0 {
		meta = metadata[0]
	}

	// Create version in write-set
	version := tx.tm.mvcc.CreateVersion(tx.mvccTx.ID, id, vector, meta)

	tx.mvccTx.mu.Lock()
	tx.mvccTx.WriteSet[id] = version
	tx.mvccTx.mu.Unlock()

	return nil
}

// Remove adds a delete operation to the transaction.
func (tx *Tx) Remove(id string) error {
	if !tx.active {
		return fmt.Errorf("transaction not active")
	}

	if tx.nest.closed {
		return ErrDatabaseClosed
	}

	// MVCC path: create delete marker
	if tx.mvccTx != nil && tx.tm != nil {
		// Check if already in write-set (pending modification)
		tx.mvccTx.mu.Lock()
		if version, exists := tx.mvccTx.WriteSet[id]; exists {
			// Already being modified in this transaction
			// Just mark for deletion
			version.DeletedByTx = tx.mvccTx.ID
			tx.mvccTx.mu.Unlock()
			return nil
		}
		tx.mvccTx.mu.Unlock()

		// Get current visible version
		currentVersion := tx.tm.mvcc.GetVisibleVersion(id, tx.mvccTx)
		if currentVersion == nil {
			return ErrVectorNotFound
		}

		// Create delete marker version
		// This is a new version with DeletedByTx set
		deleteVersion := &MVCCVersion{
			ID:          id,
			Version:     tx.mvccTx.ID,
			Vector:      nil, // No vector data needed for delete marker
			Metadata:    nil,
			CreatedByTx: currentVersion.CreatedByTx,
			DeletedByTx: tx.mvccTx.ID, // This marks it as deleted
		}

		tx.mvccTx.mu.Lock()
		tx.mvccTx.WriteSet[id] = deleteVersion
		tx.mvccTx.mu.Unlock()

		return nil
	}

	// Fallback for non-MVCC transactions
	tx.writes = append(tx.writes, WriteOp{
		Type: WALDelete,
		ID:   id,
	})

	return nil
}

// Commit applies all operations in the transaction atomically.
func (tx *Tx) Commit() error {
	// Delegate to MVCC transaction if available
	if tx.tm != nil && tx.mvccTx != nil {
		return tx.commitMVCC()
	}

	// Fallback to simple transaction
	if !tx.active {
		return fmt.Errorf("transaction not active")
	}

	tx.nest.mu.Lock()
	defer tx.nest.mu.Unlock()

	// Write all operations to WAL first
	for _, op := range tx.writes {
		entry := WALEntry{
			Type:     op.Type,
			ID:       op.ID,
			Vector:   op.Vector,
			Metadata: op.Metadata,
		}

		if tx.nest.wal != nil {
			if err := tx.nest.wal.Append(entry); err != nil {
				return fmt.Errorf("failed to write to WAL: %w", err)
			}
		}
	}

	// Flush WAL to ensure durability
	if tx.nest.wal != nil {
		if err := tx.nest.wal.Flush(); err != nil {
			return fmt.Errorf("failed to flush WAL: %w", err)
		}
	}

	// Apply operations to the database
	for _, op := range tx.writes {
		switch op.Type {
		case WALInsert, WALUpdate:
			// Auto-detect dimensions if first vector
			if tx.nest.header.Dimensions == 0 {
				tx.nest.header.Dimensions = uint32(len(op.Vector))
			}

			// Add to index
			if err := tx.nest.index.Add(op.ID, op.Vector); err != nil {
				// If already exists, update instead
				if err.Error() == fmt.Sprintf("vector with ID %s already exists", op.ID) {
					_ = tx.nest.index.Remove(op.ID)
					_ = tx.nest.index.Add(op.ID, op.Vector)
				} else {
					return fmt.Errorf("failed to add vector: %w", err)
				}
			}

			// TODO: Write to storage
			// TODO: Write metadata

			tx.nest.header.VectorCount++

		case WALDelete:
			// Remove from index
			if err := tx.nest.index.Remove(op.ID); err != nil {
				if err != ErrVectorNotFound {
					return fmt.Errorf("failed to remove vector: %w", err)
				}
			}

			// TODO: Mark as deleted in storage

			if tx.nest.header.VectorCount > 0 {
				tx.nest.header.VectorCount--
			}
		}
	}

	// Update header
	if err := tx.nest.writeHeader(); err != nil {
		return fmt.Errorf("failed to update header: %w", err)
	}

	tx.active = false
	return nil
}

// commitMVCC handles MVCC commit with multi-phase protocol
func (tx *Tx) commitMVCC() error {
	if !tx.active {
		return fmt.Errorf("transaction not active")
	}

	tx.mvccTx.mu.Lock()
	tx.mvccTx.State = TxCommitting
	tx.mvccTx.mu.Unlock()

	// Serialize commits (prevent write skew and ensure atomicity)
	tx.tm.commitLock.Lock()
	defer tx.tm.commitLock.Unlock()

	// Phase 1: Validate (detect conflicts)
	if err := tx.tm.mvcc.ValidateTransaction(tx.mvccTx); err != nil {
		_ = tx.Rollback()
		return err
	}

	// Phase 2: Write to WAL
	if err := tx.writeWALMVCC(); err != nil {
		_ = tx.Rollback()
		return err
	}

	// Phase 3: Make versions visible in MVCC
	if err := tx.makeVisibleMVCC(); err != nil {
		_ = tx.Rollback()
		return err
	}

	// Phase 4: Update index
	if err := tx.updateIndexMVCC(); err != nil {
		// Too late to rollback cleanly
		return err
	}

	// Phase 5: Mark transaction as committed
	if err := tx.tm.mvcc.CommitTx(tx.mvccTx); err != nil {
		return err
	}

	tx.active = false

	return nil
}

// writeWALMVCC writes MVCC transaction to WAL
func (tx *Tx) writeWALMVCC() error {
	if tx.nest.wal == nil {
		return nil
	}

	tx.mvccTx.mu.Lock()
	defer tx.mvccTx.mu.Unlock()

	for _, version := range tx.mvccTx.WriteSet {
		entry := WALEntry{
			Type:     WALInsert,
			ID:       version.ID,
			Vector:   version.Vector,
			Metadata: version.Metadata,
		}
		if err := tx.nest.wal.Append(entry); err != nil {
			return fmt.Errorf("WAL append failed: %w", err)
		}
	}

	return tx.nest.wal.Flush()
}

// makeVisibleMVCC makes versions visible in MVCC
func (tx *Tx) makeVisibleMVCC() error {
	tx.mvccTx.mu.Lock()
	defer tx.mvccTx.mu.Unlock()

	for id, version := range tx.mvccTx.WriteSet {
		// Add to in-memory version chain
		tx.tm.mvcc.AddVersion(id, version)

		// Persist to disk if mvccStorage available
		if tx.nest.mvccStorage != nil {
			if err := tx.nest.mvccStorage.StoreVersion(version); err != nil {
				// Critical error - version not persisted
				// Should we rollback in-memory? For now, return error
				return fmt.Errorf("failed to persist version %s (v%d): %w",
					id, version.Version, err)
			}
		}
	}

	return nil
}

// updateIndexMVCC updates HNSW index with committed changes
func (tx *Tx) updateIndexMVCC() error {
	tx.mvccTx.mu.Lock()
	writeSet := make(map[string]*MVCCVersion)
	for k, v := range tx.mvccTx.WriteSet {
		writeSet[k] = v
	}
	tx.mvccTx.mu.Unlock()

	// Acquire write lock on nest for index updates
	tx.nest.mu.Lock()
	defer tx.nest.mu.Unlock()

	for id, version := range writeSet {
		// If this is a delete marker, remove from index
		if version.DeletedByTx != 0 && version.Vector == nil {
			// Remove from index
			if err := tx.nest.index.Remove(id); err != nil {
				// Ignore ErrVectorNotFound - already deleted is OK
				if err != ErrVectorNotFound {
					return fmt.Errorf("failed to remove from index: %w", err)
				}
			}

			// Decrement count if it existed
			if tx.nest.header.VectorCount > 0 {
				tx.nest.header.VectorCount--
			}

			// Note: We keep the version in MVCC for historical queries
			// Storage layer handles the delete marker
			continue
		}

		// For insert/update operations
		if version.Vector != nil {
			// Check if vector already exists
			_, exists := tx.nest.index.Get(id)

			if exists {
				// Update: remove old version and add new
				_ = tx.nest.index.Remove(id)
				if err := tx.nest.index.Add(id, version.Vector); err != nil {
					return fmt.Errorf("failed to update index: %w", err)
				}
			} else {
				// Insert: add new vector
				if err := tx.nest.index.Add(id, version.Vector); err != nil {
					return fmt.Errorf("failed to add to index: %w", err)
				}
				tx.nest.header.VectorCount++
			}

			// Update storage
			if err := tx.nest.storeVector(id, version.Vector, version.Metadata); err != nil {
				return fmt.Errorf("failed to store vector: %w", err)
			}
		}
	}

	// Update header on disk
	if err := tx.nest.writeHeader(); err != nil {
		return fmt.Errorf("failed to update header: %w", err)
	}

	return nil
}

// Rollback cancels the transaction without applying changes.
func (tx *Tx) Rollback() error {
	// Delegate to MVCC transaction if available
	if tx.tm != nil && tx.mvccTx != nil {
		return tx.rollbackMVCC()
	}

	// Fallback to simple transaction
	if !tx.active {
		return fmt.Errorf("transaction not active")
	}

	// Simply discard all write operations
	tx.writes = nil
	tx.active = false

	return nil
}

// rollbackMVCC aborts MVCC transaction
func (tx *Tx) rollbackMVCC() error {
	if !tx.active {
		return nil
	}

	// Mark as aborted in MVCC
	tx.tm.mvcc.AbortTx(tx.mvccTx)

	// Clear write set
	tx.mvccTx.mu.Lock()
	tx.mvccTx.WriteSet = nil
	tx.mvccTx.ReadSet = nil
	tx.mvccTx.mu.Unlock()

	tx.active = false

	return nil
}

// Size returns the number of operations in the transaction.
func (tx *Tx) Size() int {
	return len(tx.writes)
}

// GetOperations returns a copy of the transaction's operations.
func (tx *Tx) GetOperations() []WriteOp {
	ops := make([]WriteOp, len(tx.writes))
	copy(ops, tx.writes)
	return ops
}

// Batch provides a convenient way to execute multiple operations atomically.
// The function f is called with a transaction, and if it returns no error,
// the transaction is committed. Otherwise, it is rolled back.
//
// Performance optimizations:
// - Pre-allocates write-set capacity for large batches
// - Uses object pooling for buffer allocations
//
// Example:
//
//	err := nest.Batch(func(tx *magpie.Tx) error {
//	    tx.Store("doc1", vector1)
//	    tx.Store("doc2", vector2)
//	    tx.Remove("doc3")
//	    return nil
//	})
func (n *Nest) Batch(f func(*Tx) error) error {
	tx, err := n.Begin()
	if err != nil {
		return err
	}

	// Pre-allocate write-set capacity for large batches
	// This reduces repeated map allocations during batch operations
	if tx.mvccTx != nil {
		tx.mvccTx.mu.Lock()
		if tx.mvccTx.WriteSet == nil {
			// Pre-allocate for 1000 operations
			tx.mvccTx.WriteSet = make(map[string]*MVCCVersion, 1000)
		}
		tx.mvccTx.mu.Unlock()
	}

	if err := f(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

// MultiWrite performs multiple Store operations in a single transaction.
// This is more efficient than calling Store multiple times.
func (n *Nest) MultiStore(items []struct {
	ID       string
	Vector   []float32
	Metadata map[string]interface{}
}) error {
	return n.Batch(func(tx *Tx) error {
		for _, item := range items {
			if err := tx.Store(item.ID, item.Vector, item.Metadata); err != nil {
				return err
			}
		}
		return nil
	})
}

// MultiRemove performs multiple Remove operations in a single transaction.
func (n *Nest) MultiRemove(ids []string) error {
	return n.Batch(func(tx *Tx) error {
		for _, id := range ids {
			if err := tx.Remove(id); err != nil {
				return err
			}
		}
		return nil
	})
}

// Snapshot creates a read-only snapshot of the database.
// The snapshot can be used for consistent reads while writes continue.
func (n *Nest) Snapshot() (*Snapshot, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.closed {
		return nil, ErrDatabaseClosed
	}

	// TODO: Create deep copy of index
	snapshot := &Snapshot{
		index:   n.index,
		version: n.header.VectorCount,
	}

	return snapshot, nil
}

// Search performs a search on the snapshot.
func (s *Snapshot) Search(query []float32, k int) []Treasure {
	// TODO: Search using snapshot's index
	return s.index.Search(query, k)
}

// Has checks if a vector exists in the transaction's view
func (tx *Tx) Has(id string) bool {
	if !tx.active {
		return false
	}

	// Check MVCC transaction first
	if tx.mvccTx != nil && tx.tm != nil {
		// Check write-set first (read-your-writes)
		tx.mvccTx.mu.Lock()
		version, inWriteSet := tx.mvccTx.WriteSet[id]
		tx.mvccTx.mu.Unlock()

		if inWriteSet {
			// In write-set, check if it's a delete marker
			return version.DeletedByTx == 0
		}

		// Check MVCC for visible version
		visibleVersion := tx.tm.mvcc.GetVisibleVersion(id, tx.mvccTx)
		return visibleVersion != nil
	}

	// Fallback: check the database directly
	tx.nest.mu.RLock()
	defer tx.nest.mu.RUnlock()
	_, exists := tx.nest.index.nodes[id]
	return exists
}

// Get retrieves a vector from the transaction's view
func (tx *Tx) Get(id string) (*Treasure, error) {
	if !tx.active {
		return nil, fmt.Errorf("transaction not active")
	}

	// Delegate to MVCC transaction if available
	if tx.tm != nil && tx.mvccTx != nil {
		// Check write-set first (read-your-own-writes)
		tx.mvccTx.mu.Lock()
		if version, exists := tx.mvccTx.WriteSet[id]; exists {
			tx.mvccTx.mu.Unlock()
			// Check if it's a delete marker
			if version.DeletedByTx != 0 {
				return nil, ErrVectorNotFound
			}
			return &Treasure{
				ID:       version.ID,
				Vector:   version.Vector,
				Metadata: version.Metadata,
			}, nil
		}
		tx.mvccTx.mu.Unlock()

		// Read from MVCC (snapshot isolation)
		version := tx.tm.mvcc.GetVisibleVersion(id, tx.mvccTx)
		if version == nil {
			return nil, ErrVectorNotFound
		}

		// Track read for conflict detection
		tx.mvccTx.mu.Lock()
		tx.mvccTx.ReadSet[id] = version.Version
		tx.mvccTx.mu.Unlock()

		return &Treasure{
			ID:       version.ID,
			Vector:   version.Vector,
			Metadata: version.Metadata,
		}, nil
	}

	// Fallback: read from database directly
	tx.nest.mu.RLock()
	defer tx.nest.mu.RUnlock()

	node, exists := tx.nest.index.Get(id)
	if !exists {
		return nil, ErrVectorNotFound
	}

	return &Treasure{
		ID:     node.ID,
		Vector: node.Vector,
	}, nil
}

// Find performs a k-NN search within the transaction's view
func (tx *Tx) Find(query []float32, k int, filter ...Filter) ([]Treasure, error) {
	if !tx.active {
		return nil, fmt.Errorf("transaction not active")
	}

	// Use nest's Find method with snapshot isolation
	var f Filter
	if len(filter) > 0 {
		f = filter[0]
	}

	var results []Treasure
	if f != nil {
		results = tx.nest.FindWithFilter(query, k, f)
	} else {
		results = tx.nest.Find(query, k)
	}

	return results, nil
}
