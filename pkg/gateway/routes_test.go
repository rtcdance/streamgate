package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestBuildCircuitBreakerConfig_Disabled(t *testing.T) {
	cfg := &config.Config{
		CircuitBreaker: config.CircuitBreakerConfig{
			Enabled: false,
		},
	}
	cbConfig := buildCircuitBreakerConfig(cfg)
	expected := middleware.DefaultCircuitBreakerConfig()
	assert.Equal(t, expected.Timeout, cbConfig.Timeout)
	assert.Equal(t, expected.FailureThreshold, cbConfig.FailureThreshold)
}

func TestBuildCircuitBreakerConfig_Enabled(t *testing.T) {
	cfg := &config.Config{
		CircuitBreaker: config.CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 10,
			SuccessThreshold: 5,
			Timeout:          "60s",
			MaxRequests:      3,
			WindowTime:       "120s",
		},
	}
	cbConfig := buildCircuitBreakerConfig(cfg)
	assert.Equal(t, 10, cbConfig.FailureThreshold)
	assert.Equal(t, 5, cbConfig.SuccessThreshold)
	assert.Equal(t, 60*time.Second, cbConfig.Timeout)
	assert.Equal(t, 3, cbConfig.MaxRequests)
	assert.Equal(t, 120*time.Second, cbConfig.WindowTime)
}

func TestBuildCircuitBreakerConfig_PartialFields(t *testing.T) {
	cfg := &config.Config{
		CircuitBreaker: config.CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 7,
			Timeout:          "invalid-duration",
		},
	}
	cbConfig := buildCircuitBreakerConfig(cfg)
	assert.Equal(t, 7, cbConfig.FailureThreshold)
	defaultCfg := middleware.DefaultCircuitBreakerConfig()
	assert.Equal(t, defaultCfg.SuccessThreshold, cbConfig.SuccessThreshold)
	assert.Equal(t, defaultCfg.Timeout, cbConfig.Timeout)
	assert.Equal(t, defaultCfg.MaxRequests, cbConfig.MaxRequests)
}

func TestBuildCircuitBreakerConfig_ZeroFieldsKeepDefaults(t *testing.T) {
	cfg := &config.Config{
		CircuitBreaker: config.CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 0,
			SuccessThreshold: 0,
			MaxRequests:      0,
		},
	}
	cbConfig := buildCircuitBreakerConfig(cfg)
	defaultCfg := middleware.DefaultCircuitBreakerConfig()
	assert.Equal(t, defaultCfg.FailureThreshold, cbConfig.FailureThreshold)
	assert.Equal(t, defaultCfg.SuccessThreshold, cbConfig.SuccessThreshold)
	assert.Equal(t, defaultCfg.MaxRequests, cbConfig.MaxRequests)
}

func TestParseBlockTagFromRoutes(t *testing.T) {
	assert.Equal(t, web3.BlockTagFinalized, parseBlockTag("finalized"))
	assert.Equal(t, web3.BlockTagLatest, parseBlockTag("latest"))
	assert.Equal(t, web3.BlockTagSafe, parseBlockTag("safe"))
	assert.Equal(t, web3.BlockTagSafe, parseBlockTag(""))
	assert.Equal(t, web3.BlockTagSafe, parseBlockTag("unknown"))
	assert.Equal(t, web3.BlockTagSafe, parseBlockTag("pending"))
}

func TestRegisterInfrastructureRoutes_HealthEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()

	registerInfrastructureRoutes(router, log, nil, nil, nil, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "healthy", resp["status"])
}

func TestRegisterInfrastructureRoutes_HealthWithDBCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()

	mockDB := &routesMockDB{pingErr: nil}
	registerInfrastructureRoutes(router, log, mockDB, nil, nil, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRegisterInfrastructureRoutes_HealthWithDBFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()

	mockDB := &routesMockDB{pingErr: context.DeadlineExceeded}
	registerInfrastructureRoutes(router, log, mockDB, nil, nil, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "unhealthy", resp["status"])
}

