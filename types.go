package magpie

import (
	"os"
	"sync"
	"time"
)

// Page constants
const (
	PageSize = 4096 // 4KB pages for OS efficiency

	// Page types
	HeaderPage   uint8 = 0x00
	VectorPage   uint8 = 0x01
	IndexPage    uint8 = 0x02
	MetadataPage uint8 = 0x03
	WALPage      uint8 = 0x04
)

// WAL operation types
const (
	WALInsert uint8 = 0x01
	WALDelete uint8 = 0x02
	WALUpdate uint8 = 0x03
)

// Nest represents a MagpieDB database instance.
// It provides the main API for storing and searching vectors.
type Nest struct {
	path             string              // Path to the database file
	file             *os.File            // Database file handle
	mmap             []byte              // Memory-mapped file
	header           *Header             // Database header
	index            *HSNWIndex          // HNSW index for similarity search
	storage          *Storage            // Storage engine
	wal              *WAL                // Write-ahead log
	vectorPages      map[string]uint64   // ID → page number mapping for vector data
	mu               sync.RWMutex        // Protects concurrent access
	closed           bool                // Database closed flag
	mvcc             *MVCCManager        // MVCC version manager
	mvccStorage      *MVCCStorage        // MVCC versioned storage engine
	txMgr            *TxManager          // Transaction manager
	metrics          *Metrics            // Metrics collection
	vectorBufferPool *VectorBufferPool   // Pool for vector buffers
	metadataPool     *MetadataBufferPool // Pool for metadata buffers
	queryCache       *QueryCache         // Query result cache
	workers          *WorkerPool         // Background worker pool
	periodicTasks    []*PeriodicTask     // Scheduled periodic tasks
}

// Treasure represents a stored vector with its metadata and similarity score.
// This is the result type returned from Find operations.
type Treasure struct {
	ID       string                 // Unique identifier
	Vector   []float32              // Vector data
	Distance float32                // Distance from query (lower is more similar)
	Metadata map[string]interface{} // Associated metadata
}

// Options configures database behavior when opening.
type Options struct {
	// Dimensions specifies the vector dimensionality.
	// If 0, dimensions are auto-detected from the first insertion.
	Dimensions int

	// Distance metric to use for similarity calculations.
	// Supported: "cosine" (default), "euclidean", "dot"
	Distance string

	// MaxSize is the maximum database file size in bytes.
	// Default: 10GB
	MaxSize int64

	// WAL enables write-ahead logging for durability.
	// Default: true
	WAL bool

	// M is the HNSW bi-directional link count per node.
	// Higher values improve search quality but increase memory usage.
	// Default: 16, Range: 4-64
	M int

	// EfConstruction controls HNSW index build quality.
	// Higher values improve search quality but slow down insertions.
	// Default: 200, Range: 100-500
	EfConstruction int

	// EfSearch controls search quality at query time.
	// Higher values improve recall but slow down searches.
	// Default: max(k, 100)
	EfSearch int

	// MaxLevel is the maximum layer in the HNSW graph.
	// Default: 16
	MaxLevel int

	// Seed for random level assignment in HNSW.
	// Default: current time
	Seed int64

	// ReadOnly opens the database in read-only mode.
	// Default: false
	ReadOnly bool

	// EnableQueryCache enables query result caching.
	// Default: false
	EnableQueryCache bool

	// QueryCacheSize is the maximum number of cached query results.
	// Default: 1000
	QueryCacheSize int

	// Workers configures background worker behavior
	Workers WorkerConfig
}

