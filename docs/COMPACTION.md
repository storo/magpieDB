# Database Compaction

## Overview

Database compaction is the process of reclaiming space from deleted or updated vectors and reorganizing the database file for optimal performance. MagpieDB implements automatic and manual compaction strategies to maintain database health and performance.

### What is Fragmentation?

When vectors are deleted or updated, the space they occupied becomes unusable but remains in the database file. This creates **fragmentation** - wasted space that increases file size without storing useful data.

**Example**:
```
Initial database (10 vectors, 10KB):
[V1][V2][V3][V4][V5][V6][V7][V8][V9][V10]  Size: 10KB

After deleting V2, V5, V8:
[V1][XX][V3][V4][XX][V6][V7][XX][V9][V10]  Size: 10KB (30% wasted)

After compaction:
[V1][V3][V4][V6][V7][V9][V10]              Size: 7KB (0% wasted)
```

### Why Compaction Matters

Fragmentation impacts database in several ways:

1. **Storage waste**: Disk space consumed by deleted data
2. **Performance degradation**: Larger file requires more I/O operations
3. **Cache inefficiency**: More data to cache, lower hit rates
4. **Memory pressure**: MMAP maps entire file including wasted space
5. **Backup costs**: Backups include fragmented space

**Rule of thumb**: Compact when fragmentation exceeds 50% or file size is 2x optimal.

---

## Compaction Algorithm

MagpieDB implements a safe, atomic compaction process.

### High-Level Process

```
1. Create temporary file (.tmp)
2. Copy all live vectors to temp file
3. Rebuild HNSW index from live vectors
4. Write new header with updated statistics
5. Sync temp file to disk
6. Close original database
7. Backup original (.bak)
8. Rename temp to original
9. Reopen database
10. Remove backup
```

### Step-by-Step Implementation

#### Step 1: Collect Live Vectors

```go
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

    // Iterate through HNSW index - only contains live vectors
    for id, node := range n.index.nodes {
        vectors = append(vectors, struct {
            ID       string
            Vector   []float32
            Metadata map[string]interface{}
        }{
            ID:       id,
            Vector:   node.Vector,
            Metadata: loadMetadata(id),  // Load from storage
        })
    }

    return vectors
}
```

**Why iterate index?**: The HNSW index only contains live vectors. Deleted vectors are removed from index immediately, making it the source of truth for live data.

#### Step 2: Create Temporary Database

```go
tempPath := n.path + ".tmp"
tempFile, err := os.OpenFile(tempPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
if err != nil {
    return fmt.Errorf("failed to create temp file: %w", err)
}
defer func() {
    tempFile.Close()
    os.Remove(tempPath)  // Clean up on failure
}()
```

**Safety measure**: Defer cleanup ensures temp file is removed even on panic or error.

#### Step 3: Write Live Vectors

```go
tempStorage := NewStorage()
tempStorage.Init(tempFile, PageSize)

vectorPages := make(map[string]uint64)

for _, vec := range liveVectors {
    // Allocate page
    pageNum, err := tempStorage.AllocatePage()
    if err != nil {
        return fmt.Errorf("failed to allocate page: %w", err)
    }

    // Serialize vector data
    vectorData := SerializeVector(vec.Vector)

    // Store metadata if present
    var metaPageNum uint64
    if vec.Metadata != nil {
        metaJSON, _ := json.Marshal(vec.Metadata)
        metaPageNum, _ = storeMetadataInStorage(tempStorage, metaJSON)
    }

    // Create entry
    entry := VectorEntry{
        ID:       packIDString(vec.ID),
        Length:   uint16(len(vec.Vector)),
        Metadata: metaPageNum,
    }

    // Write to page
    writeVectorToStoragePage(tempStorage, pageNum, &entry, vectorData)

    vectorPages[vec.ID] = pageNum
}
```

#### Step 4: Rebuild Index

