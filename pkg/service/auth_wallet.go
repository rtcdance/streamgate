package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gagliardetto/solana-go"
	"github.com/golang-jwt/jwt/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	stg "streamgate/pkg/storage"
	"streamgate/pkg/web3"
)

var (
	svcPlaybackTokenIssued = promauto.NewCounter(prometheus.CounterOpts{
		Name: "streamgate_playback_token_issued_total",
		Help: "Total playback tokens issued",
	})
)

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

// WalletSignatureVerifier verifies wallet signatures.
// Implementations must handle chain-specific verification:
// EVM chains use secp256k1/EIP-191, Solana uses ed25519.
//
//go:generate mockgen -destination=mocks/mock_wallet_sig_verifier.go -package=mocks streamgate/pkg/service WalletSignatureVerifier
type WalletSignatureVerifier interface {
	VerifySignature(ctx context.Context, address, message, signature string) (bool, error)
}

// ChainAwareSignatureVerifier extends WalletSignatureVerifier with chain routing.
// If available, the auth service will use this interface to route signatures
// to the correct verification algorithm based on chain ID.
type ChainAwareSignatureVerifier interface {
	WalletSignatureVerifier
	VerifySolanaSignature(ctx context.Context, address, message, signature string) (bool, error)
	VerifyOffchainMessage(ctx context.Context, address, message, signature string) (bool, error)
}


func defaultWalletSignatureVerifier() WalletSignatureVerifier {
	return web3.NewSignatureVerifier(zap.NewNop())
}

func defaultEIP712Verifier() *web3.EIP712Verifier {
	return web3.NewEIP712Verifier(zap.NewNop())
}

// GenerateWalletChallenge creates and stores a one-time wallet login challenge.
// Supports both EVM (hex addresses) and Solana (base58 addresses) chains.
// signType controls the signing method: "personal_sign" (default) or "eip712".
// Solana chains ignore signType and always use Ed25519 off-chain verification.
func (s *AuthService) GenerateWalletChallenge(ctx context.Context, walletAddress string, chainID int64, signType ...string) (*stg.WalletChallenge, error) {
	st := "personal_sign"
	if len(signType) > 0 && signType[0] != "" {
		st = signType[0]
	}
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
	challenge := &stg.WalletChallenge{
		ID:            generateID(),
		WalletAddress: normalizedAddr,
		ChainID:       chainID,
		SigningType:   st,
		Nonce:         nonce,
		IssuedAt:      now,
		ExpiresAt:     expiresAt,
	}
	challenge.Message = fmt.Sprintf(
		"Sign this message to authenticate with StreamGate.\nAddress: %s\nChain ID: %d\nNonce: %s\nIssued At: %s\nExpires At: %s",
		challenge.WalletAddress,
		challenge.ChainID,
		challenge.Nonce,
		challenge.IssuedAt.Format(time.RFC3339),
		challenge.ExpiresAt.Format(time.RFC3339),
	)

	// SIWE (EIP-4361) standard message format
	if st == "siwe" {
		siweMsg := web3.NewSIWEMessage(
			"streamgate.io",
			challenge.WalletAddress,
			"https://streamgate.io/login",
			challenge.ChainID,
			challenge.Nonce,
			challenge.IssuedAt,
			web3.WithSIWEExpirationTime(challenge.ExpiresAt),
		)
		challenge.Message = web3.BuildSIWEMessage(siweMsg)
	}

	if err := s.challengeStore.SaveChallenge(ctx, challenge); err != nil {
		return nil, fmt.Errorf("failed to store challenge: %w", err)
	}

	return challenge, nil
}

// AuthenticateWithWallet verifies a challenge-based wallet login and issues a JWT.
// Supports both EVM (secp256k1/EIP-191) and Solana (ed25519) signature verification.
func (s *AuthService) AuthenticateWithWallet(ctx context.Context, walletAddress, challengeID, signature string) (string, error) {
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
	// Fast-fail: skip expensive signature verification for already-used challenges.
	// The atomic MarkChallengeUsed below provides the definitive TOCTOU-safe check.
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
		if !ok {
			return "", ErrNotSupported
		}
		valid, err = verifier.VerifyOffchainMessage(ctx, normalizedAddress, challenge.Message, signature)
	} else if challenge.SigningType == "eip712" {
		// EIP-712 typed data verification: reconstruct the typed data from the challenge
		typedData := s.buildEIP712Challenge(challenge)
		valid, err = s.eip712Verifier.VerifyTypedData(normalizedAddress, typedData, signature)
	} else {
		// Default: EIP-191 personal_sign
		valid, err = s.signatureVerifier.VerifySignature(ctx, normalizedAddress, challenge.Message, signature)
	}
	if err != nil {
		return "", fmt.Errorf("failed to verify wallet signature: %w", err)
	}
	if !valid {
		return "", ErrInvalidCredential
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
		Name:              "StreamGate",
		Version:           "1",
		ChainId:           big.NewInt(challenge.ChainID),
		VerifyingContract: "",
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
			"domain":    "streamgate.io",
			"uri":       "https://streamgate.io/login",
			"version":   "1",
		},
	}
}

func (s *AuthService) generateWalletToken(walletAddress string) (string, error) {
	claims := &Claims{
		Username:      walletAddress,
		WalletAddress: walletAddress,
		JTI:           generateID(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.jwtExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Subject:   walletAddress,
		},
	}

	return s.signToken(claims)
}

// GeneratePlaybackToken creates a short-lived token for segment access after manifest authorization.
func (s *AuthService) GeneratePlaybackToken(ctx context.Context, walletAddress, contentID, contract, tokenID string, chainID int64, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}

	claims := &Claims{
		Username:      walletAddress,
		WalletAddress: walletAddress,
		ContentID:     contentID,
		Contract:      contract,
		TokenID:       tokenID,
		ChainID:       chainID,
		JTI:           generateID(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Subject:   contentID,
		},
	}

	svcPlaybackTokenIssued.Inc()
	return s.signToken(claims)
}

// ValidatePlaybackToken validates a playback token and ensures it matches the requested content.
func (s *AuthService) ValidatePlaybackToken(ctx context.Context, tokenString, contentID string) (*Claims, error) {
	claims, err := s.ParseToken(tokenString)
	if err != nil {
		return nil, err
	}
	if claims.Subject != contentID {
		return nil, errors.New("playback token content mismatch")
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
	return s.blacklist.Revoke(ctx, jti, expiresAt)
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
