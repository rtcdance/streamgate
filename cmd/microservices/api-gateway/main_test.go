package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	apiV1 "streamgate/pkg/api/v1"
	"streamgate/pkg/service"
	"streamgate/pkg/web3"
)

type mockNFTAccessVerifier struct {
	verifyResult bool
	verifyErr    error
	balance      int64
	balanceErr   error
}

type mockWeb3StatusProvider struct{}

func (m *mockNFTAccessVerifier) VerifyNFTOwnership(ctx context.Context, chainID int64, contractAddress string, tokenID string, ownerAddress string) (bool, error) {
	return m.verifyResult, m.verifyErr
}

func (m *mockNFTAccessVerifier) GetNFTBalance(ctx context.Context, chainID int64, contractAddress string, ownerAddress string) (int64, error) {
	return m.balance, m.balanceErr
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

func newTestRouter(authService *service.AuthService, verifier nftAccessVerifier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	cache := newNFTAccessCache()
	transcodingHandler := apiV1.NewTranscodingHandler(service.NewTranscodingService(nil, service.NewMemoryTranscodingQueue()))
	registerAuthRoutes(router, zap.NewNop(), authService)
	registerNFTRoutes(router, zap.NewNop(), verifier, 11155111, cache)
	registerWeb3Routes(router, zap.NewNop(), &mockWeb3StatusProvider{})
	registerStreamingRoutes(router, zap.NewNop(), verifier, authService, 11155111, cache)
	registerTranscodingRoutes(router, zap.NewNop(), transcodingHandler)
	return router
}

func newTestAuthService() (*service.AuthService, *web3.SignatureVerifier) {
	verifier := web3.NewSignatureVerifier(zap.NewNop())
	return service.NewAuthServiceWithDeps(
		"test-secret",
		nil,
		verifier,
		service.NewMemoryChallengeStore(),
		5*time.Minute,
	), verifier
}

func TestRegisterAuthRoutes_ChallengeAndLogin(t *testing.T) {
	authService, verifier := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{})

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
	router := newTestRouter(authService, &mockNFTAccessVerifier{})

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	wallet := verifier.GetAddressFromPrivateKey(privateKey)

	challenge, err := authService.GenerateWalletChallenge(wallet, 11155111)
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
	assert.Contains(t, secondRec.Body.String(), "challenge")
}

func TestRegisterStreamingRoutes_SegmentRequiresPlaybackToken(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/streaming/demo/segment/0", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRegisterStreamingRoutes_SegmentAcceptsPlaybackToken(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{})

	token, err := authService.GeneratePlaybackToken(
		"0x1234567890123456789012345678901234567890",
		"demo",
		"0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		"",
		11155111,
		time.Minute,
	)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/streaming/demo/segment/0?playback_token="+token, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "segment", rec.Body.String())
}

