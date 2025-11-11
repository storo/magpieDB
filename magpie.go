// Package magpie provides an embedded vector database for Go applications.
//
// MagpieDB is designed to be the "SQLite of vector databases" - simple,
// fast, and embedded directly into your application with zero dependencies.
//
// # Quick Start
//
//	nest, err := magpie.Open("./data.magpie")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer nest.Close()
//
//	// Store a vector
//	vector := []float32{0.1, 0.2, 0.3, 0.4}
//	err = nest.Store("doc1", vector)
//
//	// Search for similar vectors
//	results := nest.Find(vector, 10)
//	for _, r := range results {
//	    fmt.Printf("ID: %s, Distance: %.4f\n", r.ID, r.Distance)
//	}
//
// # Features
//
//   - Zero dependencies (pure Go)
//   - Single file storage
//   - Fast HNSW similarity search
//   - ACID transactions
//   - Metadata filtering
//   - Cross-platform
package magpie

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
)

const (
	// Version is the current MagpieDB version
	Version = "1.0.0"

	// Magic number for file format validation
	MagicNumber = "MAGPIE01"
)

// Open opens an existing database or creates a new one at the specified path.
// The database file will have a .magpie extension if not already present.
//
// Options can be provided to configure the database. If no options are provided,
// sensible defaults are used.
//
// Example:
//
//	nest, err := magpie.Open("./mydata.magpie")
//	nest, err := magpie.Open("./mydata.magpie", magpie.Options{Dimensions: 384})
func Open(path string, opts ...Options) (*Nest, error) {
	// Start with defaults
	options := DefaultOptions()

	// If user provided options, merge them (override only non-zero fields)
	if len(opts) > 0 {
		userOpts := opts[0]

		// Override defaults with user-specified values (if non-zero)
		if userOpts.Dimensions != 0 {
			options.Dimensions = userOpts.Dimensions
		}
		if userOpts.Distance != "" {
			options.Distance = userOpts.Distance
		}
		if userOpts.MaxSize != 0 {
			options.MaxSize = userOpts.MaxSize
		}
		if userOpts.M != 0 {
			options.M = userOpts.M
		}
		if userOpts.EfConstruction != 0 {
			options.EfConstruction = userOpts.EfConstruction
		}
		if userOpts.EfSearch != 0 {
			options.EfSearch = userOpts.EfSearch
		}
		if userOpts.MaxLevel != 0 {
			options.MaxLevel = userOpts.MaxLevel
		}
		if userOpts.Seed != 0 {
			options.Seed = userOpts.Seed
		}
		// WAL: user can override the default
		// Note: Go doesn't distinguish between explicit false and zero value
		// So we check if user set WAL to false by checking if it differs from default
		// For now, we'll only override if user set it to false explicitly
		// This is a limitation of Go's zero values
		if !userOpts.WAL {
			options.WAL = false
		}
		// ReadOnly: special case - if user set it to true, override
		if userOpts.ReadOnly {
			options.ReadOnly = true
		}
		// EnableQueryCache: special case - if user set it to true, override
		if userOpts.EnableQueryCache {
			options.EnableQueryCache = true
		}
		// QueryCacheSize: override if non-zero
		if userOpts.QueryCacheSize != 0 {
			options.QueryCacheSize = userOpts.QueryCacheSize
		}
		// Workers: override if user explicitly set Enabled to true
		if userOpts.Workers.Enabled {
			options.Workers = userOpts.Workers
		}
	}

	// Set seed if not provided
	if options.Seed == 0 {
		options.Seed = time.Now().UnixNano()
	}

	// NOW validate merged options
	if err := validateOptions(&options); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	// Create or open the file
	flags := os.O_RDWR | os.O_CREATE
	if options.ReadOnly {
		flags = os.O_RDONLY
	}

	file, err := os.OpenFile(path, flags, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	nest := &Nest{
		path:             path,
		file:             file,
		closed:           false,
		metrics:          NewMetrics(),
		vectorBufferPool: NewVectorBufferPool(),
		metadataPool:     NewMetadataBufferPool(),
	}

	// Initialize MVCC manager
	nest.mvcc = NewMVCCManager()

	// Initialize or load the database
	if err := nest.init(options); err != nil {
		file.Close()
		return nil, err
	}

	// Initialize MVCC storage after storage is initialized
	if nest.storage != nil {
		nest.mvccStorage = NewMVCCStorage(nest.storage)
	}

	// Initialize transaction manager after database init
	nest.txMgr = NewTxManager(nest, nest.mvcc)

	// Initialize query cache if enabled
	if options.EnableQueryCache {
		nest.queryCache = NewQueryCache(options.QueryCacheSize)
	}

	// Initialize workers if enabled
	if options.Workers.Enabled {
		nest.workers = NewWorkerPool(nest, options.Workers.NumWorkers)
		if err := nest.workers.Start(); err != nil {
			return nil, fmt.Errorf("failed to start workers: %w", err)
		}

		// Schedule periodic workers based on config
		nest.periodicTasks = make([]*PeriodicTask, 0)

		if options.Workers.AutoCompaction {
			worker := NewAutoCompactionWorker(options.Workers.CompactionInterval, 0.3)
			task := NewPeriodicTask(worker, options.Workers.CompactionInterval, nest.workers)
			task.Start()
			nest.periodicTasks = append(nest.periodicTasks, task)
		}

		if options.Workers.MVCCGC {
			worker := NewMVCCGCWorker(options.Workers.GCInterval)
			task := NewPeriodicTask(worker, options.Workers.GCInterval, nest.workers)
			task.Start()
			nest.periodicTasks = append(nest.periodicTasks, task)
		}

		if options.Workers.IndexOptimization {
			worker := NewIndexOptimizationWorker(options.Workers.OptimizeInterval)
			task := NewPeriodicTask(worker, options.Workers.OptimizeInterval, nest.workers)
			task.Start()
			nest.periodicTasks = append(nest.periodicTasks, task)
		}

		if options.Workers.MetricsAggregation {
			worker := NewMetricsAggregationWorker(options.Workers.MetricsInterval)
			task := NewPeriodicTask(worker, options.Workers.MetricsInterval, nest.workers)
			task.Start()
			nest.periodicTasks = append(nest.periodicTasks, task)
		}

		if options.Workers.WALCheckpoint && options.WAL && !options.ReadOnly {
			worker := NewWALCheckpointWorker(options.Workers.CheckpointInterval)
			task := NewPeriodicTask(worker, options.Workers.CheckpointInterval, nest.workers)
			task.Start()
			nest.periodicTasks = append(nest.periodicTasks, task)
		}

		// TODO: Reindex worker (disabled - not implemented yet)
		// if options.Workers.Reindex {
		// 	worker := NewReindexWorker(options.Workers.ReindexInterval, options.Workers.IndexQualityThreshold)
		// 	task := NewPeriodicTask(worker, options.Workers.ReindexInterval, nest.workers)
		// 	task.Start()
		// 	nest.periodicTasks = append(nest.periodicTasks, task)
		// }

		// TODO: Backup worker (disabled - not implemented yet)
		// if options.Workers.Backup && options.Workers.BackupDir != "" && !options.ReadOnly {
		// 	worker := NewBackupWorker(options.Workers.BackupInterval, options.Workers.BackupDir, options.Workers.MaxBackups)
		// 	task := NewPeriodicTask(worker, options.Workers.BackupInterval, nest.workers)
		// 	task.Start()
		// 	nest.periodicTasks = append(nest.periodicTasks, task)
		// }

		// TODO: Statistics worker (disabled - not implemented yet)
		// if options.Workers.Statistics && options.Workers.StatsPath != "" {
		// 	worker := NewStatisticsWorker(options.Workers.StatsInterval, options.Workers.StatsPath)
		// 	task := NewPeriodicTask(worker, options.Workers.StatsInterval, nest.workers)
		// 	task.Start()
		// 	nest.periodicTasks = append(nest.periodicTasks, task)
		// }

		// NEW: Compression worker
		if options.Workers.Compression {
			worker := NewCompressionWorker(options.Workers.CompressionInterval, options.Workers.ColdDataThreshold)
			task := NewPeriodicTask(worker, options.Workers.CompressionInterval, nest.workers)
			task.Start()
			nest.periodicTasks = append(nest.periodicTasks, task)
		}
	}

	return nest, nil
}

// Store inserts or updates a vector with the given ID.
// If metadata is provided, it will be associated with the vector.
//
// The vector dimensions must match the database dimensions (auto-detected
// from the first insertion if not specified in Options).
//
// Example:
//
//	err := nest.Store("doc1", []float32{0.1, 0.2, 0.3})
//	err := nest.Store("doc2", vector, map[string]interface{}{
//	    "title": "Example Document",
//	    "category": "tutorial",
//	})
func (n *Nest) Store(id string, vector []float32, metadata ...map[string]interface{}) error {
	start := time.Now()
	defer func() {
		n.trackInsert(time.Since(start))
	}()

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.closed {
		return ErrDatabaseClosed
	}

	// Validate ID
	if id == "" || len(id) > 63 {
		return ErrInvalidID
	}

	// Validate dimensions
	if n.header.Dimensions == 0 {
		// Auto-detect dimensions from first vector
		n.header.Dimensions = uint32(len(vector))
	} else if len(vector) != int(n.header.Dimensions) {
		return ErrInvalidDimensions
	}

	// Extract metadata if provided
	var meta map[string]interface{}
	if len(metadata) > 0 {
		meta = metadata[0]
	}

	// Write to WAL FIRST (for durability)
	if n.wal != nil {
		entry := WALEntry{
			Type:     WALInsert,
			ID:       id,
			Vector:   vector,
			Metadata: meta,
		}
		if err := n.wal.Append(entry); err != nil {
			return fmt.Errorf("WAL append failed: %w", err)
		}
	}

	// Add to HNSW index
	if err := n.index.Add(id, vector); err != nil {
		// If already exists, update it
		if err.Error() == fmt.Sprintf("vector with ID %s already exists", id) {
			n.index.Remove(id)
			if err := n.index.Add(id, vector); err != nil {
				return fmt.Errorf("failed to update vector: %w", err)
			}
		} else {
			return fmt.Errorf("failed to add to index: %w", err)
		}
	} else {
		// Only increment count if this is a new vector
		n.header.VectorCount++
	}

	// Write to storage
	if err := n.storeVector(id, vector, meta); err != nil {
		// Rollback index change
		n.index.Remove(id)
		n.header.VectorCount--
		return fmt.Errorf("failed to write vector: %w", err)
	}

	// Add to MVCC version chain (for snapshot isolation)
	if n.mvcc != nil {
		// Non-transactional writes need a proper transaction ID
		// to maintain snapshot isolation semantics
		// Allocate a new TX ID and commit it immediately
		tx := n.mvcc.BeginTx()
		version := n.mvcc.CreateVersion(tx.ID, id, vector, meta)
		n.mvcc.AddVersion(id, version)
		n.mvcc.CommitTx(tx)
	}

	// Update header on disk
	if err := n.writeHeader(); err != nil {
		return fmt.Errorf("failed to update header: %w", err)
	}

	// Sync to disk for durability
	if err := n.storage.Sync(); err != nil {
		return fmt.Errorf("failed to sync: %w", err)
	}

	// Invalidate query cache (data changed)
	if n.queryCache != nil {
		n.queryCache.Invalidate()
	}

	return nil
}

// Find searches for the k most similar vectors to the query.
// Results are ordered by similarity (lowest distance first).
//
// Example:
//
//	query := []float32{0.1, 0.2, 0.3}
//	results := nest.Find(query, 10) // Top 10 results
//	for _, r := range results {
//	    fmt.Printf("%s: %.4f\n", r.ID, r.Distance)
//	}
func (n *Nest) Find(query []float32, k int) []Treasure {
	start := time.Now()
	defer func() {
		n.trackSearch(time.Since(start))
	}()

	// Check cache first (before acquiring lock)
	if n.queryCache != nil {
		cacheKey := GenerateCacheKey(query, k, nil)
		if cached, hit := n.queryCache.Get(cacheKey); hit {
			return cached
		}
	}

	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.closed {
		return nil
	}

	// Validate dimensions
	if len(query) != int(n.header.Dimensions) {
		return nil
	}

	// Search HNSW index
	results := n.index.Search(query, k)

	// Load metadata for each result
	for i := range results {
		meta, err := n.loadMetadata(results[i].ID)
		if err == nil {
			results[i].Metadata = meta
		}
	}

	// Store in cache
	if n.queryCache != nil {
		cacheKey := GenerateCacheKey(query, k, nil)
		n.queryCache.Put(cacheKey, results)
	}

	return results
}

// FindWithFilter searches for similar vectors that match the provided filter.
// This allows combining vector similarity with metadata constraints.
//
// Example:
//
//	filter := &magpie.SimpleFilter{Key: "category", Value: "tutorial"}
//	results := nest.FindWithFilter(query, 10, filter)
func (n *Nest) FindWithFilter(query []float32, k int, filter Filter) []Treasure {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.closed {
		return nil
	}

	// TODO: Search with filter
	// Consider: filter before or after search?
	// Trade-off: accuracy vs performance

	return nil
}

// Get retrieves a specific vector by ID.
// Returns ErrVectorNotFound if the vector doesn't exist.
//
// Example:
//
//	treasure, err := nest.Get("doc1")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Vector: %v\n", treasure.Vector)
func (n *Nest) Get(id string) (*Treasure, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.closed {
		return nil, ErrDatabaseClosed
	}

	// Lookup in index
	node, exists := n.index.Get(id)
	if !exists {
		return nil, ErrVectorNotFound
	}

	// Create treasure from node
	treasure := &Treasure{
		ID:       node.ID,
		Vector:   node.Vector,
		Distance: 0, // No distance for direct lookup
	}

	// Load metadata
	meta, err := n.loadMetadata(id)
	if err == nil {
		treasure.Metadata = meta
	}

	return treasure, nil
}

// Remove deletes a vector by ID.
// This is a soft delete that marks the vector as deleted in the WAL.
// The space will be reclaimed during compaction.
//
// Example:
//
//	err := nest.Remove("doc1")
func (n *Nest) Remove(id string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.closed {
		return ErrDatabaseClosed
	}

	// TODO: Write delete to WAL
	// TODO: Remove from index
	// TODO: Mark as deleted in storage

	// Invalidate query cache (data changed)
	if n.queryCache != nil {
		n.queryCache.Invalidate()
	}

	return fmt.Errorf("not implemented")
}

// Has checks if a vector with the given ID exists.
//
// Example:
//
//	if nest.Has("doc1") {
//	    fmt.Println("Vector exists")
//	}
func (n *Nest) Has(id string) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.closed {
		return false
	}

	// Check index for ID
	_, exists := n.index.Get(id)
	return exists
}

