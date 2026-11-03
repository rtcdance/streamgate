package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/go-redis/redis/v8"
	"github.com/rtcdance/streamgate/pkg/middleware"
	stg "github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3/signature"
	"github.com/stretchr/testify/require"
)

const contractAuthWallet = "0x1234567890123456789012345678901234567890"

type contractProofStub struct {
	now     func() time.Time
	fail    error
	barrier func()
	calls   atomic.Int64
}

func (v *contractProofStub) VerifySignature(context.Context, string, string, string) (bool, error) {
	return false, errors.New("EOA fallback forbidden")
}
func (v *contractProofStub) VerifyWalletProof(_ context.Context, chain int64, _, address, message, encoded string) (*signature.AccountProof, error) {
	v.calls.Add(1)
	if v.barrier != nil {
		v.barrier()
	}
	if v.fail != nil {
		return nil, v.fail
	}
	sig, err := signature.DecodeWalletSignature(encoded)
	if err != nil {
		return nil, err
	}
	return &signature.AccountProof{Address: common.HexToAddress(address), ChainID: chain, Kind: signature.AccountContract,
		Digest:   crypto.Keccak256Hash([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message))),
		CodeHash: common.HexToHash("0x01"), BlockNumber: 17, BlockHash: common.HexToHash("0x02"), ObservedAt: v.now(), Signature: sig}, nil
}

func contractAuthFixture(t *testing.T) (*AuthService, *contractProofStub, *atomic.Int64, *stg.RedisWalletSessionStore) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	epochs, err := stg.NewRedisSessionStore(client)
	require.NoError(t, err)
	proofs, err := stg.NewRedisWalletSessionStore(client)
	require.NoError(t, err)
	clock := &atomic.Int64{}
	clock.Store(time.Now().UTC().UnixNano())
	now := func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	v := &contractProofStub{now: now}
	s := NewAuthService("contract-wallet-test-secret-at-least-32", nil, WithSessionStore(epochs), WithWalletSessionStore(proofs), WithSignatureVerifier(v))
	s.clockNow = now
	return s, v, clock, proofs
}

func contractLogin(t *testing.T, s *AuthService) (string, *Claims) {
	t.Helper()
	c, err := s.GenerateWalletChallenge(t.Context(), contractAuthWallet, 31337)
	require.NoError(t, err)
	token, err := s.AuthenticateWithWallet(t.Context(), contractAuthWallet, c.ID, "0x0102", 31337)
	require.NoError(t, err)
	claims, err := s.ParseToken(token)
	require.NoError(t, err)
	return token, claims
}

func TestContractWallet_LoginRefreshAndMetadata(t *testing.T) {
	s, _, clock, proofs := contractAuthFixture(t)
	token, claims := contractLogin(t, s)
	require.Equal(t, contractAuthWallet, claims.Subject)
	require.Equal(t, signature.AccountContract, claims.AccountKind)
	require.Equal(t, int64(31337), claims.ChainID)
	require.NotEmpty(t, claims.WalletProofID)
	require.LessOrEqual(t, claims.ExpiresAt.Sub(claims.IssuedAt.Time), time.Minute)
	r, err := proofs.Load(t.Context(), claims.SessionID)
	require.NoError(t, err)
	require.Equal(t, claims.JTI, r.CurrentJTI)
	require.Equal(t, contractAuthWallet, r.Wallet)
	clock.Add(int64(20 * time.Second))
	next, err := s.RefreshToken(t.Context(), token)
	require.NoError(t, err)
	newClaims, err := s.ParseToken(next)
	require.NoError(t, err)
	require.Equal(t, claims.WalletProofID, newClaims.WalletProofID)
	require.Equal(t, claims.WalletSessionExpiresAt, newClaims.WalletSessionExpiresAt)
	require.Equal(t, claims.ChainID, newClaims.ChainID)
	require.Equal(t, claims.SessionID, newClaims.SessionID)
	values, err := claims.ToMapClaims()
	require.NoError(t, err)
	require.Error(t, s.ValidateWalletAccess(t.Context(), values), "old JTI was consumed atomically")
	values, err = newClaims.ToMapClaims()
	require.NoError(t, err)
	require.NoError(t, s.ValidateWalletAccess(t.Context(), values))
	// Expiry grace grants a fresh proof check, never a plain two-hour JWT.
	clock.Add(int64(70 * time.Second))
	next, err = s.RefreshToken(t.Context(), next)
	require.NoError(t, err)
	newClaims, err = s.ParseToken(next)
	require.NoError(t, err)
	require.LessOrEqual(t, newClaims.ExpiresAt.Sub(newClaims.IssuedAt.Time), time.Minute)
}

