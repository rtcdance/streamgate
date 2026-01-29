package ml

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// AnomalyDetector detects anomalies in system metrics and user behavior
type AnomalyDetector struct {
	mu                  sync.RWMutex
	statisticalDetector *StatisticalAnomaly
	mlDetector          *MLAnomaly
	alerting            *AlertingSystem
	metrics             map[string]*MetricTimeSeries
	anomalies           []*Anomaly
	detectionThreshold  float64
	windowSize          int
	lastUpdate          time.Time
}

// Anomaly represents a detected anomaly
type Anomaly struct {
	ID              string
	Type            string
	Severity        string // low, medium, high, critical
	Score           float64
	Timestamp       time.Time
	Description     string
	AffectedMetrics []string
	RootCause       string
	Recommendation  string
	Resolved        bool
	ResolvedAt      time.Time
}

// MetricTimeSeries stores time series data for a metric
type MetricTimeSeries struct {
	Name       string
	Values     []float64
	Timestamps []time.Time
	Mean       float64
	StdDev     float64
	Min        float64
	Max        float64
	LastUpdate time.Time
}

// NewAnomalyDetector creates a new anomaly detector
func NewAnomalyDetector() *AnomalyDetector {
	return &AnomalyDetector{
		statisticalDetector: NewStatisticalAnomaly(),
		mlDetector:          NewMLAnomaly(),
		alerting:            NewAlertingSystem(),
		metrics:             make(map[string]*MetricTimeSeries),
		anomalies:           make([]*Anomaly, 0),
		detectionThreshold:  0.8,
		windowSize:          100,
	}
}

// AddMetricValue adds a metric value to the time series
func (ad *AnomalyDetector) AddMetricValue(metricName string, value float64) error {
	if metricName == "" {
		return fmt.Errorf("invalid metric name")
	}

	ad.mu.Lock()
	defer ad.mu.Unlock()

	ts, exists := ad.metrics[metricName]
	if !exists {
		ts = &MetricTimeSeries{
			Name:       metricName,
			Values:     make([]float64, 0),
			Timestamps: make([]time.Time, 0),
		}
		ad.metrics[metricName] = ts
	}

	ts.Values = append(ts.Values, value)
	ts.Timestamps = append(ts.Timestamps, time.Now())

	// Keep only recent values
	if len(ts.Values) > ad.windowSize {
		ts.Values = ts.Values[1:]
		ts.Timestamps = ts.Timestamps[1:]
	}

	// Update statistics
	ad.updateMetricStats(ts)
	ts.LastUpdate = time.Now()

	return nil
}

// updateMetricStats updates statistics for a metric
func (ad *AnomalyDetector) updateMetricStats(ts *MetricTimeSeries) {
	if len(ts.Values) == 0 {
		return
	}

	// Calculate mean
	sum := 0.0
	for _, v := range ts.Values {
		sum += v
	}
	ts.Mean = sum / float64(len(ts.Values))

	// Calculate standard deviation
	variance := 0.0
	for _, v := range ts.Values {
		variance += (v - ts.Mean) * (v - ts.Mean)
	}
	ts.StdDev = math.Sqrt(variance / float64(len(ts.Values)))

	// Find min and max
	ts.Min = ts.Values[0]
	ts.Max = ts.Values[0]
	for _, v := range ts.Values {
		if v < ts.Min {
			ts.Min = v
		}
		if v > ts.Max {
			ts.Max = v
		}
	}
}

// DetectAnomalies detects anomalies in all metrics
func (ad *AnomalyDetector) DetectAnomalies() ([]*Anomaly, error) {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	detectedAnomalies := make([]*Anomaly, 0)

	for metricName, ts := range ad.metrics {
		if len(ts.Values) < 2 {
			continue
		}

		// Get latest value
		latestValue := ts.Values[len(ts.Values)-1]

		// Statistical detection
		statAnomaly := ad.statisticalDetector.Detect(latestValue, ts.Mean, ts.StdDev)

		// ML detection
		mlAnomaly := ad.mlDetector.Detect(ts.Values)

		// Combine scores
		combinedScore := (statAnomaly + mlAnomaly) / 2

		if combinedScore > ad.detectionThreshold {
			severity := ad.calculateSeverity(combinedScore, latestValue, ts.Mean, ts.StdDev)

			anomaly := &Anomaly{
				ID:              fmt.Sprintf("%s_%d", metricName, time.Now().Unix()),
				Type:            metricName,
				Severity:        severity,
				Score:           combinedScore,
				Timestamp:       time.Now(),
				Description:     fmt.Sprintf("Anomaly detected in %s: value=%.2f, mean=%.2f, stddev=%.2f", metricName, latestValue, ts.Mean, ts.StdDev),
				AffectedMetrics: []string{metricName},
				RootCause:       ad.analyzeRootCause(metricName, ts),
				Recommendation:  ad.generateRecommendation(metricName, latestValue, ts.Mean),
			}

			detectedAnomalies = append(detectedAnomalies, anomaly)
			ad.anomalies = append(ad.anomalies, anomaly)

			// Generate alert
			ad.alerting.GenerateAlert(anomaly)
		}
	}

	ad.lastUpdate = time.Now()
	return detectedAnomalies, nil
}

