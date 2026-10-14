package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/rtcdance/streamgate/pkg/monitoring"
	stg "github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gagliardetto/solana-go"
	"github.com/golang-jwt/jwt/v4"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
)

var (
	svcPlaybackTokenIssued = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "streamgate_playback_token_issued_total",
		Help: "Total playback tokens issued",
	})
	svcWalletAuthTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "streamgate_wallet_auth_total",
		Help: "Total wallet authentication operations",
	}, []string{"operation", "status"})
	svcWalletAuthDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "streamgate_wallet_auth_duration_seconds",
		Help:    "Wallet authentication operation latency",
		Buckets: prometheus.DefBuckets,
	}, []string{"operation"})
)

func init() {
	for _, c := range []prometheus.Collector{svcPlaybackTokenIssued, svcWalletAuthTotal, svcWalletAuthDuration} {
		if err := prometheus.Register(c); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				panic(err)
			}
		}
	}
}

const defaultChallengeTTL = 5 * time.Minute

// IsValidSolanaAddress checks if the address is a valid Solana base58 public key.
// Uses the solana SDK for proper base58 validation.
func IsValidSolanaAddress(address string) bool {
	if len(address) < 32 || len(address) > 44 {
		return false
	}
	if strings.HasPrefix(address, "0x") {
		return false
	}
	_, err := solana.PublicKeyFromBase58(address)
	return err == nil
}

// isSolanaChain returns true for Solana chain IDs (negative values).
func isSolanaChain(chainID int64) bool {
	return chainID < 0
}

// TokenVerifyResult contains the result of a token verification.
type TokenVerifyResult struct {
	Valid         bool
	ExpiresAt     string
	WalletAddress string
}

//go:generate mockgen -destination=mocks/mock_wallet_sig_verifier.go -package=mocks streamgate/pkg/web3 SignatureVerifierInterface
type WalletSignatureVerifier = web3.SignatureVerifierInterface

// ChainAwareSignatureVerifier extends WalletSignatureVerifier with chain routing.
// If available, the auth service will use this interface to route signatures
// to the correct verification algorithm based on chain ID.
type ChainAwareSignatureVerifier interface {
	WalletSignatureVerifier
	VerifySolanaSignature(ctx context.Context, address, message, signature string) (bool, error)
	VerifyOffchainMessage(ctx context.Context, address, message, signature string) (bool, error)
}

