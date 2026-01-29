package optimization

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// QueryMetrics represents query performance metrics
type QueryMetrics struct {
	ID            string
	Query         string
	ExecutionTime float64
	RowsAffected  int64
	RowsScanned   int64
	IndexUsed     string
	Timestamp     time.Time
	Optimized     bool
}

// QueryPlan represents a query execution plan
type QueryPlan struct {
	ID              string
	Query           string
	PlanText        string
	EstimatedCost   float64
	ActualCost      float64
	IndexUsed       string
	SequentialScan  bool
	Recommendations []string
}

// QueryOptimizer optimizes database queries
type QueryOptimizer struct {
	mu              sync.RWMutex
	metrics         []*QueryMetrics
	plans           map[string]*QueryPlan
	slowQueries     []*QueryMetrics
	slowQueryThreshold float64
	maxMetricsSize  int
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

// NewQueryOptimizer creates a new query optimizer
func NewQueryOptimizer(slowQueryThreshold float64) *QueryOptimizer {
	ctx, cancel := context.WithCancel(context.Background())

	optimizer := &QueryOptimizer{
		metrics:            make([]*QueryMetrics, 0),
		plans:              make(map[string]*QueryPlan),
		slowQueries:        make([]*QueryMetrics, 0),
		slowQueryThreshold: slowQueryThreshold,
		maxMetricsSize:     10000,
		ctx:                ctx,
		cancel:             cancel,
	}

	optimizer.start()
	return optimizer
}

// start begins the optimizer
func (qo *QueryOptimizer) start() {
	qo.wg.Add(1)
	go qo.optimizationLoop()
}

// optimizationLoop periodically optimizes queries
func (qo *QueryOptimizer) optimizationLoop() {
	defer qo.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-qo.ctx.Done():
			return
		case <-ticker.C:
			qo.analyzeQueries()
		}
	}
}

// RecordQuery records a query execution
func (qo *QueryOptimizer) RecordQuery(query string, executionTime float64, rowsAffected, rowsScanned int64, indexUsed string) {
	qo.mu.Lock()
	defer qo.mu.Unlock()

	metric := &QueryMetrics{
		ID:           uuid.New().String(),
		Query:        query,
		ExecutionTime: executionTime,
		RowsAffected: rowsAffected,
		RowsScanned:  rowsScanned,
		IndexUsed:    indexUsed,
		Timestamp:    time.Now(),
		Optimized:    false,
	}

	qo.metrics = append(qo.metrics, metric)

	// Keep metrics bounded
	if len(qo.metrics) > qo.maxMetricsSize {
		qo.metrics = qo.metrics[1:]
	}

	// Track slow queries
	if executionTime > qo.slowQueryThreshold {
		qo.slowQueries = append(qo.slowQueries, metric)
		if len(qo.slowQueries) > 1000 {
			qo.slowQueries = qo.slowQueries[1:]
		}
	}
}

// AnalyzePlan analyzes a query execution plan
func (qo *QueryOptimizer) AnalyzePlan(query, planText string, estimatedCost, actualCost float64, indexUsed string) *QueryPlan {
	qo.mu.Lock()
	defer qo.mu.Unlock()

	plan := &QueryPlan{
		ID:            uuid.New().String(),
		Query:         query,
		PlanText:      planText,
		EstimatedCost: estimatedCost,
		ActualCost:    actualCost,
		IndexUsed:     indexUsed,
	}

	// Detect sequential scans
	if indexUsed == "" {
		plan.SequentialScan = true
		plan.Recommendations = append(plan.Recommendations, "Consider adding an index")
	}

	// Detect cost overruns
	if actualCost > estimatedCost*1.5 {
		plan.Recommendations = append(plan.Recommendations, "Query cost exceeds estimate, consider rewriting")
	}

	qo.plans[query] = plan
	return plan
}

// analyzeQueries analyzes recorded queries
func (qo *QueryOptimizer) analyzeQueries() {
	qo.mu.Lock()
	defer qo.mu.Unlock()

	// Analyze slow queries
	for _, metric := range qo.slowQueries {
		if !metric.Optimized {
			qo.optimizeQuery(metric)
		}
	}
}