func TestContractWallet_ConcurrentRefreshAndLogout(t *testing.T) {
	s, v, clock, _ := contractAuthFixture(t)
	token, claims := contractLogin(t, s)
	clock.Add(int64(10 * time.Second))
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	v.barrier = func() { entered <- struct{}{}; <-release }
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := s.RefreshToken(t.Context(), token); results <- err }()
	}
	<-entered
	<-entered
	close(release)
	winners := 0
	for range 2 {
		if <-results == nil {
			winners++
		}
	}
	require.Equal(t, 1, winners)
	// A verification that was started before logout must not reset the epoch.
	v.barrier = nil
	current, err := s.walletSessions.Load(t.Context(), claims.SessionID)
	require.NoError(t, err)
	currentToken, err := s.signToken(contractClaims(current, s.now()))
	require.NoError(t, err)
	entered2 := make(chan struct{}, 1)
	release2 := make(chan struct{})
	v.barrier = func() { entered2 <- struct{}{}; <-release2 }
	go func() { _, err := s.RefreshToken(t.Context(), currentToken); results <- err }()
	<-entered2
	require.Greater(t, s.RevokeSession(claims.SessionID), int64(1))
	close(release2)
	require.Error(t, <-results)
	require.False(t, s.SessionValid(claims.SessionID, 1))
}

func TestContractWallet_PolicyChangeAndDependencyAreDistinct(t *testing.T) {
	s, v, clock, _ := contractAuthFixture(t)
	token, claims := contractLogin(t, s)
	clock.Add(int64(10 * time.Second))
	v.fail = signature.ErrWalletDependency
	_, err := s.RefreshToken(t.Context(), token)
	require.ErrorIs(t, err, signature.ErrWalletDependency)
	require.True(t, s.SessionValid(claims.SessionID, 1), "an outage must not masquerade as policy revocation")
	v.fail = signature.ErrWalletSignature
	_, err = s.RefreshToken(t.Context(), token)
	require.ErrorIs(t, err, signature.ErrWalletSignature)
	require.False(t, s.SessionValid(claims.SessionID, 1))
}

func TestContractWallet_MissingMetadataCannotBecomeEOA(t *testing.T) {
	s, _, _, _ := contractAuthFixture(t)
	_, claims := contractLogin(t, s)
	claims.AccountKind = ""
	claims.WalletProofID = ""
	claims.ChainID = 0
	claims.WalletSessionExpiresAt = 0
	token, err := s.signToken(claims)
	require.NoError(t, err)
	_, err = s.RefreshToken(t.Context(), token)
	require.Error(t, err)
	values, err := claims.ToMapClaims()
	require.NoError(t, err)
	_, err = middleware.AuthorizeAccessClaims(t.Context(), middleware.AccessPolicy{SessionValidator: s}, values)
	require.ErrorIs(t, err, middleware.ErrAccessWalletInvalid)
}

func TestContractWallet_AbsoluteDeadlineDoesNotSlide(t *testing.T) {
	s, v, clock, _ := contractAuthFixture(t)
	token, claims := contractLogin(t, s)
	for range 29 {
		clock.Add(int64(4 * time.Minute))
		var err error
		token, err = s.RefreshToken(t.Context(), token)
		require.NoError(t, err)
	}
	clock.Store(time.Unix(claims.WalletSessionExpiresAt, 0).Add(-time.Second).UnixNano())
	token, err := s.RefreshToken(t.Context(), token)
	require.NoError(t, err)
	last, err := s.ParseToken(token)
	require.NoError(t, err)
	require.Equal(t, claims.WalletSessionExpiresAt, last.ExpiresAt.Unix())
	before := v.calls.Load()
	clock.Add(int64(2 * time.Minute))
	_, err = s.RefreshToken(t.Context(), token)
	require.Error(t, err)
	require.Equal(t, before, v.calls.Load(), "absolute expiry must stop before RPC")
}

