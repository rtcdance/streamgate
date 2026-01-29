package optimization

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// IndexMetrics represents index performance metrics
type IndexMetrics struct {
	ID              string
	IndexName       string
	TableName       string
	ColumnNames     []string
	SizeBytes       int64
	UsageCount      int64
	LastUsed        time.Time
	CreatedAt       time.Time
	Fragmentation   float64
	IsUnused        bool
	IsDuplicate     bool
	Recommendations []string
}

// IndexOptimizer optimizes database indexes
type IndexOptimizer struct {
	mu               sync.RWMutex
	indexes          map[string]*IndexMetrics
	unusedIndexes    []*IndexMetrics
	duplicateIndexes []*IndexMetrics
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
}

// NewIndexOptimizer creates a new index optimizer
func NewIndexOptimizer() *IndexOptimizer {
	ctx, cancel := context.WithCancel(context.Background())

	optimizer := &IndexOptimizer{
		indexes:          make(map[string]*IndexMetrics),
		unusedIndexes:    make([]*IndexMetrics, 0),
		duplicateIndexes: make([]*IndexMetrics, 0),
		ctx:              ctx,
		cancel:           cancel,
	}

	optimizer.start()
	return optimizer
}

// start begins the optimizer
func (io *IndexOptimizer) start() {
	io.wg.Add(1)
	go io.optimizationLoop()
}

// optimizationLoop periodically optimizes indexes
func (io *IndexOptimizer) optimizationLoop() {
	defer io.wg.Done()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-io.ctx.Done():
			return
		case <-ticker.C:
			io.analyzeIndexes()
		}
	}
}

// RegisterIndex registers an index
func (io *IndexOptimizer) RegisterIndex(indexName, tableName string, columnNames []string, sizeBytes int64) {
	io.mu.Lock()
	defer io.mu.Unlock()

	metric := &IndexMetrics{
		ID:          uuid.New().String(),
		IndexName:   indexName,
		TableName:   tableName,
		ColumnNames: columnNames,
		SizeBytes:   sizeBytes,
		CreatedAt:   time.Now(),
		LastUsed:    time.Now(),
	}

	io.indexes[indexName] = metric
}

// RecordIndexUsage records index usage
func (io *IndexOptimizer) RecordIndexUsage(indexName string) {
	io.mu.Lock()
	defer io.mu.Unlock()

	if metric, ok := io.indexes[indexName]; ok {
		metric.UsageCount++
		metric.LastUsed = time.Now()
	}
}

// RecordIndexFragmentation records index fragmentation
func (io *IndexOptimizer) RecordIndexFragmentation(indexName string, fragmentation float64) {
	io.mu.Lock()
	defer io.mu.Unlock()

	if metric, ok := io.indexes[indexName]; ok {
		metric.Fragmentation = fragmentation
	}
}

// analyzeIndexes analyzes all indexes
func (io *IndexOptimizer) analyzeIndexes() {
	io.mu.Lock()
	defer io.mu.Unlock()

	io.unusedIndexes = make([]*IndexMetrics, 0)
	io.duplicateIndexes = make([]*IndexMetrics, 0)

	now := time.Now()

	// Find unused indexes
	for _, metric := range io.indexes {
		if metric.UsageCount == 0 && now.Sub(metric.CreatedAt) > 24*time.Hour {
			metric.IsUnused = true
			metric.Recommendations = append(metric.Recommendations, "Consider dropping this unused index")
			io.unusedIndexes = append(io.unusedIndexes, metric)
		}

		// Check fragmentation
		if metric.Fragmentation > 30 {
			metric.Recommendations = append(metric.Recommendations, fmt.Sprintf("Index fragmentation is %.2f%% - consider rebuilding", metric.Fragmentation))
		}

		// Check size
		if metric.SizeBytes > 1024*1024*100 { // 100MB
			metric.Recommendations = append(metric.Recommendations, fmt.Sprintf("Index size is %.2f MB - consider optimization", float64(metric.SizeBytes)/1024/1024))
		}
	}

	// Find duplicate indexes
	io.findDuplicateIndexes()
}

