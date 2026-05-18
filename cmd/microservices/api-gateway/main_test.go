package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"streamgate/pkg/core/config"
	"streamgate/pkg/gateway"
	"streamgate/pkg/middleware"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/service"
	"streamgate/pkg/storage"
	"streamgate/pkg/web3"
)

type mockSegmentStorage struct {
	objects []string
}

func (m *mockSegmentStorage) Upload(ctx context.Context, bucket, objectName string, data []byte) error {
	return nil
}

func (m *mockSegmentStorage) UploadStream(ctx context.Context, bucket, objectName string, reader io.Reader, size int64) error {
	return nil
}

func (m *mockSegmentStorage) UploadWithContentType(ctx context.Context, bucket, objectName string, data []byte, contentType string) error {
	return nil
}

func (m *mockSegmentStorage) UploadStreamWithContentType(ctx context.Context, bucket, objectName string, reader io.Reader, size int64, contentType string) error {
	return nil
}

func (m *mockSegmentStorage) Download(ctx context.Context, bucket, objectName string) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockSegmentStorage) DownloadStream(ctx context.Context, bucket, objectName string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockSegmentStorage) Delete(ctx context.Context, bucket, objectName string) error {
	return nil
}

func (m *mockSegmentStorage) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	return m.objects, nil
}

func (m *mockSegmentStorage) Exists(ctx context.Context, bucket, objectName string) (bool, error) {
	return false, nil
}

type mockNFTAccessVerifier struct {
	verifyResult bool
	verifyErr    error
	balance      *big.Int
	balanceErr   error
}

type mockWeb3StatusProvider struct{}

func (m *mockNFTAccessVerifier) VerifyNFTOwnership(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error) {
	return m.verifyResult, m.verifyErr
}

func (m *mockNFTAccessVerifier) GetNFTBalance(ctx context.Context, chainID int64, contractAddress, ownerAddress string) (*big.Int, error) {
	return m.balance, m.balanceErr
}

func (m *mockNFTAccessVerifier) VerifyNFTCollectionAutoDetect(ctx context.Context, contractAddress, ownerAddress string) (bool, error) {
	return m.verifyResult, m.verifyErr
}

func (m *mockNFTAccessVerifier) VerifyNFTOwnershipAutoDetect(ctx context.Context, contractAddress, tokenID, ownerAddress string) (bool, error) {
	return m.verifyResult, m.verifyErr
}

func (m *mockWeb3StatusProvider) GetRPCStatuses() map[int64][]web3.RPCStatus {
	return map[int64][]web3.RPCStatus{
		11155111: {
			{
				URL:      "https://rpc-a.example",
				IsActive: false,
				Failures: 2,
			},
			{
				URL:      "https://rpc-b.example",
				IsActive: true,
				Failures: 0,
			},
		},
	}
}

func (m *mockWeb3StatusProvider) GetSupportedChains() []*web3.ChainConfig {
	return []*web3.ChainConfig{
		{ID: 11155111, Name: "Ethereum Sepolia"},
	}
}

