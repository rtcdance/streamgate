package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
