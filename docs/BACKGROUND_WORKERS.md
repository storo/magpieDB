# Background Workers

## Overview

MagpieDB implements a background worker system for automated database maintenance tasks. Workers run asynchronously and perform periodic operations like compaction, garbage collection, index optimization, and metrics aggregation without blocking user operations.

### Key Features

- **Goroutine-based workers**: Lightweight concurrent task execution
- **Task queue system**: Priority-based task scheduling
- **Graceful shutdown**: Clean worker termination on database close
- **Configurable intervals**: Adjust task frequency per workload
- **Error handling**: Automatic retry with exponential backoff
- **Resource-aware**: Adjusts behavior based on system load

### Why Background Workers?

Background workers automate maintenance tasks that would otherwise require manual intervention:

| Task | Without Workers | With Workers |
|------|-----------------|--------------|
| Compaction | Manual scheduling | Automatic when needed |
| MVCC GC | Memory grows unbounded | Cleaned every 5 min |
| Index optimization | Degrades over time | Maintained continuously |
| Metrics | Manual collection | Aggregated automatically |
| WAL checkpoint | WAL grows indefinitely | Flushed every 10 min |

---

## Architecture

### Worker Pool Design

```go
type WorkerPool struct {
    workers      []*Worker
    taskQueue    chan Task
    stopChan     chan struct{}
    wg           sync.WaitGroup
    nest         *Nest
    config       WorkerConfig
}

type Worker struct {
    id       int
    pool     *WorkerPool
    stopChan chan struct{}
}

type Task interface {
    Execute(nest *Nest) error
    Priority() int       // Higher = more important
    Name() string
}
```

**Architecture benefits**:
- **Multiple workers**: Parallel task execution
- **Task queue**: Decouples task submission from execution
- **Priority scheduling**: Critical tasks run first
- **Clean shutdown**: No orphaned goroutines

### Task Scheduling

```
Task submission:
  scheduledTask() → taskQueue (buffered channel)
                         ↓
                    Worker pool (N goroutines)
                         ↓
                    Execute task
                         ↓
                    Sleep until next interval
```

---

## Configuration

### WorkerConfig Structure

```go
type WorkerConfig struct {
    // Enable background workers
    Enabled bool

    // Number of worker goroutines
    NumWorkers int

    // Auto-compaction settings
    AutoCompaction       bool
    CompactionInterval   time.Duration
    CompactionThreshold  float64  // Fragmentation threshold (0.0-1.0)

    // MVCC garbage collection
    MVCCGCEnabled        bool
    MVCCGCInterval       time.Duration
    MVCCGCMaxVersions    int      // Keep at most N versions per key

    // Index optimization
    IndexOptimization    bool
    IndexOptInterval     time.Duration
    IndexOptThreshold    float64  // Deletion ratio threshold

    // Metrics aggregation
    MetricsAggregation   bool
    MetricsInterval      time.Duration

    // WAL checkpointing
    WALCheckpoint        bool
    WALCheckpointInterval time.Duration
    WALCheckpointSize    int64    // Checkpoint when WAL exceeds size
}
```

### Default Configuration

```go
var DefaultWorkerConfig = WorkerConfig{
    Enabled:               true,
    NumWorkers:            4,

    AutoCompaction:        true,
    CompactionInterval:    30 * time.Minute,
    CompactionThreshold:   0.5,

    MVCCGCEnabled:         true,
    MVCCGCInterval:        5 * time.Minute,
    MVCCGCMaxVersions:     10,

    IndexOptimization:     true,
    IndexOptInterval:      15 * time.Minute,
    IndexOptThreshold:     0.2,

    MetricsAggregation:    true,
    MetricsInterval:       1 * time.Minute,

    WALCheckpoint:         true,
    WALCheckpointInterval: 10 * time.Minute,
    WALCheckpointSize:     100 * 1024 * 1024,  // 100MB
}
```

### Enabling Workers

```go
nest, err := magpie.Open("vectors.magpie", magpie.Options{
    Dimensions: 128,
    Distance:   "cosine",
    Workers:    magpie.DefaultWorkerConfig,
})
```

### Custom Configuration

