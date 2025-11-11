package magpie

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
)

// NewHSNWIndex creates a new HNSW index with the given parameters.
func NewHSNWIndex(dimensions int, m int, efConstruct int, distance DistanceFunc) *HSNWIndex {
	return &HSNWIndex{
		nodes:       make(map[string]*HSNWNode),
		entryPoint:  nil,
		maxLevel:    0,
		m:           m,
		efConstruct: efConstruct,
		distance:    distance,
	}
}

// Add inserts a new vector into the HNSW index.
func (idx *HSNWIndex) Add(id string, vector []float32) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Check if already exists
	if _, exists := idx.nodes[id]; exists {
		return fmt.Errorf("vector with ID %s already exists", id)
	}

	// Create new node
	level := idx.assignLevel()
	node := &HSNWNode{
		ID:        id,
		Vector:    make([]float32, len(vector)),
		Level:     level,
		Neighbors: make([][]string, level+1),
	}
	copy(node.Vector, vector)

	// Initialize neighbor lists for each level
	for i := range node.Neighbors {
		node.Neighbors[i] = make([]string, 0, idx.m)
	}

	// Add to index
	idx.nodes[id] = node

	// If this is the first node, set it as entry point
	if idx.entryPoint == nil {
		idx.entryPoint = node
		idx.maxLevel = level
		return nil
	}

	// Insert node into HNSW graph
	idx.insertNode(node, idx.entryPoint)

	// Update maxLevel if necessary
	if level > idx.maxLevel {
		idx.maxLevel = level
		idx.entryPoint = node
	}

	return nil
}

// Search finds the k nearest neighbors to the query vector.
func (idx *HSNWIndex) Search(query []float32, k int) []Treasure {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if idx.entryPoint == nil {
		return nil
	}

	// Use efConstruct as default ef for search quality
	ef := idx.efConstruct
	if ef < k {
		ef = k
	}

	// Start from entry point and navigate down layers
	currentNearest := []*HSNWNode{idx.entryPoint}

	// Greedy search through upper layers (from maxLevel down to 1)
	for level := idx.maxLevel; level > 0; level-- {
		results := idx.searchLayer(query, currentNearest, 1, level)
		if len(results) > 0 {
			currentNearest = []*HSNWNode{results[0].Node}
		}
	}

	// Search layer 0 with ef for better recall
	results := idx.searchLayer(query, currentNearest, ef, 0)

	// Convert to Treasure results and return top k
	treasures := make([]Treasure, 0, k)
	for i := 0; i < len(results) && i < k; i++ {
		treasures = append(treasures, Treasure{
			ID:       results[i].Node.ID,
			Vector:   results[i].Node.Vector,
			Distance: results[i].Distance,
		})
	}

	return treasures
}

// Remove removes a vector from the index.
func (idx *HSNWIndex) Remove(id string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	node, exists := idx.nodes[id]
	if !exists {
		return ErrVectorNotFound
	}

	// 1. Remove from all neighbor lists (clean incoming links)
	for _, otherNode := range idx.nodes {
		if otherNode.ID == id {
			continue
		}

		for level := 0; level <= otherNode.Level; level++ {
			// Remove 'id' from neighbor lists
			otherNode.Neighbors[level] = removeFromSlice(otherNode.Neighbors[level], id)
		}
	}

	// 2. Reconnect neighbors (maintain graph connectivity)
	// For each level, connect neighbors of deleted node to each other
	for level := 0; level <= node.Level; level++ {
		neighbors := node.Neighbors[level]
		idx.reconnectNeighbors(neighbors, level)
	}

	// 3. Remove node from map
	delete(idx.nodes, id)

	// 4. Update entry point if this node was entry point (do this AFTER deletion)
	if idx.entryPoint != nil && idx.entryPoint.ID == id {
		idx.entryPoint = idx.selectNewEntryPoint()
		if idx.entryPoint != nil {
			idx.maxLevel = idx.entryPoint.Level
		} else {
			idx.maxLevel = 0
		}
	}

	return nil
}

// Get retrieves a node by ID.
func (idx *HSNWIndex) Get(id string) (*HSNWNode, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	node, exists := idx.nodes[id]
	return node, exists
}

// Count returns the number of nodes in the index.
func (idx *HSNWIndex) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return len(idx.nodes)
}

// MaxLevel returns the maximum level in the HNSW graph.
func (idx *HSNWIndex) MaxLevel() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return idx.maxLevel
}

// assignLevel assigns a random level to a new node using exponential decay.
func (idx *HSNWIndex) assignLevel() int {
	level := 0
	maxLevel := 16 // Default maximum level

	// Exponential decay probability (p ≈ 0.5)
	for level < maxLevel && rand.Float64() < 0.5 {
		level++
	}

	return level
}

// Clear removes all nodes from the index.
func (idx *HSNWIndex) Clear() {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.nodes = make(map[string]*HSNWNode)
	idx.entryPoint = nil
	idx.maxLevel = 0
}

