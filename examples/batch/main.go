package main

import (
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/voidlab/magpiedb"
)

func main() {
	fmt.Println("MagpieDB Batch Operations Example")
	fmt.Println("==================================")

	// Open database
	nest, err := magpie.Open("./batch_example.magpie", magpie.Options{
		Dimensions: 128,
		Distance:   "cosine",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer nest.Close()

	fmt.Println("✓ Database opened")

	// Generate random vectors
	const numVectors = 1000
	fmt.Printf("\nGenerating %d random vectors (128 dimensions)...\n", numVectors)

	items := make([]struct {
		ID       string
		Vector   []float32
		Metadata map[string]interface{}
	}, numVectors)

	categories := []string{"tutorial", "guide", "reference", "api", "example"}

	for i := 0; i < numVectors; i++ {
		items[i].ID = fmt.Sprintf("vec_%04d", i)
		items[i].Vector = randomVector(128)
		items[i].Metadata = map[string]interface{}{
			"category": categories[i%len(categories)],
			"index":    i,
			"rating":   float64(rand.Intn(5) + 1),
		}
	}

	fmt.Println("✓ Vectors generated")

	// Method 1: Individual inserts
	fmt.Println("\nMethod 1: Individual inserts")
	start := time.Now()

	for i := 0; i < 100; i++ {
		err := nest.Store(items[i].ID, items[i].Vector, items[i].Metadata)
		if err != nil {
			log.Printf("Warning: Failed to store: %v\n", err)
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("  Inserted 100 vectors in %v (%.2f vectors/sec)\n",
		elapsed, 100/elapsed.Seconds())

	// Method 2: Transaction batch
	fmt.Println("\nMethod 2: Transaction batch")
	start = time.Now()

	err = nest.Batch(func(tx *magpie.Tx) error {
		for i := 100; i < 200; i++ {
			if err := tx.Store(items[i].ID, items[i].Vector, items[i].Metadata); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		log.Printf("Warning: Batch failed: %v\n", err)
	}

	elapsed = time.Since(start)
	fmt.Printf("  Inserted 100 vectors in %v (%.2f vectors/sec)\n",
		elapsed, 100/elapsed.Seconds())

	// Method 3: MultiStore
	fmt.Println("\nMethod 3: MultiStore helper")
	start = time.Now()

	err = nest.MultiStore(items[200:300])
	if err != nil {
		log.Printf("Warning: MultiStore failed: %v\n", err)
	}

	elapsed = time.Since(start)
	fmt.Printf("  Inserted 100 vectors in %v (%.2f vectors/sec)\n",
		elapsed, 100/elapsed.Seconds())

	// Show final stats
	fmt.Printf("\nTotal vectors in database: %d\n", nest.Count())

	// Batch delete
	fmt.Println("\nBatch delete example...")
	idsToDelete := make([]string, 50)
	for i := 0; i < 50; i++ {
		idsToDelete[i] = fmt.Sprintf("vec_%04d", i)
	}

	err = nest.MultiRemove(idsToDelete)
	if err != nil {
		log.Printf("Warning: MultiRemove failed: %v\n", err)
	} else {
		fmt.Printf("✓ Deleted %d vectors\n", len(idsToDelete))
	}

	fmt.Printf("\nFinal count: %d\n", nest.Count())

	fmt.Println("\n✓ Example complete!")
	fmt.Println("\nNote: Performance numbers are for demonstration only.")
	fmt.Println("Actual performance will improve once full implementation is complete.")
}

func randomVector(dim int) []float32 {
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = rand.Float32()
	}
	return vec
}
