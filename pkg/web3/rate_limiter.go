package web3

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RPCRateLimiter controls the rate of RPC requests using a token bucket algorithm.
// It supports per-second rate limiting with burst capacity.
type RPCRateLimiter struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	rate       float64 // tokens per second
	lastRefill time.Time
	logger     *zap.Logger
}

// NewRPCRateLimiter creates a new rate limiter.
// rate is the number of requests per second; burst is the maximum burst size.
func NewRPCRateLimiter(rate, burst float64, logger *zap.Logger) *RPCRateLimiter {
	if rate <= 0 {
		rate = 10 // default: 10 req/s
	}
	if burst <= 0 {
		burst = rate * 2 // default burst = 2x rate
	}
	return &RPCRateLimiter{
		tokens:     burst, // start with full bucket
		maxTokens:  burst,
		rate:       rate,
		lastRefill: time.Now(),
		logger:     logger,
	}
}

// refill adds tokens based on elapsed time since last refill.
func (rl *RPCRateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	newTokens := elapsed * rl.rate
	rl.tokens += newTokens
	if rl.tokens > rl.maxTokens {
		rl.tokens = rl.maxTokens
	}
	rl.lastRefill = now
}

// Wait blocks until a token is available or the context is cancelled.
func (rl *RPCRateLimiter) Wait(ctx context.Context) error {
	for {
		if ok := rl.TryWait(); ok {
			return nil
		}

		// Calculate wait time for next token
		rl.mu.Lock()
		waitTime := time.Duration((1-rl.tokens)/rl.rate) * time.Second
		if waitTime < 10*time.Millisecond {
			waitTime = 10 * time.Millisecond
		}
		if waitTime > 5*time.Second {
			waitTime = 5 * time.Second
		}
		rl.mu.Unlock()

		select {
		case <-ctx.Done():
			return fmt.Errorf("rate limiter wait cancelled: %w", ctx.Err())
		case <-time.After(waitTime):
			// try again
		}
	}
}

// TryWait attempts to acquire a token without blocking.
// Returns true if a token was acquired, false otherwise.
func (rl *RPCRateLimiter) TryWait() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.refill()

	if rl.tokens >= 1 {
		rl.tokens--
		return true
	}
	return false
}

// Tokens returns the current number of available tokens (for monitoring).
func (rl *RPCRateLimiter) Tokens() float64 {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.refill()
	return rl.tokens
}

// RateLimiterConfig holds configuration for RPC rate limiting.
type RateLimiterConfig struct {
	Enabled bool    `json:"enabled" yaml:"enabled"`
	Rate    float64 `json:"rate" yaml:"rate"`   // requests per second
	Burst   float64 `json:"burst" yaml:"burst"` // max burst size
}

// DefaultRateLimiterConfig returns the default rate limiter configuration.
func DefaultRateLimiterConfig() RateLimiterConfig {
	return RateLimiterConfig{
		Enabled: false,
		Rate:    10,
		Burst:   20,
	}
}

// NewRateLimiterFromConfig creates a rate limiter from config if enabled.
// Returns nil if rate limiting is disabled.
func NewRateLimiterFromConfig(cfg RateLimiterConfig, logger *zap.Logger) *RPCRateLimiter {
	if !cfg.Enabled {
		return nil
	}
	rl := NewRPCRateLimiter(cfg.Rate, cfg.Burst, logger)
	logger.Info("RPC rate limiter enabled",
		zap.Float64("rate_per_second", cfg.Rate),
		zap.Float64("burst", cfg.Burst))
	return rl
}
