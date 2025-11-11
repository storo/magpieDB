package magpie

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestCounterIncrement tests basic counter increment operations
func TestCounterIncrement(t *testing.T) {
	counter := &Counter{}

	counter.Inc()
	if counter.Value() != 1 {
		t.Errorf("Expected counter value 1, got %d", counter.Value())
	}

	counter.Inc()
	if counter.Value() != 2 {
		t.Errorf("Expected counter value 2, got %d", counter.Value())
	}
}

// TestCounterAdd tests adding multiple values to counter
func TestCounterAdd(t *testing.T) {
	counter := &Counter{}

	counter.Add(5)
	if counter.Value() != 5 {
		t.Errorf("Expected counter value 5, got %d", counter.Value())
	}

	counter.Add(10)
	if counter.Value() != 15 {
		t.Errorf("Expected counter value 15, got %d", counter.Value())
	}
}

// TestCounterReset tests counter reset functionality
func TestCounterReset(t *testing.T) {
	counter := &Counter{}

	counter.Add(100)
	counter.Reset()

	if counter.Value() != 0 {
		t.Errorf("Expected counter value 0 after reset, got %d", counter.Value())
	}
}

// TestCounterConcurrent tests counter operations under concurrent access
func TestCounterConcurrent(t *testing.T) {
	counter := &Counter{}
	goroutines := 100
	increments := 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < increments; j++ {
				counter.Inc()
			}
		}()
	}

	wg.Wait()

	expected := int64(goroutines * increments)
	if counter.Value() != expected {
		t.Errorf("Expected counter value %d, got %d", expected, counter.Value())
	}
}

// TestHistogramRecord tests basic histogram recording
func TestHistogramRecord(t *testing.T) {
	hist := &Histogram{}

	hist.Record(1.0)
	hist.Record(2.0)
	hist.Record(3.0)

	if hist.Count() != 3 {
		t.Errorf("Expected 3 values, got %d", hist.Count())
	}
}

// TestHistogramPercentiles tests percentile calculations
func TestHistogramPercentiles(t *testing.T) {
	hist := &Histogram{}

	// Record 100 values: 0.0, 0.01, 0.02, ..., 0.99
	for i := 0; i < 100; i++ {
		hist.Record(float64(i) / 100.0)
	}

	// Test p50 (median)
	p50 := hist.Percentile(0.50)
	if p50 < 0.49 || p50 > 0.51 {
		t.Errorf("Expected p50 ~0.50, got %.2f", p50)
	}

	// Test p99
	p99 := hist.Percentile(0.99)
	if p99 < 0.98 || p99 > 1.0 {
		t.Errorf("Expected p99 ~0.99, got %.2f", p99)
	}

	// Test p99.9
	p999 := hist.Percentile(0.999)
	if p999 < 0.98 || p999 > 1.0 {
		t.Errorf("Expected p99.9 ~0.99, got %.2f", p999)
	}
}

// TestHistogramMinMax tests min and max value tracking
func TestHistogramMinMax(t *testing.T) {
	hist := &Histogram{}

	hist.Record(5.0)
	hist.Record(1.0)
	hist.Record(10.0)
	hist.Record(3.0)

	if hist.Min() != 1.0 {
		t.Errorf("Expected min 1.0, got %.2f", hist.Min())
	}

	if hist.Max() != 10.0 {
		t.Errorf("Expected max 10.0, got %.2f", hist.Max())
	}
}

// TestHistogramMean tests mean calculation
func TestHistogramMean(t *testing.T) {
	hist := &Histogram{}

	hist.Record(1.0)
	hist.Record(2.0)
	hist.Record(3.0)
	hist.Record(4.0)
	hist.Record(5.0)

	mean := hist.Mean()
	expected := 3.0

	if mean < expected-0.01 || mean > expected+0.01 {
		t.Errorf("Expected mean %.2f, got %.2f", expected, mean)
	}
}

// TestHistogramReset tests histogram reset
func TestHistogramReset(t *testing.T) {
	hist := &Histogram{}

	hist.Record(1.0)
	hist.Record(2.0)
	hist.Record(3.0)

	hist.Reset()

	if hist.Count() != 0 {
		t.Errorf("Expected count 0 after reset, got %d", hist.Count())
	}
}

// TestGaugeSetGet tests basic gauge operations
func TestGaugeSetGet(t *testing.T) {
	gauge := &Gauge{}

	gauge.Set(42)
	if gauge.Value() != 42 {
		t.Errorf("Expected gauge value 42, got %d", gauge.Value())
	}

	gauge.Set(100)
	if gauge.Value() != 100 {
		t.Errorf("Expected gauge value 100, got %d", gauge.Value())
	}
}

