package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupConcurrentRouter(limiter *ConcurrentStreamLimiter) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if wallet := c.GetHeader("X-Test-Wallet"); wallet != "" {
			c.Set("wallet_address", wallet)
		}
		c.Next()
	})
	router.Use(limiter.Middleware())
	router.GET("/stream/:id/manifest.m3u8", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "id": c.Param("id")})
	})
	return router
}

func makeStreamRequest(router *gin.Engine, wallet, contentID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/stream/"+contentID+"/manifest.m3u8", http.NoBody)
	if wallet != "" {
		req.Header.Set("X-Test-Wallet", wallet)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestNewConcurrentStreamLimiter_Defaults(t *testing.T) {
	l := NewConcurrentStreamLimiter(3, time.Minute)
	defer l.Stop()

	assert.NotNil(t, l)
	assert.Equal(t, 3, l.maxPerUser)
	assert.Equal(t, time.Minute, l.ttl)
	assert.Equal(t, 5*time.Minute, l.cleanupTick)
}

func TestConcurrentStreamLimiter_WithinLimit(t *testing.T) {
	l := NewConcurrentStreamLimiter(3, time.Minute)
	defer l.Stop()
	router := setupConcurrentRouter(l)

	for i := 0; i < 3; i++ {
		w := makeStreamRequest(router, "0xWallet", "content1")
		assert.Equal(t, http.StatusOK, w.Code)
	}
}

func TestConcurrentStreamLimiter_ExceedsLimit(t *testing.T) {
	l := NewConcurrentStreamLimiter(2, time.Minute)
	defer l.Stop()
	router := setupConcurrentRouter(l)

	w := makeStreamRequest(router, "0xWallet", "content1")
	assert.Equal(t, http.StatusOK, w.Code)

	w = makeStreamRequest(router, "0xWallet", "content2")
	assert.Equal(t, http.StatusOK, w.Code)

	w = makeStreamRequest(router, "0xWallet", "content3")
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "CONCURRENT_LIMIT", resp["code"])
}

func TestConcurrentStreamLimiter_DifferentWallets(t *testing.T) {
	l := NewConcurrentStreamLimiter(1, time.Minute)
	defer l.Stop()
	router := setupConcurrentRouter(l)

	w := makeStreamRequest(router, "0xWallet1", "content1")
	assert.Equal(t, http.StatusOK, w.Code)

	w = makeStreamRequest(router, "0xWallet2", "content1")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestConcurrentStreamLimiter_NoWallet(t *testing.T) {
	l := NewConcurrentStreamLimiter(1, time.Minute)
	defer l.Stop()
	router := setupConcurrentRouter(l)

	w := makeStreamRequest(router, "", "content1")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestConcurrentStreamLimiter_NoContentID(t *testing.T) {
	l := NewConcurrentStreamLimiter(1, time.Minute)
	defer l.Stop()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xWallet")
		c.Next()
	})
	router.Use(l.Middleware())
	router.GET("/stream/manifest.m3u8", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/stream/manifest.m3u8", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestConcurrentStreamLimiter_SameContentRenewsTTL(t *testing.T) {
	l := NewConcurrentStreamLimiter(1, time.Minute)
	defer l.Stop()
	router := setupConcurrentRouter(l)

	w := makeStreamRequest(router, "0xWallet", "content1")
	assert.Equal(t, http.StatusOK, w.Code)

	w = makeStreamRequest(router, "0xWallet", "content1")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestConcurrentStreamLimiter_ExpiredEntries(t *testing.T) {
	l := NewConcurrentStreamLimiter(1, 50*time.Millisecond)
	defer l.Stop()
	router := setupConcurrentRouter(l)

	w := makeStreamRequest(router, "0xWallet", "content1")
	assert.Equal(t, http.StatusOK, w.Code)

	time.Sleep(100 * time.Millisecond)

	w = makeStreamRequest(router, "0xWallet", "content2")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestConcurrentStreamLimiter_PruneLocked(t *testing.T) {
	l := NewConcurrentStreamLimiter(5, time.Minute)
	defer l.Stop()

	l.mu.Lock()
	l.sessions["0xWallet"] = []sessionEntry{
		{ContentID: "expired1", ExpiresAt: time.Now().Add(-time.Hour)},
		{ContentID: "expired2", ExpiresAt: time.Now().Add(-time.Minute)},
		{ContentID: "active1", ExpiresAt: time.Now().Add(time.Hour)},
	}
	active := l.pruneLocked("0xWallet", time.Now())
	l.mu.Unlock()

	assert.Len(t, active, 1)
	assert.Equal(t, "active1", active[0].ContentID)
}

func TestConcurrentStreamLimiter_PruneLockedAllExpired(t *testing.T) {
	l := NewConcurrentStreamLimiter(5, time.Minute)
	defer l.Stop()

	l.mu.Lock()
	l.sessions["0xWallet"] = []sessionEntry{
		{ContentID: "expired1", ExpiresAt: time.Now().Add(-time.Hour)},
		{ContentID: "expired2", ExpiresAt: time.Now().Add(-time.Minute)},
	}
	active := l.pruneLocked("0xWallet", time.Now())
	l.mu.Unlock()

	assert.Len(t, active, 0)
}

func TestConcurrentStreamLimiter_PruneLockedNoEntries(t *testing.T) {
	l := NewConcurrentStreamLimiter(5, time.Minute)
	defer l.Stop()

	l.mu.Lock()
	active := l.pruneLocked("0xNonexistent", time.Now())
	l.mu.Unlock()

	assert.Len(t, active, 0)
}

func TestConcurrentStreamLimiter_Stop(t *testing.T) {
	l := NewConcurrentStreamLimiter(5, time.Minute)
	l.Stop()
}

func TestConcurrentStreamLimiter_ConcurrentAccess(t *testing.T) {
	l := NewConcurrentStreamLimiter(10, time.Minute)
	defer l.Stop()
	router := setupConcurrentRouter(l)

	var wg sync.WaitGroup
	results := make(chan int, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			w := makeStreamRequest(router, "0xWallet", "content1")
			results <- w.Code
		}(i)
	}
	wg.Wait()
	close(results)

	okCount := 0
	for code := range results {
		if code == http.StatusOK {
			okCount++
		}
	}
	assert.Equal(t, 20, okCount, "same content should always be allowed (renews TTL)")
}

func TestConcurrentStreamLimiter_ExceedsLimitResponse(t *testing.T) {
	l := NewConcurrentStreamLimiter(1, time.Minute)
	defer l.Stop()
	router := setupConcurrentRouter(l)

	makeStreamRequest(router, "0xWallet", "content1")
	w := makeStreamRequest(router, "0xWallet", "content2")

	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, float64(1), resp["max_streams"])
	assert.Equal(t, float64(1), resp["active_streams"])
	assert.Equal(t, "concurrent stream limit exceeded", resp["error"])
}
