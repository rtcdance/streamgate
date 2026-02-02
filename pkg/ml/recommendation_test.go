package ml

import (
	"context"
	"testing"
	"time"
)

func TestRecommendationEngine(t *testing.T) {
	engine := NewRecommendationEngine()

	// Test adding user profile
	userProfile := &UserProfile{
		UserID:        "user1",
		ViewedContent: []string{"content1", "content2"},
		Ratings:       map[string]float64{"content1": 4.5, "content2": 3.5},
		Preferences:   make(map[string]float64),
	}

	err := engine.AddUserProfile(userProfile)
	if err != nil {
		t.Fatalf("Failed to add user profile: %v", err)
	}

	// Test adding content profile
	contentProfile := &ContentProfile{
		ContentID:  "content1",
		Title:      "Test Content",
		Category:   "test",
		Tags:       []string{"tag1", "tag2"},
		Features:   map[string]float64{"feature1": 0.5},
		Popularity: 0.8,
		ViewCount:  100,
		AvgRating:  4.0,
	}

	err = engine.AddContentProfile(contentProfile)
	if err != nil {
		t.Fatalf("Failed to add content profile: %v", err)
	}

	// Test recording user interaction
	err = engine.RecordUserInteraction("user1", "content1", 4.5)
	if err != nil {
		t.Fatalf("Failed to record user interaction: %v", err)
	}

	// Test getting recommendations
	_, err = engine.GetRecommendations(context.Background(), "user1", 5)
	if err != nil {
		t.Logf("No recommendations available (expected for new user): %v", err)
	}

	// Test metrics
	metrics := engine.GetMetrics()
	if metrics == nil {
		t.Fatal("Failed to get metrics")
	}

	// Test stats
	stats := engine.GetStats()
	if stats == nil {
		t.Fatal("Failed to get stats")
	}

	t.Logf("Stats: %+v", stats)
}

func TestCollaborativeFiltering(t *testing.T) {
	cf := NewCollaborativeFilter()

	// Add ratings
	err := cf.AddRating("user1", "content1", 5.0)
	if err != nil {
		t.Fatalf("Failed to add rating: %v", err)
	}

	err = cf.AddRating("user1", "content2", 4.0)
	if err != nil {
		t.Fatalf("Failed to add rating: %v", err)
	}

	err = cf.AddRating("user2", "content1", 5.0)
	if err != nil {
		t.Fatalf("Failed to add rating: %v", err)
	}

	err = cf.AddRating("user2", "content2", 4.0)
	if err != nil {
		t.Fatalf("Failed to add rating: %v", err)
	}

	// Get recommendations
	recs := cf.GetRecommendations("user1", 5)
	if recs == nil {
		t.Log("No recommendations available")
	}

	// Get user similarity
	similarity, err := cf.GetUserSimilarity("user1", "user2")
	if err != nil {
		t.Logf("Failed to get user similarity: %v", err)
	} else {
		t.Logf("User similarity: %.2f", similarity)
	}

	// Get stats
	stats := cf.GetStats()
	if stats == nil {
		t.Fatal("Failed to get stats")
	}

	t.Logf("Stats: %+v", stats)
}

func TestContentBasedFiltering(t *testing.T) {
	cbf := NewContentBasedFilter()

	// Add content features
	features1 := []float64{0.5, 0.6, 0.7}
	err := cbf.AddContentFeatures("content1", features1)
	if err != nil {
		t.Fatalf("Failed to add content features: %v", err)
	}

	features2 := []float64{0.5, 0.6, 0.8}
	err = cbf.AddContentFeatures("content2", features2)
	if err != nil {
		t.Fatalf("Failed to add content features: %v", err)
	}

	// Update user preferences
	userProfile := &UserProfile{
		UserID:        "user1",
		ViewedContent: []string{"content1"},
		Ratings:       map[string]float64{"content1": 4.5},
		Preferences:   make(map[string]float64),
	}

	err = cbf.UpdateUserPreferences(userProfile)
	if err != nil {
		t.Logf("Failed to update user preferences: %v", err)
	}

	// Get recommendations
	recs := cbf.GetRecommendations(userProfile, 5)
	if recs == nil {
		t.Log("No recommendations available")
	}

	// Get content similarity
	similarity, err := cbf.GetContentSimilarity("content1", "content2")
	if err != nil {
		t.Logf("Failed to get content similarity: %v", err)
	} else {
		t.Logf("Content similarity: %.2f", similarity)
	}

	// Get stats
	stats := cbf.GetStats()
	if stats == nil {
		t.Fatal("Failed to get stats")
	}

	t.Logf("Stats: %+v", stats)
}

func TestHybridRecommender(t *testing.T) {
	hr := NewHybridRecommender()

	// Add trending content
	err := hr.AddTrendingContent("content1", 0.9)
	if err != nil {
		t.Fatalf("Failed to add trending content: %v", err)
	}

	// Get trending recommendations
	recs := hr.GetTrendingRecommendations(5)
	if len(recs) == 0 {
		t.Log("No trending recommendations")
	}

	// Add personalized score
	err = hr.AddPersonalizedScore("user1", "content1", 0.8)
	if err != nil {
		t.Fatalf("Failed to add personalized score: %v", err)
	}

	// Get personalized recommendations
	recs = hr.GetPersonalizedRecommendations("user1", 5)
	if len(recs) == 0 {
		t.Log("No personalized recommendations")
	}

	// Set weights
	err = hr.SetWeights(0.4, 0.4, 0.1, 0.1)
	if err != nil {
		t.Fatalf("Failed to set weights: %v", err)
	}

	// Get weights
	weights := hr.GetWeights()
	if weights == nil {
		t.Fatal("Failed to get weights")
	}

	t.Logf("Weights: %+v", weights)
}

