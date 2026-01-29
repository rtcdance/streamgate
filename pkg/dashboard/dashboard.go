package dashboard

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DashboardMetric represents a dashboard metric
type DashboardMetric struct {
	ID        string
	Name      string
	Value     float64
	Unit      string
	Timestamp time.Time
	Status    string // "healthy", "warning", "critical"
}

// DashboardAlert represents a dashboard alert
type DashboardAlert struct {
	ID        string
	Title     string
	Message   string
	Severity  string // "info", "warning", "critical"
	Timestamp time.Time
	Resolved  bool
}

// DashboardReport represents a performance report
type DashboardReport struct {
	ID              string
	Timestamp       time.Time
	Period          string // "1h", "24h", "7d", "30d"
	Metrics         []*DashboardMetric
	Alerts          []*DashboardAlert
	Recommendations []string
	Summary         string
}

// Dashboard provides performance monitoring and reporting
type Dashboard struct {
	mu              sync.RWMutex
	metrics         map[string]*DashboardMetric
	alerts          []*DashboardAlert
	reports         []*DashboardReport
	metricHistory   map[string][]*DashboardMetric
	alertHistory    []*DashboardAlert
	maxHistorySize  int
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

// NewDashboard creates a new dashboard
func NewDashboard() *Dashboard {
	ctx, cancel := context.WithCancel(context.Background())

	dashboard := &Dashboard{
		metrics:        make(map[string]*DashboardMetric),
		alerts:         make([]*DashboardAlert, 0),
		reports:        make([]*DashboardReport, 0),
		metricHistory:  make(map[string][]*DashboardMetric),
		alertHistory:   make([]*DashboardAlert, 0),
		maxHistorySize: 10000,
		ctx:            ctx,
		cancel:         cancel,
	}

	dashboard.start()
	return dashboard
}

// start begins the dashboard
func (d *Dashboard) start() {
	d.wg.Add(1)
	go d.reportingLoop()
}

// reportingLoop periodically generates reports
func (d *Dashboard) reportingLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.generateReport()
		}
	}
}

// RecordMetric records a metric
func (d *Dashboard) RecordMetric(name string, value float64, unit string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	metric := &DashboardMetric{
		ID:        uuid.New().String(),
		Name:      name,
		Value:     value,
		Unit:      unit,
		Timestamp: time.Now(),
		Status:    d.determineStatus(name, value),
	}

	d.metrics[name] = metric

	// Track history
	if _, ok := d.metricHistory[name]; !ok {
		d.metricHistory[name] = make([]*DashboardMetric, 0)
	}

	d.metricHistory[name] = append(d.metricHistory[name], metric)
	if len(d.metricHistory[name]) > d.maxHistorySize {
		d.metricHistory[name] = d.metricHistory[name][1:]
	}
}

// determineStatus determines metric status
func (d *Dashboard) determineStatus(name string, value float64) string {
	switch name {
	case "memory_usage_mb":
		if value > 500 {
			return "critical"
		} else if value > 400 {
			return "warning"
		}
	case "cpu_usage_percent":
		if value > 80 {
			return "critical"
		} else if value > 60 {
			return "warning"
		}
	case "cache_hit_rate":
		if value < 90 {
			return "warning"
		}
	case "api_latency_ms":
		if value > 100 {
			return "critical"
		} else if value > 50 {
			return "warning"
		}
	}

	return "healthy"
}

// CreateAlert creates an alert
func (d *Dashboard) CreateAlert(title, message, severity string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	alert := &DashboardAlert{
		ID:        uuid.New().String(),
		Title:     title,
		Message:   message,
		Severity:  severity,
		Timestamp: time.Now(),
		Resolved:  false,
	}

	d.alerts = append(d.alerts, alert)
	d.alertHistory = append(d.alertHistory, alert)

	if len(d.alerts) > 100 {
		d.alerts = d.alerts[1:]
	}

	if len(d.alertHistory) > d.maxHistorySize {
		d.alertHistory = d.alertHistory[1:]
	}
}

// ResolveAlert resolves an alert
func (d *Dashboard) ResolveAlert(alertID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, alert := range d.alerts {
		if alert.ID == alertID {
			alert.Resolved = true
			break
		}
	}
}

// GetMetrics returns current metrics
func (d *Dashboard) GetMetrics() map[string]*DashboardMetric {
	d.mu.RLock()
	defer d.mu.RUnlock()

	metrics := make(map[string]*DashboardMetric)
	for k, v := range d.metrics {
		metrics[k] = v
	}

	return metrics
}