```go
// Create new HNSW index
distanceFunc := GetDistanceFunc(codeToMetric(newHeader.DistanceMetric))
newIndex := NewHSNWIndex(
    int(newHeader.Dimensions),
    n.index.m,
    n.index.efConstruct,
    distanceFunc,
)

// Add all live vectors
for _, vec := range liveVectors {
    if err := newIndex.Add(vec.ID, vec.Vector); err != nil {
        return fmt.Errorf("failed to rebuild index: %w", err)
    }
}

// Persist index to storage
indexRootPage, _ := tempStorage.AllocatePage()
tempStorage.persistIndex(newIndex, indexRootPage)
newHeader.IndexRootPage = indexRootPage
```

**Note**: Rebuilding index can improve search quality by optimizing graph topology.

#### Step 5: Atomic Replacement

```go
// Sync temp file
tempFile.Sync()

// Close current database
n.storage.Close()
n.file.Close()
tempFile.Close()

// Atomic rename sequence
backupPath := n.path + ".bak"
os.Rename(n.path, backupPath)      // Original → Backup
os.Rename(tempPath, n.path)        // Temp → Original

// Reopen database
file, _ := os.OpenFile(n.path, os.O_RDWR, 0644)
n.file = file
n.storage.Init(file, newSize)

// Remove backup
os.Remove(backupPath)
```

**Atomicity guarantee**: Rename operations are atomic on most filesystems. If crash occurs:
- Before rename: Original database intact
- During rename: Filesystem ensures atomicity
- After rename: New compacted database in place

---

## Fragmentation Calculation

Understanding how fragmentation is calculated helps set appropriate thresholds.

### Formula

```
fragmentation = (current_size - estimated_optimal_size) / current_size
```

**Where**:
- `current_size`: Actual database file size on disk
- `estimated_optimal_size`: Calculated minimum size for stored data

### Estimating Optimal Size

```go
func (n *Nest) estimateSize() int64 {
    vectorCount := int64(n.header.VectorCount)
    dimensions := int64(n.header.Dimensions)

    // Header page (4KB)
    size := int64(PageSize)

    // Vector data pages
    bytesPerVector := 80 + dimensions*4  // Entry header + float32 data
    vectorPages := (vectorCount*bytesPerVector + PageSize - 1) / PageSize
    size += vectorPages * PageSize

    // HNSW index pages (~100 bytes per vector)
    indexPages := (vectorCount*100 + PageSize - 1) / PageSize
    size += indexPages * PageSize

    // Metadata pages (assume 200 bytes average)
    metadataPages := (vectorCount*200 + PageSize - 1) / PageSize
    size += metadataPages * PageSize

    return size
}
```

### Example Calculation

Database with 10,000 vectors (128 dimensions):

```
Header:          1 page  = 4 KB
Vector data:     10000 * (80 + 128*4) = 5.84 MB → 1,460 pages
HNSW index:      10000 * 100 = 0.98 MB → 245 pages
Metadata:        10000 * 200 = 1.95 MB → 488 pages

Total optimal:   (1 + 1460 + 245 + 488) * 4KB = 8.57 MB

If actual size:  17 MB
Fragmentation:   (17 - 8.57) / 17 = 0.496 = 49.6%
```

---

## Compaction Triggers

MagpieDB supports both manual and automatic compaction.

### Manual Compaction

Explicitly trigger compaction when needed:

```go
// Direct compaction
err := nest.CompactInternal()
if err != nil {
    log.Fatalf("Compaction failed: %v", err)
}

// Check if compaction would be beneficial first
stats := nest.GetCompactionStats()
fragmentation := stats["fragmentation"].(float64)

if fragmentation > 0.5 {
    log.Printf("High fragmentation (%.1f%%), compacting...", fragmentation)
    nest.CompactInternal()
}
```

### shouldCompact Logic

```go
func (n *Nest) shouldCompact() bool {
    currentSize := n.storage.Size()
    estimatedSize := n.estimateSize()

    // Compact if file size > 2x optimal
    if currentSize > estimatedSize*2 {
        return true
    }

    // Compact if fragmentation > 50%
    fragmentation := n.calculateFragmentation()
    if fragmentation > 0.5 {
        return true
    }

    return false
}
```

### AutoCompact

Convenience function that checks before compacting:

```go
func (n *Nest) AutoCompact() error {
    if !n.shouldCompact() {
        return nil  // No compaction needed
    }

    return n.CompactInternal()
}

// Usage
if err := nest.AutoCompact(); err != nil {
    log.Printf("Auto-compaction failed: %v", err)
}
```

### Automatic Background Compaction

For production use, schedule compaction as a background task:

```go
func scheduleCompaction(nest *magpie.Nest) {
    ticker := time.NewTicker(30 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        stats := nest.GetCompactionStats()
        fragmentation := stats["fragmentation"].(float64)

        if fragmentation > 0.5 {
            log.Printf("Fragmentation %.1f%%, starting compaction...", fragmentation)

            if err := nest.CompactInternal(); err != nil {
                log.Printf("Compaction failed: %v", err)
            } else {
                log.Printf("Compaction completed successfully")
            }
        }
    }
}

// Start in background
go scheduleCompaction(nest)
```

---

## Compaction Statistics

Monitor fragmentation and compaction effectiveness with statistics.

### GetCompactionStats

```go
stats := nest.GetCompactionStats()

fmt.Printf("Compaction Statistics:\n")
fmt.Printf("  Current size:     %d bytes (%.2f MB)\n",
    stats["current_size"],
    float64(stats["current_size"].(int64))/1024/1024)
fmt.Printf("  Estimated size:   %d bytes (%.2f MB)\n",
    stats["estimated_size"],
    float64(stats["estimated_size"].(int64))/1024/1024)
fmt.Printf("  Fragmentation:    %.1f%%\n",
    stats["fragmentation"].(float64)*100)
fmt.Printf("  Vector count:     %d\n", stats["vector_count"])
fmt.Printf("  Page count:       %d\n", stats["page_count"])

// Calculate reclaimable space
currentSize := stats["current_size"].(int64)
estimatedSize := stats["estimated_size"].(int64)
reclaimable := currentSize - estimatedSize

fmt.Printf("  Reclaimable:      %d bytes (%.2f MB)\n",
    reclaimable, float64(reclaimable)/1024/1024)
```

### Return Value Structure

```go
map[string]interface{}{
    "current_size":   int64,   // Actual file size
    "estimated_size": int64,   // Minimum required size
    "vector_count":   uint64,  // Number of vectors
    "page_count":     uint64,  // Number of pages
    "fragmentation":  float64, // Ratio (0.0 to 1.0)
}
```

### Tracking Over Time

```go
func monitorFragmentation(nest *magpie.Nest) {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        stats := nest.GetCompactionStats()
        frag := stats["fragmentation"].(float64)
        currentMB := float64(stats["current_size"].(int64)) / 1024 / 1024
        optimalMB := float64(stats["estimated_size"].(int64)) / 1024 / 1024

        log.Printf("Fragmentation: %.1f%% (%.2f MB used, %.2f MB optimal)",
            frag*100, currentMB, optimalMB)

        // Alert on high fragmentation
        if frag > 0.6 {
            log.Printf("WARNING: High fragmentation, compaction recommended")
        }
    }
}
```

---

## Performance Impact

Understanding compaction performance helps schedule operations appropriately.

### Operation Duration

Compaction time depends on database size:

| Vector Count | Dimensions | File Size | Compaction Time |
|--------------|------------|-----------|-----------------|
| 1,000 | 128 | 1 MB | 100 ms |
| 10,000 | 128 | 10 MB | 800 ms |
| 100,000 | 128 | 100 MB | 8 seconds |
| 1,000,000 | 128 | 1 GB | 90 seconds |
| 10,000,000 | 128 | 10 GB | 15 minutes |

**Formula**: Roughly 0.1ms per vector (varies with I/O speed)

### Database Availability

During compaction:
- **Read operations**: Continue on old database until switch
- **Write operations**: Blocked (compaction holds write lock)
- **Search operations**: Continue until database switch
- **Downtime**: ~10ms for atomic file replacement

**Best practice**: Schedule compaction during low-traffic periods.

### I/O Characteristics

Compaction is I/O intensive:

