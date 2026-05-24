package monitoring

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewAlertManager(t *testing.T) {
	am := NewAlertManager(zap.NewNop())
	assert.NotNil(t, am)
	assert.NotNil(t, am.alerts)
	assert.NotNil(t, am.rules)
}

func TestAlertManager_AddRule(t *testing.T) {
	am := NewAlertManager(zap.NewNop())

	rule := &AlertRule{
		ID:        "rule-1",
		Name:      "HighCPU",
		Metric:    "cpu_usage",
		Condition: "gt",
		Threshold: 80,
		Level:     "warning",
		Enabled:   true,
	}
	am.AddRule(rule)

	assert.Contains(t, am.rules, "rule-1")
}

func TestAlertManager_RemoveRule(t *testing.T) {
	am := NewAlertManager(zap.NewNop())

	am.AddRule(&AlertRule{ID: "rule-1", Name: "Test"})
	am.RemoveRule("rule-1")

	assert.NotContains(t, am.rules, "rule-1")
}

func TestAlertManager_RegisterHandler(t *testing.T) {
	am := NewAlertManager(zap.NewNop())

	am.RegisterHandler(func(alert *Alert) error {
		return nil
	})

	assert.Len(t, am.handlers, 1)
}

func TestAlertManager_CheckMetric_TriggersAlert(t *testing.T) {
	am := NewAlertManager(zap.NewNop())

	var receivedAlert *Alert
	am.RegisterHandler(func(alert *Alert) error {
		receivedAlert = alert
		return nil
	})

	am.AddRule(&AlertRule{
		ID:        "cpu-high",
		Name:      "HighCPU",
		Metric:    "cpu_usage",
		Condition: "gt",
		Threshold: 80,
		Level:     "warning",
		Enabled:   true,
	})

	am.CheckMetric("cpu_usage", 95.0)

	require.NotNil(t, receivedAlert)
	assert.Equal(t, "HighCPU", receivedAlert.Title)
	assert.Equal(t, "warning", receivedAlert.Level)
	assert.Equal(t, "active", receivedAlert.Status)
	assert.Equal(t, 95.0, receivedAlert.Value)
	assert.Equal(t, 80.0, receivedAlert.Threshold)
}

func TestAlertManager_CheckMetric_DoesNotTriggerBelowThreshold(t *testing.T) {
	am := NewAlertManager(zap.NewNop())

	alertTriggered := false
	am.RegisterHandler(func(alert *Alert) error {
		alertTriggered = true
		return nil
	})

	am.AddRule(&AlertRule{
		ID:        "cpu-high",
		Name:      "HighCPU",
		Metric:    "cpu_usage",
		Condition: "gt",
		Threshold: 80,
		Level:     "warning",
		Enabled:   true,
	})

	am.CheckMetric("cpu_usage", 50.0)

	assert.False(t, alertTriggered)
}