// TestGaugeIncDec tests gauge increment and decrement
func TestGaugeIncDec(t *testing.T) {
	gauge := &Gauge{}

	gauge.Set(10)
	gauge.Inc()

	if gauge.Value() != 11 {
		t.Errorf("Expected gauge value 11, got %d", gauge.Value())
	}

	gauge.Dec()
	if gauge.Value() != 10 {
		t.Errorf("Expected gauge value 10, got %d", gauge.Value())
	}
}

// TestGaugeAdd tests gauge add operation
func TestGaugeAdd(t *testing.T) {
	gauge := &Gauge{}

	gauge.Set(10)
	gauge.Add(5)

	if gauge.Value() != 15 {
		t.Errorf("Expected gauge value 15, got %d", gauge.Value())
	}

	gauge.Add(-3)
	if gauge.Value() != 12 {
		t.Errorf("Expected gauge value 12, got %d", gauge.Value())
	}
}

// TestMetricsCounters tests metrics counter registration and retrieval
func TestMetricsCounters(t *testing.T) {
	metrics := NewMetrics()

	counter := metrics.GetCounter("test_counter")
	if counter == nil {
		t.Fatal("Expected counter to be created")
	}

	counter.Inc()

	// Retrieve same counter
	counter2 := metrics.GetCounter("test_counter")
	if counter2.Value() != 1 {
		t.Errorf("Expected counter value 1, got %d", counter2.Value())
	}
}

// TestMetricsHistograms tests metrics histogram registration and retrieval
func TestMetricsHistograms(t *testing.T) {
	metrics := NewMetrics()

	hist := metrics.GetHistogram("test_histogram")
	if hist == nil {
		t.Fatal("Expected histogram to be created")
	}

	hist.Record(1.5)

	// Retrieve same histogram
	hist2 := metrics.GetHistogram("test_histogram")
	if hist2.Count() != 1 {
		t.Errorf("Expected histogram count 1, got %d", hist2.Count())
	}
}

// TestMetricsGauges tests metrics gauge registration and retrieval
func TestMetricsGauges(t *testing.T) {
	metrics := NewMetrics()

	gauge := metrics.GetGauge("test_gauge")
	if gauge == nil {
		t.Fatal("Expected gauge to be created")
	}

	gauge.Set(42)

	// Retrieve same gauge
	gauge2 := metrics.GetGauge("test_gauge")
	if gauge2.Value() != 42 {
		t.Errorf("Expected gauge value 42, got %d", gauge2.Value())
	}
}

// TestMetricsReset tests resetting all metrics
func TestMetricsReset(t *testing.T) {
	metrics := NewMetrics()

	metrics.GetCounter("counter1").Inc()
	metrics.GetCounter("counter2").Add(5)
	metrics.GetHistogram("hist1").Record(1.0)
	metrics.GetGauge("gauge1").Set(10)

	metrics.Reset()

	// All metrics should be reset
	if metrics.GetCounter("counter1").Value() != 0 {
		t.Error("Counter should be reset to 0")
	}
	if metrics.GetHistogram("hist1").Count() != 0 {
		t.Error("Histogram should be reset")
	}
	if metrics.GetGauge("gauge1").Value() != 0 {
		t.Error("Gauge should be reset to 0")
	}
}

// TestDatabaseStats tests database statistics collection
func TestDatabaseStats(t *testing.T) {
	nest := setupTestDB(t)
	defer cleanupTestDB(t, nest)

	// Insert some vectors
	for i := 0; i < 10; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = float32(i) * 0.1
		}
		id := fmt.Sprintf("vector_%d", i)
		err := nest.Store(id, vector)
		if err != nil {
			t.Fatalf("Failed to store vector: %v", err)
		}
	}

	stats, err := nest.GetStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	if stats.VectorCount != 10 {
		t.Errorf("Expected VectorCount 10, got %d", stats.VectorCount)
	}

	if stats.IndexSize == 0 {
		t.Error("Expected non-zero IndexSize")
	}

	if stats.StorageSize == 0 {
		t.Error("Expected non-zero StorageSize")
	}
}

