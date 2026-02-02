package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState int

const (
	StateClosed CircuitBreakerState = iota
	StateOpen
	StateHalfOpen
)

func (s CircuitBreakerState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig holds configuration for circuit breaker
type CircuitBreakerConfig struct {
	FailureThreshold     int           // Number of failures before opening
	SuccessThreshold     int           // Number of successes in half-open to close
	Timeout              time.Duration // Time to wait before trying half-open
	MaxRequests          int           // Max requests in half-open state
	FailureRateThreshold float64       // Failure rate threshold (0-1)
	WindowTime           time.Duration // Time window for failure rate calculation
}

// DefaultCircuitBreakerConfig returns default configuration
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold:     5,
		SuccessThreshold:     2,
		Timeout:              30 * time.Second,
		MaxRequests:          3,
		FailureRateThreshold: 0.5,
		WindowTime:           1 * time.Minute,
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	name            string
	config          CircuitBreakerConfig
	state           CircuitBreakerState
	failureCount    int
	successCount    int
	requestCount    int
	failureWindow   []time.Time
	lastFailureTime time.Time
	lastStateChange time.Time
	mu              sync.RWMutex
	logger          *zap.Logger
	onStateChange   func(name string, from, to CircuitBreakerState)
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(name string, config CircuitBreakerConfig, logger *zap.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		name:            name,
		config:          config,
		state:           StateClosed,
		failureWindow:   make([]time.Time, 0),
		lastStateChange: time.Now(),
		logger:          logger,
	}
}

// Execute runs the given function with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	cb.mu.Lock()

	if cb.state == StateOpen {
		if time.Since(cb.lastFailureTime) < cb.config.Timeout {
			cb.mu.Unlock()
			cb.logger.Warn("Circuit breaker is open",
				zap.String("circuit", cb.name),
				zap.Duration("retry_after", cb.config.Timeout-time.Since(cb.lastFailureTime)))
			return fmt.Errorf("circuit breaker '%s' is open", cb.name)
		}
		cb.setState(StateHalfOpen)
	}

	cb.mu.Unlock()

	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.onFailure()
		return err
	}

	cb.onSuccess()
	return nil
}

// onSuccess handles successful execution
func (cb *CircuitBreaker) onSuccess() {
	cb.failureCount = 0
	cb.requestCount++

	if cb.state == StateHalfOpen {
		cb.successCount++
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.setState(StateClosed)
		}
	}
}

// onFailure handles failed execution
func (cb *CircuitBreaker) onFailure() {
	cb.failureCount++
	cb.requestCount++
	cb.lastFailureTime = time.Now()
	cb.failureWindow = append(cb.failureWindow, time.Now())

	cb.cleanupFailureWindow()

	if cb.state == StateHalfOpen {
		cb.setState(StateOpen)
		return
	}

	if cb.failureCount >= cb.config.FailureThreshold {
		cb.setState(StateOpen)
		return
	}

	if cb.calculateFailureRate() >= cb.config.FailureRateThreshold {
		cb.setState(StateOpen)
	}
}

// cleanupFailureWindow removes old failures from the window
func (cb *CircuitBreaker) cleanupFailureWindow() {
	cutoff := time.Now().Add(-cb.config.WindowTime)
	for i, t := range cb.failureWindow {
		if t.After(cutoff) {
			cb.failureWindow = cb.failureWindow[i:]
			break
		}
	}
}

// calculateFailureRate calculates the failure rate in the current window
func (cb *CircuitBreaker) calculateFailureRate() float64 {
	if cb.requestCount == 0 {
		return 0
	}
	return float64(cb.failureCount) / float64(cb.requestCount)
}

// setState changes the circuit breaker state
func (cb *CircuitBreaker) setState(newState CircuitBreakerState) {
	if cb.state == newState {
		return
	}

	oldState := cb.state
	cb.state = newState
	cb.lastStateChange = time.Now()

	if newState == StateClosed {
		cb.failureCount = 0
		cb.successCount = 0
		cb.requestCount = 0
		cb.failureWindow = make([]time.Time, 0)
	} else if newState == StateHalfOpen {
		cb.successCount = 0
	}

	cb.logger.Info("Circuit breaker state changed",
		zap.String("circuit", cb.name),
		zap.String("from", oldState.String()),
		zap.String("to", newState.String()),
		zap.Time("time", time.Now()))

	if cb.onStateChange != nil {
		cb.onStateChange(cb.name, oldState, newState)
	}
}