// Serialize serializes the index to bytes for storage.
func (idx *HSNWIndex) Serialize() ([]byte, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	buf := new(bytes.Buffer)

	// Write header: node count, maxLevel, m, efConstruct
	binary.Write(buf, binary.LittleEndian, uint32(len(idx.nodes)))
	binary.Write(buf, binary.LittleEndian, uint32(idx.maxLevel))
	binary.Write(buf, binary.LittleEndian, uint32(idx.m))
	binary.Write(buf, binary.LittleEndian, uint32(idx.efConstruct))

	// Write entry point ID (or empty if nil)
	entryPointID := ""
	if idx.entryPoint != nil {
		entryPointID = idx.entryPoint.ID
	}
	binary.Write(buf, binary.LittleEndian, uint32(len(entryPointID)))
	buf.WriteString(entryPointID)

	// Write each node
	for _, node := range idx.nodes {
		// Write ID
		binary.Write(buf, binary.LittleEndian, uint32(len(node.ID)))
		buf.WriteString(node.ID)

		// Write level
		binary.Write(buf, binary.LittleEndian, uint32(node.Level))

		// Write vector dimensions and data
		binary.Write(buf, binary.LittleEndian, uint32(len(node.Vector)))
		for _, v := range node.Vector {
			binary.Write(buf, binary.LittleEndian, v)
		}

		// Write neighbor lists for each level
		for level := 0; level <= node.Level; level++ {
			neighbors := node.Neighbors[level]
			binary.Write(buf, binary.LittleEndian, uint32(len(neighbors)))
			for _, nid := range neighbors {
				binary.Write(buf, binary.LittleEndian, uint32(len(nid)))
				buf.WriteString(nid)
			}
		}
	}

	return buf.Bytes(), nil
}

// Deserialize loads an index from serialized bytes.
func (idx *HSNWIndex) Deserialize(data []byte) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	buf := bytes.NewReader(data)

	// Read header
	var nodeCount, maxLevel, m, efConstruct uint32
	binary.Read(buf, binary.LittleEndian, &nodeCount)
	binary.Read(buf, binary.LittleEndian, &maxLevel)
	binary.Read(buf, binary.LittleEndian, &m)
	binary.Read(buf, binary.LittleEndian, &efConstruct)

	idx.maxLevel = int(maxLevel)
	idx.m = int(m)
	idx.efConstruct = int(efConstruct)
	idx.nodes = make(map[string]*HSNWNode, nodeCount)

	// Read entry point ID
	var entryIDLen uint32
	binary.Read(buf, binary.LittleEndian, &entryIDLen)
	entryIDBytes := make([]byte, entryIDLen)
	buf.Read(entryIDBytes)
	entryPointID := string(entryIDBytes)

	// Read each node
	for i := uint32(0); i < nodeCount; i++ {
		// Read ID
		var idLen uint32
		binary.Read(buf, binary.LittleEndian, &idLen)
		idBytes := make([]byte, idLen)
		buf.Read(idBytes)
		id := string(idBytes)

		// Read level
		var level uint32
		binary.Read(buf, binary.LittleEndian, &level)

		// Read vector
		var vecLen uint32
		binary.Read(buf, binary.LittleEndian, &vecLen)
		vector := make([]float32, vecLen)
		for j := uint32(0); j < vecLen; j++ {
			binary.Read(buf, binary.LittleEndian, &vector[j])
		}

		// Create node
		node := &HSNWNode{
			ID:        id,
			Vector:    vector,
			Level:     int(level),
			Neighbors: make([][]string, level+1),
		}

		// Read neighbor lists
		for lv := uint32(0); lv <= level; lv++ {
			var neighborCount uint32
			binary.Read(buf, binary.LittleEndian, &neighborCount)
			neighbors := make([]string, neighborCount)
			for j := uint32(0); j < neighborCount; j++ {
				var nidLen uint32
				binary.Read(buf, binary.LittleEndian, &nidLen)
				nidBytes := make([]byte, nidLen)
				buf.Read(nidBytes)
				neighbors[j] = string(nidBytes)
			}
			node.Neighbors[lv] = neighbors
		}

		idx.nodes[id] = node

		// Set entry point if this is it
		if id == entryPointID {
			idx.entryPoint = node
		}
	}

	return nil
}

// Helper: select new entry point (node with highest level)
func (idx *HSNWIndex) selectNewEntryPoint() *HSNWNode {
	var bestNode *HSNWNode
	maxLevel := -1

	for _, node := range idx.nodes {
		if node.Level > maxLevel {
			maxLevel = node.Level
			bestNode = node
		}
	}

	return bestNode
}