// calculateSeverity calculates anomaly severity
func (ad *AnomalyDetector) calculateSeverity(score, value, mean, stddev float64) string {
	if score > 0.95 {
		return "critical"
	}
	if score > 0.85 {
		return "high"
	}
	if score > 0.70 {
		return "medium"
	}
	return "low"
}

// analyzeRootCause analyzes potential root cause of anomaly
func (ad *AnomalyDetector) analyzeRootCause(metricName string, ts *MetricTimeSeries) string {
	if len(ts.Values) < 2 {
		return "Insufficient data"
	}

	latestValue := ts.Values[len(ts.Values)-1]
	previousValue := ts.Values[len(ts.Values)-2]

	changePercent := ((latestValue - previousValue) / previousValue) * 100

	if changePercent > 50 {
		return "Sudden spike detected"
	}
	if changePercent < -50 {
		return "Sudden drop detected"
	}
	if latestValue > ts.Mean+3*ts.StdDev {
		return "Value exceeds 3-sigma threshold"
	}
	if latestValue < ts.Mean-3*ts.StdDev {
		return "Value below 3-sigma threshold"
	}

	return "Unusual pattern detected"
}

// generateRecommendation generates recommendation for anomaly
func (ad *AnomalyDetector) generateRecommendation(metricName string, value, mean float64) string {
	if value > mean {
		return fmt.Sprintf("Investigate high %s. Consider scaling resources or optimizing performance.", metricName)
	}
	return fmt.Sprintf("Investigate low %s. Check service health and connectivity.", metricName)
}

// GetAnomalies returns detected anomalies
func (ad *AnomalyDetector) GetAnomalies(limit int) []*Anomaly {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	// Sort by timestamp descending
	sort.Slice(ad.anomalies, func(i, j int) bool {
		return ad.anomalies[i].Timestamp.After(ad.anomalies[j].Timestamp)
	})

	if len(ad.anomalies) > limit {
		return ad.anomalies[:limit]
	}

	return ad.anomalies
}

// ResolveAnomaly marks an anomaly as resolved
func (ad *AnomalyDetector) ResolveAnomaly(anomalyID string) error {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	for _, anomaly := range ad.anomalies {
		if anomaly.ID == anomalyID {
			anomaly.Resolved = true
			anomaly.ResolvedAt = time.Now()
			return nil
		}
	}

	return fmt.Errorf("anomaly not found")
}

// GetMetricStats returns statistics for a metric
func (ad *AnomalyDetector) GetMetricStats(metricName string) (*MetricTimeSeries, error) {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	ts, exists := ad.metrics[metricName]
	if !exists {
		return nil, fmt.Errorf("metric not found")
	}

	return ts, nil
}

// GetStats returns anomaly detector statistics
func (ad *AnomalyDetector) GetStats() map[string]interface{} {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	unresolved := 0
	for _, anomaly := range ad.anomalies {
		if !anomaly.Resolved {
			unresolved++
		}
	}

	return map[string]interface{}{
		"total_metrics":       len(ad.metrics),
		"total_anomalies":     len(ad.anomalies),
		"unresolved":          unresolved,
		"detection_threshold": ad.detectionThreshold,
		"window_size":         ad.windowSize,
		"last_update":         ad.lastUpdate,
	}
}

// SetDetectionThreshold sets the anomaly detection threshold
func (ad *AnomalyDetector) SetDetectionThreshold(threshold float64) error {
	if threshold < 0 || threshold > 1 {
		return fmt.Errorf("threshold must be between 0 and 1")
	}

	ad.mu.Lock()
	defer ad.mu.Unlock()

	ad.detectionThreshold = threshold
	return nil
}

// ClearAnomalies clears all anomalies
func (ad *AnomalyDetector) ClearAnomalies() {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	ad.anomalies = make([]*Anomaly, 0)
}

// ClearMetrics clears all metrics
func (ad *AnomalyDetector) ClearMetrics() {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	ad.metrics = make(map[string]*MetricTimeSeries)
}