func TestRegisterInfrastructureRoutes_HealthWithCBEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.CircuitBreaker.Enabled = true

	mwSvc := middleware.NewService(log)
	mockDB := &routesMockDB{pingErr: nil}

	registerInfrastructureRoutes(router, log, mockDB, nil, mwSvc, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRegisterInfrastructureRoutes_ReadyEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()

	registerInfrastructureRoutes(router, log, nil, nil, nil, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", http.NoBody)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, true, resp["ready"])
}

func TestRegisterInfrastructureRoutes_ReadyNotReady(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()

	mockDB := &routesMockDB{pingErr: context.DeadlineExceeded}
	registerInfrastructureRoutes(router, log, mockDB, nil, nil, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", http.NoBody)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, false, resp["ready"])
}

func TestRegisterInfrastructureRoutes_CircuitBreakersEndpoint_NoService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()

	registerInfrastructureRoutes(router, log, nil, nil, nil, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/circuit-breakers", http.NoBody)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp["circuit_breakers"])
}

func TestRegisterInfrastructureRoutes_CircuitBreakersEndpoint_WithService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.CircuitBreaker.Enabled = true

	mwSvc := middleware.NewService(log)
	cbConfig := middleware.DefaultCircuitBreakerConfig()
	_ = mwSvc.DependencyCircuitBreaker("db", cbConfig)

	registerInfrastructureRoutes(router, log, nil, nil, mwSvc, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/circuit-breakers", http.NoBody)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	breakers := resp["circuit_breakers"]
	assert.NotNil(t, breakers)
}

func TestRegisterInfrastructureRoutes_DocsEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()

	registerInfrastructureRoutes(router, log, nil, nil, nil, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", http.NoBody)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "Swagger")
}

func TestRegisterInfrastructureRoutes_MetricsEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()

	registerInfrastructureRoutes(router, log, nil, nil, nil, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRegisterInfrastructureRoutes_StorageCheckSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()

	mockStorage := &routesMockSegmentStorage{listErr: nil}
	registerInfrastructureRoutes(router, log, nil, mockStorage, nil, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRegisterInfrastructureRoutes_StorageCheckFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()

	mockStorage := &routesMockSegmentStorage{listErr: context.DeadlineExceeded}
	registerInfrastructureRoutes(router, log, nil, mockStorage, nil, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "unhealthy", resp["status"])
}

func TestRegisterRoutes_RegistersAllInfrastructureEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	authService := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-testing-",
		newMockAuthStorage(),
		sigVerifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)

	authRL := middleware.NewRateLimiter(middleware.RateLimitConfig{
		RequestsPerMinute: 10,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}, nil)
	RegisterAuthRoutes(router, log, cfg, authService, authRL)
	RegisterWeb3Routes(router, log, &routesMockWeb3StatusProvider{})

	streamLim := newStreamLimiter(10)
	streamCache := NewStreamingCache()
	RegisterStreamingSegmentRoute(router, log, authService, nil, streamLim, streamCache, "streamgate")

	registerInfrastructureRoutes(router, log, nil, nil, nil, cfg)

	infraRoutes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/health"},
		{http.MethodGet, "/metrics"},
		{http.MethodGet, "/ready"},
		{http.MethodGet, "/circuit-breakers"},
		{http.MethodGet, "/docs"},
	}
	for _, r := range infraRoutes {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(r.method, r.path, http.NoBody)
		router.ServeHTTP(w, req)
		assert.NotEqual(t, http.StatusNotFound, w.Code, "route %s %s should be registered", r.method, r.path)
	}
}

func TestRegisterRoutes_AuthRoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	authService := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-testing-",
		newMockAuthStorage(),
		sigVerifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)

	authRL := middleware.NewRateLimiter(middleware.RateLimitConfig{
		RequestsPerMinute: 100,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}, nil)
	RegisterAuthRoutes(router, log, cfg, authService, authRL)

	authRoutes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/auth/challenge"},
		{http.MethodPost, "/api/v1/auth/login"},
		{http.MethodPost, "/api/v1/auth/register"},
		{http.MethodPost, "/api/v1/auth/refresh"},
		{http.MethodPost, "/api/v1/auth/logout"},
		{http.MethodPost, "/api/v1/auth/verify"},
	}
	for _, r := range authRoutes {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(r.method, r.path, http.NoBody)
		router.ServeHTTP(w, req)
		assert.NotEqual(t, http.StatusNotFound, w.Code, "route %s %s should be registered", r.method, r.path)
	}
}

