package middleware

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

type RateLimitConfig struct {
	RequestsPerMinute int
	WindowSize        time.Duration
	CleanupInterval   time.Duration
}

func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 100,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}
}

type RateLimiter interface {
	Allow(key string) bool
	Stop()
}

type RedisClient interface {
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd
}

func NewRateLimiter(cfg RateLimitConfig, redisClient RedisClient) RateLimiter {
	if cfg.RequestsPerMinute <= 0 {
		cfg.RequestsPerMinute = 100
	}
	if cfg.WindowSize <= 0 {
		cfg.WindowSize = time.Minute
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = 5 * time.Minute
	}
	if redisClient != nil {
		fallback := newMemoryRateLimiter(cfg)
		return newRedisRateLimiter(cfg, redisClient, fallback)
	}
	return newMemoryRateLimiter(cfg)
}

type memoryRateLimiter struct {
	clients map[string]*clientEntry
	mu      sync.RWMutex
	wg      sync.WaitGroup
	config  RateLimitConfig
	done    chan struct{}
}

type clientEntry struct {
	count     int
	resetTime time.Time
}

func newMemoryRateLimiter(cfg RateLimitConfig) *memoryRateLimiter {
	rl := &memoryRateLimiter{
		clients: make(map[string]*clientEntry),
		config:  cfg,
		done:    make(chan struct{}),
	}
	rl.wg.Add(1)
	go rl.cleanup()
	return rl
}

func (rl *memoryRateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.clients[key]

	if !exists || now.After(entry.resetTime) {
		rl.clients[key] = &clientEntry{
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

func (rl *memoryRateLimiter) Stop() {
	select {
	case <-rl.done:
	default:
		close(rl.done)
	}
	rl.wg.Wait()
}

func (rl *memoryRateLimiter) cleanup() {
	defer rl.wg.Done()
	ticker := time.NewTicker(rl.config.CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for key, entry := range rl.clients {
				if now.After(entry.resetTime) {
					delete(rl.clients, key)
				}
			}
			rl.mu.Unlock()
		case <-rl.done:
			return
		}
	}
}

const slidingWindowScript = `
local key_prefix = KEYS[1]
local limit = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local now_ms = tonumber(ARGV[3])

local current_window = math.floor(now_ms / window_ms) * window_ms
local previous_window = current_window - window_ms

local current_key = key_prefix .. ":" .. current_window
local previous_key = key_prefix .. ":" .. previous_window

local elapsed = now_ms - current_window
local weight = (window_ms - elapsed) / window_ms

local previous_count = tonumber(redis.call('GET', previous_key) or '0')
local current_count = tonumber(redis.call('GET', current_key) or '0')

local weighted_count = math.floor(previous_count * weight + current_count)

if weighted_count < limit then
    current_count = redis.call('INCR', current_key)
    if current_count == 1 then
        redis.call('PEXPIRE', current_key, window_ms * 2)
    end
    return 1
end

return 0
`

type redisRateLimiter struct {
	client   RedisClient
	config   RateLimitConfig
	script   string
	fallback RateLimiter
}

func newRedisRateLimiter(cfg RateLimitConfig, client RedisClient, fallback RateLimiter) *redisRateLimiter {
	return &redisRateLimiter{
		client:   client,
		config:   cfg,
		script:   slidingWindowScript,
		fallback: fallback,
	}
}

func (rl *redisRateLimiter) Allow(key string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	redisKey := fmt.Sprintf("ratelimit:%s", key)
	nowMs := time.Now().UnixMilli()

	result, err := rl.client.Eval(ctx, rl.script, []string{redisKey},
		rl.config.RequestsPerMinute,
		rl.config.WindowSize.Milliseconds(),
		nowMs,
	).Result()

	if err != nil {
		return rl.fallback.Allow(key)
	}

	allowed, ok := result.(int64)
	if !ok {
		return rl.fallback.Allow(key)
	}

	return allowed == 1
}

func (rl *redisRateLimiter) Stop() {}

func (s *Service) RateLimitMiddleware() gin.HandlerFunc {
	limiter := s.rateLimiter
	return func(c *gin.Context) {
		key := c.ClientIP() + ":" + c.Request.URL.Path
		if wallet := GetWalletAddress(c); wallet != "" {
			key = key + ":" + wallet
		}
		if !limiter.Allow(key) {
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

func (s *Service) RateLimitMiddlewareWithConfig(cfg RateLimitConfig) (RateLimiter, gin.HandlerFunc) {
	rl := NewRateLimiter(cfg, s.redisClient)
	handler := func(c *gin.Context) {
		key := c.ClientIP() + ":" + c.Request.URL.Path
		if wallet := GetWalletAddress(c); wallet != "" {
			key = key + ":" + wallet
		}
		if !rl.Allow(key) {
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
