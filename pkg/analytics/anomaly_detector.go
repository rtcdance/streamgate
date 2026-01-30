package analytics

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AnomalyDetector detects anomalies in metrics
type AnomalyDetector struct {
	mu              sync.RWMutex
	baselines       map[string]*Baseline
	anomalies       []*AnomalyDetection
	metricsHistory  map[string][]*MetricsSnapshot
	maxHistorySize  int
	stdDevThreshold float64
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

// Baseline represents a baseline for anomaly detection
type Baseline struct {
	ServiceID   string
	MetricName  string
	Mean        float64
	StdDev      float64
	Min         float64
	Max         float64
	LastUpdated time.Time
}

// NewAnomalyDetector creates a new anomaly detector
func NewAnomalyDetector(stdDevThreshold float64) *AnomalyDetector {
	ctx, cancel := context.WithCancel(context.Background())

	ad := &AnomalyDetector{
		baselines:       make(map[string]*Baseline),
		anomalies:       make([]*AnomalyDetection, 0),
		metricsHistory:  make(map[string][]*MetricsSnapshot),
		maxHistorySize:  1000,
		stdDevThreshold: stdDevThreshold,
		ctx:             ctx,
		cancel:          cancel,
	}

	ad.start()
	return ad
}

// start begins the anomaly detection process
func (ad *AnomalyDetector) start() {
	ad.wg.Add(1)
	go ad.detectionLoop()
}

// detectionLoop periodically detects anomalies
func (ad *AnomalyDetector) detectionLoop() {
	defer ad.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ad.ctx.Done():
			return
		case <-ticker.C:
			ad.detectAnomalies()
		}
	}
}

// RecordMetric records a metric for anomaly detection
func (ad *AnomalyDetector) RecordMetric(metric *MetricsSnapshot) {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	key := metric.ServiceID
	ad.metricsHistory[key] = append(ad.metricsHistory[key], metric)

	// Keep history size bounded
	if len(ad.metricsHistory[key]) > ad.maxHistorySize {
		ad.metricsHistory[key] = ad.metricsHistory[key][1:]
	}
}

// detectAnomalies detects anomalies in the metrics
func (ad *AnomalyDetector) detectAnomalies() {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	for serviceID, metrics := range ad.metricsHistory {
		if len(metrics) < 10 {
			continue // Need at least 10 data points
		}

		// Get latest metric
		latest := metrics[len(metrics)-1]

		// Check CPU usage
		ad.checkMetricAnomaly(serviceID, "cpu_usage", latest.CPUUsage, metrics)

		// Check memory usage
		ad.checkMetricAnomaly(serviceID, "memory_usage", latest.MemoryUsage, metrics)

		// Check error rate
		ad.checkMetricAnomaly(serviceID, "error_rate", latest.ErrorRate, metrics)

		// Check latency
		ad.checkMetricAnomaly(serviceID, "latency", latest.Latency, metrics)

		// Check request rate
		ad.checkMetricAnomaly(serviceID, "request_rate", latest.RequestRate, metrics)
	}
}

// DetectAnomaliesNow performs anomaly detection immediately
func (ad *AnomalyDetector) DetectAnomaliesNow() {
	ad.detectAnomalies()
}

// checkMetricAnomaly checks if a metric value is anomalous
func (ad *AnomalyDetector) checkMetricAnomaly(serviceID, metricName string, value float64, metrics []*MetricsSnapshot) {
	key := serviceID + ":" + metricName

	// Get or create baseline
	baseline, exists := ad.baselines[key]
	if !exists {
		baseline = ad.calculateBaseline(serviceID, metricName, metrics)
		ad.baselines[key] = baseline
		return // Skip first calculation
	}

	// Check if value is anomalous
	deviation := math.Abs(value-baseline.Mean) / (baseline.StdDev + 0.001) // Add small value to avoid division by zero

	if deviation > ad.stdDevThreshold {
		severity := ad.calculateSeverity(deviation)
		anomaly := &AnomalyDetection{
			ID:            uuid.New().String(),
			Timestamp:     time.Now(),
			ServiceID:     serviceID,
			MetricName:    metricName,
			CurrentValue:  value,
			ExpectedValue: baseline.Mean,
			Deviation:     deviation,
			Severity:      severity,
			Description:   fmt.Sprintf("%s is %.2f standard deviations from baseline", metricName, deviation),
		}

		ad.anomalies = append(ad.anomalies, anomaly)

		// Keep anomalies list bounded
		if len(ad.anomalies) > 1000 {
			ad.anomalies = ad.anomalies[1:]
		}

		// Update baseline
		baseline.LastUpdated = time.Now()
	}

	// Update baseline periodically
	if time.Since(baseline.LastUpdated) > 1*time.Hour {
		newBaseline := ad.calculateBaseline(serviceID, metricName, metrics)
		ad.baselines[key] = newBaseline
	}
}