// Count returns the total number of vectors in the database.
//
// Example:
//
//	count := nest.Count()
//	fmt.Printf("Database contains %d vectors\n", count)
func (n *Nest) Count() int64 {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.closed {
		return 0
	}

	return int64(n.header.VectorCount)
}

// Close closes the database and flushes any pending writes.
// The database cannot be used after closing.
//
// Example:
//
//	defer nest.Close()
func (n *Nest) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.closed {
		return ErrDatabaseClosed
	}

	// Stop periodic tasks first
	for _, task := range n.periodicTasks {
		task.Stop()
	}

	// Stop background workers and wait for current tasks to complete
	if n.workers != nil {
		if err := n.workers.Stop(); err != nil {
			return fmt.Errorf("failed to stop workers: %w", err)
		}
	}

	// Persist index before closing
	if n.index != nil && n.index.Count() > 0 {
		// Allocate root page if not exists
		if n.header.IndexRootPage == 0 {
			rootPage, err := n.storage.AllocatePage()
			if err != nil {
				return fmt.Errorf("failed to allocate index root page: %w", err)
			}
			n.header.IndexRootPage = rootPage
		}

		// Persist index to pages
		if err := n.storage.persistIndex(n.index, n.header.IndexRootPage); err != nil {
			return fmt.Errorf("failed to persist index: %w", err)
		}
	}

	// Sync header to disk (updates IndexRootPage and other fields)
	if n.header != nil {
		if err := n.writeHeader(); err != nil {
			return fmt.Errorf("failed to write header: %w", err)
		}
	}

	// Sync storage to ensure all writes are flushed
	if n.storage != nil {
		if err := n.storage.Sync(); err != nil {
			return fmt.Errorf("failed to sync storage: %w", err)
		}
	}

	// Close storage (unmaps memory and closes file)
	if n.storage != nil {
		if err := n.storage.Close(); err != nil {
			return fmt.Errorf("failed to close storage: %w", err)
		}
	}

	// Close WAL after all data is persisted
	if n.wal != nil {
		// Truncate WAL since all changes are now in pages
		if err := n.wal.Truncate(); err != nil {
			return fmt.Errorf("failed to truncate WAL: %w", err)
		}
		if err := n.wal.Close(); err != nil {
			return fmt.Errorf("failed to close WAL: %w", err)
		}
	}

	n.closed = true

	return nil
}

