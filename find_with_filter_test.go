package magpie

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Helper function for test files
func tempFileFilter(t *testing.T) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("magpie_filter_test_%d_%s.magpie", os.Getpid(), t.Name()))
}

// TestFindWithFilterBasic tests basic filter functionality with single filter
func TestFindWithFilterBasic(t *testing.T) {
	tmpfile := tempFileFilter(t)
	defer os.Remove(tmpfile)

	opts := DefaultOptions()
	opts.Dimensions = 3
	nest, err := Open(tmpfile, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}
	defer nest.Close()

	// Insert vectors with metadata
	vectors := []struct {
		id       string
		vector   []float32
		category string
		rating   float64
	}{
		{"doc1", []float32{1.0, 0.0, 0.0}, "tutorial", 4.5},
		{"doc2", []float32{0.9, 0.1, 0.0}, "tutorial", 4.8},
		{"doc3", []float32{0.0, 1.0, 0.0}, "guide", 3.5},
		{"doc4", []float32{0.0, 0.9, 0.1}, "guide", 4.2},
		{"doc5", []float32{0.0, 0.0, 1.0}, "reference", 5.0},
	}

	for _, v := range vectors {
		err := nest.Store(v.id, v.vector, map[string]interface{}{
			"category": v.category,
			"rating":   v.rating,
		})
		if err != nil {
			t.Fatalf("failed to store %s: %v", v.id, err)
		}
	}

	// Query with filter for category="tutorial"
	query := []float32{1.0, 0.0, 0.0}
	filter := Eq("category", "tutorial")
	results := nest.FindWithFilter(query, 5, filter)

	// Should only get tutorial docs
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// Verify all results match filter
	for _, res := range results {
		if cat, ok := res.Metadata["category"].(string); !ok || cat != "tutorial" {
			t.Errorf("result %s has category %v, expected tutorial", res.ID, res.Metadata["category"])
		}
	}

	// Verify ordering by distance
	if len(results) >= 2 {
		if results[0].Distance > results[1].Distance {
			t.Error("results not ordered by distance")
		}
	}
}