// WorkerConfig configures background workers
type WorkerConfig struct {
	Enabled              bool          // Enable background workers
	NumWorkers           int           // Number of worker goroutines
	AutoCompaction       bool          // Enable auto-compaction
	CompactionInterval   time.Duration // Compaction check interval
	MVCCGC              bool          // Enable MVCC GC
	GCInterval          time.Duration // GC interval
	IndexOptimization   bool          // Enable index optimization
	OptimizeInterval    time.Duration // Optimization interval
	MetricsAggregation  bool          // Enable metrics aggregation
	MetricsInterval     time.Duration // Metrics interval
	WALCheckpoint       bool          // Enable WAL checkpointing
	CheckpointInterval  time.Duration // Checkpoint interval

	// NEW: Reindex configuration
	Reindex              bool          // Enable index reindexing
	ReindexInterval      time.Duration // Reindex check interval
	IndexQualityThreshold float64      // Quality threshold for triggering reindex

	// NEW: Backup configuration
	Backup          bool          // Enable periodic backups
	BackupInterval  time.Duration // Backup interval
	BackupDir       string        // Directory for backup files
	MaxBackups      int           // Maximum number of backups to keep

	// NEW: Statistics configuration
	Statistics     bool          // Enable statistics collection
	StatsInterval  time.Duration // Statistics collection interval
	StatsPath      string        // Path to export statistics JSON

	// NEW: Compression configuration
	Compression        bool          // Enable cold data compression
	CompressionInterval time.Duration // Compression check interval
	ColdDataThreshold  time.Duration // Age threshold for "cold" data
}

// DefaultOptions returns the default configuration.
func DefaultOptions() Options {
	return Options{
		Dimensions:       0,
		Distance:         "cosine",
		MaxSize:          10_000_000_000, // 10GB
		WAL:              true,
		M:                16,
		EfConstruction:   200,
		EfSearch:         100,
		MaxLevel:         16,
		Seed:             0,     // Will use current time
		ReadOnly:         false,
		EnableQueryCache: false,
		QueryCacheSize:   1000,
		Workers: WorkerConfig{
			Enabled:            false, // Disabled by default
			NumWorkers:         2,
			AutoCompaction:     true,
			CompactionInterval: 30 * time.Minute,
			MVCCGC:            true,
			GCInterval:        5 * time.Minute,
			IndexOptimization: true,
			OptimizeInterval:  15 * time.Minute,
			MetricsAggregation: true,
			MetricsInterval:   1 * time.Minute,
			WALCheckpoint:     true,
			CheckpointInterval: 10 * time.Minute,

			// NEW: Default configuration for new workers
			Reindex:               true,
			ReindexInterval:       1 * time.Hour,
			IndexQualityThreshold: 0.85,

			Backup:         false, // Disabled by default (user must configure path)
			BackupInterval: 6 * time.Hour,
			BackupDir:      "",
			MaxBackups:     5,

			Statistics:    false, // Disabled by default (user must configure path)
			StatsInterval: 5 * time.Minute,
			StatsPath:     "",

			Compression:         false, // Disabled by default (experimental)
			CompressionInterval: 2 * time.Hour,
			ColdDataThreshold:   24 * time.Hour,
		},
	}
}

// Header stores database metadata in the first page.
type Header struct {
	Magic          [8]byte // "MAGPIE01"
	Version        uint32  // File format version
	Dimensions     uint32  // Vector dimensions
	VectorCount    uint64  // Total number of vectors
	PageCount      uint64  // Total pages in file
	IndexRootPage  uint64  // Root page of HNSW index
	MetadataRoot   uint64  // Root page of metadata B-tree
	WALOffset      uint64  // Offset to WAL section
	DistanceMetric uint8   // 0=cosine, 1=euclidean, 2=dot
	Flags          uint8   // Feature flags
	Reserved       [4006]byte
	Checksum       uint64 // CRC64 of header
}

// PageHeader is the header for each 4KB page.
type PageHeader struct {
	Type     uint8  // Page type (vector, index, metadata, wal)
	Flags    uint8  // Page-specific flags
	Count    uint16 // Number of items in page
	NextPage uint64 // Pointer to next page in chain
	Checksum uint32 // CRC32 of page data
	Reserved [44]byte
}

// VectorEntry represents a stored vector in a vector page.
type VectorEntry struct {
	ID       [64]byte // Null-terminated string ID (max 63 chars)
	Offset   uint32   // Offset to vector data in page
	Length   uint16   // Vector dimension count
	Flags    uint8    // Entry flags
	Reserved uint8    // Alignment
	Metadata uint64   // Pointer to metadata page
}

