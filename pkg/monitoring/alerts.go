package monitoring

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Alert represents a system alert
type Alert struct {
	ID         string
	Level      string // critical, warning, info
	Title      string
	Message    string
	Service    string
	Metric     string
	Value      float64
	Threshold  float64
	CreatedAt  time.Time
	ResolvedAt *time.Time
	Status     string // active, resolved
}

// AlertRule defines a rule for triggering alerts
type AlertRule struct {
	ID        string
	Name      string
	Metric    string
	Condition string // gt, lt, eq, gte, lte
	Threshold float64
	Duration  time.Duration
	Level     string
	Enabled   bool
}

// AlertManager manages system alerts
type AlertManager struct {
	logger      *zap.Logger
	mu          sync.RWMutex
	alerts      map[string]*Alert
	rules       map[string]*AlertRule
	handlers    []AlertHandler
	lastChecked map[string]time.Time
}

// AlertHandler is a function that handles alerts
type AlertHandler func(alert *Alert) error

// NewAlertManager creates a new alert manager
func NewAlertManager(logger *zap.Logger) *AlertManager {
	return &AlertManager{
		logger:      logger,
		alerts:      make(map[string]*Alert),
		rules:       make(map[string]*AlertRule),
		handlers:    make([]AlertHandler, 0),
		lastChecked: make(map[string]time.Time),
	}
}

// AddRule adds an alert rule
func (am *AlertManager) AddRule(rule *AlertRule) {
	am.mu.Lock()
	defer am.mu.Unlock()

	am.rules[rule.ID] = rule
	am.logger.Info("Alert rule added", zap.String("rule_id", rule.ID), zap.String("name", rule.Name))
}

// RemoveRule removes an alert rule
func (am *AlertManager) RemoveRule(ruleID string) {
	am.mu.Lock()
	defer am.mu.Unlock()

	delete(am.rules, ruleID)
	am.logger.Info("Alert rule removed", zap.String("rule_id", ruleID))
}

// RegisterHandler registers an alert handler
func (am *AlertManager) RegisterHandler(handler AlertHandler) {
	am.mu.Lock()
	defer am.mu.Unlock()

	am.handlers = append(am.handlers, handler)
	am.logger.Debug("Alert handler registered")
}

// CheckMetric checks a metric against alert rules
func (am *AlertManager) CheckMetric(metricName string, value float64) {
	am.mu.RLock()
	rules := make([]*AlertRule, 0)
	for _, rule := range am.rules {
		if rule.Metric == metricName && rule.Enabled {
			rules = append(rules, rule)
		}
	}
	am.mu.RUnlock()

	for _, rule := range rules {
		if am.shouldTriggerAlert(rule, value) {
			am.triggerAlert(rule, value)
		}
	}
}

// shouldTriggerAlert checks if an alert should be triggered
func (am *AlertManager) shouldTriggerAlert(rule *AlertRule, value float64) bool {
	switch rule.Condition {
	case "gt":
		return value > rule.Threshold
	case "lt":
		return value < rule.Threshold
	case "eq":
		return value == rule.Threshold
	case "gte":
		return value >= rule.Threshold
	case "lte":
		return value <= rule.Threshold
	default:
		return false
	}
}

// triggerAlert triggers an alert
func (am *AlertManager) triggerAlert(rule *AlertRule, value float64) {
	alert := &Alert{
		ID:        fmt.Sprintf("alert-%d", time.Now().UnixNano()),
		Level:     rule.Level,
		Title:     rule.Name,
		Message:   fmt.Sprintf("Metric %s has value %f (threshold: %f)", rule.Metric, value, rule.Threshold),
		Metric:    rule.Metric,
		Value:     value,
		Threshold: rule.Threshold,
		CreatedAt: time.Now(),
		Status:    "active",
	}

	am.mu.Lock()
	am.alerts[alert.ID] = alert
	am.mu.Unlock()

	am.logger.Warn("Alert triggered", zap.String("alert_id", alert.ID), zap.String("level", alert.Level), zap.String("title", alert.Title))

	// Call handlers
	for _, handler := range am.handlers {
		if err := handler(alert); err != nil {
			am.logger.Error("Error handling alert", zap.String("alert_id", alert.ID), zap.Error(err))
		}
	}
}