// TestHealthCheckHealthy tests health check on healthy database
func TestHealthCheckHealthy(t *testing.T) {
	nest := setupTestDB(t)
	defer cleanupTestDB(t, nest)

	health, err := nest.HealthCheck()
	if err != nil {
		t.Fatalf("Failed to perform health check: %v", err)
	}

	if health.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", health.Status)
	}

	if !health.Checks["storage"] {
		t.Error("Storage check should pass")
	}

	if !health.Checks["index"] {
		t.Error("Index check should pass")
	}
}

// TestHealthCheckDegraded tests health check with high fragmentation
func TestHealthCheckDegraded(t *testing.T) {
	// This test would require simulating fragmentation
	// Skipping for now - will implement once compaction is complete
	t.Skip("Requires compaction feature")
}

// TestMetricsIntegration tests metrics collection during database operations
func TestMetricsIntegration(t *testing.T) {
	nest := setupTestDB(t)
	defer cleanupTestDB(t, nest)

	metrics := nest.GetMetrics()
	if metrics == nil {
		t.Fatal("Expected metrics to be initialized")
	}

	// Store a vector
	vector := make([]float32, 128)
	err := nest.Store("test1", vector)
	if err != nil {
		t.Fatalf("Failed to store vector: %v", err)
	}

	// Check that insert counter was incremented
	insertCounter := metrics.GetCounter("inserts")
	if insertCounter.Value() < 1 {
		t.Errorf("Expected at least 1 insert, got %d", insertCounter.Value())
	}

	// Perform search
	results := nest.Find(vector, 5)
	if len(results) == 0 {
		t.Error("Expected to find at least one result")
	}

	// Check that search counter was incremented
	searchCounter := metrics.GetCounter("searches")
	if searchCounter.Value() < 1 {
		t.Errorf("Expected at least 1 search, got %d", searchCounter.Value())
	}
}

// TestMetricsLatency tests latency histogram tracking
func TestMetricsLatency(t *testing.T) {
	nest := setupTestDB(t)
	defer cleanupTestDB(t, nest)

	// Perform operations
	vector := make([]float32, 128)
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("latency_vec_%d", i)
		_ = nest.Store(id, vector)
	}

	metrics := nest.GetMetrics()
	insertLatency := metrics.GetHistogram("insert_latency_ms")

	if insertLatency.Count() != 10 {
		t.Errorf("Expected 10 latency measurements, got %d", insertLatency.Count())
	}

	// Check that latencies are reasonable (< 100ms)
	if insertLatency.Max() > 100.0 {
		t.Errorf("Insert latency too high: %.2fms", insertLatency.Max())
	}
}

// TestMetricsConcurrent tests metrics under concurrent access
func TestMetricsConcurrent(t *testing.T) {
	metrics := NewMetrics()
	counter := metrics.GetCounter("concurrent_test")

	goroutines := 50
	increments := 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < increments; j++ {
				counter.Inc()
			}
		}()
	}

	wg.Wait()

	expected := int64(goroutines * increments)
	if counter.Value() != expected {
		t.Errorf("Expected counter value %d, got %d", expected, counter.Value())
	}
}

// BenchmarkCounterInc benchmarks counter increment performance
func BenchmarkCounterInc(b *testing.B) {
	counter := &Counter{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		counter.Inc()
	}
}

// BenchmarkCounterIncParallel benchmarks concurrent counter increments
func BenchmarkCounterIncParallel(b *testing.B) {
	counter := &Counter{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			counter.Inc()
		}
	})
}

// BenchmarkHistogramRecord benchmarks histogram recording
func BenchmarkHistogramRecord(b *testing.B) {
	hist := &Histogram{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hist.Record(float64(i) * 0.001)
	}
}

// BenchmarkMetricsOverhead benchmarks metrics collection overhead
func BenchmarkMetricsOverhead(b *testing.B) {
	metrics := NewMetrics()
	counter := metrics.GetCounter("bench_counter")
	hist := metrics.GetHistogram("bench_histogram")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		counter.Inc()
		duration := time.Since(start).Seconds() * 1000
		hist.Record(duration)
	}
}

// Helper functions

func setupTestDB(t *testing.T) *Nest {
	// Use a unique path for each test
	path := filepath.Join(os.TempDir(), fmt.Sprintf("test_metrics_%s.magpie", t.Name()))

	// Clean up any existing file
	cleanupPath(path)

	nest, err := Open(path, Options{
		Dimensions: 128,
		Distance:   "cosine",
		WAL:        false,
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	return nest
}

func cleanupTestDB(t *testing.T, nest *Nest) {
	path := nest.path
	nest.Close()
	cleanupPath(path)
}