func newTestRouter(authService *service.AuthService, verifier middleware.NFTOwnershipChecker, segStorage service.SegmentStorage) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	cache := gateway.NewNFTAccessCache()
	transcodingSvc := service.NewTranscodingService(nil, service.NewMemoryTranscodingQueue())
	metricsCollector := monitoring.NewMetricsCollector(zap.NewNop())
	serviceMetrics := monitoring.NewServiceMetricsTracker(zap.NewNop())

	router.Use(func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		metricsCollector.IncrementCounter("http_requests_total", map[string]string{
			"method": c.Request.Method,
			"route":  route,
			"status": strconv.Itoa(c.Writer.Status()),
		})
		serviceMetrics.RecordRequest("api-gateway", time.Since(startedAt).Milliseconds(), c.Writer.Status() < http.StatusInternalServerError)
	})

	router.GET("/health", func(c *gin.Context) {
		metricsCollector.IncrementCounter("health_check_success_total", nil)
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"service":   "api-gateway",
			"timestamp": time.Now().Unix(),
		})
	})
	router.GET("/metrics", func(c *gin.Context) {
		promhttp.Handler().ServeHTTP(c.Writer, c.Request)
	})
	gateway.RegisterAuthRoutes(router, zap.NewNop(), config.DefaultConfig(), authService)
	gateway.RegisterWeb3Routes(router, zap.NewNop(), &mockWeb3StatusProvider{})

	// Auth-required routes
	jwtConfig := middleware.JWTAuthConfig{
		Secret: "test-secret-that-is-at-least-32-chars",
		SkipPaths: []string{
			"/api/v1/auth/challenge",
			"/api/v1/auth/login",
			"/api/v1/web3/rpc-status",
			"/api/v1/web3/supported-chains",
		},
	}
	authGroup := router.Group("/")
	authGroup.Use(middleware.JWTAuthMiddleware(jwtConfig, zap.NewNop()))
	{
		gateway.RegisterNFTRoutes(authGroup, zap.NewNop(), verifier, &gateway.NFTAccessCacheAdapter{Cache: cache}, 11155111, 60*time.Second)
		gateway.RegisterUploadRoutes(authGroup, zap.NewNop(), nil)

		nftGateConfig := middleware.NFTGateConfig{
			Verifier:       verifier,
			Cache:          &gateway.NFTAccessCacheAdapter{Cache: cache},
			DefaultChainID: 11155111,
			CacheTTL:       60 * time.Second,
		}
		nftGroup := authGroup.Group("/")
	nftGroup.Use(middleware.NFTGateMiddleware(nftGateConfig, zap.NewNop()))
	gateway.RegisterStreamingRoutes(nftGroup, zap.NewNop(), authService, segStorage, nil)

		// Segment route uses playback token, not NFT gate
		authGroup.GET("/api/v1/streaming/:id/segment/:num", func(c *gin.Context) {
			playbackToken := c.Query("playback_token")
			if playbackToken == "" {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "missing playback token"})
				return
			}
			if _, err := authService.ValidatePlaybackToken(c.Request.Context(), playbackToken, c.Param("id")); err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid playback token"})
				return
			}
			c.String(http.StatusOK, "segment")
		})

		// Content routes (auth required)
		gateway.RegisterContentRoutes(authGroup, zap.NewNop(), nil)

		// Transcoding routes (auth required)
		gateway.RegisterTranscodingRoutes(authGroup, zap.NewNop(), transcodingSvc)
	}
	return router
}

func newTestAuthService() (*service.AuthService, *web3.SignatureVerifier) {
	verifier := web3.NewSignatureVerifier(zap.NewNop())
	return service.NewAuthServiceWithDeps(
		"test-secret-that-is-at-least-32-chars",
		nil,
		verifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		nil,
	), verifier
}

// testJWT generates a valid JWT for testing protected routes.
func testJWT(walletAddress string) string {
	claims := jwt.MapClaims{
		"wallet_address": walletAddress,
		"username":       walletAddress,
		"sub":            walletAddress,
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, _ := token.SignedString([]byte("test-secret-that-is-at-least-32-chars"))
	return s
}

func TestRegisterAuthRoutes_ChallengeAndLogin(t *testing.T) {
	authService, verifier := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{}, nil)

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	wallet := verifier.GetAddressFromPrivateKey(privateKey)

	challengeBody, _ := json.Marshal(map[string]interface{}{
		"wallet":   wallet,
		"chain_id": int64(11155111),
	})
	challengeReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/challenge", bytes.NewReader(challengeBody))
	challengeRec := httptest.NewRecorder()
	router.ServeHTTP(challengeRec, challengeReq)
	require.Equal(t, http.StatusOK, challengeRec.Code)

	var challengeResp struct {
		ChallengeID string `json:"challenge_id"`
		Message     string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(challengeRec.Body.Bytes(), &challengeResp))
	require.NotEmpty(t, challengeResp.ChallengeID)

	signature, err := verifier.SignMessage(challengeResp.Message, privateKey)
	require.NoError(t, err)

	loginBody, _ := json.Marshal(map[string]string{
		"wallet":       wallet,
		"challenge_id": challengeResp.ChallengeID,
		"signature":    signature,
	})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)

	assert.Equal(t, http.StatusOK, loginRec.Code)
	assert.Contains(t, loginRec.Body.String(), wallet)
}

func TestRegisterAuthRoutes_LoginRejectsReplay(t *testing.T) {
	authService, verifier := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{}, nil)

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	wallet := verifier.GetAddressFromPrivateKey(privateKey)

	challenge, err := authService.GenerateWalletChallenge(context.Background(), wallet, 11155111)
	require.NoError(t, err)

	signature, err := verifier.SignMessage(challenge.Message, privateKey)
	require.NoError(t, err)

	loginBody, _ := json.Marshal(map[string]string{
		"wallet":       wallet,
		"challenge_id": challenge.ID,
		"signature":    signature,
	})

	firstReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, firstReq)
	require.Equal(t, http.StatusOK, firstRec.Code)

	secondReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, secondReq)

	assert.Equal(t, http.StatusUnauthorized, secondRec.Code)
	assert.Contains(t, secondRec.Body.String(), "authentication failed")
}