// State returns the current state
func (cb *CircuitBreaker) State() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Stats returns circuit breaker statistics
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return CircuitBreakerStats{
		Name:            cb.name,
		State:           cb.state,
		FailureCount:    cb.failureCount,
		SuccessCount:    cb.successCount,
		RequestCount:    cb.requestCount,
		LastFailureTime: cb.lastFailureTime,
		LastStateChange: cb.lastStateChange,
		FailureRate:     cb.calculateFailureRate(),
	}
}

// CircuitBreakerStats holds circuit breaker statistics
type CircuitBreakerStats struct {
	Name            string
	State           CircuitBreakerState
	FailureCount    int
	SuccessCount    int
	RequestCount    int
	LastFailureTime time.Time
	LastStateChange time.Time
	FailureRate     float64
}

// SetStateChangeCallback sets a callback for state changes
func (cb *CircuitBreaker) SetStateChangeCallback(fn func(name string, from, to CircuitBreakerState)) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onStateChange = fn
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.setState(StateClosed)
}

// IsOpen returns true if the circuit is open
func (cb *CircuitBreaker) IsOpen() bool {
	return cb.State() == StateOpen
}

// IsClosed returns true if the circuit is closed
func (cb *CircuitBreaker) IsClosed() bool {
	return cb.State() == StateClosed
}

// IsHalfOpen returns true if the circuit is half-open
func (cb *CircuitBreaker) IsHalfOpen() bool {
	return cb.State() == StateHalfOpen
}

// CircuitBreakerManager manages multiple circuit breakers
type CircuitBreakerManager struct {
	breakers map[string]*CircuitBreaker
	mu       sync.RWMutex
	logger   *zap.Logger
}

// NewCircuitBreakerManager creates a new circuit breaker manager
func NewCircuitBreakerManager(logger *zap.Logger) *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers: make(map[string]*CircuitBreaker),
		logger:   logger,
	}
}

// GetOrCreate gets or creates a circuit breaker
func (m *CircuitBreakerManager) GetOrCreate(name string, config CircuitBreakerConfig) *CircuitBreaker {
	m.mu.RLock()
	cb, exists := m.breakers[name]
	m.mu.RUnlock()

	if exists {
		return cb
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if cb, exists := m.breakers[name]; exists {
		return cb
	}

	cb = NewCircuitBreaker(name, config, m.logger)
	m.breakers[name] = cb
	return cb
}

// Get returns a circuit breaker by name
func (m *CircuitBreakerManager) Get(name string) (*CircuitBreaker, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cb, exists := m.breakers[name]
	if !exists {
		return nil, fmt.Errorf("circuit breaker '%s' not found", name)
	}
	return cb, nil
}

// GetAll returns all circuit breakers
func (m *CircuitBreakerManager) GetAll() map[string]*CircuitBreaker {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*CircuitBreaker, len(m.breakers))
	for k, v := range m.breakers {
		result[k] = v
	}
	return result
}

// GetAllStats returns statistics for all circuit breakers
func (m *CircuitBreakerManager) GetAllStats() map[string]CircuitBreakerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]CircuitBreakerStats, len(m.breakers))
	for k, v := range m.breakers {
		result[k] = v.Stats()
	}
	return result
}

// ResetAll resets all circuit breakers
func (m *CircuitBreakerManager) ResetAll() {
	m.mu.RLock()
	breakers := make([]*CircuitBreaker, 0, len(m.breakers))
	for _, cb := range m.breakers {
		breakers = append(breakers, cb)
	}
	m.mu.RUnlock()

	for _, cb := range breakers {
		cb.Reset()
	}
}

// CircuitBreakerMiddleware returns a Gin middleware with circuit breaker
func (m *Service) CircuitBreakerMiddleware(name string, config CircuitBreakerConfig) gin.HandlerFunc {
	cb := NewCircuitBreaker(name, config, m.logger)

	return func(c *gin.Context) {
		err := cb.Execute(c.Request.Context(), func() error {
			c.Next()
			if c.Writer.Status() >= 500 {
				return errors.New("server error")
			}
			return nil
		})

		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":   "Service temporarily unavailable",
				"circuit": name,
				"state":   cb.State().String(),
			})
			c.Abort()
		}
	}
}
