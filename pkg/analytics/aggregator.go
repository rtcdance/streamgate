package analytics

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Aggregator aggregates analytics data into time-based buckets
type Aggregator struct {
	mu              sync.RWMutex
	aggregations    map[string]*AnalyticsAggregation
	events          []*AnalyticsEvent
	metrics         []*MetricsSnapshot
	behaviors       []*UserBehavior
	perfMetrics     []*PerformanceMetric
	businessMetrics []*BusinessMetric
	periods         []string // 1m, 5m, 15m, 1h, 1d
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

// NewAggregator creates a new aggregator
func NewAggregator() *Aggregator {
	ctx, cancel := context.WithCancel(context.Background())

	agg := &Aggregator{
		aggregations:    make(map[string]*AnalyticsAggregation),
		events:          make([]*AnalyticsEvent, 0),
		metrics:         make([]*MetricsSnapshot, 0),
		behaviors:       make([]*UserBehavior, 0),
		perfMetrics:     make([]*PerformanceMetric, 0),
		businessMetrics: make([]*BusinessMetric, 0),
		periods:         []string{"1m", "5m", "15m", "1h", "1d"},
		ctx:             ctx,
		cancel:          cancel,
	}

	agg.start()
	return agg
}

// start begins the aggregation process
func (agg *Aggregator) start() {
	agg.wg.Add(1)
	go agg.aggregationLoop()
}

// aggregationLoop periodically aggregates data
func (agg *Aggregator) aggregationLoop() {
	defer agg.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-agg.ctx.Done():
			return
		case <-ticker.C:
			agg.aggregate()
		}
	}
}

// AddEvent adds an event for aggregation
func (agg *Aggregator) AddEvent(event *AnalyticsEvent) {
	agg.mu.Lock()
	defer agg.mu.Unlock()

	agg.events = append(agg.events, event)
}

// AddMetrics adds metrics for aggregation
func (agg *Aggregator) AddMetrics(metric *MetricsSnapshot) {
	agg.mu.Lock()
	defer agg.mu.Unlock()

	agg.metrics = append(agg.metrics, metric)
}

// AddBehavior adds behavior for aggregation
func (agg *Aggregator) AddBehavior(behavior *UserBehavior) {
	agg.mu.Lock()
	defer agg.mu.Unlock()

	agg.behaviors = append(agg.behaviors, behavior)
}

// AddPerformanceMetric adds performance metric for aggregation
func (agg *Aggregator) AddPerformanceMetric(perfMetric *PerformanceMetric) {
	agg.mu.Lock()
	defer agg.mu.Unlock()

	agg.perfMetrics = append(agg.perfMetrics, perfMetric)
}

// AddBusinessMetric adds business metric for aggregation
func (agg *Aggregator) AddBusinessMetric(businessMetric *BusinessMetric) {
	agg.mu.Lock()
	defer agg.mu.Unlock()

	agg.businessMetrics = append(agg.businessMetrics, businessMetric)
}

// aggregate performs aggregation
func (agg *Aggregator) aggregate() {
	agg.mu.Lock()
	defer agg.mu.Unlock()

	now := time.Now()

	// Aggregate performance metrics
	for _, serviceID := range agg.getUniqueServiceIDs() {
		for _, period := range agg.periods {
			agg.aggregateServiceMetrics(serviceID, period, now)
		}
	}

	// Clean up old data
	agg.cleanupOldData(now)
}

// aggregateServiceMetrics aggregates metrics for a service in a period
func (agg *Aggregator) aggregateServiceMetrics(serviceID, period string, now time.Time) {
	cutoff := agg.getCutoffTime(now, period)

	var eventCount int64
	var errorCount int64
	var latencies []float64
	var successCount int64

	// Filter events for this service and period
	for _, event := range agg.events {
		if event.ServiceID == serviceID && event.Timestamp.After(cutoff) {
			eventCount++
			if eventType, ok := event.Metadata["error"]; ok && eventType.(bool) {
				errorCount++
			} else {
				successCount++
			}
		}
	}

	// Filter performance metrics for this service and period
	for _, perfMetric := range agg.perfMetrics {
		if perfMetric.ServiceID == serviceID && perfMetric.Timestamp.After(cutoff) {
			latencies = append(latencies, perfMetric.Duration)
			if perfMetric.Success {
				successCount++
			} else {
				errorCount++
			}
		}
	}

	if eventCount == 0 && len(latencies) == 0 {
		return
	}

	// Calculate percentiles
	sort.Float64s(latencies)
	p50 := agg.percentile(latencies, 50)
	p95 := agg.percentile(latencies, 95)
	p99 := agg.percentile(latencies, 99)
	avgLatency := agg.average(latencies)

	errorRate := 0.0
	successRate := 1.0
	if eventCount > 0 {
		errorRate = float64(errorCount) / float64(eventCount)
		successRate = float64(successCount) / float64(eventCount)
	}

	key := serviceID + ":" + period
	agg.aggregations[key] = &AnalyticsAggregation{
		ID:          uuid.New().String(),
		Timestamp:   now,
		ServiceID:   serviceID,
		Period:      period,
		EventCount:  eventCount,
		AvgLatency:  avgLatency,
		P50Latency:  p50,
		P95Latency:  p95,
		P99Latency:  p99,
		ErrorCount:  errorCount,
		ErrorRate:   errorRate,
		SuccessRate: successRate,
		Throughput:  float64(eventCount) / agg.getPeriodSeconds(period),
	}
}