// findDuplicateIndexes finds duplicate indexes
func (io *IndexOptimizer) findDuplicateIndexes() {
	indexMap := make(map[string][]*IndexMetrics)

	for _, metric := range io.indexes {
		key := metric.TableName
		for _, col := range metric.ColumnNames {
			key += ":" + col
		}

		indexMap[key] = append(indexMap[key], metric)
	}

	for _, indexes := range indexMap {
		if len(indexes) > 1 {
			for i := 1; i < len(indexes); i++ {
				indexes[i].IsDuplicate = true
				indexes[i].Recommendations = append(indexes[i].Recommendations, "This index is a duplicate - consider dropping")
				io.duplicateIndexes = append(io.duplicateIndexes, indexes[i])
			}
		}
	}
}

// GetIndexMetrics returns index metrics
func (io *IndexOptimizer) GetIndexMetrics(indexName string) *IndexMetrics {
	io.mu.RLock()
	defer io.mu.RUnlock()

	return io.indexes[indexName]
}

// GetAllIndexMetrics returns all index metrics
func (io *IndexOptimizer) GetAllIndexMetrics() []*IndexMetrics {
	io.mu.RLock()
	defer io.mu.RUnlock()

	var metrics []*IndexMetrics
	for _, metric := range io.indexes {
		metrics = append(metrics, metric)
	}

	return metrics
}

// GetUnusedIndexes returns unused indexes
func (io *IndexOptimizer) GetUnusedIndexes() []*IndexMetrics {
	io.mu.RLock()
	defer io.mu.RUnlock()

	return io.unusedIndexes
}

// GetDuplicateIndexes returns duplicate indexes
func (io *IndexOptimizer) GetDuplicateIndexes() []*IndexMetrics {
	io.mu.RLock()
	defer io.mu.RUnlock()

	return io.duplicateIndexes
}

// GetFragmentedIndexes returns fragmented indexes
func (io *IndexOptimizer) GetFragmentedIndexes(threshold float64) []*IndexMetrics {
	io.mu.RLock()
	defer io.mu.RUnlock()

	var fragmented []*IndexMetrics
	for _, metric := range io.indexes {
		if metric.Fragmentation > threshold {
			fragmented = append(fragmented, metric)
		}
	}

	return fragmented
}

// GetOptimizationRecommendations returns optimization recommendations
func (io *IndexOptimizer) GetOptimizationRecommendations() []string {
	io.mu.RLock()
	defer io.mu.Unlock()

	var recommendations []string

	// Check for unused indexes
	if len(io.unusedIndexes) > 0 {
		recommendations = append(recommendations, fmt.Sprintf("Found %d unused indexes - consider dropping", len(io.unusedIndexes)))
	}

	// Check for duplicate indexes
	if len(io.duplicateIndexes) > 0 {
		recommendations = append(recommendations, fmt.Sprintf("Found %d duplicate indexes - consider consolidating", len(io.duplicateIndexes)))
	}

	// Check for fragmented indexes
	fragmentedCount := 0
	for _, metric := range io.indexes {
		if metric.Fragmentation > 30 {
			fragmentedCount++
		}
	}

	if fragmentedCount > 0 {
		recommendations = append(recommendations, fmt.Sprintf("Found %d fragmented indexes - consider rebuilding", fragmentedCount))
	}

	// Check total index size
	var totalSize int64
	for _, metric := range io.indexes {
		totalSize += metric.SizeBytes
	}

	if totalSize > 1024*1024*1024 { // 1GB
		recommendations = append(recommendations, fmt.Sprintf("Total index size is %.2f GB - consider optimization", float64(totalSize)/1024/1024/1024))
	}

	return recommendations
}

// GetIndexStats returns statistics for an index
func (io *IndexOptimizer) GetIndexStats(indexName string) map[string]interface{} {
	io.mu.RLock()
	defer io.mu.RUnlock()

	stats := make(map[string]interface{})

	if metric, ok := io.indexes[indexName]; ok {
		stats["index_name"] = metric.IndexName
		stats["table_name"] = metric.TableName
		stats["size_mb"] = float64(metric.SizeBytes) / 1024 / 1024
		stats["usage_count"] = metric.UsageCount
		stats["fragmentation"] = metric.Fragmentation
		stats["is_unused"] = metric.IsUnused
		stats["is_duplicate"] = metric.IsDuplicate
		stats["recommendations"] = metric.Recommendations
	}

	return stats
}

// Close closes the optimizer
func (io *IndexOptimizer) Close() error {
	io.cancel()
	io.wg.Wait()
	return nil
}
