package monitoring

import (
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// GrafanaDashboard represents a Grafana dashboard configuration
type GrafanaDashboard struct {
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Tags        []string          `json:"tags"`
	Timezone    string            `json:"timezone"`
	Panels      []GrafanaPanel    `json:"panels"`
	Templating  GrafanaTemplating `json:"templating"`
	Time        GrafanaTime       `json:"time"`
	Refresh     string            `json:"refresh"`
}

// GrafanaPanel represents a Grafana panel
type GrafanaPanel struct {
	ID          int                    `json:"id"`
	Title       string                 `json:"title"`
	Type        string                 `json:"type"`
	GridPos     GrafanaGridPos         `json:"gridPos"`
	Targets     []GrafanaTarget        `json:"targets"`
	Options     map[string]interface{} `json:"options"`
	FieldConfig map[string]interface{} `json:"fieldConfig"`
}

// GrafanaGridPos represents panel grid position
type GrafanaGridPos struct {
	H int `json:"h"`
	W int `json:"w"`
	X int `json:"x"`
	Y int `json:"y"`
}

// GrafanaTarget represents a Prometheus query target
type GrafanaTarget struct {
	Expr         string `json:"expr"`
	RefID        string `json:"refId"`
	LegendFormat string `json:"legendFormat"`
}

// GrafanaTemplating represents dashboard templating
type GrafanaTemplating struct {
	List []GrafanaVariable `json:"list"`
}

// GrafanaVariable represents a template variable
type GrafanaVariable struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Options []string `json:"options"`
	Current string   `json:"current"`
}

// GrafanaTime represents time range
type GrafanaTime struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// DashboardBuilder builds Grafana dashboards
type DashboardBuilder struct {
	logger *zap.Logger
}

// NewDashboardBuilder creates a new dashboard builder
func NewDashboardBuilder(logger *zap.Logger) *DashboardBuilder {
	return &DashboardBuilder{
		logger: logger,
	}
}

// BuildStreamGateDashboard builds the main StreamGate dashboard
func (db *DashboardBuilder) BuildStreamGateDashboard() *GrafanaDashboard {
	dashboard := &GrafanaDashboard{
		Title:       "StreamGate Monitoring",
		Description: "Comprehensive monitoring dashboard for StreamGate platform",
		Tags:        []string{"streamgate", "monitoring", "production"},
		Timezone:    "browser",
		Refresh:     "30s",
		Time: GrafanaTime{
			From: "now-6h",
			To:   "now",
		},
		Templating: GrafanaTemplating{
			List: []GrafanaVariable{
				{
					Name:    "service",
					Type:    "query",
					Options: []string{"api-gateway", "upload", "streaming", "metadata", "cache", "auth", "worker", "transcoder", "monitor"},
					Current: "api-gateway",
				},
			},
		},
		Panels: []GrafanaPanel{
			db.buildRequestRatePanel(),
			db.buildErrorRatePanel(),
			db.buildLatencyPanel(),
			db.buildCacheHitRatePanel(),
			db.buildActiveConnectionsPanel(),
			db.buildMemoryUsagePanel(),
			db.buildCPUUsagePanel(),
			db.buildUploadMetricsPanel(),
			db.buildStreamingMetricsPanel(),
			db.buildTranscodingMetricsPanel(),
		},
	}

	return dashboard
}

// buildRequestRatePanel builds request rate panel
func (db *DashboardBuilder) buildRequestRatePanel() GrafanaPanel {
	return GrafanaPanel{
		ID:    1,
		Title: "Request Rate",
		Type:  "graph",
		GridPos: GrafanaGridPos{
			H: 8,
			W: 12,
			X: 0,
			Y: 0,
		},
		Targets: []GrafanaTarget{
			{
				Expr:         "rate(streamgate_requests_total[5m])",
				RefID:        "A",
				LegendFormat: "{{service}}",
			},
		},
		Options: map[string]interface{}{
			"legend": map[string]interface{}{
				"calcs":  []string{"mean", "max"},
				"values": true,
			},
		},
	}
}

// buildErrorRatePanel builds error rate panel
func (db *DashboardBuilder) buildErrorRatePanel() GrafanaPanel {
	return GrafanaPanel{
		ID:    2,
		Title: "Error Rate",
		Type:  "graph",
		GridPos: GrafanaGridPos{
			H: 8,
			W: 12,
			X: 12,
			Y: 0,
		},
		Targets: []GrafanaTarget{
			{
				Expr:         "rate(streamgate_service_errors[5m]) / rate(streamgate_service_requests[5m])",
				RefID:        "A",
				LegendFormat: "{{service}}",
			},
		},
		Options: map[string]interface{}{
			"legend": map[string]interface{}{
				"calcs":  []string{"mean", "max"},
				"values": true,
			},
		},
	}
}

