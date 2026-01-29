package ml

import (
	"fmt"
	"sync"
	"time"
)

// AlertingSystem manages alerts for detected anomalies
type AlertingSystem struct {
	mu              sync.RWMutex
	alerts          []*Alert
	alertRules      map[string]*MLAlertRule
	alertChannels   map[string]AlertChannel
	alertHistory    map[string][]*Alert
	suppressedUntil map[string]time.Time
}

// Alert represents an alert
type Alert struct {
	ID             string
	AnomalyID      string
	Severity       string
	Title          string
	Description    string
	Timestamp      time.Time
	Acknowledged   bool
	AcknowledgedAt time.Time
	AcknowledgedBy string
	Resolved       bool
	ResolvedAt     time.Time
}

// MLAlertRule defines rules for generating ML-based alerts
type MLAlertRule struct {
	ID               string
	Name             string
	Condition        string
	Threshold        float64
	Severity         string
	Enabled          bool
	SuppressDuration time.Duration
	NotifyChannels   []string
}

// AlertChannel defines how alerts are sent
type AlertChannel interface {
	Send(alert *Alert) error
	Name() string
}

// NewAlertingSystem creates a new alerting system
func NewAlertingSystem() *AlertingSystem {
	return &AlertingSystem{
		alerts:          make([]*Alert, 0),
		alertRules:      make(map[string]*MLAlertRule),
		alertChannels:   make(map[string]AlertChannel),
		alertHistory:    make(map[string][]*Alert),
		suppressedUntil: make(map[string]time.Time),
	}
}

// GenerateAlert generates an alert for an anomaly
func (as *AlertingSystem) GenerateAlert(anomaly *Anomaly) error {
	if anomaly == nil {
		return fmt.Errorf("invalid anomaly")
	}

	as.mu.Lock()
	defer as.mu.Unlock()

	// Check if alert is suppressed
	if suppressedUntil, exists := as.suppressedUntil[anomaly.Type]; exists {
		if time.Now().Before(suppressedUntil) {
			return nil // Alert suppressed
		}
	}

	alert := &Alert{
		ID:          fmt.Sprintf("alert_%d", time.Now().Unix()),
		AnomalyID:   anomaly.ID,
		Severity:    anomaly.Severity,
		Title:       fmt.Sprintf("Anomaly Detected: %s", anomaly.Type),
		Description: anomaly.Description,
		Timestamp:   time.Now(),
	}

	as.alerts = append(as.alerts, alert)

	// Add to history
	if _, exists := as.alertHistory[anomaly.Type]; !exists {
		as.alertHistory[anomaly.Type] = make([]*Alert, 0)
	}
	as.alertHistory[anomaly.Type] = append(as.alertHistory[anomaly.Type], alert)

	// Send to channels
	as.sendAlert(alert)

	// Set suppression
	if rule, exists := as.alertRules[anomaly.Type]; exists {
		as.suppressedUntil[anomaly.Type] = time.Now().Add(rule.SuppressDuration)
	}

	return nil
}

// sendAlert sends alert to configured channels
func (as *AlertingSystem) sendAlert(alert *Alert) {
	for _, channel := range as.alertChannels {
		go func(ch AlertChannel) {
			if err := ch.Send(alert); err != nil {
				// Log error but don't fail
				fmt.Printf("Failed to send alert via %s: %v\n", ch.Name(), err)
			}
		}(channel)
	}
}

// AddAlertRule adds an alert rule
func (as *AlertingSystem) AddAlertRule(rule *MLAlertRule) error {
	if rule == nil || rule.ID == "" {
		return fmt.Errorf("invalid alert rule")
	}

	as.mu.Lock()
	defer as.mu.Unlock()

	as.alertRules[rule.ID] = rule
	return nil
}

// RegisterAlertChannel registers an alert channel
func (as *AlertingSystem) RegisterAlertChannel(channel AlertChannel) error {
	if channel == nil {
		return fmt.Errorf("invalid alert channel")
	}

	as.mu.Lock()
	defer as.mu.Unlock()

	as.alertChannels[channel.Name()] = channel
	return nil
}