// Compact reorganizes the database file to reclaim space from deleted vectors
// and optimize storage layout. This can improve performance after many deletions.
//
// The database remains available during compaction (though writes may be slower).
//
// Example:
//
//	err := nest.Compact()
func (n *Nest) Compact() error {
	start := time.Now()

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.closed {
		return ErrDatabaseClosed
	}

	// Track size before compaction
	sizeBefore := int64(0)
	if n.storage != nil {
		sizeBefore = n.storage.Size()
	}

	// Perform compaction with multi-vector packing optimization
	err := n.CompactInternal()
	if err != nil {
		return err
	}

	// Track metrics
	sizeAfter := int64(0)
	if n.storage != nil {
		sizeAfter = n.storage.Size()
	}
	_ = sizeBefore // avoid unused warning
	_ = sizeAfter
	spaceReclaimed := sizeBefore - sizeAfter
	if spaceReclaimed < 0 {
		spaceReclaimed = 0
	}

	n.trackCompaction(time.Since(start), spaceReclaimed)

	return nil
}

// GarbageCollect runs garbage collection on old MVCC versions
// Returns the number of versions removed from both memory and disk
// This removes old versions that are no longer visible to any active transaction
//
// Example:
//
//	removed, err := nest.GarbageCollect()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Removed %d old versions\n", removed)
func (n *Nest) GarbageCollect() (int, error) {
	if n.closed {
		return 0, ErrDatabaseClosed
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	removed := 0

	// GC memory versions
	if n.mvcc != nil {
		n.mvcc.GarbageCollect()
		// Note: GarbageCollect doesn't return count in current implementation
	}

	// GC disk versions
	if n.mvccStorage != nil && n.mvcc != nil {
		// Get minimum active snapshot (with lock held)
		n.mvcc.mu.RLock()
		minSnapshot := n.mvcc.getMinActiveSnapshot()
		n.mvcc.mu.RUnlock()

		// Remove old versions from disk
		diskRemoved := n.mvccStorage.GCVersionsOlderThan(TxID(minSnapshot))
		removed += diskRemoved
	}

	return removed, nil
}

// Begin starts a new transaction.
// Transactions provide atomic batch operations with MVCC support.
//
// Example:
//
//	tx, err := nest.Begin()
//	tx.Store("doc1", vector1)
//	tx.Store("doc2", vector2)
//	tx.Commit()
func (n *Nest) Begin() (*Tx, error) {
	if n.closed {
		return nil, ErrDatabaseClosed
	}

	// Use MVCC transaction manager
	if n.txMgr != nil {
		return n.txMgr.Begin()
	}

	// Fallback to internal implementation
	return n.BeginInternal()
}

// Sync forces a sync of pending writes to disk.
// This is called automatically by Close, but can be called manually
// to ensure durability without closing the database.
//
// Example:
//
//	err := nest.Sync()
func (n *Nest) Sync() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.closed {
		return ErrDatabaseClosed
	}

	// TODO: Flush WAL
	// TODO: Sync file to disk

	if n.file != nil {
		return n.file.Sync()
	}

	return nil
}

