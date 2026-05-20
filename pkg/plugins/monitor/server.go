package monitor

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"streamgate/pkg/core"
	"streamgate/pkg/core/config"

	"go.uber.org/zap"
)

// MonitorServer handles health monitoring and metrics
type MonitorServer struct {
	config    *config.Config
	logger    *zap.Logger
	kernel    *core.Microkernel
	server    *http.Server
	collector *MetricsCollector
}

// NewMonitorServer creates a new monitor server
func NewMonitorServer(cfg *config.Config, logger *zap.Logger, kernel *core.Microkernel) (*MonitorServer, error) {
	collector := NewMetricsCollector(logger)

	return &MonitorServer{
		config:    cfg,
		logger:    logger,
		kernel:    kernel,
		collector: collector,
	}, nil
}

// Start starts the monitor server
func (s *MonitorServer) Start(ctx context.Context) error {
	handler := NewMonitorHandler(s.collector, s.logger, s.kernel)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", handler.HealthHandler)
	mux.HandleFunc("/health/live", handler.HealthHandler)
	mux.HandleFunc("/health/ready", handler.ReadyHandler)
	mux.HandleFunc("/ready", handler.ReadyHandler)

	// Monitoring endpoints
	mux.HandleFunc("/api/v1/monitor/health", handler.GetHealthHandler)
	mux.HandleFunc("/api/v1/monitor/metrics", handler.GetMetricsHandler)
	mux.HandleFunc("/api/v1/monitor/alerts", handler.GetAlertsHandler)
	mux.HandleFunc("/api/v1/monitor/logs", handler.GetLogsHandler)

	// Prometheus metrics endpoint
	mux.HandleFunc("/metrics", handler.PrometheusMetricsHandler)

	// Catch-all for 404
	mux.HandleFunc("/", handler.NotFoundHandler)

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Server.Port),
		Handler:      mux,
		ReadTimeout:  time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.config.Server.WriteTimeout) * time.Second,
	}

	// Start metrics collection
	s.collector.Start(ctx)

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Monitor server error", zap.Error(err))
		}
	}()

	return nil
}

// Stop stops the monitor server
func (s *MonitorServer) Stop(ctx context.Context) error {
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			s.logger.Error("Error shutting down monitor server", zap.Error(err))
			return err
		}
	}

	if s.collector != nil {
		s.collector.Stop()
	}

	return nil
}

// Health checks the health of the monitor server
func (s *MonitorServer) Health(ctx context.Context) error {
	if s.server == nil {
		return fmt.Errorf("monitor server not started")
	}

	if s.collector == nil {
		return fmt.Errorf("metrics collector not initialized")
	}

	return nil
}

// MetricsCollector collects system and application metrics
type MetricsCollector struct {
	logger       *zap.Logger
	running      bool
	ctx          context.Context
	cancel       context.CancelFunc
	requestCount int64
	errorCount   int64
	totalLatency int64
	reqSamples   int64
	metrics      SystemMetrics
	mu           sync.RWMutex
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(logger *zap.Logger) *MetricsCollector {
	return &MetricsCollector{
		logger: logger,
	}
}

// Start starts the metrics collector
func (c *MetricsCollector) Start(ctx context.Context) {
	if c.running {
		return
	}

	c.ctx, c.cancel = context.WithCancel(ctx)
	c.running = true

	go c.collectMetrics()
	c.logger.Info("Metrics collector started")
}

// Stop stops the metrics collector
func (c *MetricsCollector) Stop() {
	if !c.running {
		return
	}

	c.running = false
	c.cancel()

	c.logger.Info("Metrics collector stopped")
}

// collectMetrics collects metrics periodically
func (c *MetricsCollector) collectMetrics() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			c.mu.Lock()
			c.metrics = SystemMetrics{
				Timestamp:    time.Now().Unix(),
				MemoryUsage:  float64(m.Alloc) / 1024 / 1024,
				RequestCount: atomic.LoadInt64(&c.requestCount),
				ErrorCount:   atomic.LoadInt64(&c.errorCount),
				AvgLatency:   c.avgLatencyLocked(),
			}
			c.mu.Unlock()
		}
	}
}

func (c *MetricsCollector) avgLatencyLocked() float64 {
	if c.reqSamples == 0 {
		return 0
	}
	return float64(atomic.LoadInt64(&c.totalLatency)) / float64(c.reqSamples) / float64(time.Millisecond)
}

func (c *MetricsCollector) GetMetrics() *SystemMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return &SystemMetrics{
		Timestamp:    c.metrics.Timestamp,
		CPUUsage:     float64(runtime.NumGoroutine()),
		MemoryUsage:  c.metrics.MemoryUsage,
		RequestCount: atomic.LoadInt64(&c.requestCount),
		ErrorCount:   atomic.LoadInt64(&c.errorCount),
		AvgLatency:   c.metrics.AvgLatency,
	}
}

func (c *MetricsCollector) GetHealth() *HealthStatus {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	gcPause := m.PauseNs[(m.NumGC+255)%256]
	status := "healthy"
	if gcPause > uint64(100*time.Millisecond) {
		status = "degraded"
	}

	return &HealthStatus{
		Status: status,
	}
}

func (c *MetricsCollector) RecordRequest(latency time.Duration) {
	atomic.AddInt64(&c.requestCount, 1)
	atomic.AddInt64(&c.totalLatency, int64(latency))
	atomic.AddInt64(&c.reqSamples, 1)
}

func (c *MetricsCollector) RecordError() {
	atomic.AddInt64(&c.errorCount, 1)
}

// SystemMetrics represents system metrics
type SystemMetrics struct {
	Timestamp    int64   `json:"timestamp"`
	CPUUsage     float64 `json:"cpu_usage"`
	MemoryUsage  float64 `json:"memory_usage"`
	RequestCount int64   `json:"request_count"`
	ErrorCount   int64   `json:"error_count"`
	AvgLatency   float64 `json:"avg_latency"`
}
