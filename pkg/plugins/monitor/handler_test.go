package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestMonitorHandler(t *testing.T) *MonitorHandler {
	t.Helper()
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	collector := NewMetricsCollector(zap.NewNop())

	return NewMonitorHandler(collector, zap.NewNop(), kernel)
}

func TestMonitorHandler_HealthHandler_Healthy(t *testing.T) {
	handler := newTestMonitorHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	handler.HealthHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMonitorHandler_ReadyHandler(t *testing.T) {
	handler := newTestMonitorHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ReadyHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMonitorHandler_GetHealthHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestMonitorHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetHealthHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestMonitorHandler_GetHealthHandler_Success(t *testing.T) {
	handler := newTestMonitorHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetHealthHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Contains(t, resp, "Status")
}

func TestMonitorHandler_GetMetricsHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestMonitorHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetMetricsHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestMonitorHandler_GetMetricsHandler_Success(t *testing.T) {
	handler := newTestMonitorHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetMetricsHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMonitorHandler_GetAlertsHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestMonitorHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/alerts", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetAlertsHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestMonitorHandler_GetAlertsHandler_Success(t *testing.T) {
	handler := newTestMonitorHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/alerts", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetAlertsHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMonitorHandler_GetAlertsHandler_WithAlerts(t *testing.T) {
	handler := newTestMonitorHandler(t)
	handler.mu.Lock()
	handler.alerts = []*Alert{
		{ID: "alert-1", Level: "warning", Message: "test alert"},
	}
	handler.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/alerts", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetAlertsHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var alerts []*Alert
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &alerts))
	assert.Len(t, alerts, 1)
}

func TestMonitorHandler_GetLogsHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestMonitorHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/logs", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetLogsHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestMonitorHandler_GetLogsHandler_Success(t *testing.T) {
	handler := newTestMonitorHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/logs", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetLogsHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMonitorHandler_PrometheusMetricsHandler(t *testing.T) {
	handler := newTestMonitorHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()

	handler.PrometheusMetricsHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestMonitorHandler_NotFoundHandler(t *testing.T) {
	handler := newTestMonitorHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()

	handler.NotFoundHandler(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestNewMetricsCollector(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())
	assert.NotNil(t, collector)
	assert.False(t, collector.running)
}

func TestMetricsCollector_StartAndStop(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collector.Start(ctx)
	assert.True(t, collector.running)

	collector.Stop()
	assert.False(t, collector.running)
}

func TestMetricsCollector_StartIdempotent(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collector.Start(ctx)
	collector.Start(ctx)

	collector.Stop()
}

func TestMetricsCollector_StopNotRunning(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())
	collector.Stop()
}

func TestMetricsCollector_RecordRequest(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())

	collector.RecordRequest(100 * time.Millisecond)
	collector.RecordRequest(200 * time.Millisecond)

	metrics := collector.GetMetrics()
	assert.Equal(t, int64(2), metrics.RequestCount)
}

func TestMetricsCollector_RecordError(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())

	collector.RecordError()
	collector.RecordError()

	metrics := collector.GetMetrics()
	assert.Equal(t, int64(2), metrics.ErrorCount)
}

func TestMetricsCollector_GetHealth(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())

	health := collector.GetHealth()
	assert.NotNil(t, health)
	assert.Equal(t, "healthy", health.Status)
}

func TestNewMonitorServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewMonitorServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, server)
	assert.NotNil(t, server.collector)
}

