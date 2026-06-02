package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func newGatewayTestAuthService() *service.AuthService {
	return service.NewAuthServiceWithDeps(
		"test-secret-that-is-at-least-32-chars",
		nil,
		nil,
		storage.NewMemoryChallengeStore(),
		0,
		nil,
	)
}

func TestTranscodeExt_SubmitValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewTranscodingService(nil, nil)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xOwner1234567890abcdef1234567890abcdef12")
		c.Next()
	})
	RegisterTranscodingRoutes(r, zap.NewNop(), svc)

	t.Run("missing content_id", func(t *testing.T) {
		body := `{"profile":"720p","input_url":"https://example.com/video.mp4"}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/submit", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing profile", func(t *testing.T) {
		body := `{"content_id":"c1","input_url":"https://example.com/video.mp4"}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/submit", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing input_url", func(t *testing.T) {
		body := `{"content_id":"c1","profile":"720p"}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/submit", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid priority negative", func(t *testing.T) {
		body := `{"content_id":"c1","profile":"720p","input_url":"https://example.com/video.mp4","priority":-1}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/submit", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid priority too high", func(t *testing.T) {
		body := `{"content_id":"c1","profile":"720p","input_url":"https://example.com/video.mp4","priority":11}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/submit", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/submit", strings.NewReader("not json"))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestTranscodeExt_ProfilesEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewTranscodingService(nil, nil)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xOwner")
		c.Next()
	})
	RegisterTranscodingRoutes(r, zap.NewNop(), svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/transcode/profiles", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	profiles, ok := resp["profiles"]
	assert.True(t, ok)
	assert.NotNil(t, profiles)
}

func TestTranscodeExt_CancelNilService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xAny")
		c.Next()
	})
	RegisterTranscodingRoutes(r, zap.NewNop(), nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/cancel/some-id", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestTranscodeExt_SubmitNilService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xAny")
		c.Next()
	})
	RegisterTranscodingRoutes(r, zap.NewNop(), nil)

	body := `{"content_id":"c1","profile":"720p","input_url":"https://example.com/video.mp4"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/transcode/submit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestTranscodeExt_ProfilesNilService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xAny")
		c.Next()
	})
	RegisterTranscodingRoutes(r, zap.NewNop(), nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/transcode/profiles", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

type errNFTOwnershipChecker struct {
	verifyErr  error
	balanceErr error
}

func (e *errNFTOwnershipChecker) VerifyNFTOwnership(_ context.Context, _ int64, _, _, _ string) (bool, error) {
	if e.verifyErr != nil {
		return false, e.verifyErr
	}
	return true, nil
}

func (e *errNFTOwnershipChecker) GetNFTBalance(_ context.Context, _ int64, _, _ string) (*big.Int, error) {
	if e.balanceErr != nil {
		return nil, e.balanceErr
	}
	return big.NewInt(1), nil
}

func (e *errNFTOwnershipChecker) VerifyNFTOwnershipAutoDetect(_ context.Context, _ int64, _, _, _ string) (bool, error) {
	return true, nil
}

func (e *errNFTOwnershipChecker) VerifyNFTCollectionAutoDetect(_ context.Context, _ int64, _, _ string) (bool, error) {
	return true, nil
}

func (e *errNFTOwnershipChecker) GetNFTInfo(_ context.Context, _ int64, _, _ string) (*middleware.NFTMetadata, error) {
	return nil, nil
}

func TestNFTExt_BalanceError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	verifier := &errNFTOwnershipChecker{balanceErr: errors.New("RPC error")}
	RegisterNFTRoutes(r, zap.NewNop(), verifier, &mockNFTCache{}, 1, time.Minute)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/nft?contract=0x1234567890abcdef1234567890abcdef12345678", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestNFTExt_VerifyError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	verifier := &errNFTOwnershipChecker{verifyErr: errors.New("RPC error")}
	RegisterNFTRoutes(r, zap.NewNop(), verifier, &mockNFTCache{}, 1, time.Minute)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/nft/1?contract=0x1234567890abcdef1234567890abcdef12345678", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestNFTExt_MissingContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	RegisterNFTRoutes(r, zap.NewNop(), &mockNFTOwnershipChecker{}, &mockNFTCache{}, 1, time.Minute)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/nft", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNFTExt_InvalidContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	RegisterNFTRoutes(r, zap.NewNop(), &mockNFTOwnershipChecker{}, &mockNFTCache{}, 1, time.Minute)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/nft?contract=invalid", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNFTExt_VerifyEndpoint_InvalidTokenID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	RegisterNFTRoutes(r, zap.NewNop(), &mockNFTOwnershipChecker{}, &mockNFTCache{}, 1, time.Minute)

	body := `{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x1234567890abcdef1234567890abcdef12345678","token_id":"not-a-number"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNFTExt_VerifyEndpoint_MissingContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	RegisterNFTRoutes(r, zap.NewNop(), &mockNFTOwnershipChecker{}, &mockNFTCache{}, 1, time.Minute)

	body := `{"wallet":"0x1234567890abcdef1234567890abcdef12345678","token_id":"1"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNFTExt_VerifyEndpoint_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	RegisterNFTRoutes(r, zap.NewNop(), &mockNFTOwnershipChecker{}, &mockNFTCache{}, 1, time.Minute)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNFTExt_VerifyEndpoint_WithCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	cache := NewNFTAccessCache()
	defer cache.Stop()
	adapter := &NFTAccessCacheAdapter{Cache: cache}

	RegisterNFTRoutes(r, zap.NewNop(), &mockNFTOwnershipChecker{}, adapter, 1, time.Minute)

	body := `{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x1234567890abcdef1234567890abcdef12345678","token_id":"1"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["cache_hit"])
}

func TestNFTExt_VerifyEndpoint_CacheHit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	cache := NewNFTAccessCache()
	defer cache.Stop()
	cacheKey := "1:0x1234567890abcdef1234567890abcdef12345678:0x1234567890abcdef1234567890abcdef12345678:1"
	cache.Set(cacheKey, CachedNFTAccess{
		HasNFT:    true,
		Balance:   big.NewInt(1),
		ExpiresAt: time.Now().Add(time.Hour),
	})
	adapter := &NFTAccessCacheAdapter{Cache: cache}

	RegisterNFTRoutes(r, zap.NewNop(), &mockNFTOwnershipChecker{}, adapter, 1, time.Minute)

	body := `{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x1234567890abcdef1234567890abcdef12345678","token_id":"1"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["cache_hit"])
}

func TestNFTExt_VerifyEndpoint_AltFieldNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	RegisterNFTRoutes(r, zap.NewNop(), &mockNFTOwnershipChecker{}, &mockNFTCache{}, 1, time.Minute)

	body := `{"contract_address":"0x1234567890abcdef1234567890abcdef12345678","owner_address":"0x1234567890abcdef1234567890abcdef12345678","token_id":"1"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNFTExt_VerifyEndpoint_NoTokenID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	RegisterNFTRoutes(r, zap.NewNop(), &mockNFTOwnershipChecker{}, &mockNFTCache{}, 1, time.Minute)

	body := `{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x1234567890abcdef1234567890abcdef12345678"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNFTExt_BalanceSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	RegisterNFTRoutes(r, zap.NewNop(), &mockNFTOwnershipChecker{}, &mockNFTCache{}, 1, time.Minute)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/nft?contract=0x1234567890abcdef1234567890abcdef12345678", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "1", resp["balance"])
	assert.Equal(t, true, resp["has_nft"])
}

func TestNFTExt_TokenIDSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	RegisterNFTRoutes(r, zap.NewNop(), &mockNFTOwnershipChecker{}, &mockNFTCache{}, 1, time.Minute)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/nft/1?contract=0x1234567890abcdef1234567890abcdef12345678", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["has_nft"])
}

func TestNFTExt_VerifyEndpoint_VerifyError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	verifier := &errNFTOwnershipChecker{verifyErr: errors.New("RPC error")}
	RegisterNFTRoutes(r, zap.NewNop(), verifier, &mockNFTCache{}, 1, time.Minute)

	body := `{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x1234567890abcdef1234567890abcdef12345678","token_id":"1"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["has_nft"])
	assert.Equal(t, "0", resp["balance"])
}

func TestNFTExt_VerifyEndpoint_BalanceError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	verifier := &errNFTOwnershipChecker{balanceErr: errors.New("RPC error")}
	RegisterNFTRoutes(r, zap.NewNop(), verifier, &mockNFTCache{}, 1, time.Minute)

	body := `{"contract":"0x1234567890abcdef1234567890abcdef12345678","wallet":"0x1234567890abcdef1234567890abcdef12345678"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["has_nft"])
	assert.Equal(t, "0", resp["balance"])
}

func TestNFTExt_ChainIDOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	RegisterNFTRoutes(r, zap.NewNop(), &mockNFTOwnershipChecker{}, &mockNFTCache{}, 1, time.Minute)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/nft?contract=0x1234567890abcdef1234567890abcdef12345678&chain_id=137", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, float64(137), resp["chain_id"])
}

func TestNFTExt_VerifyEndpoint_AddressFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	RegisterNFTRoutes(r, zap.NewNop(), &mockNFTOwnershipChecker{}, &mockNFTCache{}, 1, time.Minute)

	body := `{"contract":"0x1234567890abcdef1234567890abcdef12345678","address":"0x1234567890abcdef1234567890abcdef12345678","token_id":"1"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/nft/verify", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestStreamingExt_RegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	authSvc := newGatewayTestAuthService()
	streamingSvc := service.NewStreamingService(nil, nil, nil, "http://localhost")
	cache := NewStreamingCache()
	limiter := newStreamLimiter(100)

	RegisterStreamingRoutes(r, zap.NewNop(), authSvc, streamingSvc, nil, limiter, cache)

	routes := r.Routes()
	found := false
	for _, route := range routes {
		if strings.Contains(route.Path, "manifest.m3u8") {
			found = true
			break
		}
	}
	assert.True(t, found, "manifest route should be registered")
}

func TestStreamingExt_SegmentRoute_NoToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	authSvc := newGatewayTestAuthService()
	cache := NewStreamingCache()
	limiter := newStreamLimiter(100)

	RegisterStreamingSegmentRoute(r, zap.NewNop(), authSvc, nil, limiter, cache)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/streaming/test-content/segment/seg0.ts", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestStreamingExt_SegmentRoute_InvalidContentID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	authSvc := newGatewayTestAuthService()
	cache := NewStreamingCache()
	limiter := newStreamLimiter(100)

	RegisterStreamingSegmentRoute(r, zap.NewNop(), authSvc, nil, limiter, cache)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/streaming/bad%20id/segment/seg0.ts?playback_token=test", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestStreamingExt_ValidateSegmentName_PathTraversal(t *testing.T) {
	assert.False(t, validateSegmentName("../etc/passwd.ts"))
	assert.False(t, validateSegmentName("seg/../../etc.ts"))
	assert.True(t, validateSegmentName("seg0.ts"))
	assert.True(t, validateSegmentName("segment_001.ts"))
	assert.False(t, validateSegmentName("segment_001.m4s"))
}

func TestStreamingExt_SegmentRoute_NoObjStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	authSvc := newGatewayTestAuthService()
	cache := NewStreamingCache()
	limiter := newStreamLimiter(100)

	RegisterStreamingSegmentRoute(r, zap.NewNop(), authSvc, nil, limiter, cache)

	token, _ := authSvc.GeneratePlaybackToken(context.Background(), "0x1234567890abcdef1234567890abcdef12345678", "content1", "", "", 1, 5*time.Minute, "")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/streaming/content1/segment/seg0.ts?playback_token="+token, http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestStreamingExt_ManifestRoute_InvalidContentID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	authSvc := newGatewayTestAuthService()
	streamingSvc := service.NewStreamingService(nil, nil, nil, "http://localhost")
	cache := NewStreamingCache()
	limiter := newStreamLimiter(100)

	RegisterStreamingRoutes(r, zap.NewNop(), authSvc, streamingSvc, nil, limiter, cache)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/streaming/bad%20id/manifest.m3u8", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestStreamingExt_ManifestRoute_LimiterFull(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0x1234567890abcdef1234567890abcdef12345678")
		c.Next()
	})

	authSvc := newGatewayTestAuthService()
	streamingSvc := service.NewStreamingService(nil, nil, nil, "http://localhost")
	cache := NewStreamingCache()
	limiter := newStreamLimiter(1)
	limiter.tryAcquire()

	RegisterStreamingRoutes(r, zap.NewNop(), authSvc, streamingSvc, nil, limiter, cache)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/streaming/content1/manifest.m3u8", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	limiter.release()
}

func TestGatewayExt_RespondNoContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", http.NoBody)

	respondNoContent(c)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestGatewayExt_AbortWithErrorDetail_5xx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetErrorLogger(zap.NewNop())
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", http.NoBody)

	abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, "internal error", "sensitive detail")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	_, hasDetail := resp["detail"]
	assert.False(t, hasDetail, "5xx errors should not expose detail")
}

func TestGatewayExt_AbortWithErrorDetail_4xx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", http.NoBody)

	abortWithErrorDetail(c, http.StatusBadRequest, ErrInvalidRequest, "bad request", "field detail")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "field detail", resp["detail"])
}

func TestGatewayExt_RespondWithRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	c.Set("request_id", "req-test-123")

	respondOK(c, gin.H{"status": "ok"})

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "req-test-123", resp["request_id"])
	assert.Equal(t, "ok", resp["status"])
}

func TestGatewayExt_RespondWithoutRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", http.NoBody)

	respondOK(c, gin.H{"status": "ok"})

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "ok", resp["status"])
	_, hasReqID := resp["request_id"]
	assert.False(t, hasReqID)
}

func TestGatewayExt_RequestIDMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/test", func(c *gin.Context) {
		reqID, _ := c.Get("request_id")
		c.String(http.StatusOK, reqID.(string))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, strings.HasPrefix(w.Body.String(), "req-"))
	assert.NotEmpty(t, w.Header().Get("X-Request-ID"))
}
