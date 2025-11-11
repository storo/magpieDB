package magpie

import (
	"container/list"
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
)

// QueryCache implements an LRU cache for query results
type QueryCache struct {
	entries   map[string]*CacheEntry
	lruList   *list.List
	maxSize   int
	hitCount  atomic.Int64
	missCount atomic.Int64
	mu        sync.RWMutex
}

// CacheEntry represents a single cache entry
type CacheEntry struct {
	key     string
	results []Treasure
	element *list.Element
}

// NewQueryCache creates a new query cache with the specified maximum size
func NewQueryCache(maxSize int) *QueryCache {
	return &QueryCache{
		entries: make(map[string]*CacheEntry),
		lruList: list.New(),
		maxSize: maxSize,
	}
}

// Get retrieves results from the cache
// Returns the cached results and true if found, nil and false otherwise
func (qc *QueryCache) Get(key string) ([]Treasure, bool) {
	qc.mu.RLock()
	defer qc.mu.RUnlock()

	entry, exists := qc.entries[key]
	if !exists {
		qc.missCount.Add(1)
		return nil, false
	}

	// Move to front (most recently used)
	qc.lruList.MoveToFront(entry.element)
	qc.hitCount.Add(1)

	return entry.results, true
}

// Put adds or updates results in the cache
func (qc *QueryCache) Put(key string, results []Treasure) {
	qc.mu.Lock()
	defer qc.mu.Unlock()

	// Check if already exists
	if entry, exists := qc.entries[key]; exists {
		qc.lruList.MoveToFront(entry.element)
		entry.results = results
		return
	}

	// Evict if at capacity
	for qc.lruList.Len() >= qc.maxSize {
		oldest := qc.lruList.Back()
		if oldest != nil {
			oldKey := oldest.Value.(string)
			delete(qc.entries, oldKey)
			qc.lruList.Remove(oldest)
		}
	}

	// Add new entry
	element := qc.lruList.PushFront(key)
	qc.entries[key] = &CacheEntry{
		key:     key,
		results: results,
		element: element,
	}
}

// Invalidate clears all entries from the cache
func (qc *QueryCache) Invalidate() {
	qc.mu.Lock()
	defer qc.mu.Unlock()

	qc.entries = make(map[string]*CacheEntry)
	qc.lruList = list.New()
}

// Stats returns cache statistics
// Returns: hits, misses, hit rate (0.0 to 1.0)
func (qc *QueryCache) Stats() (hits, misses int64, hitRate float64) {
	hits = qc.hitCount.Load()
	misses = qc.missCount.Load()
	total := hits + misses

	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	return hits, misses, hitRate
}

// Size returns the current number of entries in the cache
func (qc *QueryCache) Size() int {
	qc.mu.RLock()
	defer qc.mu.RUnlock()

	return len(qc.entries)
}

// GenerateCacheKey creates a cache key from query parameters
// Uses FNV-1a hash for speed and good distribution
func GenerateCacheKey(query []float32, k int, filter Filter) string {
	h := fnv.New64a()

	// Hash query vector
	for _, v := range query {
		// Convert float32 to bytes
		bits := uint32(v * 1000000) // Scale for precision
		h.Write([]byte{
			byte(bits),
			byte(bits >> 8),
			byte(bits >> 16),
			byte(bits >> 24),
		})
	}

	// Hash k parameter
	h.Write([]byte(fmt.Sprintf("k=%d", k)))

	// Hash filter if present
	if filter != nil {
		// Create a simple string representation of the filter
		// This is a simplified approach - production code would need
		// a more sophisticated filter serialization
		h.Write([]byte(fmt.Sprintf("filter=%v", filter)))
	}

	return fmt.Sprintf("%x", h.Sum64())
}
