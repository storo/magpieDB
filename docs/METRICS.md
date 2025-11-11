# Metrics and Observability

## Overview

MagpieDB provides a comprehensive metrics system for monitoring database performance, health, and resource usage. The metrics implementation uses atomic operations for near-zero overhead performance tracking suitable for production environments.

### Key Features

- **Zero-overhead atomic counters**: Lock-free monotonic counters using atomic operations
- **Latency histograms**: Track operation latency distributions with percentile calculations
- **Resource gauges**: Monitor current resource usage (connections, memory, index size)
- **Database statistics**: Comprehensive view of database state and health
- **Health checks**: Automated health monitoring with degradation detection

### Why Use Metrics?

Metrics are essential for:
- **Performance monitoring**: Track operation latencies and throughput
- **Capacity planning**: Monitor resource usage trends
- **Anomaly detection**: Identify performance degradations early
- **Debugging**: Diagnose production issues with quantitative data
- **SLA tracking**: Measure and report on service level objectives

The implementation uses atomic operations (`atomic.Int64`) for counters and gauges, ensuring thread-safe access without lock contention.

---

## Components

### Counters

Counters are monotonically increasing metrics that track cumulative events. They never decrease (except on reset) and are perfect for counting operations like inserts, searches, and errors.

#### Counter API

```go
type Counter struct {
    value atomic.Int64
}

// Increment by 1
counter.Inc()

// Add a delta value
counter.Add(delta int64)

// Get current value
val := counter.Value()

// Reset to zero
counter.Reset()
```

#### Example: Tracking Operations

```go
metrics := nest.GetMetrics()

// Track insert operations
insertCounter := metrics.GetCounter("inserts")
insertCounter.Inc()

// Track bytes written
bytesCounter := metrics.GetCounter("bytes_written")
bytesCounter.Add(int64(len(data)))

// Check total inserts
totalInserts := insertCounter.Value()
fmt.Printf("Total inserts: %d\n", totalInserts)
```

#### Built-in Counters

MagpieDB automatically tracks these counters:
- `inserts`: Number of Store() operations
- `searches`: Number of Find() operations
- `deletes`: Number of Remove() operations
- `compactions`: Number of compaction operations
- `space_reclaimed_bytes`: Bytes recovered by compaction

#### Performance Characteristics

- **Time complexity**: O(1) for all operations
- **Thread safety**: Lock-free using atomic operations
- **Memory overhead**: 8 bytes per counter
- **Contention**: Minimal - uses atomic compare-and-swap

---

### Histograms

Histograms track the distribution of values over time, perfect for latency measurements and understanding performance characteristics.

#### Histogram API

```go
type Histogram struct {
    values []float64
    sum    float64
    min    float64
    max    float64
    mu     sync.Mutex
}

// Record a value
histogram.Record(value float64)

// Get count of recorded values
count := histogram.Count()

// Calculate percentiles (0.0 to 1.0)
p50 := histogram.Percentile(0.50)  // Median
p95 := histogram.Percentile(0.95)  // 95th percentile
p99 := histogram.Percentile(0.99)  // 99th percentile

// Get statistics
mean := histogram.Mean()
min := histogram.Min()
max := histogram.Max()

// Reset all values
histogram.Reset()
```

#### Example: Tracking Search Latency

```go
metrics := nest.GetMetrics()
searchLatency := metrics.GetHistogram("search_latency_ms")

// Time a search operation
start := time.Now()
results := nest.Find(queryVector, 10)
duration := time.Since(start)

// Record latency in milliseconds
searchLatency.Record(float64(duration.Milliseconds()))

// Analyze latency distribution
fmt.Printf("Search latency statistics:\n")
fmt.Printf("  Mean: %.2fms\n", searchLatency.Mean())
fmt.Printf("  P50:  %.2fms\n", searchLatency.Percentile(0.50))
fmt.Printf("  P95:  %.2fms\n", searchLatency.Percentile(0.95))
fmt.Printf("  P99:  %.2fms\n", searchLatency.Percentile(0.99))
fmt.Printf("  Min:  %.2fms\n", searchLatency.Min())
fmt.Printf("  Max:  %.2fms\n", searchLatency.Max())
```

