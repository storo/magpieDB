package magpie

import (
	"os"
	"testing"
)

// BenchmarkReadPageCopy benchmarks ReadPage with copy (current implementation)
func BenchmarkReadPageCopy(b *testing.B) {
	tmpFile, err := os.CreateTemp("", "magpie_bench_*.db")
	if err != nil {
		b.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	storage := NewStorage()
	if err := storage.Init(tmpFile, PageSize*100); err != nil {
		b.Fatalf("Failed to init storage: %v", err)
	}
	defer storage.Close()

	// Write test data
	testData := make([]byte, PageSize)
	for i := 0; i < PageSize; i++ {
		testData[i] = byte(i % 256)
	}
	if err := storage.WritePage(0, testData); err != nil {
		b.Fatalf("Failed to write page: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := storage.ReadPage(0)
		if err != nil {
			b.Fatalf("Read failed: %v", err)
		}
	}
}

// BenchmarkReadPagesCopy benchmarks batch ReadPages with copy
func BenchmarkReadPagesCopy(b *testing.B) {
	tmpFile, err := os.CreateTemp("", "magpie_bench_*.db")
	if err != nil {
		b.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	storage := NewStorage()
	if err := storage.Init(tmpFile, PageSize*100); err != nil {
		b.Fatalf("Failed to init storage: %v", err)
	}
	defer storage.Close()

	// Write test data to 10 pages
	testData := make([]byte, PageSize)
	for pageNum := uint64(0); pageNum < 10; pageNum++ {
		for i := 0; i < PageSize; i++ {
			testData[i] = byte((pageNum + uint64(i)) % 256)
		}
		if err := storage.WritePage(pageNum, testData); err != nil {
			b.Fatalf("Failed to write page %d: %v", pageNum, err)
		}
	}

	pageNums := []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := storage.ReadPages(pageNums)
		if err != nil {
			b.Fatalf("ReadPages failed: %v", err)
		}
	}
}

// BenchmarkConcurrentReadsDuringRemap benchmarks realistic concurrent scenario
func BenchmarkConcurrentReadsDuringRemap(b *testing.B) {
	tmpFile, err := os.CreateTemp("", "magpie_bench_*.db")
	if err != nil {
		b.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	storage := NewStorage()
	if err := storage.Init(tmpFile, PageSize*50); err != nil {
		b.Fatalf("Failed to init storage: %v", err)
	}
	defer storage.Close()

	// Write test data
	testData := make([]byte, PageSize)
	for i := 0; i < PageSize; i++ {
		testData[i] = byte(i % 256)
	}
	for pageNum := uint64(0); pageNum < 10; pageNum++ {
		if err := storage.WritePage(pageNum, testData); err != nil {
			b.Fatalf("Failed to write page %d: %v", pageNum, err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	// Each iteration does 10 reads
	b.RunParallel(func(pb *testing.PB) {
		pageNum := uint64(0)
		for pb.Next() {
			_, err := storage.ReadPage(pageNum)
			if err != nil {
				b.Fatalf("Read failed: %v", err)
			}
			pageNum = (pageNum + 1) % 10
		}
	})
}

// BenchmarkWritePage benchmarks WritePage (not affected by the fix)
func BenchmarkWritePage(b *testing.B) {
	tmpFile, err := os.CreateTemp("", "magpie_bench_*.db")
	if err != nil {
		b.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	storage := NewStorage()
	if err := storage.Init(tmpFile, PageSize*100); err != nil {
		b.Fatalf("Failed to init storage: %v", err)
	}
	defer storage.Close()

	// Prepare test data
	testData := make([]byte, PageSize)
	for i := 0; i < PageSize; i++ {
		testData[i] = byte(i % 256)
	}

	// Allocate pages first
	for i := 0; i < b.N; i++ {
		if _, err := storage.AllocatePage(); err != nil {
			b.Fatalf("Failed to allocate page: %v", err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := storage.WritePage(uint64(i), testData); err != nil {
			b.Fatalf("Write failed: %v", err)
		}
	}
}

// BenchmarkAllocatePage benchmarks page allocation (includes remap overhead)
func BenchmarkAllocatePage(b *testing.B) {
	tmpFile, err := os.CreateTemp("", "magpie_bench_*.db")
	if err != nil {
		b.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	storage := NewStorage()
	if err := storage.Init(tmpFile, PageSize*10); err != nil {
		b.Fatalf("Failed to init storage: %v", err)
	}
	defer storage.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if _, err := storage.AllocatePage(); err != nil {
			b.Fatalf("Allocate failed: %v", err)
		}
	}
}
