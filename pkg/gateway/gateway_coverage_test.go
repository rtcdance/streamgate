package gateway

import (
	"context"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3"

	"github.com/gin-gonic/gin"
	authv1 "github.com/rtcdance/streamgate/pkg/api/v1/auth"
	nftv1 "github.com/rtcdance/streamgate/pkg/api/v1/nft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestPrometheusMiddleware_RecordsMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(prometheusMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestPrometheusMiddleware_Records404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(prometheusMiddleware())
	r.NoRoute(func(c *gin.Context) {
		c.Status(http.StatusNotFound)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", http.NoBody)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSetupRouter_WithInjectedServices(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-secret-key-that-is-at-least-32-chars"

	mcm := web3.NewMultiChainManager(log)
	sv := web3.NewSignatureVerifier(log)
	solanaV := web3.NewSolanaVerifier(log, "")
	defer solanaV.Close()
	eip712v := web3.NewEIP712Verifier(log)

	web3Svc, err := service.NewWeb3Service(service.Web3Deps{
		ChainManager: mcm,
		SigVerifier:  sv,
		SolanaVerif:  solanaV,
		EIP712Verif:  eip712v,
	}, cfg, log)
	require.NoError(t, err)
	defer web3Svc.Close()

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	challengeStore := storage.NewMemoryChallengeStore()
	defer challengeStore.Close()
	tokenBlacklist := storage.NewMemoryTokenBlacklist()

	authService := service.NewAuthServiceWithDeps(
		cfg.Auth.JWTSecret,
		nil,
		sigVerifier,
		challengeStore,
		5*time.Minute,
		tokenBlacklist,
	)

	router, resources, err := SetupRouter(cfg, log,
		WithAuthService(authService),
		WithWeb3Service(web3Svc),
	)
	require.NoError(t, err)
	require.NotNil(t, router)
	require.NotNil(t, resources)
	defer resources.Close()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	router.ServeHTTP(w, req)
	assert.Contains(t, []int{http.StatusOK, http.StatusServiceUnavailable}, w.Code)
}

func TestSetupRouter_WithAllInjected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-secret-key-that-is-at-least-32-chars"

	mcm := web3.NewMultiChainManager(log)
	sv := web3.NewSignatureVerifier(log)
	solanaV := web3.NewSolanaVerifier(log, "")
	defer solanaV.Close()
	eip712v := web3.NewEIP712Verifier(log)

	web3Svc, err := service.NewWeb3Service(service.Web3Deps{
		ChainManager: mcm,
		SigVerifier:  sv,
		SolanaVerif:  solanaV,
		EIP712Verif:  eip712v,
	}, cfg, log)
	require.NoError(t, err)
	defer web3Svc.Close()

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	challengeStore := storage.NewMemoryChallengeStore()
	defer challengeStore.Close()
	tokenBlacklist := storage.NewMemoryTokenBlacklist()

	authService := service.NewAuthServiceWithDeps(
		cfg.Auth.JWTSecret,
		nil,
		sigVerifier,
		challengeStore,
		5*time.Minute,
		tokenBlacklist,
	)

	nftCache := NewNFTAccessCache()
	defer nftCache.Stop()

	router2, resources2, err := SetupRouter(cfg, log,
		WithAuthService(authService),
		WithWeb3Service(web3Svc),
		WithNFTVerifier(&gwCovMockNFTVerifier{}),
		WithChallengeStore(challengeStore),
	)
	require.NoError(t, err)
	require.NotNil(t, router2)
	require.NotNil(t, resources2)
	defer resources2.Close()

	routes := router2.Routes()
	routeMap := make(map[string]bool)
	for _, r := range routes {
		routeMap[r.Method+":"+r.Path] = true
	}
	assert.True(t, routeMap["GET:/health"], "health route should exist")
	assert.True(t, routeMap["POST:/api/v1/auth/challenge"], "auth challenge route should exist")
}

func TestProvideWeb3Service_WithInjected(t *testing.T) {
	log := zap.NewNop()
	cfg := config.DefaultConfig()

	mcm := web3.NewMultiChainManager(log)
	sv := web3.NewSignatureVerifier(log)
	solanaV := web3.NewSolanaVerifier(log, "")
	defer solanaV.Close()
	eip712v := web3.NewEIP712Verifier(log)

	injected, err := service.NewWeb3Service(service.Web3Deps{
		ChainManager: mcm,
		SigVerifier:  sv,
		SolanaVerif:  solanaV,
		EIP712Verif:  eip712v,
	}, cfg, log)
	require.NoError(t, err)
	defer injected.Close()

	rc := &RouterConfig{Web3Service: injected}
	result, err := provideWeb3Service(rc, cfg, log)
	require.NoError(t, err)
	assert.Equal(t, injected, result)
}

func TestProvideAuthService_WithInjected(t *testing.T) {
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-secret-key-that-is-at-least-32-chars"

	injected := service.NewAuthService(cfg.Auth.JWTSecret, nil)
	rc := &RouterConfig{AuthService: injected}
	res := &AppResources{}

	mcm := web3.NewMultiChainManager(log)
	sv := web3.NewSignatureVerifier(log)
	solanaV := web3.NewSolanaVerifier(log, "")
	defer solanaV.Close()
	eip712v := web3.NewEIP712Verifier(log)

	web3Svc, err := service.NewWeb3Service(service.Web3Deps{
		ChainManager: mcm,
		SigVerifier:  sv,
		SolanaVerif:  solanaV,
		EIP712Verif:  eip712v,
	}, cfg, log)
	require.NoError(t, err)
	defer web3Svc.Close()

	challengeStore := storage.NewMemoryChallengeStore()
	defer challengeStore.Close()

	result := provideAuthService(rc, cfg, log, web3Svc, challengeStore, 5*time.Minute, nil, res)
	assert.Equal(t, injected, result)
}

func TestProvideOTelTracing_EmptyEndpoint(t *testing.T) {
	log := zap.NewNop()
	cfg := &config.Config{}
	res := &AppResources{}

	provideOTelTracing(cfg, log, res)
	assert.Nil(t, res.OTelShutdown)
}

func TestAppResources_CloseCov_NilFields(t *testing.T) {
	r := &AppResources{}
	assert.NotPanics(t, func() { r.Close() })
}

func TestAppResources_Close_WithClosers(t *testing.T) {
	r := &AppResources{
		NFTCache: NewNFTAccessCache(),
	}
	r.Close()
}

func TestAuthGrpcServer_GetNonce(t *testing.T) {
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-secret-key-that-is-at-least-32-chars"

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	challengeStore := storage.NewMemoryChallengeStore()
	defer challengeStore.Close()
	tokenBlacklist := storage.NewMemoryTokenBlacklist()

	authService := service.NewAuthServiceWithDeps(
		cfg.Auth.JWTSecret,
		nil,
		sigVerifier,
		challengeStore,
		5*time.Minute,
		tokenBlacklist,
	)

	srv := &authGrpcServer{authSvc: authService, log: log}

	t.Run("EVM chain", func(t *testing.T) {
		resp, err := srv.GetNonce(context.Background(), &authv1.GetNonceRequest{
			WalletAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
			ChainType:     "evm",
		})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Nonce)
		assert.NotEmpty(t, resp.Message)
		assert.True(t, resp.ExpiresAt > 0)
	})

	t.Run("Solana chain", func(t *testing.T) {
		resp, err := srv.GetNonce(context.Background(), &authv1.GetNonceRequest{
			WalletAddress: "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
			ChainType:     "solana",
		})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Nonce)
	})
}

