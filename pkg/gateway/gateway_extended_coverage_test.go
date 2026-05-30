package gateway

import (
	"context"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type gwCovVerifier struct {
	mu           sync.RWMutex
	verifyFn     func(ctx context.Context, chainID int64, contract, tokenID, owner string) (bool, error)
	balanceFn    func(ctx context.Context, chainID int64, contract, owner string) (*big.Int, error)
	ownershipFn  func(ctx context.Context, chainID int64, contract, tokenID, owner string) (bool, error)
	collectionFn func(ctx context.Context, chainID int64, contract, owner string) (bool, error)
	nftInfoFn    func(ctx context.Context, chainID int64, contract, tokenID string) (*middleware.NFTMetadata, error)
}

func (v *gwCovVerifier) VerifyNFTOwnership(ctx context.Context, chainID int64, contract, tokenID, owner string) (bool, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.verifyFn != nil {
		return v.verifyFn(ctx, chainID, contract, tokenID, owner)
	}
	return false, nil
}

func (v *gwCovVerifier) GetNFTBalance(ctx context.Context, chainID int64, contract, owner string) (*big.Int, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.balanceFn != nil {
		return v.balanceFn(ctx, chainID, contract, owner)
	}
	return big.NewInt(0), nil
}

func (v *gwCovVerifier) VerifyNFTOwnershipAutoDetect(ctx context.Context, chainID int64, contract, tokenID, owner string) (bool, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.ownershipFn != nil {
		return v.ownershipFn(ctx, chainID, contract, tokenID, owner)
	}
	return false, nil
}

func (v *gwCovVerifier) VerifyNFTCollectionAutoDetect(ctx context.Context, chainID int64, contract, owner string) (bool, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.collectionFn != nil {
		return v.collectionFn(ctx, chainID, contract, owner)
	}
	return false, nil
}

func (v *gwCovVerifier) GetNFTInfo(ctx context.Context, chainID int64, contract, tokenID string) (*middleware.NFTMetadata, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.nftInfoFn != nil {
		return v.nftInfoFn(ctx, chainID, contract, tokenID)
	}
	return nil, nil
}

var _ middleware.NFTOwnershipChecker = (*gwCovVerifier)(nil)

type gwCovCache struct {
	mu    sync.RWMutex
	store map[string]middleware.NFTAccessEntry
}

func newGwCovCache() *gwCovCache {
	return &gwCovCache{store: make(map[string]middleware.NFTAccessEntry)}
}

func (c *gwCovCache) Get(_ context.Context, key string) (middleware.NFTAccessEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.store[key]
	return v, ok
}

func (c *gwCovCache) Set(_ context.Context, key string, entry middleware.NFTAccessEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = entry
}

func (c *gwCovCache) Delete(_ context.Context, key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.store, key)
}

func (c *gwCovCache) DeleteByPrefix(_ context.Context, prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.store {
		if strings.HasPrefix(k, prefix) {
			delete(c.store, k)
		}
	}
}

var _ middleware.NFTAccessCache = (*gwCovCache)(nil)

func TestGwCov_NFTRoutes_BalanceSuccess(t *testing.T) {
	verifier := &gwCovVerifier{
		balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
			return big.NewInt(3), nil
		},
	}
	r := gin.New()
	RegisterNFTRoutes(r, zap.NewNop(), verifier, nil, 1, 60*time.Second)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/nft?contract=0x1234567890abcdef1234567890abcdef12345678", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"balance":"3"`)
}

func TestGwCov_NFTRoutes_BalanceError(t *testing.T) {
	verifier := &gwCovVerifier{
		balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
			return nil, errors.New("RPC error")
		},
	}
	r := gin.New()
	RegisterNFTRoutes(r, zap.NewNop(), verifier, nil, 1, 60*time.Second)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/nft?contract=0x1234567890abcdef1234567890abcdef12345678", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGwCov_NFTRoutes_OwnershipSuccess(t *testing.T) {
	verifier := &gwCovVerifier{
		verifyFn: func(_ context.Context, _ int64, _ string, _ string, _ string) (bool, error) {
			return true, nil
		},
	}
	r := gin.New()
	RegisterNFTRoutes(r, zap.NewNop(), verifier, nil, 1, 60*time.Second)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/nft/1?contract=0x1234567890abcdef1234567890abcdef12345678", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"has_nft":true`)
}