// GetAlerts returns current alerts
func (d *Dashboard) GetAlerts() []*DashboardAlert {
	d.mu.RLock()
	defer d.mu.RUnlock()

	alerts := make([]*DashboardAlert, len(d.alerts))
	copy(alerts, d.alerts)

	return alerts
}

// GetMetricHistory returns metric history
func (d *Dashboard) GetMetricHistory(name string, limit int) []*DashboardMetric {
	d.mu.RLock()
	defer d.mu.RUnlock()

	history, ok := d.metricHistory[name]
	if !ok {
		return make([]*DashboardMetric, 0)
	}

	if len(history) <= limit {
		return history
	}

	return history[len(history)-limit:]
}

// GetAlertHistory returns alert history
func (d *Dashboard) GetAlertHistory(limit int) []*DashboardAlert {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.alertHistory) <= limit {
		return d.alertHistory
	}

	return d.alertHistory[len(d.alertHistory)-limit:]
}

// generateReport generates a performance report
func (d *Dashboard) generateReport() {
	d.mu.Lock()
	defer d.mu.Unlock()

	report := &DashboardReport{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Period:    "5m",
		Metrics:   make([]*DashboardMetric, 0),
		Alerts:    make([]*DashboardAlert, 0),
	}

	// Collect current metrics
	for _, metric := range d.metrics {
		report.Metrics = append(report.Metrics, metric)
	}

	// Collect unresolved alerts
	for _, alert := range d.alerts {
		if !alert.Resolved {
			report.Alerts = append(report.Alerts, alert)
		}
	}

	// Generate recommendations
	report.Recommendations = d.generateRecommendations()

	// Generate summary
	report.Summary = d.generateSummary(report)

	d.reports = append(d.reports, report)
	if len(d.reports) > 1000 {
		d.reports = d.reports[1:]
	}
}

// generateRecommendations generates recommendations
func (d *Dashboard) generateRecommendations() []string {
	recommendations := make([]string, 0)

	for name, metric := range d.metrics {
		if metric.Status == "critical" {
			recommendations = append(recommendations, fmt.Sprintf("Critical: %s is %v %s", name, metric.Value, metric.Unit))
		} else if metric.Status == "warning" {
			recommendations = append(recommendations, fmt.Sprintf("Warning: %s is %v %s", name, metric.Value, metric.Unit))
		}
	}

	return recommendations
}

// generateSummary generates a summary
func (d *Dashboard) generateSummary(report *DashboardReport) string {
	criticalCount := 0
	warningCount := 0

	for _, metric := range report.Metrics {
		if metric.Status == "critical" {
			criticalCount++
		} else if metric.Status == "warning" {
			warningCount++
		}
	}

	return fmt.Sprintf("System Status: %d critical, %d warning, %d healthy", criticalCount, warningCount, len(report.Metrics)-criticalCount-warningCount)
}

// GetReports returns performance reports
func (d *Dashboard) GetReports(limit int) []*DashboardReport {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.reports) <= limit {
		return d.reports
	}

	return d.reports[len(d.reports)-limit:]
}

// GetLatestReport returns the latest report
func (d *Dashboard) GetLatestReport() *DashboardReport {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.reports) == 0 {
		return nil
	}

	return d.reports[len(d.reports)-1]
}

// GetDashboardStatus returns overall dashboard status
func (d *Dashboard) GetDashboardStatus() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	status := make(map[string]interface{})

	criticalCount := 0
	warningCount := 0
	healthyCount := 0

	for _, metric := range d.metrics {
		if metric.Status == "critical" {
			criticalCount++
		} else if metric.Status == "warning" {
			warningCount++
		} else {
			healthyCount++
		}
	}

	status["total_metrics"] = len(d.metrics)
	status["critical_metrics"] = criticalCount
	status["warning_metrics"] = warningCount
	status["healthy_metrics"] = healthyCount
	status["total_alerts"] = len(d.alerts)
	status["unresolved_alerts"] = len(d.alerts)
	status["overall_status"] = d.determineOverallStatus(criticalCount, warningCount)

	return status
}

// determineOverallStatus determines overall status
func (d *Dashboard) determineOverallStatus(critical, warning int) string {
	if critical > 0 {
		return "critical"
	} else if warning > 0 {
		return "warning"
	}

	return "healthy"
}

// Close closes the dashboard
func (d *Dashboard) Close() error {
	d.cancel()
	d.wg.Wait()
	return nil
}