// buildLatencyPanel builds latency panel
func (db *DashboardBuilder) buildLatencyPanel() GrafanaPanel {
	return GrafanaPanel{
		ID:    3,
		Title: "Latency (ms)",
		Type:  "graph",
		GridPos: GrafanaGridPos{
			H: 8,
			W: 12,
			X: 0,
			Y: 8,
		},
		Targets: []GrafanaTarget{
			{
				Expr:         "streamgate_service_latency_avg",
				RefID:        "A",
				LegendFormat: "{{service}} avg",
			},
			{
				Expr:         "streamgate_service_latency_max",
				RefID:        "B",
				LegendFormat: "{{service}} max",
			},
		},
		Options: map[string]interface{}{
			"legend": map[string]interface{}{
				"calcs":  []string{"mean", "max"},
				"values": true,
			},
		},
	}
}

// buildCacheHitRatePanel builds cache hit rate panel
func (db *DashboardBuilder) buildCacheHitRatePanel() GrafanaPanel {
	return GrafanaPanel{
		ID:    4,
		Title: "Cache Hit Rate",
		Type:  "gauge",
		GridPos: GrafanaGridPos{
			H: 8,
			W: 12,
			X: 12,
			Y: 8,
		},
		Targets: []GrafanaTarget{
			{
				Expr:  "rate(streamgate_requests_total{endpoint=~\".*cache.*\"}[5m])",
				RefID: "A",
			},
		},
		Options: map[string]interface{}{
			"orientation":          "auto",
			"showThresholdLabels":  false,
			"showThresholdMarkers": true,
		},
	}
}

// buildActiveConnectionsPanel builds active connections panel
func (db *DashboardBuilder) buildActiveConnectionsPanel() GrafanaPanel {
	return GrafanaPanel{
		ID:    5,
		Title: "Active Connections",
		Type:  "stat",
		GridPos: GrafanaGridPos{
			H: 4,
			W: 6,
			X: 0,
			Y: 16,
		},
		Targets: []GrafanaTarget{
			{
				Expr:  "streamgate_gauge_value{metric=\"active_connections\"}",
				RefID: "A",
			},
		},
		Options: map[string]interface{}{
			"colorMode": "background",
			"graphMode": "area",
		},
	}
}

// buildMemoryUsagePanel builds memory usage panel
func (db *DashboardBuilder) buildMemoryUsagePanel() GrafanaPanel {
	return GrafanaPanel{
		ID:    6,
		Title: "Memory Usage",
		Type:  "stat",
		GridPos: GrafanaGridPos{
			H: 4,
			W: 6,
			X: 6,
			Y: 16,
		},
		Targets: []GrafanaTarget{
			{
				Expr:  "streamgate_gauge_value{metric=\"memory_usage\"}",
				RefID: "A",
			},
		},
		Options: map[string]interface{}{
			"colorMode": "background",
			"graphMode": "area",
		},
	}
}

// buildCPUUsagePanel builds CPU usage panel
func (db *DashboardBuilder) buildCPUUsagePanel() GrafanaPanel {
	return GrafanaPanel{
		ID:    7,
		Title: "CPU Usage",
		Type:  "stat",
		GridPos: GrafanaGridPos{
			H: 4,
			W: 6,
			X: 12,
			Y: 16,
		},
		Targets: []GrafanaTarget{
			{
				Expr:  "streamgate_gauge_value{metric=\"cpu_usage\"}",
				RefID: "A",
			},
		},
		Options: map[string]interface{}{
			"colorMode": "background",
			"graphMode": "area",
		},
	}
}

// buildUploadMetricsPanel builds upload metrics panel
func (db *DashboardBuilder) buildUploadMetricsPanel() GrafanaPanel {
	return GrafanaPanel{
		ID:    8,
		Title: "Upload Metrics",
		Type:  "table",
		GridPos: GrafanaGridPos{
			H: 8,
			W: 12,
			X: 0,
			Y: 20,
		},
		Targets: []GrafanaTarget{
			{
				Expr:  "rate(streamgate_requests_total{service=\"upload\"}[5m])",
				RefID: "A",
			},
		},
		Options: map[string]interface{}{
			"showHeader": true,
		},
	}
}

// buildStreamingMetricsPanel builds streaming metrics panel
func (db *DashboardBuilder) buildStreamingMetricsPanel() GrafanaPanel {
	return GrafanaPanel{
		ID:    9,
		Title: "Streaming Metrics",
		Type:  "table",
		GridPos: GrafanaGridPos{
			H: 8,
			W: 12,
			X: 12,
			Y: 20,
		},
		Targets: []GrafanaTarget{
			{
				Expr:  "rate(streamgate_requests_total{service=\"streaming\"}[5m])",
				RefID: "A",
			},
		},
		Options: map[string]interface{}{
			"showHeader": true,
		},
	}
}