// TestFindWithFilterNilFilter tests that nil filter behaves like Find()
func TestFindWithFilterNilFilter(t *testing.T) {
	tmpfile := tempFileFilter(t)
	defer os.Remove(tmpfile)

	opts := DefaultOptions()
	opts.Dimensions = 2
	nest, err := Open(tmpfile, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}
	defer nest.Close()

	// Insert test data
	for i := 0; i < 5; i++ {
		vec := []float32{float32(i), 0.0}
		err := nest.Store(fmt.Sprintf("vec%d", i), vec, map[string]interface{}{
			"index": i,
		})
		if err != nil {
			t.Fatalf("failed to store: %v", err)
		}
	}

	query := []float32{2.0, 0.0}
	k := 3

	// Compare nil filter with Find()
	resultsFind := nest.Find(query, k)
	resultsFilterNil := nest.FindWithFilter(query, k, nil)

	if len(resultsFind) != len(resultsFilterNil) {
		t.Errorf("nil filter results differ from Find(): %d vs %d", len(resultsFilterNil), len(resultsFind))
	}

	for i := range resultsFind {
		if resultsFind[i].ID != resultsFilterNil[i].ID {
			t.Errorf("result %d differs: %s vs %s", i, resultsFilterNil[i].ID, resultsFind[i].ID)
		}
	}
}
// TestFindWithFilterComplex tests complex filter combinations (AND/OR/NOT)
func TestFindWithFilterComplex(t *testing.T) {
	tmpfile := tempFileFilter(t)
	defer os.Remove(tmpfile)

	opts := DefaultOptions()
	opts.Dimensions = 4
	nest, err := Open(tmpfile, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}
	defer nest.Close()

	// Insert diverse vectors
	vectors := []struct {
		id       string
		vector   []float32
		category string
		rating   float64
		premium  bool
	}{
		{"v1", []float32{1.0, 0.0, 0.0, 0.0}, "tutorial", 4.5, true},
		{"v2", []float32{0.9, 0.1, 0.0, 0.0}, "tutorial", 3.2, false},
		{"v3", []float32{0.8, 0.2, 0.0, 0.0}, "guide", 4.8, true},
		{"v4", []float32{0.7, 0.3, 0.0, 0.0}, "guide", 3.0, false},
		{"v5", []float32{0.0, 1.0, 0.0, 0.0}, "reference", 5.0, true},
		{"v6", []float32{0.0, 0.9, 0.1, 0.0}, "tutorial", 4.9, true},
	}

	for _, v := range vectors {
		err := nest.Store(v.id, v.vector, map[string]interface{}{
			"category": v.category,
			"rating":   v.rating,
			"premium":  v.premium,
		})
		if err != nil {
			t.Fatalf("failed to store %s: %v", v.id, err)
		}
	}

	// Complex filter: (category="tutorial" OR category="guide") AND rating >= 4.0 AND premium=true
	complexFilter := And(
		Or(
			Eq("category", "tutorial"),
			Eq("category", "guide"),
		),
		Between("rating", 4.0, 5.0),
		Eq("premium", true),
	)

	query := []float32{1.0, 0.0, 0.0, 0.0}
	results := nest.FindWithFilter(query, 10, complexFilter)

	// Should match: v1 (tutorial, 4.5, premium), v3 (guide, 4.8, premium), v6 (tutorial, 4.9, premium)
	// Should NOT match: v2 (not premium), v4 (rating too low), v5 (reference category)
	expectedIDs := map[string]bool{"v1": true, "v3": true, "v6": true}

	if len(results) != len(expectedIDs) {
		t.Errorf("expected %d results, got %d", len(expectedIDs), len(results))
	}

	for _, res := range results {
		if !expectedIDs[res.ID] {
			t.Errorf("unexpected result: %s", res.ID)
		}

		// Verify filter conditions
		cat := res.Metadata["category"].(string)
		if cat != "tutorial" && cat != "guide" {
			t.Errorf("result %s has invalid category: %s", res.ID, cat)
		}

		rating := res.Metadata["rating"].(float64)
		if rating < 4.0 {
			t.Errorf("result %s has rating below 4.0: %f", res.ID, rating)
		}

		premium := res.Metadata["premium"].(bool)
		if !premium {
			t.Errorf("result %s is not premium", res.ID)
		}
	}

	// Test NOT filter
	notFilter := Not(Eq("category", "tutorial"))
	results = nest.FindWithFilter(query, 10, notFilter)

	for _, res := range results {
		if cat := res.Metadata["category"].(string); cat == "tutorial" {
			t.Errorf("NOT filter failed: result %s has category=tutorial", res.ID)
		}
	}
}

// TestFindWithFilterNoMatches tests filter that matches nothing
func TestFindWithFilterNoMatches(t *testing.T) {
	tmpfile := tempFileFilter(t)
	defer os.Remove(tmpfile)

	opts := DefaultOptions()
	opts.Dimensions = 2
	nest, err := Open(tmpfile, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}
	defer nest.Close()

	// Insert some vectors
	for i := 0; i < 10; i++ {
		vec := []float32{float32(i) / 10.0, float32(10-i) / 10.0}
		err := nest.Store(fmt.Sprintf("vec%d", i), vec, map[string]interface{}{
			"type": "normal",
		})
		if err != nil {
			t.Fatalf("failed to store vec%d: %v", i, err)
		}
	}

	// Filter for non-existent category
	filter := Eq("type", "nonexistent")
	query := []float32{0.5, 0.5}
	results := nest.FindWithFilter(query, 5, filter)

	if len(results) != 0 {
		t.Errorf("expected 0 results for non-matching filter, got %d", len(results))
	}
}