// GenerateWalletChallenge creates and stores a one-time wallet login challenge.
// Supports both EVM (hex addresses) and Solana (base58 addresses) chains.
// signType controls the signing method: "siwe" (default, EIP-4361), "personal_sign",
// or "eip712". Solana chains ignore signType and always use Ed25519 off-chain verification.
//
// When possible, prefer "siwe" — it follows the EIP-4361 standard and provides better
// wallet UX (structured parsing, human-readable domain, nonce).
func (s *AuthService) GenerateWalletChallenge(ctx context.Context, walletAddress string, chainID int64, signType ...string) (*stg.WalletChallenge, error) {
	start := time.Now()
	_, span := monitoring.StartOTelSpan(ctx, "auth.generate_wallet_challenge",
		attribute.Int64("chain_id", chainID),
	)
	defer span.End()

	var normalizedAddr string
	if isSolanaChain(chainID) {
		if !IsValidSolanaAddress(walletAddress) {
			return nil, fmt.Errorf("invalid Solana wallet address: %s", walletAddress)
		}
		normalizedAddr = walletAddress
	} else {
		if !common.IsHexAddress(walletAddress) {
			return nil, fmt.Errorf("invalid wallet address: %s", walletAddress)
		}
		normalizedAddr = common.HexToAddress(walletAddress).Hex()
	}

	nonce, err := generateNonce()
	if err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(s.challengeTTL)

	st := "siwe"
	if len(signType) > 0 && signType[0] != "" {
		st = strings.ToLower(strings.TrimSpace(signType[0]))
	}
	// Fail closed on an unrecognised signing type. Previously anything other
	// than "siwe"/"eip712" silently fell through to the plain personal_sign
	// message, so a typo or an unsupported wallet type would be accepted under
	// a weaker scheme than the caller asked for.
	switch st {
	case "siwe", "personal_sign", "eip712":
	default:
		return nil, fmt.Errorf("%w: unsupported signing type %q (want siwe, personal_sign or eip712)", ErrNotSupported, st)
	}

	challenge := &stg.WalletChallenge{
		ID:            generateID(),
		WalletAddress: normalizedAddr,
		ChainID:       chainID,
		SigningType:   st,
		Nonce:         nonce,
		IssuedAt:      now,
		ExpiresAt:     expiresAt,
	}

	switch st {
	case "siwe":
		siweMsg := web3.NewSIWEMessage(
			s.siweDomain,
			challenge.WalletAddress,
			s.siweURI,
			challenge.ChainID,
			challenge.Nonce,
			challenge.IssuedAt,
			web3.WithSIWEExpirationTime(challenge.ExpiresAt),
		)
		challenge.Message = web3.BuildSIWEMessage(siweMsg)
	case "eip712":
		typedData := s.buildEIP712Challenge(challenge)
		encoded, err := json.Marshal(typedData)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize EIP-712 typed data: %w", err)
		}
		challenge.Message = string(encoded)
	default:
		challenge.Message = fmt.Sprintf(
			"Sign this message to authenticate with StreamGate.\nAddress: %s\nChain ID: %d\nNonce: %s\nIssued At: %s\nExpires At: %s",
			challenge.WalletAddress,
			challenge.ChainID,
			challenge.Nonce,
			challenge.IssuedAt.Format(time.RFC3339),
			challenge.ExpiresAt.Format(time.RFC3339),
		)
	}

	if err := s.challengeStore.SaveChallenge(ctx, challenge); err != nil {
		svcWalletAuthTotal.WithLabelValues("generate_challenge", "failure").Inc()
		svcWalletAuthDuration.WithLabelValues("generate_challenge").Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("failed to store challenge: %w", err)
	}

	svcWalletAuthTotal.WithLabelValues("generate_challenge", "success").Inc()
	svcWalletAuthDuration.WithLabelValues("generate_challenge").Observe(time.Since(start).Seconds())
	return challenge, nil
}