func TestRegisterRoutes_Web3RoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()

	provider := &routesMockWeb3StatusProvider{
		statuses: map[int64][]web3.RPCStatus{},
		chains:   []*web3.ChainConfig{},
	}
	RegisterWeb3Routes(router, log, provider)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/web3/rpc-status", http.NoBody)
	router.ServeHTTP(w, req)
	assert.NotEqual(t, http.StatusNotFound, w.Code)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRegisterRoutes_StreamingSegmentRouteRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	authService := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-testing-",
		newMockAuthStorage(),
		sigVerifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)

	streamLim := newStreamLimiter(10)
	streamCache := NewStreamingCache()

	RegisterStreamingSegmentRoute(router, log, authService, nil, streamLim, streamCache, "streamgate")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/streaming/test-content/segment/00001.ts", http.NoBody)
	router.ServeHTTP(w, req)
	assert.NotEqual(t, http.StatusNotFound, w.Code)
}

func TestRegisterProtectedRoutes_NFTRoutesPresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-jwt-secret-key-for-testing-"

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	authService := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-testing-",
		newMockAuthStorage(),
		sigVerifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)

	nftCache := NewNFTAccessCache()
	defer nftCache.Stop()
	nftCacheBackend := &NFTAccessCacheAdapter{Cache: nftCache}

	svc := &serviceInit{
		AuthService:        authService,
		NFTVerifier:        &routesMockNFTVerifier{},
		NFTCacheBackend:    nftCacheBackend,
		GatingRuleResolver: &routesMockGatingRuleResolver{},
	}

	streamLim := newStreamLimiter(10)
	streamCache := NewStreamingCache()

	registerProtectedRoutes(router, cfg, log, svc, streamLim, streamCache)

	routes := router.Routes()
	hasNFTRoute := false
	for _, r := range routes {
		if r.Path == APIPrefix+"/nft" && r.Method == http.MethodGet {
			hasNFTRoute = true
		}
	}
	assert.True(t, hasNFTRoute, "NFT GET route should be registered")
}

func TestRegisterProtectedRoutes_AuthProtectedRoutesPresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-jwt-secret-key-for-testing-"

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	authService := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-testing-",
		newMockAuthStorage(),
		sigVerifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)

	nftCache := NewNFTAccessCache()
	defer nftCache.Stop()

	svc := &serviceInit{
		AuthService:        authService,
		NFTVerifier:        &routesMockNFTVerifier{},
		NFTCacheBackend:    &NFTAccessCacheAdapter{Cache: nftCache},
		GatingRuleResolver: &routesMockGatingRuleResolver{},
	}

	streamLim := newStreamLimiter(10)
	streamCache := NewStreamingCache()

	registerProtectedRoutes(router, cfg, log, svc, streamLim, streamCache)

	routes := router.Routes()
	hasProfileRoute := false
	for _, r := range routes {
		if r.Path == APIPrefix+"/auth/profile" && r.Method == http.MethodGet {
			hasProfileRoute = true
		}
	}
	assert.True(t, hasProfileRoute, "auth profile route should be registered")
}

func TestRegisterProtectedRoutes_StreamingManifestRoutePresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-jwt-secret-key-for-testing-"

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	authService := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-testing-",
		newMockAuthStorage(),
		sigVerifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)

	nftCache := NewNFTAccessCache()
	defer nftCache.Stop()

	svc := &serviceInit{
		AuthService:        authService,
		NFTVerifier:        &routesMockNFTVerifier{},
		NFTCacheBackend:    &NFTAccessCacheAdapter{Cache: nftCache},
		StreamingSvc:       service.NewStreamingService(nil, nil, nil, "", log.Named("streaming")),
		GatingRuleResolver: &routesMockGatingRuleResolver{},
	}

	streamLim := newStreamLimiter(10)
	streamCache := NewStreamingCache()

	registerProtectedRoutes(router, cfg, log, svc, streamLim, streamCache)

	routes := router.Routes()
	hasManifestRoute := false
	for _, r := range routes {
		if r.Path == APIPrefix+"/streaming/:id/manifest.m3u8" && r.Method == http.MethodGet {
			hasManifestRoute = true
		}
	}
	assert.True(t, hasManifestRoute, "streaming manifest route should be registered")
}

