package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/golang-jwt/jwt/v4"
	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"
	stg "github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3/signature"
)

type ContractWalletPolicy = config.ContractWalletConfig

func defaultContractWalletPolicy() ContractWalletPolicy { return config.DefaultContractWalletConfig() }

// WithContractWalletPolicy rejects invalid direct-constructor budgets at use;
// the runtime config loader rejects them before starting any server.
func WithContractWalletPolicy(policy ContractWalletPolicy) AuthServiceOption {
	return func(s *AuthService) {
		policy = policy.Effective()
		if err := policy.Validate(); err != nil {
			s.walletPolicyErr = err
			return
		}
		s.walletPolicy = policy
	}
}
func (s *AuthService) contractWalletPolicy() ContractWalletPolicy { return s.walletPolicy.Effective() }
func (s *AuthService) WalletAuthBodyLimit() int64                 { return s.contractWalletPolicy().MaxBodyBytes }
func (s *AuthService) contractClockSkew() time.Duration {
	skew := s.ClockSkew()
	if limit := s.contractWalletPolicy().ClockSkew; limit < skew {
		skew = limit
	}
	return skew
}
func (s *AuthService) claimClockSkew(c *Claims) time.Duration {
	if requiresContractProof(c) {
		return s.contractClockSkew()
	}
	return s.ClockSkew()
}
func (s *AuthService) freshWalletProof(proof *signature.AccountProof) bool {
	return s.walletPolicyErr == nil && proof.Fresh(s.now()) && s.now().Sub(proof.ObservedAt) <= s.contractWalletPolicy().ProofMaxAge
}

type walletProofVerifier interface {
	VerifyWalletProof(context.Context, int64, string, string, string, string) (*signature.AccountProof, error)
}

// WithWalletSessionStore enables durable contract-wallet proof persistence.
// There is deliberately no process-memory fallback for this authority.
func WithWalletSessionStore(store stg.WalletSessionStore) AuthServiceOption {
	return func(s *AuthService) { s.walletSessions = store }
}

func requiresContractProof(claims *Claims) bool {
	return claims.AccountKind == signature.AccountContract || claims.WalletProofID != "" || strings.HasPrefix(claims.SessionID, middleware.ContractWalletSessionPrefix)
}

func walletStorageError(err error) error {
	if errors.Is(err, stg.ErrWalletSessionUnavailable) {
		return signature.ErrWalletDependency
	}
	return ErrInvalidCredential
}

func (s *AuthService) contractRecord(ctx context.Context, claims *Claims, access bool) (*stg.WalletSession, error) {
	if s.walletPolicyErr != nil {
		return nil, signature.ErrWalletDependency
	}
	if s.walletSessions == nil {
		return nil, signature.ErrWalletDependency
	}
	if claims.AccountKind != signature.AccountContract || !strings.HasPrefix(claims.SessionID, middleware.ContractWalletSessionPrefix) || claims.WalletProofID == "" || claims.ChainID <= 0 || claims.WalletSessionExpiresAt <= 0 || claims.ExpiresAt == nil {
		return nil, ErrInvalidCredential
	}
	r, err := s.walletSessions.Load(ctx, claims.SessionID)
	if err != nil {
		return nil, walletStorageError(err)
	}
	if r.ProofID != claims.WalletProofID || r.Wallet != claims.WalletAddress || r.ChainID != claims.ChainID || r.AuthEpoch != claims.AuthEpoch || r.RestoreEpoch != claims.RestoreEpoch || r.Kind != claims.AccountKind || r.AbsoluteExpiresAt.Sub(r.AdmittedAt) > s.contractWalletPolicy().AbsoluteSessionTTL || r.AbsoluteExpiresAt.Unix() != claims.WalletSessionExpiresAt || claims.ExpiresAt.Time.After(r.LeaseExpiresAt) || s.now().After(r.AbsoluteExpiresAt.Add(s.contractClockSkew())) {
		return nil, ErrInvalidCredential
	}
	if access && (r.CurrentJTI != claims.JTI || claims.Subject != r.Wallet) {
		return nil, ErrInvalidCredential
	}
	return r, nil
}

func contractClaims(r *stg.WalletSession, now time.Time) *Claims {
	return &Claims{Username: r.Wallet, TokenType: AccessTokenType, WalletAddress: r.Wallet, ChainID: r.ChainID,
		AccountKind: signature.AccountContract, WalletProofID: r.ProofID, WalletSessionExpiresAt: r.AbsoluteExpiresAt.Unix(),
		JTI: r.CurrentJTI, SessionID: r.SessionID, AuthEpoch: r.AuthEpoch, RestoreEpoch: r.RestoreEpoch,
		RegisteredClaims: jwt.RegisteredClaims{Issuer: AccessTokenIssuer, Subject: r.Wallet, Audience: jwt.ClaimStrings{AccessTokenAudience},
			IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(r.LeaseExpiresAt)}}
}

func (s *AuthService) walletLeaseExpiry(now, absolute time.Time) time.Time {
	expires := now.Add(s.contractWalletPolicy().LeaseTTL)
	if expires.After(absolute) {
		expires = absolute
	}
	return jwt.NewNumericDate(expires).Time
}