// AuthenticateWithWallet verifies a challenge-based wallet login and issues a JWT.
// Supports both EVM (secp256k1/EIP-191) and Solana (ed25519) signature verification.
func (s *AuthService) AuthenticateWithWallet(ctx context.Context, walletAddress, challengeID, signature string, chainID int64) (result string, err error) {
	start := time.Now()
	_, span := monitoring.StartOTelSpan(ctx, "auth.authenticate_with_wallet",
		attribute.Int64("chain_id", chainID),
	)
	defer func() {
		span.End()
		status := "success"
		if err != nil {
			status = "failure"
		}
		svcWalletAuthTotal.WithLabelValues("authenticate", status).Inc()
		svcWalletAuthDuration.WithLabelValues("authenticate").Observe(time.Since(start).Seconds())

		if s.auditLogger != nil {
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			}
			s.auditLogger.Log(ctx, "auth.wallet_login", walletAddress, "auth", challengeID, err == nil, errMsg, "")
		}
	}()

	if challengeID == "" {
		return "", ErrInvalidRequest
	}
	if signature == "" {
		return "", ErrInvalidRequest
	}

	challenge, err := s.challengeStore.GetChallenge(ctx, challengeID)
	if err != nil {
		return "", err
	}

	var normalizedAddress string
	if isSolanaChain(challenge.ChainID) {
		// Solana path: base58 address, no hex normalization
		if !IsValidSolanaAddress(walletAddress) {
			return "", fmt.Errorf("invalid Solana wallet address: %s", walletAddress)
		}
		normalizedAddress = walletAddress
	} else {
		// EVM path: hex address normalization
		if !common.IsHexAddress(walletAddress) {
			return "", fmt.Errorf("invalid wallet address: %s", walletAddress)
		}
		normalizedAddress = common.HexToAddress(walletAddress).Hex()
	}

	if challenge.WalletAddress != normalizedAddress {
		return "", ErrInvalidCredential
	}
	if challenge.ChainID != 0 && chainID != challenge.ChainID {
		return "", ErrChainIDMismatch
	}
	if !challenge.UsedAt.IsZero() {
		return "", stg.ErrChallengeUsed
	}
	if time.Now().UTC().After(challenge.ExpiresAt) {
		return "", ErrChallengeExpired
	}

	// Route to the correct signature verifier based on chain type and signing type
	var valid bool
	if isSolanaChain(challenge.ChainID) {
		verifier, ok := s.signatureVerifier.(ChainAwareSignatureVerifier)
		if !ok || verifier == nil {
			return "", ErrNotSupported
		}
		valid, err = verifier.VerifyOffchainMessage(ctx, normalizedAddress, challenge.Message, signature)
	} else if challenge.SigningType == "eip712" {
		if s.eip712Verifier == nil {
			return "", ErrNotSupported
		}
		// EIP-712 typed data verification: parse the stored JSON message
		var typedData web3.EIP712TypedData
		if err := json.Unmarshal([]byte(challenge.Message), &typedData); err != nil {
			return "", fmt.Errorf("failed to parse stored EIP-712 typed data: %w", err)
		}
		valid, err = s.eip712Verifier.VerifyTypedData(normalizedAddress, &typedData, signature)
	} else {
		if s.signatureVerifier == nil {
			return "", ErrNotSupported
		}
		// Default: EIP-191 personal_sign
		valid, err = s.signatureVerifier.VerifySignature(ctx, normalizedAddress, challenge.Message, signature)
	}
	if err != nil {
		return "", fmt.Errorf("failed to verify wallet signature: %w", err)
	}
	if !valid {
		return "", ErrInvalidCredential
	}

	// For SIWE challenges, validate the parsed message against expected values.
	// This ensures domain binding (phishing protection), nonce match, and
	// address consistency per EIP-4361.
	if challenge.SigningType == "siwe" {
		parsedSIWE, parseErr := web3.ParseSIWEMessage(challenge.Message)
		if parseErr != nil {
			return "", fmt.Errorf("failed to parse SIWE message: %w", parseErr)
		}
		if validateErr := web3.ValidateSIWEMessage(parsedSIWE,
			s.siweDomain,
			normalizedAddress,
			challenge.Nonce,
			challenge.ChainID,
		); validateErr != nil {
			return "", fmt.Errorf("SIWE validation failed: %w", validateErr)
		}
	}

	if err := s.challengeStore.MarkChallengeUsed(ctx, challengeID, time.Now().UTC()); err != nil {
		// This catches the TOCTOU race: if a concurrent request consumed the challenge
		// between the fast-fail check above and this atomic mark, the error here
		// prevents token issuance.
		return "", fmt.Errorf("failed to consume challenge: %w", err)
	}

	return s.generateWalletToken(normalizedAddress)
}

// buildEIP712Challenge constructs an EIP-712 typed data structure from a wallet challenge.
// This allows wallets to sign a structured message instead of a plain-text string,
// providing better user experience and security in MetaMask and similar wallets.
func (s *AuthService) buildEIP712Challenge(challenge *stg.WalletChallenge) *web3.EIP712TypedData {
	domain := web3.EIP712Domain{
		Name:    "StreamGate",
		Version: "1",
		ChainId: big.NewInt(challenge.ChainID),
	}

	return &web3.EIP712TypedData{
		Types: web3.EIP712Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
			},
			"Authentication": {
				{Name: "wallet", Type: "address"},
				{Name: "nonce", Type: "string"},
				{Name: "issuedAt", Type: "string"},
				{Name: "expiresAt", Type: "string"},
				{Name: "domain", Type: "string"},
				{Name: "uri", Type: "string"},
				{Name: "version", Type: "string"},
			},
		},
		PrimaryType: "Authentication",
		Domain:      domain,
		Message: map[string]interface{}{
			"wallet":    challenge.WalletAddress,
			"nonce":     challenge.Nonce,
			"issuedAt":  challenge.IssuedAt.Format(time.RFC3339),
			"expiresAt": challenge.ExpiresAt.Format(time.RFC3339),
			"domain":    s.siweDomain,
			"uri":       s.siweURI,
			"version":   "1",
		},
	}
}