#### Built-in Histograms

MagpieDB automatically tracks these histograms:
- `insert_latency_ms`: Store() operation latencies
- `search_latency_ms`: Find() operation latencies
- `delete_latency_ms`: Remove() operation latencies
- `compaction_duration_ms`: Compaction operation duration

#### Percentile Calculation

Percentiles are calculated using the nearest-rank method:
1. Sort all recorded values
2. Calculate index: `ceil(count * percentile) - 1`
3. Return value at that index

**Example**: For 100 values and p95:
- Index = ceil(100 * 0.95) - 1 = 94
- Returns the 95th value when sorted

#### Performance Characteristics

- **Recording**: O(1) append operation
- **Percentile calculation**: O(n log n) due to sorting (creates sorted copy)
- **Memory**: O(n) where n is number of recorded values
- **Best practice**: Reset periodically to prevent unbounded growth

---

### Gauges

Gauges represent current values that can increase or decrease. They're perfect for monitoring instantaneous state like active connections, memory usage, or queue length.

#### Gauge API

```go
type Gauge struct {
    value atomic.Int64
}

// Set to specific value
gauge.Set(value int64)

// Increment by 1
gauge.Inc()

// Decrement by 1
gauge.Dec()

// Add delta (positive or negative)
gauge.Add(delta int64)

// Get current value
val := gauge.Value()
```

#### Example: Monitoring Index Size

```go
metrics := nest.GetMetrics()
indexSizeGauge := metrics.GetGauge("index_size_bytes")

// Update gauge when index changes
func (n *Nest) Store(id string, vector []float32) error {
    // ... store logic ...

    // Update index size gauge
    stats, _ := n.GetStats()
    indexSizeGauge.Set(int64(stats.IndexSize))

    return nil
}

// Monitor index size
currentSize := indexSizeGauge.Value()
fmt.Printf("Current index size: %d bytes\n", currentSize)
```

#### Example: Tracking Active Connections

```go
metrics := nest.GetMetrics()
activeConnGauge := metrics.GetGauge("active_connections")

// Connection established
activeConnGauge.Inc()

// Connection closed
defer activeConnGauge.Dec()

// Monitor connections
connections := activeConnGauge.Value()
if connections > maxConnections {
    log.Warn("High connection count: %d", connections)
}
```

#### Performance Characteristics

- **Time complexity**: O(1) for all operations
- **Thread safety**: Lock-free using atomic operations
- **Memory overhead**: 8 bytes per gauge
- **Use case**: Current state that changes frequently

---

## Database Statistics

The `DatabaseStats` struct provides comprehensive database health and performance metrics.

### DatabaseStats Structure

```go
type DatabaseStats struct {
    VectorCount      uint64  // Total number of vectors stored
    IndexSize        uint64  // Size of HNSW index in bytes
    FragmentationPct float64 // Fragmentation percentage (0-100)
    VersionCount     uint64  // Number of MVCC versions
    WALSize          uint64  // Write-ahead log size in bytes
    StorageSize      uint64  // Total storage size in bytes
}
```

### Getting Statistics

```go
stats, err := nest.GetStats()
if err != nil {
    log.Fatalf("Failed to get stats: %v", err)
}

fmt.Printf("Database Statistics:\n")
fmt.Printf("  Vectors:        %d\n", stats.VectorCount)
fmt.Printf("  Index size:     %d bytes (%.2f MB)\n",
    stats.IndexSize, float64(stats.IndexSize)/1024/1024)
fmt.Printf("  Storage size:   %d bytes (%.2f MB)\n",
    stats.StorageSize, float64(stats.StorageSize)/1024/1024)
fmt.Printf("  Fragmentation:  %.1f%%\n", stats.FragmentationPct)
fmt.Printf("  MVCC versions:  %d\n", stats.VersionCount)
fmt.Printf("  WAL size:       %d bytes\n", stats.WALSize)
```