// Helper: reconnect neighbors after node removal
func (idx *HSNWIndex) reconnectNeighbors(neighbors []string, level int) {
	// For small number of neighbors, create bidirectional links
	for i, id1 := range neighbors {
		node1, exists := idx.nodes[id1]
		if !exists || level > node1.Level {
			continue
		}

		for j, id2 := range neighbors {
			if i >= j {
				continue
			}

			node2, exists := idx.nodes[id2]
			if !exists || level > node2.Level {
				continue
			}

			// Add mutual connections if not already connected
			if !contains(node1.Neighbors[level], id2) && len(node1.Neighbors[level]) < idx.m*2 {
				node1.Neighbors[level] = append(node1.Neighbors[level], id2)
			}
			if !contains(node2.Neighbors[level], id1) && len(node2.Neighbors[level]) < idx.m*2 {
				node2.Neighbors[level] = append(node2.Neighbors[level], id1)
			}
		}
	}
}

// Helper functions
func removeFromSlice(slice []string, value string) []string {
	result := make([]string, 0, len(slice))
	for _, v := range slice {
		if v != value {
			result = append(result, v)
		}
	}
	return result
}

func contains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

// OptimizeIndex performs graph optimization
func (idx *HSNWIndex) OptimizeIndex() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// 1. Rebalance entry point
	idx.optimizeEntryPoint()

	// 2. Clean up dangling references
	idx.cleanupDanglingReferences()

	return nil
}

func (idx *HSNWIndex) optimizeEntryPoint() {
	// Select best entry point based on:
	// - Highest level
	// - Good connectivity
	bestNode := idx.selectNewEntryPoint()
	if bestNode != nil {
		idx.entryPoint = bestNode
		idx.maxLevel = bestNode.Level
	}
}

func (idx *HSNWIndex) cleanupDanglingReferences() {
	// Remove references to non-existent nodes
	for _, node := range idx.nodes {
		for level := 0; level <= node.Level; level++ {
			cleaned := make([]string, 0, len(node.Neighbors[level]))
			for _, neighborID := range node.Neighbors[level] {
				if _, exists := idx.nodes[neighborID]; exists {
					cleaned = append(cleaned, neighborID)
				}
			}
			node.Neighbors[level] = cleaned
		}
	}
}

// IndexDiagnostics returns index health information
type IndexDiagnostics struct {
	NodeCount          int
	MaxLevel           int
	AvgDegree          float64
	DanglingReferences int
	OrphanedNodes      int
	DisconnectedNodes  int
	EntryPointValid    bool
}

func (idx *HSNWIndex) Diagnostics() *IndexDiagnostics {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	diag := &IndexDiagnostics{
		NodeCount: len(idx.nodes),
		MaxLevel:  idx.maxLevel,
	}

	// Calculate average degree
	totalDegree := 0
	for _, node := range idx.nodes {
		for level := 0; level <= node.Level; level++ {
			totalDegree += len(node.Neighbors[level])
		}
	}
	if len(idx.nodes) > 0 {
		// Average across all levels of all nodes
		totalLevels := 0
		for _, node := range idx.nodes {
			totalLevels += node.Level + 1
		}
		if totalLevels > 0 {
			diag.AvgDegree = float64(totalDegree) / float64(totalLevels)
		}
	}

	// Detect dangling references
	for _, node := range idx.nodes {
		for level := 0; level <= node.Level; level++ {
			for _, neighborID := range node.Neighbors[level] {
				if _, exists := idx.nodes[neighborID]; !exists {
					diag.DanglingReferences++
				}
			}
		}
	}

	// Detect orphaned nodes (no incoming connections)
	incomingCounts := make(map[string]int)
	for _, node := range idx.nodes {
		for level := 0; level <= node.Level; level++ {
			for _, neighborID := range node.Neighbors[level] {
				incomingCounts[neighborID]++
			}
		}
	}

	for id := range idx.nodes {
		if incomingCounts[id] == 0 && (idx.entryPoint == nil || id != idx.entryPoint.ID) {
			diag.OrphanedNodes++
		}
	}

	// Check entry point validity
	if idx.entryPoint != nil {
		_, diag.EntryPointValid = idx.nodes[idx.entryPoint.ID]
	} else {
		diag.EntryPointValid = len(idx.nodes) == 0
	}

	return diag
}

// IndexStats returns index statistics
type IndexStats struct {
	NodeCount    int
	MaxLevel     int
	AvgNeighbors float64
	MemoryUsage  int64
}

func (idx *HSNWIndex) Stats() *IndexStats {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	stats := &IndexStats{
		NodeCount: len(idx.nodes),
		MaxLevel:  idx.maxLevel,
	}

	// Calculate average neighbors
	totalNeighbors := 0
	totalLevels := 0
	for _, node := range idx.nodes {
		for level := 0; level <= node.Level; level++ {
			totalNeighbors += len(node.Neighbors[level])
			totalLevels++
		}
	}
	if totalLevels > 0 {
		stats.AvgNeighbors = float64(totalNeighbors) / float64(totalLevels)
	}

	// Estimate memory usage
	for _, node := range idx.nodes {
		stats.MemoryUsage += int64(len(node.Vector) * 4) // float32 = 4 bytes
		stats.MemoryUsage += int64(len(node.ID))
		for level := 0; level <= node.Level; level++ {
			stats.MemoryUsage += int64(len(node.Neighbors[level]) * 8) // string pointer
		}
	}

	return stats
}
