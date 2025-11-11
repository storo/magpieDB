package magpie

import (
	"fmt"
	"math"
	"time"
)

// ReindexWorker periodically rebuilds the HNSW index to maintain quality
type ReindexWorker struct {
	interval         time.Duration
	qualityThreshold float64
}

// NewReindexWorker creates a new reindex worker
func NewReindexWorker(interval time.Duration, threshold float64) *ReindexWorker {
	if threshold == 0 {
		threshold = 0.85 // Default: 85% quality threshold
	}
	return &ReindexWorker{
		interval:         interval,
		qualityThreshold: threshold,
	}
}

func (w *ReindexWorker) Execute(nest *Nest) error {
	// Check if reindex is needed (requires read lock)
	nest.mu.RLock()
	if nest.index == nil {
		nest.mu.RUnlock()
		return nil // No index to reindex
	}
	quality := analyzeIndexQuality(nest.index)
	needsReindex := quality < w.qualityThreshold
	nest.mu.RUnlock()

	if !needsReindex {
		return nil // Index quality is good
	}

	// Perform reindex with write lock
	return nest.Reindex()
}

func (w *ReindexWorker) Priority() int {
	return 6 // Medium-high priority
}

func (w *ReindexWorker) Name() string {
	return "Reindex"
}

// analyzeIndexQuality calculates index quality (0.0-1.0)
func analyzeIndexQuality(idx *HSNWIndex) float64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if len(idx.nodes) < 10 {
		return 1.0 // Too small to measure degradation
	}

	connectedCount := 0
	neighborCounts := make([]int, 0, len(idx.nodes)*2)

	for _, node := range idx.nodes {
		if len(node.Neighbors) > 0 && len(node.Neighbors[0]) > 0 {
			connectedCount++
		}
		for _, neighbors := range node.Neighbors {
			neighborCounts = append(neighborCounts, len(neighbors))
		}
	}

	connectivity := float64(connectedCount) / float64(len(idx.nodes))

	if len(neighborCounts) == 0 {
		return connectivity
	}

	avgNeighbors := 0.0
	for _, count := range neighborCounts {
		avgNeighbors += float64(count)
	}
	avgNeighbors /= float64(len(neighborCounts))

	variance := 0.0
	for _, count := range neighborCounts {
		diff := float64(count) - avgNeighbors
		variance += diff * diff
	}
	variance /= float64(len(neighborCounts))

	maxVariance := float64(idx.m * idx.m)
	normalizedVariance := math.Min(variance/maxVariance, 1.0)

	quality := connectivity*0.7 + (1.0-normalizedVariance)*0.3
	return quality
}

// Reindex rebuilds the HNSW index from scratch
func (n *Nest) Reindex() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.index == nil {
		return nil
	}

	vectors := make([]struct {
		id  string
		vec []float32
	}, 0, len(n.index.nodes))

	for id, node := range n.index.nodes {
		vecCopy := make([]float32, len(node.Vector))
		copy(vecCopy, node.Vector)
		vectors = append(vectors, struct {
			id  string
			vec []float32
		}{id, vecCopy})
	}

	newIndex := NewHSNWIndex(
		n.index.m,
		n.index.efConstruct,
		len(vectors[0].vec),
		n.index.distance,
	)

	for _, v := range vectors {
		if err := newIndex.Add(v.id, v.vec); err != nil {
			return fmt.Errorf("reindex insert failed for %s: %w", v.id, err)
		}
	}

	n.index = newIndex
	return nil
}