func TestContractWallet_CapabilityClipsPresentedCredential(t *testing.T) {
	s, _, clock, _ := contractAuthFixture(t)
	token, claims := contractLogin(t, s)
	values, err := claims.ToMapClaims()
	require.NoError(t, err)
	binding, err := SessionBindingFromVerifiedClaims(claims.SessionID, 1, claims.RestoreEpoch, values)
	require.NoError(t, err)
	authz := &PlaybackAuthorization{seal: authorizationSeal, wallet: claims.WalletAddress, contentID: "movie", session: binding, rule: middleware.GatingRule{ChainID: 31337}}
	capToken, err := s.IssueCapability(t.Context(), authz, time.Minute, "")
	require.NoError(t, err)
	capClaims, err := s.ParsePlaybackToken(t.Context(), capToken, "movie", "")
	require.NoError(t, err)
	require.Equal(t, claims.ExpiresAt.Time, capClaims.ExpiresAt.Time)
	clock.Add(int64(20 * time.Second))
	_, err = s.RefreshToken(t.Context(), token)
	require.NoError(t, err)
	_, err = s.IssueCapability(t.Context(), authz, time.Minute, "")
	require.Error(t, err, "old access JTI cannot borrow the renewed lease")
	values, err = capClaims.ToMapClaims()
	require.NoError(t, err)
	authz.session, err = SessionBindingFromVerifiedClaims(claims.SessionID, 1, claims.RestoreEpoch, values)
	require.NoError(t, err)
	reissued, err := s.IssueCapability(t.Context(), authz, time.Minute, "")
	require.NoError(t, err)
	current, err := s.ParsePlaybackToken(t.Context(), reissued, "movie", "")
	require.NoError(t, err)
	require.Equal(t, capClaims.ExpiresAt.Time, current.ExpiresAt.Time, "a capability cannot roll its original access deadline forward")
	authz.rule.ChainID = 1
	_, err = s.IssueCapability(t.Context(), authz, time.Minute, "")
	require.ErrorIs(t, err, ErrPlaybackDenied)
}

func TestContractWallet_ConfiguredLeaseLifetimeAndFreshness(t *testing.T) {
	s, v, clock, _ := contractAuthFixture(t)
	p := defaultContractWalletPolicy()
	p.LeaseTTL = 10 * time.Second
	p.AbsoluteSessionTTL = 30 * time.Minute
	p.ClockSkew = time.Second
	p.ProofMaxAge = time.Second
	p.MaxBodyBytes = 4096
	WithContractWalletPolicy(p)(s)
	_, claims := contractLogin(t, s)
	require.Equal(t, 10*time.Second, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
	require.LessOrEqual(t, time.Unix(claims.WalletSessionExpiresAt, 0).Sub(s.now()), 30*time.Minute)
	require.Equal(t, int64(4096), s.WalletAuthBodyLimit())
	v.now = func() time.Time { return time.Unix(0, clock.Load()).Add(-2 * time.Second).UTC() }
	c, err := s.GenerateWalletChallenge(t.Context(), contractAuthWallet, 31337)
	require.NoError(t, err)
	_, err = s.AuthenticateWithWallet(t.Context(), contractAuthWallet, c.ID, "0x0102", 31337)
	require.ErrorIs(t, err, signature.ErrWalletDependency)
	v.now = s.now
	token, err := s.signToken(claims)
	require.NoError(t, err)
	clock.Store(claims.ExpiresAt.Add(2 * time.Second).UnixNano())
	_, err = s.ParseToken(token)
	require.ErrorIs(t, err, ErrTokenExpired)
}
