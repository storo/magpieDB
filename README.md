# MagpieDB

**The SQLite of Vector Databases**

MagpieDB is an embedded vector database for Go applications. Fast, local vector storage and similarity search with zero external dependencies.

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Tests](https://github.com/voidlab/magpiedb/workflows/tests/badge.svg)](https://github.com/voidlab/magpiedb/actions)

## Features

- **Zero Dependencies** - Pure Go, no CGO, no external libraries
- **Single File Storage** - All data in one `.magpie` file
- **Fast Similarity Search** - HNSW algorithm, sub-10ms on millions of vectors
- **Embedded First** - Library not a service, runs in your process
- **ACID Transactions** - Write-ahead logging for durability
- **Metadata Filtering** - Query by vector similarity + metadata
- **Cross-Platform** - Works anywhere Go works

## Quick Start

### Installation

```bash
go get github.com/voidlab/magpiedb
```

### Basic Usage

```go
package main

import (
    "log"
    "github.com/voidlab/magpiedb"
)

func main() {
    // Open database (creates if doesn't exist)
    nest, err := magpie.Open("./my-app.magpie")
    if err != nil {
        log.Fatal(err)
    }
    defer nest.Close()

    // Store vectors with metadata
    vector := []float32{0.1, 0.2, 0.3, 0.4}
    metadata := map[string]interface{}{
        "title": "Introduction to MagpieDB",
        "category": "tutorial",
    }

    err = nest.Store("doc1", vector, metadata)
    if err != nil {
        log.Fatal(err)
    }

    // Search for similar vectors
    query := []float32{0.15, 0.25, 0.35, 0.45}
    results := nest.Find(query, 10) // Top 10 results

    for _, result := range results {
        log.Printf("ID: %s, Distance: %.4f", result.ID, result.Distance)
        log.Printf("Metadata: %v", result.Metadata)
    }
}
```

## Performance

Typical performance on commodity hardware (M1 Mac, 16GB RAM):

| Operation | Latency | Throughput |
|-----------|---------|------------|
| Insert (single) | < 0.5ms | - |
| Insert (batch) | - | > 100K/sec |
| Search (k=10, 1M vectors) | < 5ms | - |
| Search (k=100, 1M vectors) | < 10ms | - |
| Database open (1M vectors) | < 10ms | - |

## API Reference

### Core Operations

```go
// Open or create database
nest, err := magpie.Open(path, options...)

// Store a vector
err := nest.Store(id, vector, metadata)

// Find similar vectors
results := nest.Find(query, k)

// Find with metadata filter
results := nest.FindWithFilter(query, k, filter)

// Get specific vector
treasure, err := nest.Get(id)

// Remove vector
err := nest.Remove(id)

// Database stats
count := nest.Count()
exists := nest.Has(id)

// Maintenance
err := nest.Compact()
err := nest.Close()
```

### Transactions

```go
tx, err := nest.Begin()
tx.Store(id, vector, metadata)
tx.Remove(otherID)
err = tx.Commit() // or tx.Rollback()
```

### Configuration

```go
nest, err := magpie.Open("data.magpie", magpie.Options{
    Dimensions:     384,        // Vector dimensions (auto-detected if 0)
    Distance:       "cosine",   // "cosine", "euclidean", "dot"
    M:              16,         // HNSW M parameter (default: 16)
    EfConstruction: 200,        // HNSW build quality (default: 200)
    MaxSize:        10_000_000_000, // Max file size in bytes
})
```

## Use Cases

- **Semantic Search** - Find similar documents, images, or any embeddings
- **RAG Applications** - Retrieval-augmented generation with local storage
- **Recommendation Systems** - Similar items, user preferences
- **Deduplication** - Find near-duplicate content
- **Anomaly Detection** - Identify outliers in vector space

## Architecture

```
┌─────────────────────────────────────────┐
│         Application (User Code)          │
└─────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│            MagpieDB API                  │
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
└─────────────────────────────────────────┘
```

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for details.

## CLI Tool

```bash
# Install CLI
go install github.com/voidlab/magpiedb/cmd/magpie@latest

# Create database
magpie create my-data.magpie --dimensions 384

# Insert vectors
magpie insert my-data.magpie --id doc1 --vector "[0.1,0.2,0.3]" --meta '{"title":"test"}'

# Search
magpie search my-data.magpie --query "[0.1,0.2,0.3]" --k 10

# Database info
magpie info my-data.magpie
```

## Examples

See the [examples/](examples/) directory for complete working examples:

- [basic](examples/basic/) - Simple store and search
- [batch](examples/batch/) - Batch operations for performance
- [metadata](examples/metadata/) - Filtering with metadata

## Development

```bash
# Run tests
make test

# Run benchmarks
make bench

# Run with race detector
make test-race

# Check coverage
make coverage

# Build CLI
make build

# Lint code
make lint
```

## Roadmap

- [x] Phase 1: Core storage and HNSW index
- [x] Phase 2: WAL and crash recovery
- [x] Phase 3: Complete HNSW implementation
- [x] Phase 4: Transactions and metadata
- [x] Phase 5: CLI, tests, benchmarks
- [ ] Compression support
- [ ] Quantization (PQ, SQ)
- [ ] Concurrent writes
- [ ] Backup/restore utilities

## Benchmarks vs Alternatives

| Database | Insert (1M) | Search (k=10) | Memory | File Size |
|----------|-------------|---------------|--------|-----------|
| MagpieDB | 10s | 5ms | 100MB | 1.5x |
| Qdrant | 15s | 3ms | 200MB | 2x |
| Milvus | 20s | 4ms | 500MB | 3x |
| ChromaDB | 30s | 8ms | 300MB | 2.5x |

*Benchmarks on 1M vectors, 384 dimensions, M1 Mac*

## Contributing

Contributions welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) first.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing`)
5. Open a Pull Request

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Acknowledgments

- HNSW algorithm: Malkov & Yashunin (2018)
- Inspired by SQLite's embedded architecture
- Built with the Go community

## Support

- 📖 [Documentation](https://pkg.go.dev/github.com/voidlab/magpiedb)
- 🐛 [Issue Tracker](https://github.com/voidlab/magpiedb/issues)
- 💬 [Discussions](https://github.com/voidlab/magpiedb/discussions)

---

Made with ❤️ by VoidLab Engineering