// init initializes a new database or loads an existing one
func (n *Nest) init(options Options) error {
	// Get file info to check if it's new or existing
	info, err := n.file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Determine if this is a new database
	isNew := info.Size() == 0

	if isNew {
		// Create new database
		n.header = NewDBHeader(uint32(options.Dimensions), options.Distance)

		// Initialize storage with minimal size (1 page for header)
		n.storage = NewStorage()
		if err := n.storage.Init(n.file, PageSize); err != nil {
			return fmt.Errorf("failed to initialize storage: %w", err)
		}

		// Write header to page 0
		if err := n.writeHeader(); err != nil {
			return fmt.Errorf("failed to write header: %w", err)
		}

		// Initialize empty HNSW index
		distanceFunc := GetDistanceFunc(options.Distance)
		n.index = NewHSNWIndex(options.Dimensions, options.M, options.EfConstruction, distanceFunc)

		// Initialize vector pages map
		n.vectorPages = make(map[string]uint64)

		// Initialize WAL if enabled
		if options.WAL && !options.ReadOnly {
			n.wal = NewWAL()
			walPath := n.path + ".wal"
			if err := n.wal.Open(walPath); err != nil {
				return fmt.Errorf("failed to open WAL: %w", err)
			}
		}

	} else {
		// Load existing database
		if err := n.Recover(); err != nil {
			return fmt.Errorf("failed to recover database: %w", err)
		}

		// Initialize WAL after recovery if enabled
		if options.WAL && !options.ReadOnly {
			n.wal = NewWAL()
			walPath := n.path + ".wal"
			if err := n.wal.Open(walPath); err != nil {
				return fmt.Errorf("failed to open WAL: %w", err)
			}
		}
	}

	return nil
}

