package ml

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewAlertingSystem(t *testing.T) {
	as := NewAlertingSystem()
	require.NotNil(t, as)
	assert.Empty(t, as.alerts)
	assert.Empty(t, as.alertRules)
	assert.Empty(t, as.alertChannels)
}

func TestAlertingSystem_GenerateAlert_NilAnomaly(t *testing.T) {
	as := NewAlertingSystem()
	err := as.GenerateAlert(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid anomaly")
}

func TestAlertingSystem_GenerateAlert_ValidAnomaly(t *testing.T) {
	as := NewAlertingSystem()
	defer as.Close()

	anomaly := &Anomaly{
		ID:          "anom-1",
		Type:        "cpu_spike",
		Severity:    "critical",
		Description: "CPU usage exceeded 95%",
	}
	err := as.GenerateAlert(anomaly)
	require.NoError(t, err)

	alerts := as.GetAlerts(10)
	assert.Len(t, alerts, 1)
	assert.Equal(t, "anom-1", alerts[0].AnomalyID)
	assert.Equal(t, "critical", alerts[0].Severity)
	assert.Contains(t, alerts[0].Title, "cpu_spike")
	assert.Equal(t, "CPU usage exceeded 95%", alerts[0].Description)
}

func TestAlertingSystem_GenerateAlert_Suppressed(t *testing.T) {
	as := NewAlertingSystem()
	defer as.Close()

	rule := &MLAlertRule{
		ID:               "cpu_spike",
		Name:             "CPU Spike",
		Condition:        "cpu > 95",
		Threshold:        95.0,
		Severity:         "critical",
		Enabled:          true,
		SuppressDuration: 1 * time.Hour,
	}
	err := as.AddAlertRule(rule)
	require.NoError(t, err)

	anomaly := &Anomaly{
		ID:          "anom-1",
		Type:        "cpu_spike",
		Severity:    "critical",
		Description: "CPU spike",
	}
	err = as.GenerateAlert(anomaly)
	require.NoError(t, err)

	err = as.GenerateAlert(anomaly)
	require.NoError(t, err)

	alerts := as.GetAlerts(10)
	assert.Len(t, alerts, 1)
}

func TestAlertingSystem_GenerateAlert_SuppressionExpired(t *testing.T) {
	as := NewAlertingSystem()
	defer as.Close()

	rule := &MLAlertRule{
		ID:               "cpu_spike",
		Name:             "CPU Spike",
		SuppressDuration: 1 * time.Nanosecond,
		Enabled:          true,
	}
	err := as.AddAlertRule(rule)
	require.NoError(t, err)

	anomaly := &Anomaly{
		ID:       "anom-1",
		Type:     "cpu_spike",
		Severity: "critical",
	}
	err = as.GenerateAlert(anomaly)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	err = as.GenerateAlert(anomaly)
	require.NoError(t, err)

	alerts := as.GetAlerts(10)
	assert.Len(t, alerts, 2)
}

func TestAlertingSystem_GenerateAlert_History(t *testing.T) {
	as := NewAlertingSystem()
	defer as.Close()

	anomaly := &Anomaly{
		ID:       "anom-1",
		Type:     "latency",
		Severity: "warning",
	}
	err := as.GenerateAlert(anomaly)
	require.NoError(t, err)

	history := as.GetAlertHistory("latency", 10)
	assert.Len(t, history, 1)
	assert.Equal(t, "anom-1", history[0].AnomalyID)
}

func TestAlertingSystem_GenerateAlert_NoRuleNoSuppression(t *testing.T) {
	as := NewAlertingSystem()
	defer as.Close()

	anomaly := &Anomaly{
		ID:       "anom-1",
		Type:     "disk_full",
		Severity: "critical",
	}
	err := as.GenerateAlert(anomaly)
	require.NoError(t, err)

	anomaly2 := &Anomaly{
		ID:       "anom-2",
		Type:     "disk_full",
		Severity: "critical",
	}
	err = as.GenerateAlert(anomaly2)
	require.NoError(t, err)

	alerts := as.GetAlerts(10)
	assert.Len(t, alerts, 2)
}

func TestAlertingSystem_AddAlertRule_NilRule(t *testing.T) {
	as := NewAlertingSystem()
	err := as.AddAlertRule(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid alert rule")
}

func TestAlertingSystem_AddAlertRule_EmptyID(t *testing.T) {
	as := NewAlertingSystem()
	err := as.AddAlertRule(&MLAlertRule{Name: "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid alert rule")
}

func TestAlertingSystem_AddAlertRule_Valid(t *testing.T) {
	as := NewAlertingSystem()
	rule := &MLAlertRule{
		ID:        "rule-1",
		Name:      "Test Rule",
		Condition: "metric > 100",
		Threshold: 100.0,
		Severity:  "warning",
		Enabled:   true,
	}
	err := as.AddAlertRule(rule)
	require.NoError(t, err)

	rules := as.GetAlertRules()
	assert.Len(t, rules, 1)
	assert.Equal(t, "Test Rule", rules["rule-1"].Name)
}

func TestAlertingSystem_RegisterAlertChannel_Nil(t *testing.T) {
	as := NewAlertingSystem()
	err := as.RegisterAlertChannel(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid alert channel")
}

func TestAlertingSystem_RegisterAlertChannel_Valid(t *testing.T) {
	as := NewAlertingSystem()
	ch := NewSimpleAlertChannel("email")
	err := as.RegisterAlertChannel(ch)
	require.NoError(t, err)

	stats := as.GetStats()
	assert.Equal(t, 1, stats["alert_channels"])
}

type mockAlertChannel struct {
	name    string
	sendErr error
	called  atomic.Int32
}

func (m *mockAlertChannel) Send(alert *Alert) error {
	m.called.Add(1)
	return m.sendErr
}

func (m *mockAlertChannel) Name() string {
	return m.name
}

func TestAlertingSystem_GenerateAlert_SendsToChannels(t *testing.T) {
	as := NewAlertingSystem()
	defer as.Close()

	ch := &mockAlertChannel{name: "slack"}
	err := as.RegisterAlertChannel(ch)
	require.NoError(t, err)

	anomaly := &Anomaly{
		ID:       "anom-1",
		Type:     "cpu_spike",
		Severity: "critical",
	}
	err = as.GenerateAlert(anomaly)
	require.NoError(t, err)

	as.Close()
	assert.Equal(t, int32(1), ch.called.Load())
}

func TestAlertingSystem_GenerateAlert_ChannelError(t *testing.T) {
	as := NewAlertingSystem()
	as.log = zap.NewNop()
	defer as.Close()

	ch := &mockAlertChannel{name: "failing", sendErr: assert.AnError}
	err := as.RegisterAlertChannel(ch)
	require.NoError(t, err)

	anomaly := &Anomaly{
		ID:       "anom-1",
		Type:     "cpu_spike",
		Severity: "critical",
	}
	err = as.GenerateAlert(anomaly)
	require.NoError(t, err)

	as.Close()
	assert.Equal(t, int32(1), ch.called.Load())
}

func TestAlertingSystem_AcknowledgeAlert_Found(t *testing.T) {
	as := NewAlertingSystem()
	defer as.Close()

	anomaly := &Anomaly{
		ID:       "anom-1",
		Type:     "cpu_spike",
		Severity: "critical",
	}
	err := as.GenerateAlert(anomaly)
	require.NoError(t, err)

	alerts := as.GetAlerts(10)
	require.Len(t, alerts, 1)

	err = as.AcknowledgeAlert(alerts[0].ID, "admin")
	require.NoError(t, err)

	alerts = as.GetAlerts(10)
	assert.True(t, alerts[0].Acknowledged)
	assert.Equal(t, "admin", alerts[0].AcknowledgedBy)
}

func TestAlertingSystem_AcknowledgeAlert_NotFound(t *testing.T) {
	as := NewAlertingSystem()
	err := as.AcknowledgeAlert("nonexistent", "admin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "alert not found")
}

func TestAlertingSystem_ResolveAlert_Found(t *testing.T) {
	as := NewAlertingSystem()
	defer as.Close()

	anomaly := &Anomaly{
		ID:       "anom-1",
		Type:     "cpu_spike",
		Severity: "critical",
	}
	err := as.GenerateAlert(anomaly)
	require.NoError(t, err)

	alerts := as.GetAlerts(10)
	require.Len(t, alerts, 1)

	err = as.ResolveAlert(alerts[0].ID)
	require.NoError(t, err)

	alerts = as.GetAlerts(10)
	assert.Empty(t, alerts)
}

func TestAlertingSystem_ResolveAlert_NotFound(t *testing.T) {
	as := NewAlertingSystem()
	err := as.ResolveAlert("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "alert not found")
}

func TestAlertingSystem_GetAlerts_Limit(t *testing.T) {
	as := NewAlertingSystem()
	defer as.Close()

	for i := 0; i < 5; i++ {
		anomaly := &Anomaly{
			ID:       "anom-" + string(rune('0'+i)),
			Type:     "cpu_spike",
			Severity: "warning",
		}
		err := as.GenerateAlert(anomaly)
		require.NoError(t, err)
	}

	alerts := as.GetAlerts(3)
	assert.Len(t, alerts, 3)
}

func TestAlertingSystem_GetAlerts_ExcludesResolved(t *testing.T) {
	as := NewAlertingSystem()
	defer as.Close()

	anomaly1 := &Anomaly{ID: "anom-1", Type: "cpu", Severity: "warning"}
	anomaly2 := &Anomaly{ID: "anom-2", Type: "mem", Severity: "warning"}
	err := as.GenerateAlert(anomaly1)
	require.NoError(t, err)
	err = as.GenerateAlert(anomaly2)
	require.NoError(t, err)

	alerts := as.GetAlerts(10)
	require.Len(t, alerts, 2)

	err = as.ResolveAlert(alerts[0].ID)
	require.NoError(t, err)

	alerts = as.GetAlerts(10)
	assert.Len(t, alerts, 1)
}

func TestAlertingSystem_GetAlertHistory_NotFound(t *testing.T) {
	as := NewAlertingSystem()
	history := as.GetAlertHistory("nonexistent", 10)
	assert.Nil(t, history)
}

func TestAlertingSystem_GetAlertHistory_Limit(t *testing.T) {
	as := NewAlertingSystem()
	defer as.Close()

	for i := 0; i < 5; i++ {
		anomaly := &Anomaly{
			ID:       "anom-" + string(rune('0'+i)),
			Type:     "latency",
			Severity: "warning",
		}
		err := as.GenerateAlert(anomaly)
		require.NoError(t, err)
	}

	history := as.GetAlertHistory("latency", 3)
	assert.Len(t, history, 3)
}

func TestAlertingSystem_GetStats(t *testing.T) {
	as := NewAlertingSystem()
	defer as.Close()

	anomaly := &Anomaly{ID: "anom-1", Type: "cpu", Severity: "warning"}
	err := as.GenerateAlert(anomaly)
	require.NoError(t, err)

	stats := as.GetStats()
	assert.Equal(t, 1, stats["total_alerts"])
	assert.Equal(t, 1, stats["active_alerts"])
	assert.Equal(t, 0, stats["alert_rules"])
	assert.Equal(t, 0, stats["alert_channels"])
	assert.Equal(t, 1, stats["alert_history"])
}

func TestAlertingSystem_ClearAlerts(t *testing.T) {
	as := NewAlertingSystem()
	defer as.Close()

	anomaly := &Anomaly{ID: "anom-1", Type: "cpu", Severity: "warning"}
	err := as.GenerateAlert(anomaly)
	require.NoError(t, err)

	as.ClearAlerts()
	alerts := as.GetAlerts(10)
	assert.Empty(t, alerts)
}

func TestAlertingSystem_ClearAlertHistory(t *testing.T) {
	as := NewAlertingSystem()
	defer as.Close()

	anomaly := &Anomaly{ID: "anom-1", Type: "cpu", Severity: "warning"}
	err := as.GenerateAlert(anomaly)
	require.NoError(t, err)

	as.ClearAlertHistory()
	history := as.GetAlertHistory("cpu", 10)
	assert.Nil(t, history)
}

func TestAlertingSystem_DisableAlertRule(t *testing.T) {
	as := NewAlertingSystem()
	rule := &MLAlertRule{ID: "rule-1", Name: "Test", Enabled: true}
	err := as.AddAlertRule(rule)
	require.NoError(t, err)

	err = as.DisableAlertRule("rule-1")
	require.NoError(t, err)

	rules := as.GetAlertRules()
	assert.False(t, rules["rule-1"].Enabled)
}

func TestAlertingSystem_DisableAlertRule_NotFound(t *testing.T) {
	as := NewAlertingSystem()
	err := as.DisableAlertRule("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "alert rule not found")
}

func TestAlertingSystem_EnableAlertRule(t *testing.T) {
	as := NewAlertingSystem()
	rule := &MLAlertRule{ID: "rule-1", Name: "Test", Enabled: false}
	err := as.AddAlertRule(rule)
	require.NoError(t, err)

	err = as.EnableAlertRule("rule-1")
	require.NoError(t, err)

	rules := as.GetAlertRules()
	assert.True(t, rules["rule-1"].Enabled)
}

func TestAlertingSystem_EnableAlertRule_NotFound(t *testing.T) {
	as := NewAlertingSystem()
	err := as.EnableAlertRule("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "alert rule not found")
}

func TestAlertingSystem_GetAlertRules_Copy(t *testing.T) {
	as := NewAlertingSystem()
	rule := &MLAlertRule{ID: "rule-1", Name: "Test"}
	err := as.AddAlertRule(rule)
	require.NoError(t, err)

	rules := as.GetAlertRules()
	rules["rule-2"] = &MLAlertRule{ID: "rule-2", Name: "Other"}

	originalRules := as.GetAlertRules()
	assert.Len(t, originalRules, 1)
}

func TestSimpleAlertChannel_Send(t *testing.T) {
	ch := NewSimpleAlertChannel("email")
	assert.Equal(t, "email", ch.Name())

	alert := &Alert{
		ID:          "alert-1",
		Title:       "Test Alert",
		Description: "Test description",
		Severity:    "warning",
	}
	err := ch.Send(alert)
	assert.NoError(t, err)
}

func TestSimpleAlertChannel_Send_NilAlert(t *testing.T) {
	ch := NewSimpleAlertChannel("email")
	err := ch.Send(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid alert")
}

func TestAnomaly_Fields(t *testing.T) {
	now := time.Now()
	a := &Anomaly{
		ID:             "anom-1",
		Type:           "cpu_spike",
		Severity:       "critical",
		Score:          0.95,
		Timestamp:      now,
		Description:    "CPU exceeded threshold",
		RootCause:      "Runaway process",
		Recommendation: "Kill process PID 1234",
	}
	assert.Equal(t, "anom-1", a.ID)
	assert.Equal(t, "cpu_spike", a.Type)
	assert.Equal(t, "critical", a.Severity)
	assert.Equal(t, 0.95, a.Score)
	assert.Equal(t, now, a.Timestamp)
	assert.Equal(t, "CPU exceeded threshold", a.Description)
	assert.Equal(t, "Runaway process", a.RootCause)
	assert.Equal(t, "Kill process PID 1234", a.Recommendation)
}

func TestAlert_Fields(t *testing.T) {
	now := time.Now()
	a := &Alert{
		ID:             "alert-1",
		AnomalyID:      "anom-1",
		Severity:       "critical",
		Title:          "CPU Spike",
		Description:    "CPU at 99%",
		Timestamp:      now,
		Acknowledged:   true,
		AcknowledgedAt: now,
		AcknowledgedBy: "admin",
		Resolved:       false,
	}
	assert.Equal(t, "alert-1", a.ID)
	assert.Equal(t, "anom-1", a.AnomalyID)
	assert.Equal(t, "critical", a.Severity)
	assert.True(t, a.Acknowledged)
	assert.Equal(t, "admin", a.AcknowledgedBy)
	assert.False(t, a.Resolved)
}

func TestMLAlertRule_Fields(t *testing.T) {
	rule := &MLAlertRule{
		ID:               "rule-1",
		Name:             "CPU Alert",
		Condition:        "cpu > 95",
		Threshold:        95.0,
		Severity:         "critical",
		Enabled:          true,
		SuppressDuration: 30 * time.Minute,
		NotifyChannels:   []string{"slack", "email"},
	}
	assert.Equal(t, "rule-1", rule.ID)
	assert.Equal(t, "CPU Alert", rule.Name)
	assert.Equal(t, "cpu > 95", rule.Condition)
	assert.Equal(t, 95.0, rule.Threshold)
	assert.Equal(t, "critical", rule.Severity)
	assert.True(t, rule.Enabled)
	assert.Equal(t, 30*time.Minute, rule.SuppressDuration)
	assert.Len(t, rule.NotifyChannels, 2)
}

func TestAlertingSystem_MultipleChannels(t *testing.T) {
	as := NewAlertingSystem()
	defer as.Close()

	ch1 := &mockAlertChannel{name: "slack"}
	ch2 := &mockAlertChannel{name: "email"}
	err := as.RegisterAlertChannel(ch1)
	require.NoError(t, err)
	err = as.RegisterAlertChannel(ch2)
	require.NoError(t, err)

	anomaly := &Anomaly{ID: "anom-1", Type: "cpu", Severity: "critical"}
	err = as.GenerateAlert(anomaly)
	require.NoError(t, err)

	as.Close()
	assert.Equal(t, int32(1), ch1.called.Load())
	assert.Equal(t, int32(1), ch2.called.Load())
}

func TestAlertingSystem_Close_WaitsForChannels(t *testing.T) {
	as := NewAlertingSystem()

	ch := &mockAlertChannel{name: "slow"}
	err := as.RegisterAlertChannel(ch)
	require.NoError(t, err)

	anomaly := &Anomaly{ID: "anom-1", Type: "cpu", Severity: "critical"}
	err = as.GenerateAlert(anomaly)
	require.NoError(t, err)

	as.Close()
	assert.Equal(t, int32(1), ch.called.Load())
}
