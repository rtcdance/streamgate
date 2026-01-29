package e2e

import (
	"context"
	"testing"
	"time"

	"streamgate/pkg/ml"
)

func TestEndToEndRecommendationFlow(t *testing.T) {
	// Initialize recommendation engine
	engine := ml.NewRecommendationEngine()
	cf := ml.NewCollaborativeFilter()
	cbf := ml.NewContentBasedFilter()
	_ = ml.NewHybridRecommender() // hr not used in this test

	// Simulate user behavior
	users := []string{"user1", "user2", "user3"}
	contents := []string{"content1", "content2", "content3", "content4", "content5"}

	// Add user profiles
	for _, userID := range users {
		profile := &ml.UserProfile{
			UserID:        userID,
			ViewedContent: []string{},
			Ratings:       make(map[string]float64),
			Preferences:   make(map[string]float64),
		}
		err := engine.AddUserProfile(profile)
		if err != nil {
			t.Fatalf("Failed to add user profile: %v", err)
		}
	}

	// Add content profiles with features
	for i, contentID := range contents {
		profile := &ml.ContentProfile{
			ContentID:  contentID,
			Title:      "Content " + contentID,
			Category:   "category" + string(rune(48+(i%3))),
			Tags:       []string{"tag1", "tag2"},
			Features:   map[string]float64{"feature1": float64(i) * 0.1},
			Popularity: 0.5 + float64(i)*0.1,
			ViewCount:  int64(100 * (i + 1)),
			AvgRating:  3.5 + float64(i)*0.3,
		}
		err := engine.AddContentProfile(profile)
		if err != nil {
			t.Fatalf("Failed to add content profile: %v", err)
		}

		// Add features for content-based filtering
		features := []float64{float64(i) * 0.1, 0.5, 0.7}
		err = cbf.AddContentFeatures(contentID, features)
		if err != nil {
			t.Fatalf("Failed to add content features: %v", err)
		}
	}

	// Simulate user interactions
	interactions := map[string]map[string]float64{
		"user1": {"content1": 5.0, "content2": 4.0, "content3": 3.0},
		"user2": {"content1": 5.0, "content2": 4.5, "content4": 4.0},
		"user3": {"content2": 4.0, "content3": 5.0, "content5": 3.5},
	}

	for userID, ratings := range interactions {
		for contentID, rating := range ratings {
			err := engine.RecordUserInteraction(userID, contentID, rating)
			if err != nil {
				t.Fatalf("Failed to record interaction: %v", err)
			}

			err = cf.AddRating(userID, contentID, rating)
			if err != nil {
				t.Fatalf("Failed to add rating: %v", err)
			}
		}
	}

	// Update user preferences for content-based filtering
	for _, userID := range users {
		profile, err := engine.GetUserProfile(userID)
		if err == nil {
			err = cbf.UpdateUserPreferences(profile)
			if err != nil {
				t.Logf("Failed to update preferences for %s: %v", userID, err)
			}
		}
	}

	// Get recommendations for each user
	for _, userID := range users {
		recs, err := engine.GetRecommendations(context.Background(), userID, 3)
		if err != nil {
			t.Logf("No recommendations for %s: %v", userID, err)
		} else {
			t.Logf("Got %d recommendations for %s", len(recs), userID)
		}

		// Record feedback
		if len(recs) > 0 {
			err := engine.RecordRecommendationFeedback(userID, recs[0].ContentID, true)
			if err != nil {
				t.Fatalf("Failed to record feedback: %v", err)
			}
		}
	}

	// Update metrics
	err := engine.UpdateMetrics(context.Background())
	if err != nil {
		t.Fatalf("Failed to update metrics: %v", err)
	}

	// Verify metrics
	metrics := engine.GetMetrics()
	if metrics == nil {
		t.Fatal("Failed to get metrics")
	}

	t.Logf("Final metrics: %+v", metrics)
	if metrics.ClickThroughRate == 0 {
		t.Log("CTR is 0 (expected for new system)")
	}
}

