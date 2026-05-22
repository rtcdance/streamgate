package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestMemoryRateLimiter_Allow(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 5,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}
	rl := newMemoryRateLimiter(cfg)
	defer rl.Stop()

	for i := 0; i < cfg.RequestsPerMinute; i++ {
		assert.True(t, rl.Allow(context.Background(), "test-key"), "request %d should be allowed", i+1)
	}

	assert.False(t, rl.Allow(context.Background(), "test-key"), "request should be denied after exceeding limit")
	assert.False(t, rl.Allow(context.Background(), "test-key"), "subsequent request should also be denied")
}

func TestMemoryRateLimiter_DifferentKeys(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 3,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}
	rl := newMemoryRateLimiter(cfg)
	defer rl.Stop()

	for i := 0; i < cfg.RequestsPerMinute; i++ {
		assert.True(t, rl.Allow(context.Background(), "key-a"), "key-a request %d should be allowed", i+1)
	}
	assert.False(t, rl.Allow(context.Background(), "key-a"), "key-a should be denied after limit")

	for i := 0; i < cfg.RequestsPerMinute; i++ {
		assert.True(t, rl.Allow(context.Background(), "key-b"), "key-b request %d should be allowed", i+1)
	}
	assert.False(t, rl.Allow(context.Background(), "key-b"), "key-b should be denied after limit")
}

func TestMemoryRateLimiter_WindowReset(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 3,
		WindowSize:        50 * time.Millisecond,
		CleanupInterval:   5 * time.Minute,
	}
	rl := newMemoryRateLimiter(cfg)
	defer rl.Stop()

	for i := 0; i < cfg.RequestsPerMinute; i++ {
		assert.True(t, rl.Allow(context.Background(), "test-key"), "request %d should be allowed", i+1)
	}
	assert.False(t, rl.Allow(context.Background(), "test-key"), "should be denied at limit")

	time.Sleep(100 * time.Millisecond)

	assert.True(t, rl.Allow(context.Background(), "test-key"), "should be allowed after window reset")
}

func TestMemoryRateLimiter_ConcurrentAccess(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 100,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}
	rl := newMemoryRateLimiter(cfg)
	defer rl.Stop()

	var wg sync.WaitGroup
	results := make(chan bool, cfg.RequestsPerMinute+50)

	for i := 0; i < cfg.RequestsPerMinute+50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- rl.Allow(context.Background(), "concurrent-key")
		}()
	}
	wg.Wait()
	close(results)

	allowed := 0
	for r := range results {
		if r {
			allowed++
		}
	}
	assert.Equal(t, cfg.RequestsPerMinute, allowed,
		"exactly %d requests should be allowed under concurrent access", cfg.RequestsPerMinute)
}

func TestMemoryRateLimiter_Stop(t *testing.T) {
	rl := newMemoryRateLimiter(DefaultRateLimitConfig())
	rl.Stop()
	assert.True(t, rl.Allow(context.Background(), "after-stop-1"), "allow should still work after stop (cleanup goroutine stopped)")
	assert.True(t, rl.Allow(context.Background(), "after-stop-2"), "allow should still work after stop")

	rl.Stop()
}

func TestRateLimiter_CustomConfig(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 2,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}
	rl := NewRateLimiter(cfg, nil)
	defer rl.Stop()

	assert.True(t, rl.Allow(context.Background(), "k1"))
	assert.True(t, rl.Allow(context.Background(), "k1"))
	assert.False(t, rl.Allow(context.Background(), "k1"))
}

func TestRateLimiter_ZeroConfigDefaults(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 0,
		WindowSize:        0,
		CleanupInterval:   0,
	}
	rl := NewRateLimiter(cfg, nil)
	defer rl.Stop()

	for i := 0; i < 100; i++ {
		assert.True(t, rl.Allow(context.Background(), "test-key"), "default config allows 100 requests")
	}
	assert.False(t, rl.Allow(context.Background(), "test-key"), "default config denies request 101")
}

func TestRateLimitMiddleware_Integration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	cfg := RateLimitConfig{
		RequestsPerMinute: 3,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}
	rl, handler := (&Service{rateLimiter: NewRateLimiter(cfg, nil)}).RateLimitMiddlewareWithConfig(cfg)
	defer rl.Stop()

	router.Use(handler)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	makeRequest := func(ip string) int {
		req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
		req.RemoteAddr = ip + ":8080"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}

	for i := 0; i < cfg.RequestsPerMinute; i++ {
		code := makeRequest("10.0.0.1")
		assert.Equal(t, http.StatusOK, code, "request %d from 10.0.0.1 should succeed", i+1)
	}
	assert.Equal(t, http.StatusTooManyRequests, makeRequest("10.0.0.1"),
		"request after limit from same IP should be rate-limited")

	assert.Equal(t, http.StatusOK, makeRequest("10.0.0.2"),
		"request from different IP should succeed")
}

func TestNewRateLimiter_NilRedis(t *testing.T) {
	rl := NewRateLimiter(DefaultRateLimitConfig(), nil)
	assert.NotNil(t, rl)
	rl.Stop()
}

func TestDefaultRateLimitConfig(t *testing.T) {
	cfg := DefaultRateLimitConfig()
	assert.Equal(t, 100, cfg.RequestsPerMinute)
	assert.Equal(t, time.Minute, cfg.WindowSize)
}

func BenchmarkMemoryRateLimiter_Allow(b *testing.B) {
	rl := newMemoryRateLimiter(DefaultRateLimitConfig())
	defer rl.Stop()

	keys := make([]string, 1000)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.Allow(context.Background(), keys[i%len(keys)])
	}
}

func BenchmarkMemoryRateLimiter_AllowConcurrent(b *testing.B) {
	rl := newMemoryRateLimiter(DefaultRateLimitConfig())
	defer rl.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			rl.Allow(context.Background(), fmt.Sprintf("key-%d", i%100))
			i++
		}
	})
}
