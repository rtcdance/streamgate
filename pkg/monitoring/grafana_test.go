package monitoring

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewDashboardBuilder(t *testing.T) {
	builder := NewDashboardBuilder(zap.NewNop())
	assert.NotNil(t, builder)
}

func TestDashboardBuilder_BuildStreamGateDashboard(t *testing.T) {
	builder := NewDashboardBuilder(zap.NewNop())
	dashboard := builder.BuildStreamGateDashboard()

	require.NotNil(t, dashboard)
	assert.Equal(t, "StreamGate Monitoring", dashboard.Title)
	assert.Equal(t, "browser", dashboard.Timezone)
	assert.Equal(t, "30s", dashboard.Refresh)
	assert.Equal(t, "now-6h", dashboard.Time.From)
	assert.Equal(t, "now", dashboard.Time.To)
	assert.NotEmpty(t, dashboard.Tags)
	assert.Len(t, dashboard.Panels, 10)
	assert.NotEmpty(t, dashboard.Templating.List)
}

func TestDashboardBuilder_PanelStructure(t *testing.T) {
	builder := NewDashboardBuilder(zap.NewNop())
	dashboard := builder.BuildStreamGateDashboard()

	tests := []struct {
		name       string
		panelIndex int
		expectedID int
		title      string
		panelType  string
	}{
		{"request rate panel", 0, 1, "Request Rate", "graph"},
		{"error rate panel", 1, 2, "Error Rate", "graph"},
		{"latency panel", 2, 3, "Latency (ms)", "graph"},
		{"cache hit rate panel", 3, 4, "Cache Hit Rate", "gauge"},
		{"active connections panel", 4, 5, "Active Connections", "stat"},
		{"memory usage panel", 5, 6, "Memory Usage", "stat"},
		{"cpu usage panel", 6, 7, "CPU Usage", "stat"},
		{"upload metrics panel", 7, 8, "Upload Metrics", "table"},
		{"streaming metrics panel", 8, 9, "Streaming Metrics", "table"},
		{"transcoding metrics panel", 9, 10, "Transcoding Metrics", "table"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			panel := dashboard.Panels[tc.panelIndex]
			assert.Equal(t, tc.expectedID, panel.ID)
			assert.Equal(t, tc.title, panel.Title)
			assert.Equal(t, tc.panelType, panel.Type)
			assert.NotEmpty(t, panel.Targets)
		})
	}
}

func TestDashboardBuilder_PanelTargets(t *testing.T) {
	builder := NewDashboardBuilder(zap.NewNop())
	dashboard := builder.BuildStreamGateDashboard()

	requestRatePanel := dashboard.Panels[0]
	require.Len(t, requestRatePanel.Targets, 1)
	assert.Contains(t, requestRatePanel.Targets[0].Expr, "streamgate_http_requests_total")
	assert.Equal(t, "A", requestRatePanel.Targets[0].RefID)

	latencyPanel := dashboard.Panels[2]
	require.Len(t, latencyPanel.Targets, 2)
	assert.Contains(t, latencyPanel.Targets[0].Expr, "streamgate_service_latency_ms")
	assert.Contains(t, latencyPanel.Targets[1].Expr, "histogram_quantile")
}

func TestDashboardBuilder_Templating(t *testing.T) {
	builder := NewDashboardBuilder(zap.NewNop())
	dashboard := builder.BuildStreamGateDashboard()

	require.Len(t, dashboard.Templating.List, 1)
	svcVar := dashboard.Templating.List[0]
	assert.Equal(t, "service", svcVar.Name)
	assert.Equal(t, "query", svcVar.Type)
	assert.Equal(t, "api-gateway", svcVar.Current)
	assert.Len(t, svcVar.Options, 9)
}

func TestDashboardBuilder_ExportJSON(t *testing.T) {
	builder := NewDashboardBuilder(zap.NewNop())
	dashboard := builder.BuildStreamGateDashboard()

	jsonStr, err := builder.ExportJSON(dashboard)
	require.NoError(t, err)
	assert.NotEmpty(t, jsonStr)
	assert.Contains(t, jsonStr, "StreamGate Monitoring")
	assert.Contains(t, jsonStr, "panels")
}

