# MagpieDB Architecture

This document describes the internal architecture and design decisions of MagpieDB.

## Overview

MagpieDB is designed as an embedded vector database following the "SQLite philosophy":
- Simple, embedded library (not a service)
- Single file storage
- Zero external dependencies
- Cross-platform pure Go implementation

## System Architecture

```
┌─────────────────────────────────────────┐
│         Application (User Code)          │
└─────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│            MagpieDB API                  │
│   (Open, Store, Find, Remove, Close)    │
└─────────────────────────────────────────┘
                    │
        ┌───────────┴───────────┐
        ▼                       ▼
┌──────────────┐       ┌──────────────────┐
│ HNSW Index   │       │ Storage Engine   │
│ (In-Memory)  │       │ (Memory-Mapped)  │
└──────────────┘       └──────────────────┘
        │                       │
        └───────────┬───────────┘
                    ▼
┌─────────────────────────────────────────┐
│          Single File (.magpie)           │
│  [Header][Vectors][Index][Metadata][WAL] │
└─────────────────────────────────────────┘
```

## Components

### 1. API Layer (`magpie.go`)

The public API provides a minimal, intuitive interface:

```go
nest := magpie.Open("data.magpie")
nest.Store(id, vector, metadata)
results := nest.Find(query, k)
nest.Close()
```

Design principles:
- Simple, obvious names
- Minimal configuration
- Sensible defaults
- Error handling without panics

### 2. Storage Engine (`storage.go`, `page.go`)

**Responsibilities:**
- Memory-mapped file access
- Page management (4KB pages)
- Serialization/deserialization
- Checksum verification

**Key decisions:**

**4KB Page Size**: Matches OS page size for efficient I/O and memory mapping.

**Memory Mapping**: Zero-copy reads via `mmap()`. The OS handles caching and memory management.

**Page Layout**:
```
[Page Header: 64 bytes]
  - Type (1 byte)
  - Count (2 bytes)
  - Next page pointer (8 bytes)
  - Checksum (4 bytes)
  - Reserved (49 bytes)

[Page Data: 4032 bytes]
  - Vector entries or index nodes
```

### 3. HNSW Index (`index.go`, `hnsw.go`)

**Algorithm**: Hierarchical Navigable Small World (HNSW) graphs

**Why HNSW?**
- Best balance of speed vs accuracy for embedded use
- Incremental updates (important for embedded DB)
- Predictable memory usage
- Well-studied algorithm with proven results

**Key parameters:**
- `M` (16): Number of bi-directional links per node
- `EfConstruction` (200): Build quality vs speed tradeoff
- `MaxLevel` (16): Maximum hierarchy depth

**Search complexity:**
- Time: O(log N) average
- Space: O(M × N)

### 4. Distance Metrics (`distance.go`)

Supported metrics:
1. **Cosine Similarity** (default) - Best for normalized embeddings
2. **Euclidean Distance** (L2) - Traditional distance metric
3. **Dot Product** - For pre-normalized vectors

Implementation optimizations:
- Single-pass dot product + norm calculation
- No unnecessary square roots
- Float32 for memory efficiency

### 5. Write-Ahead Log (`wal.go`)

**Purpose**: Durability and crash recovery

**Design**:
- Append-only file (`.magpie.wal`)
- Buffered writes (batch of 100)
- CRC32 checksums per entry
- Sequential replay on recovery

**WAL Entry Format**:
```
[Sequence: 8 bytes]
[Type: 1 byte] (Insert/Update/Delete)
[ID length: 4 bytes]
[ID: N bytes]
[Vector length: 4 bytes]
[Vector data: N×4 bytes]
[Metadata length: 4 bytes]
[Metadata JSON: N bytes]
[Checksum: 4 bytes]
```

### 6. Recovery (`recovery.go`)

**Crash Recovery Process**:
1. Read and validate file header
2. Load vector pages
3. Rebuild HNSW index from vectors
4. Replay WAL entries
5. Truncate WAL

**Integrity Checks**:
- Magic number verification
- Header checksum (CRC64)
- Page checksums (CRC32)
- WAL entry checksums (CRC32)

### 7. Compaction (`compact.go`)

**When to compact:**
- File size > 2× optimal size
- Manual invocation

**Process**:
1. Create temporary file
2. Write live vectors to new file
3. Rebuild index in optimal layout
4. Atomic swap (rename)
5. Remove old file

**Safety**:
- Backup created before swap
- Atomic file operations
- Rollback on failure

### 8. Transactions (`transaction.go`)

**ACID Properties**:
- **Atomicity**: WAL ensures all-or-nothing
- **Consistency**: Checksums and validation
- **Isolation**: Single writer (simple lock)
- **Durability**: fsync() on commit

**Transaction Flow**:
```go
tx := nest.Begin()
tx.Store(...)
tx.Store(...)
tx.Commit() // or Rollback()
```