// ResolveAlert resolves an alert
func (am *AlertManager) ResolveAlert(alertID string) {
	am.mu.Lock()
	defer am.mu.Unlock()

	alert, exists := am.alerts[alertID]
	if !exists {
		return
	}

	now := time.Now()
	alert.ResolvedAt = &now
	alert.Status = "resolved"

	am.logger.Info("Alert resolved", zap.String("alert_id", alertID))
}

// GetAlert gets an alert by ID
func (am *AlertManager) GetAlert(alertID string) *Alert {
	am.mu.RLock()
	defer am.mu.RUnlock()

	return am.alerts[alertID]
}

// GetActiveAlerts returns all active alerts
func (am *AlertManager) GetActiveAlerts() []*Alert {
	am.mu.RLock()
	defer am.mu.RUnlock()

	alerts := make([]*Alert, 0)
	for _, alert := range am.alerts {
		if alert.Status == "active" {
			alerts = append(alerts, alert)
		}
	}

	return alerts
}

// GetAlertsByLevel returns alerts by level
func (am *AlertManager) GetAlertsByLevel(level string) []*Alert {
	am.mu.RLock()
	defer am.mu.RUnlock()

	alerts := make([]*Alert, 0)
	for _, alert := range am.alerts {
		if alert.Level == level {
			alerts = append(alerts, alert)
		}
	}

	return alerts
}

// GetAlertCount returns the count of alerts
func (am *AlertManager) GetAlertCount() map[string]int {
	am.mu.RLock()
	defer am.mu.RUnlock()

	counts := map[string]int{
		"total":    len(am.alerts),
		"active":   0,
		"resolved": 0,
		"critical": 0,
		"warning":  0,
		"info":     0,
	}

	for _, alert := range am.alerts {
		if alert.Status == "active" {
			counts["active"]++
		} else {
			counts["resolved"]++
		}

		switch alert.Level {
		case "critical":
			counts["critical"]++
		case "warning":
			counts["warning"]++
		case "info":
			counts["info"]++
		}
	}

	return counts
}

// HealthChecker checks system health
type HealthChecker struct {
	logger *zap.Logger
	checks map[string]HealthCheck
	mu     sync.RWMutex
}

// HealthCheck is a function that checks health
type HealthCheck func() (bool, string, error)

// HealthStatus represents the health status
type HealthStatus struct {
	Status    string
	Checks    map[string]CheckResult
	Uptime    time.Duration
	LastCheck time.Time
}

// CheckResult represents the result of a health check
type CheckResult struct {
	Status    bool
	Message   string
	Error     string
	LastCheck time.Time
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(logger *zap.Logger) *HealthChecker {
	return &HealthChecker{
		logger: logger,
		checks: make(map[string]HealthCheck),
	}
}

// RegisterCheck registers a health check
func (hc *HealthChecker) RegisterCheck(name string, check HealthCheck) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.checks[name] = check
	hc.logger.Debug("Health check registered", zap.String("name", name))
}

// Check performs all health checks
func (hc *HealthChecker) Check() *HealthStatus {
	hc.mu.RLock()
	checks := make(map[string]HealthCheck)
	for name, check := range hc.checks {
		checks[name] = check
	}
	hc.mu.RUnlock()

	status := &HealthStatus{
		Status:    "healthy",
		Checks:    make(map[string]CheckResult),
		LastCheck: time.Now(),
	}

	for name, check := range checks {
		healthy, message, err := check()

		result := CheckResult{
			Status:    healthy,
			Message:   message,
			LastCheck: time.Now(),
		}

		if err != nil {
			result.Error = err.Error()
			status.Status = "unhealthy"
		}

		if !healthy {
			status.Status = "degraded"
		}

		status.Checks[name] = result
	}

	return status
}

// GetStatus gets the current health status
func (hc *HealthChecker) GetStatus() *HealthStatus {
	return hc.Check()
}
