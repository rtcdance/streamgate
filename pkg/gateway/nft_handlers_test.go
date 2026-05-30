package gateway

import (
	"bytes"
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewNFTAccessCache(t *testing.T) {
	cache := NewNFTAccessCache()
	assert.NotNil(t, cache)
	cache.Stop()
}

func TestNFTAccessCache_GetSet(t *testing.T) {
	cache := NewNFTAccessCache()
	defer cache.Stop()

	_, ok := cache.Get("key1")
	assert.False(t, ok)

	cache.Set("key1", CachedNFTAccess{
		HasNFT:    true,
		ExpiresAt: time.Now().Add(time.Hour),
	})

	entry, ok := cache.Get("key1")
	assert.True(t, ok)
	assert.True(t, entry.HasNFT)
}

func TestNFTAccessCache_Get_Expired(t *testing.T) {
	cache := NewNFTAccessCache()
	defer cache.Stop()

	cache.Set("key1", CachedNFTAccess{
		HasNFT:    true,
		ExpiresAt: time.Now().Add(-time.Hour),
	})

	_, ok := cache.Get("key1")
	assert.False(t, ok)
}

func TestNFTAccessCache_Delete(t *testing.T) {
	cache := NewNFTAccessCache()
	defer cache.Stop()

	cache.Set("key1", CachedNFTAccess{
		HasNFT:    true,
		ExpiresAt: time.Now().Add(time.Hour),
	})

	cache.Delete("key1")

	_, ok := cache.Get("key1")
	assert.False(t, ok)
}

func TestNFTAccessCache_DeleteByPrefix(t *testing.T) {
	cache := NewNFTAccessCache()
	defer cache.Stop()

	cache.Set("1:0xabc:0xcontract:1", CachedNFTAccess{HasNFT: true, ExpiresAt: time.Now().Add(time.Hour)})
	cache.Set("1:0xabc:0xcontract:2", CachedNFTAccess{HasNFT: false, ExpiresAt: time.Now().Add(time.Hour)})
	cache.Set("2:0xdef:0xcontract:1", CachedNFTAccess{HasNFT: true, ExpiresAt: time.Now().Add(time.Hour)})

	cache.DeleteByPrefix("1:0xabc")

	_, ok := cache.Get("1:0xabc:0xcontract:1")
	assert.False(t, ok)
	_, ok = cache.Get("1:0xabc:0xcontract:2")
	assert.False(t, ok)
	_, ok = cache.Get("2:0xdef:0xcontract:1")
	assert.True(t, ok)
}

func TestNFTAccessCache_Eviction(t *testing.T) {
	cache := NewNFTAccessCache()
	cache.maxSize = 5
	defer cache.Stop()

	for i := 0; i < 10; i++ {
		cache.Set(string(rune('a'+i)), CachedNFTAccess{
			HasNFT:    true,
			ExpiresAt: time.Now().Add(time.Hour),
		})
	}

	assert.LessOrEqual(t, len(cache.entries), cache.maxSize+cache.maxSize/10)
}

func TestNFTAccessCacheAdapter_Get(t *testing.T) {
	cache := NewNFTAccessCache()
	defer cache.Stop()

	adapter := &NFTAccessCacheAdapter{Cache: cache}

	_, ok := adapter.Get(context.TODO(), "key1")
	assert.False(t, ok)

	adapter.Set(context.TODO(), "key1", middleware.NFTAccessEntry{
		HasNFT:  true,
		Balance: big.NewInt(1),
		Expires: time.Now().Add(time.Hour),
	})

	entry, ok := adapter.Get(context.TODO(), "key1")
	assert.True(t, ok)
	assert.True(t, entry.HasNFT)
}

func TestNFTAccessCacheAdapter_Delete(t *testing.T) {
	cache := NewNFTAccessCache()
	defer cache.Stop()

	adapter := &NFTAccessCacheAdapter{Cache: cache}
	adapter.Set(context.TODO(), "key1", middleware.NFTAccessEntry{
		HasNFT:  true,
		Balance: big.NewInt(1),
		Expires: time.Now().Add(time.Hour),
	})
	adapter.Delete(context.TODO(), "key1")

	_, ok := adapter.Get(context.TODO(), "key1")
	assert.False(t, ok)
}

func TestNFTAccessCacheAdapter_DeleteByPrefix(t *testing.T) {
	cache := NewNFTAccessCache()
	defer cache.Stop()

	adapter := &NFTAccessCacheAdapter{Cache: cache}
	adapter.Set(context.TODO(), "prefix:key1", middleware.NFTAccessEntry{
		HasNFT:  true,
		Balance: big.NewInt(1),
		Expires: time.Now().Add(time.Hour),
	})
	adapter.DeleteByPrefix(context.TODO(), "prefix:")

	_, ok := adapter.Get(context.TODO(), "prefix:key1")
	assert.False(t, ok)
}

func TestNFTAccessCache_EvictExpired_Empty(t *testing.T) {
	cache := NewNFTAccessCache()
	defer cache.Stop()

	cache.evictExpired()
	assert.Empty(t, cache.entries)
}

func TestNFTAccessCache_EvictExpired(t *testing.T) {
	cache := NewNFTAccessCache()
	defer cache.Stop()

	cache.Set("expired1", CachedNFTAccess{HasNFT: true, ExpiresAt: time.Now().Add(-time.Hour)})
	cache.Set("expired2", CachedNFTAccess{HasNFT: false, ExpiresAt: time.Now().Add(-30 * time.Minute)})
	cache.Set("valid1", CachedNFTAccess{HasNFT: true, ExpiresAt: time.Now().Add(time.Hour)})
	cache.Set("valid2", CachedNFTAccess{HasNFT: false, ExpiresAt: time.Now().Add(30 * time.Minute)})

	assert.Len(t, cache.entries, 4)

	cache.evictExpired()

	assert.Len(t, cache.entries, 2)
	_, ok := cache.Get("valid1")
	assert.True(t, ok)
	_, ok = cache.Get("valid2")
	assert.True(t, ok)
	_, ok = cache.Get("expired1")
	assert.False(t, ok)
	_, ok = cache.Get("expired2")
	assert.False(t, ok)
}

func TestNFTAccessCache_CleanupLoop_Stop(t *testing.T) {
	cache := NewNFTAccessCache()
	cache.Set("test", CachedNFTAccess{HasNFT: true, ExpiresAt: time.Now().Add(-time.Hour)})

	done := make(chan struct{})
	go func() {
		cache.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanupLoop did not exit within 1s after Stop()")
	}
}

// mockNFTOwnershipChecker implements middleware.NFTOwnershipChecker for tests.
type mockNFTOwnershipChecker struct{}

func (m *mockNFTOwnershipChecker) VerifyNFTOwnership(_ context.Context, _ int64, _, _, _ string) (bool, error) {
	return true, nil
}

func (m *mockNFTOwnershipChecker) GetNFTBalance(_ context.Context, _ int64, _, _ string) (*big.Int, error) {
	return big.NewInt(1), nil
}

func (m *mockNFTOwnershipChecker) VerifyNFTOwnershipAutoDetect(_ context.Context, _ int64, _, _, _ string) (bool, error) {
	return true, nil
}

func (m *mockNFTOwnershipChecker) VerifyNFTCollectionAutoDetect(_ context.Context, _ int64, _, _ string) (bool, error) {
	return true, nil
}

func (m *mockNFTOwnershipChecker) GetNFTInfo(_ context.Context, _ int64, _, _ string) (*middleware.NFTMetadata, error) {
	return nil, nil
}

// mockNFTCache implements middleware.NFTAccessCache for tests.
type mockNFTCache struct{}

func (m *mockNFTCache) Get(_ context.Context, _ string) (middleware.NFTAccessEntry, bool) {
	return middleware.NFTAccessEntry{}, false
}
func (m *mockNFTCache) Set(_ context.Context, _ string, _ middleware.NFTAccessEntry) {}
func (m *mockNFTCache) Delete(_ context.Context, _ string)                           {}
func (m *mockNFTCache) DeleteByPrefix(_ context.Context, _ string)                   {}

func TestRegisterNFTRoutes_RoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	RegisterNFTRoutes(r, zap.NewNop(), &mockNFTOwnershipChecker{}, &mockNFTCache{}, 1, time.Minute)

	tests := []struct {
		method string
		path   string
		name   string
		body   string
	}{
		{method: "GET", path: APIPrefix + "/nft?contract=0x1234567890abcdef1234567890abcdef12345678", name: "list nft balance"},
		{method: "GET", path: APIPrefix + "/nft/1?contract=0x1234567890abcdef1234567890abcdef12345678", name: "get nft by token id"},
		{method: "POST", path: APIPrefix + "/nft/verify", name: "verify nft ownership", body: `{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x1234567890abcdef1234567890abcdef12345678","token_id":"1"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			var req *http.Request
			if tt.body != "" {
				req = httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tt.method, tt.path, http.NoBody)
			}
			r.ServeHTTP(w, req)
			assert.NotEqual(t, http.StatusNotFound, w.Code, "route should be registered")
		})
	}
}