### Field Descriptions

#### VectorCount
Total number of vectors currently stored in the database. This is the count of live vectors, excluding deleted ones (after compaction).

#### IndexSize
Estimated memory usage of the HNSW index structure. Calculated based on:
- Node count
- Average neighbors per level (M * 2)
- Average number of levels (typically 2-4)
- Memory per neighbor reference (~72 bytes)

Formula: `nodeCount * avgNeighbors * avgLevels * 72`

#### FragmentationPct
Percentage of wasted space in the database file due to deletions and updates.

- **0-20%**: Healthy, minimal waste
- **20-50%**: Consider compaction during low-traffic periods
- **50%+**: High fragmentation, compaction recommended

Calculation: `(currentSize - estimatedOptimalSize) / estimatedOptimalSize * 100`

#### VersionCount
Number of MVCC (Multi-Version Concurrency Control) versions maintained for snapshot isolation. High counts indicate:
- Long-running transactions
- Frequent updates to same keys
- Need for MVCC garbage collection

#### WALSize
Current size of the Write-Ahead Log in bytes. Large WAL sizes indicate:
- Many uncommitted transactions
- WAL checkpoint needed
- High write throughput

#### StorageSize
Total database file size on disk, including:
- Vector data
- Index structures
- Metadata
- Free space (fragmentation)

### Example: Monitoring Dashboard

```go
func monitorDatabase(nest *magpie.Nest) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        stats, err := nest.GetStats()
        if err != nil {
            log.Printf("Error getting stats: %v", err)
            continue
        }

        // Check fragmentation
        if stats.FragmentationPct > 50 {
            log.Printf("WARNING: High fragmentation %.1f%%, consider compaction",
                stats.FragmentationPct)
        }

        // Check MVCC versions
        if stats.VersionCount > 10000 {
            log.Printf("WARNING: High MVCC version count: %d", stats.VersionCount)
        }

        // Check WAL size
        if stats.WALSize > 100*1024*1024 { // 100MB
            log.Printf("WARNING: Large WAL size: %.2f MB",
                float64(stats.WALSize)/1024/1024)
        }

        // Log dashboard
        log.Printf("Database: %d vectors, %.1f%% fragmentation, %d MB storage",
            stats.VectorCount, stats.FragmentationPct,
            stats.StorageSize/1024/1024)
    }
}
```

---

## Health Checks

The health check system provides automated monitoring of database health with three status levels.

### Health Status Levels

1. **healthy**: All systems operational
2. **degraded**: Performance issues detected but database is functional
3. **unhealthy**: Critical issues detected, database may not be operational

### HealthCheck Structure

```go
type HealthCheck struct {
    Status  string          // "healthy", "degraded", or "unhealthy"
    Checks  map[string]bool // Individual check results
    Message string          // Human-readable status description
}
```

### Performing Health Checks

```go
health, err := nest.HealthCheck()
if err != nil {
    log.Fatalf("Health check failed: %v", err)
}

fmt.Printf("Status: %s\n", health.Status)
fmt.Printf("Message: %s\n", health.Message)
fmt.Printf("\nIndividual Checks:\n")
for name, passing := range health.Checks {
    status := "✓"
    if !passing {
        status = "✗"
    }
    fmt.Printf("  %s %s\n", status, name)
}
```

### Built-in Health Checks

#### Storage Check
Verifies that the storage engine is initialized and accessible.
- **Pass**: Storage is initialized
- **Fail**: Storage is nil or inaccessible

#### Index Check
Verifies that the HNSW index is valid and contains vectors.
- **Pass**: Index exists and node count >= 0
- **Fail**: Index is nil or corrupted

#### Header Check
Verifies database header consistency.
- **Pass**: Header exists and vector count >= 0
- **Fail**: Header is nil or invalid

