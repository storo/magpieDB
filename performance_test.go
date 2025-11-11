package magpie

import (
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"testing"
	"time"
)

// ===== INSERTION BENCHMARKS =====

func BenchmarkInsert1KVectors(b *testing.B) {
	benchmarkInsertN(b, 1000, 128)
}

func BenchmarkInsert10KVectors(b *testing.B) {
	benchmarkInsertN(b, 10000, 128)
}

func BenchmarkInsert100KVectors(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping 100K insert in short mode")
	}
	benchmarkInsertN(b, 100000, 128)
}

func BenchmarkInsert1MVectors(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping 1M insert in short mode")
	}
	benchmarkInsertN(b, 1000000, 128)
}

func benchmarkInsertN(b *testing.B, n, dims int) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		path := fmt.Sprintf("/tmp/bench_insert_%d_%d.magpie", n, i)
		defer os.Remove(path)

		nest, err := Open(path, Options{
			Dimensions: dims,
			M:          16,
			EfConstruction: 200,
		})
		if err != nil {
			b.Fatal(err)
		}

		vectors := generateVectors(n, dims)

		b.StartTimer()
		start := time.Now()

		for j := 0; j < n; j++ {
			id := fmt.Sprintf("vec%d", j)
			if err := nest.Store(id, vectors[j]); err != nil {
				b.Fatal(err)
			}
		}

		elapsed := time.Since(start)
		b.StopTimer()

		nest.Close()

		opsPerSec := float64(n) / elapsed.Seconds()
		msPerInsert := float64(elapsed.Milliseconds()) / float64(n)

		b.ReportMetric(opsPerSec, "inserts/sec")
		b.ReportMetric(msPerInsert, "ms/insert")
		b.ReportMetric(float64(elapsed.Milliseconds()), "total_ms")
	}
}

// ===== SEARCH BENCHMARKS =====

func BenchmarkSearch1K_K10(b *testing.B) {
	benchmarkSearchNK(b, 1000, 10, 128)
}

func BenchmarkSearch10K_K10(b *testing.B) {
	benchmarkSearchNK(b, 10000, 10, 128)
}

func BenchmarkSearch100K_K10(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping 100K search in short mode")
	}
	benchmarkSearchNK(b, 100000, 10, 128)
}

func BenchmarkSearch1M_K10(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping 1M search in short mode")
	}
	benchmarkSearchNK(b, 1000000, 10, 128)
}

func BenchmarkSearch1M_K100(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping 1M search in short mode")
	}
	benchmarkSearchNK(b, 1000000, 100, 128)
}

func benchmarkSearchNK(b *testing.B, n, k, dims int) {
	path := fmt.Sprintf("/tmp/bench_search_%d.magpie", n)
	defer os.Remove(path)

	nest := setupBenchmarkDB(b, path, n, dims)
	defer nest.Close()

	query := make([]float32, dims)
	for i := range query {
		query[i] = rand.Float32()
	}

	b.ResetTimer()
	b.ReportAllocs()

	start := time.Now()
	for i := 0; i < b.N; i++ {
		results := nest.Find(query, k)
		if len(results) == 0 {
			b.Fatal("no results")
		}
	}
	elapsed := time.Since(start)

	b.StopTimer()

	opsPerSec := float64(b.N) / elapsed.Seconds()
	avgLatency := float64(elapsed.Microseconds()) / float64(b.N)

	b.ReportMetric(opsPerSec, "searches/sec")
	b.ReportMetric(avgLatency, "us/search")
}

// ===== PARALLEL SEARCH BENCHMARKS =====

func BenchmarkParallelSearch1M_K10(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping parallel search in short mode")
	}

	path := "/tmp/bench_parallel_1m.magpie"
	defer os.Remove(path)

	nest := setupBenchmarkDB(b, path, 1000000, 128)
	defer nest.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		query := make([]float32, 128)
		for i := range query {
			query[i] = rand.Float32()
		}

		for pb.Next() {
			nest.Find(query, 10)
		}
	})
}

func BenchmarkParallelSearch100K_K10(b *testing.B) {
	path := "/tmp/bench_parallel_100k.magpie"
	defer os.Remove(path)

	nest := setupBenchmarkDB(b, path, 100000, 128)
	defer nest.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		query := make([]float32, 128)
		for i := range query {
			query[i] = rand.Float32()
		}

		for pb.Next() {
			nest.Find(query, 10)
		}
	})
}