func TestMonitorServer_Health_NotStarted(t *testing.T) {
	server := &MonitorServer{logger: zap.NewNop()}

	err := server.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestMonitorServer_Health_NoCollector(t *testing.T) {
	server := &MonitorServer{
		logger: zap.NewNop(),
		server: &http.Server{},
	}

	err := server.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestCollectMetrics(t *testing.T) {
	metrics := CollectMetrics()
	assert.NotNil(t, metrics)
	assert.Equal(t, float64(0), metrics.CPUUsage)
	assert.Equal(t, float64(0), metrics.MemoryUsage)
}

func TestGetHealth(t *testing.T) {
	health := GetHealth()
	assert.NotNil(t, health)
	assert.Equal(t, "healthy", health.Status)
}

func TestGenerateAlert(t *testing.T) {
	alert := GenerateAlert("critical", "system overloaded")
	assert.NotNil(t, alert)
	assert.Equal(t, "critical", alert.Level)
	assert.Equal(t, "system overloaded", alert.Message)
}

func TestMonitorPlugin_NameVersion(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewMonitorPlugin(cfg, zap.NewNop())

	assert.Equal(t, "monitor", plugin.Name())
	assert.Equal(t, "1.0.0", plugin.Version())
}

func TestMonitorPlugin_Health_NotStarted(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewMonitorPlugin(cfg, zap.NewNop())

	err := plugin.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestMonitorPlugin_Init(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewMonitorPlugin(cfg, zap.NewNop())

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, plugin.server)
}

func TestMonitorPlugin_DependsOn(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewMonitorPlugin(cfg, zap.NewNop())

	deps := plugin.DependsOn()
	assert.Nil(t, deps)
}

func TestMonitorPlugin_Stop_NoServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	plugin := NewMonitorPlugin(cfg, zap.NewNop())

	err := plugin.Stop(context.Background())
	require.NoError(t, err)
}

func TestMetricsCollector_RecordRequest_Latency(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())

	collector.RecordRequest(50 * time.Millisecond)
	collector.RecordRequest(150 * time.Millisecond)

	metrics := collector.GetMetrics()
	assert.Equal(t, int64(2), metrics.RequestCount)
}

func TestMetricsCollector_AvgLatency(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector.Start(ctx)

	collector.RecordRequest(100 * time.Millisecond)
	collector.RecordRequest(200 * time.Millisecond)

	metrics := collector.GetMetrics()
	assert.Equal(t, int64(2), metrics.RequestCount)
	assert.GreaterOrEqual(t, metrics.AvgLatency, float64(0))
}

func TestMonitorServer_New(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewMonitorServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, server)
	assert.NotNil(t, server.collector)
}

func TestMonitorServer_Stop(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewMonitorServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)

	err = server.Stop(context.Background())
	require.NoError(t, err)
}

func TestMonitorServer_StartAndStop(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewMonitorServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, server.Start(ctx))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/health", http.NoBody)
	rec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	require.NoError(t, server.Stop(stopCtx))
}

func TestMonitorServer_Health_WithServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewMonitorServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, server.Start(ctx))

	err = server.Health(context.Background())
	require.NoError(t, err)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	_ = server.Stop(stopCtx)
}

func TestMonitorPlugin_StartAndStop(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	plugin := NewMonitorPlugin(cfg, zap.NewNop())
	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	err = plugin.Init(context.Background(), kernel)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = plugin.Start(ctx)
	require.NoError(t, err)

	err = plugin.Health(context.Background())
	require.NoError(t, err)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	err = plugin.Stop(stopCtx)
	require.NoError(t, err)
}

func TestMetricsCollector_AvgLatencyLocked_WithSamples(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())

	collector.RecordRequest(100 * time.Millisecond)
	collector.RecordRequest(200 * time.Millisecond)

	collector.mu.Lock()
	avg := collector.avgLatencyLocked()
	collector.mu.Unlock()

	assert.GreaterOrEqual(t, avg, float64(0))
}

func TestMetricsCollector_CollectMetrics(t *testing.T) {
	collector := NewMetricsCollector(zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector.Start(ctx)

	collector.RecordRequest(50 * time.Millisecond)

	time.Sleep(100 * time.Millisecond)
	cancel()

	metrics := collector.GetMetrics()
	assert.NotNil(t, metrics)
}

func TestMonitorServer_Stop_WithCollector(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewMonitorServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, server.Start(ctx))

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	require.NoError(t, server.Stop(stopCtx))
}

func TestMonitorServer_Routes(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.Port = 0
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewMonitorServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, server.Start(ctx))

	tests := []struct {
		name   string
		method string
		path   string
		code   int
	}{
		{"health", http.MethodGet, "/health", http.StatusOK},
		{"ready", http.MethodGet, "/ready", http.StatusOK},
		{"metrics", http.MethodGet, "/api/v1/monitor/metrics", http.StatusOK},
		{"alerts", http.MethodGet, "/api/v1/monitor/alerts", http.StatusOK},
		{"logs", http.MethodGet, "/api/v1/monitor/logs", http.StatusOK},
		{"prometheus", http.MethodGet, "/metrics", http.StatusOK},
		{"not found", http.MethodGet, "/nonexistent", http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			rec := httptest.NewRecorder()
			server.server.Handler.ServeHTTP(rec, req)
			assert.Equal(t, tc.code, rec.Code)
		})
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	_ = server.Stop(stopCtx)
}
