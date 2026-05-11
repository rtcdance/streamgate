package api

import (
	"sync"
	"time"
)

// bucket is a per-client token bucket for rate limiting.
type bucket struct {
	tokens    float64
	maxTokens float64
	refill    float64 // tokens added per second
	lastRefill time.Time
}

func (b *bucket) refillTokens() {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * b.refill
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
	b.lastRefill = now
}

// RateLimiter limits request rate per client IP using a token bucket algorithm.
type RateLimiter struct {
	limit    float64 // requests per second
	burst    int     // max burst size
	buckets  sync.Map // string → *bucket
	mu       sync.Mutex
}

// NewRateLimiter creates a new rate limiter.
// limit is the number of requests allowed per second per client.
func NewRateLimiter(limit int) *RateLimiter {
	if limit <= 0 {
		limit = 1
	}
	return &RateLimiter{
		limit: float64(limit),
		burst: limit, // burst equals the per-second rate
	}
}

// Allow checks if a request from the given client IP is allowed.
// Returns true if the request is within rate limits, false otherwise.
func (r *RateLimiter) Allow(clientIP string) bool {
	now := time.Now()

	val, loaded := r.buckets.LoadOrStore(clientIP, &bucket{
		tokens:     float64(r.burst) - 1,
		maxTokens:  float64(r.burst),
		refill:     r.limit,
		lastRefill: now,
	})

	b := val.(*bucket)
	if !loaded {
		// New bucket: we already consumed one token in the initial value
		return true
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	b.refillTokens()

	if b.tokens < 1 {
		return false
	}

	b.tokens--
	return true
}
