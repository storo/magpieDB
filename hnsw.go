package magpie

import (
	"container/heap"
	"math/rand"
)

// HNSW algorithm implementation
// Based on "Efficient and robust approximate nearest neighbor search using
// Hierarchical Navigable Small World graphs" by Malkov and Yashunin (2018)

// searchLayer performs greedy search for ef nearest neighbors at a given layer.
// OPTIMIZED: Added early termination, visit limits, and better distance thresholds
func (idx *HSNWIndex) searchLayer(query []float32, entryPoints []*HSNWNode, ef int, layer int) []*SearchResult {
	// OPTIMIZATION 1: Preallocate visited map with larger capacity
	visited := make(map[string]bool, ef*3)
	candidates := &MinHeap{}
	results := &MaxHeap{}

	// OPTIMIZATION 2: Visit threshold to prevent excessive exploration
	visitThreshold := ef * 3
	visitCount := 0

	// Initialize with entry points
	for _, ep := range entryPoints {
		if ep == nil {
			continue
		}

		dist := idx.distance(query, ep.Vector)
		visited[ep.ID] = true

		heap.Push(candidates, &SearchResult{Node: ep, Distance: dist})
		heap.Push(results, &SearchResult{Node: ep, Distance: dist})

		if results.Len() > ef {
			heap.Pop(results)
		}
	}

	// Search loop
	for candidates.Len() > 0 {
		current := heap.Pop(candidates).(*SearchResult)

		// OPTIMIZATION 3: Stricter early exit with visit count
		visitCount++
		if visitCount > visitThreshold {
			break
		}

		// OPTIMIZATION 4: Better early termination with distance threshold
		if results.Len() >= ef {
			worstResult := results.Peek()
			// Stop if current is 10% worse than worst result
			if current.Distance > worstResult.Distance*1.1 {
				break
			}
		}

		// OPTIMIZATION 5: Skip if already far from best results
		if results.Len() >= ef && current.Distance > results.Peek().Distance {
			continue
		}

		// Check neighbors at this layer
		neighbors := current.Node.Neighbors
		if layer >= len(neighbors) {
			continue
		}

		for _, neighborID := range neighbors[layer] {
			if visited[neighborID] {
				continue
			}
			visited[neighborID] = true

			neighbor, exists := idx.nodes[neighborID]
			if !exists {
				continue
			}

			dist := idx.distance(query, neighbor.Vector)

			if results.Len() < ef || dist < results.Peek().Distance {
				heap.Push(candidates, &SearchResult{Node: neighbor, Distance: dist})
				heap.Push(results, &SearchResult{Node: neighbor, Distance: dist})

				// OPTIMIZATION 6: Limit result heap size immediately
				if results.Len() > ef {
					heap.Pop(results)
				}
			}
		}
	}

	// Convert results to slice
	return results.ToSlice()
}

// selectNeighbors selects m neighbors using a heuristic.
// Uses a simple strategy for now, can be enhanced with diverse neighbor selection.
func (idx *HSNWIndex) selectNeighbors(candidates []*SearchResult, m int) []string {
	if len(candidates) <= m {
		result := make([]string, len(candidates))
		for i, c := range candidates {
			result[i] = c.Node.ID
		}
		return result
	}

	// Take the m closest neighbors
	result := make([]string, m)
	for i := 0; i < m; i++ {
		result[i] = candidates[i].Node.ID
	}

	return result
}

// insertNode inserts a node into the HNSW graph at all layers.
func (idx *HSNWIndex) insertNode(node *HSNWNode, entryPoint *HSNWNode) {
	// Find nearest neighbors at each layer
	currentNearest := []*HSNWNode{entryPoint}

	// Traverse from top layer to node's layer
	for lc := idx.maxLevel; lc > node.Level; lc-- {
		results := idx.searchLayer(node.Vector, currentNearest, 1, lc)
		if len(results) > 0 {
			currentNearest = []*HSNWNode{results[0].Node}
		}
	}

	// Insert at each layer from node's level to 0
	for lc := node.Level; lc >= 0; lc-- {
		// Find efConstruction nearest neighbors
		results := idx.searchLayer(node.Vector, currentNearest, idx.efConstruct, lc)

		// Select m neighbors
		m := idx.m
		if lc == 0 {
			m = idx.m * 2 // Layer 0 can have more connections
		}

		neighbors := idx.selectNeighbors(results, m)
		node.Neighbors[lc] = neighbors

		// Add bidirectional links
		for _, neighborID := range neighbors {
			neighbor, exists := idx.nodes[neighborID]
			if !exists {
				continue
			}

			// Add node to neighbor's list
			if lc < len(neighbor.Neighbors) {
				neighbor.Neighbors[lc] = append(neighbor.Neighbors[lc], node.ID)

				// Prune if exceeds maximum
				maxConn := idx.m
				if lc == 0 {
					maxConn = idx.m * 2
				}

				if len(neighbor.Neighbors[lc]) > maxConn {
					// Re-select neighbors for this node
					candidateResults := make([]*SearchResult, 0, len(neighbor.Neighbors[lc]))
					for _, nid := range neighbor.Neighbors[lc] {
						n, ok := idx.nodes[nid]
						if !ok {
							continue
						}
						dist := idx.distance(neighbor.Vector, n.Vector)
						candidateResults = append(candidateResults, &SearchResult{Node: n, Distance: dist})
					}

					// Sort by distance
					for i := 0; i < len(candidateResults); i++ {
						for j := i + 1; j < len(candidateResults); j++ {
							if candidateResults[j].Distance < candidateResults[i].Distance {
								candidateResults[i], candidateResults[j] = candidateResults[j], candidateResults[i]
							}
						}
					}

					neighbor.Neighbors[lc] = idx.selectNeighbors(candidateResults, maxConn)
				}
			}
		}

		// Update current nearest for next layer
		if len(results) > 0 {
			currentNearest = make([]*HSNWNode, len(results))
			for i, r := range results {
				currentNearest[i] = r.Node
			}
		}
	}
}

// MinHeap implements a min-heap of SearchResults
type MinHeap []*SearchResult

func (h MinHeap) Len() int           { return len(h) }
func (h MinHeap) Less(i, j int) bool { return h[i].Distance < h[j].Distance }
func (h MinHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *MinHeap) Push(x interface{}) {
	*h = append(*h, x.(*SearchResult))
}

func (h *MinHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func (h MinHeap) Peek() *SearchResult {
	if len(h) == 0 {
		return nil
	}
	return h[0]
}

// MaxHeap implements a max-heap of SearchResults
type MaxHeap []*SearchResult

func (h MaxHeap) Len() int           { return len(h) }
func (h MaxHeap) Less(i, j int) bool { return h[i].Distance > h[j].Distance }
func (h MaxHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *MaxHeap) Push(x interface{}) {
	*h = append(*h, x.(*SearchResult))
}

func (h *MaxHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func (h MaxHeap) Peek() *SearchResult {
	if len(h) == 0 {
		return nil
	}
	return h[0]
}

func (h MaxHeap) ToSlice() []*SearchResult {
	// Convert to slice and sort by distance (ascending)
	result := make([]*SearchResult, len(h))
	copy(result, h)

	// Simple bubble sort
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Distance < result[i].Distance {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}

// randLevel generates a random level for a new node.
func randLevel(maxLevel int) int {
	level := 0
	for level < maxLevel && rand.Float64() < 0.5 {
		level++
	}
	return level
}