func TestRegisterProtectedRoutes_ContentRoutesPresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-jwt-secret-key-for-testing-"

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	authService := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-testing-",
		newMockAuthStorage(),
		sigVerifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)

	nftCache := NewNFTAccessCache()
	defer nftCache.Stop()

	svc := &serviceInit{
		AuthService:        authService,
		NFTVerifier:        &routesMockNFTVerifier{},
		NFTCacheBackend:    &NFTAccessCacheAdapter{Cache: nftCache},
		ContentService:     nil,
		GatingRuleResolver: &routesMockGatingRuleResolver{},
	}

	streamLim := newStreamLimiter(10)
	streamCache := NewStreamingCache()

	registerProtectedRoutes(router, cfg, log, svc, streamLim, streamCache)

	routes := router.Routes()
	hasContentListRoute := false
	for _, r := range routes {
		if r.Path == APIPrefix+"/content" && r.Method == http.MethodGet {
			hasContentListRoute = true
		}
	}
	assert.True(t, hasContentListRoute, "content list route should be registered")
}

func TestRegisterProtectedRoutes_TranscodingRoutesPresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-jwt-secret-key-for-testing-"

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	authService := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-testing-",
		newMockAuthStorage(),
		sigVerifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)

	nftCache := NewNFTAccessCache()
	defer nftCache.Stop()

	svc := &serviceInit{
		AuthService:        authService,
		NFTVerifier:        &routesMockNFTVerifier{},
		NFTCacheBackend:    &NFTAccessCacheAdapter{Cache: nftCache},
		TranscodingSvc:     nil,
		GatingRuleResolver: &routesMockGatingRuleResolver{},
	}

	streamLim := newStreamLimiter(10)
	streamCache := NewStreamingCache()

	registerProtectedRoutes(router, cfg, log, svc, streamLim, streamCache)

	routes := router.Routes()
	hasSubmitRoute := false
	for _, r := range routes {
		if r.Path == APIPrefix+"/transcode/submit" && r.Method == http.MethodPost {
			hasSubmitRoute = true
		}
	}
	assert.True(t, hasSubmitRoute, "transcode submit route should be registered")
}

func TestRegisterProtectedRoutes_GatingRuleRoutesWhenServiceSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-jwt-secret-key-for-testing-"

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	authService := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-testing-",
		newMockAuthStorage(),
		sigVerifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)

	nftCache := NewNFTAccessCache()
	defer nftCache.Stop()

	svc := &serviceInit{
		AuthService:        authService,
		NFTVerifier:        &routesMockNFTVerifier{},
		NFTCacheBackend:    &NFTAccessCacheAdapter{Cache: nftCache},
		GatingRuleSvc:      service.NewGatingRuleService(nil, log.Named("gating")),
		GatingRuleResolver: &routesMockGatingRuleResolver{},
	}

	streamLim := newStreamLimiter(10)
	streamCache := NewStreamingCache()

	registerProtectedRoutes(router, cfg, log, svc, streamLim, streamCache)

	routes := router.Routes()
	hasGatingRoute := false
	for _, r := range routes {
		if r.Path == APIPrefix+"/content/:id/gating-rules" && r.Method == http.MethodGet {
			hasGatingRoute = true
		}
	}
	assert.True(t, hasGatingRoute, "gating rule route should be registered when GatingRuleSvc is set")
}

func TestRegisterProtectedRoutes_NoGatingRuleRoutesWhenServiceNil(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-jwt-secret-key-for-testing-"

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	authService := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-testing-",
		newMockAuthStorage(),
		sigVerifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)

	nftCache := NewNFTAccessCache()
	defer nftCache.Stop()

	svc := &serviceInit{
		AuthService:        authService,
		NFTVerifier:        &routesMockNFTVerifier{},
		NFTCacheBackend:    &NFTAccessCacheAdapter{Cache: nftCache},
		GatingRuleSvc:      nil,
		GatingRuleResolver: &routesMockGatingRuleResolver{},
	}

	streamLim := newStreamLimiter(10)
	streamCache := NewStreamingCache()

	registerProtectedRoutes(router, cfg, log, svc, streamLim, streamCache)

	routes := router.Routes()
	for _, r := range routes {
		assert.NotContains(t, r.Path, "gating-rules", "gating rule routes should not be registered when GatingRuleSvc is nil")
	}
}