func TestGwCov_NFTRoutes_OwnershipError(t *testing.T) {
	verifier := &gwCovVerifier{
		verifyFn: func(_ context.Context, _ int64, _ string, _ string, _ string) (bool, error) {
			return false, errors.New("RPC error")
		},
	}
	r := gin.New()
	RegisterNFTRoutes(r, zap.NewNop(), verifier, nil, 1, 60*time.Second)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/nft/1?contract=0x1234567890abcdef1234567890abcdef12345678", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGwCov_NFTRoutes_VerifyWithTokenID(t *testing.T) {
	verifier := &gwCovVerifier{
		verifyFn: func(_ context.Context, _ int64, _ string, _ string, _ string) (bool, error) {
			return true, nil
		},
	}
	r := gin.New()
	RegisterNFTRoutes(r, zap.NewNop(), verifier, nil, 1, 60*time.Second)
	w := httptest.NewRecorder()
	body := `{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18","token_id":"42"}`
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"has_nft":true`)
}

func TestGwCov_NFTRoutes_VerifyWithoutTokenID(t *testing.T) {
	verifier := &gwCovVerifier{
		balanceFn: func(_ context.Context, _ int64, _ string, _ string) (*big.Int, error) {
			return big.NewInt(2), nil
		},
	}
	r := gin.New()
	RegisterNFTRoutes(r, zap.NewNop(), verifier, nil, 1, 60*time.Second)
	w := httptest.NewRecorder()
	body := `{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"}`
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"has_nft":true`)
	assert.Contains(t, w.Body.String(), `"balance":"2"`)
}

func TestGwCov_NFTRoutes_VerifyCacheHit(t *testing.T) {
	verifier := &gwCovVerifier{}
	cache := newGwCovCache()
	cache.Set(context.Background(), "1:0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18:0x1234567890abcdef1234567890abcdef12345678:42", middleware.NFTAccessEntry{
		HasNFT:  true,
		Balance: big.NewInt(1),
		Expires: time.Now().Add(time.Hour),
	})
	r := gin.New()
	RegisterNFTRoutes(r, zap.NewNop(), verifier, cache, 1, 60*time.Second)
	w := httptest.NewRecorder()
	body := `{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18","token_id":"42"}`
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"cache_hit":true`)
}

func TestGwCov_NFTRoutes_VerifyCachePopulated(t *testing.T) {
	verifier := &gwCovVerifier{
		verifyFn: func(_ context.Context, _ int64, _ string, _ string, _ string) (bool, error) {
			return true, nil
		},
	}
	cache := newGwCovCache()
	r := gin.New()
	RegisterNFTRoutes(r, zap.NewNop(), verifier, cache, 1, 60*time.Second)
	w := httptest.NewRecorder()
	body := `{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18","token_id":"99"}`
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"cache_hit":false`)
	key := "1:0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18:0x1234567890abcdef1234567890abcdef12345678:99"
	entry, ok := cache.Get(context.Background(), key)
	assert.True(t, ok)
	assert.True(t, entry.HasNFT)
}

func TestGwCov_NFTRoutes_VerifyInvalidTokenID(t *testing.T) {
	verifier := &gwCovVerifier{}
	r := gin.New()
	RegisterNFTRoutes(r, zap.NewNop(), verifier, nil, 1, 60*time.Second)
	w := httptest.NewRecorder()
	body := `{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18","token_id":"not-a-number"}`
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGwCov_NFTRoutes_VerifyMissingContract(t *testing.T) {
	verifier := &gwCovVerifier{}
	r := gin.New()
	RegisterNFTRoutes(r, zap.NewNop(), verifier, nil, 1, 60*time.Second)
	w := httptest.NewRecorder()
	body := `{"wallet":"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"}`
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGwCov_NFTRoutes_VerifyInvalidJSON(t *testing.T) {
	verifier := &gwCovVerifier{}
	r := gin.New()
	RegisterNFTRoutes(r, zap.NewNop(), verifier, nil, 1, 60*time.Second)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGwCov_NFTRoutes_VerifyError(t *testing.T) {
	verifier := &gwCovVerifier{
		verifyFn: func(_ context.Context, _ int64, _ string, _ string, _ string) (bool, error) {
			return false, errors.New("RPC error")
		},
	}
	r := gin.New()
	RegisterNFTRoutes(r, zap.NewNop(), verifier, nil, 1, 60*time.Second)
	w := httptest.NewRecorder()
	body := `{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18","token_id":"1"}`
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGwCov_NFTRoutes_ChainIDOverride(t *testing.T) {
	var capturedChainID int64
	verifier := &gwCovVerifier{
		balanceFn: func(_ context.Context, chainID int64, _ string, _ string) (*big.Int, error) {
			capturedChainID = chainID
			return big.NewInt(1), nil
		},
	}
	r := gin.New()
	RegisterNFTRoutes(r, zap.NewNop(), verifier, nil, 1, 60*time.Second)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/nft?contract=0x1234567890abcdef1234567890abcdef12345678&chain_id=137", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, int64(137), capturedChainID)
}

func TestGwCov_NFTRoutes_DefaultChainID(t *testing.T) {
	var capturedChainID int64
	verifier := &gwCovVerifier{
		balanceFn: func(_ context.Context, chainID int64, _ string, _ string) (*big.Int, error) {
			capturedChainID = chainID
			return big.NewInt(1), nil
		},
	}
	r := gin.New()
	RegisterNFTRoutes(r, zap.NewNop(), verifier, nil, 42, 60*time.Second)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/nft?contract=0x1234567890abcdef1234567890abcdef12345678", http.NoBody)
	req.Header.Set("X-Wallet-Address", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, int64(42), capturedChainID)
}

func TestGwCov_StreamingRoutes_InvalidContentID(t *testing.T) {
	authSvc := service.NewAuthService("test-secret-that-is-at-least-32-chars", nil)
	r := gin.New()
	cache := NewStreamingCache()
	limiter := newStreamLimiter(100)
	RegisterStreamingRoutes(r, zap.NewNop(), authSvc, nil, nil, limiter, cache)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/streaming/bad%20id/manifest.m3u8", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGwCov_StreamingRoutes_LimiterFull(t *testing.T) {
	authSvc := service.NewAuthService("test-secret-that-is-at-least-32-chars", nil)
	r := gin.New()
	limiter := newStreamLimiter(1)
	limiter.tryAcquire()
	cache := NewStreamingCache()
	RegisterStreamingRoutes(r, zap.NewNop(), authSvc, nil, nil, limiter, cache)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/streaming/test-content/manifest.m3u8", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestGwCov_StreamingSegmentRoute_NoToken(t *testing.T) {
	authSvc := service.NewAuthService("test-secret-that-is-at-least-32-chars", nil)
	r := gin.New()
	cache := NewStreamingCache()
	limiter := newStreamLimiter(100)
	RegisterStreamingSegmentRoute(r, zap.NewNop(), authSvc, nil, limiter, cache)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/streaming/test/segment/seg0.ts", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGwCov_StreamingSegmentRoute_InvalidContentID(t *testing.T) {
	authSvc := service.NewAuthService("test-secret-that-is-at-least-32-chars", nil)
	r := gin.New()
	cache := NewStreamingCache()
	limiter := newStreamLimiter(100)
	RegisterStreamingSegmentRoute(r, zap.NewNop(), authSvc, nil, limiter, cache)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/streaming/bad%20id/segment/seg0.ts?playback_token=test", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGwCov_StreamingSegmentRoute_InvalidSegmentName(t *testing.T) {
	authSvc := service.NewAuthService("test-secret-that-is-at-least-32-chars", nil)
	token, err := authSvc.GeneratePlaybackToken(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "test-content", "", "", 1, 2*time.Minute, "")
	require.NoError(t, err)
	r := gin.New()
	cache := NewStreamingCache()
	limiter := newStreamLimiter(100)
	RegisterStreamingSegmentRoute(r, zap.NewNop(), authSvc, nil, limiter, cache)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/streaming/test-content/segment/seg0.txt?playback_token="+token, http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGwCov_StreamingSegmentRoute_NoObjStorage(t *testing.T) {
	authSvc := service.NewAuthService("test-secret-that-is-at-least-32-chars", nil)
	token, err := authSvc.GeneratePlaybackToken(context.Background(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", "test-content", "", "", 1, 2*time.Minute, "")
	require.NoError(t, err)
	r := gin.New()
	cache := NewStreamingCache()
	limiter := newStreamLimiter(100)
	RegisterStreamingSegmentRoute(r, zap.NewNop(), authSvc, nil, limiter, cache)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/streaming/test-content/segment/seg0.ts?playback_token="+token, http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGwCov_ProvideWeb3Service_Injected(t *testing.T) {
	cfg := &config.Config{Web3: config.Web3Config{ChainID: 1}}
	rc := &RouterConfig{
		Web3Service: &service.Web3Service{},
	}
	svc, err := provideWeb3Service(rc, cfg, zap.NewNop())
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestGwCov_ProvideTokenBlacklist_NilRedis(t *testing.T) {
	res := &AppResources{}
	bl := provideTokenBlacklist(zap.NewNop(), nil, res)
	assert.NotNil(t, bl)
	assert.Nil(t, res.TokenBlacklist)
}

func TestGwCov_ProvideUploadService_NilDB(t *testing.T) {
	rc := &RouterConfig{}
	cfg := &config.Config{}
	svc := provideUploadService(rc, cfg, zap.NewNop(), nil, nil, nil)
	assert.Nil(t, svc)
}

func TestGwCov_ProvideUploadService_NilObjStorage(t *testing.T) {
	rc := &RouterConfig{}
	cfg := &config.Config{}
	svc := provideUploadService(rc, cfg, zap.NewNop(), nil, nil, nil)
	assert.Nil(t, svc)
}

func TestGwCov_ProvideContentService_WithDB(t *testing.T) {
	rc := &RouterConfig{}
	svc := provideContentService(rc, nil, zap.NewNop())
	assert.Nil(t, svc)
}

func TestGwCov_ProvideContentService_Injected(t *testing.T) {
	rc := &RouterConfig{
		ContentService: &service.ContentService{},
	}
	svc := provideContentService(rc, nil, zap.NewNop())
	assert.NotNil(t, svc)
}

func TestGwCov_ProvideObjectStorage_Injected(t *testing.T) {
	rc := &RouterConfig{
		SegmentStorage: &mockSegmentStorage{},
	}
	cfg := &config.Config{Storage: config.StorageConfig{}}
	svc := provideObjectStorage(rc, cfg, zap.NewNop(), &AppResources{})
	assert.NotNil(t, svc)
}

func TestGwCov_ProvideOTelTracing_EmptyEndpoint(t *testing.T) {
	cfg := &config.Config{Monitoring: config.MonitoringConfig{}}
	res := &AppResources{}
	provideOTelTracing(cfg, zap.NewNop(), res)
	assert.Nil(t, res.OTelShutdown)
}

func TestGwCov_ParseChallengeTTL_Default(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{}}
	ttl := parseChallengeTTL(cfg)
	assert.Equal(t, 5*time.Minute, ttl)
}

func TestGwCov_ParseChallengeTTL_Custom(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{NonceExpiry: "10m"}}
	ttl := parseChallengeTTL(cfg)
	assert.Equal(t, 10*time.Minute, ttl)
}

func TestGwCov_ParseChallengeTTL_Invalid(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{NonceExpiry: "not-a-duration"}}
	ttl := parseChallengeTTL(cfg)
	assert.Equal(t, 5*time.Minute, ttl)
}

func TestGwCov_ParseChallengeTTL_Negative(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{NonceExpiry: "-5m"}}
	ttl := parseChallengeTTL(cfg)
	assert.Equal(t, 5*time.Minute, ttl)
}

func TestGwCov_ParseChallengeTTL_Zero(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{NonceExpiry: "0s"}}
	ttl := parseChallengeTTL(cfg)
	assert.Equal(t, 5*time.Minute, ttl)
}

func TestGwCov_AbortWithErrorDetail_5xx(t *testing.T) {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, "internal error", "sensitive detail")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal error")
	assert.NotContains(t, w.Body.String(), "sensitive detail")
}

func TestGwCov_AbortWithErrorDetail_4xx(t *testing.T) {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		abortWithErrorDetail(c, http.StatusBadRequest, ErrInvalidRequest, "bad request", "field X is invalid")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "bad request")
	assert.Contains(t, w.Body.String(), "field X is invalid")
}

func TestGwCov_AbortWithValidationError(t *testing.T) {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		abortWithValidationError(c, map[string]string{"field": "required"})
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "validation")
}

func TestGwCov_AbortWithValidationError_WithErrorMessage(t *testing.T) {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		abortWithValidationError(c, map[string]string{"_error": "custom error", "field": "required"})
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "custom error")
}

func TestGwCov_Respond_WithRequestID(t *testing.T) {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		c.Set("request_id", "req-123")
		respond(c, http.StatusOK, gin.H{"data": "test"})
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "request_id")
	assert.Contains(t, w.Body.String(), "req-123")
}

func TestGwCov_Respond_NonMapData(t *testing.T) {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		c.Set("request_id", "req-123")
		respond(c, http.StatusOK, "just a string")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGwCov_InternalErrMsg_NilError(t *testing.T) {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		msg := internalErrMsg(c, nil)
		assert.Equal(t, "an internal error occurred", msg)
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	r.ServeHTTP(w, req)
}

func TestGwCov_APIError_WithDetail(t *testing.T) {
	err := APIError{Error: "test", Code: "TEST"}
	result := err.WithDetail("some detail")
	assert.Equal(t, "some detail", result.Detail)
	assert.Equal(t, "", err.Detail)
}

func TestGwCov_IsValidContentID(t *testing.T) {
	tests := []struct {
		id    string
		valid bool
	}{
		{"abc123", true},
		{"test-content_123", true},
		{"", false},
		{"bad id", false},
		{"a/b", false},
		{strings.Repeat("a", 257), false},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			assert.Equal(t, tt.valid, isValidContentID(tt.id))
		})
	}
}