func (s *AuthService) generateWalletToken(walletAddress string) (string, error) {
	sessionID, err := s.StartSession()
	if err != nil {
		return "", err
	}
	claims := &Claims{
		Username:         walletAddress,
		WalletAddress:    walletAddress,
		JTI:              generateID(),
		SessionID:        sessionID,
		AuthEpoch:        1,
		RegisteredClaims: accessTokenClaims(walletAddress, s.jwtExpiry),
	}

	return s.signToken(claims)
}

// GeneratePlaybackToken creates a short-lived token for segment access after manifest authorization.
// PlaybackTokenOption customises capability issuance.
type PlaybackTokenOption func(*playbackTokenOptions)

type playbackTokenOptions struct {
	sessionID   string
	authEpoch   int64
	ruleVersion int64
	// ruleVersionSet distinguishes "the caller supplied version 0" from "the
	// caller supplied nothing", because 0 is a legal version (a content that has
	// never had a rule written).
	ruleVersionSet bool
}

// WithSessionBinding ties a capability to the session that requested it, so
// revoking that session (a logout) stops media already in flight instead of
// letting the capability run out its full TTL.
func WithSessionBinding(sessionID string, epoch int64) PlaybackTokenOption {
	return func(o *playbackTokenOptions) {
		o.sessionID, o.authEpoch = sessionID, epoch
	}
}

// WithRuleVersion records the rule version the capability was issued under, so a
// later rule change makes it stale without any per-segment chain query.
func WithRuleVersion(version int64) PlaybackTokenOption {
	return func(o *playbackTokenOptions) {
		o.ruleVersion, o.ruleVersionSet = version, true
	}
}

func (s *AuthService) GeneratePlaybackToken(ctx context.Context, walletAddress, contentID, contract, tokenID string, chainID int64, ttl time.Duration, clientFingerprint string, options ...PlaybackTokenOption) (string, error) {
	// A playback token is a wallet-bound capability proving prior NFT
	// authorization; issuing one to an empty wallet would mint an
	// unattributable credential.
	if walletAddress == "" {
		monitoring.AuthOperationsTotal.WithLabelValues("generate_playback_token", "failure").Inc()
		return "", errors.New("wallet address is required for playback token")
	}

	_, span := monitoring.StartOTelSpan(ctx, "auth.generate_playback_token",
		attribute.String("content_id", contentID),
		attribute.Int64("chain_id", chainID),
	)
	defer span.End()

	if ttl <= 0 {
		// The policy default, not a second hardcoded window: a caller that
		// forgets the TTL must not get a 30-minute capability by accident.
		ttl = s.PlaybackTokenTTL()
	}

	opts := playbackTokenOptions{}
	for _, apply := range options {
		apply(&opts)
	}
	if opts.sessionID == "" {
		// A capability must always name a session, because every playlist and
		// segment resolves it through ParsePlaybackToken, which requires the
		// binding. With no caller-supplied session it gets its own — valid, but
		// only revocable by revoking that session. The production path always
		// supplies the requesting session so a logout reaches media in flight.
		sessionID, err := s.StartSession()
		if err != nil {
			monitoring.AuthOperationsTotal.WithLabelValues("generate_playback_token", "failure").Inc()
			return "", err
		}
		opts.sessionID, opts.authEpoch = sessionID, 1
	}
	if !opts.ruleVersionSet {
		// The version the capability is issued under comes from the authority, so
		// a capability is never minted with a version that was guessed. If the
		// authority cannot be read the issuance FAILS: a capability with no
		// version could never be invalidated by a rule change.
		version, err := s.currentRuleVersion(ctx, contentID)
		if err != nil {
			monitoring.AuthOperationsTotal.WithLabelValues("generate_playback_token", "failure").Inc()
			return "", fmt.Errorf("failed to read the rule version for content %s: %w", contentID, err)
		}
		opts.ruleVersion = version
	}

	claims := &Claims{
		Username:          walletAddress,
		TokenType:         "playback",
		WalletAddress:     walletAddress,
		ContentID:         contentID,
		Contract:          contract,
		TokenID:           tokenID,
		ChainID:           chainID,
		JTI:               generateID(),
		ClientFingerprint: clientFingerprint,
		SessionID:         opts.sessionID,
		AuthEpoch:         opts.authEpoch,
		RuleVersion:       opts.ruleVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: "streamgate",
			// A capability and an access token are signed with the same key, so
			// the audience is what stops one being presented where the other is
			// expected.
			Audience:  jwt.ClaimStrings{PlaybackCapabilityAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Subject:   contentID,
		},
	}

	svcPlaybackTokenIssued.Inc()
	// A capability is signed with the capability key — INDEPENDENT of the
	// access key — so an access token can never verify as a capability (and
	// the audience check is a second, independent discriminator).
	token, err := s.signTokenWithKind(claims, capabilityToken)
	if err != nil {
		monitoring.AuthOperationsTotal.WithLabelValues("generate_playback_token", "failure").Inc()
		return "", err
	}
	monitoring.AuthOperationsTotal.WithLabelValues("generate_playback_token", "success").Inc()
	return token, nil
}

