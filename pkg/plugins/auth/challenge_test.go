package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewChallengeResponseAuth_DefaultConfig(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), nil)
	assert.NotNil(t, cra)
	assert.Equal(t, 5*time.Minute, cra.config.ChallengeTTL)
	assert.Equal(t, 3, cra.config.MaxAttempts)
	assert.True(t, cra.config.RequireSignature)
}

func TestNewChallengeResponseAuth_CustomConfig(t *testing.T) {
	config := &AuthConfig{
		ChallengeTTL:     10 * time.Minute,
		MaxAttempts:      5,
		RequireSignature: false,
	}
	cra := NewChallengeResponseAuth(zap.NewNop(), config)
	assert.Equal(t, 10*time.Minute, cra.config.ChallengeTTL)
	assert.Equal(t, 5, cra.config.MaxAttempts)
}

func TestChallengeResponseAuth_GenerateChallenge(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), nil)
	ctx := context.Background()

	challenge, err := cra.GenerateChallenge(ctx, "client-1")
	require.NoError(t, err)
	assert.NotEmpty(t, challenge.ID)
	assert.NotEmpty(t, challenge.Nonce)
	assert.False(t, challenge.ExpiresAt.IsZero())
	assert.False(t, challenge.Used)
	assert.Equal(t, 0, challenge.Attempts)
}

func TestChallengeResponseAuth_VerifyResponse_Success(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), &AuthConfig{ChallengeTTL: 5 * time.Minute, MaxAttempts: 3})
	ctx := context.Background()

	challenge, err := cra.GenerateChallenge(ctx, "client-1")
	require.NoError(t, err)

	verifier := NewSHA256Verifier("test-secret")
	expectedResponse, err := verifier.ComputeResponse(challenge.Nonce)
	require.NoError(t, err)

	valid, err := cra.VerifyResponse(ctx, challenge.ID, expectedResponse, verifier)
	require.NoError(t, err)
	assert.True(t, valid)

	retrieved, err := cra.GetChallenge(ctx, challenge.ID)
	require.NoError(t, err)
	assert.True(t, retrieved.Used)
}

func TestChallengeResponseAuth_VerifyResponse_InvalidResponse(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), &AuthConfig{ChallengeTTL: 5 * time.Minute, MaxAttempts: 3})
	ctx := context.Background()

	challenge, err := cra.GenerateChallenge(ctx, "client-1")
	require.NoError(t, err)

	verifier := NewSHA256Verifier("test-secret")
	valid, err := cra.VerifyResponse(ctx, challenge.ID, "wrong-response", verifier)
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestChallengeResponseAuth_VerifyResponse_ChallengeNotFound(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), nil)
	verifier := NewSHA256Verifier("secret")

	_, err := cra.VerifyResponse(context.Background(), "nonexistent", "response", verifier)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestChallengeResponseAuth_VerifyResponse_ChallengeAlreadyUsed(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), &AuthConfig{ChallengeTTL: 5 * time.Minute, MaxAttempts: 3})
	ctx := context.Background()

	challenge, err := cra.GenerateChallenge(ctx, "client-1")
	require.NoError(t, err)

	verifier := NewSHA256Verifier("test-secret")
	expectedResponse, err := verifier.ComputeResponse(challenge.Nonce)
	require.NoError(t, err)

	valid, err := cra.VerifyResponse(ctx, challenge.ID, expectedResponse, verifier)
	require.NoError(t, err)
	assert.True(t, valid)

	valid, err = cra.VerifyResponse(ctx, challenge.ID, expectedResponse, verifier)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already used")
}