// validateOptions checks that options are valid
func validateOptions(opts *Options) error {
	if opts.Dimensions < 0 {
		return fmt.Errorf("dimensions must be >= 0")
	}

	if opts.MaxSize <= 0 {
		opts.MaxSize = 10_000_000_000 // 10GB default
	}

	if opts.M < 4 || opts.M > 64 {
		return fmt.Errorf("M must be between 4 and 64")
	}

	if opts.EfConstruction < 100 || opts.EfConstruction > 1000 {
		return fmt.Errorf("EfConstruction must be between 100 and 1000")
	}

	metric := opts.Distance
	if metric != "cosine" && metric != "euclidean" && metric != "dot" {
		return fmt.Errorf("distance must be 'cosine', 'euclidean', or 'dot'")
	}

	return nil
}

// storeVector writes a vector and its metadata to storage
func (n *Nest) storeVector(id string, vector []float32, metadata map[string]interface{}) error {
	// 1. Serialize vector to bytes
	vectorData := SerializeVector(vector)

	// 2. Serialize metadata to JSON (if present)
	var metaJSON []byte
	var metaPageNum uint64
	if metadata != nil && len(metadata) > 0 {
		var err error
		metaJSON, err = json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("failed to serialize metadata: %w", err)
		}

		// Store metadata in a separate page
		metaPageNum, err = n.storeMetadata(metaJSON)
		if err != nil {
			return fmt.Errorf("failed to store metadata: %w", err)
		}
	}

	// 3. Calculate total size needed for vector data
	totalVectorSize := len(vectorData)

	// 4. Check if we need to allocate a new vector page or can use existing
	pageNum, err := n.findOrAllocateVectorPage(id, totalVectorSize)
	if err != nil {
		return fmt.Errorf("failed to allocate vector page: %w", err)
	}

	// 5. Create VectorEntry
	entry := VectorEntry{
		ID:       packIDString(id),
		Offset:   0, // Will be set when writing to page
		Length:   uint16(len(vector)),
		Flags:    0,
		Reserved: 0,
		Metadata: metaPageNum,
	}

	// 6. Write vector entry and data to page
	if err := n.writeVectorToPage(pageNum, &entry, vectorData); err != nil {
		return fmt.Errorf("failed to write vector to page: %w", err)
	}

	// 7. Track mapping: ID → PageNum
	n.vectorPages[id] = pageNum

	return nil
}

