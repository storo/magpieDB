package magpie

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Counter is a monotonically increasing metric counter
type Counter struct {
	value atomic.Int64
}

// Inc increments the counter by 1
func (c *Counter) Inc() {
	c.value.Add(1)
}

// Add increments the counter by the given delta
func (c *Counter) Add(delta int64) {
	c.value.Add(delta)
}

// Value returns the current counter value
func (c *Counter) Value() int64 {
	return c.value.Load()
}

// Reset resets the counter to zero
func (c *Counter) Reset() {
	c.value.Store(0)
}

// Histogram tracks distribution of values
type Histogram struct {
	values []float64
	sum    float64
	min    float64
	max    float64
	mu     sync.Mutex
}

// Record adds a value to the histogram
func (h *Histogram) Record(value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.values = append(h.values, value)
	h.sum += value

	if len(h.values) == 1 {
		h.min = value
		h.max = value
	} else {
		if value < h.min {
			h.min = value
		}
		if value > h.max {
			h.max = value
		}
	}
}

// Count returns the number of recorded values
func (h *Histogram) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.values)
}

// Percentile calculates the given percentile (0.0 to 1.0)
func (h *Histogram) Percentile(p float64) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.values) == 0 {
		return 0
	}

	// Create a sorted copy
	sorted := make([]float64, len(h.values))
	copy(sorted, h.values)
	sort.Float64s(sorted)

	// Calculate index
	index := int(math.Ceil(float64(len(sorted))*p)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}

	return sorted[index]
}

// Min returns the minimum value
func (h *Histogram) Min() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.values) == 0 {
		return 0
	}
	return h.min
}

// Max returns the maximum value
func (h *Histogram) Max() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.values) == 0 {
		return 0
	}
	return h.max
}

// Mean returns the average value
func (h *Histogram) Mean() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.values) == 0 {
		return 0
	}
	return h.sum / float64(len(h.values))
}

// Reset clears all recorded values
func (h *Histogram) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.values = nil
	h.sum = 0
	h.min = 0
	h.max = 0
}

// Gauge is a metric that can go up or down
type Gauge struct {
	value atomic.Int64
}

// Set sets the gauge to a specific value
func (g *Gauge) Set(value int64) {
	g.value.Store(value)
}

// Inc increments the gauge by 1
func (g *Gauge) Inc() {
	g.value.Add(1)
}

// Dec decrements the gauge by 1
func (g *Gauge) Dec() {
	g.value.Add(-1)
}

// Add adds (or subtracts if negative) a delta to the gauge
func (g *Gauge) Add(delta int64) {
	g.value.Add(delta)
}

// Value returns the current gauge value
func (g *Gauge) Value() int64 {
	return g.value.Load()
}

// Metrics is a collection of all database metrics
type Metrics struct {
	counters   map[string]*Counter
	histograms map[string]*Histogram
	gauges     map[string]*Gauge
	mu         sync.RWMutex
}

// NewMetrics creates a new metrics collection
func NewMetrics() *Metrics {
	return &Metrics{
		counters:   make(map[string]*Counter),
		histograms: make(map[string]*Histogram),
		gauges:     make(map[string]*Gauge),
	}
}

// GetCounter returns a counter by name, creating it if necessary
func (m *Metrics) GetCounter(name string) *Counter {
	m.mu.RLock()
	counter, exists := m.counters[name]
	m.mu.RUnlock()

	if exists {
		return counter
	}

	// Need write lock to create new counter
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	counter, exists = m.counters[name]
	if exists {
		return counter
	}

	counter = &Counter{}
	m.counters[name] = counter
	return counter
}

// GetHistogram returns a histogram by name, creating it if necessary
func (m *Metrics) GetHistogram(name string) *Histogram {
	m.mu.RLock()
	hist, exists := m.histograms[name]
	m.mu.RUnlock()

	if exists {
		return hist
	}

	// Need write lock to create new histogram
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	hist, exists = m.histograms[name]
	if exists {
		return hist
	}

	hist = &Histogram{}
	m.histograms[name] = hist
	return hist
}

// GetGauge returns a gauge by name, creating it if necessary
func (m *Metrics) GetGauge(name string) *Gauge {
	m.mu.RLock()
	gauge, exists := m.gauges[name]
	m.mu.RUnlock()

	if exists {
		return gauge
	}

	// Need write lock to create new gauge
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	gauge, exists = m.gauges[name]
	if exists {
		return gauge
	}

	gauge = &Gauge{}
	m.gauges[name] = gauge
	return gauge
}

// Reset resets all metrics
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, counter := range m.counters {
		counter.Reset()
	}

	for _, hist := range m.histograms {
		hist.Reset()
	}

	for _, gauge := range m.gauges {
		gauge.Set(0)
	}
}

// DatabaseStats contains database statistics
type DatabaseStats struct {
	VectorCount      uint64  // Total number of vectors
	IndexSize        uint64  // Size of HNSW index in bytes
	FragmentationPct float64 // Fragmentation percentage (0-100)
	VersionCount     uint64  // Number of MVCC versions
	WALSize          uint64  // Write-ahead log size in bytes
	StorageSize      uint64  // Total storage size in bytes
}

// HealthCheck contains database health information
type HealthCheck struct {
	Status  string          // "healthy", "degraded", "unhealthy"
	Checks  map[string]bool // Individual check results
	Message string          // Human-readable status message
}

