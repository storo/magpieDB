package magpie

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// Benchmark configurations matching the spec
var (
	TestDimensions = []int{128, 384, 768, 1536}
	TestSizes      = []int{1_000, 10_000, 100_000}
)

func BenchmarkInsertSingle(b *testing.B) {
	for _, dim := range TestDimensions {
		b.Run(fmt.Sprintf("dim=%d", dim), func(b *testing.B) {
			tmpfile := tempBenchFile()
			defer os.Remove(tmpfile)

			nest, err := Open(tmpfile, Options{Dimensions: dim})
			if err != nil {
				b.Fatal(err)
			}
			defer nest.Close()

			vector := randomVector(dim)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				id := fmt.Sprintf("vec_%d", i)
				_ = nest.Store(id, vector)
				// TODO: Check error when implemented
			}
		})
	}
}

func BenchmarkBatchInsert(b *testing.B) {
	batchSizes := []int{100, 1000, 10000}

	for _, dim := range []int{384, 768} {
		for _, batchSize := range batchSizes {
			b.Run(fmt.Sprintf("dim=%d_batch=%d", dim, batchSize), func(b *testing.B) {
				tmpfile := tempBenchFile()
				defer os.Remove(tmpfile)

				nest, err := Open(tmpfile, Options{Dimensions: dim})
				if err != nil {
					b.Fatal(err)
				}
				defer nest.Close()

				// Prepare batch
				items := make([]struct {
					ID       string
					Vector   []float32
					Metadata map[string]interface{}
				}, batchSize)

				for i := range items {
					items[i].ID = fmt.Sprintf("vec_%d", i)
					items[i].Vector = randomVector(dim)
				}

				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = nest.MultiStore(items)
					// TODO: Check error when implemented
				}
			})
		}
	}
}

func BenchmarkSearch(b *testing.B) {
	sizes := []int{1_000, 10_000}
	k := 10

	for _, dim := range []int{384, 768} {
		for _, size := range sizes {
			b.Run(fmt.Sprintf("dim=%d_size=%d_k=%d", dim, size, k), func(b *testing.B) {
				tmpfile := tempBenchFile()
				defer os.Remove(tmpfile)

				nest, err := Open(tmpfile, Options{Dimensions: dim})
				if err != nil {
					b.Fatal(err)
				}
				defer nest.Close()

				// Insert vectors
				for i := 0; i < size; i++ {
					id := fmt.Sprintf("vec_%d", i)
					vector := randomVector(dim)
					_ = nest.Store(id, vector)
				}

				query := randomVector(dim)

				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = nest.Find(query, k)
				}
			})
		}
	}
}

func BenchmarkSearchWithFilter(b *testing.B) {
	dim := 384
	size := 10_000
	k := 10

	tmpfile := tempBenchFile()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: dim})
	if err != nil {
		b.Fatal(err)
	}
	defer nest.Close()

	// Insert vectors with metadata
	for i := 0; i < size; i++ {
		id := fmt.Sprintf("vec_%d", i)
		vector := randomVector(dim)
		metadata := map[string]interface{}{
			"category": fmt.Sprintf("cat_%d", i%10),
			"rating":   float64(i%5) + 1,
		}
		_ = nest.Store(id, vector, metadata)
	}

	query := randomVector(dim)
	filter := Eq("category", "cat_1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = nest.FindWithFilter(query, k, filter)
	}
}

func BenchmarkOpen(b *testing.B) {
	sizes := []int{1_000, 10_000, 100_000}
	dim := 384

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			// Create database with data
			tmpfile := tempBenchFile()
			defer os.Remove(tmpfile)

			nest, err := Open(tmpfile, Options{Dimensions: dim})
			if err != nil {
				b.Fatal(err)
			}

			// Insert vectors
			for i := 0; i < size; i++ {
				id := fmt.Sprintf("vec_%d", i)
				vector := randomVector(dim)
				_ = nest.Store(id, vector)
			}

			nest.Close()

			// Benchmark opening
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				nest, err := Open(tmpfile)
				if err != nil {
					b.Fatal(err)
				}
				nest.Close()
			}
		})
	}
}

func BenchmarkTransaction(b *testing.B) {
	dim := 384
	batchSize := 100

	tmpfile := tempBenchFile()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: dim})
	if err != nil {
		b.Fatal(err)
	}
	defer nest.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := nest.Batch(func(tx *Tx) error {
			for j := 0; j < batchSize; j++ {
				id := fmt.Sprintf("vec_%d_%d", i, j)
				vector := randomVector(dim)
				_ = tx.Store(id, vector)
			}
			return nil
		})
		_ = err
	}
}

func BenchmarkCompact(b *testing.B) {
	dim := 384
	size := 10_000

	for i := 0; i < b.N; i++ {
		b.StopTimer()

		tmpfile := tempBenchFile()
		defer os.Remove(tmpfile)

		nest, err := Open(tmpfile, Options{Dimensions: dim})
		if err != nil {
			b.Fatal(err)
		}

		// Insert vectors
		for j := 0; j < size; j++ {
			id := fmt.Sprintf("vec_%d", j)
			vector := randomVector(dim)
			_ = nest.Store(id, vector)
		}

		// Delete half
		for j := 0; j < size/2; j++ {
			id := fmt.Sprintf("vec_%d", j)
			_ = nest.Remove(id)
		}

		b.StartTimer()
		_ = nest.Compact()
		b.StopTimer()

		nest.Close()
	}
}

// Helper functions

func randomVector(dim int) []float32 {
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = rand.Float32()
	}
	return vec
}

func tempBenchFile() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("magpie_bench_%d.magpie", rand.Int()))
}

// Memory benchmark
func BenchmarkMemoryUsage(b *testing.B) {
	dim := 384
	size := 1_000_000

	tmpfile := tempBenchFile()
	defer os.Remove(tmpfile)

	nest, err := Open(tmpfile, Options{Dimensions: dim})
	if err != nil {
		b.Fatal(err)
	}
	defer nest.Close()

	b.ReportAllocs()
	for i := 0; i < size; i++ {
		id := fmt.Sprintf("vec_%d", i)
		vector := randomVector(dim)
		_ = nest.Store(id, vector)
	}
}