// ===== MEMORY BENCHMARKS =====

func BenchmarkMemoryUsage1M(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping memory test in short mode")
	}

	path := "/tmp/bench_memory_1m.magpie"
	defer os.Remove(path)

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	nest, err := Open(path, Options{Dimensions: 128})
	if err != nil {
		b.Fatal(err)
	}

	// Insert 1M vectors
	for i := 0; i < 1000000; i++ {
		id := fmt.Sprintf("vec%d", i)
		vec := make([]float32, 128)
		for j := range vec {
			vec[j] = rand.Float32()
		}
		if err := nest.Store(id, vec); err != nil {
			b.Fatal(err)
		}

		if i%100000 == 0 && i > 0 {
			runtime.GC()
		}
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	nest.Close()

	memoryUsed := m2.Alloc - m1.Alloc
	memoryMB := float64(memoryUsed) / (1024 * 1024)

	b.ReportMetric(memoryMB, "MB")
	b.Logf("Memory used: %.2f MB", memoryMB)
	b.Logf("Memory per vector: %.2f bytes", float64(memoryUsed)/1000000.0)
}

func BenchmarkMemoryUsage100K(b *testing.B) {
	path := "/tmp/bench_memory_100k.magpie"
	defer os.Remove(path)

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	nest, err := Open(path, Options{Dimensions: 128})
	if err != nil {
		b.Fatal(err)
	}

	// Insert 100K vectors
	for i := 0; i < 100000; i++ {
		id := fmt.Sprintf("vec%d", i)
		vec := make([]float32, 128)
		for j := range vec {
			vec[j] = rand.Float32()
		}
		if err := nest.Store(id, vec); err != nil {
			b.Fatal(err)
		}
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	nest.Close()

	memoryUsed := m2.Alloc - m1.Alloc
	memoryMB := float64(memoryUsed) / (1024 * 1024)

	b.ReportMetric(memoryMB, "MB")
	b.Logf("Memory used: %.2f MB", memoryMB)
}

// ===== CACHE BENCHMARKS =====

func BenchmarkCacheHitRate(b *testing.B) {
	path := "/tmp/bench_cache.magpie"
	defer os.Remove(path)

	nest, err := Open(path, Options{
		Dimensions:       128,
		EnableQueryCache: true,
		QueryCacheSize:   1000,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer nest.Close()

	// Insert 10K vectors
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("vec%d", i)
		vec := make([]float32, 128)
		for j := range vec {
			vec[j] = rand.Float32()
		}
		nest.Store(id, vec)
	}

	query := make([]float32, 128)
	for i := range query {
		query[i] = rand.Float32()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nest.Find(query, 10) // Same query = cache hits
	}

	if nest.queryCache != nil {
		hits, misses, hitRate := nest.queryCache.Stats()
		b.ReportMetric(hitRate*100, "hit_rate_%")
		b.Logf("Cache hits: %d, misses: %d, hit rate: %.2f%%", hits, misses, hitRate*100)
	}
}

func BenchmarkCacheVaryingQueries(b *testing.B) {
	path := "/tmp/bench_cache_varying.magpie"
	defer os.Remove(path)

	nest, err := Open(path, Options{
		Dimensions:       128,
		EnableQueryCache: true,
		QueryCacheSize:   100,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer nest.Close()

	// Insert 10K vectors
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("vec%d", i)
		vec := make([]float32, 128)
		nest.Store(id, vec)
	}

	// Create 50 different queries
	queries := make([][]float32, 50)
	for i := range queries {
		queries[i] = make([]float32, 128)
		for j := range queries[i] {
			queries[i][j] = rand.Float32()
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Use queries in rotation to test cache effectiveness
		query := queries[i%len(queries)]
		nest.Find(query, 10)
	}

	if nest.queryCache != nil {
		hits, misses, hitRate := nest.queryCache.Stats()
		b.ReportMetric(hitRate*100, "hit_rate_%")
		b.Logf("Cache hits: %d, misses: %d, hit rate: %.2f%%", hits, misses, hitRate*100)
	}
}

// ===== THROUGHPUT BENCHMARKS =====

func BenchmarkMixedWorkload1M(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping mixed workload in short mode")
	}

	path := "/tmp/bench_mixed_1m.magpie"
	defer os.Remove(path)

	nest := setupBenchmarkDB(b, path, 1000000, 128)
	defer nest.Close()

	query := make([]float32, 128)
	for i := range query {
		query[i] = rand.Float32()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 70% reads, 20% writes, 10% deletes
		op := rand.Intn(10)
		switch {
		case op < 7: // Read
			nest.Find(query, 10)
		case op < 9: // Write
			id := fmt.Sprintf("new_vec_%d", i)
			vec := make([]float32, 128)
			nest.Store(id, vec)
		default: // Delete
			id := fmt.Sprintf("vec%d", rand.Intn(1000000))
			nest.Remove(id)
		}
	}
}

// ===== DIMENSION SCALING BENCHMARKS =====

func BenchmarkSearchDimensions(b *testing.B) {
	dimensions := []int{128, 256, 384, 768, 1536}

	for _, dim := range dimensions {
		b.Run(fmt.Sprintf("dim=%d", dim), func(b *testing.B) {
			path := fmt.Sprintf("/tmp/bench_dim_%d.magpie", dim)
			defer os.Remove(path)

			nest := setupBenchmarkDB(b, path, 10000, dim)
			defer nest.Close()

			query := make([]float32, dim)
			for i := range query {
				query[i] = rand.Float32()
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				nest.Find(query, 10)
			}
		})
	}
}

func BenchmarkInsertDimensions(b *testing.B) {
	dimensions := []int{128, 256, 384, 768, 1536}

	for _, dim := range dimensions {
		b.Run(fmt.Sprintf("dim=%d", dim), func(b *testing.B) {
			path := fmt.Sprintf("/tmp/bench_insert_dim_%d.magpie", dim)
			defer os.Remove(path)

			nest, err := Open(path, Options{Dimensions: dim})
			if err != nil {
				b.Fatal(err)
			}
			defer nest.Close()

			vec := make([]float32, dim)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				id := fmt.Sprintf("vec_%d", i)
				nest.Store(id, vec)
			}
		})
	}
}

// ===== FILE SIZE BENCHMARK =====

func BenchmarkFileSize1M(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping file size test in short mode")
	}

	path := "/tmp/bench_filesize_1m.magpie"
	defer os.Remove(path)

	nest, err := Open(path, Options{Dimensions: 128})
	if err != nil {
		b.Fatal(err)
	}

	// Insert 1M vectors
	for i := 0; i < 1000000; i++ {
		id := fmt.Sprintf("vec%d", i)
		vec := make([]float32, 128)
		for j := range vec {
			vec[j] = rand.Float32()
		}
		nest.Store(id, vec)
	}

	nest.Close()

	// Check file size
	info, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}

	sizeMB := float64(info.Size()) / (1024 * 1024)
	sizeGB := sizeMB / 1024

	b.ReportMetric(sizeMB, "MB")
	b.Logf("File size: %.2f MB (%.2f GB)", sizeMB, sizeGB)
	b.Logf("Bytes per vector: %.2f", float64(info.Size())/1000000.0)
}

// ===== HELPERS =====

func setupBenchmarkDB(b *testing.B, path string, n, dims int) *Nest {
	b.Helper()

	nest, err := Open(path, Options{
		Dimensions:       dims,
		M:                16,
		EfConstruction:   200,
		EnableQueryCache: false,
	})
	if err != nil {
		b.Fatal(err)
	}

	vectors := generateVectors(n, dims)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("vec%d", i)
		if err := nest.Store(id, vectors[i]); err != nil {
			b.Fatal(err)
		}

		// Progress indicator for large datasets
		if n >= 100000 && i > 0 && i%(n/10) == 0 {
			b.Logf("Setup progress: %d%%", (i*100)/n)
		}
	}

	return nest
}

func generateVectors(n, dims int) [][]float32 {
	vectors := make([][]float32, n)
	for i := 0; i < n; i++ {
		vectors[i] = make([]float32, dims)
		for j := 0; j < dims; j++ {
			vectors[i][j] = rand.Float32()
		}
	}
	return vectors
}