// ParsePlaybackToken validates a playback token against the requested content
// WITHOUT a wallet-binding check: the wallet comes FROM the token's claims.
// Used by paths where the token itself is the credential (PlaybackTokenAuth
// middleware, token-only mounts). Fingerprint binding is enforced when the
// token was issued with one: a fingerprint-bound token must be presented with
// the same X-Client-Fingerprint; tokens issued without a fingerprint are not
// fingerprint-bound.
func (s *AuthService) ParsePlaybackToken(ctx context.Context, tokenString, contentID, clientFingerprint string) (*Claims, error) {
	_, span := monitoring.StartOTelSpan(ctx, "auth.parse_playback_token",
		attribute.String("content_id", contentID),
	)
	defer span.End()

	// Verified against the capability key ONLY: with key separation an access
	// token can never pass here even before the audience check, because the
	// two credential kinds are signed with independent keys.
	claims, err := s.parseTokenCapability(tokenString)
	if err != nil {
		monitoring.AuthOperationsTotal.WithLabelValues("parse_playback_token", "failure").Inc()
		return nil, err
	}
	if claims.TokenType != "playback" {
		monitoring.AuthOperationsTotal.WithLabelValues("parse_playback_token", "failure").Inc()
		return nil, errors.New("not a playback token")
	}
	// The audience keeps a credential minted for one purpose from being spent on
	// another. Both are signed with the same key, so this is the discriminator.
	if !claimAudienceContains(claims.Audience, PlaybackCapabilityAudience) {
		monitoring.AuthOperationsTotal.WithLabelValues("parse_playback_token", "failure").Inc()
		return nil, errors.New("playback token audience mismatch")
	}
	if claims.Subject != contentID {
		monitoring.AuthOperationsTotal.WithLabelValues("parse_playback_token", "failure").Inc()
		return nil, errors.New("playback token content mismatch")
	}
	// A fingerprint-bound token must be presented with the same fingerprint —
	// presenting NO fingerprint does not satisfy the binding.
	if claims.ClientFingerprint != "" && claims.ClientFingerprint != clientFingerprint {
		monitoring.AuthOperationsTotal.WithLabelValues("parse_playback_token", "failure").Inc()
		return nil, errors.New("playback token fingerprint mismatch")
	}
	if s.blacklist != nil && claims.ID != "" {
		if s.blacklist.IsRevoked(ctx, claims.ID) {
			monitoring.AuthOperationsTotal.WithLabelValues("parse_playback_token", "failure").Inc()
			return nil, ErrTokenRevoked
		}
	}
	// The revocation version. Every playlist, segment, init and key request
	// resolves the capability through here, so this is the one place that has to
	// hold for revocation to reach media already in flight. Without it, logging
	// out would only stop the exact token presented and a capability already
	// handed to a player would keep working for its whole TTL.
	//
	// Presence is required, not defaulted: a capability minted before sessions
	// existed has no epoch, and treating "absent" as "current" would let such a
	// credential bypass revocation entirely.
	if claims.SessionID == "" {
		monitoring.AuthOperationsTotal.WithLabelValues("parse_playback_token", "failure").Inc()
		return nil, errors.New("playback token is not bound to a session")
	}
	if !s.SessionValid(claims.SessionID, claims.AuthEpoch) {
		monitoring.AuthOperationsTotal.WithLabelValues("parse_playback_token", "failure").Inc()
		return nil, ErrTokenRevoked
	}
	// The authorization-rule version. A segment request cannot afford a chain
	// query, so the capability carries the version it was issued under and is
	// refused once the content's rules move. Without this, raising a rule's
	// threshold or retargeting it would not reach media already in flight until
	// the capability's TTL happened to expire.
	if current, err := s.currentRuleVersion(ctx, claims.Subject); err != nil {
		monitoring.AuthOperationsTotal.WithLabelValues("parse_playback_token", "failure").Inc()
		return nil, fmt.Errorf("failed to read the rule version: %w", err)
	} else if current != claims.RuleVersion {
		monitoring.AuthOperationsTotal.WithLabelValues("parse_playback_token", "failure").Inc()
		return nil, ErrTokenRevoked
	}
	monitoring.AuthOperationsTotal.WithLabelValues("parse_playback_token", "success").Inc()
	return claims, nil
}