func TestRegisterProtectedRoutes_PlaybackStatsRoutesWhenServiceSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-jwt-secret-key-for-testing-"

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	authService := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-testing-",
		newMockAuthStorage(),
		sigVerifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)

	nftCache := NewNFTAccessCache()
	defer nftCache.Stop()

	svc := &serviceInit{
		AuthService:        authService,
		NFTVerifier:        &routesMockNFTVerifier{},
		NFTCacheBackend:    &NFTAccessCacheAdapter{Cache: nftCache},
		PlaybackStatsSvc:   service.NewPlaybackStatsService(nil, log.Named("stats")),
		GatingRuleResolver: &routesMockGatingRuleResolver{},
	}

	streamLim := newStreamLimiter(10)
	streamCache := NewStreamingCache()

	registerProtectedRoutes(router, cfg, log, svc, streamLim, streamCache)

	routes := router.Routes()
	hasStatsRoute := false
	for _, r := range routes {
		if r.Path == APIPrefix+"/stats/playback" && r.Method == http.MethodPost {
			hasStatsRoute = true
		}
	}
	assert.True(t, hasStatsRoute, "playback stats route should be registered when PlaybackStatsSvc is set")
}

func TestRegisterProtectedRoutes_CategoryRoutesWhenServiceSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-jwt-secret-key-for-testing-"

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	authService := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-testing-",
		newMockAuthStorage(),
		sigVerifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)

	nftCache := NewNFTAccessCache()
	defer nftCache.Stop()

	svc := &serviceInit{
		AuthService:        authService,
		NFTVerifier:        &routesMockNFTVerifier{},
		NFTCacheBackend:    &NFTAccessCacheAdapter{Cache: nftCache},
		CategorySvc:        service.NewCategoryService(nil, log.Named("category")),
		GatingRuleResolver: &routesMockGatingRuleResolver{},
	}

	streamLim := newStreamLimiter(10)
	streamCache := NewStreamingCache()

	registerProtectedRoutes(router, cfg, log, svc, streamLim, streamCache)

	routes := router.Routes()
	hasCategoryRoute := false
	for _, r := range routes {
		if r.Path == APIPrefix+"/categories" && r.Method == http.MethodGet {
			hasCategoryRoute = true
		}
	}
	assert.True(t, hasCategoryRoute, "category route should be registered when CategorySvc is set")
}

func TestRegisterProtectedRoutes_UploadRoutesPresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-jwt-secret-key-for-testing-"

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	authService := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-testing-",
		newMockAuthStorage(),
		sigVerifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)

	nftCache := NewNFTAccessCache()
	defer nftCache.Stop()

	svc := &serviceInit{
		AuthService:        authService,
		NFTVerifier:        &routesMockNFTVerifier{},
		NFTCacheBackend:    &NFTAccessCacheAdapter{Cache: nftCache},
		GatingRuleResolver: &routesMockGatingRuleResolver{},
	}

	streamLim := newStreamLimiter(10)
	streamCache := NewStreamingCache()

	registerProtectedRoutes(router, cfg, log, svc, streamLim, streamCache)

	routes := router.Routes()
	routePaths := make(map[string]bool)
	for _, r := range routes {
		routePaths[r.Path] = true
	}
	assert.True(t, routePaths[APIPrefix+"/auth/profile"], "auth profile should be registered")
	assert.True(t, routePaths[APIPrefix+"/nft"], "NFT route should be registered")
	assert.True(t, routePaths[APIPrefix+"/streaming/:id/manifest.m3u8"], "streaming manifest should be registered")
	assert.True(t, routePaths[APIPrefix+"/content"], "content route should be registered")
	assert.True(t, routePaths[APIPrefix+"/transcode/submit"], "transcode submit should be registered")
}