func TestGwCov_ValidateSegmentName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"seg0.ts", true},
		{"720p/seg0.ts", true},
		{"../etc/passwd.ts", false},
		{"seg0.txt", false},
		{"seg0\\ts", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, validateSegmentName(tt.name))
		})
	}
}

func TestGwCov_ExtractSegmentNumber(t *testing.T) {
	tests := []struct {
		segName string
		num     int
	}{
		{"seg0.ts", 0},
		{"seg123.ts", 123},
		{"720p/seg5.ts", 5},
		{"quality/seg99.ts", 99},
	}
	for _, tt := range tests {
		t.Run(tt.segName, func(t *testing.T) {
			assert.Equal(t, tt.num, extractSegmentNumber(tt.segName))
		})
	}
}

func TestGwCov_ExtractPlaybackToken(t *testing.T) {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		token := extractPlaybackToken(c)
		c.String(http.StatusOK, token)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test?playback_token=query-token", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, "query-token", w.Body.String())

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer bearer-token")
	r.ServeHTTP(w, req)
	assert.Equal(t, "bearer-token", w.Body.String())
}

func TestGwCov_BuildSegmentCandidates(t *testing.T) {
	cache := NewStreamingCache()
	candidates := buildSegmentCandidates("content1", "seg0.ts", "", cache)
	assert.NotEmpty(t, candidates)

	cache.SetSegmentIndex("content1", map[string][]string{
		"720p":  {"seg0.ts", "seg1.ts"},
		"1080p": {"seg0.ts", "seg1.ts"},
	})
	candidates = buildSegmentCandidates("content1", "seg0.ts", "720p", cache)
	assert.Len(t, candidates, 3)
	found720 := false
	for _, c := range candidates {
		if c.prio == 2 {
			found720 = true
		}
	}
	assert.True(t, found720)
}

