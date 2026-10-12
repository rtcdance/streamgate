package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const signTypeTestSecret = "sign-type-test-secret-at-least-32-chars!"
const signTypeTestWallet = "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"

// An unrecognised signing type used to fall through to the plain personal_sign
// message, so a typo or an unsupported wallet silently got a weaker scheme than
// requested. It must be rejected instead.
func TestGenerateWalletChallenge_RejectsUnknownSigningType(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)

	for _, st := range []string{"foo", "siwe2", "metamask", "personal-sign"} {
		_, err := svc.GenerateWalletChallenge(context.Background(), signTypeTestWallet, 1, st)
		require.Error(t, err, "signing type %q must be rejected", st)
		assert.True(t, errors.Is(err, ErrNotSupported), "want ErrNotSupported, got %v", err)
	}
}

func TestGenerateWalletChallenge_AcceptsSupportedSigningTypes(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)

	for _, st := range []string{"siwe", "personal_sign", "eip712", "", "SIWE"} {
		challenge, err := svc.GenerateWalletChallenge(context.Background(), signTypeTestWallet, 1, st)
		require.NoError(t, err, "signing type %q must be accepted", st)
		assert.NotEmpty(t, challenge.Message)
		assert.NotEmpty(t, challenge.Nonce)
	}
}

// Every access token must carry the issuer and audience the middleware
// requires, otherwise a correctly-configured deployment would reject its own
// tokens.
func TestAccessToken_CarriesIssuerAudienceAndSubject(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)

	token, err := svc.generateWalletToken(signTypeTestWallet)
	require.NoError(t, err)

	claims, err := svc.ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, AccessTokenIssuer, claims.Issuer)
	assert.Contains(t, []string(claims.Audience), AccessTokenAudience)
	assert.Equal(t, signTypeTestWallet, claims.Subject)
	assert.Equal(t, signTypeTestWallet, claims.WalletAddress)
}

// The nonce is the anti-replay value: it must be at least 128 bits of entropy
// (32 hex characters) and differ on every challenge.
func TestGenerateWalletChallenge_NonceIs128BitAndUnique(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)

	seen := make(map[string]bool)
	for i := 0; i < 16; i++ {
		challenge, err := svc.GenerateWalletChallenge(context.Background(), signTypeTestWallet, 1)
		require.NoError(t, err)
		assert.Len(t, challenge.Nonce, 32, "nonce must be 128 bits of hex")
		assert.False(t, seen[challenge.Nonce], "nonce must not repeat")
		seen[challenge.Nonce] = true
	}
}

// The challenge TTL is capped at 300s so a signed message cannot be replayed
// long after it was issued.
func TestGenerateWalletChallenge_TTLWithinBound(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)

	challenge, err := svc.GenerateWalletChallenge(context.Background(), signTypeTestWallet, 1)
	require.NoError(t, err)

	ttl := challenge.ExpiresAt.Sub(challenge.IssuedAt)
	assert.Greater(t, ttl, time.Duration(0))
	assert.LessOrEqual(t, ttl, 300*time.Second, "challenge TTL must not exceed 300s")
}