func TestAlertManager_shouldTriggerAlert_Conditions(t *testing.T) {
	am := NewAlertManager(zap.NewNop())

	tests := []struct {
		name      string
		condition string
		threshold float64
		value     float64
		expected  bool
	}{
		{"gt triggers", "gt", 80, 90, true},
		{"gt no trigger", "gt", 80, 70, false},
		{"lt triggers", "lt", 20, 10, true},
		{"lt no trigger", "lt", 20, 30, false},
		{"eq triggers", "eq", 42, 42, true},
		{"eq no trigger", "eq", 42, 43, false},
		{"gte triggers equal", "gte", 80, 80, true},
		{"gte triggers greater", "gte", 80, 90, true},
		{"gte no trigger", "gte", 80, 70, false},
		{"lte triggers equal", "lte", 80, 80, true},
		{"lte triggers less", "lte", 80, 70, true},
		{"lte no trigger", "lte", 80, 90, false},
		{"unknown condition", "unknown", 80, 90, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rule := &AlertRule{Condition: tc.condition, Threshold: tc.threshold}
			result := am.shouldTriggerAlert(rule, tc.value)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestAlertManager_CheckMetric_DisabledRule(t *testing.T) {
	am := NewAlertManager(zap.NewNop())

	alertTriggered := false
	am.RegisterHandler(func(alert *Alert) error {
		alertTriggered = true
		return nil
	})

	am.AddRule(&AlertRule{
		ID:        "disabled-rule",
		Name:      "Disabled",
		Metric:    "cpu_usage",
		Condition: "gt",
		Threshold: 80,
		Enabled:   false,
	})

	am.CheckMetric("cpu_usage", 95.0)
	assert.False(t, alertTriggered)
}

func TestAlertManager_CheckMetric_DifferentMetric(t *testing.T) {
	am := NewAlertManager(zap.NewNop())

	alertTriggered := false
	am.RegisterHandler(func(alert *Alert) error {
		alertTriggered = true
		return nil
	})

	am.AddRule(&AlertRule{
		ID:        "cpu-high",
		Name:      "HighCPU",
		Metric:    "cpu_usage",
		Condition: "gt",
		Threshold: 80,
		Enabled:   true,
	})

	am.CheckMetric("memory_usage", 95.0)
	assert.False(t, alertTriggered)
}

func TestAlertManager_ResolveAlert(t *testing.T) {
	am := NewAlertManager(zap.NewNop())

	am.AddRule(&AlertRule{
		ID:        "cpu-high",
		Name:      "HighCPU",
		Metric:    "cpu_usage",
		Condition: "gt",
		Threshold: 80,
		Level:     "warning",
		Enabled:   true,
	})

	am.CheckMetric("cpu_usage", 95.0)

	activeAlerts := am.GetActiveAlerts()
	require.Len(t, activeAlerts, 1)

	alertID := activeAlerts[0].ID
	am.ResolveAlert(alertID)

	alert := am.GetAlert(alertID)
	require.NotNil(t, alert)
	assert.Equal(t, "resolved", alert.Status)
	assert.NotNil(t, alert.ResolvedAt)
}

func TestAlertManager_ResolveAlert_NonExistent(t *testing.T) {
	am := NewAlertManager(zap.NewNop())
	am.ResolveAlert("nonexistent")
}

func TestAlertManager_GetActiveAlerts(t *testing.T) {
	am := NewAlertManager(zap.NewNop())

	am.AddRule(&AlertRule{ID: "r1", Name: "Alert1", Metric: "m1", Condition: "gt", Threshold: 10, Level: "warning", Enabled: true})
	am.AddRule(&AlertRule{ID: "r2", Name: "Alert2", Metric: "m2", Condition: "gt", Threshold: 10, Level: "critical", Enabled: true})

	am.CheckMetric("m1", 20.0)
	am.CheckMetric("m2", 20.0)

	active := am.GetActiveAlerts()
	assert.Len(t, active, 2)
}

func TestAlertManager_GetAlertsByLevel(t *testing.T) {
	am := NewAlertManager(zap.NewNop())

	am.AddRule(&AlertRule{ID: "r1", Name: "Warning", Metric: "m1", Condition: "gt", Threshold: 10, Level: "warning", Enabled: true})
	am.AddRule(&AlertRule{ID: "r2", Name: "Critical", Metric: "m2", Condition: "gt", Threshold: 10, Level: "critical", Enabled: true})

	am.CheckMetric("m1", 20.0)
	am.CheckMetric("m2", 20.0)

	warnings := am.GetAlertsByLevel("warning")
	assert.Len(t, warnings, 1)

	criticals := am.GetAlertsByLevel("critical")
	assert.Len(t, criticals, 1)
}

func TestAlertManager_GetAlertCount(t *testing.T) {
	am := NewAlertManager(zap.NewNop())

	am.AddRule(&AlertRule{ID: "r1", Name: "Warning", Metric: "m1", Condition: "gt", Threshold: 10, Level: "warning", Enabled: true})
	am.AddRule(&AlertRule{ID: "r2", Name: "Critical", Metric: "m2", Condition: "gt", Threshold: 10, Level: "critical", Enabled: true})

	am.CheckMetric("m1", 20.0)
	am.CheckMetric("m2", 20.0)

	counts := am.GetAlertCount()
	assert.Equal(t, 2, counts["total"])
	assert.Equal(t, 2, counts["active"])
	assert.Equal(t, 0, counts["resolved"])
	assert.Equal(t, 1, counts["warning"])
	assert.Equal(t, 1, counts["critical"])
}

func TestAlertManager_HandlerError(t *testing.T) {
	am := NewAlertManager(zap.NewNop())

	am.RegisterHandler(func(alert *Alert) error {
		return assert.AnError
	})

	am.AddRule(&AlertRule{ID: "r1", Name: "Test", Metric: "m1", Condition: "gt", Threshold: 10, Level: "warning", Enabled: true})

	am.CheckMetric("m1", 20.0)

	active := am.GetActiveAlerts()
	assert.Len(t, active, 1)
}

func TestNewHealthChecker(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	assert.NotNil(t, hc)
	assert.NotNil(t, hc.checks)
}

func TestHealthChecker_RegisterCheck(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())

	hc.RegisterCheck("database", func() (bool, string, error) {
		return true, "ok", nil
	})

	assert.Contains(t, hc.checks, "database")
}

func TestHealthChecker_Check_AllHealthy(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())

	hc.RegisterCheck("db", func() (bool, string, error) {
		return true, "connected", nil
	})
	hc.RegisterCheck("redis", func() (bool, string, error) {
		return true, "connected", nil
	})

	status := hc.Check()

	assert.Equal(t, "healthy", status.Status)
	assert.Len(t, status.Checks, 2)
	assert.True(t, status.Checks["db"].Status)
	assert.True(t, status.Checks["redis"].Status)
}

func TestHealthChecker_Check_Degraded(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())

	hc.RegisterCheck("db", func() (bool, string, error) {
		return true, "connected", nil
	})
	hc.RegisterCheck("cache", func() (bool, string, error) {
		return false, "degraded", nil
	})

	status := hc.Check()

	assert.Equal(t, "degraded", status.Status)
	assert.False(t, status.Checks["cache"].Status)
}

func TestHealthChecker_Check_Unhealthy(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())

	hc.RegisterCheck("db", func() (bool, string, error) {
		return true, "ok", nil
	})
	hc.RegisterCheck("queue", func() (bool, string, error) {
		return true, "connection refused", assert.AnError
	})

	status := hc.Check()

	assert.Equal(t, "unhealthy", status.Status)
	assert.NotEmpty(t, status.Checks["queue"].Error)
}

func TestHealthChecker_GetStatus(t *testing.T) {
	hc := NewHealthChecker(zap.NewNop())
	hc.RegisterCheck("db", func() (bool, string, error) {
		return true, "ok", nil
	})

	status := hc.GetStatus()
	assert.Equal(t, "healthy", status.Status)
}
