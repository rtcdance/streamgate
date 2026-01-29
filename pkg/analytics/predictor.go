package analytics

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Predictor makes predictions based on historical data
type Predictor struct {
	mu             sync.RWMutex
	models         map[string]*PredictionModel
	predictions    []*PredictionResult
	metricsHistory map[string][]*MetricsSnapshot
	maxHistorySize int
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

// PredictionModel represents a trained prediction model
type PredictionModel struct {
	ServiceID      string
	MetricName     string
	ModelType      string // linear, exponential, seasonal
	Coefficients   []float64
	LastUpdated    time.Time
	Accuracy       float64
	TrainingPoints int
}

// NewPredictor creates a new predictor
func NewPredictor() *Predictor {
	ctx, cancel := context.WithCancel(context.Background())

	p := &Predictor{
		models:         make(map[string]*PredictionModel),
		predictions:    make([]*PredictionResult, 0),
		metricsHistory: make(map[string][]*MetricsSnapshot),
		maxHistorySize: 10000,
		ctx:            ctx,
		cancel:         cancel,
	}

	p.start()
	return p
}

// start begins the prediction process
func (p *Predictor) start() {
	p.wg.Add(1)
	go p.predictionLoop()
}

// predictionLoop periodically makes predictions
func (p *Predictor) predictionLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.makePredictions()
		}
	}
}

// RecordMetric records a metric for prediction
func (p *Predictor) RecordMetric(metric *MetricsSnapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := metric.ServiceID
	p.metricsHistory[key] = append(p.metricsHistory[key], metric)

	// Keep history size bounded
	if len(p.metricsHistory[key]) > p.maxHistorySize {
		p.metricsHistory[key] = p.metricsHistory[key][1:]
	}
}

// makePredictions makes predictions for all services
func (p *Predictor) makePredictions() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for serviceID, metrics := range p.metricsHistory {
		if len(metrics) < 20 {
			continue // Need at least 20 data points
		}

		// Make predictions for different metrics
		p.predictMetric(serviceID, "cpu_usage", metrics)
		p.predictMetric(serviceID, "memory_usage", metrics)
		p.predictMetric(serviceID, "error_rate", metrics)
		p.predictMetric(serviceID, "request_rate", metrics)
	}
}

// predictMetric makes a prediction for a specific metric
func (p *Predictor) predictMetric(serviceID, metricName string, metrics []*MetricsSnapshot) {
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
		case "request_rate":
			value = metric.RequestRate
		default:
			continue
		}

		values = append(values, value)
	}

	if len(values) < 20 {
		return
	}

	// Train or update model
	key := serviceID + ":" + metricName
	model, exists := p.models[key]
	if !exists || time.Since(model.LastUpdated) > 1*time.Hour {
		model = p.trainModel(serviceID, metricName, values)
		p.models[key] = model
	}

	// Make predictions for different time horizons
	for _, horizon := range []string{"5m", "15m", "1h"} {
		prediction := p.makePrediction(serviceID, metricName, model, horizon, values)
		p.predictions = append(p.predictions, prediction)

		// Keep predictions list bounded
		if len(p.predictions) > 10000 {
			p.predictions = p.predictions[1:]
		}
	}
}

// trainModel trains a prediction model
func (p *Predictor) trainModel(serviceID, metricName string, values []float64) *PredictionModel {
	// Use simple linear regression for now
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

	slope := (n*sumXY - sumX*sumY) / (n*sumX2 - sumX*sumX)
	intercept := (sumY - slope*sumX) / n

	// Calculate R-squared for accuracy
	meanY := sumY / n
	ssRes := 0.0
	ssTot := 0.0

	for i, v := range values {
		predicted := intercept + slope*float64(i)
		ssRes += math.Pow(v-predicted, 2)
		ssTot += math.Pow(v-meanY, 2)
	}

	accuracy := 1.0
	if ssTot > 0 {
		accuracy = 1.0 - (ssRes / ssTot)
	}

	return &PredictionModel{
		ServiceID:      serviceID,
		MetricName:     metricName,
		ModelType:      "linear",
		Coefficients:   []float64{intercept, slope},
		LastUpdated:    time.Now(),
		Accuracy:       accuracy,
		TrainingPoints: len(values),
	}
}