// ValidatePlaybackToken validates a playback token, ensures it matches the
// requested content, and enforces that it was issued to walletAddress. The
// wallet is REQUIRED — passing an empty wallet is an error, not a skip — which
// forces every call site to prove whose wallet it is checking against and
// prevents the silent no-wallet-compare downgrade this check used to have.
func (s *AuthService) ValidatePlaybackToken(ctx context.Context, tokenString, contentID, clientFingerprint, walletAddress string) (*Claims, error) {
	if walletAddress == "" {
		monitoring.AuthOperationsTotal.WithLabelValues("validate_playback_token", "failure").Inc()
		return nil, errors.New("wallet address is required to validate playback token")
	}
	claims, err := s.ParsePlaybackToken(ctx, tokenString, contentID, clientFingerprint)
	if err != nil {
		return nil, err
	}
	if claims.WalletAddress != walletAddress {
		monitoring.AuthOperationsTotal.WithLabelValues("validate_playback_token", "failure").Inc()
		return nil, errors.New("playback token wallet mismatch")
	}
	return claims, nil
}

func generateNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return hex.EncodeToString(buf), nil
}

// RevokeToken parses a JWT and adds its JTI to the blacklist.
// This is best-effort: expired or invalid tokens are silently accepted.
func (s *AuthService) RevokeToken(ctx context.Context, tokenString string) error {
	claims, err := s.ParseToken(tokenString)
	if err != nil {
		// Token already invalid, nothing to revoke
		return nil
	}
	if s.blacklist == nil {
		return nil
	}
	jti := claims.JTI
	if jti == "" {
		return nil
	}
	expiresAt := time.Now()
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time
	}
	if err := s.blacklist.Revoke(ctx, jti, expiresAt); err != nil {
		if s.auditLogger != nil {
			s.auditLogger.Log(ctx, "auth.token_revoke", claims.WalletAddress, "token", jti, false, err.Error(), "")
		}
		return err
	}
	if s.auditLogger != nil {
		s.auditLogger.Log(ctx, "auth.token_revoke", claims.WalletAddress, "token", jti, true, "", "")
	}
	return nil
}

// VerifyToken checks if a token is valid, not expired, and not revoked.
func (s *AuthService) VerifyToken(ctx context.Context, tokenString string) (*TokenVerifyResult, error) {
	claims, err := s.ParseToken(tokenString)
	if err != nil {
		return &TokenVerifyResult{Valid: false}, err
	}
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		return &TokenVerifyResult{Valid: false}, ErrTokenExpired
	}
	if s.blacklist != nil && claims.JTI != "" && s.blacklist.IsRevoked(ctx, claims.JTI) {
		return &TokenVerifyResult{Valid: false}, ErrTokenRevoked
	}
	expiresAtStr := ""
	if claims.ExpiresAt != nil {
		expiresAtStr = claims.ExpiresAt.Format(time.RFC3339)
	}
	return &TokenVerifyResult{
		Valid:         true,
		ExpiresAt:     expiresAtStr,
		WalletAddress: claims.WalletAddress,
	}, nil
}

// IsTokenRevoked checks if a token's JTI is in the blacklist.
func (s *AuthService) IsTokenRevoked(ctx context.Context, jti string) bool {
	if s.blacklist == nil || jti == "" {
		return false
	}
	return s.blacklist.IsRevoked(ctx, jti)
}
