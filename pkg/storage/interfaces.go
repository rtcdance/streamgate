package storage

import (
	"context"

	"streamgate/pkg/models"
)

// UserRepository abstracts user data access.
//
//go:generate mockgen -destination=mocks/mock_user_repository.go -package=mocks streamgate/pkg/storage UserRepository
type UserRepository interface {
	GetUser(ctx context.Context, username string) (*models.User, error)
	CreateUser(ctx context.Context, user *models.User) error
	UpdateUser(ctx context.Context, user *models.User) error
}
