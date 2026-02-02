package metrics

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// MetricsCollector collects and exposes Prometheus metrics
type MetricsCollector struct {
	registry *prometheus.Registry
	logger   *zap.Logger
	mu       sync.RWMutex
	metrics  map[string]Metric
}

// Metric represents a metric
type Metric interface {
	Name() string
	Type() MetricType
	Labels() []string
	Value() interface{}
}

// MetricType represents the type of a metric
type MetricType string

const (
	MetricTypeCounter   MetricType = "counter"
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeHistogram MetricType = "histogram"
	MetricTypeSummary   MetricType = "summary"
)

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(logger *zap.Logger) *MetricsCollector {
	return &MetricsCollector{
		registry: prometheus.NewRegistry(),
		logger:   logger,
		metrics:  make(map[string]Metric),
	}
}

// Counter represents a counter metric
type Counter struct {
	name   string
	labels []string
	vec    *prometheus.CounterVec
}

// NewCounter creates a new counter metric
func (mc *MetricsCollector) NewCounter(name, help string, labels []string) (*Counter, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if _, exists := mc.metrics[name]; exists {
		return nil, fmt.Errorf("metric already exists: %s", name)
	}

	vec := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: name,
			Help: help,
		},
		labels,
	)

	if err := mc.registry.Register(vec); err != nil {
		return nil, fmt.Errorf("failed to register counter: %w", err)
	}

	counter := &Counter{
		name:   name,
		labels: labels,
		vec:    vec,
	}

	mc.metrics[name] = counter
	mc.logger.Debug("Counter registered",
		zap.String("name", name),
		zap.Strings("labels", labels))

	return counter, nil
}

// Name returns the metric name
func (c *Counter) Name() string {
	return c.name
}

// Type returns the metric type
func (c *Counter) Type() MetricType {
	return MetricTypeCounter
}

// Labels returns the metric labels
func (c *Counter) Labels() []string {
	return c.labels
}

// Value returns the metric value
func (c *Counter) Value() interface{} {
	return nil
}

// Increment increments the counter
func (c *Counter) Increment(labelValues ...string) {
	c.vec.WithLabelValues(labelValues...).Inc()
}

// Add adds a value to the counter
func (c *Counter) Add(value float64, labelValues ...string) {
	c.vec.WithLabelValues(labelValues...).Add(value)
}

// Gauge represents a gauge metric
type Gauge struct {
	name   string
	labels []string
	vec    *prometheus.GaugeVec
}

// NewGauge creates a new gauge metric
func (mc *MetricsCollector) NewGauge(name, help string, labels []string) (*Gauge, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if _, exists := mc.metrics[name]; exists {
		return nil, fmt.Errorf("metric already exists: %s", name)
	}

	vec := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: name,
			Help: help,
		},
		labels,
	)

	if err := mc.registry.Register(vec); err != nil {
		return nil, fmt.Errorf("failed to register gauge: %w", err)
	}

	gauge := &Gauge{
		name:   name,
		labels: labels,
		vec:    vec,
	}

	mc.metrics[name] = gauge
	mc.logger.Debug("Gauge registered",
		zap.String("name", name),
		zap.Strings("labels", labels))

	return gauge, nil
}

// Name returns the metric name
func (g *Gauge) Name() string {
	return g.name
}

// Type returns the metric type
func (g *Gauge) Type() MetricType {
	return MetricTypeGauge
}

// Labels returns the metric labels
func (g *Gauge) Labels() []string {
	return g.labels
}

// Value returns the metric value
func (g *Gauge) Value() interface{} {
	return nil
}

// Set sets the gauge value
func (g *Gauge) Set(value float64, labelValues ...string) {
	g.vec.WithLabelValues(labelValues...).Set(value)
}

// Increment increments the gauge
func (g *Gauge) Increment(labelValues ...string) {
	g.vec.WithLabelValues(labelValues...).Inc()
}

// Decrement decrements the gauge
func (g *Gauge) Decrement(labelValues ...string) {
	g.vec.WithLabelValues(labelValues...).Dec()
}

// Add adds a value to the gauge
func (g *Gauge) Add(value float64, labelValues ...string) {
	g.vec.WithLabelValues(labelValues...).Add(value)
}

// Histogram represents a histogram metric
type Histogram struct {
	name    string
	labels  []string
	buckets []float64
	vec     *prometheus.HistogramVec
}

// NewHistogram creates a new histogram metric
func (mc *MetricsCollector) NewHistogram(name, help string, labels []string, buckets []float64) (*Histogram, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if _, exists := mc.metrics[name]; exists {
		return nil, fmt.Errorf("metric already exists: %s", name)
	}

	if len(buckets) == 0 {
		buckets = prometheus.DefBuckets
	}

	vec := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    name,
			Help:    help,
			Buckets: buckets,
		},
		labels,
	)

	if err := mc.registry.Register(vec); err != nil {
		return nil, fmt.Errorf("failed to register histogram: %w", err)
	}

	histogram := &Histogram{
		name:    name,
		labels:  labels,
		buckets: buckets,
		vec:     vec,
	}

	mc.metrics[name] = histogram
	mc.logger.Debug("Histogram registered",
		zap.String("name", name),
		zap.Strings("labels", labels))

	return histogram, nil
}