// GetAggregations returns aggregations for a service
func (agg *Aggregator) GetAggregations(serviceID string) []*AnalyticsAggregation {
	agg.mu.RLock()
	defer agg.mu.RUnlock()

	var result []*AnalyticsAggregation
	for _, period := range agg.periods {
		key := serviceID + ":" + period
		if agg, ok := agg.aggregations[key]; ok {
			result = append(result, agg)
		}
	}

	return result
}

// GetLatestAggregation returns the latest aggregation for a service
func (agg *Aggregator) GetLatestAggregation(serviceID, period string) *AnalyticsAggregation {
	agg.mu.RLock()
	defer agg.mu.RUnlock()

	key := serviceID + ":" + period
	return agg.aggregations[key]
}

// getUniqueServiceIDs returns unique service IDs
func (agg *Aggregator) getUniqueServiceIDs() []string {
	serviceMap := make(map[string]bool)

	for _, event := range agg.events {
		serviceMap[event.ServiceID] = true
	}

	for _, metric := range agg.metrics {
		serviceMap[metric.ServiceID] = true
	}

	for _, perfMetric := range agg.perfMetrics {
		serviceMap[perfMetric.ServiceID] = true
	}

	var services []string
	for service := range serviceMap {
		services = append(services, service)
	}

	return services
}

// getCutoffTime returns the cutoff time for a period
func (agg *Aggregator) getCutoffTime(now time.Time, period string) time.Time {
	switch period {
	case "1m":
		return now.Add(-1 * time.Minute)
	case "5m":
		return now.Add(-5 * time.Minute)
	case "15m":
		return now.Add(-15 * time.Minute)
	case "1h":
		return now.Add(-1 * time.Hour)
	case "1d":
		return now.Add(-24 * time.Hour)
	default:
		return now.Add(-1 * time.Minute)
	}
}

// getPeriodSeconds returns the number of seconds in a period
func (agg *Aggregator) getPeriodSeconds(period string) float64 {
	switch period {
	case "1m":
		return 60
	case "5m":
		return 300
	case "15m":
		return 900
	case "1h":
		return 3600
	case "1d":
		return 86400
	default:
		return 60
	}
}

// percentile calculates the percentile of a slice
func (agg *Aggregator) percentile(data []float64, p float64) float64 {
	if len(data) == 0 {
		return 0
	}

	index := int(float64(len(data)) * p / 100)
	if index >= len(data) {
		index = len(data) - 1
	}

	return data[index]
}

// average calculates the average of a slice
func (agg *Aggregator) average(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range data {
		sum += v
	}

	return sum / float64(len(data))
}

// cleanupOldData removes old data
func (agg *Aggregator) cleanupOldData(now time.Time) {
	cutoff := now.Add(-24 * time.Hour)

	// Clean up events
	newEvents := make([]*AnalyticsEvent, 0)
	for _, event := range agg.events {
		if event.Timestamp.After(cutoff) {
			newEvents = append(newEvents, event)
		}
	}
	agg.events = newEvents

	// Clean up metrics
	newMetrics := make([]*MetricsSnapshot, 0)
	for _, metric := range agg.metrics {
		if metric.Timestamp.After(cutoff) {
			newMetrics = append(newMetrics, metric)
		}
	}
	agg.metrics = newMetrics

	// Clean up behaviors
	newBehaviors := make([]*UserBehavior, 0)
	for _, behavior := range agg.behaviors {
		if behavior.Timestamp.After(cutoff) {
			newBehaviors = append(newBehaviors, behavior)
		}
	}
	agg.behaviors = newBehaviors

	// Clean up performance metrics
	newPerfMetrics := make([]*PerformanceMetric, 0)
	for _, perfMetric := range agg.perfMetrics {
		if perfMetric.Timestamp.After(cutoff) {
			newPerfMetrics = append(newPerfMetrics, perfMetric)
		}
	}
	agg.perfMetrics = newPerfMetrics

	// Clean up business metrics
	newBusinessMetrics := make([]*BusinessMetric, 0)
	for _, businessMetric := range agg.businessMetrics {
		if businessMetric.Timestamp.After(cutoff) {
			newBusinessMetrics = append(newBusinessMetrics, businessMetric)
		}
	}
	agg.businessMetrics = newBusinessMetrics
}

// Close closes the aggregator
func (agg *Aggregator) Close() error {
	agg.cancel()
	agg.wg.Wait()
	return nil
}