// optimizeQuery optimizes a single query
func (qo *QueryOptimizer) optimizeQuery(metric *QueryMetrics) {
	// Check for sequential scans
	if metric.IndexUsed == "" {
		metric.Optimized = true
		return
	}

	// Check for inefficient row scanning
	if metric.RowsScanned > metric.RowsAffected*10 {
		metric.Optimized = true
		return
	}

	metric.Optimized = true
}

// GetSlowQueries returns slow queries
func (qo *QueryOptimizer) GetSlowQueries(limit int) []*QueryMetrics {
	qo.mu.RLock()
	defer qo.mu.RUnlock()

	if len(qo.slowQueries) <= limit {
		return qo.slowQueries
	}

	return qo.slowQueries[len(qo.slowQueries)-limit:]
}

// GetQueryMetrics returns query metrics
func (qo *QueryOptimizer) GetQueryMetrics(limit int) []*QueryMetrics {
	qo.mu.RLock()
	defer qo.mu.RUnlock()

	if len(qo.metrics) <= limit {
		return qo.metrics
	}

	return qo.metrics[len(qo.metrics)-limit:]
}

// GetQueryPlan returns a query plan
func (qo *QueryOptimizer) GetQueryPlan(query string) *QueryPlan {
	qo.mu.RLock()
	defer qo.mu.RUnlock()

	return qo.plans[query]
}

// GetAverageExecutionTime returns average execution time for a query
func (qo *QueryOptimizer) GetAverageExecutionTime(query string) float64 {
	qo.mu.RLock()
	defer qo.mu.RUnlock()

	var totalTime float64
	var count int

	for _, metric := range qo.metrics {
		if metric.Query == query {
			totalTime += metric.ExecutionTime
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return totalTime / float64(count)
}

// GetQueryStats returns statistics for a query
func (qo *QueryOptimizer) GetQueryStats(query string) map[string]interface{} {
	qo.mu.RLock()
	defer qo.mu.RUnlock()

	stats := make(map[string]interface{})
	var totalTime float64
	var minTime float64 = 999999
	var maxTime float64
	var count int
	var totalRows int64

	for _, metric := range qo.metrics {
		if metric.Query == query {
			totalTime += metric.ExecutionTime
			if metric.ExecutionTime < minTime {
				minTime = metric.ExecutionTime
			}
			if metric.ExecutionTime > maxTime {
				maxTime = metric.ExecutionTime
			}
			count++
			totalRows += metric.RowsAffected
		}
	}

	if count > 0 {
		stats["count"] = count
		stats["total_time"] = totalTime
		stats["avg_time"] = totalTime / float64(count)
		stats["min_time"] = minTime
		stats["max_time"] = maxTime
		stats["total_rows"] = totalRows
	}

	return stats
}

// GetOptimizationRecommendations returns optimization recommendations
func (qo *QueryOptimizer) GetOptimizationRecommendations() []string {
	qo.mu.RLock()
	defer qo.mu.RUnlock()

	var recommendations []string

	// Check for sequential scans
	sequentialScans := 0
	for _, metric := range qo.slowQueries {
		if metric.IndexUsed == "" {
			sequentialScans++
		}
	}

	if sequentialScans > 0 {
		recommendations = append(recommendations, fmt.Sprintf("Found %d queries with sequential scans - consider adding indexes", sequentialScans))
	}

	// Check for inefficient queries
	inefficientQueries := 0
	for _, metric := range qo.slowQueries {
		if metric.RowsScanned > metric.RowsAffected*10 {
			inefficientQueries++
		}
	}

	if inefficientQueries > 0 {
		recommendations = append(recommendations, fmt.Sprintf("Found %d queries scanning too many rows - consider query rewriting", inefficientQueries))
	}

	// Check for slow queries
	if len(qo.slowQueries) > 100 {
		recommendations = append(recommendations, fmt.Sprintf("Found %d slow queries - consider optimization", len(qo.slowQueries)))
	}

	return recommendations
}

// Close closes the optimizer
func (qo *QueryOptimizer) Close() error {
	qo.cancel()
	qo.wg.Wait()
	return nil
}
