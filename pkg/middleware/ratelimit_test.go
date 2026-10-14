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
	"github.com/go-redis/redis/v8"
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

func TestRateLimitMiddleware_ServiceHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	cfg := RateLimitConfig{
		RequestsPerMinute: 2,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}
	rl := NewRateLimiter(cfg, nil)
	defer rl.Stop()

	svc := &Service{rateLimiter: rl}
	router.Use(svc.RateLimitMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.RemoteAddr = "10.0.0.1:8080"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req2.RemoteAddr = "10.0.0.1:8080"
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	req3 := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req3.RemoteAddr = "10.0.0.1:8080"
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusTooManyRequests, w3.Code)
}

func TestRateLimitMiddleware_WithWalletAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	cfg := RateLimitConfig{
		RequestsPerMinute: 2,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}
	rl := NewRateLimiter(cfg, nil)
	defer rl.Stop()

	svc := &Service{rateLimiter: rl}
	router.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xWallet123")
		c.Next()
	})
	router.Use(svc.RateLimitMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

type mockRedisClient struct {
	evalFn func(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd
}

func (m *mockRedisClient) Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	return m.evalFn(ctx, script, keys, args...)
}

func TestRedisRateLimiter_Allow_Success(t *testing.T) {
	cfg := DefaultRateLimitConfig()
	fallback := newMemoryRateLimiter(cfg)
	defer fallback.Stop()

	client := &mockRedisClient{
		evalFn: func(_ context.Context, _ string, _ []string, _ ...interface{}) *redis.Cmd {
			cmd := redis.NewCmdResult(int64(1), nil)
			return cmd
		},
	}

	rl := newRedisRateLimiter(cfg, client, fallback)
	assert.True(t, rl.Allow(context.Background(), "test-key"))
	rl.Stop()
}

func TestRedisRateLimiter_Allow_Denied(t *testing.T) {
	cfg := DefaultRateLimitConfig()
	fallback := newMemoryRateLimiter(cfg)
	defer fallback.Stop()

	client := &mockRedisClient{
		evalFn: func(_ context.Context, _ string, _ []string, _ ...interface{}) *redis.Cmd {
			cmd := redis.NewCmdResult(int64(0), nil)
			return cmd
		},
	}

	rl := newRedisRateLimiter(cfg, client, fallback)
	assert.False(t, rl.Allow(context.Background(), "test-key"))
	rl.Stop()
}

func TestRedisRateLimiter_Allow_ErrorFallsBack(t *testing.T) {
	cfg := DefaultRateLimitConfig()
	fallback := newMemoryRateLimiter(cfg)
	defer fallback.Stop()

	client := &mockRedisClient{
		evalFn: func(_ context.Context, _ string, _ []string, _ ...interface{}) *redis.Cmd {
			cmd := redis.NewCmdResult(nil, context.DeadlineExceeded)
			return cmd
		},
	}

	rl := newRedisRateLimiter(cfg, client, fallback)
	assert.True(t, rl.Allow(context.Background(), "test-key"))
	rl.Stop()
}

func TestRedisRateLimiter_Allow_WrongTypeFallsBack(t *testing.T) {
	cfg := DefaultRateLimitConfig()
	fallback := newMemoryRateLimiter(cfg)
	defer fallback.Stop()

	client := &mockRedisClient{
		evalFn: func(_ context.Context, _ string, _ []string, _ ...interface{}) *redis.Cmd {
			cmd := redis.NewCmdResult("not-int64", nil)
			return cmd
		},
	}

	rl := newRedisRateLimiter(cfg, client, fallback)
	assert.True(t, rl.Allow(context.Background(), "test-key"))
	rl.Stop()
}

func TestMemoryRateLimiter_Eviction(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 100,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}
	rl := newMemoryRateLimiter(cfg)
	rl.maxClients = 3
	defer rl.Stop()

	for i := 0; i < 5; i++ {
		assert.True(t, rl.Allow(context.Background(), fmt.Sprintf("key-%d", i)))
	}
	assert.LessOrEqual(t, len(rl.clients), rl.maxClients)
}