// Name returns the metric name
func (h *Histogram) Name() string {
	return h.name
}

// Type returns the metric type
func (h *Histogram) Type() MetricType {
	return MetricTypeHistogram
}

// Labels returns the metric labels
func (h *Histogram) Labels() []string {
	return h.labels
}

// Value returns the metric value
func (h *Histogram) Value() interface{} {
	return nil
}

// Observe observes a value
func (h *Histogram) Observe(value float64, labelValues ...string) {
	h.vec.WithLabelValues(labelValues...).Observe(value)
}

// Summary represents a summary metric
type Summary struct {
	name       string
	labels     []string
	objectives map[float64]float64
	vec        *prometheus.SummaryVec
}

// NewSummary creates a new summary metric
func (mc *MetricsCollector) NewSummary(name, help string, labels []string, objectives map[float64]float64) (*Summary, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if _, exists := mc.metrics[name]; exists {
		return nil, fmt.Errorf("metric already exists: %s", name)
	}

	if len(objectives) == 0 {
		objectives = map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001}
	}

	vec := prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       name,
			Help:       help,
			Objectives: objectives,
		},
		labels,
	)

	if err := mc.registry.Register(vec); err != nil {
		return nil, fmt.Errorf("failed to register summary: %w", err)
	}

	summary := &Summary{
		name:       name,
		labels:     labels,
		objectives: objectives,
		vec:        vec,
	}

	mc.metrics[name] = summary
	mc.logger.Debug("Summary registered",
		zap.String("name", name),
		zap.Strings("labels", labels))

	return summary, nil
}

// Name returns the metric name
func (s *Summary) Name() string {
	return s.name
}

// Type returns the metric type
func (s *Summary) Type() MetricType {
	return MetricTypeSummary
}

// Labels returns the metric labels
func (s *Summary) Labels() []string {
	return s.labels
}

// Value returns the metric value
func (s *Summary) Value() interface{} {
	return nil
}

// Observe observes a value
func (s *Summary) Observe(value float64, labelValues ...string) {
	s.vec.WithLabelValues(labelValues...).Observe(value)
}

// GetMetric retrieves a metric by name
func (mc *MetricsCollector) GetMetric(name string) (Metric, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	metric, exists := mc.metrics[name]
	if !exists {
		return nil, fmt.Errorf("metric not found: %s", name)
	}

	return metric, nil
}

// ListMetrics returns all registered metrics
func (mc *MetricsCollector) ListMetrics() []Metric {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	metrics := make([]Metric, 0, len(mc.metrics))
	for _, metric := range mc.metrics {
		metrics = append(metrics, metric)
	}

	return metrics
}

// StartServer starts the metrics HTTP server
func (mc *MetricsCollector) StartServer(ctx context.Context, addr string) error {
	mc.logger.Info("Starting metrics server",
		zap.String("addr", addr))

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(mc.registry, promhttp.HandlerOpts{}))

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		mc.logger.Info("Metrics server started",
			zap.String("addr", addr))

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			mc.logger.Error("Metrics server error",
				zap.Error(err))
		}
	}()

	go func() {
		<-ctx.Done()
		mc.logger.Info("Shutting down metrics server")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			mc.logger.Error("Failed to shutdown metrics server",
				zap.Error(err))
		}
	}()

	return nil
}

// DefaultMetrics creates default metrics for the application
func (mc *MetricsCollector) DefaultMetrics() error {
	// HTTP request counter
	if _, err := mc.NewCounter(
		"http_requests_total",
		"Total number of HTTP requests",
		[]string{"method", "path", "status"},
	); err != nil {
		return err
	}

	// HTTP request duration histogram
	if _, err := mc.NewHistogram(
		"http_request_duration_seconds",
		"HTTP request duration in seconds",
		[]string{"method", "path"},
		prometheus.DefBuckets,
	); err != nil {
		return err
	}

	// Active connections gauge
	if _, err := mc.NewGauge(
		"active_connections",
		"Number of active connections",
		[]string{"type"},
	); err != nil {
		return err
	}

	// Database query counter
	if _, err := mc.NewCounter(
		"database_queries_total",
		"Total number of database queries",
		[]string{"operation", "table"},
	); err != nil {
		return err
	}

	// Database query duration histogram
	if _, err := mc.NewHistogram(
		"database_query_duration_seconds",
		"Database query duration in seconds",
		[]string{"operation", "table"},
		prometheus.DefBuckets,
	); err != nil {
		return err
	}

	// Cache hit/miss counter
	if _, err := mc.NewCounter(
		"cache_operations_total",
		"Total number of cache operations",
		[]string{"operation", "status"},
	); err != nil {
		return err
	}

	// Transcoding jobs counter
	if _, err := mc.NewCounter(
		"transcoding_jobs_total",
		"Total number of transcoding jobs",
		[]string{"status"},
	); err != nil {
		return err
	}

	// Transcoding duration histogram
	if _, err := mc.NewHistogram(
		"transcoding_duration_seconds",
		"Transcoding duration in seconds",
		[]string{"format"},
		prometheus.DefBuckets,
	); err != nil {
		return err
	}

	// Storage usage gauge
	if _, err := mc.NewGauge(
		"storage_usage_bytes",
		"Storage usage in bytes",
		[]string{"type"},
	); err != nil {
		return err
	}

	// NFT verification counter
	if _, err := mc.NewCounter(
		"nft_verifications_total",
		"Total number of NFT verifications",
		[]string{"chain", "status"},
	); err != nil {
		return err
	}

	mc.logger.Info("Default metrics registered")
	return nil
}