// makePrediction makes a prediction using a model
func (p *Predictor) makePrediction(serviceID, metricName string, model *PredictionModel, horizon string, values []float64) *PredictionResult {
	// Calculate steps ahead based on horizon
	stepsAhead := p.getStepsAhead(horizon)

	// Make prediction
	lastIndex := float64(len(values) - 1)
	predictedIndex := lastIndex + float64(stepsAhead)
	predictedValue := model.Coefficients[0] + model.Coefficients[1]*predictedIndex

	// Ensure predicted value is within reasonable bounds
	if predictedValue < 0 {
		predictedValue = 0
	}

	// Calculate confidence based on model accuracy
	confidence := math.Max(0, model.Accuracy)

	// Generate recommendation
	recommendation := p.generateRecommendation(metricName, predictedValue, values[len(values)-1])

	return &PredictionResult{
		ID:             uuid.New().String(),
		Timestamp:      time.Now(),
		PredictionType: metricName,
		ServiceID:      serviceID,
		PredictedValue: predictedValue,
		Confidence:     confidence,
		TimeHorizon:    horizon,
		Recommendation: recommendation,
	}
}

// getStepsAhead returns the number of steps ahead for a horizon
func (p *Predictor) getStepsAhead(horizon string) int {
	switch horizon {
	case "5m":
		return 5
	case "15m":
		return 15
	case "1h":
		return 60
	default:
		return 5
	}
}

// generateRecommendation generates a recommendation based on prediction
func (p *Predictor) generateRecommendation(metricName string, predictedValue, currentValue float64) string {
	percentChange := ((predictedValue - currentValue) / (currentValue + 0.001)) * 100

	switch metricName {
	case "cpu_usage":
		if percentChange > 20 {
			return "Consider scaling up - CPU usage expected to increase significantly"
		} else if percentChange < -20 {
			return "Consider scaling down - CPU usage expected to decrease"
		}
		return "CPU usage expected to remain stable"

	case "memory_usage":
		if percentChange > 20 {
			return "Monitor memory usage - expected to increase significantly"
		} else if percentChange < -20 {
			return "Memory usage expected to decrease"
		}
		return "Memory usage expected to remain stable"

	case "error_rate":
		if percentChange > 10 {
			return "Alert: Error rate expected to increase - investigate potential issues"
		} else if percentChange < -10 {
			return "Error rate expected to improve"
		}
		return "Error rate expected to remain stable"

	case "request_rate":
		if percentChange > 30 {
			return "High traffic expected - ensure sufficient capacity"
		} else if percentChange < -30 {
			return "Lower traffic expected"
		}
		return "Request rate expected to remain stable"

	default:
		return "No specific recommendation"
	}
}

// GetPredictions returns recent predictions for a service
func (p *Predictor) GetPredictions(serviceID string, limit int) []*PredictionResult {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result []*PredictionResult
	for i := len(p.predictions) - 1; i >= 0 && len(result) < limit; i-- {
		if p.predictions[i].ServiceID == serviceID {
			result = append(result, p.predictions[i])
		}
	}

	return result
}

// GetLatestPrediction returns the latest prediction for a metric
func (p *Predictor) GetLatestPrediction(serviceID, metricName, horizon string) *PredictionResult {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for i := len(p.predictions) - 1; i >= 0; i-- {
		pred := p.predictions[i]
		if pred.ServiceID == serviceID && pred.PredictionType == metricName && pred.TimeHorizon == horizon {
			return pred
		}
	}

	return nil
}

// GetModelAccuracy returns the accuracy of a model
func (p *Predictor) GetModelAccuracy(serviceID, metricName string) float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	key := serviceID + ":" + metricName
	if model, ok := p.models[key]; ok {
		return model.Accuracy
	}

	return 0
}

// Close closes the predictor
func (p *Predictor) Close() error {
	p.cancel()
	p.wg.Wait()
	return nil
}
