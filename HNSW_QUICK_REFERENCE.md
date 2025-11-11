# HNSW Quick Reference Guide

## API Reference

### Creating an Index

```go
import magpie "github.com/voidlab/magpiedb"

// NewHSNWIndex(dimensions, m, efConstruction, distanceFunc)
idx := magpie.NewHSNWIndex(128, 16, 200, magpie.CosineSimilarity)
```

**Parameters:**
- `dimensions`: Vector dimensionality (e.g., 128, 384, 768)
- `m`: Bidirectional links per node (recommended: 16, range: 4-64)
- `efConstruction`: Build quality (recommended: 200, range: 100-500)
- `distanceFunc`: Distance metric (CosineSimilarity, EuclideanDistance, DotProduct)

### Adding Vectors

```go
vector := []float32{0.1, 0.2, 0.3, ...} // Must match dimensions
err := idx.Add("unique-id", vector)
if err != nil {
    // Handle error (duplicate ID, etc.)
}
```

### Searching

```go
query := []float32{0.1, 0.2, 0.3, ...} // Same dimensions
k := 10 // Number of nearest neighbors
results := idx.Search(query, k)

for _, result := range results {
    fmt.Printf("ID: %s, Distance: %.6f\n", result.ID, result.Distance)
}
```

**Result Type:**
```go
type Treasure struct {
    ID       string    // Vector ID
    Vector   []float32 // Vector data
    Distance float32   // Distance from query (lower = more similar)
    Metadata map[string]interface{} // Associated metadata
}
```

### Removing Vectors

```go
err := idx.Remove("vector-id")
if err != nil {
    // Handle error (not found, etc.)
}
```

### Serialization

```go
// Save to bytes
data, err := idx.Serialize()
if err != nil {
    // Handle error
}

// Save to file
os.WriteFile("index.bin", data, 0644)

// Load from bytes
idx2 := magpie.NewHSNWIndex(128, 16, 200, magpie.CosineSimilarity)
err = idx2.Deserialize(data)
if err != nil {
    // Handle error
}
```

### Utility Methods

```go
// Get node count
count := idx.Count()

// Get maximum level
maxLevel := idx.MaxLevel()

// Get specific node
node, exists := idx.Get("vector-id")
if exists {
    fmt.Printf("Level: %d, Neighbors: %v\n", node.Level, node.Neighbors)
}

// Clear all nodes
idx.Clear()
```

## Distance Functions

### Cosine Similarity (Default)
- Best for: Text embeddings, normalized vectors
- Range: 0 (identical) to 2 (opposite)
- Formula: `1 - dot(a,b) / (||a|| * ||b||)`

```go
idx := magpie.NewHSNWIndex(dim, 16, 200, magpie.CosineSimilarity)
```

### Euclidean Distance (L2)
- Best for: Image embeddings, spatial data
- Range: 0 (identical) to ∞
- Formula: `sqrt(sum((a[i] - b[i])^2))`

```go
idx := magpie.NewHSNWIndex(dim, 16, 200, magpie.EuclideanDistance)
```

### Dot Product
- Best for: Pre-normalized vectors, maximum inner product search
- Range: -∞ to ∞ (negated for distance)
- Formula: `-sum(a[i] * b[i])`

```go
idx := magpie.NewHSNWIndex(dim, 16, 200, magpie.DotProduct)
```

## Parameter Tuning

### M (Connections per Node)
- **Low (4-8)**: Lower memory, faster insertion, lower recall
- **Medium (16)**: Balanced (recommended for most cases)
- **High (32-64)**: Higher memory, slower insertion, better recall

### EfConstruction (Build Quality)
- **Low (100)**: Fast insertion, lower recall
- **Medium (200)**: Balanced (recommended)
- **High (500+)**: Slow insertion, excellent recall

### Search Quality (ef parameter)
The `Search()` method automatically uses `ef = max(k, efConstruction)` for quality results.

