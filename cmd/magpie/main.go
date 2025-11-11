package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/voidlab/magpiedb"
)

const version = "1.0.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "create":
		createCmd()
	case "insert":
		insertCmd()
	case "search":
		searchCmd()
	case "get":
		getCmd()
	case "remove":
		removeCmd()
	case "info":
		infoCmd()
	case "compact":
		compactCmd()
	case "version":
		fmt.Printf("magpie version %s\n", version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func createCmd() {
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	dimensions := fs.Int("dimensions", 0, "Vector dimensions (auto-detect if 0)")
	distance := fs.String("distance", "cosine", "Distance metric (cosine, euclidean, dot)")
	m := fs.Int("m", 16, "HNSW M parameter")
	ef := fs.Int("ef", 200, "HNSW EfConstruction parameter")

	fs.Parse(os.Args[2:])

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: magpie create <database> [options]\n")
		fs.PrintDefaults()
		os.Exit(1)
	}

	dbPath := fs.Arg(0)

	opts := magpie.Options{
		Dimensions:     *dimensions,
		Distance:       *distance,
		M:              *m,
		EfConstruction: *ef,
	}

	nest, err := magpie.Open(dbPath, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create database: %v\n", err)
		os.Exit(1)
	}
	defer nest.Close()

	fmt.Printf("Created database: %s\n", dbPath)
	if *dimensions > 0 {
		fmt.Printf("Dimensions: %d\n", *dimensions)
	}
	fmt.Printf("Distance metric: %s\n", *distance)
}

func insertCmd() {
	fs := flag.NewFlagSet("insert", flag.ExitOnError)
	id := fs.String("id", "", "Vector ID (required)")
	vectorStr := fs.String("vector", "", "Vector as JSON array (required)")
	metaStr := fs.String("meta", "", "Metadata as JSON object")

	fs.Parse(os.Args[2:])

	if fs.NArg() < 1 || *id == "" || *vectorStr == "" {
		fmt.Fprintf(os.Stderr, "Usage: magpie insert <database> --id <id> --vector <json> [--meta <json>]\n")
		fs.PrintDefaults()
		os.Exit(1)
	}

	dbPath := fs.Arg(0)

	// Parse vector
	var vector []float32
	if err := json.Unmarshal([]byte(*vectorStr), &vector); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse vector: %v\n", err)
		os.Exit(1)
	}

	// Parse metadata
	var metadata map[string]interface{}
	if *metaStr != "" {
		if err := json.Unmarshal([]byte(*metaStr), &metadata); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse metadata: %v\n", err)
			os.Exit(1)
		}
	}

	// Open database
	nest, err := magpie.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer nest.Close()

	// Insert vector
	if metadata != nil {
		err = nest.Store(*id, vector, metadata)
	} else {
		err = nest.Store(*id, vector)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to insert vector: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Inserted vector: %s\n", *id)
}

func searchCmd() {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	queryStr := fs.String("query", "", "Query vector as JSON array (required)")
	k := fs.Int("k", 10, "Number of results")
	filterStr := fs.String("filter", "", "Filter as JSON (e.g., {\"key\":\"value\"})")

	fs.Parse(os.Args[2:])

	if fs.NArg() < 1 || *queryStr == "" {
		fmt.Fprintf(os.Stderr, "Usage: magpie search <database> --query <json> [--k <num>] [--filter <json>]\n")
		fs.PrintDefaults()
		os.Exit(1)
	}

	dbPath := fs.Arg(0)

	// Parse query vector
	var query []float32
	if err := json.Unmarshal([]byte(*queryStr), &query); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse query: %v\n", err)
		os.Exit(1)
	}

	// Open database
	nest, err := magpie.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer nest.Close()

	// Search
	var results []magpie.Treasure
	if *filterStr != "" {
		// Parse filter
		var filterData map[string]interface{}
		if err := json.Unmarshal([]byte(*filterStr), &filterData); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse filter: %v\n", err)
			os.Exit(1)
		}

		// Build filter (simple equality for now)
		var filter magpie.Filter
		for k, v := range filterData {
			filter = magpie.Eq(k, v)
			break // Only support single filter for now
		}

		results = nest.FindWithFilter(query, *k, filter)
	} else {
		results = nest.Find(query, *k)
	}

	// Print results
	fmt.Printf("Found %d results:\n\n", len(results))
	for i, result := range results {
		fmt.Printf("%d. ID: %s\n", i+1, result.ID)
		fmt.Printf("   Distance: %.6f\n", result.Distance)
		if result.Metadata != nil {
			metaJSON, _ := json.MarshalIndent(result.Metadata, "   ", "  ")
			fmt.Printf("   Metadata: %s\n", metaJSON)
		}
		fmt.Println()
	}
}