func TestAnomalyDetector(t *testing.T) {
	ad := NewAnomalyDetector()

	// Add metric values
	for i := 0; i < 10; i++ {
		err := ad.AddMetricValue("cpu_usage", float64(i*10))
		if err != nil {
			t.Fatalf("Failed to add metric value: %v", err)
		}
	}

	// Add anomalous value
	err := ad.AddMetricValue("cpu_usage", 95.0)
	if err != nil {
		t.Fatalf("Failed to add anomalous metric value: %v", err)
	}

	// Detect anomalies
	anomalies, err := ad.DetectAnomalies()
	if err != nil {
		t.Fatalf("Failed to detect anomalies: %v", err)
	}

	t.Logf("Detected %d anomalies", len(anomalies))

	// Get anomalies
	allAnomalies := ad.GetAnomalies(10)
	if len(allAnomalies) > 0 {
		t.Logf("Total anomalies: %d", len(allAnomalies))
	}

	// Get stats
	stats := ad.GetStats()
	if stats == nil {
		t.Fatal("Failed to get stats")
	}

	t.Logf("Stats: %+v", stats)
}

func TestPredictiveMaintenance(t *testing.T) {
	pm := NewPredictiveMaintenance()

	// Add component metrics
	metrics := &ComponentMetrics{
		ComponentID: "component1",
		CPUUsage:    []float64{0.5, 0.6, 0.7, 0.8, 0.9},
		MemoryUsage: []float64{0.4, 0.5, 0.6, 0.7, 0.8},
		ErrorRate:   []float64{0.01, 0.02, 0.03, 0.04, 0.05},
	}

	err := pm.AddComponentMetrics("component1", metrics)
	if err != nil {
		t.Fatalf("Failed to add component metrics: %v", err)
	}

	// Predict failures
	predictions, err := pm.PredictFailures()
	if err != nil {
		t.Fatalf("Failed to predict failures: %v", err)
	}

	t.Logf("Predictions: %d", len(predictions))

	// Get predictions
	allPredictions := pm.GetPredictions(10)
	if len(allPredictions) > 0 {
		t.Logf("Total predictions: %d", len(allPredictions))
	}

	// Record maintenance event
	event := &MaintenanceEvent{
		ID:          "event1",
		ComponentID: "component1",
		EventType:   "preventive",
		Description: "Preventive maintenance",
		StartTime:   time.Now(),
		EndTime:     time.Now().Add(1 * time.Hour),
		Duration:    1 * time.Hour,
		Success:     true,
		Cost:        100.0,
	}

	err = pm.RecordMaintenanceEvent(event)
	if err != nil {
		t.Fatalf("Failed to record maintenance event: %v", err)
	}

	// Get stats
	stats := pm.GetStats()
	if stats == nil {
		t.Fatal("Failed to get stats")
	}

	t.Logf("Stats: %+v", stats)
}

func TestIntelligentOptimization(t *testing.T) {
	io := NewIntelligentOptimization()

	// Add parameter
	param := &Parameter{
		Name:         "cache_size",
		CurrentValue: 100.0,
		MinValue:     10.0,
		MaxValue:     1000.0,
		StepSize:     10.0,
		ImpactScore:  0.8,
	}

	err := io.AddParameter(param)
	if err != nil {
		t.Fatalf("Failed to add parameter: %v", err)
	}

	// Tune parameters
	optimizations, err := io.TuneParameters()
	if err != nil {
		t.Fatalf("Failed to tune parameters: %v", err)
	}

	t.Logf("Optimizations: %d", len(optimizations))

	// Get optimizations
	allOptimizations := io.GetOptimizations(10)
	if len(allOptimizations) > 0 {
		t.Logf("Total optimizations: %d", len(allOptimizations))

		// Apply optimization
		err = io.ApplyOptimization(allOptimizations[0].ID)
		if err != nil {
			t.Fatalf("Failed to apply optimization: %v", err)
		}
	}

	// Get stats
	stats := io.GetStats()
	if stats == nil {
		t.Fatal("Failed to get stats")
	}

	t.Logf("Stats: %+v", stats)
}

func BenchmarkRecommendationEngine(b *testing.B) {
	engine := NewRecommendationEngine()

	userProfile := &UserProfile{
		UserID:        "user1",
		ViewedContent: []string{"content1", "content2"},
		Ratings:       map[string]float64{"content1": 4.5},
		Preferences:   make(map[string]float64),
	}
	engine.AddUserProfile(userProfile)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.RecordUserInteraction("user1", "content1", 4.5)
	}
}

func BenchmarkAnomalyDetection(b *testing.B) {
	ad := NewAnomalyDetector()

	for i := 0; i < 100; i++ {
		ad.AddMetricValue("cpu_usage", float64(i%100))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ad.DetectAnomalies()
	}
}