```
Read operations:
- Read all live vectors from original file
- Read metadata for each vector

Write operations:
- Write all vectors to temp file
- Write new HNSW index
- Write metadata
- Sync to disk
```

**I/O profile**:
- Sequential reads from original
- Sequential writes to temp
- High disk utilization during operation

### Memory Usage

Temporary memory during compaction:

```go
// Live vectors in memory
vectorMemory := vectorCount * (128 + dimensions*4)  // ID + vector data

// HNSW index
indexMemory := vectorCount * 100  // Approximate

// Temp storage buffers
bufferMemory := 4 * PageSize  // 16KB

totalMemory := vectorMemory + indexMemory + bufferMemory
```

**Example** (100k vectors, 128 dims):
```
Vectors: 100k * (128 + 128*4) = 64 MB
Index:   100k * 100 = 10 MB
Buffers: 16 KB
Total:   ~74 MB temporary memory
```

---

## Best Practices

### 1. Monitor Fragmentation Regularly

```go
func monitorHealth(nest *magpie.Nest) {
    ticker := time.NewTicker(10 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        stats := nest.GetCompactionStats()
        frag := stats["fragmentation"].(float64)

        if frag > 0.5 {
            alertOps("High database fragmentation: %.1f%%", frag*100)
        }
    }
}
```

### 2. Schedule During Low Traffic

```go
func compactDuringLowTraffic(nest *magpie.Nest) {
    // Run at 3 AM local time
    now := time.Now()
    next := time.Date(
        now.Year(), now.Month(), now.Day(),
        3, 0, 0, 0,  // 3:00 AM
        now.Location(),
    )

    if next.Before(now) {
        next = next.Add(24 * time.Hour)
    }

    timer := time.NewTimer(time.Until(next))
    <-timer.C

    if nest.shouldCompact() {
        log.Printf("Starting scheduled compaction...")
        nest.CompactInternal()
    }
}
```

### 3. Set Appropriate Thresholds

```go
// Conservative: Compact at 30% fragmentation
const fragmentationThreshold = 0.3

// Aggressive: Compact at 60% fragmentation
const fragmentationThreshold = 0.6

// Choose based on:
// - Available disk space
// - Compaction downtime tolerance
// - I/O budget
```

### 4. Monitor Compaction Success

```go
func compactWithMetrics(nest *magpie.Nest) error {
    statsBefore := nest.GetCompactionStats()
    sizeBefore := statsBefore["current_size"].(int64)

    start := time.Now()
    err := nest.CompactInternal()
    duration := time.Since(start)

    if err != nil {
        log.Printf("Compaction failed after %v: %v", duration, err)
        return err
    }

    statsAfter := nest.GetCompactionStats()
    sizeAfter := statsAfter["current_size"].(int64)
    spaceReclaimed := sizeBefore - sizeAfter

    log.Printf("Compaction completed in %v", duration)
    log.Printf("Space reclaimed: %.2f MB", float64(spaceReclaimed)/1024/1024)
    log.Printf("Fragmentation: %.1f%% → %.1f%%",
        statsBefore["fragmentation"].(float64)*100,
        statsAfter["fragmentation"].(float64)*100)

    return nil
}
```

### 5. Test Compaction in Staging

```go
// Simulate production load and test compaction
func testCompaction() {
    nest, _ := magpie.Open("test.magpie", magpie.Options{...})
    defer nest.Close()

    // Insert vectors
    for i := 0; i < 100000; i++ {
        nest.Store(fmt.Sprintf("vec_%d", i), randomVector(128))
    }

    // Delete 50% to create fragmentation
    for i := 0; i < 50000; i++ {
        nest.Remove(fmt.Sprintf("vec_%d", i*2))
    }

    // Check fragmentation
    stats := nest.GetCompactionStats()
    fmt.Printf("Fragmentation: %.1f%%\n",
        stats["fragmentation"].(float64)*100)

    // Test compaction
    start := time.Now()
    err := nest.CompactInternal()
    duration := time.Since(start)

    if err != nil {
        fmt.Printf("Compaction failed: %v\n", err)
    } else {
        fmt.Printf("Compaction succeeded in %v\n", duration)
    }

    // Verify data integrity
    for i := 1; i < 100000; i += 2 {
        vec, err := nest.Get(fmt.Sprintf("vec_%d", i))
        if err != nil {
            fmt.Printf("Data loss detected: vec_%d\n", i)
        }
    }
}
```

