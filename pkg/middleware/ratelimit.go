package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimitConfig controls the behaviour of the rate limiter.
type RateLimitConfig struct {
	RequestsPerMinute int           // max requests per window per IP (default 100)
	WindowSize        time.Duration // sliding window length (default 1 minute)
	CleanupInterval   time.Duration // how often stale entries are evicted (default 5 minutes)
}

// DefaultRateLimitConfig returns sensible defaults.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 100,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}
}

// RateLimiter manages per-IP rate tracking and exposes Stop for graceful shutdown.
type RateLimiter struct {
	clients map[string]*clientEntry
	mu      sync.RWMutex
	wg      sync.WaitGroup // tracks cleanup goroutine
	config  RateLimitConfig
	done    chan struct{}
}

type clientEntry struct {
	count     int
	resetTime time.Time
}

// NewRateLimiter creates a RateLimiter and starts the background cleanup goroutine.
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	if cfg.RequestsPerMinute <= 0 {
		cfg.RequestsPerMinute = 100
	}
	if cfg.WindowSize <= 0 {
		cfg.WindowSize = time.Minute
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = 5 * time.Minute
	}
	rl := &RateLimiter{
		clients: make(map[string]*clientEntry),
		config:  cfg,
		done:    make(chan struct{}),
	}
	rl.wg.Add(1)
	go rl.cleanup()
	return rl
}

// Allow checks whether clientIP is still within the rate limit.
func (rl *RateLimiter) Allow(clientIP string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.clients[clientIP]

	if !exists || now.After(entry.resetTime) {
		rl.clients[clientIP] = &clientEntry{
			count:     1,
			resetTime: now.Add(rl.config.WindowSize),
		}
		return true
	}

	if entry.count < rl.config.RequestsPerMinute {
		entry.count++
		return true
	}

	return false
}

// Stop terminates the background cleanup goroutine and waits for it to exit.
func (rl *RateLimiter) Stop() {
	select {
	case <-rl.done:
		// already stopped
	default:
		close(rl.done)
	}
	rl.wg.Wait()
}

func (rl *RateLimiter) cleanup() {
	defer rl.wg.Done()
	ticker := time.NewTicker(rl.config.CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for ip, entry := range rl.clients {
				if now.After(entry.resetTime) {
					delete(rl.clients, ip)
				}
			}
			rl.mu.Unlock()
		case <-rl.done:
			return
		}
	}
}

// RateLimitMiddleware returns a rate limit middleware with default config.
// For production use with proper cleanup, use RateLimitMiddlewareWithConfig instead.
func (s *Service) RateLimitMiddleware() gin.HandlerFunc {
	limiter := NewRateLimiter(DefaultRateLimitConfig())
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		if !limiter.Allow(clientIP) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded",
				"code":  "RATE_LIMITED",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// RateLimitMiddlewareWithConfig returns a rate limit middleware with the given config.
// The returned RateLimiter must be stopped via Stop() when the server shuts down.
func (s *Service) RateLimitMiddlewareWithConfig(cfg RateLimitConfig) (*RateLimiter, gin.HandlerFunc) {
	rl := NewRateLimiter(cfg)
	handler := func(c *gin.Context) {
		clientIP := c.ClientIP()
		if !rl.Allow(clientIP) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded",
				"code":  "RATE_LIMITED",
			})
			c.Abort()
			return
		}
		c.Next()
	}
	return rl, handler
}
