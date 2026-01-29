package monitoring

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// MetricsCollector collects system metrics
type MetricsCollector struct {
	logger    *zap.Logger
	mu        sync.RWMutex
	metrics   map[string]*Metric
	startTime time.Time
}

// Metric represents a single metric
type Metric struct {
	Name        string
	Type        string // counter, gauge, histogram, timer
	Value       float64
	Count       int64
	Sum         float64
	Min         float64
	Max         float64
	LastUpdated time.Time
	Tags        map[string]string
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(logger *zap.Logger) *MetricsCollector {
	return &MetricsCollector{
		logger:    logger,
		metrics:   make(map[string]*Metric),
		startTime: time.Now(),
	}
}

// IncrementCounter increments a counter metric
func (mc *MetricsCollector) IncrementCounter(name string, tags map[string]string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	key := mc.getMetricKey(name, tags)
	metric, exists := mc.metrics[key]

	if !exists {
		metric = &Metric{
			Name:  name,
			Type:  "counter",
			Value: 0,
			Count: 0,
			Min:   0,
			Max:   0,
			Tags:  tags,
		}
		mc.metrics[key] = metric
	}

	metric.Value++
	metric.Count++
	metric.LastUpdated = time.Now()
}

// SetGauge sets a gauge metric
func (mc *MetricsCollector) SetGauge(name string, value float64, tags map[string]string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	key := mc.getMetricKey(name, tags)
	metric, exists := mc.metrics[key]

	if !exists {
		metric = &Metric{
			Name:  name,
			Type:  "gauge",
			Value: value,
			Count: 1,
			Min:   value,
			Max:   value,
			Tags:  tags,
		}
		mc.metrics[key] = metric
	} else {
		metric.Value = value
		metric.Count++
		if value < metric.Min {
			metric.Min = value
		}
		if value > metric.Max {
			metric.Max = value
		}
	}

	metric.LastUpdated = time.Now()
}

// RecordHistogram records a histogram metric
func (mc *MetricsCollector) RecordHistogram(name string, value float64, tags map[string]string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	key := mc.getMetricKey(name, tags)
	metric, exists := mc.metrics[key]

	if !exists {
		metric = &Metric{
			Name:  name,
			Type:  "histogram",
			Value: value,
			Count: 1,
			Sum:   value,
			Min:   value,
			Max:   value,
			Tags:  tags,
		}
		mc.metrics[key] = metric
	} else {
		metric.Count++
		metric.Sum += value
		if value < metric.Min {
			metric.Min = value
		}
		if value > metric.Max {
			metric.Max = value
		}
		metric.Value = metric.Sum / float64(metric.Count) // Average
	}

	metric.LastUpdated = time.Now()
}

// RecordTimer records a timer metric
func (mc *MetricsCollector) RecordTimer(name string, duration time.Duration, tags map[string]string) {
	mc.RecordHistogram(name, float64(duration.Milliseconds()), tags)
}

// GetMetric gets a metric by name
func (mc *MetricsCollector) GetMetric(name string) *Metric {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	for _, metric := range mc.metrics {
		if metric.Name == name {
			return metric
		}
	}

	return nil
}

// GetAllMetrics returns all metrics
func (mc *MetricsCollector) GetAllMetrics() map[string]*Metric {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make(map[string]*Metric)
	for key, metric := range mc.metrics {
		result[key] = metric
	}

	return result
}

// GetMetricsSnapshot returns a snapshot of all metrics
func (mc *MetricsCollector) GetMetricsSnapshot() map[string]interface{} {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	snapshot := make(map[string]interface{})
	snapshot["uptime"] = time.Since(mc.startTime).Seconds()
	snapshot["metrics_count"] = len(mc.metrics)

	metrics := make(map[string]interface{})
	for key, metric := range mc.metrics {
		metrics[key] = map[string]interface{}{
			"name":  metric.Name,
			"type":  metric.Type,
			"value": metric.Value,
			"count": metric.Count,
			"sum":   metric.Sum,
			"min":   metric.Min,
			"max":   metric.Max,
		}
	}

	snapshot["metrics"] = metrics
	return snapshot
}

// Reset resets all metrics
func (mc *MetricsCollector) Reset() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.metrics = make(map[string]*Metric)
	mc.startTime = time.Now()
}

// getMetricKey generates a unique key for a metric
func (mc *MetricsCollector) getMetricKey(name string, tags map[string]string) string {
	key := name
	for k, v := range tags {
		key += ":" + k + "=" + v
	}
	return key
}

// ServiceMetrics tracks service-specific metrics
type ServiceMetrics struct {
	ServiceName    string
	RequestCount   int64
	ErrorCount     int64
	SuccessCount   int64
	TotalLatency   int64
	AverageLatency float64
	MinLatency     int64
	MaxLatency     int64
	LastUpdated    time.Time
}

// ServiceMetricsTracker tracks metrics for multiple services
type ServiceMetricsTracker struct {
	logger   *zap.Logger
	mu       sync.RWMutex
	services map[string]*ServiceMetrics
}

// NewServiceMetricsTracker creates a new service metrics tracker
func NewServiceMetricsTracker(logger *zap.Logger) *ServiceMetricsTracker {
	return &ServiceMetricsTracker{
		logger:   logger,
		services: make(map[string]*ServiceMetrics),
	}
}

// RecordRequest records a service request
func (smt *ServiceMetricsTracker) RecordRequest(serviceName string, latency int64, success bool) {
	smt.mu.Lock()
	defer smt.mu.Unlock()

	metrics, exists := smt.services[serviceName]
	if !exists {
		metrics = &ServiceMetrics{
			ServiceName: serviceName,
			MinLatency:  latency,
			MaxLatency:  latency,
		}
		smt.services[serviceName] = metrics
	}

	metrics.RequestCount++
	metrics.TotalLatency += latency

	if success {
		metrics.SuccessCount++
	} else {
		metrics.ErrorCount++
	}

	if latency < metrics.MinLatency {
		metrics.MinLatency = latency
	}
	if latency > metrics.MaxLatency {
		metrics.MaxLatency = latency
	}

	metrics.AverageLatency = float64(metrics.TotalLatency) / float64(metrics.RequestCount)
	metrics.LastUpdated = time.Now()
}

// GetServiceMetrics gets metrics for a service
func (smt *ServiceMetricsTracker) GetServiceMetrics(serviceName string) *ServiceMetrics {
	smt.mu.RLock()
	defer smt.mu.RUnlock()

	return smt.services[serviceName]
}

// GetAllServiceMetrics returns metrics for all services
func (smt *ServiceMetricsTracker) GetAllServiceMetrics() map[string]*ServiceMetrics {
	smt.mu.RLock()
	defer smt.mu.RUnlock()

	result := make(map[string]*ServiceMetrics)
	for name, metrics := range smt.services {
		result[name] = metrics
	}

	return result
}

// GetErrorRate gets the error rate for a service
func (smt *ServiceMetricsTracker) GetErrorRate(serviceName string) float64 {
	smt.mu.RLock()
	defer smt.mu.RUnlock()

	metrics, exists := smt.services[serviceName]
	if !exists || metrics.RequestCount == 0 {
		return 0
	}

	return float64(metrics.ErrorCount) / float64(metrics.RequestCount)
}

// GetSuccessRate gets the success rate for a service
func (smt *ServiceMetricsTracker) GetSuccessRate(serviceName string) float64 {
	return 1 - smt.GetErrorRate(serviceName)
}