func TestRegisterInfrastructureRoutes_WithCBService_RegistersBreakers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.CircuitBreaker.Enabled = true

	mwSvc := middleware.NewService(log)
	cbConfig := middleware.DefaultCircuitBreakerConfig()
	_ = mwSvc.DependencyCircuitBreaker("db", cbConfig)
	_ = mwSvc.DependencyCircuitBreaker("redis", cbConfig)

	registerInfrastructureRoutes(router, log, nil, nil, mwSvc, cfg)

	stats := mwSvc.AllCircuitBreakerStats()
	assert.Contains(t, stats, "db")
	assert.Contains(t, stats, "redis")
}

type routesMockDB struct {
	pingErr error
}

func (m *routesMockDB) Query(_ context.Context, _ string, _ ...interface{}) (storage.Rows, error) {
	return nil, nil
}

func (m *routesMockDB) QueryRow(_ context.Context, _ string, _ ...interface{}) *storage.CancelRow {
	return nil
}

func (m *routesMockDB) Exec(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
	return nil, nil
}

func (m *routesMockDB) Begin(_ context.Context) (*sql.Tx, error) {
	return nil, nil
}

func (m *routesMockDB) InTransaction(_ context.Context, _ func(*sql.Tx) error) error {
	return nil
}

func (m *routesMockDB) Ping(_ context.Context) error {
	return m.pingErr
}

func (m *routesMockDB) Close() error {
	return nil
}

type routesMockSegmentStorage struct {
	listErr error
}

func (m *routesMockSegmentStorage) Upload(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

func (m *routesMockSegmentStorage) UploadStream(_ context.Context, _, _ string, _ io.Reader, _ int64) error {
	return nil
}

func (m *routesMockSegmentStorage) UploadWithContentType(_ context.Context, _, _ string, _ []byte, _ string) error {
	return nil
}

func (m *routesMockSegmentStorage) UploadStreamWithContentType(_ context.Context, _, _ string, _ io.Reader, _ int64, _ string) error {
	return nil
}

func (m *routesMockSegmentStorage) Download(_ context.Context, _, _ string) ([]byte, error) {
	return nil, nil
}

func (m *routesMockSegmentStorage) DownloadStream(_ context.Context, _, _ string) (io.ReadCloser, error) {
	return nil, nil
}

func (m *routesMockSegmentStorage) Delete(_ context.Context, _, _ string) error {
	return nil
}

func (m *routesMockSegmentStorage) ListObjects(_ context.Context, _, _ string) ([]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return []string{}, nil
}

func (m *routesMockSegmentStorage) Exists(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

func (m *routesMockSegmentStorage) CreateBucket(_ context.Context, _ string) error {
	return nil
}

type routesMockNFTVerifier struct{}

func (m *routesMockNFTVerifier) VerifyNFTOwnership(_ context.Context, _ int64, _, _, _ string) (bool, error) {
	return true, nil
}

func (m *routesMockNFTVerifier) VerifyNFTOwnershipAutoDetect(_ context.Context, _ int64, _, _, _ string) (bool, error) {
	return true, nil
}

func (m *routesMockNFTVerifier) VerifyNFTCollectionAutoDetect(_ context.Context, _ int64, _, _ string) (bool, error) {
	return true, nil
}

func (m *routesMockNFTVerifier) GetNFTBalance(_ context.Context, _ int64, _, _ string) (*big.Int, error) {
	return big.NewInt(1), nil
}

func (m *routesMockNFTVerifier) GetNFTInfo(_ context.Context, _ int64, _, _ string) (*middleware.NFTMetadata, error) {
	return &middleware.NFTMetadata{}, nil
}

type routesMockWeb3StatusProvider struct {
	statuses map[int64][]web3.RPCStatus
	chains   []*web3.ChainConfig
}

func (m *routesMockWeb3StatusProvider) GetRPCStatuses() map[int64][]web3.RPCStatus {
	return m.statuses
}

func (m *routesMockWeb3StatusProvider) GetSupportedChains() []*web3.ChainConfig {
	return m.chains
}

type routesMockGatingRuleResolver struct{}

func (m *routesMockGatingRuleResolver) GetActiveRulesForContent(_ context.Context, _ string) ([]middleware.GatingRule, error) {
	return []middleware.GatingRule{}, nil
}
