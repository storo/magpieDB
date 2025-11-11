package main

import (
	"fmt"
	"log"

	"github.com/voidlab/magpiedb"
)

func main() {
	fmt.Println("MagpieDB Basic Example")
	fmt.Println("======================")

	// Open or create database
	nest, err := magpie.Open("./example.magpie")
	if err != nil {
		log.Fatal(err)
	}
	defer nest.Close()

	fmt.Println("✓ Database opened")

	// Store some vectors
	fmt.Println("\nStoring vectors...")

	vectors := []struct {
		id  string
		vec []float32
	}{
		{"doc1", []float32{0.1, 0.2, 0.3, 0.4}},
		{"doc2", []float32{0.2, 0.3, 0.4, 0.5}},
		{"doc3", []float32{0.9, 0.8, 0.7, 0.6}},
		{"doc4", []float32{0.15, 0.25, 0.35, 0.45}},
	}

	for _, v := range vectors {
		err := nest.Store(v.id, v.vec)
		if err != nil {
			log.Printf("Warning: Failed to store %s: %v\n", v.id, err)
		} else {
			fmt.Printf("  ✓ Stored %s\n", v.id)
		}
	}

	// Get count
	count := nest.Count()
	fmt.Printf("\nTotal vectors: %d\n", count)

	// Check if vector exists
	if nest.Has("doc1") {
		fmt.Println("✓ Vector 'doc1' exists")
	}

	// Get a specific vector
	fmt.Println("\nRetrieving vector 'doc1'...")
	treasure, err := nest.Get("doc1")
	if err != nil {
		log.Printf("Warning: Failed to get vector: %v\n", err)
	} else {
		fmt.Printf("  ID: %s\n", treasure.ID)
		fmt.Printf("  Vector: %v\n", treasure.Vector)
	}

	// Search for similar vectors
	fmt.Println("\nSearching for vectors similar to [0.15, 0.25, 0.35, 0.45]...")
	query := []float32{0.15, 0.25, 0.35, 0.45}
	results := nest.Find(query, 3)

	if len(results) > 0 {
		fmt.Printf("Found %d results:\n", len(results))
		for i, result := range results {
			fmt.Printf("  %d. %s (distance: %.4f)\n", i+1, result.ID, result.Distance)
		}
	} else {
		fmt.Println("  (No results - search not yet implemented)")
	}

	// Remove a vector
	fmt.Println("\nRemoving vector 'doc3'...")
	err = nest.Remove("doc3")
	if err != nil {
		log.Printf("Warning: Failed to remove: %v\n", err)
	} else {
		fmt.Println("  ✓ Removed")
	}

	// Final count
	fmt.Printf("\nFinal vector count: %d\n", nest.Count())

	fmt.Println("\n✓ Example complete!")
	fmt.Println("\nNote: Some operations may show warnings until full implementation is complete.")
}
