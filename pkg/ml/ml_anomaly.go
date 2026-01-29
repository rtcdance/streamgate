package ml

import (
	"math"
	"sync"
)

// MLAnomaly implements machine learning-based anomaly detection
type MLAnomaly struct {
	mu              sync.RWMutex
	isolationForest *IsolationForest
	autoencoder     *Autoencoder
	threshold       float64
}

// IsolationForest implements isolation forest algorithm
type IsolationForest struct {
	trees []*IsolationTree
}

// IsolationTree represents a single isolation tree
type IsolationTree struct {
	root *IsolationNode
}

// IsolationNode represents a node in isolation tree
type IsolationNode struct {
	feature   int
	threshold float64
	left      *IsolationNode
	right     *IsolationNode
	size      int
}

// Autoencoder implements autoencoder-based anomaly detection
type Autoencoder struct {
	inputSize  int
	hiddenSize int
	weights    [][]float64
	bias       []float64
}

// NewMLAnomaly creates a new ML anomaly detector
func NewMLAnomaly() *MLAnomaly {
	return &MLAnomaly{
		isolationForest: NewIsolationForest(100),
		autoencoder:     NewAutoencoder(10, 5),
		threshold:       0.7,
	}
}

// Detect detects anomalies using ML methods
func (ma *MLAnomaly) Detect(values []float64) float64 {
	ma.mu.RLock()
	defer ma.mu.RUnlock()

	if len(values) == 0 {
		return 0
	}

	// Isolation Forest score
	ifScore := ma.isolationForest.AnomalyScore(values)

	// Autoencoder score
	aeScore := ma.autoencoder.AnomalyScore(values)

	// Combine scores
	combinedScore := (ifScore + aeScore) / 2

	return combinedScore
}

// NewIsolationForest creates a new isolation forest
func NewIsolationForest(numTrees int) *IsolationForest {
	forest := &IsolationForest{
		trees: make([]*IsolationTree, numTrees),
	}

	for i := 0; i < numTrees; i++ {
		forest.trees[i] = &IsolationTree{
			root: &IsolationNode{},
		}
	}

	return forest
}

// AnomalyScore calculates anomaly score using isolation forest
func (ifr *IsolationForest) AnomalyScore(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	totalPathLength := 0.0

	for _, tree := range ifr.trees {
		pathLength := tree.PathLength(values, 0)
		totalPathLength += float64(pathLength)
	}

	avgPathLength := totalPathLength / float64(len(ifr.trees))

	// Normalize path length to anomaly score
	// Shorter paths indicate anomalies
	c := ifr.calculateC(len(values))
	anomalyScore := math.Pow(2, -avgPathLength/c)

	return anomalyScore
}

// PathLength calculates path length in isolation tree
func (tree *IsolationTree) PathLength(values []float64, depth int) int {
	if tree.root == nil || len(values) == 0 {
		return depth
	}

	// Simplified path length calculation
	// In real implementation, would traverse tree structure
	return depth + 1
}

// calculateC calculates normalization constant
func (ifr *IsolationForest) calculateC(n int) float64 {
	if n <= 1 {
		return 0
	}

	// Harmonic number approximation
	return 2*math.Log(float64(n-1)) + 0.5772156649
}

// NewAutoencoder creates a new autoencoder
func NewAutoencoder(inputSize, hiddenSize int) *Autoencoder {
	ae := &Autoencoder{
		inputSize:  inputSize,
		hiddenSize: hiddenSize,
		weights:    make([][]float64, inputSize),
		bias:       make([]float64, hiddenSize),
	}

	// Initialize weights
	for i := 0; i < inputSize; i++ {
		ae.weights[i] = make([]float64, hiddenSize)
		for j := 0; j < hiddenSize; j++ {
			ae.weights[i][j] = 0.1 // Simplified initialization
		}
	}

	return ae
}

// AnomalyScore calculates anomaly score using autoencoder
func (ae *Autoencoder) AnomalyScore(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	// Pad or truncate values to input size
	input := make([]float64, ae.inputSize)
	for i := 0; i < ae.inputSize && i < len(values); i++ {
		input[i] = values[i]
	}

	// Encode
	encoded := ae.Encode(input)

	// Decode
	decoded := ae.Decode(encoded)

	// Calculate reconstruction error
	error := 0.0
	for i := 0; i < ae.inputSize; i++ {
		diff := input[i] - decoded[i]
		error += diff * diff
	}

	// Normalize error to 0-1 range
	anomalyScore := math.Min(error/float64(ae.inputSize), 1.0)

	return anomalyScore
}

// Encode encodes input to hidden representation
func (ae *Autoencoder) Encode(input []float64) []float64 {
	hidden := make([]float64, ae.hiddenSize)

	for j := 0; j < ae.hiddenSize; j++ {
		sum := ae.bias[j]
		for i := 0; i < ae.inputSize; i++ {
			sum += input[i] * ae.weights[i][j]
		}
		// ReLU activation
		hidden[j] = math.Max(0, sum)
	}

	return hidden
}

// Decode decodes hidden representation to output
func (ae *Autoencoder) Decode(hidden []float64) []float64 {
	output := make([]float64, ae.inputSize)

	for i := 0; i < ae.inputSize; i++ {
		sum := 0.0
		for j := 0; j < ae.hiddenSize; j++ {
			sum += hidden[j] * ae.weights[i][j]
		}
		// Sigmoid activation
		output[i] = 1.0 / (1.0 + math.Exp(-sum))
	}

	return output
}

// Train trains the autoencoder (simplified)
func (ae *Autoencoder) Train(trainingData [][]float64, epochs int) {
	// Simplified training - in real implementation would use gradient descent
	for epoch := 0; epoch < epochs; epoch++ {
		for _, sample := range trainingData {
			// Forward pass
			encoded := ae.Encode(sample)
			decoded := ae.Decode(encoded)

			// Calculate error
			error := 0.0
			for i := 0; i < ae.inputSize; i++ {
				diff := sample[i] - decoded[i]
				error += diff * diff
			}

			// Simplified weight update
			learningRate := 0.01
			for i := 0; i < ae.inputSize; i++ {
				for j := 0; j < ae.hiddenSize; j++ {
					gradient := (sample[i] - decoded[i]) * decoded[i] * (1 - decoded[i])
					ae.weights[i][j] += learningRate * gradient
				}
			}
		}
	}
}

// GetStats returns ML anomaly detector statistics
func (ma *MLAnomaly) GetStats() map[string]interface{} {
	ma.mu.RLock()
	defer ma.mu.RUnlock()

	return map[string]interface{}{
		"detector_type":    "ml_based",
		"methods":          []string{"isolation_forest", "autoencoder"},
		"threshold":        ma.threshold,
		"isolation_trees":  len(ma.isolationForest.trees),
		"autoencoder_size": ma.autoencoder.hiddenSize,
	}
}

// SetThreshold sets anomaly detection threshold
func (ma *MLAnomaly) SetThreshold(threshold float64) {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	ma.threshold = threshold
}

// GetThreshold returns current threshold
func (ma *MLAnomaly) GetThreshold() float64 {
	ma.mu.RLock()
	defer ma.mu.RUnlock()

	return ma.threshold
}