```go
nest, err := magpie.Open("vectors.magpie", magpie.Options{
    Dimensions: 128,
    Distance:   "cosine",
    Workers: magpie.WorkerConfig{
        Enabled:               true,
        NumWorkers:            2,  // Fewer workers for low-power systems

        AutoCompaction:        true,
        CompactionInterval:    1 * time.Hour,  // Less frequent
        CompactionThreshold:   0.7,            // Higher threshold

        MVCCGCEnabled:         true,
        MVCCGCInterval:        10 * time.Minute,  // More frequent
        MVCCGCMaxVersions:     5,                  // Keep fewer versions

        IndexOptimization:     false,  // Disable if not needed

        MetricsAggregation:    true,
        MetricsInterval:       30 * time.Second,

        WALCheckpoint:         true,
        WALCheckpointInterval: 5 * time.Minute,
        WALCheckpointSize:     50 * 1024 * 1024,  // 50MB
    },
})
```

---

## Worker Types

### 1. AutoCompactionWorker

Monitors database fragmentation and triggers compaction automatically.

#### Configuration

```go
AutoCompaction:       true
CompactionInterval:   30 * time.Minute
CompactionThreshold:  0.5  // Compact at 50% fragmentation
```

#### Implementation

```go
type AutoCompactionTask struct {
    threshold float64
}

func (t *AutoCompactionTask) Execute(nest *Nest) error {
    stats := nest.GetCompactionStats()
    fragmentation := stats["fragmentation"].(float64)

    if fragmentation < t.threshold {
        return nil  // No compaction needed
    }

    log.Printf("Auto-compaction triggered (%.1f%% fragmentation)", fragmentation*100)

    start := time.Now()
    err := nest.CompactInternal()
    duration := time.Since(start)

    if err != nil {
        log.Printf("Auto-compaction failed: %v", err)
        return err
    }

    log.Printf("Auto-compaction completed in %v", duration)
    return nil
}

func (t *AutoCompactionTask) Priority() int { return 5 }
func (t *AutoCompactionTask) Name() string { return "AutoCompaction" }
```

#### Behavior

- **Runs every**: 30 minutes (configurable)
- **Checks**: Database fragmentation via `GetCompactionStats()`
- **Triggers**: Compaction if fragmentation > threshold
- **Priority**: Medium (5/10)
- **Duration**: 1s to 15 minutes (depends on database size)

#### Tuning

```go
// Aggressive: Compact frequently, keep database tight
AutoCompaction:       true
CompactionInterval:   15 * time.Minute
CompactionThreshold:  0.3

// Conservative: Compact rarely, minimize overhead
AutoCompaction:       true
CompactionInterval:   2 * time.Hour
CompactionThreshold:  0.7

// Disabled: Manual compaction only
AutoCompaction:       false
```

---

### 2. MVCCGCWorker

Garbage collects old MVCC versions to prevent memory bloat.

#### Configuration

```go
MVCCGCEnabled:      true
MVCCGCInterval:     5 * time.Minute
MVCCGCMaxVersions:  10  // Keep max 10 versions per key
```

#### Implementation

```go
type MVCCGCTask struct {
    maxVersions int
}

func (t *MVCCGCTask) Execute(nest *Nest) error {
    if nest.mvcc == nil {
        return nil
    }

    before := nest.mvcc.VersionCount()

    // Remove versions older than oldest active transaction
    oldestTxn := nest.txMgr.OldestActiveTransaction()
    removed := nest.mvcc.GarbageCollect(oldestTxn, t.maxVersions)

    after := nest.mvcc.VersionCount()

    if removed > 0 {
        log.Printf("MVCC GC: removed %d versions (%d → %d)",
            removed, before, after)
    }

    return nil
}

func (t *MVCCGCTask) Priority() int { return 7 }
func (t *MVCCGCTask) Name() string { return "MVCC_GC" }
```

#### Behavior

- **Runs every**: 5 minutes (configurable)
- **Removes**: Old MVCC versions no longer needed
- **Keeps**: Versions for active transactions
- **Priority**: High (7/10) - memory management critical
- **Duration**: < 100ms typically

#### Tuning

```go
// Aggressive: Keep only recent versions
MVCCGCEnabled:      true
MVCCGCInterval:     1 * time.Minute
MVCCGCMaxVersions:  3

// Conservative: Keep more history
MVCCGCEnabled:      true
MVCCGCInterval:     15 * time.Minute
MVCCGCMaxVersions:  50

// Disabled: Versions accumulate (high memory usage)
MVCCGCEnabled:      false
```

