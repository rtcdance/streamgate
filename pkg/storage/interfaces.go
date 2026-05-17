package storage

import (
	"context"
	"errors"
	"time"

	"streamgate/pkg/models"
)

var (
	// ErrChallengeUsed is returned when a challenge has already been consumed.
	ErrChallengeUsed = errors.New("challenge already used")
	// ErrChallengeNotFound is returned when a challenge ID does not exist.
	ErrChallengeNotFound = errors.New("challenge not found")
)

// UserRepository abstracts user data access.
//
//go:generate mockgen -destination=mocks/mock_user_repository.go -package=mocks streamgate/pkg/storage UserRepository
type UserRepository interface {
	GetUser(ctx context.Context, username string) (*models.User, error)
	CreateUser(ctx context.Context, user *models.User) error
	UpdateUser(ctx context.Context, user *models.User) error
}

// TokenBlacklist stores revoked JWT IDs.
//
//go:generate mockgen -destination=mocks/mock_token_blacklist.go -package=mocks streamgate/pkg/storage TokenBlacklist
type TokenBlacklist interface {
	Revoke(ctx context.Context, jti string, expiresAt time.Time) error
	IsRevoked(ctx context.Context, jti string) bool
	Close() error
}

// ChallengeStore stores wallet login challenges.
//
//go:generate mockgen -destination=mocks/mock_challenge_store.go -package=mocks streamgate/pkg/storage ChallengeStore
type ChallengeStore interface {
	SaveChallenge(ctx context.Context, challenge *WalletChallenge) error
	GetChallenge(ctx context.Context, id string) (*WalletChallenge, error)
	MarkChallengeUsed(ctx context.Context, id string, usedAt time.Time) error
}
