package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The capability lifetime is a policy value, not a constant buried in a
// handler: a playback capability is a bearer credential carried in a URL, so
// the window in which a revoked or transferred holding still plays is bounded
// by this number.
func TestAuthService_PlaybackTokenTTL(t *testing.T) {
	t.Run("defaults to the policy value", func(t *testing.T) {
		svc := NewAuthService(signTypeTestSecret, nil)
		assert.Equal(t, DefaultPlaybackTokenTTL, svc.PlaybackTokenTTL())
		assert.Equal(t, 60*time.Second, svc.PlaybackTokenTTL())
	})

	t.Run("configured value is used", func(t *testing.T) {
		svc := NewAuthService(signTypeTestSecret, nil, WithPlaybackTokenTTL(30*time.Second))
		assert.Equal(t, 30*time.Second, svc.PlaybackTokenTTL())
	})

	t.Run("above the ceiling is clamped", func(t *testing.T) {
		svc := NewAuthService(signTypeTestSecret, nil, WithPlaybackTokenTTL(30*time.Minute))
		assert.Equal(t, MaxPlaybackTokenTTL, svc.PlaybackTokenTTL(),
			"a caller bypassing config validation must not widen the window")
	})

	t.Run("non-positive is ignored", func(t *testing.T) {
		svc := NewAuthService(signTypeTestSecret, nil, WithPlaybackTokenTTL(0))
		assert.Equal(t, DefaultPlaybackTokenTTL, svc.PlaybackTokenTTL())
	})
}

// The issued token must actually expire on that bound, not merely report it.
func TestGeneratePlaybackToken_ExpiresOnThePolicyBound(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil, WithPlaybackTokenTTL(2*time.Second))

	token, err := svc.GeneratePlaybackToken(t.Context(), signTypeTestWallet, "content-1", "", "", 1, svc.PlaybackTokenTTL(), "")
	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	_, err = svc.ParsePlaybackToken(t.Context(), token, "content-1", "")
	assert.NoError(t, err, "the token must be valid immediately after issue")
}

// Once the TTL passes, the same capability must be refused. ParsePlaybackToken
// has no expiry check of its own — it relies entirely on parseTokenCapability —
// so without this case, disabling that check would go unnoticed (the SG-15
// reverse proof demonstrated exactly that).
func TestParsePlaybackToken_RefusesAnExpiredCapability(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil, WithPlaybackTokenTTL(30*time.Second))

	token, err := svc.GeneratePlaybackToken(t.Context(), signTypeTestWallet, "content-1", "", "", 1,
		svc.PlaybackTokenTTL(), "")
	require.NoError(t, err)

	_, err = svc.ParsePlaybackToken(t.Context(), token, "content-1", "")
	require.NoError(t, err, "the capability must be valid immediately after issue")

	// Move the clock past the TTL (plus the clock-skew tolerance).
	svc.clockNow = func() time.Time {
		return time.Now().Add(svc.PlaybackTokenTTL() + svc.ClockSkew() + time.Second)
	}
	_, err = svc.ParsePlaybackToken(t.Context(), token, "content-1", "")
	assert.ErrorIs(t, err, ErrTokenExpired, "an expired capability must not be accepted")
}

// A capability and an access token are signed with the same key, so the
// audience is what stops one being spent where the other is expected.
func TestGeneratePlaybackToken_StampsItsOwnAudience(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)

	token, err := svc.GeneratePlaybackToken(t.Context(), signTypeTestWallet, "content-1", "", "", 1, svc.PlaybackTokenTTL(), "")
	require.NoError(t, err)

	claims, err := svc.ParseToken(token)
	require.NoError(t, err)
	assert.Contains(t, []string(claims.Audience), PlaybackCapabilityAudience)
	assert.NotContains(t, []string(claims.Audience), AccessTokenAudience,
		"a capability must not carry the access audience")
	assert.Equal(t, "playback", claims.TokenType)
}

// An access token must not be spendable as a capability.
func TestParsePlaybackToken_RejectsAnAccessToken(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)

	access, err := svc.generateWalletToken(signTypeTestWallet)
	require.NoError(t, err)

	_, err = svc.ParsePlaybackToken(t.Context(), access, "content-1", "")
	assert.Error(t, err, "an access token must not be usable as a playback capability")
}

// A caller that forgets the TTL gets the policy default, not a second hardcoded
// window: a zero TTL used to fall back to 30 minutes.
func TestGeneratePlaybackToken_ZeroTTLFallsBackToThePolicy(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)

	token, err := svc.GeneratePlaybackToken(t.Context(), signTypeTestWallet, "content-1", "", "", 1, 0, "")
	require.NoError(t, err)

	claims, err := svc.ParseToken(token)
	require.NoError(t, err)
	require.NotNil(t, claims.ExpiresAt)

	remaining := time.Until(claims.ExpiresAt.Time)
	assert.LessOrEqual(t, remaining, DefaultPlaybackTokenTTL+5*time.Second,
		"a zero TTL must fall back to the 60s policy, not 30 minutes")
	assert.Greater(t, remaining, time.Duration(0))
}
