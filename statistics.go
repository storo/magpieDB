package magpie

import (
	"encoding/json"
	"os"
	"time"
)

// StatisticsWorker collects and exports database statistics
type StatisticsWorker struct {
	interval   time.Duration
	exportPath string
}

// NewStatisticsWorker creates a new statistics worker
func NewStatisticsWorker(interval time.Duration, exportPath string) *StatisticsWorker {
	return &StatisticsWorker{
		interval:   interval,
		exportPath: exportPath,
	}
}

func (w *StatisticsWorker) Execute(nest *Nest) error {
	stats := nest.CollectStatistics()
	return stats.ExportToFile(w.exportPath)
}

func (w *StatisticsWorker) Priority() int {
	return 8
}

func (w *StatisticsWorker) Name() string {
	return "Statistics"
}

// DatabaseStatistics holds comprehensive database metrics
type DatabaseStatistics struct {
	Timestamp      time.Time    `json:"timestamp"`
	VectorCount    int64        `json:"vector_count"`
	Dimensions     int          `json:"dimensions"`
	IndexQuality   float64      `json:"index_quality"`
	CacheHitRate   float64      `json:"cache_hit_rate"`
	StorageMetrics StorageStats `json:"storage"`
	MemoryUsage    int64        `json:"memory_usage_bytes"`
}

// StorageStats contains storage-related metrics
type StorageStats struct {
	FileSize      int64   `json:"file_size_bytes"`
	UsedPages     int64   `json:"used_pages"`
	Fragmentation float64 `json:"fragmentation"`
	WALSize       int64   `json:"wal_size_bytes"`
}

// CollectStatistics aggregates all database metrics
func (n *Nest) CollectStatistics() *DatabaseStatistics {
	n.mu.RLock()
	defer n.mu.RUnlock()

	stats := &DatabaseStatistics{
		Timestamp:   time.Now(),
		VectorCount: int64(n.header.VectorCount),
		Dimensions:  int(n.header.Dimensions),
	}

	if n.index != nil {
		stats.IndexQuality = analyzeIndexQuality(n.index)
	}

	if n.queryCache != nil {
		_, _, hitRate := n.queryCache.Stats()
		stats.CacheHitRate = hitRate
	}

	if fileInfo, err := os.Stat(n.path); err == nil {
		stats.StorageMetrics.FileSize = fileInfo.Size()
		stats.StorageMetrics.UsedPages = int64(n.header.PageCount)

		optimalSize := n.estimateOptimalSize()
		if optimalSize > 0 && stats.StorageMetrics.FileSize > optimalSize {
			wastedSpace := stats.StorageMetrics.FileSize - optimalSize
			stats.StorageMetrics.Fragmentation = float64(wastedSpace) / float64(stats.StorageMetrics.FileSize)
		}
	}

	if n.wal != nil {
		stats.StorageMetrics.WALSize = n.wal.Size()
	}

	stats.MemoryUsage = n.estimateMemoryUsage()

	return stats
}

// ExportToFile writes statistics to a JSON file
func (s *DatabaseStatistics) ExportToFile(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// estimateOptimalSize calculates the theoretical minimum file size
func (n *Nest) estimateOptimalSize() int64 {
	size := int64(PageSize)

	vectorCount := int64(n.header.VectorCount)
	vectorsPerPage := PageSize / (int(n.header.Dimensions)*4 + 100)
	if vectorsPerPage == 0 {
		vectorsPerPage = 1
	}
	vectorPages := (vectorCount + int64(vectorsPerPage) - 1) / int64(vectorsPerPage)
	size += vectorPages * PageSize

	if n.index != nil {
		indexPages := (int64(len(n.index.nodes)) + 49) / 50
		size += indexPages * PageSize
	}

	return size
}

// estimateMemoryUsage approximates in-memory data structures size
func (n *Nest) estimateMemoryUsage() int64 {
	var total int64

	if n.index != nil {
		total += int64(len(n.index.nodes)) * 1024
	}

	if n.queryCache != nil {
		total += int64(n.queryCache.Size()) * 2048
	}

	total += 1024 * 1024

	return total
}

// GetHitRate returns the query cache hit rate (helper for cache)
func (qc *QueryCache) GetHitRate() float64 {
	_, _, hitRate := qc.Stats()
	return hitRate
}