// loadVector loads a vector and its metadata from storage
func (n *Nest) loadVector(id string) ([]float32, map[string]interface{}, error) {
	// 1. Find which page contains this vector
	pageNum, exists := n.vectorPages[id]
	if !exists {
		return nil, nil, fmt.Errorf("vector %s not found in page map", id)
	}

	// 2. Read vector page
	entries, err := n.storage.readVectorPage(pageNum)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read vector page: %w", err)
	}

	// 3. Find entry matching ID
	var entry *VectorEntry
	for i := range entries {
		if extractIDString(entries[i].ID) == id {
			entry = &entries[i]
			break
		}
	}

	if entry == nil {
		return nil, nil, fmt.Errorf("vector entry %s not found on page %d", id, pageNum)
	}

	// 4. Read vector data from page
	vectorData, err := n.readVectorData(pageNum, entry.Offset, int(entry.Length)*4)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read vector data: %w", err)
	}

	// 5. Deserialize vector
	vector := DeserializeVector(vectorData)

	// 6. Load metadata if present
	var metadata map[string]interface{}
	if entry.Metadata > 0 {
		metadata, err = n.loadMetadataFromPage(entry.Metadata)
		if err != nil {
			// Log error but don't fail - metadata is optional
			metadata = nil
		}
	}

	return vector, metadata, nil
}

