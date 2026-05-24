package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestAuthHandler(t *testing.T) *AuthHandler {
	t.Helper()
	kernel, err := core.NewMicrokernel(&config.Config{Mode: "monolith"}, zap.NewNop())
	require.NoError(t, err)
	verifier := NewAuthVerifier(zap.NewNop())
	return NewAuthHandler(verifier, zap.NewNop(), kernel)
}

func TestAuthHandler_HealthHandler_Healthy(t *testing.T) {
	handler := newTestAuthHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	handler.HealthHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthHandler_ReadyHandler(t *testing.T) {
	handler := newTestAuthHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ReadyHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthHandler_VerifySignatureHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestAuthHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/verify-signature", http.NoBody)
	rec := httptest.NewRecorder()

	handler.VerifySignatureHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestAuthHandler_VerifySignatureHandler_InvalidBody(t *testing.T) {
	handler := newTestAuthHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/verify-signature", bytes.NewReader([]byte("invalid")))
	rec := httptest.NewRecorder()

	handler.VerifySignatureHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAuthHandler_VerifySignatureHandler_NotConfigured(t *testing.T) {
	handler := newTestAuthHandler(t)

	body, _ := json.Marshal(map[string]string{
		"address":   "0x1234",
		"message":   "hello",
		"signature": "0xsig",
	})
	req := httptest.NewRequest(http.MethodPost, "/verify-signature", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.VerifySignatureHandler(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestAuthHandler_VerifySignatureHandler_WithMockVerifier(t *testing.T) {
	kernel, err := core.NewMicrokernel(&config.Config{Mode: "monolith"}, zap.NewNop())
	require.NoError(t, err)

	mockSig := &mockWalletSigVerifier{valid: true}
	verifier := NewAuthVerifierWithVerifiers(zap.NewNop(), mockSig, nil, nil)
	handler := NewAuthHandler(verifier, zap.NewNop(), kernel)

	body, _ := json.Marshal(map[string]string{
		"address":   "0x1234",
		"message":   "hello",
		"signature": "0xsig",
	})
	req := httptest.NewRequest(http.MethodPost, "/verify-signature", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.VerifySignatureHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["valid"])
}

func TestAuthHandler_VerifySignatureHandler_VerifierError(t *testing.T) {
	kernel, err := core.NewMicrokernel(&config.Config{Mode: "monolith"}, zap.NewNop())
	require.NoError(t, err)

	mockSig := &mockWalletSigVerifier{err: assert.AnError}
	verifier := NewAuthVerifierWithVerifiers(zap.NewNop(), mockSig, nil, nil)
	handler := NewAuthHandler(verifier, zap.NewNop(), kernel)

	body, _ := json.Marshal(map[string]string{
		"address":   "0x1234",
		"message":   "hello",
		"signature": "0xsig",
	})
	req := httptest.NewRequest(http.MethodPost, "/verify-signature", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.VerifySignatureHandler(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestAuthHandler_VerifyNFTHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestAuthHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/verify-nft", http.NoBody)
	rec := httptest.NewRecorder()

	handler.VerifyNFTHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestAuthHandler_VerifyNFTHandler_InvalidBody(t *testing.T) {
	handler := newTestAuthHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/verify-nft", bytes.NewReader([]byte("bad")))
	rec := httptest.NewRecorder()

	handler.VerifyNFTHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAuthHandler_VerifyNFTHandler_NotConfigured(t *testing.T) {
	handler := newTestAuthHandler(t)

	body, _ := json.Marshal(map[string]string{
		"address":          "0x1234",
		"contract_address": "0xcontract",
		"token_id":         "1",
	})
	req := httptest.NewRequest(http.MethodPost, "/verify-nft", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.VerifyNFTHandler(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestAuthHandler_VerifyNFTHandler_WithMockVerifier(t *testing.T) {
	kernel, err := core.NewMicrokernel(&config.Config{Mode: "monolith"}, zap.NewNop())
	require.NoError(t, err)

	mockNFT := &mockNFTVerifier{valid: true}
	verifier := NewAuthVerifierWithVerifiers(zap.NewNop(), nil, mockNFT, nil)
	handler := NewAuthHandler(verifier, zap.NewNop(), kernel)

	body, _ := json.Marshal(map[string]string{
		"address":          "0x1234",
		"contract_address": "0xcontract",
		"token_id":         "1",
	})
	req := httptest.NewRequest(http.MethodPost, "/verify-nft", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.VerifyNFTHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthHandler_VerifyTokenHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestAuthHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/verify-token", http.NoBody)
	rec := httptest.NewRecorder()

	handler.VerifyTokenHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestAuthHandler_VerifyTokenHandler_InvalidBody(t *testing.T) {
	handler := newTestAuthHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/verify-token", bytes.NewReader([]byte("bad")))
	rec := httptest.NewRecorder()

	handler.VerifyTokenHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAuthHandler_VerifyTokenHandler_WithMockVerifier(t *testing.T) {
	kernel, err := core.NewMicrokernel(&config.Config{Mode: "monolith"}, zap.NewNop())
	require.NoError(t, err)

	mockJWT := &mockJWTVerifier{valid: true}
	verifier := NewAuthVerifierWithVerifiers(zap.NewNop(), nil, nil, mockJWT)
	handler := NewAuthHandler(verifier, zap.NewNop(), kernel)

	body, _ := json.Marshal(map[string]string{
		"token": "test-jwt-token",
	})
	req := httptest.NewRequest(http.MethodPost, "/verify-token", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.VerifyTokenHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthHandler_GetChallengeHandler_MethodNotAllowed(t *testing.T) {
	handler := newTestAuthHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/challenge", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetChallengeHandler(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestAuthHandler_GetChallengeHandler_InvalidBody(t *testing.T) {
	handler := newTestAuthHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/challenge", bytes.NewReader([]byte("bad")))
	rec := httptest.NewRecorder()

	handler.GetChallengeHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAuthHandler_GetChallengeHandler_Success(t *testing.T) {
	handler := newTestAuthHandler(t)

	body, _ := json.Marshal(map[string]string{
		"address": "0x1234",
	})
	req := httptest.NewRequest(http.MethodPost, "/challenge", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.GetChallengeHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["challenge"])
}

func TestAuthHandler_NotFoundHandler(t *testing.T) {
	handler := newTestAuthHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()

	handler.NotFoundHandler(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestNewAuthServer(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	cfg.Server.ReadTimeout = 1
	cfg.Server.WriteTimeout = 1

	kernel, err := core.NewMicrokernel(cfg, zap.NewNop())
	require.NoError(t, err)

	server, err := NewAuthServer(cfg, zap.NewNop(), kernel)
	require.NoError(t, err)
	assert.NotNil(t, server)
	assert.NotNil(t, server.verifier)
}

func TestAuthServer_Health_NotStarted(t *testing.T) {
	cfg := &config.Config{Mode: "monolith"}
	server := &AuthServer{config: cfg, logger: zap.NewNop()}

	err := server.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestAuthVerifier_VerifySignature_NotConfigured(t *testing.T) {
	v := NewAuthVerifier(zap.NewNop())
	_, err := v.VerifySignature(context.Background(), "addr", "msg", "sig")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestAuthVerifier_VerifyNFT_NotConfigured(t *testing.T) {
	v := NewAuthVerifier(zap.NewNop())
	_, err := v.VerifyNFT(context.Background(), "addr", "contract", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestAuthVerifier_VerifyToken_NotConfigured(t *testing.T) {
	v := NewAuthVerifier(zap.NewNop())
	_, err := v.VerifyToken(context.Background(), "token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestAuthVerifier_VerifySignature_WithVerifier(t *testing.T) {
	mock := &mockWalletSigVerifier{valid: true}
	v := NewAuthVerifierWithVerifiers(zap.NewNop(), mock, nil, nil)

	valid, err := v.VerifySignature(context.Background(), "addr", "msg", "sig")
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestAuthVerifier_VerifyNFT_WithVerifier(t *testing.T) {
	mock := &mockNFTVerifier{valid: true}
	v := NewAuthVerifierWithVerifiers(zap.NewNop(), nil, mock, nil)

	valid, err := v.VerifyNFT(context.Background(), "addr", "contract", "1")
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestAuthVerifier_VerifyToken_WithVerifier(t *testing.T) {
	mock := &mockJWTVerifier{valid: true}
	v := NewAuthVerifierWithVerifiers(zap.NewNop(), nil, nil, mock)

	valid, err := v.VerifyToken(context.Background(), "token")
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestAuthVerifier_GetChallenge(t *testing.T) {
	v := NewAuthVerifier(zap.NewNop())

	challenge, err := v.GetChallenge(context.Background(), "0x1234")
	require.NoError(t, err)
	assert.Contains(t, challenge, "0x1234")
	assert.Contains(t, challenge, "StreamGate")
}

func TestNewMultiChainVerifier(t *testing.T) {
	v := NewMultiChainVerifier()
	assert.NotNil(t, v)
}

func TestMultiChainVerifier_VerifyEVM_NotConfigured(t *testing.T) {
	v := NewMultiChainVerifier()
	_, err := v.VerifyEVM(context.Background(), "addr", "msg", "sig")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "EVM")
}

func TestMultiChainVerifier_VerifySolana_NotConfigured(t *testing.T) {
	v := NewMultiChainVerifier()
	_, err := v.VerifySolana(context.Background(), "addr", "msg", "sig")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Solana")
}

func TestMultiChainVerifier_VerifyEVM_WithVerifier(t *testing.T) {
	mock := &mockWalletSigVerifier{valid: true}
	v := NewMultiChainVerifierWithVerifiers(mock, nil)

	valid, err := v.VerifyEVM(context.Background(), "addr", "msg", "sig")
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestMultiChainVerifier_VerifySolana_WithVerifier(t *testing.T) {
	mock := &mockSolanaVerifier{valid: true}
	v := NewMultiChainVerifierWithVerifiers(nil, mock)

	valid, err := v.VerifySolana(context.Background(), "addr", "msg", "sig")
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestNewWeb3Client(t *testing.T) {
	client := NewWeb3Client("http://localhost:8545")
	assert.NotNil(t, client)
	assert.Equal(t, "http://localhost:8545", client.rpcURL)
}

func TestWeb3Client_GetBalance(t *testing.T) {
	client := NewWeb3Client("http://localhost:8545")
	balance, err := client.GetBalance("0x1234")
	require.NoError(t, err)
	assert.Equal(t, "0", balance)
}

func TestNewSignatureHelper(t *testing.T) {
	sh := NewSignatureHelper()
	assert.NotNil(t, sh)
}

func TestSignatureHelper_Verify_NotConfigured(t *testing.T) {
	sh := NewSignatureHelper()
	_, err := sh.Verify(context.Background(), "addr", "msg", "sig")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestSignatureHelper_Verify_WithVerifier(t *testing.T) {
	mock := &mockWalletSigVerifier{valid: true}
	sh := NewSignatureHelperWithVerifier(mock)

	valid, err := sh.Verify(context.Background(), "addr", "msg", "sig")
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestSignatureHelper_RecoverAddress_NotConfigured(t *testing.T) {
	sh := NewSignatureHelper()
	_, err := sh.RecoverAddress(context.Background(), "msg", "sig")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestSignatureHelper_RecoverAddress_ConfiguredButUnsupported(t *testing.T) {
	mock := &mockWalletSigVerifier{valid: true}
	sh := NewSignatureHelperWithVerifier(mock)

	_, err := sh.RecoverAddress(context.Background(), "msg", "sig")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not directly supported")
}

func TestAuthMiddleware_HandleAuth_ResponseVerification(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), nil)
	sm := NewSessionManager(zap.NewNop(), &SessionConfig{SessionTTL: 1 * time.Hour, CleanupInterval: 1 * time.Hour})
	defer sm.Close()

	middleware := NewAuthMiddleware(cra, sm, true, zap.NewNop())
	ctx := context.Background()

	challengeResp, err := middleware.HandleChallenge(ctx, &ChallengeRequest{ClientID: "client-1"})
	require.NoError(t, err)
	assert.NotEmpty(t, challengeResp.ChallengeID)

	verifier := NewSHA256Verifier("secret")
	challenge, err := cra.GetChallenge(ctx, challengeResp.ChallengeID)
	require.NoError(t, err)

	expectedResponse, err := verifier.ComputeResponse(challenge.Nonce)
	require.NoError(t, err)

	authResp, err := middleware.HandleAuth(ctx, &AuthRequest{
		ChallengeID: challengeResp.ChallengeID,
		Response:    expectedResponse,
	}, verifier)
	require.NoError(t, err)
	assert.NotEmpty(t, authResp.SessionID)
}

func TestAuthMiddleware_HandleAuth_InvalidResponse(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), nil)
	sm := NewSessionManager(zap.NewNop(), &SessionConfig{SessionTTL: 1 * time.Hour, CleanupInterval: 1 * time.Hour})
	defer sm.Close()

	middleware := NewAuthMiddleware(cra, sm, true, zap.NewNop())
	ctx := context.Background()

	challengeResp, err := middleware.HandleChallenge(ctx, &ChallengeRequest{ClientID: "client-1"})
	require.NoError(t, err)

	verifier := NewSHA256Verifier("secret")
	_, err = middleware.HandleAuth(ctx, &AuthRequest{
		ChallengeID: challengeResp.ChallengeID,
		Response:    "wrong-response",
	}, verifier)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid response")
}

func TestAuthMiddleware_HandleAuth_SignatureWithoutInterface(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), nil)
	sm := NewSessionManager(zap.NewNop(), &SessionConfig{SessionTTL: 1 * time.Hour, CleanupInterval: 1 * time.Hour})
	defer sm.Close()

	middleware := NewAuthMiddleware(cra, sm, true, zap.NewNop())
	ctx := context.Background()

	challengeResp, err := middleware.HandleChallenge(ctx, &ChallengeRequest{ClientID: "client-1"})
	require.NoError(t, err)

	verifier := NewSHA256Verifier("secret")
	_, err = middleware.HandleAuth(ctx, &AuthRequest{
		ChallengeID: challengeResp.ChallengeID,
		Signature:   "sig",
		PublicKey:   "pubkey",
	}, verifier)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support signature")
}

func TestSessionManager_ExpiredSession(t *testing.T) {
	sm := NewSessionManager(zap.NewNop(), &SessionConfig{SessionTTL: 1 * time.Nanosecond, CleanupInterval: 1 * time.Hour})
	defer sm.Close()

	ctx := context.Background()
	session, err := sm.CreateSession(ctx, "client-1", "pubkey-1")
	require.NoError(t, err)

	time.Sleep(1 * time.Millisecond)

	_, err = sm.GetSession(ctx, session.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestSessionManager_ValidateSession_Expired(t *testing.T) {
	sm := NewSessionManager(zap.NewNop(), &SessionConfig{SessionTTL: 1 * time.Nanosecond, CleanupInterval: 1 * time.Hour})
	defer sm.Close()

	ctx := context.Background()
	session, err := sm.CreateSession(ctx, "client-1", "pubkey-1")
	require.NoError(t, err)

	time.Sleep(1 * time.Millisecond)

	valid, err := sm.ValidateSession(ctx, session.ID)
	require.Error(t, err)
	assert.False(t, valid)
}

func TestSHA256Verifier_ComputeResponse(t *testing.T) {
	verifier := NewSHA256Verifier("test-secret")
	response, err := verifier.ComputeResponse("nonce123")
	require.NoError(t, err)

	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write([]byte("nonce123"))
	expected := hex.EncodeToString(mac.Sum(nil))

	assert.Equal(t, expected, response)
}

func TestHMACVerifier_ComputeResponse(t *testing.T) {
	verifier := NewHMACVerifier("test-secret")
	response, err := verifier.ComputeResponse("nonce123")
	require.NoError(t, err)
	assert.NotEmpty(t, response)
}

type mockWalletSigVerifier struct {
	valid bool
	err   error
}

func (m *mockWalletSigVerifier) VerifySignature(ctx context.Context, address, message, signature string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.valid, nil
}

type mockNFTVerifier struct {
	valid bool
	err   error
}

func (m *mockNFTVerifier) VerifyNFTOwnership(ctx context.Context, chainID int64, contractAddress, tokenID, ownerAddress string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.valid, nil
}

type mockJWTVerifier struct {
	valid bool
	err   error
}

func (m *mockJWTVerifier) VerifyToken(tokenString string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.valid, nil
}

type mockSolanaVerifier struct {
	valid bool
	err   error
}

func (m *mockSolanaVerifier) VerifySolanaSignature(ctx context.Context, address, message, signature string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.valid, nil
}