func TestRegisterStreamingRoutes_ManifestSuccess(t *testing.T) {
	authService, verifier := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{balance: 1})

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	wallet := verifier.GetAddressFromPrivateKey(privateKey)

	challenge, err := authService.GenerateWalletChallenge(wallet, 11155111)
	require.NoError(t, err)

	signature, err := verifier.SignMessage(challenge.Message, privateKey)
	require.NoError(t, err)

	token, err := authService.AuthenticateWithWallet(wallet, challenge.ID, signature)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/streaming/demo/manifest.m3u8?contract=0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		nil,
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
	router := newTestRouter(authService, &mockNFTAccessVerifier{balance: 2})

	body, _ := json.Marshal(map[string]interface{}{
		"wallet":   "0x1234567890123456789012345678901234567890",
		"contract": "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		"chain_id": int64(11155111),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"has_nft":true`)
	assert.Contains(t, rec.Body.String(), `"balance":2`)
	assert.Contains(t, rec.Body.String(), `"cache_hit":false`)
}

func TestRegisterNFTRoutes_VerifyByTokenOwnership(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{verifyResult: true})

	body, _ := json.Marshal(map[string]interface{}{
		"wallet":   "0x1234567890123456789012345678901234567890",
		"contract": "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		"token_id": "1",
		"chain_id": int64(11155111),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"has_nft":true`)
	assert.Contains(t, rec.Body.String(), `"balance":1`)
}

func TestRegisterNFTRoutes_VerifyReturnsCacheHitOnSecondRequest(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{balance: 2})

	body, _ := json.Marshal(map[string]interface{}{
		"wallet":   "0x1234567890123456789012345678901234567890",
		"contract": "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		"chain_id": int64(11155111),
	})

	firstReq := httptest.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewReader(body))
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, firstReq)
	require.Equal(t, http.StatusOK, firstRec.Code)
	assert.Contains(t, firstRec.Body.String(), `"cache_hit":false`)

	secondReq := httptest.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewReader(body))
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, secondReq)
	require.Equal(t, http.StatusOK, secondRec.Code)
	assert.Contains(t, secondRec.Body.String(), `"cache_hit":true`)
	assert.Contains(t, secondRec.Body.String(), `"balance":2`)
}

func TestNFTAccessCache_ExpiresEntry(t *testing.T) {
	cache := newNFTAccessCache()
	cache.set("demo", cachedNFTAccess{
		HasNFT:    true,
		Balance:   1,
		ExpiresAt: time.Now().Add(-time.Second),
	})

	entry, ok := cache.get("demo")
	assert.False(t, ok)
	assert.Equal(t, cachedNFTAccess{}, entry)

	_, stillPresent := cache.entries["demo"]
	assert.False(t, stillPresent)
}

func TestRegisterTranscodingRoutes_SubmitAndStatus(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{})

	body, _ := json.Marshal(map[string]interface{}{
		"content_id": "content-42",
		"profile":    "720p",
		"input_url":  "https://example.com/input.mp4",
		"priority":   3,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transcode/submit", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), `"task_id":`)

	var resp struct {
		TaskID string `json:"task_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.TaskID)

	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/transcode/status/"+resp.TaskID, nil)
	statusRec := httptest.NewRecorder()
	router.ServeHTTP(statusRec, statusReq)

	require.Equal(t, http.StatusOK, statusRec.Code)
	assert.Contains(t, statusRec.Body.String(), resp.TaskID)
	assert.Contains(t, statusRec.Body.String(), `"content_id":"content-42"`)
}

func TestRegisterTranscodingRoutes_ListTasks(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{})

	body, _ := json.Marshal(map[string]interface{}{
		"content_id": "content-list",
		"profile":    "720p",
		"input_url":  "https://example.com/input.mp4",
		"priority":   1,
	})
	submitReq := httptest.NewRequest(http.MethodPost, "/api/v1/transcode/submit", bytes.NewReader(body))
	submitRec := httptest.NewRecorder()
	router.ServeHTTP(submitRec, submitReq)
	require.Equal(t, http.StatusAccepted, submitRec.Code)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/transcode/tasks?content_id=content-list", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)

	require.Equal(t, http.StatusOK, listRec.Code)
	assert.Contains(t, listRec.Body.String(), `"tasks":`)
	assert.Contains(t, listRec.Body.String(), `"content_id":"content-list"`)
}

func TestRegisterTranscodingRoutes_ListTasksPagination(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{})

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
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusAccepted, rec.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/transcode/tasks?content_id=content-a&limit=1&offset=1", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)

	require.Equal(t, http.StatusOK, listRec.Code)
	assert.Contains(t, listRec.Body.String(), `"tasks":`)
	assert.Contains(t, listRec.Body.String(), `"content_id":"content-a"`)
	assert.NotContains(t, listRec.Body.String(), `"content_id":"content-b"`)
}

func TestRegisterWeb3Routes_RPCStatus(t *testing.T) {
	authService, _ := newTestAuthService()
	router := newTestRouter(authService, &mockNFTAccessVerifier{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/web3/rpc-status", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"chain_id":11155111`)
	assert.Contains(t, rec.Body.String(), `"name":"Ethereum Sepolia"`)
	assert.Contains(t, rec.Body.String(), `"url":"https://rpc-b.example"`)
	assert.Contains(t, rec.Body.String(), `"is_active":true`)
}