#### WAL Check
Verifies Write-Ahead Log is functional (if enabled).
- **Pass**: WAL is initialized (when enabled)
- **Fail**: WAL enabled but not initialized

#### Fragmentation Check
Monitors database fragmentation levels.
- **Pass**: Fragmentation <= 50%
- **Fail**: Fragmentation > 50% (sets status to "degraded")

### Example: Automated Health Monitoring

```go
func healthCheckLoop(nest *magpie.Nest, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for range ticker.C {
        health, err := nest.HealthCheck()
        if err != nil {
            log.Printf("ERROR: Health check failed: %v", err)
            // Alert on-call engineer
            alertOncall("health_check_failed", err)
            continue
        }

        switch health.Status {
        case "healthy":
            log.Printf("Status: HEALTHY - %s", health.Message)

        case "degraded":
            log.Printf("WARNING: Status DEGRADED - %s", health.Message)
            // Send alert but don't page
            sendAlert("database_degraded", health.Message)

        case "unhealthy":
            log.Printf("CRITICAL: Status UNHEALTHY - %s", health.Message)
            // Page on-call engineer
            alertOncall("database_unhealthy", health.Message)
        }

        // Check individual components
        for check, passing := range health.Checks {
            if !passing {
                log.Printf("FAILED CHECK: %s", check)
            }
        }
    }
}

// Usage
go healthCheckLoop(nest, 1*time.Minute)
```

### Example: HTTP Health Endpoint

```go
func healthHandler(nest *magpie.Nest) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        health, err := nest.HealthCheck()
        if err != nil {
            w.WriteHeader(http.StatusInternalServerError)
            json.NewEncoder(w).Encode(map[string]string{
                "status": "error",
                "error":  err.Error(),
            })
            return
        }

        // Set HTTP status based on health
        statusCode := http.StatusOK
        if health.Status == "degraded" {
            statusCode = http.StatusOK // 200 - still serving traffic
        } else if health.Status == "unhealthy" {
            statusCode = http.StatusServiceUnavailable // 503
        }

        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(statusCode)
        json.NewEncoder(w).Encode(health)
    }
}

// Register endpoint
http.HandleFunc("/health", healthHandler(nest))
```

---

## Integration with Operations

### Automatic Metric Tracking

MagpieDB automatically tracks metrics for all operations:

```go
// Store operation
func (n *Nest) Store(id string, vector []float32) error {
    start := time.Now()
    defer func() {
        n.trackInsert(time.Since(start))
    }()

    // ... storage logic ...

    return nil
}

// Find operation
func (n *Nest) Find(query []float32, k int) []Treasure {
    start := time.Now()
    defer func() {
        n.trackSearch(time.Since(start))
    }()

    // ... search logic ...

    return results
}
```

### Custom Metrics

You can add custom application-specific metrics:

```go
metrics := nest.GetMetrics()

// Custom counter for errors
errorCounter := metrics.GetCounter("application_errors")

// Track errors
func processVector(nest *magpie.Nest, id string, vec []float32) error {
    err := nest.Store(id, vec)
    if err != nil {
        errorCounter.Inc()
        return err
    }
    return nil
}

// Custom histogram for batch sizes
batchSizeHist := metrics.GetHistogram("batch_sizes")

func processBatch(nest *magpie.Nest, vectors map[string][]float32) error {
    batchSizeHist.Record(float64(len(vectors)))

    for id, vec := range vectors {
        if err := nest.Store(id, vec); err != nil {
            return err
        }
    }
    return nil
}
```

---

## Exporting Metrics

### Prometheus Integration

Export metrics to Prometheus for monitoring and alerting:

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

// Create Prometheus metrics
var (
    magpieInserts = prometheus.NewCounter(prometheus.CounterOpts{
        Name: "magpiedb_inserts_total",
        Help: "Total number of insert operations",
    })

    magpieSearchLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
        Name:    "magpiedb_search_latency_ms",
        Help:    "Search operation latency in milliseconds",
        Buckets: prometheus.ExponentialBuckets(1, 2, 10), // 1ms to 512ms
    })

    magpieVectorCount = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "magpiedb_vectors_total",
        Help: "Current number of vectors stored",
    })
)