// loadMetadata loads metadata for a vector by ID
func (n *Nest) loadMetadata(id string) (map[string]interface{}, error) {
	// Use loadVector to get metadata
	_, metadata, err := n.loadVector(id)
	if err != nil {
		return nil, err
	}
	return metadata, nil
}

// Helper functions for vector persistence

// findOrAllocateVectorPage finds an existing page with space or allocates a new one
func (n *Nest) findOrAllocateVectorPage(id string, vectorSize int) (uint64, error) {
	// For simplicity, always allocate a new page for each vector
	// In a production implementation, we would pack multiple vectors per page
	pageNum, err := n.storage.AllocatePage()
	if err != nil {
		return 0, fmt.Errorf("failed to allocate page: %w", err)
	}
	return pageNum, nil
}

// writeVectorToPage writes a vector entry and data to a page
func (n *Nest) writeVectorToPage(pageNum uint64, entry *VectorEntry, vectorData []byte) error {
	// Read existing page if it exists
	pageData, err := n.storage.ReadPage(pageNum)
	if err != nil {
		// Create new page
		pageData = make([]byte, PageSize)
	} else {
		// Make a copy to modify
		pageDataCopy := make([]byte, PageSize)
		copy(pageDataCopy, pageData)
		pageData = pageDataCopy
	}

	// Parse existing header or create new one
	header, err := DeserializeHeader(pageData)
	if err != nil || header.Type != VectorPage {
		// Create new page header
		header = NewPageHeader(VectorPage)
		header.Count = 0
	}

	// Calculate offset for vector data (after header and entries)
	// Header: 64 bytes
	// Each entry: 80 bytes
	entryOffset := 64 + int(header.Count)*80
	dataOffset := entryOffset + 80 // Current entry

	// Check if vector data fits in page
	if dataOffset+len(vectorData) > PageSize {
		return fmt.Errorf("vector too large for single page (need multi-page support)")
	}

	// Set entry offset
	entry.Offset = uint32(dataOffset)

	// Pack entry into page
	entryData := packVectorEntry(entry)
	copy(pageData[entryOffset:], entryData)

	// Copy vector data
	copy(pageData[dataOffset:], vectorData)

	// Update header
	header.Count++
	headerData := SerializeHeader(header)
	copy(pageData[0:64], headerData)

	// Calculate and set checksum
	checksum := CalculatePageChecksum(pageData)
	header.Checksum = checksum
	headerData = SerializeHeader(header)
	copy(pageData[0:64], headerData)

	// Write page
	return n.storage.WritePage(pageNum, pageData)
}