---

### 3. IndexOptimizationWorker

Maintains HNSW graph quality by cleaning up after deletions.

#### Configuration

```go
IndexOptimization:   true
IndexOptInterval:    15 * time.Minute
IndexOptThreshold:   0.2  // Optimize if 20% deletions
```

#### Implementation

```go
type IndexOptimizationTask struct {
    threshold float64
}

func (t *IndexOptimizationTask) Execute(nest *Nest) error {
    diag := nest.index.Diagnostics()

    // Calculate deletion ratio
    deletionRatio := float64(diag.DanglingReferences) / float64(diag.NodeCount)

    if deletionRatio < t.threshold {
        return nil  // No optimization needed
    }

    log.Printf("Index optimization triggered (%.1f%% dangling refs)",
        deletionRatio*100)

    err := nest.index.OptimizeIndex()
    if err != nil {
        log.Printf("Index optimization failed: %v", err)
        return err
    }

    log.Printf("Index optimization completed")
    return nil
}

func (t *IndexOptimizationTask) Priority() int { return 6 }
func (t *IndexOptimizationTask) Name() string { return "IndexOptimization" }
```

#### Behavior

- **Runs every**: 15 minutes (configurable)
- **Checks**: Dangling reference ratio in HNSW graph
- **Performs**: Entry point reselection and graph cleanup
- **Priority**: Medium-high (6/10)
- **Duration**: 10-500ms (depends on index size)

#### Tuning

```go
// Aggressive: Maintain perfect graph quality
IndexOptimization:   true
IndexOptInterval:    5 * time.Minute
IndexOptThreshold:   0.1

// Conservative: Tolerate more degradation
IndexOptimization:   true
IndexOptInterval:    30 * time.Minute
IndexOptThreshold:   0.3

// Disabled: Manual optimization only
IndexOptimization:   false
```

---

### 4. MetricsAggregationWorker

Aggregates and calculates derived metrics periodically.

#### Configuration

```go
MetricsAggregation:   true
MetricsInterval:      1 * time.Minute
```

#### Implementation

```go
type MetricsAggregationTask struct{}

func (t *MetricsAggregationTask) Execute(nest *Nest) error {
    metrics := nest.GetMetrics()

    // Calculate rates (per second)
    insertRate := calculateRate(metrics.GetCounter("inserts"))
    searchRate := calculateRate(metrics.GetCounter("searches"))

    // Update derived metrics
    metrics.GetGauge("insert_rate_per_sec").Set(int64(insertRate))
    metrics.GetGauge("search_rate_per_sec").Set(int64(searchRate))

    // Calculate percentiles (expensive, do periodically)
    searchLatency := metrics.GetHistogram("search_latency_ms")
    p95 := searchLatency.Percentile(0.95)
    p99 := searchLatency.Percentile(0.99)

    metrics.GetGauge("search_p95_ms").Set(int64(p95))
    metrics.GetGauge("search_p99_ms").Set(int64(p99))

    return nil
}

func (t *MetricsAggregationTask) Priority() int { return 3 }
func (t *MetricsAggregationTask) Name() string { return "MetricsAggregation" }
```

#### Behavior

- **Runs every**: 1 minute (configurable)
- **Calculates**: Rates, percentiles, derived metrics
- **Updates**: Gauge metrics for monitoring systems
- **Priority**: Low (3/10) - not critical
- **Duration**: < 10ms typically

#### Tuning

```go
// High-frequency: Real-time metrics
MetricsAggregation:   true
MetricsInterval:      10 * time.Second

// Low-frequency: Reduced overhead
MetricsAggregation:   true
MetricsInterval:      5 * time.Minute

// Disabled: Manual metric calculation
MetricsAggregation:   false
```

---

### 5. WALCheckpointWorker

Periodically flushes WAL to main database file and truncates log.

#### Configuration

```go
WALCheckpoint:         true
WALCheckpointInterval: 10 * time.Minute
WALCheckpointSize:     100 * 1024 * 1024  // 100MB
```

#### Implementation