func init() {
    prometheus.MustRegister(magpieInserts)
    prometheus.MustRegister(magpieSearchLatency)
    prometheus.MustRegister(magpieVectorCount)
}

// Sync MagpieDB metrics to Prometheus
func syncMetrics(nest *magpie.Nest) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        metrics := nest.GetMetrics()

        // Sync counters
        inserts := metrics.GetCounter("inserts").Value()
        magpieInserts.Add(float64(inserts))

        // Sync histograms (sample recent values)
        searchHist := metrics.GetHistogram("search_latency_ms")
        magpieSearchLatency.Observe(searchHist.Mean())

        // Sync gauges
        stats, _ := nest.GetStats()
        magpieVectorCount.Set(float64(stats.VectorCount))
    }
}

// HTTP endpoint for Prometheus scraping
http.Handle("/metrics", promhttp.Handler())
```

### Example Prometheus Queries

```promql
# Request rate (per second)
rate(magpiedb_inserts_total[5m])

# P95 search latency
histogram_quantile(0.95, rate(magpiedb_search_latency_ms_bucket[5m]))

# Vector count growth rate
rate(magpiedb_vectors_total[1h])

# Alert on high latency
magpiedb_search_latency_ms > 100
```

---

## Performance Overhead

### Atomic Operations Benchmark

Metrics use atomic operations which have minimal overhead:

```go
// BenchmarkCounterInc
// Result: ~2.5 ns/op (0.0000025 milliseconds)
counter := &Counter{}
counter.Inc()

// BenchmarkCounterIncParallel
// Result: ~15 ns/op even with 8 concurrent goroutines
```

### Overhead Analysis

**Counter operations**:
- Sequential: 2.5 nanoseconds per increment
- Concurrent (8 cores): 15 nanoseconds per increment
- **Impact**: Negligible - less than 0.001% of typical operation time

**Histogram operations**:
- Record: O(1) - simple append (~10 ns)
- Percentile: O(n log n) - sorting creates copy (~100 µs for 10k values)
- **Impact**: Recording is negligible, percentile calculation should be done periodically

**Best practices**:
1. Use counters freely - virtually zero overhead
2. Record to histograms on every operation - low overhead
3. Calculate percentiles periodically (every 10-60 seconds), not per operation
4. Reset histograms periodically to prevent unbounded memory growth

---

## Best Practices

### 1. Reset Histograms Periodically

```go
// Reset histograms every hour to prevent memory growth
ticker := time.NewTicker(1 * time.Hour)
go func() {
    for range ticker.C {
        metrics := nest.GetMetrics()
        metrics.GetHistogram("search_latency_ms").Reset()
        metrics.GetHistogram("insert_latency_ms").Reset()
    }
}()
```

### 2. Use Gauges for Current State

```go
// Good: Track current queue length
queueLength := metrics.GetGauge("queue_length")
queueLength.Set(int64(len(queue)))

// Bad: Using counter for current state (will only increase)
// queueLength := metrics.GetCounter("queue_length") // Wrong!
```

### 3. Export Metrics to External Systems

Don't rely solely on in-process metrics:

```go
// Export to Prometheus, StatsD, or CloudWatch
// This enables:
// - Historical analysis
// - Alerting
// - Correlation with other services
// - Dashboards
```

### 4. Monitor Health Continuously

```go
// Regular health checks in production
go func() {
    ticker := time.NewTicker(30 * time.Second)
    for range ticker.C {
        health, _ := nest.HealthCheck()
        if health.Status != "healthy" {
            log.Printf("Database health: %s - %s", health.Status, health.Message)
        }
    }
}()
```

### 5. Set Alerting Thresholds

```yaml
# Example Prometheus alerts
groups:
  - name: magpiedb
    rules:
      - alert: HighSearchLatency
        expr: magpiedb_search_latency_ms_p95 > 100
        for: 5m
        annotations:
          summary: "MagpieDB search latency is high"

      - alert: HighFragmentation
        expr: magpiedb_fragmentation_pct > 50
        for: 1h
        annotations:
          summary: "MagpieDB fragmentation above 50%"