// buildTranscodingMetricsPanel builds transcoding metrics panel
func (db *DashboardBuilder) buildTranscodingMetricsPanel() GrafanaPanel {
	return GrafanaPanel{
		ID:    10,
		Title: "Transcoding Metrics",
		Type:  "table",
		GridPos: GrafanaGridPos{
			H: 8,
			W: 24,
			X: 0,
			Y: 28,
		},
		Targets: []GrafanaTarget{
			{
				Expr:  "rate(streamgate_requests_total{service=\"transcoder\"}[5m])",
				RefID: "A",
			},
		},
		Options: map[string]interface{}{
			"showHeader": true,
		},
	}
}

// ExportJSON exports dashboard as JSON
func (db *DashboardBuilder) ExportJSON(dashboard *GrafanaDashboard) (string, error) {
	data, err := json.MarshalIndent(dashboard, "", "  ")
	if err != nil {
		db.logger.Error("Failed to marshal dashboard",
			zap.Error(err))
		return "", err
	}

	return string(data), nil
}

// DashboardManager manages multiple dashboards
type DashboardManager struct {
	logger     *zap.Logger
	dashboards map[string]*GrafanaDashboard
}

// NewDashboardManager creates a new dashboard manager
func NewDashboardManager(logger *zap.Logger) *DashboardManager {
	return &DashboardManager{
		logger:     logger,
		dashboards: make(map[string]*GrafanaDashboard),
	}
}

// RegisterDashboard registers a dashboard
func (dm *DashboardManager) RegisterDashboard(name string, dashboard *GrafanaDashboard) {
	dm.dashboards[name] = dashboard
	dm.logger.Debug("Dashboard registered",
		zap.String("name", name))
}

// GetDashboard gets a dashboard
func (dm *DashboardManager) GetDashboard(name string) *GrafanaDashboard {
	return dm.dashboards[name]
}

// ExportAllDashboards exports all dashboards as JSON
func (dm *DashboardManager) ExportAllDashboards() map[string]string {
	result := make(map[string]string)

	for name, dashboard := range dm.dashboards {
		data, err := json.MarshalIndent(dashboard, "", "  ")
		if err != nil {
			dm.logger.Error("Failed to marshal dashboard",
				zap.String("name", name),
				zap.Error(err))
			continue
		}

		result[name] = string(data)
	}

	return result
}

// GenerateDashboardURL generates a Grafana dashboard URL
func GenerateDashboardURL(grafanaURL string, dashboardName string) string {
	return fmt.Sprintf("%s/d/%s/%s", grafanaURL, dashboardName, dashboardName)
}

// GrafanaAlertRule represents a Grafana alert rule
type GrafanaAlertRule struct {
	Name        string
	Condition   string
	Threshold   float64
	Duration    time.Duration
	Severity    string
	Description string
}

// AlertRuleBuilder builds alert rules
type AlertRuleBuilder struct {
	logger *zap.Logger
}

// NewAlertRuleBuilder creates a new alert rule builder
func NewAlertRuleBuilder(logger *zap.Logger) *AlertRuleBuilder {
	return &AlertRuleBuilder{
		logger: logger,
	}
}

// BuildHighErrorRateAlert builds high error rate alert
func (arb *AlertRuleBuilder) BuildHighErrorRateAlert() *AlertRule {
	return &AlertRule{
		Name:      "HighErrorRate",
		Condition: "rate(streamgate_service_errors[5m]) / rate(streamgate_service_requests[5m]) > 0.1",
		Threshold: 0.1,
		Duration:  5 * time.Minute,
		Level:     "critical",
	}
}

// BuildHighLatencyAlert builds high latency alert
func (arb *AlertRuleBuilder) BuildHighLatencyAlert() *AlertRule {
	return &AlertRule{
		Name:      "HighLatency",
		Condition: "streamgate_service_latency_avg > 5000",
		Threshold: 5000,
		Duration:  5 * time.Minute,
		Level:     "warning",
	}
}

// BuildHighMemoryAlert builds high memory alert
func (arb *AlertRuleBuilder) BuildHighMemoryAlert() *AlertRule {
	return &AlertRule{
		Name:      "HighMemory",
		Condition: "streamgate_gauge_value{metric=\"memory_usage\"} > 80",
		Threshold: 80,
		Duration:  5 * time.Minute,
		Level:     "warning",
	}
}

// BuildHighCPUAlert builds high CPU alert
func (arb *AlertRuleBuilder) BuildHighCPUAlert() *AlertRule {
	return &AlertRule{
		Name:      "HighCPU",
		Condition: "streamgate_gauge_value{metric=\"cpu_usage\"} > 80",
		Threshold: 80,
		Duration:  5 * time.Minute,
		Level:     "warning",
	}
}

// BuildServiceDownAlert builds service down alert
func (arb *AlertRuleBuilder) BuildServiceDownAlert() *AlertRule {
	return &AlertRule{
		Name:      "ServiceDown",
		Condition: "up{job=\"streamgate\"} == 0",
		Threshold: 0,
		Duration:  1 * time.Minute,
		Level:     "critical",
	}
}
