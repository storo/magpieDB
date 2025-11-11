package magpie

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// =============================================================================
// Worker Pool Infrastructure
// =============================================================================

// Task represents a background task that can be executed by workers
type Task interface {
	Execute(nest *Nest) error
	Priority() int
	Name() string
}

// WorkerPool manages a pool of background worker goroutines
type WorkerPool struct {
	nest       *Nest
	tasks      chan Task
	numWorkers int
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.Mutex
	running    bool
	panicCount *int64 // Track number of panics recovered
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(nest *Nest, numWorkers int) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	var panicCounter int64
	return &WorkerPool{
		nest:       nest,
		tasks:      make(chan Task, 100),
		numWorkers: numWorkers,
		ctx:        ctx,
		cancel:     cancel,
		running:    false,
		panicCount: &panicCounter,
	}
}

// Start starts the worker pool and spawns worker goroutines
func (wp *WorkerPool) Start() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.running {
		return fmt.Errorf("worker pool already running")
	}

	wp.running = true

	// Start worker goroutines
	for i := 0; i < wp.numWorkers; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}

	return nil
}

// Stop gracefully stops the worker pool, waiting for current tasks to complete
func (wp *WorkerPool) Stop() error {
	wp.mu.Lock()
	if !wp.running {
		wp.mu.Unlock()
		return nil
	}
	wp.running = false
	wp.mu.Unlock()

	// Cancel context to signal workers to stop
	wp.cancel()

	// Close task channel (no more tasks will be accepted)
	close(wp.tasks)

	// Wait for all workers to finish current tasks
	wp.wg.Wait()

	return nil
}

// SubmitTask adds a task to the worker queue
func (wp *WorkerPool) SubmitTask(task Task) error {
	wp.mu.Lock()
	running := wp.running
	wp.mu.Unlock()

	if !running {
		return fmt.Errorf("worker pool not running")
	}

	// Try to send task, but respect context cancellation
	select {
	case wp.tasks <- task:
		return nil
	case <-wp.ctx.Done():
		return fmt.Errorf("worker pool shutting down")
	}
}

// worker is the main worker goroutine that processes tasks
func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	for {
		select {
		case task, ok := <-wp.tasks:
			if !ok {
				// Channel closed, exit worker
				return
			}

			// Execute task with panic recovery
			wp.safeExecute(id, task)

		case <-wp.ctx.Done():
			// Context cancelled, exit worker
			return
		}
	}
}

// safeExecute wraps task execution with panic recovery
func (wp *WorkerPool) safeExecute(workerID int, task Task) {
	defer func() {
		if r := recover(); r != nil {
			// Increment panic counter
			atomic.AddInt64(wp.panicCount, 1)

			// Get stack trace
			stack := debug.Stack()

			// Log panic with full details
			log.Printf("[WORKER PANIC] Worker %d recovered from panic\n"+
				"Task: %s\n"+
				"Panic: %v\n"+
				"Stack trace:\n%s\n",
				workerID,
				task.Name(),
				r,
				string(stack))
		}
	}()

	// Execute task
	if err := task.Execute(wp.nest); err != nil {
		// Log error but don't crash worker
		// In production, would use proper logging
		log.Printf("[WORKER ERROR] Worker %d: task %s failed: %v", workerID, task.Name(), err)
	}
}

// =============================================================================
// Individual Worker Implementations
// =============================================================================

// AutoCompactionWorker periodically checks if compaction is needed
type AutoCompactionWorker struct {
	interval            time.Duration
	fragmentationThreshold float64
}

// NewAutoCompactionWorker creates a new auto-compaction worker
func NewAutoCompactionWorker(interval time.Duration, threshold float64) *AutoCompactionWorker {
	if threshold == 0 {
		threshold = 0.3 // 30% fragmentation default
	}
	return &AutoCompactionWorker{
		interval:            interval,
		fragmentationThreshold: threshold,
	}
}

func (w *AutoCompactionWorker) Execute(nest *Nest) error {
	// Compaction disabled - feature not yet implemented
	// nest.mu.RLock()
	// needsCompaction := nest.shouldCompact()
	// nest.mu.RUnlock()

	// if needsCompaction {
	// 	return nest.Compact()
	// }
	return nil
}

func (w *AutoCompactionWorker) Priority() int {
	return 5 // Medium priority
}

func (w *AutoCompactionWorker) Name() string {
	return "AutoCompaction"
}

// MVCCGCWorker periodically runs garbage collection on old MVCC versions
type MVCCGCWorker struct {
	interval time.Duration
}

