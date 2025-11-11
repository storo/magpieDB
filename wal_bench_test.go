package magpie

import (
	"fmt"
	"path/filepath"
	"testing"
)

// BenchmarkWALAppend benchmarks sequential append operations
func BenchmarkWALAppend(b *testing.B) {
	tmpDir := b.TempDir()
	walPath := filepath.Join(tmpDir, "bench.wal")

	wal := NewWAL()
	defer wal.Close()

	if err := wal.Open(walPath); err != nil {
		b.Fatalf("Failed to open WAL: %v", err)
	}

	entry := WALEntry{
		Type:     WALInsert,
		ID:       "benchmark_vector",
		Vector:   []float32{1.0, 2.0, 3.0, 4.0, 5.0},
		Metadata: map[string]interface{}{"key": "value", "index": 0},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.ID = fmt.Sprintf("vec%d", i)
		if err := wal.Append(entry); err != nil {
			b.Fatalf("Failed to append: %v", err)
		}
	}
	b.StopTimer()

	if err := wal.Flush(); err != nil {
		b.Fatalf("Failed to flush: %v", err)
	}
}

// BenchmarkWALBatchFlush benchmarks batch flush performance
func BenchmarkWALBatchFlush(b *testing.B) {
	tmpDir := b.TempDir()
	walPath := filepath.Join(tmpDir, "bench.wal")

	wal := NewWAL()
	defer wal.Close()

	if err := wal.Open(walPath); err != nil {
		b.Fatalf("Failed to open WAL: %v", err)
	}

	entry := WALEntry{
		Type:     WALInsert,
		ID:       "benchmark_vector",
		Vector:   []float32{1.0, 2.0, 3.0, 4.0, 5.0},
		Metadata: map[string]interface{}{"key": "value", "index": 0},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Append 100 entries (triggers auto-flush)
		for j := 0; j < 100; j++ {
			entry.ID = fmt.Sprintf("vec%d_%d", i, j)
			if err := wal.Append(entry); err != nil {
				b.Fatalf("Failed to append: %v", err)
			}
		}
	}
	b.StopTimer()

	if err := wal.Flush(); err != nil {
		b.Fatalf("Failed to flush: %v", err)
	}
}

// BenchmarkWALRead benchmarks reading all entries
func BenchmarkWALRead(b *testing.B) {
	tmpDir := b.TempDir()
	walPath := filepath.Join(tmpDir, "bench.wal")

	wal := NewWAL()
	defer wal.Close()

	if err := wal.Open(walPath); err != nil {
		b.Fatalf("Failed to open WAL: %v", err)
	}

	// Write 1000 entries
	for i := 0; i < 1000; i++ {
		entry := WALEntry{
			Type:     WALInsert,
			ID:       fmt.Sprintf("vec%d", i),
			Vector:   []float32{float32(i), float32(i + 1), float32(i + 2)},
			Metadata: map[string]interface{}{"index": i},
		}
		if err := wal.Append(entry); err != nil {
			b.Fatalf("Failed to append: %v", err)
		}
	}

	if err := wal.Flush(); err != nil {
		b.Fatalf("Failed to flush: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entries, err := wal.ReadAll()
		if err != nil {
			b.Fatalf("Failed to read: %v", err)
		}
		if len(entries) != 1000 {
			b.Fatalf("Expected 1000 entries, got %d", len(entries))
		}
	}
}

// BenchmarkWALConcurrentAppend benchmarks concurrent append operations
func BenchmarkWALConcurrentAppend(b *testing.B) {
	tmpDir := b.TempDir()
	walPath := filepath.Join(tmpDir, "bench.wal")

	wal := NewWAL()
	defer wal.Close()

	if err := wal.Open(walPath); err != nil {
		b.Fatalf("Failed to open WAL: %v", err)
	}

	entry := WALEntry{
		Type:     WALInsert,
		ID:       "benchmark_vector",
		Vector:   []float32{1.0, 2.0, 3.0, 4.0, 5.0},
		Metadata: map[string]interface{}{"key": "value"},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			entry.ID = fmt.Sprintf("vec%d", i)
			if err := wal.Append(entry); err != nil {
				b.Fatalf("Failed to append: %v", err)
			}
			i++
		}
	})
	b.StopTimer()

	if err := wal.Flush(); err != nil {
		b.Fatalf("Failed to flush: %v", err)
	}
}

// BenchmarkWALLargeVector benchmarks large vector entries
func BenchmarkWALLargeVector(b *testing.B) {
	tmpDir := b.TempDir()
	walPath := filepath.Join(tmpDir, "bench.wal")

	wal := NewWAL()
	defer wal.Close()

	if err := wal.Open(walPath); err != nil {
		b.Fatalf("Failed to open WAL: %v", err)
	}

	// Create 1536-dimensional vector (OpenAI embedding size)
	largeVector := make([]float32, 1536)
	for i := range largeVector {
		largeVector[i] = float32(i) * 0.001
	}

	entry := WALEntry{
		Type:     WALInsert,
		ID:       "large_vector",
		Vector:   largeVector,
		Metadata: map[string]interface{}{"dimensions": 1536},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.ID = fmt.Sprintf("vec%d", i)
		if err := wal.Append(entry); err != nil {
			b.Fatalf("Failed to append: %v", err)
		}
	}
	b.StopTimer()

	if err := wal.Flush(); err != nil {
		b.Fatalf("Failed to flush: %v", err)
	}
}
