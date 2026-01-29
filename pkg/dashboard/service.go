package dashboard

import (
	"context"
	"sync"
)

// Service provides dashboard functionality
type Service struct {
	mu        sync.RWMutex
	dashboard *Dashboard
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewService creates a new dashboard service
func NewService() *Service {
	ctx, cancel := context.WithCancel(context.Background())

	service := &Service{
		dashboard: NewDashboard(),
		ctx:       ctx,
		cancel:    cancel,
	}

	return service
}

// RecordMetric records a metric
func (s *Service) RecordMetric(name string, value float64, unit string) {
	s.dashboard.RecordMetric(name, value, unit)
}

// CreateAlert creates an alert
func (s *Service) CreateAlert(title, message, severity string) {
	s.dashboard.CreateAlert(title, message, severity)
}

// ResolveAlert resolves an alert
func (s *Service) ResolveAlert(alertID string) {
	s.dashboard.ResolveAlert(alertID)
}

// GetMetrics returns current metrics
func (s *Service) GetMetrics() map[string]*DashboardMetric {
	return s.dashboard.GetMetrics()
}

// GetAlerts returns current alerts
func (s *Service) GetAlerts() []*DashboardAlert {
	return s.dashboard.GetAlerts()
}

// GetMetricHistory returns metric history
func (s *Service) GetMetricHistory(name string, limit int) []*DashboardMetric {
	return s.dashboard.GetMetricHistory(name, limit)
}

// GetAlertHistory returns alert history
func (s *Service) GetAlertHistory(limit int) []*DashboardAlert {
	return s.dashboard.GetAlertHistory(limit)
}

// GetReports returns performance reports
func (s *Service) GetReports(limit int) []*DashboardReport {
	return s.dashboard.GetReports(limit)
}

// GetLatestReport returns the latest report
func (s *Service) GetLatestReport() *DashboardReport {
	return s.dashboard.GetLatestReport()
}

// GetDashboardStatus returns overall dashboard status
func (s *Service) GetDashboardStatus() map[string]interface{} {
	return s.dashboard.GetDashboardStatus()
}

// Close closes the service
func (s *Service) Close() error {
	s.cancel()
	return s.dashboard.Close()
}
