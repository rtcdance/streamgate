package service

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// quantityVerifier answers the REAL quantity the authorizer compares, and
// refuses to answer without a resolved block (mirrors the gate's contract).
type quantityVerifier struct {
	quantity *big.Int
	err      error
}

func (v *quantityVerifier) VerifyNFTOwnership(context.Context, int64, string, string, string) (bool, error) {
	return v.quantity != nil && v.quantity.Sign() > 0, nil
}
func (v *quantityVerifier) GetNFTBalance(context.Context, int64, string, string) (*big.Int, error) {
	return v.quantity, nil
}
func (v *quantityVerifier) VerifyNFTOwnershipAutoDetect(context.Context, int64, string, string, string) (bool, error) {
	return v.quantity != nil && v.quantity.Sign() > 0, nil
}
func (v *quantityVerifier) VerifyNFTCollectionAutoDetect(context.Context, int64, string, string) (bool, error) {
	return v.quantity != nil && v.quantity.Sign() > 0, nil
}
func (v *quantityVerifier) GetNFTInfo(context.Context, int64, string, string) (*middleware.NFTMetadata, error) {
	return &middleware.NFTMetadata{}, nil
}
func (v *quantityVerifier) GetNFTOwnershipQuantity(_ context.Context, _ int64, _, _, _ string, block *big.Int) (*big.Int, error) {
	if block == nil {
		return nil, errors.New("block evidence is required")
	}
	if v.err != nil {
		return nil, v.err
	}
	return new(big.Int).Set(v.quantity), nil
}

// stableBlockProver resolves one immutable block, so the gate's unchanged-block
// check passes.
type stableBlockProver struct{}

func (stableBlockProver) HeaderByNumber(context.Context, int64, *big.Int) (*middleware.BlockHeaderInfo, error) {
	return &middleware.BlockHeaderInfo{Number: 100, Hash: "0xblock"}, nil
}
func (stableBlockProver) HeaderByTag(context.Context, int64, string) (*middleware.BlockHeaderInfo, error) {
	return &middleware.BlockHeaderInfo{Number: 100, Hash: "0xblock"}, nil
}

// countingSnapshotResolver returns a scripted sequence of snapshots, clamping
// to the last one once the script is exhausted, and counts resolutions.
type countingSnapshotResolver struct {
	script []PolicySnapshot
	calls  int
}

func (r *countingSnapshotResolver) ResolvePolicySnapshot(_ context.Context, contentID string) (PolicySnapshot, error) {
	r.calls++
	idx := r.calls - 1
	if idx >= len(r.script) {
		idx = len(r.script) - 1
	}
	snap := r.script[idx]
	snap.ContentID = contentID
	return snap, nil
}

func authzRule(minBalance int) middleware.GatingRule {
	return middleware.GatingRule{
		ContractAddress: "0x1111111111111111111111111111111111111111",
		ChainID:         1,
		Standard:        "ERC721",
		MinBalance:      minBalance,
	}
}

func newPlaybackTestAuthorizer(t *testing.T, verifier middleware.NFTOwnershipChecker, resolver PolicySnapshotResolver) *PlaybackAuthorizer {
	t.Helper()
	gate := &middleware.NFTGateConfig{
		Verifier:       verifier,
		BlockProver:    stableBlockProver{},
		DefaultChainID: 1,
	}
	return NewPlaybackAuthorizer(gate, resolver, zap.NewNop())
}

func decodeUnverifiedClaims(t *testing.T, token string) jwt.MapClaims {
	t.Helper()
	claims := jwt.MapClaims{}
	_, _, err := new(jwt.Parser).ParseUnverified(token, claims)
	require.NoError(t, err)
	return claims
}

// newSessionID starts a live session so a capability minted in a test inherits
// one, the way every production caller must.
func newSessionID(t *testing.T, svc *AuthService) string {
	t.Helper()
	id, err := svc.StartSession()
	require.NoError(t, err)
	return id
}

