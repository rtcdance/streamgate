package middleware

import (
	"container/heap"
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	rateLimitTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "streamgate_ratelimit_total",
		Help: "Total rate limit checks",
	}, []string{"result", "backend"})

	rateLimitFallback = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "streamgate_ratelimit_fallback_total",
		Help: "Rate limiter fallbacks to memory backend",
	})
)

func init() {
	for _, c := range []prometheus.Collector{rateLimitTotal, rateLimitFallback} {
		if err := prometheus.Register(c); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				panic(err)
			}
		}
	}
}

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
	Allow(ctx context.Context, key string) bool
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
	clients    map[string]*clientEntry
	pq         clientHeap
	mu         sync.RWMutex
	wg         sync.WaitGroup
	config     RateLimitConfig
	maxClients int
	done       chan struct{}
}

type clientEntry struct {
	key        string
	count      int
	resetTime  time.Time
	lastAccess time.Time
	index      int
}

type clientHeap []*clientEntry

func (h clientHeap) Len() int           { return len(h) }
func (h clientHeap) Less(i, j int) bool { return h[i].lastAccess.Before(h[j].lastAccess) }
func (h clientHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *clientHeap) Push(x any) {
	n := len(*h)
	e := x.(*clientEntry)
	e.index = n
	*h = append(*h, e)
}
func (h *clientHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	old[n-1] = nil
	e.index = -1
	*h = old[:n-1]
	return e
}

func newMemoryRateLimiter(cfg RateLimitConfig) *memoryRateLimiter {
	rl := &memoryRateLimiter{
		clients:    make(map[string]*clientEntry),
		pq:         make(clientHeap, 0),
		config:     cfg,
		maxClients: 10000,
		done:       make(chan struct{}),
	}
	rl.wg.Add(1)
	go rl.cleanup()
	return rl
}

func (rl *memoryRateLimiter) Allow(ctx context.Context, key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.clients[key]

	if !exists || now.After(entry.resetTime) {
		if len(rl.clients) >= rl.maxClients {
			rl.evictOldest()
		}
		entry = &clientEntry{
			key:        key,
			count:      1,
			resetTime:  now.Add(rl.config.WindowSize),
			lastAccess: now,
		}
		rl.clients[key] = entry
		heap.Push(&rl.pq, entry)
		rateLimitTotal.WithLabelValues("allowed", "memory").Inc()
		return true
	}

	entry.lastAccess = now
	heap.Fix(&rl.pq, entry.index)
	if entry.count >= rl.config.RequestsPerMinute {
		rateLimitTotal.WithLabelValues("denied", "memory").Inc()
		return false
	}
	entry.count++
	rateLimitTotal.WithLabelValues("allowed", "memory").Inc()
	return true
}

func (rl *memoryRateLimiter) evictOldest() {
	if rl.pq.Len() == 0 {
		return
	}
	oldest := heap.Pop(&rl.pq).(*clientEntry)
	delete(rl.clients, oldest.key)
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

func (rl *redisRateLimiter) Allow(ctx context.Context, key string) bool {
	evalCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	redisKey := fmt.Sprintf("streamgate:rl:%s", key)
	nowMs := time.Now().UnixMilli()

	result, err := rl.client.Eval(evalCtx, rl.script, []string{redisKey},
		rl.config.RequestsPerMinute,
		rl.config.WindowSize.Milliseconds(),
		nowMs,
	).Result()

	if err != nil {
		rateLimitFallback.Inc()
		return rl.fallback.Allow(ctx, key)
	}

	allowed, ok := result.(int64)
	if !ok {
		rateLimitFallback.Inc()
		return rl.fallback.Allow(ctx, key)
	}

	if allowed == 1 {
		rateLimitTotal.WithLabelValues("allowed", "redis").Inc()
		return true
	}
	rateLimitTotal.WithLabelValues("denied", "redis").Inc()
	return false
}

func (rl *redisRateLimiter) Stop() {}

func (s *Service) RateLimitMiddleware() gin.HandlerFunc {
	limiter := s.rateLimiter
	return func(c *gin.Context) {
		key := c.ClientIP() + ":" + c.Request.URL.Path
		if wallet := GetWalletAddress(c); wallet != "" {
			key = key + ":" + wallet
		}
		if !limiter.Allow(c.Request.Context(), key) {
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
		if !rl.Allow(c.Request.Context(), key) {
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
