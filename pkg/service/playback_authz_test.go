package service

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
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

// Evidence that aged past the freshness bound between the gate decision and
// issuance is a retryable service condition. Classifying it as a denial would
// answer 403 ("you hold no NFT") for a holder whose read was simply 5s old,
// which is exactly what a loaded server must never do.
func TestAuthorizePlayback_StaleEvidenceIsUnavailableNotDenied(t *testing.T) {
	resolver := &countingSnapshotResolver{script: []PolicySnapshot{
		{ContentID: "c1", Rules: []middleware.GatingRule{authzRule(1)}, Version: 1, VerifiedAt: time.Now()},
	}}
	gate := &middleware.NFTGateConfig{
		Verifier:       &quantityVerifier{quantity: big.NewInt(1)},
		BlockProver:    stableBlockProver{},
		DefaultChainID: 1,
		MaxEvidenceAge: time.Nanosecond,
	}
	h := NewPlaybackAuthorizer(gate, resolver, zap.NewNop())

	authz, err := h.AuthorizePlayback(t.Context(), "c1", capSessionWallet, SessionBinding{SessionID: "s1", AuthEpoch: 1})
	require.Error(t, err, "stale evidence must never mint a capability")
	assert.Nil(t, authz)
	assert.True(t, errors.Is(err, ErrAuthorizationUnavailable), "stale evidence is unavailable, not denied: %v", err)
	assert.False(t, errors.Is(err, ErrPlaybackDenied))
}

type playbackSnapshotResolverFunc func(context.Context, string) (PolicySnapshot, error)

func (f playbackSnapshotResolverFunc) ResolvePolicySnapshot(ctx context.Context, contentID string) (PolicySnapshot, error) {
	return f(ctx, contentID)
}

type playbackFreshnessCache struct {
	mu    sync.Mutex
	entry middleware.NFTAccessEntry
}

func (c *playbackFreshnessCache) Get(context.Context, string) (middleware.NFTAccessEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entry
	if entry.Balance == nil {
		return entry, false
	}
	entry.Balance = new(big.Int).Set(entry.Balance)
	return entry, true
}

func (c *playbackFreshnessCache) Set(_ context.Context, _ string, entry middleware.NFTAccessEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry.Balance = new(big.Int).Set(entry.Balance)
	c.entry = entry
}

func (c *playbackFreshnessCache) Delete(context.Context, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entry = middleware.NFTAccessEntry{}
}

func (c *playbackFreshnessCache) DeleteByPrefix(ctx context.Context, prefix string) {
	c.Delete(ctx, prefix)
}

type playbackFreshnessVerifier struct {
	quantityVerifier
	calls atomic.Int32
}

func (v *playbackFreshnessVerifier) GetNFTOwnershipQuantity(ctx context.Context, chainID int64, contract, tokenID, wallet string, block *big.Int) (*big.Int, error) {
	v.calls.Add(1)
	if block == nil || block.Cmp(big.NewInt(100)) != 0 {
		return nil, errors.New("ownership refresh must name the resolved safe block")
	}
	return v.quantityVerifier.GetNFTOwnershipQuantity(ctx, chainID, contract, tokenID, wallet, block)
}