```go
type WALCheckpointTask struct {
    maxSize int64
}

func (t *WALCheckpointTask) Execute(nest *Nest) error {
    if nest.wal == nil {
        return nil
    }

    walSize := nest.wal.Size()

    // Check if checkpoint needed
    if walSize < t.maxSize {
        return nil
    }

    log.Printf("WAL checkpoint triggered (size: %.2f MB)",
        float64(walSize)/1024/1024)

    start := time.Now()
    err := nest.wal.Checkpoint()
    duration := time.Since(start)

    if err != nil {
        log.Printf("WAL checkpoint failed: %v", err)
        return err
    }

    log.Printf("WAL checkpoint completed in %v, reclaimed %.2f MB",
        duration, float64(walSize)/1024/1024)

    return nil
}

func (t *WALCheckpointTask) Priority() int { return 8 }
func (t *WALCheckpointTask) Name() string { return "WALCheckpoint" }
```

#### Behavior

- **Runs every**: 10 minutes (configurable)
- **Checks**: WAL size
- **Triggers**: Checkpoint if size exceeds threshold
- **Priority**: High (8/10) - durability critical
- **Duration**: 100ms - 2s (depends on WAL size)

#### Tuning

```go
// Aggressive: Keep WAL small
WALCheckpoint:         true
WALCheckpointInterval: 5 * time.Minute
WALCheckpointSize:     50 * 1024 * 1024  // 50MB

// Conservative: Reduce checkpoint overhead
WALCheckpoint:         true
WALCheckpointInterval: 30 * time.Minute
WALCheckpointSize:     500 * 1024 * 1024  // 500MB

// Disabled: Manual checkpoint only
WALCheckpoint:         false
```

---

## Lifecycle Management

### Starting Workers

Workers start automatically when database opens with `Workers.Enabled: true`:

```go
nest, err := magpie.Open("vectors.magpie", magpie.Options{
    Dimensions: 128,
    Workers: magpie.WorkerConfig{
        Enabled: true,
        // ... other config ...
    },
})

// Workers start immediately in background
// No additional code needed
```

### Graceful Shutdown

Workers stop cleanly on `nest.Close()`:

```go
// Close database
err := nest.Close()

// This will:
// 1. Signal all workers to stop
// 2. Wait for current tasks to complete
// 3. Drain task queue
// 4. Close worker goroutines
// 5. Then close database

// No worker leaks or orphaned goroutines
```

### Implementation

```go
func (n *Nest) Close() error {
    // Signal workers to stop
    if n.workerPool != nil {
        n.workerPool.Stop()
    }

    // ... close other resources ...

    return nil
}

func (wp *WorkerPool) Stop() {
    // Signal all workers
    close(wp.stopChan)

    // Wait for workers to finish current tasks
    wp.wg.Wait()

    // Drain remaining tasks
    close(wp.taskQueue)
    for range wp.taskQueue {
        // Discard pending tasks
    }
}
```

---

## Monitoring

### Worker Status

Check worker status and health:

```go
// Get worker statistics
stats := nest.GetWorkerStats()

fmt.Printf("Workers: %d active\n", stats.ActiveWorkers)
fmt.Printf("Tasks executed: %d\n", stats.TasksExecuted)
fmt.Printf("Tasks failed: %d\n", stats.TasksFailed)
fmt.Printf("Queue depth: %d\n", stats.QueueDepth)

// Per-task statistics
for name, taskStats := range stats.Tasks {
    fmt.Printf("\nTask: %s\n", name)
    fmt.Printf("  Executions: %d\n", taskStats.Executions)
    fmt.Printf("  Failures: %d\n", taskStats.Failures)
    fmt.Printf("  Avg duration: %v\n", taskStats.AvgDuration)
    fmt.Printf("  Last run: %v\n", taskStats.LastRun)
}
```

### Logging

Workers log their activities:

```
2024-11-10 10:00:15 [Worker-1] Starting AutoCompaction task
2024-11-10 10:00:16 [Worker-1] Auto-compaction triggered (52.3% fragmentation)
2024-11-10 10:00:23 [Worker-1] Auto-compaction completed in 7.2s
2024-11-10 10:00:23 [Worker-1] Task completed successfully

2024-11-10 10:05:00 [Worker-2] Starting MVCC_GC task
2024-11-10 10:05:00 [Worker-2] MVCC GC: removed 1,234 versions (5,678 → 4,444)
2024-11-10 10:05:00 [Worker-2] Task completed successfully
```