func TestGwCov_NFTAccessCache_SetEviction(t *testing.T) {
	cache := NewNFTAccessCache()
	defer cache.Stop()
	smallCache := &NFTAccessCache{
		entries: make(map[string]CachedNFTAccess),
		maxSize: 3,
		stopCh:  make(chan struct{}),
	}
	defer close(smallCache.stopCh)
	smallCache.Set("key1", CachedNFTAccess{ExpiresAt: time.Now().Add(time.Hour)})
	smallCache.Set("key2", CachedNFTAccess{ExpiresAt: time.Now().Add(time.Hour)})
	smallCache.Set("key3", CachedNFTAccess{ExpiresAt: time.Now().Add(time.Hour)})
	smallCache.Set("key4", CachedNFTAccess{ExpiresAt: time.Now().Add(time.Hour)})
	assert.LessOrEqual(t, len(smallCache.entries), 3)
}

func TestGwCov_NFTAccessCache_GetExpired(t *testing.T) {
	cache := NewNFTAccessCache()
	defer cache.Stop()
	cache.Set("expired", CachedNFTAccess{ExpiresAt: time.Now().Add(-time.Hour)})
	_, ok := cache.Get("expired")
	assert.False(t, ok)
}

func TestGwCov_NFTAccessCache_Delete(t *testing.T) {
	cache := NewNFTAccessCache()
	defer cache.Stop()
	cache.Set("key1", CachedNFTAccess{ExpiresAt: time.Now().Add(time.Hour)})
	cache.Delete("key1")
	_, ok := cache.Get("key1")
	assert.False(t, ok)
}