func (s *AuthService) requireWalletIssuance(ctx context.Context, r *stg.WalletSession, proof *signature.AccountProof) error {
	if !s.freshWalletProof(proof) || !s.now().Before(r.LeaseExpiresAt) || !s.now().Before(r.AbsoluteExpiresAt) {
		return signature.ErrWalletDependency
	}
	epoch, known := s.sessionStore.Epoch(ctx, r.SessionID)
	if !known || epoch != r.AuthEpoch {
		return ErrInvalidCredential
	}
	return s.requireCurrentRestoreEpoch(r.RestoreEpoch)
}

func (s *AuthService) generateContractWalletToken(ctx context.Context, c *stg.WalletChallenge, proof *signature.AccountProof) (string, error) {
	if s.walletSessions == nil || proof == nil || !s.freshWalletProof(proof) {
		return "", signature.ErrWalletDependency
	}
	if proof.Kind != signature.AccountContract || proof.Address.Hex() != c.WalletAddress || proof.ChainID != c.ChainID {
		return "", ErrInvalidCredential
	}
	// The signed session namespace makes missing wallet metadata a refusal,
	// never a legacy EOA downgrade, without adding proof reads to EOA requests.
	sid := middleware.ContractWalletSessionPrefix + generateID()
	if err := s.sessionStore.Create(ctx, sid); err != nil {
		return "", signature.ErrWalletDependency
	}
	restore, err := s.stampRestoreEpoch()
	if err != nil {
		return "", err
	}
	now := s.now().UTC()
	lifetime := s.jwtExpiry
	if lifetime > s.contractWalletPolicy().AbsoluteSessionTTL {
		lifetime = s.contractWalletPolicy().AbsoluteSessionTTL
	}
	if lifetime <= 0 {
		return "", ErrInvalidCredential
	}
	absolute := jwt.NewNumericDate(now.Add(lifetime)).Time
	r := &stg.WalletSession{Schema: 1, SessionID: sid, ProofID: generateID(), Version: 1,
		Wallet: c.WalletAddress, ChainID: c.ChainID, Kind: signature.AccountContract, AuthEpoch: 1, RestoreEpoch: restore,
		CurrentJTI: generateID(), Scheme: c.SigningType, Message: c.Message, Signature: "0x" + common.Bytes2Hex(proof.Signature),
		Digest: proof.Digest.Hex(), CodeHash: proof.CodeHash.Hex(), BlockNumber: proof.BlockNumber, BlockHash: proof.BlockHash.Hex(), ObservedAt: proof.ObservedAt,
		AdmittedAt: now, AbsoluteExpiresAt: absolute, LeaseExpiresAt: s.walletLeaseExpiry(now, absolute)}
	if err := s.walletSessions.Create(ctx, r); err != nil {
		return "", walletStorageError(err)
	}
	if err := s.requireWalletIssuance(ctx, r, proof); err != nil {
		return "", err
	}
	token, err := s.signToken(contractClaims(r, now))
	if err != nil {
		return "", err
	}
	if err := s.requireWalletIssuance(ctx, r, proof); err != nil {
		return "", err
	}
	return token, nil
}

func (s *AuthService) refreshContractWallet(ctx context.Context, claims *Claims) (string, error) {
	r, err := s.contractRecord(ctx, claims, true)
	if err != nil {
		return "", err
	}
	if !s.now().Before(r.AbsoluteExpiresAt) {
		return "", ErrTokenExpired
	}
	if !s.SessionValid(r.SessionID, r.AuthEpoch) {
		return "", ErrInvalidCredential
	}
	if err := s.requireCurrentRestoreEpoch(r.RestoreEpoch); err != nil {
		return "", err
	}
	v, ok := s.signatureVerifier.(walletProofVerifier)
	if !ok {
		return "", signature.ErrWalletDependency
	}
	// This rechecks admitted server state, not a new login: no new nonce is
	// consumed and the original challenge admission expiry is not extended.
	proof, err := v.VerifyWalletProof(ctx, r.ChainID, r.Scheme, r.Wallet, r.Message, r.Signature)
	if err != nil {
		if errors.Is(err, signature.ErrWalletSignature) || errors.Is(err, signature.ErrWalletUnsupported) {
			if s.RevokeSession(r.SessionID) == SessionEpochUnknown {
				return "", signature.ErrWalletDependency
			}
		}
		return "", err
	}
	if proof == nil || proof.Kind != signature.AccountContract || proof.Address.Hex() != r.Wallet || proof.ChainID != r.ChainID || proof.Digest.Hex() != r.Digest {
		return "", ErrInvalidCredential
	}
	now := s.now().UTC()
	next := *r
	next.Version++
	next.CurrentJTI = generateID()
	next.LeaseExpiresAt = s.walletLeaseExpiry(now, r.AbsoluteExpiresAt)
	next.ObservedAt = proof.ObservedAt
	next.BlockNumber = proof.BlockNumber
	next.BlockHash = proof.BlockHash.Hex()
	next.CodeHash = proof.CodeHash.Hex()
	if err := s.requireWalletIssuance(ctx, &next, proof); err != nil {
		return "", err
	}
	token, err := s.signToken(contractClaims(&next, now))
	if err != nil {
		return "", err
	}
	// CAS consumes the current JTI and version together with the REAL epoch.
	// Exactly one concurrent refresh can win; a late verifier cannot undo logout.
	if err := s.walletSessions.CompareAndSwap(ctx, r, &next); err != nil {
		return "", walletStorageError(err)
	}
	if err := s.requireWalletIssuance(ctx, &next, proof); err != nil {
		return "", err
	}
	return token, nil
}

