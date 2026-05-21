package monitoring

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// Prometheus bridge metrics — all plugin-level IncrementCounter/SetGauge/RecordHistogram
// calls are forwarded here so promhttp.Handler() on /metrics is the single source of truth.
var (
	pluginOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "streamgate_plugin_operations_total",
			Help: "Total plugin-level operations tracked by MetricsCollector",
		},
		[]string{"metric"},
	)
	pluginGaugeValue = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "streamgate_plugin_gauge",
			Help: "Current gauge value tracked by MetricsCollector",
		},
		[]string{"metric"},
	)
	pluginHistogramSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "streamgate_plugin_duration_seconds",
			Help:    "Histogram of plugin-level durations tracked by MetricsCollector",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"metric"},
	)
	serviceRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "streamgate_service_requests_total",
			Help: "Total per-service requests tracked by ServiceMetricsTracker",
		},
		[]string{"service", "status"},
	)
	serviceLatencyMs = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "streamgate_service_latency_ms",
			Help:    "Per-service request latency in milliseconds",
			Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000},
		},
		[]string{"service"},
	)
	StreamingViewersActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "streamgate_streaming_viewers_active",
		Help: "Current number of active streaming sessions",
	})
	StreamingSegmentsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "streamgate_streaming_segments_total",
			Help: "Total number of segment requests served, by quality",
		},
		[]string{"quality"},
	)
	StreamingManifestsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "streamgate_streaming_manifests_total",
		Help: "Total number of manifest requests served",
	})
	StreamingCacheHitsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "streamgate_streaming_cache_hits_total",
			Help: "Total number of cache hits, by cache layer (manifest, segment_index)",
		},
		[]string{"cache"},
	)
	StreamingDownloadDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "streamgate_streaming_download_seconds",
			Help:    "Segment download duration from object storage in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"status"},
	)
	TranscodingQueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "streamgate_transcoding_queue_depth",
		Help: "Current number of pending transcoding tasks in the queue",
	})
	TranscodingWorkersActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "streamgate_transcoding_workers_active",
		Help: "Current number of active transcoding worker goroutines",
	})
	AuthOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "streamgate_auth_operations_total",
			Help: "Total auth operations by type and status",
		},
		[]string{"operation", "status"},
	)
	EventIndexerEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "streamgate_event_indexer_events_total",
			Help: "Total events indexed by contract and event type",
		},
		[]string{"contract", "event_type", "status"},
	)
	EventIndexerReorgsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "streamgate_event_indexer_reorgs_total",
			Help: "Total reorgs detected by event indexer",
		},
	)
	EventIndexerCurrentBlock = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "streamgate_event_indexer_current_block",
			Help: "Current block being indexed",
		},
	)
	EventIndexerIndexDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "streamgate_event_indexer_index_duration_seconds",
			Help:    "Duration of event indexing operations",
			Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60},
		},
		[]string{"mode"},
	)
)

func init() {
	for _, c := range []prometheus.Collector{
		pluginOperationsTotal,
		pluginGaugeValue,
		pluginHistogramSeconds,
		serviceRequestsTotal,
		serviceLatencyMs,
		StreamingViewersActive,
		StreamingSegmentsTotal,
		StreamingManifestsTotal,
		StreamingCacheHitsTotal,
		StreamingDownloadDuration,
		TranscodingQueueDepth,
		TranscodingWorkersActive,
		AuthOperationsTotal,
		EventIndexerEventsTotal,
		EventIndexerReorgsTotal,
		EventIndexerCurrentBlock,
		EventIndexerIndexDuration,
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	} {
		if err := prometheus.Register(c); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				panic(err)
			}
		}
	}
}

// MetricsCollector collects system metrics.
// All operations are bridged to the Prometheus default registry so that
// promhttp.Handler() serves the authoritative metrics.
// The in-memory map is kept for GetMetric/GetAllMetrics/GetMetricsSnapshot
// backward compatibility.
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

// IncrementCounter increments a counter metric and bridges to Prometheus.
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

	// Bridge to Prometheus
	pluginOperationsTotal.WithLabelValues(name).Inc()
}

// SetGauge sets a gauge metric and bridges to Prometheus.
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

	// Bridge to Prometheus
	pluginGaugeValue.WithLabelValues(name).Set(value)
}

// RecordHistogram records a histogram metric and bridges to Prometheus.
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

	// Bridge to Prometheus — value is in ms from callers, convert to seconds
	pluginHistogramSeconds.WithLabelValues(name).Observe(value / 1000.0)
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
	if len(tags) == 0 {
		return name
	}
	// Deterministic key generation: sort tag keys
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	key := name
	for _, k := range keys {
		key += fmt.Sprintf(":%s=%s", k, tags[k])
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

// ServiceMetricsTracker tracks metrics for multiple services.
// RecordRequest is bridged to Prometheus so promhttp.Handler() is authoritative.
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

// RecordRequest records a service request and bridges to Prometheus.
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

	// Bridge to Prometheus
	status := "success"
	if !success {
		status = "error"
	}
	serviceRequestsTotal.WithLabelValues(serviceName, status).Inc()
	serviceLatencyMs.WithLabelValues(serviceName).Observe(float64(latency))
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