### 9. Metadata Filtering (`filter.go`)

**Filter Types**:
- Equality: `Eq(key, value)`
- Range: `Between(key, min, max)`
- Existence: `Exists(key)`
- String: `Contains`, `StartsWith`, `EndsWith`
- Composite: `And`, `Or`, `Not`

**Implementation Strategy**:
- Post-filtering (after vector search)
- Trade-off: Accuracy vs performance
- Future: Pre-filtering with inverted index

## File Format

### Database File Structure

```
Offset  | Size    | Content
--------|---------|------------------
0x0000  | 4KB     | Header Page
0x1000  | N×4KB   | Vector Pages
0xN000  | M×4KB   | Index Pages
0xM000  | K×4KB   | Metadata Pages
EOF     | Variable| WAL Section
```

### Header (Page 0)

```
Offset | Size | Field
-------|------|----------------
0      | 8    | Magic ("MAGPIE01")
8      | 4    | Version
12     | 4    | Dimensions
16     | 8    | Vector Count
24     | 8    | Page Count
32     | 8    | Index Root Page
40     | 8    | Metadata Root Page
48     | 8    | WAL Offset
56     | 1    | Distance Metric
57     | 1    | Flags
58     | 4006 | Reserved
4064   | 8    | Checksum (CRC64)
```

## Design Decisions

### Pure Go

**Decision**: No CGO, no C dependencies

**Rationale**:
- Cross-compilation simplicity
- Static binaries
- No platform-specific build issues
- Easier deployment

**Trade-off**: Can't use SIMD libraries like FAISS
- Accept: Pure Go is "fast enough" for embedded use case

### Single Writer

**Decision**: One writer at a time (read-write lock)

**Rationale**:
- Simpler implementation
- Fewer race conditions
- Embedded use case = typically single process
- Transactions provide batching

**Alternative considered**: Multi-writer with MVCC
- Rejected: Too complex for embedded DB

### In-Memory Index

**Decision**: HNSW index kept in RAM

**Rationale**:
- Fast searches (sub-10ms)
- Simple implementation
- Index rebuilt on open from disk

**Trade-off**: Large databases need more RAM
- Mitigation: Lazy loading (future work)

### Memory-Mapped I/O

**Decision**: Use `mmap()` for file access

**Rationale**:
- Zero-copy reads
- OS handles page cache
- Simpler than manual buffering

**Platform consideration**: Works on Linux, macOS, Windows

## Performance Characteristics

### Target Benchmarks

| Operation | Target | Conditions |
|-----------|--------|------------|
| Open | < 10ms | 1M vectors |
| Insert | < 0.5ms | Single |
| Batch Insert | > 100K/s | Buffered |
| Search k=10 | < 5ms | 1M vectors |
| Search k=100 | < 10ms | 1M vectors |

### Memory Usage

Approximate memory per vector:
- Vector data: `dimensions × 4` bytes
- HNSW node: ~100 bytes
- Metadata: variable

Example (384 dimensions):
- 1M vectors ≈ 1.5GB memory
- 10M vectors ≈ 15GB memory

### Disk Usage

- Raw vectors: `count × dimensions × 4` bytes
- With overhead: ~1.5× raw size
- After compaction: ~1.2× raw size

## Future Enhancements

### Planned Features

1. **Quantization**
   - Product Quantization (PQ)
   - Scalar Quantization (SQ)
   - Reduce memory by 4-8×

2. **Lazy Index Loading**
   - Load HNSW graph on-demand
   - Support larger-than-RAM databases

3. **Compression**
   - LZ4 for metadata
   - Delta encoding for vectors

4. **Concurrent Writes**
   - MVCC for multi-writer support
   - More complex, but better for web services

### Non-Goals

These are explicitly **not** planned:

- ❌ Distributed mode (use Qdrant/Milvus instead)
- ❌ Multiple indices per DB (keep it simple)
- ❌ Built-in embedding generation (user's choice)
- ❌ Query language (it's a library, not a service)
- ❌ Network protocol (embedded only)

## References

1. **HNSW Algorithm**
   - Malkov, Y., & Yashunin, D. (2018). "Efficient and robust approximate nearest neighbor search using Hierarchical Navigable Small World graphs"
   - https://arxiv.org/abs/1603.09320

2. **SQLite Design**
   - https://www.sqlite.org/arch.html
   - Inspiration for embedded database architecture

3. **Memory-Mapped I/O**
   - POSIX mmap(2) specification
   - Windows CreateFileMapping

## Contributing

See the main README for contribution guidelines.

For architecture questions or proposals:
1. Open a GitHub issue
2. Tag with `architecture` label
3. Describe the problem and proposed solution
4. Consider backward compatibility

---

**Document Version**: 1.0
**Last Updated**: November 2025
**Maintained By**: VoidLab Engineering