```

---

## Complete Example

Here's a comprehensive example showing all metrics features:

```go
package main

import (
    "fmt"
    "log"
    "time"
    "github.com/yourusername/magpiedb"
)

func main() {
    // Open database
    nest, err := magpie.Open("vectors.magpie", magpie.Options{
        Dimensions: 128,
        Distance:   "cosine",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer nest.Close()

    // Start monitoring
    go monitorMetrics(nest)
    go healthMonitor(nest)

    // Perform operations
    for i := 0; i < 1000; i++ {
        vector := generateRandomVector(128)
        id := fmt.Sprintf("vec_%d", i)

        if err := nest.Store(id, vector); err != nil {
            log.Printf("Store error: %v", err)
        }
    }

    // Print metrics summary
    printMetricsSummary(nest)

    // Keep running for monitoring
    time.Sleep(5 * time.Minute)
}

func monitorMetrics(nest *magpie.Nest) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        metrics := nest.GetMetrics()

        // Get counters
        inserts := metrics.GetCounter("inserts").Value()
        searches := metrics.GetCounter("searches").Value()

        // Get latency percentiles
        searchLatency := metrics.GetHistogram("search_latency_ms")

        log.Printf("Metrics - Inserts: %d, Searches: %d, P95 Latency: %.2fms",
            inserts, searches, searchLatency.Percentile(0.95))
    }
}

func healthMonitor(nest *magpie.Nest) {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        health, err := nest.HealthCheck()
        if err != nil {
            log.Printf("Health check error: %v", err)
            continue
        }

        if health.Status != "healthy" {
            log.Printf("WARNING: Database %s - %s", health.Status, health.Message)
        }
    }
}

func printMetricsSummary(nest *magpie.Nest) {
    metrics := nest.GetMetrics()
    stats, _ := nest.GetStats()

    fmt.Println("\n=== Metrics Summary ===")

    // Counters
    fmt.Printf("Operations:\n")
    fmt.Printf("  Inserts:  %d\n", metrics.GetCounter("inserts").Value())
    fmt.Printf("  Searches: %d\n", metrics.GetCounter("searches").Value())
    fmt.Printf("  Deletes:  %d\n", metrics.GetCounter("deletes").Value())

    // Latencies
    fmt.Printf("\nSearch Latency:\n")
    searchLatency := metrics.GetHistogram("search_latency_ms")
    fmt.Printf("  Mean: %.2fms\n", searchLatency.Mean())
    fmt.Printf("  P50:  %.2fms\n", searchLatency.Percentile(0.50))
    fmt.Printf("  P95:  %.2fms\n", searchLatency.Percentile(0.95))
    fmt.Printf("  P99:  %.2fms\n", searchLatency.Percentile(0.99))

    // Database stats
    fmt.Printf("\nDatabase Statistics:\n")
    fmt.Printf("  Vectors:       %d\n", stats.VectorCount)
    fmt.Printf("  Storage:       %.2f MB\n", float64(stats.StorageSize)/1024/1024)
    fmt.Printf("  Fragmentation: %.1f%%\n", stats.FragmentationPct)
}

func generateRandomVector(dims int) []float32 {
    vec := make([]float32, dims)
    for i := range vec {
        vec[i] = rand.Float32()
    }
    return vec
}
```

---

## Related Documentation

- [ARCHITECTURE.md](./ARCHITECTURE.md) - Overall system architecture
- [INDEX_OPERATIONS.md](./INDEX_OPERATIONS.md) - Index operations and optimization
- [COMPACTION.md](./COMPACTION.md) - Database compaction and fragmentation
- [BACKGROUND_WORKERS.md](./BACKGROUND_WORKERS.md) - Automated background tasks