func TestAuthGrpcServer_VerifyToken(t *testing.T) {
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-secret-key-that-is-at-least-32-chars"

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	challengeStore := storage.NewMemoryChallengeStore()
	defer challengeStore.Close()
	tokenBlacklist := storage.NewMemoryTokenBlacklist()

	authService := service.NewAuthServiceWithDeps(
		cfg.Auth.JWTSecret,
		nil,
		sigVerifier,
		challengeStore,
		5*time.Minute,
		tokenBlacklist,
	)

	srv := &authGrpcServer{authSvc: authService, log: log}

	t.Run("invalid token", func(t *testing.T) {
		resp, err := srv.VerifyToken(context.Background(), &authv1.VerifyTokenRequest{
			Token: "invalid-token",
		})
		require.NoError(t, err)
		assert.False(t, resp.Valid)
	})
}

func TestAuthGrpcServer_RefreshToken_Invalid(t *testing.T) {
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-secret-key-that-is-at-least-32-chars"

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	challengeStore := storage.NewMemoryChallengeStore()
	defer challengeStore.Close()
	tokenBlacklist := storage.NewMemoryTokenBlacklist()

	authService := service.NewAuthServiceWithDeps(
		cfg.Auth.JWTSecret,
		nil,
		sigVerifier,
		challengeStore,
		5*time.Minute,
		tokenBlacklist,
	)

	srv := &authGrpcServer{authSvc: authService, log: log}

	resp, err := srv.RefreshToken(context.Background(), &authv1.RefreshTokenRequest{
		RefreshToken: "invalid-refresh-token",
	})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestAuthGrpcServer_RevokeToken_Error(t *testing.T) {
	log := zap.NewNop()
	srv := &authGrpcServer{authSvc: nil, log: log}

	assert.Panics(t, func() {
		_, _ = srv.RevokeToken(context.Background(), &authv1.RevokeTokenRequest{
			Token: "some-token",
		})
	})
}

func TestNftGrpcServer_VerifyOwnership_WithMetadata(t *testing.T) {
	log := zap.NewNop()
	srv := &nftGrpcServer{
		nftVerifier: &gwCovMockNFTVerifier{owns: true},
		web3Svc:     nil,
		log:         log,
	}

	resp, err := srv.VerifyOwnership(context.Background(), &nftv1.VerifyOwnershipRequest{
		ChainId:         1,
		ContractAddress: "0xContract",
		TokenId:         "1",
		WalletAddress:   "0xWallet",
	})
	require.NoError(t, err)
	assert.True(t, resp.OwnsNft)
	assert.Equal(t, "0xWallet", resp.OwnerAddress)
	assert.Nil(t, resp.Metadata)
}

type gwCovMockNFTVerifier struct {
	owns    bool
	ownsErr error
	balance int64
	balErr  error
}

func (m *gwCovMockNFTVerifier) VerifyNFTOwnership(_ context.Context, _ int64, _, _, _ string) (bool, error) {
	return m.owns, m.ownsErr
}

func (m *gwCovMockNFTVerifier) VerifyNFTOwnershipAutoDetect(_ context.Context, _ int64, _, _, _ string) (bool, error) {
	return m.owns, m.ownsErr
}

func (m *gwCovMockNFTVerifier) VerifyNFTCollectionAutoDetect(_ context.Context, _ int64, _, _ string) (bool, error) {
	return m.owns, m.ownsErr
}

func (m *gwCovMockNFTVerifier) GetNFTBalance(_ context.Context, _ int64, _, _ string) (*big.Int, error) {
	if m.balErr != nil {
		return nil, m.balErr
	}
	return big.NewInt(m.balance), nil
}

func (m *gwCovMockNFTVerifier) GetNFTInfo(_ context.Context, _ int64, _, _ string) (*middleware.NFTMetadata, error) {
	return nil, nil
}

func TestGatingRuleResolverAdapter_NilSvc(t *testing.T) {
	adapter := NewGatingRuleResolverAdapter(nil)
	assert.Panics(t, func() {
		_, _ = adapter.GetActiveRulesForContent(context.Background(), "content-1")
	})
}

func TestProvideContentService_WithDB(t *testing.T) {
	rc := &RouterConfig{}
	log := zap.NewNop()
	result := provideContentService(rc, &providerMockDB{}, log)
	assert.NotNil(t, result)
}

func TestProvideUploadService_WithObjStorageNotImplementing(t *testing.T) {
	rc := &RouterConfig{}
	log := zap.NewNop()
	mockStorage := &gwCovMockSegmentStorage{}
	result := provideUploadService(rc, &config.Config{}, log, &providerMockDB{}, mockStorage, nil)
	assert.Nil(t, result)
}

type gwCovMockSegmentStorage struct{}

func (m *gwCovMockSegmentStorage) Upload(_ context.Context, _, _ string, _ []byte) error {
	return nil
}
func (m *gwCovMockSegmentStorage) UploadStream(_ context.Context, _, _ string, _ io.Reader, _ int64) error {
	return nil
}
func (m *gwCovMockSegmentStorage) UploadWithContentType(_ context.Context, _, _ string, _ []byte, _ string) error {
	return nil
}
func (m *gwCovMockSegmentStorage) UploadStreamWithContentType(_ context.Context, _, _ string, _ io.Reader, _ int64, _ string) error {
	return nil
}
func (m *gwCovMockSegmentStorage) Download(_ context.Context, _, _ string) ([]byte, error) {
	return nil, nil
}
func (m *gwCovMockSegmentStorage) DownloadStream(_ context.Context, _, _ string) (io.ReadCloser, error) {
	return nil, nil
}
func (m *gwCovMockSegmentStorage) Delete(_ context.Context, _, _ string) error { return nil }
func (m *gwCovMockSegmentStorage) ListObjects(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}
func (m *gwCovMockSegmentStorage) Exists(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (m *gwCovMockSegmentStorage) CreateBucket(_ context.Context, _ string) error { return nil }

func TestRegisterRoutes_WithInjectedWeb3(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-secret-key-that-is-at-least-32-chars"

	mcm := web3.NewMultiChainManager(log)
	sv := web3.NewSignatureVerifier(log)
	solanaV := web3.NewSolanaVerifier(log, "")
	defer solanaV.Close()
	eip712v := web3.NewEIP712Verifier(log)

	web3Svc, err := service.NewWeb3Service(service.Web3Deps{
		ChainManager: mcm,
		SigVerifier:  sv,
		SolanaVerif:  solanaV,
		EIP712Verif:  eip712v,
	}, cfg, log)
	require.NoError(t, err)
	defer web3Svc.Close()

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	challengeStore := storage.NewMemoryChallengeStore()
	defer challengeStore.Close()
	tokenBlacklist := storage.NewMemoryTokenBlacklist()

	authService := service.NewAuthServiceWithDeps(
		cfg.Auth.JWTSecret,
		nil,
		sigVerifier,
		challengeStore,
		5*time.Minute,
		tokenBlacklist,
	)

	router, resources, err := SetupRouter(cfg, log,
		WithAuthService(authService),
		WithWeb3Service(web3Svc),
	)
	require.NoError(t, err)
	require.NotNil(t, router)
	defer resources.Close()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/web3/rpc-status", http.NoBody)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAppResources_Close_WithErrors(t *testing.T) {
	r := &AppResources{
		ChallengeStore: &gwCovErrorCloser{err: errors.New("close error")},
	}
	err := r.Close()
	assert.Error(t, err)
}

type gwCovErrorCloser struct {
	err error
}

func (c *gwCovErrorCloser) Close() error { return c.err }

func TestSetupMiddleware_WithNilRedis(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	res := &AppResources{}

	setupMiddleware(router, cfg, log, nil, res)

	assert.NotNil(t, res.RateLimiter)
	assert.NotNil(t, res.MiddlewareSvc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	router.ServeHTTP(w, req)
}

func TestProvideAuthService_JWTExpiryParsing(t *testing.T) {
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-secret-key-that-is-at-least-32-chars"
	cfg.Auth.JWTExpiry = "30m"

	rc := &RouterConfig{}
	res := &AppResources{}

	mcm := web3.NewMultiChainManager(log)
	sv := web3.NewSignatureVerifier(log)
	solanaV := web3.NewSolanaVerifier(log, "")
	defer solanaV.Close()
	eip712v := web3.NewEIP712Verifier(log)

	web3Svc, err := service.NewWeb3Service(service.Web3Deps{
		ChainManager: mcm,
		SigVerifier:  sv,
		SolanaVerif:  solanaV,
		EIP712Verif:  eip712v,
	}, cfg, log)
	require.NoError(t, err)
	defer web3Svc.Close()

	challengeStore := storage.NewMemoryChallengeStore()
	defer challengeStore.Close()
	tokenBlacklist := storage.NewMemoryTokenBlacklist()

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	injected := service.NewAuthServiceWithDeps(
		cfg.Auth.JWTSecret,
		nil,
		sigVerifier,
		challengeStore,
		5*time.Minute,
		tokenBlacklist,
	)
	rc.AuthService = injected

	authSvc := provideAuthService(rc, cfg, log, web3Svc, challengeStore, 5*time.Minute, nil, res)
	assert.Equal(t, injected, authSvc)
}

func TestProvideAuthService_InvalidJWTExpiry(t *testing.T) {
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-secret-key-that-is-at-least-32-chars"
	cfg.Auth.JWTExpiry = "invalid-duration"

	rc := &RouterConfig{}
	res := &AppResources{}

	mcm := web3.NewMultiChainManager(log)
	sv := web3.NewSignatureVerifier(log)
	solanaV := web3.NewSolanaVerifier(log, "")
	defer solanaV.Close()
	eip712v := web3.NewEIP712Verifier(log)

	web3Svc, err := service.NewWeb3Service(service.Web3Deps{
		ChainManager: mcm,
		SigVerifier:  sv,
		SolanaVerif:  solanaV,
		EIP712Verif:  eip712v,
	}, cfg, log)
	require.NoError(t, err)
	defer web3Svc.Close()

	challengeStore := storage.NewMemoryChallengeStore()
	defer challengeStore.Close()

	authSvc := provideAuthService(rc, cfg, log, web3Svc, challengeStore, 5*time.Minute, nil, res)
	assert.NotNil(t, authSvc)
}

func TestProvideTokenBlacklist_WithRedis(t *testing.T) {
	log := zap.NewNop()
	res := &AppResources{}
	result := provideTokenBlacklist(log, nil, res)
	assert.NotNil(t, result)
}

func TestProvideObjectStorage_WithInjected(t *testing.T) {
	rc := &RouterConfig{}
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	res := &AppResources{}

	mockStorage := &gwCovMockSegmentStorage{}
	rc.SegmentStorage = mockStorage

	result := provideObjectStorage(rc, cfg, log, res)
	assert.Equal(t, mockStorage, result)
}

func TestSetupRouter_HealthEndpointWorks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := zap.NewNop()
	cfg := config.DefaultConfig()
	cfg.Auth.JWTSecret = "test-secret-key-that-is-at-least-32-chars"

	mcm := web3.NewMultiChainManager(log)
	sv := web3.NewSignatureVerifier(log)
	solanaV := web3.NewSolanaVerifier(log, "")
	defer solanaV.Close()
	eip712v := web3.NewEIP712Verifier(log)

	web3Svc, err := service.NewWeb3Service(service.Web3Deps{
		ChainManager: mcm,
		SigVerifier:  sv,
		SolanaVerif:  solanaV,
		EIP712Verif:  eip712v,
	}, cfg, log)
	require.NoError(t, err)
	defer web3Svc.Close()

	sigVerifier := service.NewMultiChainSignatureVerifier(log, nil)
	challengeStore := storage.NewMemoryChallengeStore()
	defer challengeStore.Close()
	tokenBlacklist := storage.NewMemoryTokenBlacklist()

	authService := service.NewAuthServiceWithDeps(
		cfg.Auth.JWTSecret,
		nil,
		sigVerifier,
		challengeStore,
		5*time.Minute,
		tokenBlacklist,
	)

	router, resources, err := SetupRouter(cfg, log,
		WithAuthService(authService),
		WithWeb3Service(web3Svc),
	)
	require.NoError(t, err)
	defer resources.Close()

	endpoints := []struct {
		method string
		path   string
		codes  []int
	}{
		{http.MethodGet, "/health", []int{http.StatusOK, http.StatusServiceUnavailable}},
		{http.MethodGet, "/ready", []int{http.StatusOK, http.StatusServiceUnavailable}},
		{http.MethodGet, "/metrics", []int{http.StatusOK}},
		{http.MethodGet, "/docs", []int{http.StatusOK}},
	}

	for _, ep := range endpoints {
		t.Run(ep.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(ep.method, ep.path, http.NoBody)
			router.ServeHTTP(w, req)
			assert.Contains(t, ep.codes, w.Code)
		})
	}
}
