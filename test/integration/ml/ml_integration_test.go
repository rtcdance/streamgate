package ml

import (
	"context"
	"testing"
	"time"

	"streamgate/pkg/ml"
)

func TestMLPipelineIntegration(t *testing.T) {
	// Create all ML components
	engine := ml.NewRecommendationEngine()
	detector := ml.NewAnomalyDetector()
	maintenance := ml.NewPredictiveMaintenance()
	optimization := ml.NewIntelligentOptimization()

	// Setup user profiles
	userProfile := &ml.UserProfile{
		UserID:        "user1",
		ViewedContent: []string{"content1", "content2", "content3"},
		Ratings:       map[string]float64{"content1": 5.0, "content2": 4.0, "content3": 3.0},
		Preferences:   make(map[string]float64),
	}

	err := engine.AddUserProfile(userProfile)
	if err != nil {
		t.Fatalf("Failed to add user profile: %v", err)
	}

	// Setup content profiles
	for i := 1; i <= 5; i++ {
		contentID := "content" + string(rune(48+i))
		profile := &ml.ContentProfile{
			ContentID:  contentID,
			Title:      "Content " + string(rune(48+i)),
			Category:   "test",
			Tags:       []string{"tag1", "tag2"},
			Features:   map[string]float64{"feature1": 0.5},
			Popularity: 0.8,
			ViewCount:  100 * int64(i),
			AvgRating:  4.0,
		}

		err := engine.AddContentProfile(profile)
		if err != nil {
			t.Fatalf("Failed to add content profile: %v", err)
		}
	}

	// Test recommendation flow
	recs, err := engine.GetRecommendations(context.Background(), "user1", 3)
	if err != nil {
		t.Logf("Recommendation error (expected for new user): %v", err)
	} else {
		t.Logf("Got %d recommendations", len(recs))
	}

	// Test anomaly detection flow
	for i := 0; i < 20; i++ {
		value := float64(i * 5)
		if i > 15 {
			value = 200.0 // Anomalous spike
		}
		err := detector.AddMetricValue("cpu_usage", value)
		if err != nil {
			t.Fatalf("Failed to add metric: %v", err)
		}
	}

	anomalies, err := detector.DetectAnomalies()
	if err != nil {
		t.Fatalf("Failed to detect anomalies: %v", err)
	}

	t.Logf("Detected %d anomalies", len(anomalies))

	// Test predictive maintenance flow
	metrics := &ml.ComponentMetrics{
		ComponentID: "component1",
		CPUUsage:    []float64{0.5, 0.6, 0.7, 0.8, 0.9},
		MemoryUsage: []float64{0.4, 0.5, 0.6, 0.7, 0.8},
		ErrorRate:   []float64{0.01, 0.02, 0.03, 0.04, 0.05},
	}

	err = maintenance.AddComponentMetrics("component1", metrics)
	if err != nil {
		t.Fatalf("Failed to add component metrics: %v", err)
	}

	predictions, err := maintenance.PredictFailures()
	if err != nil {
		t.Fatalf("Failed to predict failures: %v", err)
	}

	t.Logf("Got %d failure predictions", len(predictions))

	// Test optimization flow
	param := &ml.Parameter{
		Name:         "cache_size",
		CurrentValue: 100.0,
		MinValue:     10.0,
		MaxValue:     1000.0,
		StepSize:     10.0,
		ImpactScore:  0.8,
	}

	err = optimization.AddParameter(param)
	if err != nil {
		t.Fatalf("Failed to add parameter: %v", err)
	}

	opts, err := optimization.TuneParameters()
	if err != nil {
		t.Fatalf("Failed to tune parameters: %v", err)
	}

	t.Logf("Got %d optimizations", len(opts))

	// Verify all systems are working
	recStats := engine.GetStats()
	detectorStats := detector.GetStats()
	maintenanceStats := maintenance.GetStats()
	optimizationStats := optimization.GetStats()

	if recStats == nil || detectorStats == nil || maintenanceStats == nil || optimizationStats == nil {
		t.Fatal("Failed to get stats from one or more systems")
	}

	t.Logf("Recommendation stats: %+v", recStats)
	t.Logf("Detector stats: %+v", detectorStats)
	t.Logf("Maintenance stats: %+v", maintenanceStats)
	t.Logf("Optimization stats: %+v", optimizationStats)
}

func TestRecommendationWithFeedback(t *testing.T) {
	engine := ml.NewRecommendationEngine()
	cf := ml.NewCollaborativeFilter()

	// Add multiple users and ratings
	for u := 1; u <= 3; u++ {
		userID := "user" + string(rune(48+u))
		for c := 1; c <= 5; c++ {
			contentID := "content" + string(rune(48+c))
			rating := float64((u+c)%5 + 1)

			err := cf.AddRating(userID, contentID, rating)
			if err != nil {
				t.Fatalf("Failed to add rating: %v", err)
			}
		}
	}

	// Get recommendations
	recs := cf.GetRecommendations("user1", 3)
	t.Logf("Got %d recommendations", len(recs))

	// Record feedback
	if len(recs) > 0 {
		err := engine.RecordRecommendationFeedback("user1", recs[0].ContentID, true)
		if err != nil {
			t.Fatalf("Failed to record feedback: %v", err)
		}
	}

	// Check metrics
	metrics := engine.GetMetrics()
	if metrics == nil {
		t.Fatal("Failed to get metrics")
	}

	t.Logf("Metrics: %+v", metrics)
}

