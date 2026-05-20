package resilience

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

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

type CircuitBreakerConfig struct {
	FailureThreshold     int
	SuccessThreshold     int
	Timeout              time.Duration
	MaxRequests          int
	FailureRateThreshold float64
	WindowTime           time.Duration
}

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

type CircuitBreaker struct {
	name            string
	config          CircuitBreakerConfig
	state           CircuitBreakerState
	failureCount    int
	successCount    int
	requestCount    int
	halfOpenCount   int
	failureWindow   []time.Time
	requestWindow   []time.Time
	lastFailureTime time.Time
	lastStateChange time.Time
	mu              sync.RWMutex
	logger          *zap.Logger
	onStateChange   func(name string, from, to CircuitBreakerState)
}

func NewCircuitBreaker(name string, config CircuitBreakerConfig, logger *zap.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		name:            name,
		config:          config,
		state:           StateClosed,
		failureWindow:   make([]time.Time, 0),
		requestWindow:   make([]time.Time, 0),
		lastStateChange: time.Now(),
		logger:          logger,
	}
}

func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) (retErr error) {
	cb.mu.Lock()

	if cb.state == StateOpen {
		if time.Since(cb.lastFailureTime) < cb.config.Timeout {
			cb.mu.Unlock()
			return fmt.Errorf("circuit breaker '%s' is open", cb.name)
		}
		cb.setState(StateHalfOpen)
	}

	wasHalfOpen := cb.state == StateHalfOpen
	if wasHalfOpen {
		if cb.config.MaxRequests > 0 && cb.halfOpenCount >= cb.config.MaxRequests {
			cb.mu.Unlock()
			return fmt.Errorf("circuit breaker '%s' is half-open and max requests exceeded", cb.name)
		}
		cb.halfOpenCount++
	}

	cb.mu.Unlock()

	var panicked bool

	defer func() {
		if r := recover(); r != nil {
			panicked = true
			retErr = fmt.Errorf("panic in circuit breaker '%s': %v", cb.name, r)
			cb.logger.Error("panic recovered in circuit breaker execute",
				zap.String("circuit", cb.name),
				zap.Any("panic", r))
		}
		cb.mu.Lock()
		if wasHalfOpen {
			cb.halfOpenCount--
		}
		if panicked {
			cb.mu.Unlock()
			return
		}
		if retErr != nil {
			cb.onFailure()
		} else {
			cb.onSuccess()
		}
		cb.mu.Unlock()
	}()

	retErr = fn()
	return
}

func (cb *CircuitBreaker) onSuccess() {
	cb.failureCount = 0
	cb.requestCount++
	cb.requestWindow = append(cb.requestWindow, time.Now())
	cb.cleanupWindows()

	if cb.state == StateHalfOpen {
		cb.successCount++
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.setState(StateClosed)
		}
	}
}

func (cb *CircuitBreaker) onFailure() {
	cb.failureCount++
	cb.requestCount++
	cb.lastFailureTime = time.Now()
	cb.failureWindow = append(cb.failureWindow, time.Now())
	cb.requestWindow = append(cb.requestWindow, time.Now())

	cb.cleanupWindows()

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

func trimWindow(window *[]time.Time, cutoff time.Time) {
	for i, t := range *window {
		if t.After(cutoff) {
			*window = (*window)[i:]
			return
		}
	}
	*window = (*window)[:0]
}

func (cb *CircuitBreaker) cleanupWindows() {
	cutoff := time.Now().Add(-cb.config.WindowTime)
	trimWindow(&cb.failureWindow, cutoff)
	trimWindow(&cb.requestWindow, cutoff)
}

func (cb *CircuitBreaker) calculateFailureRate() float64 {
	cutoff := time.Now().Add(-cb.config.WindowTime)
	failuresInWindow := 0
	for _, t := range cb.failureWindow {
		if t.After(cutoff) {
			failuresInWindow++
		}
	}
	requestsInWindow := 0
	for _, t := range cb.requestWindow {
		if t.After(cutoff) {
			requestsInWindow++
		}
	}
	if requestsInWindow == 0 {
		return 0
	}
	return float64(failuresInWindow) / float64(requestsInWindow)
}

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
		cb.halfOpenCount = 0
		cb.failureWindow = make([]time.Time, 0)
		cb.requestWindow = make([]time.Time, 0)
	} else if newState == StateHalfOpen {
		cb.successCount = 0
		cb.halfOpenCount = 0
	} else if newState == StateOpen {
		cb.halfOpenCount = 0
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

func (cb *CircuitBreaker) State() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

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

func (cb *CircuitBreaker) SetStateChangeCallback(fn func(name string, from, to CircuitBreakerState)) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onStateChange = fn
}

func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.setState(StateClosed)
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	if cb.state == StateOpen {
		return time.Since(cb.lastFailureTime) >= cb.config.Timeout
	}
	return true
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	cb.onSuccess()
	cb.mu.Unlock()
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	cb.onFailure()
	cb.mu.Unlock()
}

func (cb *CircuitBreaker) IsOpen() bool {
	return cb.State() == StateOpen
}

func (cb *CircuitBreaker) IsClosed() bool {
	return cb.State() == StateClosed
}

func (cb *CircuitBreaker) IsHalfOpen() bool {
	return cb.State() == StateHalfOpen
}

type CircuitBreakerManager struct {
	breakers map[string]*CircuitBreaker
	mu       sync.RWMutex
	logger   *zap.Logger
}

func NewCircuitBreakerManager(logger *zap.Logger) *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers: make(map[string]*CircuitBreaker),
		logger:   logger,
	}
}

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

func (m *CircuitBreakerManager) Get(name string) (*CircuitBreaker, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cb, exists := m.breakers[name]
	if !exists {
		return nil, fmt.Errorf("circuit breaker '%s' not found", name)
	}
	return cb, nil
}

func (m *CircuitBreakerManager) GetAll() map[string]*CircuitBreaker {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*CircuitBreaker, len(m.breakers))
	for k, v := range m.breakers {
		result[k] = v
	}
	return result
}

func (m *CircuitBreakerManager) GetAllStats() map[string]CircuitBreakerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]CircuitBreakerStats, len(m.breakers))
	for k, v := range m.breakers {
		result[k] = v.Stats()
	}
	return result
}

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
