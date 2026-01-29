package optimization

import (
	"context"
	"sync"
	"time"
)

// Service provides performance optimization functionality
type Service struct {
	mu                sync.RWMutex
	cache             *MultiLevelCache
	queryOptimizer    *QueryOptimizer
	indexOptimizer    *IndexOptimizer
	resourceOptimizer *ResourceOptimizer
	ctx               context.Context
	cancel            context.CancelFunc
}

// NewService creates a new optimization service
func NewService() *Service {
	ctx, cancel := context.WithCancel(context.Background())

	service := &Service{
		cache:             NewMultiLevelCache(1000, 10000, 100000),
		queryOptimizer:    NewQueryOptimizer(100.0), // 100ms threshold
		indexOptimizer:    NewIndexOptimizer(),
		resourceOptimizer: NewResourceOptimizer(500*1024*1024, 80.0), // 500MB, 80% CPU
		ctx:               ctx,
		cancel:            cancel,
	}

	return service
}

// Cache operations

// SetCache sets a value in the cache
func (s *Service) SetCache(key string, value interface{}, ttl time.Duration, level CacheLevel) error {
	return s.cache.Set(key, value, ttl, level)
}

// GetCache retrieves a value from the cache
func (s *Service) GetCache(key string) (interface{}, bool) {
	return s.cache.Get(key)
}

// DeleteCache deletes a value from the cache
func (s *Service) DeleteCache(key string) {
	s.cache.Delete(key)
}

// ClearCache clears all caches
func (s *Service) ClearCache() {
	s.cache.Clear()
}

// GetCacheStats returns cache statistics
func (s *Service) GetCacheStats() *CacheStats {
	return s.cache.GetStats()
}

// Query optimization operations

// RecordQuery records a query execution
func (s *Service) RecordQuery(query string, executionTime float64, rowsAffected, rowsScanned int64, indexUsed string) {
	s.queryOptimizer.RecordQuery(query, executionTime, rowsAffected, rowsScanned, indexUsed)
}

// AnalyzePlan analyzes a query execution plan
func (s *Service) AnalyzePlan(query, planText string, estimatedCost, actualCost float64, indexUsed string) *QueryPlan {
	return s.queryOptimizer.AnalyzePlan(query, planText, estimatedCost, actualCost, indexUsed)
}

// GetSlowQueries returns slow queries
func (s *Service) GetSlowQueries(limit int) []*QueryMetrics {
	return s.queryOptimizer.GetSlowQueries(limit)
}

// GetQueryMetrics returns query metrics
func (s *Service) GetQueryMetrics(limit int) []*QueryMetrics {
	return s.queryOptimizer.GetQueryMetrics(limit)
}

// GetQueryPlan returns a query plan
func (s *Service) GetQueryPlan(query string) *QueryPlan {
	return s.queryOptimizer.GetQueryPlan(query)
}

// GetAverageExecutionTime returns average execution time for a query
func (s *Service) GetAverageExecutionTime(query string) float64 {
	return s.queryOptimizer.GetAverageExecutionTime(query)
}

// GetQueryStats returns statistics for a query
func (s *Service) GetQueryStats(query string) map[string]interface{} {
	return s.queryOptimizer.GetQueryStats(query)
}

// Index optimization operations

// RegisterIndex registers an index
func (s *Service) RegisterIndex(indexName, tableName string, columnNames []string, sizeBytes int64) {
	s.indexOptimizer.RegisterIndex(indexName, tableName, columnNames, sizeBytes)
}

// RecordIndexUsage records index usage
func (s *Service) RecordIndexUsage(indexName string) {
	s.indexOptimizer.RecordIndexUsage(indexName)
}

// RecordIndexFragmentation records index fragmentation
func (s *Service) RecordIndexFragmentation(indexName string, fragmentation float64) {
	s.indexOptimizer.RecordIndexFragmentation(indexName, fragmentation)
}

// GetIndexMetrics returns index metrics
func (s *Service) GetIndexMetrics(indexName string) *IndexMetrics {
	return s.indexOptimizer.GetIndexMetrics(indexName)
}

// GetAllIndexMetrics returns all index metrics
func (s *Service) GetAllIndexMetrics() []*IndexMetrics {
	return s.indexOptimizer.GetAllIndexMetrics()
}

// GetUnusedIndexes returns unused indexes
func (s *Service) GetUnusedIndexes() []*IndexMetrics {
	return s.indexOptimizer.GetUnusedIndexes()
}

// GetDuplicateIndexes returns duplicate indexes
func (s *Service) GetDuplicateIndexes() []*IndexMetrics {
	return s.indexOptimizer.GetDuplicateIndexes()
}

// GetFragmentedIndexes returns fragmented indexes
func (s *Service) GetFragmentedIndexes(threshold float64) []*IndexMetrics {
	return s.indexOptimizer.GetFragmentedIndexes(threshold)
}

// Optimization recommendations

// GetOptimizationRecommendations returns all optimization recommendations
func (s *Service) GetOptimizationRecommendations() map[string][]string {
	recommendations := make(map[string][]string)

	recommendations["cache"] = []string{} // Cache recommendations
	recommendations["query"] = s.queryOptimizer.GetOptimizationRecommendations()
	recommendations["index"] = s.indexOptimizer.GetOptimizationRecommendations()
	recommendations["resource"] = s.resourceOptimizer.GetOptimizationRecommendations()

	return recommendations
}

// Resource optimization operations

// GetMemoryMetrics returns memory metrics
func (s *Service) GetMemoryMetrics(limit int) []*MemoryMetrics {
	return s.resourceOptimizer.GetMemoryMetrics(limit)
}

// GetCPUMetrics returns CPU metrics
func (s *Service) GetCPUMetrics(limit int) []*CPUMetrics {
	return s.resourceOptimizer.GetCPUMetrics(limit)
}

// GetMemoryTrends returns memory trends
func (s *Service) GetMemoryTrends() []*MemoryMetrics {
	return s.resourceOptimizer.GetMemoryTrends()
}

// GetCPUTrends returns CPU trends
func (s *Service) GetCPUTrends() []*CPUMetrics {
	return s.resourceOptimizer.GetCPUTrends()
}

// GetMemoryStats returns memory statistics
func (s *Service) GetMemoryStats() map[string]interface{} {
	return s.resourceOptimizer.GetMemoryStats()
}

// GetCPUStats returns CPU statistics
func (s *Service) GetCPUStats() map[string]interface{} {
	return s.resourceOptimizer.GetCPUStats()
}

// ForceGC forces garbage collection
func (s *Service) ForceGC() {
	s.resourceOptimizer.ForceGC()
}

// Close closes the optimization service
func (s *Service) Close() error {
	s.cancel()

	if err := s.cache.Close(); err != nil {
		return err
	}

	if err := s.queryOptimizer.Close(); err != nil {
		return err
	}

	if err := s.indexOptimizer.Close(); err != nil {
		return err
	}

	return nil
}
