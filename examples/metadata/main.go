package main

import (
	"fmt"
	"log"
	"math/rand"

	"github.com/voidlab/magpiedb"
)

func main() {
	fmt.Println("MagpieDB Metadata Filtering Example")
	fmt.Println("====================================")

	// Open database
	nest, err := magpie.Open("./metadata_example.magpie", magpie.Options{
		Dimensions: 128,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer nest.Close()

	fmt.Println("✓ Database opened")

	// Insert documents with rich metadata
	fmt.Println("\nInserting documents with metadata...")

	docs := []struct {
		id       string
		vector   []float32
		metadata map[string]interface{}
	}{
		{
			"intro_guide",
			randomVector(128),
			map[string]interface{}{
				"title":    "Introduction to MagpieDB",
				"category": "tutorial",
				"level":    "beginner",
				"rating":   4.8,
				"tags":     []string{"getting-started", "basics"},
				"featured": true,
			},
		},
		{
			"advanced_guide",
			randomVector(128),
			map[string]interface{}{
				"title":    "Advanced MagpieDB Techniques",
				"category": "tutorial",
				"level":    "advanced",
				"rating":   4.9,
				"tags":     []string{"optimization", "performance"},
				"featured": true,
			},
		},
		{
			"api_ref",
			randomVector(128),
			map[string]interface{}{
				"title":    "API Reference",
				"category": "reference",
				"level":    "intermediate",
				"rating":   4.5,
				"tags":     []string{"api", "documentation"},
				"featured": false,
			},
		},
		{
			"quick_start",
			randomVector(128),
			map[string]interface{}{
				"title":    "Quick Start Guide",
				"category": "guide",
				"level":    "beginner",
				"rating":   4.7,
				"tags":     []string{"quick-start", "basics"},
				"featured": true,
			},
		},
		{
			"perf_tuning",
			randomVector(128),
			map[string]interface{}{
				"title":    "Performance Tuning",
				"category": "guide",
				"level":    "advanced",
				"rating":   4.6,
				"tags":     []string{"performance", "optimization"},
				"featured": false,
			},
		},
	}

	for _, doc := range docs {
		err := nest.Store(doc.id, doc.vector, doc.metadata)
		if err != nil {
			log.Printf("Warning: Failed to store %s: %v\n", doc.id, err)
		} else {
			fmt.Printf("  ✓ Stored %s\n", doc.id)
		}
	}

	fmt.Printf("\nTotal documents: %d\n", nest.Count())

	// Example 1: Simple equality filter
	fmt.Println("\n--- Example 1: Find tutorials ---")
	filter1 := magpie.Eq("category", "tutorial")
	query := randomVector(128)

	results := nest.FindWithFilter(query, 10, filter1)
	printResults("Tutorials", results)

	// Example 2: Range filter
	fmt.Println("\n--- Example 2: High-rated content (rating >= 4.7) ---")
	filter2 := magpie.Between("rating", 4.7, 5.0)

	results = nest.FindWithFilter(query, 10, filter2)
	printResults("High-rated content", results)

	// Example 3: Combined filters (AND)
	fmt.Println("\n--- Example 3: Advanced tutorials ---")
	filter3 := magpie.And(
		magpie.Eq("category", "tutorial"),
		magpie.Eq("level", "advanced"),
	)

	results = nest.FindWithFilter(query, 10, filter3)
	printResults("Advanced tutorials", results)

	// Example 4: OR filter
	fmt.Println("\n--- Example 4: Tutorials OR guides ---")
	filter4 := magpie.Or(
		magpie.Eq("category", "tutorial"),
		magpie.Eq("category", "guide"),
	)

	results = nest.FindWithFilter(query, 10, filter4)
	printResults("Tutorials or guides", results)

	// Example 5: Complex filter
	fmt.Println("\n--- Example 5: Featured beginner content ---")
	filter5 := magpie.And(
		magpie.Eq("featured", true),
		magpie.Eq("level", "beginner"),
	)

	results = nest.FindWithFilter(query, 10, filter5)
	printResults("Featured beginner content", results)

	// Example 6: String contains
	fmt.Println("\n--- Example 6: Titles containing 'Guide' ---")
	filter6 := magpie.Contains("title", "Guide")

	results = nest.FindWithFilter(query, 10, filter6)
	printResults("Titles with 'Guide'", results)

	// Example 7: Complex nested filter
	fmt.Println("\n--- Example 7: Complex query ---")
	// (featured = true AND rating >= 4.7) OR (category = "reference")
	filter7 := magpie.Or(
		magpie.And(
			magpie.Eq("featured", true),
			magpie.Between("rating", 4.7, 5.0),
		),
		magpie.Eq("category", "reference"),
	)

	results = nest.FindWithFilter(query, 10, filter7)
	printResults("Complex query", results)

	fmt.Println("\n✓ Example complete!")
	fmt.Println("\nNote: Results will appear once search functionality is fully implemented.")
}

func printResults(title string, results []magpie.Treasure) {
	if len(results) == 0 {
		fmt.Println("  (No results - filter functionality not yet fully implemented)")
		return
	}

	fmt.Printf("  Found %d results:\n", len(results))
	for i, result := range results {
		fmt.Printf("  %d. %s (distance: %.4f)\n", i+1, result.ID, result.Distance)
		if title, ok := result.Metadata["title"].(string); ok {
			fmt.Printf("     Title: %s\n", title)
		}
		if category, ok := result.Metadata["category"].(string); ok {
			fmt.Printf("     Category: %s\n", category)
		}
		if rating, ok := result.Metadata["rating"].(float64); ok {
			fmt.Printf("     Rating: %.1f\n", rating)
		}
	}
}

func randomVector(dim int) []float32 {
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = rand.Float32()
	}
	return vec
}
