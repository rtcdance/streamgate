package ml

import (
	"math"
	"sync"
)

// StatisticalAnomaly implements statistical anomaly detection methods
type StatisticalAnomaly struct {
	mu sync.RWMutex
}

// NewStatisticalAnomaly creates a new statistical anomaly detector
func NewStatisticalAnomaly() *StatisticalAnomaly {
	return &StatisticalAnomaly{}
}

// Detect detects anomalies using statistical methods
func (sa *StatisticalAnomaly) Detect(value, mean, stddev float64) float64 {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	if stddev == 0 {
		return 0
	}

	// Z-score method
	zScore := math.Abs((value - mean) / stddev)

	// Convert z-score to anomaly score (0-1)
	// Using error function approximation
	anomalyScore := sa.zScoreToAnomalyScore(zScore)

	return anomalyScore
}

// zScoreToAnomalyScore converts z-score to anomaly score
func (sa *StatisticalAnomaly) zScoreToAnomalyScore(zScore float64) float64 {
	// Using sigmoid-like function
	// Score increases with z-score
	// At z=1: ~0.27, z=2: ~0.73, z=3: ~0.95
	return 1.0 / (1.0 + math.Exp(-zScore+2))
}

// DetectOutliers detects outliers in a dataset using IQR method
func (sa *StatisticalAnomaly) DetectOutliers(values []float64) []int {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	if len(values) < 4 {
		return nil
	}

	// Sort values
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sa.quickSort(sorted, 0, len(sorted)-1)

	// Calculate quartiles
	q1 := sa.percentile(sorted, 0.25)
	q3 := sa.percentile(sorted, 0.75)
	iqr := q3 - q1

	// Define outlier bounds
	lowerBound := q1 - 1.5*iqr
	upperBound := q3 + 1.5*iqr

	// Find outliers
	outliers := make([]int, 0)
	for i, v := range values {
		if v < lowerBound || v > upperBound {
			outliers = append(outliers, i)
		}
	}

	return outliers
}

// DetectTrend detects trend in time series
func (sa *StatisticalAnomaly) DetectTrend(values []float64) string {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	if len(values) < 2 {
		return "insufficient_data"
	}

	// Calculate linear regression slope
	slope := sa.calculateSlope(values)

	if slope > 0.1 {
		return "increasing"
	}
	if slope < -0.1 {
		return "decreasing"
	}
	return "stable"
}

// calculateSlope calculates linear regression slope
func (sa *StatisticalAnomaly) calculateSlope(values []float64) float64 {
	n := float64(len(values))
	sumX := 0.0
	sumY := 0.0
	sumXY := 0.0
	sumX2 := 0.0

	for i, v := range values {
		x := float64(i)
		sumX += x
		sumY += v
		sumXY += x * v
		sumX2 += x * x
	}

	numerator := n*sumXY - sumX*sumY
	denominator := n*sumX2 - sumX*sumX

	if denominator == 0 {
		return 0
	}

	return numerator / denominator
}

// DetectSeasonality detects seasonal patterns
func (sa *StatisticalAnomaly) DetectSeasonality(values []float64, period int) float64 {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	if len(values) < period*2 {
		return 0
	}

	// Calculate autocorrelation at lag=period
	autocorr := sa.calculateAutocorrelation(values, period)

	return math.Abs(autocorr)
}

// calculateAutocorrelation calculates autocorrelation at given lag
func (sa *StatisticalAnomaly) calculateAutocorrelation(values []float64, lag int) float64 {
	if len(values) <= lag {
		return 0
	}

	// Calculate mean
	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))

	// Calculate variance
	variance := 0.0
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	variance /= float64(len(values))

	if variance == 0 {
		return 0
	}

	// Calculate covariance at lag
	covariance := 0.0
	for i := 0; i < len(values)-lag; i++ {
		covariance += (values[i] - mean) * (values[i+lag] - mean)
	}
	covariance /= float64(len(values) - lag)

	return covariance / variance
}

// percentile calculates percentile of sorted values
func (sa *StatisticalAnomaly) percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}

	index := p * float64(len(sorted)-1)
	lower := int(index)
	upper := lower + 1

	if upper >= len(sorted) {
		return sorted[lower]
	}

	weight := index - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

// quickSort sorts array in place
func (sa *StatisticalAnomaly) quickSort(arr []float64, low, high int) {
	if low < high {
		pi := sa.partition(arr, low, high)
		sa.quickSort(arr, low, pi-1)
		sa.quickSort(arr, pi+1, high)
	}
}

// partition partitions array for quicksort
func (sa *StatisticalAnomaly) partition(arr []float64, low, high int) int {
	pivot := arr[high]
	i := low - 1

	for j := low; j < high; j++ {
		if arr[j] < pivot {
			i++
			arr[i], arr[j] = arr[j], arr[i]
		}
	}

	arr[i+1], arr[high] = arr[high], arr[i+1]
	return i + 1
}

// DetectSpike detects sudden spikes in values
func (sa *StatisticalAnomaly) DetectSpike(values []float64, threshold float64) []int {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	if len(values) < 2 {
		return nil
	}

	spikes := make([]int, 0)

	for i := 1; i < len(values); i++ {
		changePercent := math.Abs((values[i] - values[i-1]) / values[i-1])
		if changePercent > threshold {
			spikes = append(spikes, i)
		}
	}

	return spikes
}

// CalculateMovingAverage calculates moving average
func (sa *StatisticalAnomaly) CalculateMovingAverage(values []float64, window int) []float64 {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	if len(values) < window {
		return nil
	}

	result := make([]float64, len(values)-window+1)

	for i := 0; i <= len(values)-window; i++ {
		sum := 0.0
		for j := 0; j < window; j++ {
			sum += values[i+j]
		}
		result[i] = sum / float64(window)
	}

	return result
}

// CalculateExponentialMovingAverage calculates exponential moving average
func (sa *StatisticalAnomaly) CalculateExponentialMovingAverage(values []float64, alpha float64) []float64 {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	if len(values) == 0 {
		return nil
	}

	result := make([]float64, len(values))
	result[0] = values[0]

	for i := 1; i < len(values); i++ {
		result[i] = alpha*values[i] + (1-alpha)*result[i-1]
	}

	return result
}

// GetStats returns statistical anomaly detector statistics
func (sa *StatisticalAnomaly) GetStats() map[string]interface{} {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	return map[string]interface{}{
		"detector_type": "statistical",
		"methods":       []string{"z-score", "iqr", "trend", "seasonality", "spike"},
	}
}