// NewMVCCGCWorker creates a new MVCC garbage collection worker
func NewMVCCGCWorker(interval time.Duration) *MVCCGCWorker {
	return &MVCCGCWorker{
		interval: interval,
	}
}

func (w *MVCCGCWorker) Execute(nest *Nest) error {
	// Run MVCC garbage collection
	_, err := nest.GarbageCollect()
	return err
}

func (w *MVCCGCWorker) Priority() int {
	return 3 // Lower priority
}

func (w *MVCCGCWorker) Name() string {
	return "MVCCGC"
}

// IndexOptimizationWorker periodically optimizes the HNSW index
type IndexOptimizationWorker struct {
	interval time.Duration
}

// NewIndexOptimizationWorker creates a new index optimization worker
func NewIndexOptimizationWorker(interval time.Duration) *IndexOptimizationWorker {
	return &IndexOptimizationWorker{
		interval: interval,
	}
}

func (w *IndexOptimizationWorker) Execute(nest *Nest) error {
	nest.mu.RLock()
	defer nest.mu.RUnlock()

	if nest.index != nil {
		// Call Optimize if it exists (we'll implement this next)
		// For now, just return success
		return nil
	}
	return nil
}

func (w *IndexOptimizationWorker) Priority() int {
	return 4 // Medium-low priority
}

func (w *IndexOptimizationWorker) Name() string {
	return "IndexOptimization"
}

// MetricsAggregationWorker periodically aggregates metrics
type MetricsAggregationWorker struct {
	interval time.Duration
}

// NewMetricsAggregationWorker creates a new metrics aggregation worker
func NewMetricsAggregationWorker(interval time.Duration) *MetricsAggregationWorker {
	return &MetricsAggregationWorker{
		interval: interval,
	}
}

func (w *MetricsAggregationWorker) Execute(nest *Nest) error {
	// Update aggregated metrics
	// Metrics are already tracked in nest.metrics
	// This worker would calculate rates, percentiles, etc.
	return nil
}

func (w *MetricsAggregationWorker) Priority() int {
	return 2 // Low priority
}

func (w *MetricsAggregationWorker) Name() string {
	return "MetricsAggregation"
}

// WALCheckpointWorker periodically checkpoints the WAL to main storage
type WALCheckpointWorker struct {
	interval time.Duration
}

// NewWALCheckpointWorker creates a new WAL checkpoint worker
func NewWALCheckpointWorker(interval time.Duration) *WALCheckpointWorker {
	return &WALCheckpointWorker{
		interval: interval,
	}
}

func (w *WALCheckpointWorker) Execute(nest *Nest) error {
	nest.mu.Lock()
	defer nest.mu.Unlock()

	if nest.wal == nil {
		return nil // No WAL enabled
	}

	// Flush WAL to storage
	if err := nest.wal.Flush(); err != nil {
		return fmt.Errorf("failed to flush WAL: %w", err)
	}

	// Checkpoint: ensure data is persisted
	if nest.storage != nil {
		if err := nest.storage.Sync(); err != nil {
			return fmt.Errorf("failed to sync storage: %w", err)
		}
	}

	// Truncate WAL after successful checkpoint
	if err := nest.wal.Truncate(); err != nil {
		return fmt.Errorf("failed to truncate WAL: %w", err)
	}

	return nil
}

func (w *WALCheckpointWorker) Priority() int {
	return 7 // High priority (data durability)
}

func (w *WALCheckpointWorker) Name() string {
	return "WALCheckpoint"
}

// =============================================================================
// Periodic Task Scheduler
// =============================================================================

// PeriodicTask wraps a task to run it periodically
type PeriodicTask struct {
	task     Task
	interval time.Duration
	pool     *WorkerPool
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewPeriodicTask creates a new periodic task
func NewPeriodicTask(task Task, interval time.Duration, pool *WorkerPool) *PeriodicTask {
	ctx, cancel := context.WithCancel(context.Background())
	return &PeriodicTask{
		task:     task,
		interval: interval,
		pool:     pool,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start begins running the task periodically
func (pt *PeriodicTask) Start() {
	pt.wg.Add(1)
	go func() {
		defer pt.wg.Done()
		ticker := time.NewTicker(pt.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Submit task to pool
				_ = pt.pool.SubmitTask(pt.task)
			case <-pt.ctx.Done():
				return
			}
		}
	}()
}

// Stop stops the periodic task
func (pt *PeriodicTask) Stop() {
	pt.cancel()
	pt.wg.Wait()
}