// A capability must inherit the caller's login session; an authorization with
// no session can never issue one.
func TestAuthorizePlayback_RefusesAnEmptySession(t *testing.T) {
	resolver := &countingSnapshotResolver{script: []PolicySnapshot{
		{ContentID: "c1", Rules: []middleware.GatingRule{authzRule(1)}, Version: 1, VerifiedAt: time.Now()},
	}}
	h := newPlaybackTestAuthorizer(t, &quantityVerifier{quantity: big.NewInt(1)}, resolver)

	_, err := h.AuthorizePlayback(t.Context(), "c1", capSessionWallet, SessionBinding{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session")
}

// A capability cannot be minted from an authorization assembled outside the
// service: the zero value carries no seal, so issuance refuses it.
func TestIssueCapability_RefusesAnUnsealedAuthorization(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)

	_, err := svc.IssueCapability(t.Context(), &PlaybackAuthorization{}, time.Minute, "")
	require.Error(t, err)

	_, err = svc.IssueCapability(t.Context(), nil, time.Minute, "")
	require.Error(t, err)
}

// The deterministic barrier: the rule version moves between the decision and
// issuance, so no usable capability carrying the new version may be produced.
func TestAuthorizePlayback_RefusesWhenTheRuleTightensUnderIssuance(t *testing.T) {
	resolver := &countingSnapshotResolver{script: []PolicySnapshot{
		{ContentID: "c1", Rules: []middleware.GatingRule{authzRule(1)}, Version: 1, VerifiedAt: time.Now()},
		{ContentID: "c1", Rules: []middleware.GatingRule{authzRule(2)}, Version: 2, VerifiedAt: time.Now()},
	}}
	// Holds 1: allowed under v1 (min 1), denied under v2 (min 2).
	h := newPlaybackTestAuthorizer(t, &quantityVerifier{quantity: big.NewInt(1)}, resolver)

	authz, err := h.AuthorizePlayback(t.Context(), "c1", capSessionWallet, SessionBinding{SessionID: "s1", AuthEpoch: 1})
	require.Error(t, err, "a rule tightened before issuance must refuse")
	assert.Nil(t, authz)
}

// A second change after the single re-authorization is a refusal, not an
// unbounded retry.
func TestAuthorizePlayback_RefusesOnASecondRuleAdvance(t *testing.T) {
	resolver := &countingSnapshotResolver{script: []PolicySnapshot{
		{ContentID: "c1", Rules: []middleware.GatingRule{authzRule(1)}, Version: 1, VerifiedAt: time.Now()},
		{ContentID: "c1", Rules: []middleware.GatingRule{authzRule(1)}, Version: 2, VerifiedAt: time.Now()},
		{ContentID: "c1", Rules: []middleware.GatingRule{authzRule(1)}, Version: 3, VerifiedAt: time.Now()},
	}}
	h := newPlaybackTestAuthorizer(t, &quantityVerifier{quantity: big.NewInt(5)}, resolver)

	authz, err := h.AuthorizePlayback(t.Context(), "c1", capSessionWallet, SessionBinding{SessionID: "s1", AuthEpoch: 1})
	require.Error(t, err, "a second rule advance must refuse rather than retry")
	assert.Nil(t, authz)
}

// Issuance consumes the authorization's frozen rule version; it never re-reads
// a newer version and re-labels an older decision.
func TestIssueCapability_UsesTheAuthorizationsRuleVersion(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)
	// If issuance consulted the authority at all it would see 99.
	svc.SetRuleVersionReader(func(context.Context, string) (int64, error) { return 99, nil })

	sessionID, err := svc.StartSession()
	require.NoError(t, err)

	resolver := &countingSnapshotResolver{script: []PolicySnapshot{
		{ContentID: "c1", Rules: []middleware.GatingRule{authzRule(1)}, Version: 7, VerifiedAt: time.Now()},
	}}
	h := newPlaybackTestAuthorizer(t, &quantityVerifier{quantity: big.NewInt(1)}, resolver)

	authz, err := h.AuthorizePlayback(t.Context(), "c1", capSessionWallet, SessionBinding{SessionID: sessionID, AuthEpoch: 1})
	require.NoError(t, err)
	assert.EqualValues(t, 7, authz.RuleVersion())

	before := resolver.calls
	token, err := svc.IssueCapability(t.Context(), authz, svc.PlaybackTokenTTL(), "")
	require.NoError(t, err)
	assert.Equal(t, before, resolver.calls, "issuance must not re-read the resolver")

	claims := decodeUnverifiedClaims(t, token)
	assert.EqualValues(t, 7, claims["rule_version"], "the capability carries the authorization's version")
	assert.Equal(t, sessionID, claims["session_id"], "the capability inherits the caller's session")
	assert.Equal(t, "playback", claims["token_type"])
}

// noRulesAuthorizer builds an authorizer whose snapshot has no active rules, so
// the configured gating mode alone decides.
func noRulesAuthorizer(t *testing.T, mode string, version int64) *PlaybackAuthorizer {
	t.Helper()
	resolver := &countingSnapshotResolver{script: []PolicySnapshot{
		{ContentID: "c1", Version: version, VerifiedAt: time.Now()},
	}}
	gate := &middleware.NFTGateConfig{
		Verifier:       &quantityVerifier{quantity: big.NewInt(0)},
		BlockProver:    stableBlockProver{},
		DefaultChainID: 1,
		GatingMode:     mode,
	}
	return NewPlaybackAuthorizer(gate, resolver, zap.NewNop())
}

// Content with no active rules is denied unless the operator explicitly opted
// into "open"; without that, the gate fails closed.
func TestAuthorizePlayback_NoRulesFailsClosed(t *testing.T) {
	h := noRulesAuthorizer(t, "closed", 3)

	authz, err := h.AuthorizePlayback(t.Context(), "c1", capSessionWallet, SessionBinding{SessionID: "s1", AuthEpoch: 1})
	require.ErrorIs(t, err, ErrNoGatingRules)
	assert.Nil(t, authz)
}

// With gating_mode "open" the HTTP gate authorizes an unlisted content, so the
// issuance path must not turn the same request into a denial — otherwise the two
// entry points disagree on one policy. The capability still carries the rule
// version, so writing a rule later invalidates media already in flight.
func TestAuthorizePlayback_OpenModeIssuesWithoutARule(t *testing.T) {
	h := noRulesAuthorizer(t, "open", 3)

	authz, err := h.AuthorizePlayback(t.Context(), "c1", capSessionWallet, SessionBinding{SessionID: "s1", AuthEpoch: 1})
	require.NoError(t, err)
	require.NotNil(t, authz)
	assert.EqualValues(t, 3, authz.RuleVersion())

	svc := NewAuthService(signTypeTestSecret, nil)
	token, err := svc.IssueCapability(t.Context(), authz, svc.PlaybackTokenTTL(), "")
	require.NoError(t, err)

	claims := decodeUnverifiedClaims(t, token)
	assert.EqualValues(t, 3, claims["rule_version"])
	assert.Equal(t, "c1", claims["content_id"])
}