// RecordHTTPRequest records an HTTP request
func (mc *MetricsCollector) RecordHTTPRequest(method, path string, status int, duration time.Duration) error {
	counter, err := mc.GetMetric("http_requests_total")
	if err != nil {
		return err
	}

	c, ok := counter.(*Counter)
	if !ok {
		return fmt.Errorf("metric is not a counter")
	}

	c.Increment(method, path, fmt.Sprintf("%d", status))

	histogram, err := mc.GetMetric("http_request_duration_seconds")
	if err != nil {
		return err
	}

	h, ok := histogram.(*Histogram)
	if !ok {
		return fmt.Errorf("metric is not a histogram")
	}

	h.Observe(duration.Seconds(), method, path)

	return nil
}

// RecordDatabaseQuery records a database query
func (mc *MetricsCollector) RecordDatabaseQuery(operation, table string, duration time.Duration) error {
	counter, err := mc.GetMetric("database_queries_total")
	if err != nil {
		return err
	}

	c, ok := counter.(*Counter)
	if !ok {
		return fmt.Errorf("metric is not a counter")
	}

	c.Increment(operation, table)

	histogram, err := mc.GetMetric("database_query_duration_seconds")
	if err != nil {
		return err
	}

	h, ok := histogram.(*Histogram)
	if !ok {
		return fmt.Errorf("metric is not a histogram")
	}

	h.Observe(duration.Seconds(), operation, table)

	return nil
}

// RecordCacheOperation records a cache operation
func (mc *MetricsCollector) RecordCacheOperation(operation, status string) error {
	counter, err := mc.GetMetric("cache_operations_total")
	if err != nil {
		return err
	}

	c, ok := counter.(*Counter)
	if !ok {
		return fmt.Errorf("metric is not a counter")
	}

	c.Increment(operation, status)

	return nil
}

// RecordTranscodingJob records a transcoding job
func (mc *MetricsCollector) RecordTranscodingJob(status, format string, duration time.Duration) error {
	counter, err := mc.GetMetric("transcoding_jobs_total")
	if err != nil {
		return err
	}

	c, ok := counter.(*Counter)
	if !ok {
		return fmt.Errorf("metric is not a counter")
	}

	c.Increment(status)

	histogram, err := mc.GetMetric("transcoding_duration_seconds")
	if err != nil {
		return err
	}

	h, ok := histogram.(*Histogram)
	if !ok {
		return fmt.Errorf("metric is not a histogram")
	}

	h.Observe(duration.Seconds(), format)

	return nil
}

// RecordNFTVerification records an NFT verification
func (mc *MetricsCollector) RecordNFTVerification(chain, status string) error {
	counter, err := mc.GetMetric("nft_verifications_total")
	if err != nil {
		return err
	}

	c, ok := counter.(*Counter)
	if !ok {
		return fmt.Errorf("metric is not a counter")
	}

	c.Increment(chain, status)

	return nil
}

// UpdateStorageUsage updates storage usage metrics
func (mc *MetricsCollector) UpdateStorageUsage(storageType string, usage int64) error {
	gauge, err := mc.GetMetric("storage_usage_bytes")
	if err != nil {
		return err
	}

	g, ok := gauge.(*Gauge)
	if !ok {
		return fmt.Errorf("metric is not a gauge")
	}

	g.Set(float64(usage), storageType)

	return nil
}

// UpdateActiveConnections updates active connections metrics
func (mc *MetricsCollector) UpdateActiveConnections(connectionType string, count int) error {
	gauge, err := mc.GetMetric("active_connections")
	if err != nil {
		return err
	}

	g, ok := gauge.(*Gauge)
	if !ok {
		return fmt.Errorf("metric is not a gauge")
	}

	g.Set(float64(count), connectionType)

	return nil
}
