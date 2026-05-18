package auth

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v4"

	stg "streamgate/pkg/storage"
)

//go:generate mockgen -destination=mocks/mock_token_verifier.go -package=mocks streamgate/pkg/service/auth TokenVerifier
type TokenVerifier interface {
	ParseToken(tokenString string) (*Claims, error)
}

//go:generate mockgen -destination=mocks/mock_token_blacklist.go -package=mocks streamgate/pkg/service/auth TokenBlacklist
type TokenBlacklist interface {
	Revoke(ctx context.Context, jti string, expiresAt time.Time) error
	IsRevoked(ctx context.Context, jti string) bool
	Close() error
}

//go:generate mockgen -destination=mocks/mock_wallet_sig_verifier.go -package=mocks streamgate/pkg/service/auth WalletSignatureVerifier
type WalletSignatureVerifier interface {
	VerifySignature(ctx context.Context, address, message, signature string) (bool, error)
}

//go:generate mockgen -destination=mocks/mock_challenge_store.go -package=mocks streamgate/pkg/service/auth ChallengeStore
type ChallengeStore interface {
	SaveChallenge(ctx context.Context, challenge *stg.WalletChallenge) error
	GetChallenge(ctx context.Context, id string) (*stg.WalletChallenge, error)
	MarkChallengeUsed(ctx context.Context, id string, usedAt time.Time) error
}

type TokenVerifyResult struct {
	Valid         bool
	ExpiresAt     string
	WalletAddress string
}

type Claims struct {
	Username      string `json:"username"`
	WalletAddress string `json:"wallet_address,omitempty"`
	ContentID     string `json:"content_id,omitempty"`
	Contract      string `json:"contract,omitempty"`
	TokenID       string `json:"token_id,omitempty"`
	ChainID       int64  `json:"chain_id,omitempty"`
	JTI           string `json:"jti,omitempty"`
	jwt.RegisteredClaims
}