func getCmd() {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	fs.Parse(os.Args[2:])

	if fs.NArg() < 2 {
		fmt.Fprintf(os.Stderr, "Usage: magpie get <database> <id>\n")
		os.Exit(1)
	}

	dbPath := fs.Arg(0)
	id := fs.Arg(1)

	nest, err := magpie.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer nest.Close()

	treasure, err := nest.Get(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get vector: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("ID: %s\n", treasure.ID)
	fmt.Printf("Vector: %v\n", treasure.Vector)
	if treasure.Metadata != nil {
		metaJSON, _ := json.MarshalIndent(treasure.Metadata, "", "  ")
		fmt.Printf("Metadata: %s\n", metaJSON)
	}
}

func removeCmd() {
	fs := flag.NewFlagSet("remove", flag.ExitOnError)
	fs.Parse(os.Args[2:])

	if fs.NArg() < 2 {
		fmt.Fprintf(os.Stderr, "Usage: magpie remove <database> <id>\n")
		os.Exit(1)
	}

	dbPath := fs.Arg(0)
	id := fs.Arg(1)

	nest, err := magpie.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer nest.Close()

	if err := nest.Remove(id); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to remove vector: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Removed vector: %s\n", id)
}

func infoCmd() {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	fs.Parse(os.Args[2:])

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: magpie info <database>\n")
		os.Exit(1)
	}

	dbPath := fs.Arg(0)

	nest, err := magpie.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer nest.Close()

	fmt.Printf("Database: %s\n", dbPath)
	fmt.Printf("Vector count: %d\n", nest.Count())

	// File size
	info, err := os.Stat(dbPath)
	if err == nil {
		fmt.Printf("File size: %s\n", formatBytes(info.Size()))
	}

	// TODO: Print more stats when available
	// - Dimensions
	// - Distance metric
	// - HNSW parameters
	// - Fragmentation
}

func compactCmd() {
	fs := flag.NewFlagSet("compact", flag.ExitOnError)
	fs.Parse(os.Args[2:])

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: magpie compact <database>\n")
		os.Exit(1)
	}

	dbPath := fs.Arg(0)

	nest, err := magpie.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer nest.Close()

	fmt.Println("Compacting database...")

	if err := nest.Compact(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to compact: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Compaction complete")
}

func printUsage() {
	usage := `magpie - Vector database CLI

Usage:
  magpie <command> [arguments]

Commands:
  create      Create a new database
  insert      Insert a vector
  search      Search for similar vectors
  get         Get a vector by ID
  remove      Remove a vector
  info        Show database information
  compact     Compact the database
  version     Show version
  help        Show this help

Examples:
  # Create a database
  magpie create mydata.magpie --dimensions 384

  # Insert a vector
  magpie insert mydata.magpie --id doc1 --vector "[0.1,0.2,0.3]" --meta '{"title":"test"}'

  # Search
  magpie search mydata.magpie --query "[0.1,0.2,0.3]" --k 10

  # Get vector
  magpie get mydata.magpie doc1

  # Database info
  magpie info mydata.magpie

Use "magpie <command> --help" for more information about a command.
`
	fmt.Print(usage)
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func parseVector(s string) ([]float32, error) {
	// Remove brackets and whitespace
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "[]")

	// Split by comma
	parts := strings.Split(s, ",")
	vector := make([]float32, len(parts))

	for i, part := range parts {
		part = strings.TrimSpace(part)
		val, err := strconv.ParseFloat(part, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid number: %s", part)
		}
		vector[i] = float32(val)
	}

	return vector, nil
}