// TestFindWithFilterAllMatch tests filter that matches all vectors
func TestFindWithFilterAllMatch(t *testing.T) {
	tmpfile := tempFileFilter(t)
	defer os.Remove(tmpfile)

	opts := DefaultOptions()
	opts.Dimensions = 2
	nest, err := Open(tmpfile, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}
	defer nest.Close()

	// Insert vectors with same category
	n := 20
	for i := 0; i < n; i++ {
		vec := []float32{float32(i) / 20.0, float32(20-i) / 20.0}
		err := nest.Store(fmt.Sprintf("vec%d", i), vec, map[string]interface{}{
			"category": "common",
		})
		if err != nil {
			t.Fatalf("failed to store vec%d: %v", i, err)
		}
	}

	// Filter that matches all
	filter := Eq("category", "common")
	query := []float32{0.5, 0.5}
	k := 10
	results := nest.FindWithFilter(query, k, filter)

	// Should return k results (all match, limited by k)
	if len(results) != k {
		t.Errorf("expected %d results, got %d", k, len(results))
	}

	// Verify all have correct category
	for _, res := range results {
		if cat := res.Metadata["category"].(string); cat != "common" {
			t.Errorf("result %s has wrong category: %s", res.ID, cat)
		}
	}
}

// TestFindWithFilterVsFind compares filtered vs unfiltered results
func TestFindWithFilterVsFind(t *testing.T) {
	tmpfile := tempFileFilter(t)
	defer os.Remove(tmpfile)

	opts := DefaultOptions()
	opts.Dimensions = 3
	nest, err := Open(tmpfile, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}
	defer nest.Close()

	// Insert vectors with two categories
	for i := 0; i < 20; i++ {
		vec := []float32{
			float32(i) / 20.0,
			float32(20-i) / 20.0,
			0.5,
		}
		category := "A"
		if i%2 == 0 {
			category = "B"
		}
		err := nest.Store(fmt.Sprintf("vec%d", i), vec, map[string]interface{}{
			"category": category,
		})
		if err != nil {
			t.Fatalf("failed to store vec%d: %v", i, err)
		}
	}

	query := []float32{0.5, 0.5, 0.5}
	k := 10

	// Unfiltered search
	unfilteredResults := nest.Find(query, k)

	// Filtered search for category B
	filter := Eq("category", "B")
	filteredResults := nest.FindWithFilter(query, k, filter)

	// Filtered results should be subset or equal length
	if len(filteredResults) > len(unfilteredResults) {
		t.Error("filtered results cannot exceed unfiltered results")
	}

	// All filtered results should have category B
	for _, res := range filteredResults {
		if cat := res.Metadata["category"].(string); cat != "B" {
			t.Errorf("filtered result %s has wrong category: %s", res.ID, cat)
		}
	}

	// Filtered results should still be ordered by distance
	for i := 1; i < len(filteredResults); i++ {
		if filteredResults[i-1].Distance > filteredResults[i].Distance {
			t.Error("filtered results not properly ordered by distance")
		}
	}
}