func TestGwCov_NFTAccessCache_DeleteByPrefix(t *testing.T) {
	cache := NewNFTAccessCache()
	defer cache.Stop()
	cache.Set("1:0xabc:0xdef:1", CachedNFTAccess{ExpiresAt: time.Now().Add(time.Hour)})
	cache.Set("1:0xabc:0xdef:2", CachedNFTAccess{ExpiresAt: time.Now().Add(time.Hour)})
	cache.Set("2:0xabc:0xdef:1", CachedNFTAccess{ExpiresAt: time.Now().Add(time.Hour)})
	cache.DeleteByPrefix("1:")
	_, ok1 := cache.Get("1:0xabc:0xdef:1")
	assert.False(t, ok1)
	_, ok2 := cache.Get("2:0xabc:0xdef:1")
	assert.True(t, ok2)
}

type mockSegmentStorage struct{}

func (m *mockSegmentStorage) Upload(_ context.Context, _, _ string, _ []byte) error {
	return nil
}
func (m *mockSegmentStorage) UploadStream(_ context.Context, _, _ string, _ io.Reader, _ int64) error {
	return nil
}
func (m *mockSegmentStorage) UploadWithContentType(_ context.Context, _, _ string, _ []byte, _ string) error {
	return nil
}
func (m *mockSegmentStorage) UploadStreamWithContentType(_ context.Context, _, _ string, _ io.Reader, _ int64, _ string) error {
	return nil
}
func (m *mockSegmentStorage) Download(_ context.Context, _, _ string) ([]byte, error) {
	return nil, nil
}
func (m *mockSegmentStorage) DownloadStream(_ context.Context, _, _ string) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockSegmentStorage) Delete(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockSegmentStorage) ListObjects(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}
func (m *mockSegmentStorage) Exists(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

var _ service.SegmentStorage = (*mockSegmentStorage)(nil)
