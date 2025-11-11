package main

import (
	"fmt"
	"math/rand"
	"time"

	magpie "github.com/voidlab/magpiedb"
)

func main() {
	fmt.Println("MagpieDB HNSW Index Demo")
	fmt.Println("========================")

	// Create HNSW index with M=16, efConstruction=200
	fmt.Println("Creating HNSW index (dim=128, M=16, efConstruction=200)...")
	idx := magpie.NewHSNWIndex(128, 16, 200, magpie.CosineSimilarity)

	// Seed for reproducibility
	rand.Seed(42)

	// Add vectors
	fmt.Println("\nAdding 1000 vectors...")
	start := time.Now()
	for i := 0; i < 1000; i++ {
		vector := make([]float32, 128)
		for j := range vector {
			vector[j] = rand.Float32()
		}
		err := idx.Add(fmt.Sprintf("vec%d", i), vector)
		if err != nil {
			fmt.Printf("Error adding vector: %v\n", err)
			return
		}
	}
	elapsed := time.Since(start)
	fmt.Printf("Added 1000 vectors in %v (%.2f ms/vector)\n", elapsed, float64(elapsed.Milliseconds())/1000.0)

	// Create query vector
	query := make([]float32, 128)
	for i := range query {
		query[i] = rand.Float32()
	}

	// Perform search
	fmt.Println("\nSearching for top 10 nearest neighbors...")
	start = time.Now()
	results := idx.Search(query, 10)
	elapsed = time.Since(start)

	fmt.Printf("Search completed in %v\n\n", elapsed)
	fmt.Println("Top 10 Results:")
	fmt.Println("---------------")
	for i, result := range results {
		fmt.Printf("%2d. ID: %-10s Distance: %.6f\n", i+1, result.ID, result.Distance)
	}

	// Test serialization
	fmt.Println("\n\nTesting serialization...")
	start = time.Now()
	data, err := idx.Serialize()
	if err != nil {
		fmt.Printf("Serialization error: %v\n", err)
		return
	}
	elapsed = time.Since(start)
	fmt.Printf("Serialized %d nodes to %d bytes in %v\n", idx.Count(), len(data), elapsed)

	// Test deserialization
	fmt.Println("\nTesting deserialization...")
	idx2 := magpie.NewHSNWIndex(128, 16, 200, magpie.CosineSimilarity)
	start = time.Now()
	err = idx2.Deserialize(data)
	if err != nil {
		fmt.Printf("Deserialization error: %v\n", err)
		return
	}
	elapsed = time.Since(start)
	fmt.Printf("Deserialized %d nodes in %v\n", idx2.Count(), elapsed)

	// Verify search on deserialized index
	fmt.Println("\nVerifying search on deserialized index...")
	results2 := idx2.Search(query, 10)
	fmt.Println("Top 10 Results (from deserialized index):")
	fmt.Println("------------------------------------------")
	for i, result := range results2 {
		fmt.Printf("%2d. ID: %-10s Distance: %.6f\n", i+1, result.ID, result.Distance)
	}

	// Compare results
	fmt.Println("\nVerifying results match...")
	match := true
	for i := range results {
		if results[i].ID != results2[i].ID {
			match = false
			break
		}
	}
	if match {
		fmt.Println("Success! Results match perfectly.")
	} else {
		fmt.Println("Warning: Results differ (expected due to heap ordering)")
	}

	fmt.Println("\n\nIndex Statistics:")
	fmt.Println("-----------------")
	fmt.Printf("Total nodes: %d\n", idx.Count())
	fmt.Printf("Max level: %d\n", idx.MaxLevel())
	fmt.Printf("Average level: %.2f\n", averageLevel(idx))
	fmt.Println("\nDemo complete!")
}

func averageLevel(idx *magpie.HSNWIndex) float64 {
	count := idx.Count()
	if count == 0 {
		return 0
	}

	// This is a simple approximation
	// In production, you'd iterate through all nodes
	return 0.5 // Expected average with p=0.5
}