// TestFindWithFilterMetadata tests filters with complex metadata structures
func TestFindWithFilterMetadata(t *testing.T) {
	tmpfile := tempFileFilter(t)
	defer os.Remove(tmpfile)

	opts := DefaultOptions()
	opts.Dimensions = 2
	nest, err := Open(tmpfile, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}
	defer nest.Close()

	// Insert vectors with diverse metadata
	testData := []struct {
		id       string
		vector   []float32
		title    string
		views    int
		rating   float64
		verified bool
	}{
		{
			"doc1", []float32{1.0, 0.0},
			"Introduction to Go",
			1000, 4.5, true,
		},
		{
			"doc2", []float32{0.9, 0.1},
			"Advanced Go Patterns",
			500, 4.8, true,
		},
		{
			"doc3", []float32{0.0, 1.0},
			"Python Basics",
			2000, 4.2, false,
		},
		{
			"doc4", []float32{0.1, 0.9},
			"Rust for Beginners",
			800, 4.7, true,
		},
	}

	for _, td := range testData {
		err := nest.Store(td.id, td.vector, map[string]interface{}{
			"title":    td.title,
			"views":    td.views,
			"rating":   td.rating,
			"verified": td.verified,
		})
		if err != nil {
			t.Fatalf("failed to store %s: %v", td.id, err)
		}
	}

	// Test string contains filter
	containsFilter := Contains("title", "Go")
	query := []float32{1.0, 0.0}
	results := nest.FindWithFilter(query, 10, containsFilter)

	expectedContains := map[string]bool{"doc1": true, "doc2": true}
	if len(results) != len(expectedContains) {
		t.Errorf("contains filter: expected %d results, got %d", len(expectedContains), len(results))
	}
	for _, res := range results {
		if !expectedContains[res.ID] {
			t.Errorf("contains filter returned unexpected result: %s", res.ID)
		}
	}

	// Test range filter
	rangeFilter := Between("views", 500, 1000)
	results = nest.FindWithFilter(query, 10, rangeFilter)

	for _, res := range results {
		views := int(res.Metadata["views"].(float64))
		if views < 500 || views > 1000 {
			t.Errorf("range filter failed: %s has views=%d", res.ID, views)
		}
	}

	// Test exists filter
	existsFilter := Exists("verified")
	results = nest.FindWithFilter(query, 10, existsFilter)

	if len(results) != 4 {
		t.Errorf("exists filter: expected 4 results, got %d", len(results))
	}

	// Test complex multi-condition filter
	complexFilter := And(
		Between("rating", 4.5, 5.0),
		Eq("verified", true),
		Contains("title", "Go"),
	)
	results = nest.FindWithFilter(query, 10, complexFilter)

	// Should match: doc2 (rating 4.8, verified, "Advanced Go Patterns")
	// Borderline: doc1 (rating 4.5 is inclusive)
	for _, res := range results {
		rating := res.Metadata["rating"].(float64)
		verified := res.Metadata["verified"].(bool)
		title := res.Metadata["title"].(string)

		if rating < 4.5 || rating > 5.0 {
			t.Errorf("%s: rating %f out of range", res.ID, rating)
		}
		if !verified {
			t.Errorf("%s: not verified", res.ID)
		}
		if !Contains("title", "Go").Match(res.Metadata) {
			t.Errorf("%s: title '%s' doesn't contain 'Go'", res.ID, title)
		}
	}
}

// TestFindWithFilterConcurrent tests concurrent filtered searches
func TestFindWithFilterConcurrent(t *testing.T) {
	tmpfile := tempFileFilter(t)
	defer os.Remove(tmpfile)

	opts := DefaultOptions()
	opts.Dimensions = 4
	nest, err := Open(tmpfile, opts)
	if err != nil {
		t.Fatalf("failed to open nest: %v", err)
	}
	defer nest.Close()

	// Insert data
	n := 100
	for i := 0; i < n; i++ {
		vec := []float32{
			float32(i % 10),
			float32(i % 7),
			float32(i % 5),
			float32(i % 3),
		}
		err := nest.Store(fmt.Sprintf("vec%d", i), vec, map[string]interface{}{
			"category": fmt.Sprintf("cat%d", i%5),
			"value":    i,
		})
		if err != nil {
			t.Fatalf("failed to store: %v", err)
		}
	}

	// Concurrent searches with different filters
	const numGoroutines = 10
	const numSearches = 20

	done := make(chan bool, numGoroutines)
	for g := 0; g < numGoroutines; g++ {
		go func(gid int) {
			query := []float32{5.0, 3.0, 2.0, 1.0}
			filter := Eq("category", fmt.Sprintf("cat%d", gid%5))

			for i := 0; i < numSearches; i++ {
				results := nest.FindWithFilter(query, 10, filter)

				// Verify all results match filter
				for _, res := range results {
					if cat := res.Metadata["category"].(string); cat != fmt.Sprintf("cat%d", gid%5) {
						t.Errorf("goroutine %d: wrong category %s", gid, cat)
					}
				}
			}
			done <- true
		}(g)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}