// HSNWIndex is the in-memory HNSW graph index.
type HSNWIndex struct {
	nodes       map[string]*HSNWNode // All nodes by ID
	entryPoint  *HSNWNode            // Top-level entry point
	maxLevel    int                  // Maximum level in graph
	m           int                  // Bi-directional links per node
	efConstruct int                  // Build quality parameter
	distance    DistanceFunc         // Distance function
	mu          sync.RWMutex         // Protects index modifications
}

// HSNWNode represents a single node in the HNSW graph.
type HSNWNode struct {
	ID        string     // Unique identifier
	Vector    []float32  // Vector data (copy)
	Level     int        // Highest level this node appears in
	Neighbors [][]string // neighbors[level] = list of neighbor IDs
}

// DistanceFunc calculates the distance between two vectors.
// Lower values indicate higher similarity.
type DistanceFunc func(a, b []float32) float32

// WALEntry is a single write-ahead log entry.
type WALEntry struct {
	Sequence uint64                 // Monotonic sequence number
	Type     uint8                  // Operation type (insert, delete, update)
	ID       string                 // Vector ID
	Vector   []float32              // Vector data
	Metadata map[string]interface{} // Metadata
	Checksum uint32                 // CRC32 checksum
}

// Tx represents a database transaction.
type Tx struct {
	nest     *Nest
	writes   []WriteOp
	snapshot *Snapshot
	active   bool
	mvccTx   *MVCCTransaction // MVCC transaction context
	tm       *TxManager       // Transaction manager reference
}

// WriteOp represents a write operation in a transaction.
type WriteOp struct {
	Type     uint8 // Insert, delete, update
	ID       string
	Vector   []float32
	Metadata map[string]interface{}
}

// Snapshot is a read-only point-in-time view of the database.
type Snapshot struct {
	index   *HSNWIndex
	version uint64
}

// Filter is an interface for metadata filtering during searches.
type Filter interface {
	// Match returns true if the metadata passes the filter.
	Match(metadata map[string]interface{}) bool
}

// SimpleFilter provides basic key-value filtering.
type SimpleFilter struct {
	Key   string
	Value interface{}
}

// Match implements the Filter interface.
func (f *SimpleFilter) Match(metadata map[string]interface{}) bool {
	v, ok := metadata[f.Key]
	if !ok {
		return false
	}
	return v == f.Value
}

// AndFilter combines multiple filters with AND logic.
type AndFilter struct {
	Filters []Filter
}

// Match implements the Filter interface.
func (f *AndFilter) Match(metadata map[string]interface{}) bool {
	for _, filter := range f.Filters {
		if !filter.Match(metadata) {
			return false
		}
	}
	return true
}

// OrFilter combines multiple filters with OR logic.
type OrFilter struct {
	Filters []Filter
}

// Match implements the Filter interface.
func (f *OrFilter) Match(metadata map[string]interface{}) bool {
	for _, filter := range f.Filters {
		if filter.Match(metadata) {
			return true
		}
	}
	return false
}

// SearchResult is an internal type used during HNSW search.
type SearchResult struct {
	Node     *HSNWNode
	Distance float32
}

// Error types
var (
	ErrDatabaseClosed    = &MagpieError{"database is closed"}
	ErrInvalidDimensions = &MagpieError{"invalid vector dimensions"}
	ErrVectorNotFound    = &MagpieError{"vector not found"}
	ErrCorruptedData     = &MagpieError{"corrupted database"}
	ErrTransactionActive = &MagpieError{"transaction already active"}
	ErrReadOnly          = &MagpieError{"database is read-only"}
	ErrInvalidID         = &MagpieError{"invalid vector ID"}
	ErrTxAborted         = &MagpieError{"transaction aborted"}
	ErrTxNotActive       = &MagpieError{"transaction not active"}
	ErrVersionDeleted    = &MagpieError{"version already deleted"}
)

// MagpieError represents a MagpieDB-specific error.
type MagpieError struct {
	Message string
}

func (e *MagpieError) Error() string {
	return "magpie: " + e.Message
}