## Performance Characteristics

### Time Complexity
- **Insert**: O(M * efConstruction * log(N))
- **Search**: O(ef * log(N))
- **Space**: O(N * M * avgLevel)

### Typical Performance (128-dim, 1000 vectors)
- **Insert**: ~1.5 ms/vector
- **Search (k=10)**: ~1 ms/query
- **Memory**: ~1 MB for 1000 vectors (including graph structure)

### Scaling Guidelines
- **1K vectors**: All defaults work well
- **10K vectors**: Consider M=16, efConstruction=200
- **100K vectors**: Consider M=24, efConstruction=300
- **1M+ vectors**: Consider M=32, efConstruction=400, optimize for your use case

## Common Patterns

### Batch Insertion
```go
vectors := loadVectors() // []struct{ID string, Vector []float32}
for _, v := range vectors {
    if err := idx.Add(v.ID, v.Vector); err != nil {
        log.Printf("Failed to add %s: %v", v.ID, err)
    }
}
```

### Concurrent Searches
```go
var wg sync.WaitGroup
for _, query := range queries {
    wg.Add(1)
    go func(q []float32) {
        defer wg.Done()
        results := idx.Search(q, 10)
        processResults(results)
    }(query)
}
wg.Wait()
```

### Incremental Updates
```go
// Remove old version
idx.Remove("doc-123")

// Add new version
idx.Add("doc-123", newVector)
```

## Error Handling

```go
// Check for duplicate IDs
if err := idx.Add(id, vec); err != nil {
    if strings.Contains(err.Error(), "already exists") {
        // Handle duplicate
        idx.Remove(id)
        idx.Add(id, vec)
    }
}

// Check for not found
if err := idx.Remove(id); err != nil {
    if err == magpie.ErrVectorNotFound {
        // Handle not found
    }
}
```

## Testing

```bash
# Run all HNSW tests
go test -v -run TestHNSW

# Run benchmarks
go test -bench=BenchmarkHNSW -benchmem

# Run demo
go run examples/hnsw_demo.go
```

## Common Issues

### 1. Dimensions Mismatch
```go
// ERROR: Adding vector with wrong dimensions
idx := magpie.NewHSNWIndex(128, 16, 200, magpie.CosineSimilarity)
vector := make([]float32, 256) // Wrong!
```

**Solution**: Always ensure vectors match the index dimensions.

### 2. Poor Search Quality
**Symptoms**: Low recall, missing expected results

**Solutions**:
- Increase efConstruction (200 → 300 → 400)
- Increase M (16 → 24 → 32)
- Check distance metric (cosine for normalized, euclidean for raw)

### 3. Slow Insertion
**Symptoms**: Takes too long to build index

**Solutions**:
- Decrease efConstruction (200 → 150 → 100)
- Decrease M (16 → 12 → 8)
- Insert in batches if possible

### 4. High Memory Usage
**Symptoms**: Index uses too much RAM

**Solutions**:
- Decrease M (fewer connections per node)
- Use smaller vector dimensions if possible
- Consider disk-based storage (coming in future phases)

## Best Practices

1. **Normalize vectors** when using cosine similarity
2. **Set consistent parameters** across serialization/deserialization
3. **Use concurrent searches** for throughput (reads are thread-safe)
4. **Serialize periodically** for persistence
5. **Monitor maxLevel** - should be ~log2(N) for healthy distribution
6. **Test with your data** - optimal parameters vary by use case

## Files Location

- **Main Index**: `/home/storo/magpieDB/index.go`
- **Algorithm**: `/home/storo/magpieDB/hnsw.go`
- **Tests**: `/home/storo/magpieDB/index_test.go`
- **Demo**: `/home/storo/magpieDB/examples/hnsw_demo.go`
- **Summary**: `/home/storo/magpieDB/HNSW_IMPLEMENTATION_SUMMARY.md`

---

For more details, see the full implementation summary or run the demo application.