func TestChallengeResponseAuth_VerifyResponse_ExpiredChallenge(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), &AuthConfig{ChallengeTTL: 1 * time.Nanosecond, MaxAttempts: 3})
	ctx := context.Background()

	challenge, err := cra.GenerateChallenge(ctx, "client-1")
	require.NoError(t, err)

	time.Sleep(1 * time.Millisecond)

	verifier := NewSHA256Verifier("test-secret")
	_, err = cra.VerifyResponse(ctx, challenge.ID, "response", verifier)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestChallengeResponseAuth_VerifyResponse_MaxAttemptsExceeded(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), &AuthConfig{ChallengeTTL: 5 * time.Minute, MaxAttempts: 1})
	ctx := context.Background()

	challenge, err := cra.GenerateChallenge(ctx, "client-1")
	require.NoError(t, err)

	verifier := NewSHA256Verifier("test-secret")
	valid, err := cra.VerifyResponse(ctx, challenge.ID, "wrong", verifier)
	require.NoError(t, err)
	assert.False(t, valid)

	_, err = cra.VerifyResponse(ctx, challenge.ID, "wrong", verifier)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max attempts")
}

func TestChallengeResponseAuth_VerifySignature_Success(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), &AuthConfig{ChallengeTTL: 5 * time.Minute, MaxAttempts: 3})
	ctx := context.Background()

	challenge, err := cra.GenerateChallenge(ctx, "client-1")
	require.NoError(t, err)

	mockVerifier := &mockSignatureVerifier{valid: true}
	valid, err := cra.VerifySignature(ctx, challenge.ID, "sig", "pubkey", mockVerifier)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestChallengeResponseAuth_VerifySignature_Invalid(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), &AuthConfig{ChallengeTTL: 5 * time.Minute, MaxAttempts: 3})
	ctx := context.Background()

	challenge, err := cra.GenerateChallenge(ctx, "client-1")
	require.NoError(t, err)

	mockVerifier := &mockSignatureVerifier{valid: false}
	valid, err := cra.VerifySignature(ctx, challenge.ID, "sig", "pubkey", mockVerifier)
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestChallengeResponseAuth_VerifySignature_NotFound(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), nil)
	mockVerifier := &mockSignatureVerifier{valid: true}

	_, err := cra.VerifySignature(context.Background(), "nonexistent", "sig", "pubkey", mockVerifier)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestChallengeResponseAuth_GetChallenge(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), nil)
	ctx := context.Background()

	challenge, err := cra.GenerateChallenge(ctx, "client-1")
	require.NoError(t, err)

	retrieved, err := cra.GetChallenge(ctx, challenge.ID)
	require.NoError(t, err)
	assert.Equal(t, challenge.ID, retrieved.ID)
}

func TestChallengeResponseAuth_GetChallenge_NotFound(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), nil)
	_, err := cra.GetChallenge(context.Background(), "nonexistent")
	require.Error(t, err)
}

func TestChallengeResponseAuth_CleanupExpiredChallenges(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), &AuthConfig{ChallengeTTL: 1 * time.Nanosecond, MaxAttempts: 3})
	ctx := context.Background()

	_, err := cra.GenerateChallenge(ctx, "client-1")
	require.NoError(t, err)

	time.Sleep(1 * time.Millisecond)

	err = cra.CleanupExpiredChallenges(ctx)
	require.NoError(t, err)

	assert.Len(t, cra.challenges, 0)
}

func TestSHA256Verifier(t *testing.T) {
	verifier := NewSHA256Verifier("secret")
	response, err := verifier.ComputeResponse("nonce")
	require.NoError(t, err)

	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write([]byte("nonce"))
	expected := hex.EncodeToString(mac.Sum(nil))

	assert.Equal(t, expected, response)
}

func TestHMACVerifier(t *testing.T) {
	verifier := NewHMACVerifier("secret")
	response, err := verifier.ComputeResponse("nonce")
	require.NoError(t, err)

	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write([]byte("nonce"))
	expected := hex.EncodeToString(mac.Sum(nil))

	assert.NotEqual(t, expected, response)
	assert.NotEmpty(t, response)
}

func TestNewSessionManager_DefaultConfig(t *testing.T) {
	sm := NewSessionManager(zap.NewNop(), nil)
	defer sm.Close()

	assert.NotNil(t, sm)
	assert.Equal(t, 24*time.Hour, sm.config.SessionTTL)
}