func TestRegisterStreamingRoutes_SegmentRequiresPlaybackToken(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/streaming/demo/segment/0", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+testJWT("0x1234567890123456789012345678901234567890"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRegisterStreamingRoutes_SegmentAcceptsPlaybackToken(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{}, nil)

	token, err := authService.GeneratePlaybackToken(
		context.Background(),
		"0x1234567890123456789012345678901234567890",
		"demo",
		"0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		"",
		11155111,
		time.Minute,
	)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/streaming/demo/segment/0?playback_token="+token, http.NoBody)
	req.Header.Set("Authorization", "Bearer "+testJWT("0x1234567890123456789012345678901234567890"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "segment", rec.Body.String())
}

func TestRegisterStreamingRoutes_ManifestSuccess(t *testing.T) {
	authService, verifier := newTestAuthService()
	segStorage := &mockSegmentStorage{
		objects: []string{
			"streams/demo/720p/seg0000.ts",
			"streams/demo/720p/seg0001.ts",
		},
	}
	router := newTestRouter(authService, &mockNFTAccessVerifier{balance: big.NewInt(1)}, segStorage)

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	wallet := verifier.GetAddressFromPrivateKey(privateKey)

	challenge, err := authService.GenerateWalletChallenge(context.Background(), wallet, 11155111)
	require.NoError(t, err)

	signature, err := verifier.SignMessage(challenge.Message, privateKey)
	require.NoError(t, err)

	token, err := authService.AuthenticateWithWallet(context.Background(), wallet, challenge.ID, signature)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/streaming/demo/manifest.m3u8?contract=0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		http.NoBody,
	)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "#EXTM3U")
	assert.Contains(t, rec.Body.String(), "playback_token=")
}

