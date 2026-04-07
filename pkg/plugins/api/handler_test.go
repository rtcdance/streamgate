package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
	"streamgate/pkg/service"
	"streamgate/pkg/web3"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()

	cfg, err := config.LoadConfig()
	require.NoError(t, err)
	cfg.Mode = "monolith"
	cfg.Web3.ChainID = 11155111
	cfg.Auth.JWTSecret = "test-secret"
	cfg.Auth.NonceExpiry = "5m"

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	return NewHandler(kernel, zap.NewNop())
}

func TestHandler_ChallengeAndLogin(t *testing.T) {
	handler := newTestHandler(t)
	verifier := web3.NewSignatureVerifier(zap.NewNop())
	handler.authService = configTestAuthService(verifier)

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	wallet := verifier.GetAddressFromPrivateKey(privateKey)

	challengeBody, _ := json.Marshal(map[string]interface{}{
		"wallet":   wallet,
		"chain_id": int64(11155111),
	})
	challengeReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/challenge", bytes.NewReader(challengeBody))
	challengeRec := httptest.NewRecorder()
	handler.AuthChallengeHandler(challengeRec, challengeReq)
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
	handler.AuthLoginHandler(loginRec, loginReq)
	require.Equal(t, http.StatusOK, loginRec.Code)
	assert.Contains(t, loginRec.Body.String(), wallet)
}

func TestHandler_ProtectedManifestRequiresToken(t *testing.T) {
	handler := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/streaming/test/manifest.m3u8?contract=0x123", nil)
	rec := httptest.NewRecorder()
	handler.StreamingAccessHandler(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_ProtectedSegmentRequiresPlaybackToken(t *testing.T) {
	handler := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/streaming/test/segment/0", nil)
	rec := httptest.NewRecorder()
	handler.StreamingAccessHandler(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_ProtectedSegmentAcceptsPlaybackToken(t *testing.T) {
	handler := newTestHandler(t)
	handler.authService = configTestAuthService(web3.NewSignatureVerifier(zap.NewNop()))

	token, err := handler.authService.GeneratePlaybackToken(
		"0x1234567890123456789012345678901234567890",
		"test",
		"0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		"",
		11155111,
		time.Minute,
	)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/streaming/test/segment/0?playback_token="+token, nil)
	rec := httptest.NewRecorder()
	handler.StreamingAccessHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "segment", rec.Body.String())
}

func TestHandler_MetricsExposeAuthAndStreamingCounters(t *testing.T) {
	handler := newTestHandler(t)
	handler.metricsCollector.IncrementCounter("auth_login_success_total", nil)
	handler.metricsCollector.IncrementCounter("streaming_segment_success_total", nil)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.MetricsHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "streamgate_requests_total")
	assert.Contains(t, rec.Body.String(), "auth_login_success_total")
	assert.Contains(t, rec.Body.String(), "streaming_segment_success_total")
}

func TestHandler_VerifyNFTReturnsCacheHitOnSecondRequest(t *testing.T) {
	handler := newTestHandler(t)
	handler.setCachedNFTAccess(
		nftAccessCacheKey(11155111, "0x1234567890123456789012345678901234567890", "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f", ""),
		cachedNFTAccess{
			HasNFT:    true,
			Balance:   2,
			ExpiresAt: time.Now().Add(time.Minute),
		},
	)

	body, _ := json.Marshal(map[string]interface{}{
		"chain_id": 11155111,
		"wallet":   "0x1234567890123456789012345678901234567890",
		"contract": "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nft/verify", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.VerifyNFTHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"cache_hit":true`)
	assert.Contains(t, rec.Body.String(), `"balance":2`)
}

func configTestAuthService(verifier *web3.SignatureVerifier) *service.AuthService {
	return service.NewAuthServiceWithDeps("test-secret", nil, verifier, service.NewMemoryChallengeStore(), 5*time.Minute)
}