func TestSessionManager_CreateAndGetSession(t *testing.T) {
	sm := NewSessionManager(zap.NewNop(), &SessionConfig{SessionTTL: 1 * time.Hour, CleanupInterval: 1 * time.Hour})
	defer sm.Close()

	ctx := context.Background()
	session, err := sm.CreateSession(ctx, "client-1", "pubkey-1")
	require.NoError(t, err)
	assert.NotEmpty(t, session.ID)
	assert.Equal(t, "client-1", session.ClientID)
	assert.Equal(t, "pubkey-1", session.PublicKey)

	retrieved, err := sm.GetSession(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, session.ID, retrieved.ID)
}

func TestSessionManager_GetSession_NotFound(t *testing.T) {
	sm := NewSessionManager(zap.NewNop(), &SessionConfig{SessionTTL: 1 * time.Hour, CleanupInterval: 1 * time.Hour})
	defer sm.Close()

	_, err := sm.GetSession(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSessionManager_ValidateSession(t *testing.T) {
	sm := NewSessionManager(zap.NewNop(), &SessionConfig{SessionTTL: 1 * time.Hour, CleanupInterval: 1 * time.Hour})
	defer sm.Close()

	ctx := context.Background()
	session, err := sm.CreateSession(ctx, "client-1", "pubkey-1")
	require.NoError(t, err)

	valid, err := sm.ValidateSession(ctx, session.ID)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestSessionManager_RefreshSession(t *testing.T) {
	sm := NewSessionManager(zap.NewNop(), &SessionConfig{SessionTTL: 1 * time.Hour, CleanupInterval: 1 * time.Hour})
	defer sm.Close()

	ctx := context.Background()
	session, err := sm.CreateSession(ctx, "client-1", "pubkey-1")
	require.NoError(t, err)

	oldExpiry := session.ExpiresAt
	err = sm.RefreshSession(ctx, session.ID)
	require.NoError(t, err)

	retrieved, err := sm.GetSession(ctx, session.ID)
	require.NoError(t, err)
	assert.True(t, retrieved.ExpiresAt.After(oldExpiry))
}

func TestSessionManager_RefreshSession_NotFound(t *testing.T) {
	sm := NewSessionManager(zap.NewNop(), &SessionConfig{SessionTTL: 1 * time.Hour, CleanupInterval: 1 * time.Hour})
	defer sm.Close()

	err := sm.RefreshSession(context.Background(), "nonexistent")
	require.Error(t, err)
}

func TestSessionManager_RevokeSession(t *testing.T) {
	sm := NewSessionManager(zap.NewNop(), &SessionConfig{SessionTTL: 1 * time.Hour, CleanupInterval: 1 * time.Hour})
	defer sm.Close()

	ctx := context.Background()
	session, err := sm.CreateSession(ctx, "client-1", "pubkey-1")
	require.NoError(t, err)

	err = sm.RevokeSession(ctx, session.ID)
	require.NoError(t, err)

	_, err = sm.GetSession(ctx, session.ID)
	require.Error(t, err)
}

func TestNewAuthMiddleware_NoAuthRequired(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), nil)
	sm := NewSessionManager(zap.NewNop(), nil)
	defer sm.Close()

	middleware := NewAuthMiddleware(cra, sm, false, zap.NewNop())
	session, err := middleware.Authenticate(context.Background(), "any-session")
	require.NoError(t, err)
	assert.Nil(t, session)
}

func TestAuthMiddleware_HandleChallenge(t *testing.T) {
	cra := NewChallengeResponseAuth(zap.NewNop(), nil)
	sm := NewSessionManager(zap.NewNop(), nil)
	defer sm.Close()

	middleware := NewAuthMiddleware(cra, sm, true, zap.NewNop())
	ctx := context.Background()

	resp, err := middleware.HandleChallenge(ctx, &ChallengeRequest{ClientID: "client-1"})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.ChallengeID)
	assert.NotEmpty(t, resp.Nonce)
	assert.NotEmpty(t, resp.ExpiresAt)
}

type mockSignatureVerifier struct {
	valid bool
	err   error
}

func (m *mockSignatureVerifier) VerifySignature(publicKey, message, signature string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.valid, nil
}