func TestRegisterNFTRoutes_VerifyByBalance(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{balance: big.NewInt(2)}, nil)

	body, _ := json.Marshal(map[string]interface{}{
		"wallet":   "0x1234567890123456789012345678901234567890",
		"contract": "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		"chain_id": int64(11155111),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testJWT("0x1234567890123456789012345678901234567890"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"has_nft":true`)
	assert.Contains(t, rec.Body.String(), `"balance":"2"`)
	assert.Contains(t, rec.Body.String(), `"cache_hit":false`)
}

func TestRegisterNFTRoutes_VerifyByTokenOwnership(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{verifyResult: true}, nil)

	body, _ := json.Marshal(map[string]interface{}{
		"wallet":   "0x1234567890123456789012345678901234567890",
		"contract": "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		"token_id": "1",
		"chain_id": int64(11155111),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testJWT("0x1234567890123456789012345678901234567890"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"has_nft":true`)
	assert.Contains(t, rec.Body.String(), `"balance":"1"`)
}

func TestRegisterNFTRoutes_VerifyReturnsCacheHitOnSecondRequest(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{balance: big.NewInt(2)}, nil)

	body, _ := json.Marshal(map[string]interface{}{
		"wallet":   "0x1234567890123456789012345678901234567890",
		"contract": "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		"chain_id": int64(11155111),
	})

	firstReq := httptest.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewReader(body))
	firstReq.Header.Set("Authorization", "Bearer "+testJWT("0x1234567890123456789012345678901234567890"))
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, firstReq)
	require.Equal(t, http.StatusOK, firstRec.Code)
	assert.Contains(t, firstRec.Body.String(), `"cache_hit":false`)

	secondReq := httptest.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewReader(body))
	secondReq.Header.Set("Authorization", "Bearer "+testJWT("0x1234567890123456789012345678901234567890"))
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, secondReq)
	require.Equal(t, http.StatusOK, secondRec.Code)
	assert.Contains(t, secondRec.Body.String(), `"cache_hit":true`)
	assert.Contains(t, secondRec.Body.String(), `"balance":"2"`)
}

func TestNFTAccessCache_ExpiresEntry(t *testing.T) {
	cache := gateway.NewNFTAccessCache()
	cache.Set("demo", gateway.CachedNFTAccess{
		HasNFT:    true,
		Balance:   big.NewInt(1),
		ExpiresAt: time.Now().Add(-time.Second),
	})

	entry, ok := cache.Get("demo")
	assert.False(t, ok)
	assert.Equal(t, gateway.CachedNFTAccess{}, entry)
}

func TestContentRoutes_RequireAuth(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{}, nil)

	// Without auth
	req := httptest.NewRequest(http.MethodGet, "/api/v1/content", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// With auth
	req = httptest.NewRequest(http.MethodGet, "/api/v1/content", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+testJWT("0x1234567890123456789012345678901234567890"))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	// Returns 503 because no ContentService/DB is configured
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestRegisterTranscodingRoutes_SubmitAndStatus(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{}, nil)

	body, _ := json.Marshal(map[string]interface{}{
		"content_id": "content-42",
		"profile":    "720p",
		"input_url":  "https://example.com/input.mp4",
		"priority":   3,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transcode/submit", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testJWT("0x1234567890123456789012345678901234567890"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), `"task_id":`)

	var resp struct {
		TaskID string `json:"task_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.TaskID)

	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/transcode/status/"+resp.TaskID, http.NoBody)
	statusReq.Header.Set("Authorization", "Bearer "+testJWT("0x1234567890123456789012345678901234567890"))
	statusRec := httptest.NewRecorder()
	router.ServeHTTP(statusRec, statusReq)

	require.Equal(t, http.StatusOK, statusRec.Code)
	assert.Contains(t, statusRec.Body.String(), resp.TaskID)
	assert.Contains(t, statusRec.Body.String(), `"content_id":"content-42"`)
}

func TestRegisterTranscodingRoutes_ListTasks(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{}, nil)
	jwtToken := testJWT("0x1234567890123456789012345678901234567890")

	body, _ := json.Marshal(map[string]interface{}{
		"content_id": "content-list",
		"profile":    "720p",
		"input_url":  "https://example.com/input.mp4",
		"priority":   1,
	})
	submitReq := httptest.NewRequest(http.MethodPost, "/api/v1/transcode/submit", bytes.NewReader(body))
	submitReq.Header.Set("Authorization", "Bearer "+jwtToken)
	submitRec := httptest.NewRecorder()
	router.ServeHTTP(submitRec, submitReq)
	require.Equal(t, http.StatusAccepted, submitRec.Code)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/transcode/tasks?content_id=content-list", http.NoBody)
	listReq.Header.Set("Authorization", "Bearer "+jwtToken)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)

	require.Equal(t, http.StatusOK, listRec.Code)
	assert.Contains(t, listRec.Body.String(), `"tasks":`)
	assert.Contains(t, listRec.Body.String(), `"content_id":"content-list"`)
}

func TestRegisterTranscodingRoutes_ListTasksPagination(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{}, nil)
	jwtToken := testJWT("0x1234567890123456789012345678901234567890")

	body1, _ := json.Marshal(map[string]interface{}{
		"content_id": "content-a",
		"profile":    "720p",
		"input_url":  "https://example.com/a1.mp4",
		"priority":   1,
	})
	body2, _ := json.Marshal(map[string]interface{}{
		"content_id": "content-a",
		"profile":    "480p",
		"input_url":  "https://example.com/a2.mp4",
		"priority":   1,
	})
	body3, _ := json.Marshal(map[string]interface{}{
		"content_id": "content-b",
		"profile":    "1080p",
		"input_url":  "https://example.com/b1.mp4",
		"priority":   1,
	})

	for _, body := range [][]byte{body1, body2, body3} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/transcode/submit", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusAccepted, rec.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/transcode/tasks?content_id=content-a&limit=1&offset=1", http.NoBody)
	listReq.Header.Set("Authorization", "Bearer "+jwtToken)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)

	require.Equal(t, http.StatusOK, listRec.Code)
	assert.Contains(t, listRec.Body.String(), `"tasks":`)
	assert.Contains(t, listRec.Body.String(), `"content_id":"content-a"`)
	assert.NotContains(t, listRec.Body.String(), `"content_id":"content-b"`)
}

func TestRegisterWeb3Routes_RPCStatus(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/web3/rpc-status", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"chain_id":11155111`)
	assert.Contains(t, rec.Body.String(), `"name":"Ethereum Sepolia"`)
	assert.Contains(t, rec.Body.String(), `"url":"https://rpc-b.example"`)
	assert.Contains(t, rec.Body.String(), `"is_active":true`)
}

func TestMetricsRoute_ExposesPrometheusOutput(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{}, nil)

	healthReq := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	healthRec := httptest.NewRecorder()
	router.ServeHTTP(healthRec, healthReq)
	require.Equal(t, http.StatusOK, healthRec.Code)

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	metricsRec := httptest.NewRecorder()
	router.ServeHTTP(metricsRec, metricsReq)

	require.Equal(t, http.StatusOK, metricsRec.Code)
	assert.Contains(t, metricsRec.Body.String(), "http_requests_total")
	assert.Contains(t, metricsRec.Body.String(), "health_check_success_total")
	assert.Contains(t, metricsRec.Body.String(), "streamgate_service_requests")
}