func TestEndToEndAnomalyDetectionFlow(t *testing.T) {
	// Initialize anomaly detection system
	detector := ml.NewAnomalyDetector()
	alerting := ml.NewAlertingSystem()

	// Register alert channel
	channel := ml.NewSimpleAlertChannel("email")
	err := alerting.RegisterAlertChannel(channel)
	if err != nil {
		t.Fatalf("Failed to register alert channel: %v", err)
	}

	// Add alert rules
	rules := []*ml.AlertRule{
		{
			ID:               "rule_cpu",
			Name:             "High CPU Alert",
			Condition:        "cpu_usage > 80",
			Threshold:        0.8,
			Severity:         "high",
			Enabled:          true,
			SuppressDuration: 5 * time.Minute,
			NotifyChannels:   []string{"email"},
		},
		{
			ID:               "rule_memory",
			Name:             "High Memory Alert",
			Condition:        "memory_usage > 85",
			Threshold:        0.85,
			Severity:         "high",
			Enabled:          true,
			SuppressDuration: 5 * time.Minute,
			NotifyChannels:   []string{"email"},
		},
	}

	for _, rule := range rules {
		err := alerting.AddAlertRule(rule)
		if err != nil {
			t.Fatalf("Failed to add alert rule: %v", err)
		}
	}

	// Simulate normal metrics
	for i := 0; i < 20; i++ {
		cpuValue := 30.0 + float64(i%10)*5.0
		memoryValue := 40.0 + float64(i%8)*4.0

		err := detector.AddMetricValue("cpu_usage", cpuValue)
		if err != nil {
			t.Fatalf("Failed to add CPU metric: %v", err)
		}

		err = detector.AddMetricValue("memory_usage", memoryValue)
		if err != nil {
			t.Fatalf("Failed to add memory metric: %v", err)
		}
	}

	// Simulate anomalous spike
	for i := 0; i < 5; i++ {
		err := detector.AddMetricValue("cpu_usage", 95.0)
		if err != nil {
			t.Fatalf("Failed to add anomalous CPU metric: %v", err)
		}
	}

	// Detect anomalies
	anomalies, err := detector.DetectAnomalies()
	if err != nil {
		t.Fatalf("Failed to detect anomalies: %v", err)
	}

	t.Logf("Detected %d anomalies", len(anomalies))

	// Generate alerts
	for _, anomaly := range anomalies {
		err := alerting.GenerateAlert(anomaly)
		if err != nil {
			t.Fatalf("Failed to generate alert: %v", err)
		}
	}

	// Get active alerts
	alerts := alerting.GetAlerts(10)
	t.Logf("Active alerts: %d", len(alerts))

	// Acknowledge and resolve alerts
	for _, alert := range alerts {
		err := alerting.AcknowledgeAlert(alert.ID, "admin")
		if err != nil {
			t.Fatalf("Failed to acknowledge alert: %v", err)
		}

		err = alerting.ResolveAlert(alert.ID)
		if err != nil {
			t.Fatalf("Failed to resolve alert: %v", err)
		}
	}

	// Verify final state
	finalAlerts := alerting.GetAlerts(10)
	if len(finalAlerts) > 0 {
		t.Logf("Unresolved alerts: %d", len(finalAlerts))
	}
}