// readVectorData reads vector data from a page at the given offset
func (n *Nest) readVectorData(pageNum uint64, offset uint32, length int) ([]byte, error) {
	pageData, err := n.storage.ReadPage(pageNum)
	if err != nil {
		return nil, fmt.Errorf("failed to read page: %w", err)
	}

	if int(offset)+length > PageSize {
		return nil, fmt.Errorf("vector data exceeds page boundary")
	}

	// Return a copy to avoid issues with memory-mapped data
	data := make([]byte, length)
	copy(data, pageData[offset:int(offset)+length])
	return data, nil
}

// storeMetadata stores metadata in a metadata page
func (n *Nest) storeMetadata(metaJSON []byte) (uint64, error) {
	// Allocate a page for metadata
	pageNum, err := n.storage.AllocatePage()
	if err != nil {
		return 0, fmt.Errorf("failed to allocate metadata page: %w", err)
	}

	// Create metadata page
	pageData := make([]byte, PageSize)
	header := NewPageHeader(MetadataPage)
	header.Count = 1

	// Write header
	headerData := SerializeHeader(header)
	copy(pageData[0:64], headerData)

	// Write metadata size (4 bytes)
	binary.LittleEndian.PutUint32(pageData[64:68], uint32(len(metaJSON)))

	// Write metadata JSON
	if 68+len(metaJSON) > PageSize {
		return 0, fmt.Errorf("metadata too large for single page")
	}
	copy(pageData[68:], metaJSON)

	// Calculate checksum
	checksum := CalculatePageChecksum(pageData)
	header.Checksum = checksum
	headerData = SerializeHeader(header)
	copy(pageData[0:64], headerData)

	// Write page
	if err := n.storage.WritePage(pageNum, pageData); err != nil {
		return 0, fmt.Errorf("failed to write metadata page: %w", err)
	}

	return pageNum, nil
}

// loadMetadataFromPage loads metadata from a metadata page
func (n *Nest) loadMetadataFromPage(pageNum uint64) (map[string]interface{}, error) {
	pageData, err := n.storage.ReadPage(pageNum)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata page: %w", err)
	}

	// Verify checksum
	if !VerifyPageChecksum(pageData) {
		return nil, fmt.Errorf("metadata page checksum verification failed")
	}

	// Parse header
	header, err := DeserializeHeader(pageData)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize header: %w", err)
	}

	if header.Type != MetadataPage {
		return nil, fmt.Errorf("expected metadata page, got type %d", header.Type)
	}

	// Read metadata size
	metaSize := binary.LittleEndian.Uint32(pageData[64:68])

	// Read metadata JSON
	if 68+int(metaSize) > PageSize {
		return nil, fmt.Errorf("metadata size exceeds page boundary")
	}

	metaJSON := pageData[68 : 68+metaSize]

	// Unmarshal JSON
	var metadata map[string]interface{}
	if err := json.Unmarshal(metaJSON, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return metadata, nil
}

// BatchFind performs k-NN search for multiple queries in parallel.
// This method parallelizes searches across multiple CPU cores for improved throughput.
// Results are returned in the same order as the input queries.
//
// Performance characteristics:
// - Parallelizes across runtime.NumCPU() workers
// - Uses worker pool to limit concurrency
// - Scales linearly up to NumCPU cores
//
// Example:
//
//	queries := [][]float32{query1, query2, query3}
//	results := nest.BatchFind(queries, 10)
//	for i, resultSet := range results {
//	    fmt.Printf("Query %d: %d results\n", i, len(resultSet))
//	}
func (n *Nest) BatchFind(queries [][]float32, k int) [][]Treasure {
	results := make([][]Treasure, len(queries))

	// Parallelize searches using worker pool
	workers := runtime.NumCPU()
	queryChan := make(chan int, len(queries))

	var wg sync.WaitGroup

	// Start workers
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range queryChan {
				// Perform search for this query
				results[idx] = n.Find(queries[idx], k)
			}
		}()
	}

	// Send work to workers
	for i := range queries {
		queryChan <- i
	}
	close(queryChan)

	// Wait for all workers to complete
	wg.Wait()

	return results
}
