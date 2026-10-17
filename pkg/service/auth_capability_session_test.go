package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const capSessionWallet = "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18"

// A capability must inherit the caller's session; issuance must never silently
// create a separate session, because that would leave media in flight beyond
// the caller's login (a logout could no longer reach it).
func TestGeneratePlaybackToken_RefusesWithoutACallerSuppliedSession(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)

	_, err := svc.GeneratePlaybackToken(t.Context(), capSessionWallet, "content-1", "", "", 1, svc.PlaybackTokenTTL(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session")
}

// WithSessionBinding is what lets a logout reach media already in flight: the
// capability inherits the requesting session rather than getting its own.
func TestGeneratePlaybackToken_InheritsTheCallersSession(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)
	sessionID, err := svc.StartSession()
	require.NoError(t, err)

	token, err := svc.GeneratePlaybackToken(t.Context(), capSessionWallet, "content-1", "", "", 1,
		svc.PlaybackTokenTTL(), "", WithSessionBinding(sessionID, 1))
	require.NoError(t, err)

	claims, err := svc.ParsePlaybackToken(t.Context(), token, "content-1", "")
	require.NoError(t, err)
	assert.Equal(t, sessionID, claims.SessionID)

	// The whole point: revoking that session kills the capability now, not
	// whenever its TTL happens to run out.
	svc.RevokeSession(sessionID)
	_, err = svc.ParsePlaybackToken(t.Context(), token, "content-1", "")
	assert.ErrorIs(t, err, ErrTokenRevoked,
		"a capability of a revoked session must be refused")
}

// A capability whose session is unknown is refused rather than treated as
// session-less and allowed.
func TestParsePlaybackToken_RefusesAnUnknownSession(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)

	token, err := svc.GeneratePlaybackToken(t.Context(), capSessionWallet, "content-1", "", "", 1,
		svc.PlaybackTokenTTL(), "", WithSessionBinding("session-that-never-existed", 1))
	require.NoError(t, err)

	_, err = svc.ParsePlaybackToken(t.Context(), token, "content-1", "")
	assert.ErrorIs(t, err, ErrTokenRevoked)
}

// A capability minted before the session scheme (no session_id) must be refused,
// not grandfathered in — otherwise revocation is bypassed by presenting an older
// credential.
func TestParsePlaybackToken_RefusesACapabilityWithoutASession(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)

	legacy := Claims{
		Username:      capSessionWallet,
		TokenType:     "playback",
		WalletAddress: capSessionWallet,
		ContentID:     "content-1",
	}
	legacy.RegisteredClaims = accessTokenClaims("content-1", time.Minute)
	legacy.Audience = []string{PlaybackCapabilityAudience}
	legacy.Subject = "content-1"

	token, err := svc.signToken(&legacy)
	require.NoError(t, err)

	_, err = svc.ParsePlaybackToken(t.Context(), token, "content-1", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not bound to a session")
}
