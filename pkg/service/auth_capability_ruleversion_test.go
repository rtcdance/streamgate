package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A capability carries the rule version it was issued under, so a rule change
// reaches media already in flight without a per-segment chain query.
func TestGeneratePlaybackToken_CarriesTheRuleVersion(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)
	svc.SetRuleVersionReader(func(context.Context, string) (int64, error) { return 7, nil })

	token, err := svc.GeneratePlaybackToken(t.Context(), capSessionWallet, "content-1", "", "", 1,
		svc.PlaybackTokenTTL(), "")
	require.NoError(t, err)

	claims, err := svc.ParsePlaybackToken(t.Context(), token, "content-1", "")
	require.NoError(t, err)
	assert.EqualValues(t, 7, claims.RuleVersion)
}

// Raising a rule's threshold must refuse a capability that was issued before the
// change — otherwise the change only reaches media when the TTL happens to
// expire, which is exactly what the version exists to avoid.
func TestParsePlaybackToken_RefusesACapabilityFromBeforeARuleChange(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)

	version := int64(3)
	svc.SetRuleVersionReader(func(context.Context, string) (int64, error) { return version, nil })

	token, err := svc.GeneratePlaybackToken(t.Context(), capSessionWallet, "content-1", "", "", 1,
		svc.PlaybackTokenTTL(), "")
	require.NoError(t, err)

	_, err = svc.ParsePlaybackToken(t.Context(), token, "content-1", "")
	require.NoError(t, err, "the capability must be valid while the version is unchanged")

	version = 4
	_, err = svc.ParsePlaybackToken(t.Context(), token, "content-1", "")
	assert.ErrorIs(t, err, ErrTokenRevoked,
		"a capability issued under an older rule version must be refused")
}

// An unreadable authority refuses: a version that cannot be checked is not a
// version that matches.
func TestParsePlaybackToken_RefusesWhenTheRuleVersionIsUnreadable(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)
	svc.SetRuleVersionReader(func(context.Context, string) (int64, error) { return 1, nil })

	token, err := svc.GeneratePlaybackToken(t.Context(), capSessionWallet, "content-1", "", "", 1,
		svc.PlaybackTokenTTL(), "")
	require.NoError(t, err)

	svc.SetRuleVersionReader(func(context.Context, string) (int64, error) {
		return 0, errors.New("authority unavailable")
	})
	_, err = svc.ParsePlaybackToken(t.Context(), token, "content-1", "")
	assert.Error(t, err, "an unreadable rule version must refuse the capability")
}

// Issuance must not mint a capability with a guessed version: if the authority
// cannot be read, there is no version to bind it to.
func TestGeneratePlaybackToken_FailsWhenTheRuleVersionCannotBeRead(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)
	svc.SetRuleVersionReader(func(context.Context, string) (int64, error) {
		return 0, errors.New("authority unavailable")
	})

	_, err := svc.GeneratePlaybackToken(t.Context(), capSessionWallet, "content-1", "", "", 1,
		svc.PlaybackTokenTTL(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule version")
}

// WithRuleVersion lets a caller that already holds the version (the gate) pin it
// at ISSUANCE without a second read. Validation still consults the authority —
// that is the whole point of the check.
func TestGeneratePlaybackToken_ExplicitRuleVersionSkipsTheIssuanceRead(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)
	reads := 0
	svc.SetRuleVersionReader(func(context.Context, string) (int64, error) {
		reads++
		return 9, nil
	})

	token, err := svc.GeneratePlaybackToken(t.Context(), capSessionWallet, "content-1", "", "", 1,
		svc.PlaybackTokenTTL(), "", WithRuleVersion(9))
	require.NoError(t, err)
	assert.Equal(t, 0, reads, "issuance must not read the authority when the version is supplied")

	claims, err := svc.ParsePlaybackToken(t.Context(), token, "content-1", "")
	require.NoError(t, err)
	assert.EqualValues(t, 9, claims.RuleVersion)
	assert.Equal(t, 1, reads, "validation must consult the authority")
}