// calculateBaseline calculates baseline statistics for a metric
func (ad *AnomalyDetector) calculateBaseline(serviceID, metricName string, metrics []*MetricsSnapshot) *Baseline {
	var values []float64

	for _, metric := range metrics {
		var value float64
		switch metricName {
		case "cpu_usage":
			value = metric.CPUUsage
		case "memory_usage":
			value = metric.MemoryUsage
		case "error_rate":
			value = metric.ErrorRate
		case "latency":
			value = metric.Latency
		case "request_rate":
			value = metric.RequestRate
		default:
			continue
		}

		values = append(values, value)
	}

	if len(values) == 0 {
		return &Baseline{
			ServiceID:   serviceID,
			MetricName:  metricName,
			Mean:        0,
			StdDev:      0,
			Min:         0,
			Max:         0,
			LastUpdated: time.Now(),
		}
	}

	mean := ad.calculateMean(values)
	stdDev := ad.calculateStdDev(values, mean)
	min := ad.calculateMin(values)
	max := ad.calculateMax(values)

	return &Baseline{
		ServiceID:   serviceID,
		MetricName:  metricName,
		Mean:        mean,
		StdDev:      stdDev,
		Min:         min,
		Max:         max,
		LastUpdated: time.Now(),
	}
}

// calculateSeverity calculates the severity of an anomaly
func (ad *AnomalyDetector) calculateSeverity(deviation float64) string {
	if deviation > 5 {
		return "critical"
	} else if deviation > 3 {
		return "high"
	} else if deviation > 2 {
		return "medium"
	}
	return "low"
}

// calculateMean calculates the mean of values
func (ad *AnomalyDetector) calculateMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}

	return sum / float64(len(values))
}

// calculateStdDev calculates the standard deviation of values
func (ad *AnomalyDetector) calculateStdDev(values []float64, mean float64) float64 {
	if len(values) == 0 {
		return 0
	}

	variance := 0.0
	for _, v := range values {
		variance += math.Pow(v-mean, 2)
	}

	variance /= float64(len(values))
	return math.Sqrt(variance)
}

// calculateMin calculates the minimum of values
func (ad *AnomalyDetector) calculateMin(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	min := values[0]
	for _, v := range values {
		if v < min {
			min = v
		}
	}

	return min
}

// calculateMax calculates the maximum of values
func (ad *AnomalyDetector) calculateMax(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	max := values[0]
	for _, v := range values {
		if v > max {
			max = v
		}
	}

	return max
}

// GetAnomalies returns recent anomalies
func (ad *AnomalyDetector) GetAnomalies(serviceID string, limit int) []*AnomalyDetection {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	var result []*AnomalyDetection
	for i := len(ad.anomalies) - 1; i >= 0 && len(result) < limit; i-- {
		if ad.anomalies[i].ServiceID == serviceID {
			result = append(result, ad.anomalies[i])
		}
	}

	return result
}

// GetAllAnomalies returns all recent anomalies
func (ad *AnomalyDetector) GetAllAnomalies(limit int) []*AnomalyDetection {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	if len(ad.anomalies) <= limit {
		return ad.anomalies
	}

	return ad.anomalies[len(ad.anomalies)-limit:]
}

// Close closes the anomaly detector
func (ad *AnomalyDetector) Close() error {
	ad.cancel()
	ad.wg.Wait()
	return nil
}