func TestDashboardManager_RegisterAndGet(t *testing.T) {
	dm := NewDashboardManager(zap.NewNop())

	dashboard := &GrafanaDashboard{Title: "Test Dashboard"}
	dm.RegisterDashboard("test", dashboard)

	retrieved := dm.GetDashboard("test")
	assert.Equal(t, dashboard, retrieved)

	missing := dm.GetDashboard("nonexistent")
	assert.Nil(t, missing)
}

func TestDashboardManager_ExportAllDashboards(t *testing.T) {
	dm := NewDashboardManager(zap.NewNop())

	dm.RegisterDashboard("main", &GrafanaDashboard{Title: "Main"})
	dm.RegisterDashboard("infra", &GrafanaDashboard{Title: "Infrastructure"})

	exports := dm.ExportAllDashboards()
	assert.Len(t, exports, 2)
	assert.Contains(t, exports, "main")
	assert.Contains(t, exports, "infra")
	assert.Contains(t, exports["main"], "Main")
	assert.Contains(t, exports["infra"], "Infrastructure")
}

func TestGenerateDashboardURL(t *testing.T) {
	tests := []struct {
		name         string
		grafanaURL   string
		dashboardName string
		expected     string
	}{
		{"standard", "http://grafana.example.com", "main", "http://grafana.example.com/d/main/main"},
		{"with port", "http://localhost:3000", "infra", "http://localhost:3000/d/infra/infra"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := GenerateDashboardURL(tc.grafanaURL, tc.dashboardName)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestNewAlertRuleBuilder(t *testing.T) {
	builder := NewAlertRuleBuilder(zap.NewNop())
	assert.NotNil(t, builder)
}

func TestAlertRuleBuilder_BuildHighErrorRateAlert(t *testing.T) {
	builder := NewAlertRuleBuilder(zap.NewNop())
	rule := builder.BuildHighErrorRateAlert()

	require.NotNil(t, rule)
	assert.Equal(t, "HighErrorRate", rule.Name)
	assert.Equal(t, 0.1, rule.Threshold)
	assert.Equal(t, "critical", rule.Level)
	assert.Equal(t, 5*time.Minute, rule.Duration)
	assert.Contains(t, rule.Condition, "error")
}

func TestAlertRuleBuilder_BuildHighLatencyAlert(t *testing.T) {
	builder := NewAlertRuleBuilder(zap.NewNop())
	rule := builder.BuildHighLatencyAlert()

	require.NotNil(t, rule)
	assert.Equal(t, "HighLatency", rule.Name)
	assert.Equal(t, 5000.0, rule.Threshold)
	assert.Equal(t, "warning", rule.Level)
}

func TestAlertRuleBuilder_BuildHighMemoryAlert(t *testing.T) {
	builder := NewAlertRuleBuilder(zap.NewNop())
	rule := builder.BuildHighMemoryAlert()

	require.NotNil(t, rule)
	assert.Equal(t, "HighMemory", rule.Name)
	assert.Equal(t, 80.0, rule.Threshold)
	assert.Equal(t, "warning", rule.Level)
}

func TestAlertRuleBuilder_BuildHighCPUAlert(t *testing.T) {
	builder := NewAlertRuleBuilder(zap.NewNop())
	rule := builder.BuildHighCPUAlert()

	require.NotNil(t, rule)
	assert.Equal(t, "HighCPU", rule.Name)
	assert.Equal(t, 80.0, rule.Threshold)
	assert.Equal(t, "warning", rule.Level)
}

func TestAlertRuleBuilder_BuildServiceDownAlert(t *testing.T) {
	builder := NewAlertRuleBuilder(zap.NewNop())
	rule := builder.BuildServiceDownAlert()

	require.NotNil(t, rule)
	assert.Equal(t, "ServiceDown", rule.Name)
	assert.Equal(t, 0.0, rule.Threshold)
	assert.Equal(t, "critical", rule.Level)
	assert.Equal(t, 1*time.Minute, rule.Duration)
}