func TestNewRateLimiter_WithRedis(t *testing.T) {
	cfg := DefaultRateLimitConfig()
	client := &mockRedisClient{
		evalFn: func(_ context.Context, _ string, _ []string, _ ...interface{}) *redis.Cmd {
			return redis.NewCmdResult(int64(1), nil)
		},
	}
	rl := NewRateLimiter(cfg, client)
	assert.NotNil(t, rl)
	rl.Stop()
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

func TestMemoryRateLimiter_RetryAfter(t *testing.T) {
	cfg := RateLimitConfig{RequestsPerMinute: 1, WindowSize: time.Minute, CleanupInterval: 5 * time.Minute}
	rl := newMemoryRateLimiter(cfg)
	defer rl.Stop()

	// Before any window exists the conservative wait is the whole window.
	assert.Equal(t, cfg.WindowSize, rl.RetryAfter(context.Background(), "no-window"))

	assert.True(t, rl.Allow(context.Background(), "key"))
	assert.False(t, rl.Allow(context.Background(), "key"))

	ra := rl.RetryAfter(context.Background(), "key")
	assert.Greater(t, ra, time.Duration(0))
	assert.LessOrEqual(t, ra, cfg.WindowSize)
}

func TestRedisRateLimiter_RetryAfter(t *testing.T) {
	cfg := RateLimitConfig{RequestsPerMinute: 1, WindowSize: time.Minute, CleanupInterval: 5 * time.Minute}
	fallback := newMemoryRateLimiter(cfg)
	defer fallback.Stop()
	client := &mockRedisClient{
		evalFn: func(_ context.Context, _ string, _ []string, _ ...interface{}) *redis.Cmd {
			return redis.NewCmdResult(int64(0), nil)
		},
	}
	rl := newRedisRateLimiter(cfg, client, fallback)
	rl.Stop()

	ra := rl.RetryAfter(context.Background(), "key")
	assert.Greater(t, ra, time.Duration(0))
	assert.LessOrEqual(t, ra, cfg.WindowSize)
}

// stubLimiterWithoutRetryAfter implements RateLimiter but not retryAfterer, to
// verify writeRateLimitResponse degrades gracefully (no Retry-After header).
type stubLimiterWithoutRetryAfter struct{}

func (stubLimiterWithoutRetryAfter) Allow(context.Context, string) bool { return false }
func (stubLimiterWithoutRetryAfter) Stop()                              {}

func TestWriteRateLimitResponse_NoRetryAfterWhenLimiterLacksCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/test", http.NoBody)

	writeRateLimitResponse(c, stubLimiterWithoutRetryAfter{}, "key")

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Empty(t, w.Header().Get("Retry-After"))
}

func TestRateLimitMiddleware_RetryAfterHeaderOn429(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := RateLimitConfig{RequestsPerMinute: 1, WindowSize: time.Minute, CleanupInterval: 5 * time.Minute}
	rl, handler := (&Service{rateLimiter: NewRateLimiter(cfg, nil)}).RateLimitMiddlewareWithConfig(cfg)
	defer rl.Stop()

	router := gin.New()
	router.Use(handler)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	makeRequest := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
		req.RemoteAddr = "10.0.0.9:8080"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	assert.Equal(t, http.StatusOK, makeRequest().Code)
	w := makeRequest()
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	retryAfter := w.Header().Get("Retry-After")
	assert.NotEmpty(t, retryAfter, "429 response must carry Retry-After (SG-07 precondition)")
	secs := 0
	_, err := fmt.Sscanf(retryAfter, "%d", &secs)
	assert.NoError(t, err, "Retry-After must be an integer number of seconds, got %q", retryAfter)
	assert.GreaterOrEqual(t, secs, 1)
	assert.LessOrEqual(t, secs, int(cfg.WindowSize.Seconds()))
}

// TestRateLimitMiddleware_MediaRoutesAreNotIPMerged pins the SG-07/SG-14 fix:
// the IP-keyed global limiter must NOT consume budget for streaming media
// routes. It runs before auth, so the wallet is unknown and every viewer
// behind one address shares a single "IP:path" bucket — merging all viewers
// into one budget. The media budget belongs to MediaRateLimitMiddleware,
// which runs after auth and keys by wallet + route template.
func TestRateLimitMiddleware_MediaRoutesAreNotIPMerged(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := RateLimitConfig{RequestsPerMinute: 3, WindowSize: time.Minute, CleanupInterval: 5 * time.Minute}
	rl, handler := (&Service{rateLimiter: NewRateLimiter(cfg, nil)}).RateLimitMiddlewareWithConfig(cfg)
	defer rl.Stop()

	router := gin.New()
	router.Use(handler)
	router.GET("/api/v1/streaming/:id/manifest.m3u8", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/api/v1/content", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	get := func(path, ip string) int {
		req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		req.RemoteAddr = ip + ":8080"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}

	// Well past the global budget: media must still answer 200 because its
	// budget is enforced later, per wallet.
	for i := 0; i < cfg.RequestsPerMinute+5; i++ {
		assert.Equal(t, http.StatusOK, get("/api/v1/streaming/c1/manifest.m3u8", "10.0.0.7"),
			"media request %d must not be limited by the IP-keyed global limiter", i+1)
	}
	// Non-media routes keep the global limiter's protection.
	for i := 0; i < cfg.RequestsPerMinute; i++ {
		assert.Equal(t, http.StatusOK, get("/api/v1/content", "10.0.0.7"), "request %d", i+1)
	}
	assert.Equal(t, http.StatusTooManyRequests, get("/api/v1/content", "10.0.0.7"),
		"non-media route past the budget must still be 429")
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