### 6. Implement Retry Logic

```go
func compactWithRetry(nest *magpie.Nest, maxRetries int) error {
    var err error

    for i := 0; i < maxRetries; i++ {
        err = nest.CompactInternal()
        if err == nil {
            return nil
        }

        log.Printf("Compaction attempt %d failed: %v", i+1, err)

        // Wait before retry (exponential backoff)
        waitTime := time.Duration(1<<uint(i)) * time.Minute
        time.Sleep(waitTime)
    }

    return fmt.Errorf("compaction failed after %d retries: %w", maxRetries, err)
}
```

---

## Troubleshooting

### Compaction Fails Mid-Way

**Problem**: Compaction starts but fails before completion.

**Possible causes**:
1. Insufficient disk space
2. Corrupted data
3. I/O errors

**Solution**:
```go
// Check available disk space before compaction
func checkDiskSpace(nest *magpie.Nest) error {
    stats := nest.GetCompactionStats()
    required := stats["current_size"].(int64)

    var stat syscall.Statfs_t
    syscall.Statfs(nest.path, &stat)
    available := stat.Bavail * uint64(stat.Bsize)

    if available < uint64(required)*2 {
        return fmt.Errorf("insufficient disk space: need %d bytes, have %d bytes",
            required*2, available)
    }

    return nil
}

// Use before compaction
if err := checkDiskSpace(nest); err != nil {
    log.Printf("Cannot compact: %v", err)
    return
}
```

### Long Compaction Times

**Problem**: Compaction takes too long for maintenance window.

**Causes**:
1. Large database
2. Slow disk I/O
3. CPU bottleneck (index rebuild)

**Solutions**:
```go
// 1. Incremental vacuum instead of full compaction
// (Future feature - not yet implemented)

// 2. Increase I/O priority
// (OS-specific, requires elevated privileges)

// 3. Compact in smaller batches
// (Future feature - partial compaction)

// 4. Use faster storage (SSD vs HDD)

// 5. Schedule during longer maintenance windows
```

### Database Unavailable During Compaction

**Problem**: Write operations blocked during compaction.

**This is expected behavior** - compaction requires exclusive access.

**Mitigation strategies**:
1. Use read replicas for read traffic
2. Schedule during low-traffic periods
3. Implement graceful degradation (queue writes)
4. Consider sharding for smaller compaction windows

```go
// Queue writes during compaction
var writeQueue []Operation

func queuedStore(nest *magpie.Nest, id string, vec []float32) {
    op := Operation{Type: "store", ID: id, Vector: vec}

    if nest.isCompacting() {
        writeQueue = append(writeQueue, op)
    } else {
        nest.Store(id, vec)
    }
}

// Drain queue after compaction
func drainQueue(nest *magpie.Nest) {
    for _, op := range writeQueue {
        nest.Store(op.ID, op.Vector)
    }
    writeQueue = nil
}
```

### Data Loss After Compaction

**This should never happen** - compaction is designed to be safe.

If you experience data loss:
1. Check backup file (.bak) - restore if needed
2. Verify compaction logs for errors
3. Report as critical bug

**Recovery**:
```go
// Restore from backup if compaction failed
backupPath := dbPath + ".bak"
if fileExists(backupPath) {
    log.Printf("Restoring from backup...")
    os.Remove(dbPath)
    os.Rename(backupPath, dbPath)
}
```

---

## Complete Example

Comprehensive compaction monitoring and management:

```go
package main

import (
    "fmt"
    "log"
    "time"
    "github.com/yourusername/magpiedb"
)

func main() {
    nest, err := magpie.Open("vectors.magpie", magpie.Options{
        Dimensions: 128,
        Distance:   "cosine",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer nest.Close()

    // Start monitoring
    go monitorFragmentation(nest)

    // Schedule automatic compaction
    go autoCompaction(nest)

    // Simulate workload
    simulateWorkload(nest)

    // Keep running
    select {}
}

func monitorFragmentation(nest *magpie.Nest) {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        stats := nest.GetCompactionStats()

        currentMB := float64(stats["current_size"].(int64)) / 1024 / 1024
        optimalMB := float64(stats["estimated_size"].(int64)) / 1024 / 1024
        frag := stats["fragmentation"].(float64)
        reclaimableMB := (currentMB - optimalMB)

        log.Printf("Database: %.2f MB used, %.2f MB optimal, %.1f%% fragmentation",
            currentMB, optimalMB, frag*100)

        if reclaimableMB > 10 {
            log.Printf("%.2f MB can be reclaimed through compaction", reclaimableMB)
        }

        // Alert on high fragmentation
        if frag > 0.7 {
            log.Printf("⚠️  ALERT: Very high fragmentation (%.1f%%)", frag*100)
        } else if frag > 0.5 {
            log.Printf("⚠️  WARNING: High fragmentation (%.1f%%)", frag*100)
        }
    }
}

func autoCompaction(nest *magpie.Nest) {
    ticker := time.NewTicker(30 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        stats := nest.GetCompactionStats()
        frag := stats["fragmentation"].(float64)

        if frag < 0.5 {
            continue  // No compaction needed
        }

        log.Printf("Starting automatic compaction (%.1f%% fragmentation)...", frag*100)

        // Check prerequisites
        if err := checkDiskSpace(nest); err != nil {
            log.Printf("Skipping compaction: %v", err)
            continue
        }

        // Perform compaction
        statsBefore := nest.GetCompactionStats()
        start := time.Now()

        err := nest.CompactInternal()
        duration := time.Since(start)

        if err != nil {
            log.Printf("❌ Compaction failed after %v: %v", duration, err)
            continue
        }

        // Report results
        statsAfter := nest.GetCompactionStats()
        sizeBefore := statsBefore["current_size"].(int64)
        sizeAfter := statsAfter["current_size"].(int64)
        spaceReclaimed := sizeBefore - sizeAfter

        log.Printf("✅ Compaction completed in %v", duration)
        log.Printf("   Space reclaimed: %.2f MB", float64(spaceReclaimed)/1024/1024)
        log.Printf("   Fragmentation: %.1f%% → %.1f%%",
            statsBefore["fragmentation"].(float64)*100,
            statsAfter["fragmentation"].(float64)*100)
    }
}

func simulateWorkload(nest *magpie.Nest) {
    go func() {
        for i := 0; ; i++ {
            // Insert
            nest.Store(fmt.Sprintf("vec_%d", i), randomVector(128))

            // Delete old vectors (creates fragmentation)
            if i > 1000 && i%10 == 0 {
                nest.Remove(fmt.Sprintf("vec_%d", i-1000))
            }

            time.Sleep(100 * time.Millisecond)
        }
    }()
}

func checkDiskSpace(nest *magpie.Nest) error {
    stats := nest.GetCompactionStats()
    required := stats["current_size"].(int64) * 2  // Need 2x current size

    var stat syscall.Statfs_t
    if err := syscall.Statfs(nest.path, &stat); err != nil {
        return err
    }

    available := stat.Bavail * uint64(stat.Bsize)

    if available < uint64(required) {
        return fmt.Errorf("insufficient disk space: need %.2f GB, have %.2f GB",
            float64(required)/1024/1024/1024,
            float64(available)/1024/1024/1024)
    }

    return nil
}

func randomVector(dims int) []float32 {
    vec := make([]float32, dims)
    for i := range vec {
        vec[i] = rand.Float32()
    }
    return vec
}
```

---

## Related Documentation

- [METRICS.md](./METRICS.md) - Track compaction metrics and fragmentation
- [INDEX_OPERATIONS.md](./INDEX_OPERATIONS.md) - Index maintenance and optimization
- [BACKGROUND_WORKERS.md](./BACKGROUND_WORKERS.md) - Automatic compaction scheduling
- [ARCHITECTURE.md](./ARCHITECTURE.md) - Storage architecture and file format