### Metrics Integration

Workers export metrics:

```go
metrics := nest.GetMetrics()

// Task execution counts
compactions := metrics.GetCounter("worker.compaction.executions").Value()
gcRuns := metrics.GetCounter("worker.mvcc_gc.executions").Value()

// Task durations
compactionHist := metrics.GetHistogram("worker.compaction.duration_ms")
fmt.Printf("Compaction P95: %.2fms\n", compactionHist.Percentile(0.95))

// Error counts
compactionErrors := metrics.GetCounter("worker.compaction.errors").Value()
```

---

## Best Practices

### 1. Tune Intervals for Workload

```go
// High write rate: More frequent maintenance
Workers: WorkerConfig{
    CompactionInterval:    15 * time.Minute,
    MVCCGCInterval:        2 * time.Minute,
    IndexOptInterval:      10 * time.Minute,
}

// Low write rate: Less frequent maintenance
Workers: WorkerConfig{
    CompactionInterval:    2 * time.Hour,
    MVCCGCInterval:        15 * time.Minute,
    IndexOptInterval:      30 * time.Minute,
}

// Read-only: Disable most workers
Workers: WorkerConfig{
    AutoCompaction:        false,
    MVCCGCEnabled:         false,
    IndexOptimization:     false,
    MetricsAggregation:    true,  // Keep metrics
}
```

### 2. Adjust Worker Count

```go
// High-end server (8+ cores)
NumWorkers: 8

// Standard server (4 cores)
NumWorkers: 4

// Low-power system (2 cores)
NumWorkers: 2

// Embedded device (1 core)
NumWorkers: 1
```

### 3. Set Appropriate Thresholds

```go
// Development: Frequent compaction for testing
CompactionThreshold: 0.3  // Compact at 30%

// Production: Balance performance vs overhead
CompactionThreshold: 0.5  // Compact at 50%

// Cost-sensitive: Minimize compaction overhead
CompactionThreshold: 0.7  // Compact at 70%
```

### 4. Monitor Worker Performance

```go
func monitorWorkers(nest *magpie.Nest) {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        stats := nest.GetWorkerStats()

        // Check for task failures
        if stats.TasksFailed > 0 {
            log.Printf("WARNING: %d worker task failures", stats.TasksFailed)
        }

        // Check queue depth
        if stats.QueueDepth > 10 {
            log.Printf("WARNING: High task queue depth: %d", stats.QueueDepth)
        }

        // Log worker health
        log.Printf("Workers: %d active, %d tasks executed, %d queued",
            stats.ActiveWorkers, stats.TasksExecuted, stats.QueueDepth)
    }
}
```

### 5. Disable Workers in Tests

```go
// Test code - disable workers to avoid interference
func TestDatabaseOperations(t *testing.T) {
    nest, err := magpie.Open("test.magpie", magpie.Options{
        Dimensions: 128,
        Workers: magpie.WorkerConfig{
            Enabled: false,  // Disable all workers
        },
    })
    defer nest.Close()

    // ... test operations ...
}
```

---

## Troubleshooting

### Workers Not Starting

**Problem**: Workers configured but not running.

**Diagnosis**:
```go
stats := nest.GetWorkerStats()
if stats.ActiveWorkers == 0 {
    log.Printf("ERROR: No workers running")
}
```

**Solutions**:
1. Check `Workers.Enabled: true`
2. Check `Workers.NumWorkers > 0`
3. Check logs for startup errors
4. Verify database opened successfully

### High CPU Usage

**Problem**: Workers consuming too much CPU.

**Causes**:
1. Intervals too short
2. Too many workers
3. Tasks taking too long

**Solutions**:
```go
// Increase intervals
CompactionInterval:    2 * time.Hour  // Was 15 minutes
MVCCGCInterval:        15 * time.Minute  // Was 5 minutes

// Reduce worker count
NumWorkers: 2  // Was 4

// Disable non-essential workers
IndexOptimization: false
MetricsAggregation: false
```

### Goroutine Leaks

