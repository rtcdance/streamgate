package service

import (
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"

	"github.com/golang-jwt/jwt/v4"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tolerance is an input to the revocation bound, so its default must be the
// policy value rather than a larger hardcoded one.
func TestAuthService_ClockSkewDefaultsToThePolicy(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)
	assert.Equal(t, DefaultClockSkew, svc.ClockSkew())
}

// A tolerance above the ceiling is clamped, for the same reason the TTL is: a
// caller that bypasses config must not widen the window the bound was derived
// from.
func TestAuthService_ClockSkewIsClamped(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil, WithClockSkew(30*time.Second))
	assert.Equal(t, MaxClockSkew, svc.ClockSkew(),
		"a tolerance above the policy ceiling must be clamped, not applied")
}

func TestAuthService_ClockSkewIgnoresANegativeValue(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil, WithClockSkew(-1*time.Second))
	assert.Equal(t, DefaultClockSkew, svc.ClockSkew())
}

// The tolerance must actually govern exp, not merely be stored.
func TestParseToken_AppliesTheConfiguredTolerance(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil, WithClockSkew(5*time.Second))

	// 3s past expiry: inside the tolerance, so still usable.
	inside := &Claims{Username: "u", JTI: generateID()}
	inside.RegisteredClaims = accessTokenClaims("u", 0)
	inside.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-3 * time.Second))
	inside.NotBefore = jwt.NewNumericDate(time.Now().Add(-time.Hour))
	tokenInside, err := svc.signToken(inside)
	require.NoError(t, err)
	_, err = svc.ParseToken(tokenInside)
	assert.NoError(t, err, "3s past expiry is within a 5s tolerance")

	// 10s past expiry: outside it, so refused.
	outside := &Claims{Username: "u", JTI: generateID()}
	outside.RegisteredClaims = accessTokenClaims("u", 0)
	outside.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-10 * time.Second))
	outside.NotBefore = jwt.NewNumericDate(time.Now().Add(-time.Hour))
	tokenOutside, err := svc.signToken(outside)
	require.NoError(t, err)
	_, err = svc.ParseToken(tokenOutside)
	assert.ErrorIs(t, err, ErrTokenExpired, "10s past expiry must be refused")
}

// The service's own policy constants must not drift from the config's: the
// config value is wired in at runtime, and a mismatch would mean the service's
// default is looser than the policy it is supposed to mirror.
func TestClockSkewPolicy_MatchesTheConfigPolicy(t *testing.T) {
	assert.Equal(t, config.DefaultClockSkew, DefaultClockSkew)
	assert.Equal(t, config.MaxClockSkew, MaxClockSkew)
}
