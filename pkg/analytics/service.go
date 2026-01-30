package analytics

import (
	"context"
	"sync"
	"time"
)

// Service provides analytics functionality
type Service struct {
	mu              sync.RWMutex
	collector       *EventCollector
	aggregator      *Aggregator
	anomalyDetector *AnomalyDetector
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewService creates a new analytics service
func NewService() *Service {
	ctx, cancel := context.WithCancel(context.Background())

	service := &Service{
		collector:       NewEventCollector(1000, 5*time.Second),
		aggregator:      NewAggregator(),
		anomalyDetector: NewAnomalyDetector(2.0), // 2 standard deviations
		ctx:             ctx,
		cancel:          cancel,
	}

	// Subscribe collector to aggregator
	service.collector.Subscribe("event", func(event interface{}) error {
		if e, ok := event.(*AnalyticsEvent); ok {
			service.aggregator.AddEvent(e)
		}
		return nil
	})

	service.collector.Subscribe("metrics", func(event interface{}) error {
		if m, ok := event.(*MetricsSnapshot); ok {
			service.aggregator.AddMetrics(m)
			service.anomalyDetector.RecordMetric(m)
		}
		return nil
	})

	service.collector.Subscribe("behavior", func(event interface{}) error {
		if b, ok := event.(*UserBehavior); ok {
			service.aggregator.AddBehavior(b)
		}
		return nil
	})

	service.collector.Subscribe("performance", func(event interface{}) error {
		if p, ok := event.(*PerformanceMetric); ok {
			service.aggregator.AddPerformanceMetric(p)
		}
		return nil
	})

	service.collector.Subscribe("business", func(event interface{}) error {
		if b, ok := event.(*BusinessMetric); ok {
			service.aggregator.AddBusinessMetric(b)
		}
		return nil
	})

	return service
}

// RecordEvent records an analytics event
func (s *Service) RecordEvent(eventType, serviceID, userID string, metadata map[string]interface{}, tags map[string]string) {
	s.collector.RecordEvent(eventType, serviceID, userID, metadata, tags)
}

// RecordMetrics records system metrics
func (s *Service) RecordMetrics(serviceID string, cpu, memory, disk, requestRate, errorRate, latency, cacheHitRate float64) {
	s.collector.RecordMetrics(serviceID, cpu, memory, disk, requestRate, errorRate, latency, cacheHitRate)
}

// RecordUserBehavior records user behavior
func (s *Service) RecordUserBehavior(userID, action, contentID, clientIP, userAgent, sessionID string, duration int64, success bool, errorMsg string) {
	s.collector.RecordUserBehavior(userID, action, contentID, clientIP, userAgent, sessionID, duration, success, errorMsg)
}

// RecordPerformanceMetric records performance metrics
func (s *Service) RecordPerformanceMetric(serviceID, operation string, duration, resourceUsed, throughput float64, success bool, errorType string) {
	s.collector.RecordPerformanceMetric(serviceID, operation, duration, resourceUsed, throughput, success, errorType)
}

// RecordBusinessMetric records business metrics
func (s *Service) RecordBusinessMetric(metricType string, value float64, unit string, dimension map[string]string) {
	s.collector.RecordBusinessMetric(metricType, value, unit, dimension)
}

// GetAggregations returns aggregations for a service
func (s *Service) GetAggregations(serviceID string) []*AnalyticsAggregation {
	return s.aggregator.GetAggregations(serviceID)
}

// GetLatestAggregation returns the latest aggregation for a service
func (s *Service) GetLatestAggregation(serviceID, period string) *AnalyticsAggregation {
	return s.aggregator.GetLatestAggregation(serviceID, period)
}

// GetAnomalies returns recent anomalies for a service
func (s *Service) GetAnomalies(serviceID string, limit int) []*AnomalyDetection {
	return s.anomalyDetector.GetAnomalies(serviceID, limit)
}

// GetAllAnomalies returns all recent anomalies
func (s *Service) GetAllAnomalies(limit int) []*AnomalyDetection {
	return s.anomalyDetector.GetAllAnomalies(limit)
}

// GetDashboardData returns data for dashboard visualization
func (s *Service) GetDashboardData(serviceID string) *DashboardData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data := &DashboardData{
		Timestamp:      time.Now(),
		ServiceMetrics: make(map[string]*MetricsSnapshot),
		Aggregations:   s.aggregator.GetAggregations(serviceID),
		Anomalies:      s.anomalyDetector.GetAnomalies(serviceID, 10),
		TopErrors:      []string{},
		TopUsers:       []string{},
		SystemHealth:   s.calculateSystemHealth(serviceID),
	}

	return data
}

// FlushNow flushes the collector buffer immediately
func (s *Service) FlushNow() {
	s.collector.FlushNow()
}

// AggregateNow triggers immediate aggregation
func (s *Service) AggregateNow() {
	s.aggregator.AggregateNow()
}

// DetectAnomaliesNow triggers immediate anomaly detection
func (s *Service) DetectAnomaliesNow() {
	s.anomalyDetector.DetectAnomaliesNow()
}

// MakePredictionsNow triggers immediate predictions
func (s *Service) MakePredictionsNow() {
}

// calculateSystemHealth calculates the overall system health
func (s *Service) calculateSystemHealth(serviceID string) string {
	anomalies := s.anomalyDetector.GetAnomalies(serviceID, 100)

	criticalCount := 0
	highCount := 0

	for _, anomaly := range anomalies {
		if anomaly.Severity == "critical" {
			criticalCount++
		} else if anomaly.Severity == "high" {
			highCount++
		}
	}

	if criticalCount > 0 {
		return "critical"
	} else if highCount > 2 {
		return "degraded"
	}

	return "healthy"
}

// Close closes the analytics service
func (s *Service) Close() error {
	s.cancel()

	if err := s.collector.Close(); err != nil {
		return err
	}

	if err := s.aggregator.Close(); err != nil {
		return err
	}

	if err := s.anomalyDetector.Close(); err != nil {
		return err
	}

	return nil
}