// The second policy read is a barrier AFTER the cached holding has granted.
// Aging that original evidence, without expiring its cache TTL, must cause one
// real pinned refresh; neither a transfer nor another slow read permits a grant.
func TestAuthorizePlayback_RefreshesEvidenceExpiredDuringSecondSnapshot(t *testing.T) {
	tests := []struct {
		name          string
		quantity      int64
		thirdVersion  int64
		delayThird    bool
		wantErr       error
		wantMessage   string
		wantSnapshots int32
	}{
		{name: "holding_retained", quantity: 1, thirdVersion: 7, wantSnapshots: 3},
		{name: "holding_transferred", quantity: 0, thirdVersion: 7, wantErr: ErrPlaybackDenied, wantSnapshots: 2},
		{name: "rule_changes_during_refresh", quantity: 1, thirdVersion: 8, wantMessage: "rule version changed again", wantSnapshots: 3},
		{name: "refreshed_evidence_expires_at_third_snapshot", quantity: 1, thirdVersion: 7, delayThird: true, wantErr: ErrAuthorizationUnavailable, wantSnapshots: 3},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wallet := fmt.Sprintf("0x%040x", 0xf000+i)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cache := &playbackFreshnessCache{entry: middleware.NFTAccessEntry{
				HasNFT: true, Balance: big.NewInt(1), BlockNumber: 100, BlockHash: "0xblock",
				ObservedAt: time.Now().Add(-2 * time.Second), Expires: time.Now().Add(time.Minute),
			}}
			original, _ := cache.Get(ctx, "")
			verifier := &playbackFreshnessVerifier{quantityVerifier: quantityVerifier{quantity: big.NewInt(tt.quantity)}}
			gate := &middleware.NFTGateConfig{
				Verifier: verifier, BlockProver: stableBlockProver{}, Cache: cache,
				BlockVerifyCache: middleware.NewBlockHashCache(), DefaultChainID: 1,
			}
			bound := middleware.EvidenceAgeBound(gate)
			require.Equal(t, 5*time.Second, bound)
			waitPastEvidence := func(observedAt time.Time) error {
				timer := time.NewTimer(time.Until(observedAt.Add(bound).Add(time.Millisecond)))
				defer timer.Stop()
				select {
				case <-timer.C:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			var snapshots atomic.Int32
			var refreshStartedAt time.Time
			resolver := playbackSnapshotResolverFunc(func(_ context.Context, contentID string) (PolicySnapshot, error) {
				call := snapshots.Add(1)
				version := int64(7)
				if call == 2 {
					// Reaching this barrier proves EvaluatePolicy already allowed
					// the cached holding without consulting the current holdings.
					assert.Zero(t, verifier.calls.Load())
					if err := waitPastEvidence(original.ObservedAt); err != nil {
						return PolicySnapshot{}, err
					}
					assert.Greater(t, time.Since(original.ObservedAt), bound)
					assert.True(t, original.Expires.After(time.Now()), "cache TTL must still be live")
					refreshStartedAt = time.Now()
				}
				if call == 3 {
					version = tt.thirdVersion
					if tt.delayThird {
						fresh, ok := cache.Get(ctx, "")
						if !ok {
							return PolicySnapshot{}, errors.New("missing refreshed evidence")
						}
						if err := waitPastEvidence(fresh.ObservedAt); err != nil {
							return PolicySnapshot{}, err
						}
					}
				}
				return PolicySnapshot{ContentID: contentID, Rules: []middleware.GatingRule{authzRule(1)}, Version: version, VerifiedAt: time.Now()}, nil
			})
			h := NewPlaybackAuthorizer(gate, resolver, zap.NewNop())

			authz, err := h.AuthorizePlayback(ctx, "c1", wallet, SessionBinding{SessionID: "s1", AuthEpoch: 1})
			assert.Equal(t, int32(1), verifier.calls.Load(), "stale cache must force exactly one real ownership refresh")
			assert.Equal(t, tt.wantSnapshots, snapshots.Load(), "reauthorization must be bounded and checked against a third snapshot")
			if tt.wantErr != nil || tt.wantMessage != "" {
				require.Error(t, err)
				assert.Nil(t, authz)
				if tt.wantErr != nil {
					assert.ErrorIs(t, err, tt.wantErr)
				} else {
					assert.Contains(t, err.Error(), tt.wantMessage)
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, authz)
			fresh, ok := cache.Get(ctx, "")
			require.True(t, ok)
			assert.Equal(t, fresh.ObservedAt, authz.EvidenceObservedAt(), "grant must retain the fresh query's original observation time")
			assert.False(t, authz.EvidenceObservedAt().Before(refreshStartedAt))
			assert.NotEqual(t, original.ObservedAt, authz.EvidenceObservedAt())
			assert.EqualValues(t, 7, authz.RuleVersion(), "a same-version refresh must not invent a policy version")
			number, hash := authz.BlockEvidence()
			assert.EqualValues(t, 100, number)
			assert.Equal(t, "0xblock", hash)
		})
	}
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

	authz, err := h.AuthorizePlayback(t.Context(), "c1", capSessionWallet, SessionBinding{SessionID: sessionID, AuthEpoch: 1, RestoreEpoch: currentRestoreEpoch(t, svc)})
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
	svc := NewAuthService(signTypeTestSecret, nil)
	sessionID := newSessionID(t, svc)

	authz, err := h.AuthorizePlayback(t.Context(), "c1", capSessionWallet, SessionBinding{SessionID: sessionID, AuthEpoch: 1, RestoreEpoch: currentRestoreEpoch(t, svc)})
	require.NoError(t, err)
	require.NotNil(t, authz)
	assert.EqualValues(t, 3, authz.RuleVersion())

	token, err := svc.IssueCapability(t.Context(), authz, svc.PlaybackTokenTTL(), "")
	require.NoError(t, err)

	claims := decodeUnverifiedClaims(t, token)
	assert.EqualValues(t, 3, claims["rule_version"])
	assert.Equal(t, "c1", claims["content_id"])
}