func TestAnomalyDetectionWithAlerting(t *testing.T) {
	detector := ml.NewAnomalyDetector()
	alerting := ml.NewAlertingSystem()

	// Register alert channel
	channel := ml.NewSimpleAlertChannel("test_channel")
	err := alerting.RegisterAlertChannel(channel)
	if err != nil {
		t.Fatalf("Failed to register alert channel: %v", err)
	}

	// Add alert rule
	rule := &ml.AlertRule{
		ID:               "rule1",
		Name:             "High CPU Alert",
		Condition:        "cpu_usage > 80",
		Threshold:        0.8,
		Severity:         "high",
		Enabled:          true,
		SuppressDuration: 5 * time.Minute,
		NotifyChannels:   []string{"test_channel"},
	}

	err = alerting.AddAlertRule(rule)
	if err != nil {
		t.Fatalf("Failed to add alert rule: %v", err)
	}

	// Add metrics and detect anomalies
	for i := 0; i < 15; i++ {
		value := float64(i * 6)
		if i > 12 {
			value = 95.0 // Anomalous spike
		}
		err := detector.AddMetricValue("cpu_usage", value)
		if err != nil {
			t.Fatalf("Failed to add metric: %v", err)
		}
	}

	anomalies, err := detector.DetectAnomalies()
	if err != nil {
		t.Fatalf("Failed to detect anomalies: %v", err)
	}

	// Generate alerts
	for _, anomaly := range anomalies {
		err := alerting.GenerateAlert(anomaly)
		if err != nil {
			t.Fatalf("Failed to generate alert: %v", err)
		}
	}

	// Get alerts
	alerts := alerting.GetAlerts(10)
	t.Logf("Generated %d alerts", len(alerts))

	// Acknowledge alert
	if len(alerts) > 0 {
		err := alerting.AcknowledgeAlert(alerts[0].ID, "admin")
		if err != nil {
			t.Fatalf("Failed to acknowledge alert: %v", err)
		}
	}

	// Get stats
	stats := alerting.GetStats()
	if stats == nil {
		t.Fatal("Failed to get stats")
	}

	t.Logf("Alerting stats: %+v", stats)
}

func TestPredictiveMaintenanceWorkflow(t *testing.T) {
	maintenance := ml.NewPredictiveMaintenance()

	// Add component metrics
	metrics := &ml.ComponentMetrics{
		ComponentID: "component1",
		CPUUsage:    []float64{0.5, 0.6, 0.7, 0.8, 0.9},
		MemoryUsage: []float64{0.4, 0.5, 0.6, 0.7, 0.8},
		ErrorRate:   []float64{0.01, 0.02, 0.03, 0.04, 0.05},
	}

	err := maintenance.AddComponentMetrics("component1", metrics)
	if err != nil {
		t.Fatalf("Failed to add component metrics: %v", err)
	}

	// Predict failures
	predictions, err := maintenance.PredictFailures()
	if err != nil {
		t.Fatalf("Failed to predict failures: %v", err)
	}

	t.Logf("Got %d predictions", len(predictions))

	// Execute prediction
	if len(predictions) > 0 {
		err := maintenance.ExecutePrediction(predictions[0].ID)
		if err != nil {
			t.Fatalf("Failed to execute prediction: %v", err)
		}

		// Record maintenance event
		event := &ml.MaintenanceEvent{
			ID:          "event1",
			ComponentID: "component1",
			EventType:   "preventive",
			Description: "Preventive maintenance based on prediction",
			StartTime:   time.Now(),
			EndTime:     time.Now().Add(1 * time.Hour),
			Duration:    1 * time.Hour,
			Success:     true,
			Cost:        100.0,
		}

		err = maintenance.RecordMaintenanceEvent(event)
		if err != nil {
			t.Fatalf("Failed to record maintenance event: %v", err)
		}
	}

	// Get maintenance history
	history := maintenance.GetMaintenanceHistory(10)
	t.Logf("Maintenance history: %d events", len(history))

	// Get stats
	stats := maintenance.GetStats()
	if stats == nil {
		t.Fatal("Failed to get stats")
	}

	t.Logf("Maintenance stats: %+v", stats)
}

func TestOptimizationWorkflow(t *testing.T) {
	optimization := ml.NewIntelligentOptimization()

	// Add parameters
	params := []*ml.Parameter{
		{
			Name:         "cache_size",
			CurrentValue: 100.0,
			MinValue:     10.0,
			MaxValue:     1000.0,
			StepSize:     10.0,
			ImpactScore:  0.8,
		},
		{
			Name:         "thread_pool_size",
			CurrentValue: 50.0,
			MinValue:     10.0,
			MaxValue:     200.0,
			StepSize:     5.0,
			ImpactScore:  0.7,
		},
	}

	for _, param := range params {
		err := optimization.AddParameter(param)
		if err != nil {
			t.Fatalf("Failed to add parameter: %v", err)
		}
	}

	// Tune parameters
	opts, err := optimization.TuneParameters()
	if err != nil {
		t.Fatalf("Failed to tune parameters: %v", err)
	}

	t.Logf("Got %d optimizations", len(opts))

	// Apply optimizations
	for _, opt := range opts {
		err := optimization.ApplyOptimization(opt.ID)
		if err != nil {
			t.Fatalf("Failed to apply optimization: %v", err)
		}
	}

	// Get stats
	stats := optimization.GetStats()
	if stats == nil {
		t.Fatal("Failed to get stats")
	}

	t.Logf("Optimization stats: %+v", stats)
}