// ValidateWalletAccess is called by the shared HTTP/gRPC policy after signature
// verification. It performs no chain RPC on ordinary API or media requests.
func (s *AuthService) ValidateWalletAccess(ctx context.Context, values jwt.MapClaims) error {
	var c Claims
	if err := decodeWalletAccessClaims(values, &c); err != nil {
		return middleware.ErrAccessWalletInvalid
	}
	if !requiresContractProof(&c) {
		return nil
	}
	if c.ExpiresAt == nil || s.now().After(c.ExpiresAt.Time.Add(s.contractClockSkew())) || c.IssuedAt == nil || c.NotBefore == nil || (!c.ExpiresAt.Time.After(c.IssuedAt.Time) || c.ExpiresAt.Time.Sub(c.IssuedAt.Time) > s.contractWalletPolicy().LeaseTTL) || s.now().Add(s.contractClockSkew()).Before(c.NotBefore.Time) || s.now().Add(s.contractClockSkew()).Before(c.IssuedAt.Time) {
		return middleware.ErrAccessWalletInvalid
	}
	_, err := s.contractRecord(ctx, &c, true)
	if errors.Is(err, signature.ErrWalletDependency) {
		return middleware.ErrAccessWalletUnavailable
	}
	if err != nil {
		return middleware.ErrAccessWalletInvalid
	}
	return nil
}

func decodeWalletAccessClaims(values jwt.MapClaims, claims *Claims) error {
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, claims)
}

// ToMapClaims copies verified token metadata for transport propagation.
func (c *Claims) ToMapClaims() (jwt.MapClaims, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	var result jwt.MapClaims
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *AuthService) boundContractCapability(ctx context.Context, authz *PlaybackAuthorization, capability *Claims) error {
	binding := authz.session
	meta := binding.wallet
	if !strings.HasPrefix(binding.SessionID, middleware.ContractWalletSessionPrefix) && (meta == nil || (meta.AccountKind != signature.AccountContract && meta.WalletProofID == "")) {
		return nil
	}
	if meta == nil {
		return ErrAuthorizationSessionRequired
	}
	if meta.CredentialType != AccessTokenType && meta.CredentialType != CapabilityTokenType {
		return ErrAuthorizationSessionRequired
	}
	if meta.ExpiresAt.IsZero() || !s.now().Before(meta.ExpiresAt) {
		return ErrTokenExpired
	}
	if authz.rule.ChainID != 0 && authz.rule.ChainID != meta.ChainID {
		return ErrPlaybackDenied
	}
	original := &Claims{WalletAddress: authz.wallet, AccountKind: meta.AccountKind, ChainID: meta.ChainID, WalletProofID: meta.WalletProofID, WalletSessionExpiresAt: meta.WalletSessionExpiresAt,
		SessionID: binding.SessionID, AuthEpoch: binding.AuthEpoch, RestoreEpoch: binding.RestoreEpoch, JTI: meta.JTI,
		RegisteredClaims: jwt.RegisteredClaims{Subject: authz.wallet, ExpiresAt: jwt.NewNumericDate(meta.ExpiresAt)}}
	if _, err := s.contractRecord(ctx, original, meta.CredentialType == AccessTokenType); err != nil {
		return err
	}
	capability.AccountKind = signature.AccountContract
	capability.WalletProofID = meta.WalletProofID
	capability.WalletSessionExpiresAt = meta.WalletSessionExpiresAt
	capability.ChainID = meta.ChainID
	if capability.ExpiresAt.Time.After(meta.ExpiresAt) {
		capability.ExpiresAt = jwt.NewNumericDate(meta.ExpiresAt)
	}
	return nil
}

// WithWalletDefaultChainID binds chain-less RPC methods to this server profile.
func WithWalletDefaultChainID(chainID int64) AuthServiceOption {
	return func(s *AuthService) {
		if chainID > 0 {
			s.walletDefaultChainID = chainID
		}
	}
}
func (s *AuthService) DefaultWalletChainID() int64 { return s.walletDefaultChainID }

// AuthenticateStoredWalletChallenge uses the admitted challenge's chain for
// protocols that carry only the challenge ID. Caller metadata cannot re-label it.
func (s *AuthService) AuthenticateStoredWalletChallenge(ctx context.Context, wallet, id, sig string) (string, error) {
	challenge, err := s.challengeStore.GetChallenge(ctx, id)
	if err != nil {
		return "", err
	}
	return s.AuthenticateWithWallet(ctx, wallet, id, sig, challenge.ChainID)
}