**Problem**: Goroutines not terminating on Close().

**Diagnosis**:
```go
import "runtime"

before := runtime.NumGoroutine()
nest.Close()
time.Sleep(1 * time.Second)
after := runtime.NumGoroutine()

if after >= before {
    log.Printf("WARNING: Possible goroutine leak (%d → %d)", before, after)
}
```

**This should not happen** - report as bug if observed.

**Workaround**: Ensure `nest.Close()` is called.

### Task Queue Backlog

**Problem**: Tasks queuing up faster than execution.

**Symptoms**:
```go
stats := nest.GetWorkerStats()
if stats.QueueDepth > 100 {
    log.Printf("WARNING: High queue depth: %d", stats.QueueDepth)
}
```

**Solutions**:
1. Increase worker count
2. Increase task intervals
3. Disable slow tasks
4. Investigate why tasks are slow

---

## Complete Example

Comprehensive worker configuration and monitoring:

```go
package main

import (
    "log"
    "time"
    "github.com/yourusername/magpiedb"
)

func main() {
    // Open with custom worker configuration
    nest, err := magpie.Open("vectors.magpie", magpie.Options{
        Dimensions: 128,
        Distance:   "cosine",
        Workers: magpie.WorkerConfig{
            Enabled:               true,
            NumWorkers:            4,

            AutoCompaction:        true,
            CompactionInterval:    30 * time.Minute,
            CompactionThreshold:   0.5,

            MVCCGCEnabled:         true,
            MVCCGCInterval:        5 * time.Minute,
            MVCCGCMaxVersions:     10,

            IndexOptimization:     true,
            IndexOptInterval:      15 * time.Minute,
            IndexOptThreshold:     0.2,

            MetricsAggregation:    true,
            MetricsInterval:       1 * time.Minute,

            WALCheckpoint:         true,
            WALCheckpointInterval: 10 * time.Minute,
            WALCheckpointSize:     100 * 1024 * 1024,
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    defer nest.Close()

    // Start monitoring
    go monitorWorkers(nest)

    // Simulate workload
    go simulateWorkload(nest)

    // Run for 1 hour
    time.Sleep(1 * time.Hour)
}

func monitorWorkers(nest *magpie.Nest) {
    ticker := time.NewTicker(2 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        stats := nest.GetWorkerStats()

        log.Printf("\n=== Worker Statistics ===")
        log.Printf("Active workers: %d", stats.ActiveWorkers)
        log.Printf("Tasks executed: %d", stats.TasksExecuted)
        log.Printf("Tasks failed: %d", stats.TasksFailed)
        log.Printf("Queue depth: %d", stats.QueueDepth)

        // Per-task stats
        for name, task := range stats.Tasks {
            log.Printf("\n%s:", name)
            log.Printf("  Executions: %d", task.Executions)
            log.Printf("  Failures: %d", task.Failures)
            log.Printf("  Avg duration: %v", task.AvgDuration)
            log.Printf("  Last run: %v ago", time.Since(task.LastRun))

            // Alert on failures
            if task.Failures > 0 {
                failRate := float64(task.Failures) / float64(task.Executions)
                if failRate > 0.1 {
                    log.Printf("  ⚠️  WARNING: High failure rate: %.1f%%", failRate*100)
                }
            }
        }

        // Alert on queue depth
        if stats.QueueDepth > 5 {
            log.Printf("\n⚠️  WARNING: High task queue depth: %d", stats.QueueDepth)
        }
    }
}

func simulateWorkload(nest *magpie.Nest) {
    for i := 0; ; i++ {
        // Insert
        nest.Store(fmt.Sprintf("vec_%d", i), randomVector(128))

        // Occasionally delete (creates fragmentation)
        if i > 0 && i%100 == 0 {
            nest.Remove(fmt.Sprintf("vec_%d", i-100))
        }

        time.Sleep(10 * time.Millisecond)
    }
}
```

---

## Related Documentation

- [METRICS.md](./METRICS.md) - Monitor worker performance
- [COMPACTION.md](./COMPACTION.md) - Understanding compaction worker
- [INDEX_OPERATIONS.md](./INDEX_OPERATIONS.md) - Index optimization worker
- [ARCHITECTURE.md](./ARCHITECTURE.md) - System architecture overview