// AcknowledgeAlert acknowledges an alert
func (as *AlertingSystem) AcknowledgeAlert(alertID, acknowledgedBy string) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	for _, alert := range as.alerts {
		if alert.ID == alertID {
			alert.Acknowledged = true
			alert.AcknowledgedAt = time.Now()
			alert.AcknowledgedBy = acknowledgedBy
			return nil
		}
	}

	return fmt.Errorf("alert not found")
}

// ResolveAlert resolves an alert
func (as *AlertingSystem) ResolveAlert(alertID string) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	for _, alert := range as.alerts {
		if alert.ID == alertID {
			alert.Resolved = true
			alert.ResolvedAt = time.Now()
			return nil
		}
	}

	return fmt.Errorf("alert not found")
}

// GetAlerts returns active alerts
func (as *AlertingSystem) GetAlerts(limit int) []*Alert {
	as.mu.RLock()
	defer as.mu.RUnlock()

	// Filter unresolved alerts
	active := make([]*Alert, 0)
	for _, alert := range as.alerts {
		if !alert.Resolved {
			active = append(active, alert)
		}
	}

	if len(active) > limit {
		return active[:limit]
	}

	return active
}

// GetAlertHistory returns alert history for a metric
func (as *AlertingSystem) GetAlertHistory(metricName string, limit int) []*Alert {
	as.mu.RLock()
	defer as.mu.RUnlock()

	history, exists := as.alertHistory[metricName]
	if !exists {
		return nil
	}

	if len(history) > limit {
		return history[len(history)-limit:]
	}

	return history
}

// GetStats returns alerting system statistics
func (as *AlertingSystem) GetStats() map[string]interface{} {
	as.mu.RLock()
	defer as.mu.RUnlock()

	activeAlerts := 0
	for _, alert := range as.alerts {
		if !alert.Resolved {
			activeAlerts++
		}
	}

	return map[string]interface{}{
		"total_alerts":   len(as.alerts),
		"active_alerts":  activeAlerts,
		"alert_rules":    len(as.alertRules),
		"alert_channels": len(as.alertChannels),
		"alert_history":  len(as.alertHistory),
	}
}

// ClearAlerts clears all alerts
func (as *AlertingSystem) ClearAlerts() {
	as.mu.Lock()
	defer as.mu.Unlock()

	as.alerts = make([]*Alert, 0)
}

// ClearAlertHistory clears alert history
func (as *AlertingSystem) ClearAlertHistory() {
	as.mu.Lock()
	defer as.mu.Unlock()

	as.alertHistory = make(map[string][]*Alert)
}

// DisableAlertRule disables an alert rule
func (as *AlertingSystem) DisableAlertRule(ruleID string) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	rule, exists := as.alertRules[ruleID]
	if !exists {
		return fmt.Errorf("alert rule not found")
	}

	rule.Enabled = false
	return nil
}

// EnableAlertRule enables an alert rule
func (as *AlertingSystem) EnableAlertRule(ruleID string) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	rule, exists := as.alertRules[ruleID]
	if !exists {
		return fmt.Errorf("alert rule not found")
	}

	rule.Enabled = true
	return nil
}

// GetAlertRules returns all alert rules
func (as *AlertingSystem) GetAlertRules() map[string]*MLAlertRule {
	as.mu.RLock()
	defer as.mu.RUnlock()

	rules := make(map[string]*MLAlertRule)
	for k, v := range as.alertRules {
		rules[k] = v
	}

	return rules
}

// SimpleAlertChannel is a simple implementation of AlertChannel
type SimpleAlertChannel struct {
	name string
}

// NewSimpleAlertChannel creates a new simple alert channel
func NewSimpleAlertChannel(name string) *SimpleAlertChannel {
	return &SimpleAlertChannel{name: name}
}

// Send sends an alert
func (sac *SimpleAlertChannel) Send(alert *Alert) error {
	if alert == nil {
		return fmt.Errorf("invalid alert")
	}

	// In real implementation, would send to external service
	fmt.Printf("[%s] Alert: %s - %s (Severity: %s)\n", sac.name, alert.Title, alert.Description, alert.Severity)
	return nil
}

// Name returns channel name
func (sac *SimpleAlertChannel) Name() string {
	return sac.name
}