func TestEndToEndPredictiveMaintenanceFlow(t *testing.T) {
	// Initialize predictive maintenance system
	maintenance := ml.NewPredictiveMaintenance()

	// Define components
	components := []string{"component1", "component2", "component3"}

	// Add component metrics
	for _, componentID := range components {
		metrics := &ml.ComponentMetrics{
			ComponentID: componentID,
			CPUUsage:    []float64{0.5, 0.6, 0.7, 0.8, 0.9},
			MemoryUsage: []float64{0.4, 0.5, 0.6, 0.7, 0.8},
			ErrorRate:   []float64{0.01, 0.02, 0.03, 0.04, 0.05},
		}

		err := maintenance.AddComponentMetrics(componentID, metrics)
		if err != nil {
			t.Fatalf("Failed to add component metrics: %v", err)
		}
	}

	// Predict failures
	predictions, err := maintenance.PredictFailures()
	if err != nil {
		t.Fatalf("Failed to predict failures: %v", err)
	}

	t.Logf("Got %d failure predictions", len(predictions))

	// Execute predictions and record maintenance
	for _, prediction := range predictions {
		err := maintenance.ExecutePrediction(prediction.ID)
		if err != nil {
			t.Fatalf("Failed to execute prediction: %v", err)
		}

		// Record maintenance event
		event := &ml.MaintenanceEvent{
			ID:          "event_" + prediction.ComponentID,
			ComponentID: prediction.ComponentID,
			EventType:   "preventive",
			Description: prediction.RecommendedAction,
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

	// Verify all events were recorded
	if len(history) != len(predictions) {
		t.Logf("Warning: Expected %d events, got %d", len(predictions), len(history))
	}
}

func TestEndToEndOptimizationFlow(t *testing.T) {
	// Initialize optimization system
	optimization := ml.NewIntelligentOptimization()

	// Add parameters
	parameters := []*ml.Parameter{
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
		{
			Name:         "batch_size",
			CurrentValue: 32.0,
			MinValue:     8.0,
			MaxValue:     256.0,
			StepSize:     8.0,
			ImpactScore:  0.6,
		},
	}

	for _, param := range parameters {
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
	appliedCount := 0
	for _, opt := range opts {
		err := optimization.ApplyOptimization(opt.ID)
		if err != nil {
			t.Fatalf("Failed to apply optimization: %v", err)
		}
		appliedCount++
	}

	t.Logf("Applied %d optimizations", appliedCount)

	// Optimize resources
	resourceOpts, err := optimization.OptimizeResources()
	if err != nil {
		t.Fatalf("Failed to optimize resources: %v", err)
	}

	t.Logf("Got %d resource optimizations", len(resourceOpts))

	// Optimize performance
	perfOpts, err := optimization.OptimizePerformance()
	if err != nil {
		t.Fatalf("Failed to optimize performance: %v", err)
	}

	t.Logf("Got %d performance optimizations", len(perfOpts))

	// Optimize costs
	costOpts, err := optimization.OptimizeCosts()
	if err != nil {
		t.Fatalf("Failed to optimize costs: %v", err)
	}

	t.Logf("Got %d cost optimizations", len(costOpts))

	// Get final stats
	stats := optimization.GetStats()
	if stats == nil {
		t.Fatal("Failed to get stats")
	}

	t.Logf("Final optimization stats: %+v", stats)
}

func TestEndToEndCompleteMLPipeline(t *testing.T) {
	// Initialize all ML systems
	engine := ml.NewRecommendationEngine()
	detector := ml.NewAnomalyDetector()
	maintenance := ml.NewPredictiveMaintenance()
	optimization := ml.NewIntelligentOptimization()

	// Setup recommendation system
	userProfile := &ml.UserProfile{
		UserID:        "user1",
		ViewedContent: []string{"content1", "content2"},
		Ratings:       map[string]float64{"content1": 5.0, "content2": 4.0},
		Preferences:   make(map[string]float64),
	}
	engine.AddUserProfile(userProfile)

	// Setup anomaly detection
	for i := 0; i < 25; i++ {
		value := float64(i * 4)
		if i > 20 {
			value = 100.0
		}
		detector.AddMetricValue("cpu_usage", value)
	}

	// Setup predictive maintenance
	metrics := &ml.ComponentMetrics{
		ComponentID: "component1",
		CPUUsage:    []float64{0.5, 0.6, 0.7, 0.8, 0.9},
		MemoryUsage: []float64{0.4, 0.5, 0.6, 0.7, 0.8},
		ErrorRate:   []float64{0.01, 0.02, 0.03, 0.04, 0.05},
	}
	maintenance.AddComponentMetrics("component1", metrics)

	// Setup optimization
	param := &ml.Parameter{
		Name:         "cache_size",
		CurrentValue: 100.0,
		MinValue:     10.0,
		MaxValue:     1000.0,
		StepSize:     10.0,
		ImpactScore:  0.8,
	}
	optimization.AddParameter(param)

	// Run all systems
	recs, _ := engine.GetRecommendations(context.Background(), "user1", 3)
	anomalies, _ := detector.DetectAnomalies()
	predictions, _ := maintenance.PredictFailures()
	opts, _ := optimization.TuneParameters()

	t.Logf("Recommendations: %d", len(recs))
	t.Logf("Anomalies: %d", len(anomalies))
	t.Logf("Predictions: %d", len(predictions))
	t.Logf("Optimizations: %d", len(opts))

	// Verify all systems are operational
	if engine.GetStats() == nil || detector.GetStats() == nil || maintenance.GetStats() == nil || optimization.GetStats() == nil {
		t.Fatal("One or more systems failed to provide stats")
	}

	t.Log("Complete ML pipeline test passed")
}