// GetMetrics returns the metrics collection for this database
func (n *Nest) GetMetrics() *Metrics {
	n.mu.RLock()
	defer n.mu.RUnlock()

	// Initialize metrics if not already done
	if n.metrics == nil {
		n.mu.RUnlock()
		n.mu.Lock()
		if n.metrics == nil {
			n.metrics = NewMetrics()
		}
		n.mu.Unlock()
		n.mu.RLock()
	}

	return n.metrics
}

// Stats returns database statistics (convenience method)
func (n *Nest) Stats() *DatabaseStats {
	stats, _ := n.GetStats()
	return stats
}

// GetStats returns database statistics
func (n *Nest) GetStats() (*DatabaseStats, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.closed {
		return nil, ErrDatabaseClosed
	}

	stats := &DatabaseStats{
		VectorCount: n.header.VectorCount,
	}

	// Calculate index size (approximate)
	if n.index != nil {
		nodeCount := n.index.Count()
		avgNeighborsPerLevel := n.index.m * 2
		avgLevels := 3 // Typical HNSW has 2-4 levels
		// Each neighbor reference is ~8 bytes (pointer) + ID string (~64 bytes)
		stats.IndexSize = uint64(nodeCount * avgNeighborsPerLevel * avgLevels * 72)
	}

	// Calculate storage size
	if n.storage != nil {
		stats.StorageSize = uint64(n.storage.Size())
	}

	// Calculate fragmentation
	if n.storage != nil {
		currentSize := n.storage.Size()
		estimatedSize := n.estimateSizeMetrics()
		if estimatedSize > 0 && currentSize > estimatedSize {
			stats.FragmentationPct = float64(currentSize-estimatedSize) / float64(estimatedSize) * 100
		}
	}

	// Count MVCC versions
	if n.mvcc != nil {
		stats.VersionCount = uint64(n.mvcc.VersionCount())
	}

	// Calculate WAL size
	if n.wal != nil {
		stats.WALSize = uint64(n.wal.Size())
	}

	return stats, nil
}

// HealthCheck performs a health check on the database
func (n *Nest) HealthCheck() (*HealthCheck, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.closed {
		return nil, ErrDatabaseClosed
	}

	health := &HealthCheck{
		Status: "healthy",
		Checks: make(map[string]bool),
	}

	// Check storage
	if n.storage != nil {
		health.Checks["storage"] = true
	} else {
		health.Checks["storage"] = false
		health.Status = "unhealthy"
	}

	// Check index
	if n.index != nil && n.index.Count() >= 0 {
		health.Checks["index"] = true
	} else {
		health.Checks["index"] = false
		health.Status = "unhealthy"
	}

	// Check header consistency
	if n.header != nil && n.header.VectorCount >= 0 {
		health.Checks["header"] = true
	} else {
		health.Checks["header"] = false
		health.Status = "unhealthy"
	}

	// Check WAL if enabled
	if n.wal != nil {
		health.Checks["wal"] = true
	}

	// Check fragmentation
	if n.storage != nil {
		currentSize := n.storage.Size()
		estimatedSize := n.estimateSizeMetrics()
		if estimatedSize > 0 {
			fragmentation := float64(currentSize-estimatedSize) / float64(estimatedSize)
			if fragmentation > 0.5 {
				health.Status = "degraded"
				health.Message = fmt.Sprintf("High fragmentation: %.1f%%", fragmentation*100)
			}
			health.Checks["fragmentation"] = fragmentation <= 0.5
		}
	}

	// Set message if healthy
	if health.Status == "healthy" {
		health.Message = "All systems operational"
	}

	return health, nil
}

// trackInsert tracks an insert operation in metrics
func (n *Nest) trackInsert(duration time.Duration) {
	if n.metrics == nil {
		return
	}

	n.metrics.GetCounter("inserts").Inc()
	n.metrics.GetHistogram("insert_latency_ms").Record(float64(duration.Milliseconds()))
}

// trackSearch tracks a search operation in metrics
func (n *Nest) trackSearch(duration time.Duration) {
	if n.metrics == nil {
		return
	}

	n.metrics.GetCounter("searches").Inc()
	n.metrics.GetHistogram("search_latency_ms").Record(float64(duration.Milliseconds()))
}

// trackDelete tracks a delete operation in metrics
func (n *Nest) trackDelete(duration time.Duration) {
	if n.metrics == nil {
		return
	}

	n.metrics.GetCounter("deletes").Inc()
	n.metrics.GetHistogram("delete_latency_ms").Record(float64(duration.Milliseconds()))
}

// trackCompaction tracks a compaction operation in metrics
func (n *Nest) trackCompaction(duration time.Duration, spaceReclaimed int64) {
	if n.metrics == nil {
		return
	}

	n.metrics.GetCounter("compactions").Inc()
	n.metrics.GetHistogram("compaction_duration_ms").Record(float64(duration.Milliseconds()))
	n.metrics.GetCounter("space_reclaimed_bytes").Add(spaceReclaimed)
}

// estimateSize estimates the minimum required size for stored data
func (n *Nest) estimateSizeMetrics() int64 {
	// Estimate based on vector count and dimensions
	vectorCount := int64(n.header.VectorCount)
	dimensions := int64(n.header.Dimensions)

	// Base size: vector data (4 bytes per dimension) + ID (assume 64 bytes) + metadata overhead
	bytesPerVector := dimensions*4 + 64 + 128 // vector + ID + metadata overhead

	return vectorCount * bytesPerVector
}
