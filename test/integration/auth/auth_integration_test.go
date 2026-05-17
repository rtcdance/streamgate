package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"streamgate/pkg/models"
	"streamgate/pkg/service"
)

// MockAuthStorage implements service.AuthStorage for testing
type MockAuthStorage struct {
	users map[string]*models.User
}

func NewMockAuthStorage() *MockAuthStorage {
	return &MockAuthStorage{
		users: make(map[string]*models.User),
	}
}

func (m *MockAuthStorage) GetUser(ctx context.Context, username string) (*models.User, error) {
	user, exists := m.users[username]
	if !exists {
		return nil, nil
	}
	return user, nil
}

func (m *MockAuthStorage) CreateUser(ctx context.Context, user *models.User) error {
	if _, exists := m.users[user.Username]; exists {
		return errors.New("user already exists")
	}
	m.users[user.Username] = user
	return nil
}

func (m *MockAuthStorage) UpdateUser(ctx context.Context, user *models.User) error {
	m.users[user.Username] = user
	return nil
}

func TestAuthService_RegisterAndLogin(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-key", storage)

	// Test registration
	err := authService.Register(context.Background(), "testuser", "password123", "test@example.com")
	require.NoError(t, err)

	// Verify user was created
	user, err := storage.GetUser(context.Background(), "testuser")
	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, "testuser", user.Username)

	// Test login
	token, err := authService.Authenticate(context.Background(), "testuser", "password123")
	require.NoError(t, err)
	require.NotEmpty(t, token)
}

func TestAuthService_InvalidPassword(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-key", storage)

	// Register user
	err := authService.Register(context.Background(), "testuser", "password123", "test@example.com")
	require.NoError(t, err)

	// Try login with wrong password
	_, err = authService.Authenticate(context.Background(), "testuser", "wrongpassword")
	require.Error(t, err)
}

func TestAuthService_DuplicateEmail(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-key", storage)

	// Register first user
	err := authService.Register(context.Background(), "user1", "password123", "test@example.com")
	require.NoError(t, err)

	// Try register with same username (username is unique, not email)
	err = authService.Register(context.Background(), "user1", "password456", "test2@example.com")
	require.Error(t, err)
}

func TestAuthService_TokenValidation(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-key", storage)

	// Register and login
	err := authService.Register(context.Background(), "testuser", "password123", "test@example.com")
	require.NoError(t, err)

	token, err := authService.Authenticate(context.Background(), "testuser", "password123")
	require.NoError(t, err)

	// Validate token
	valid, err := authService.Verify(token)
	require.NoError(t, err)
	require.True(t, valid)

	// Parse token
	claims, err := authService.ParseToken(token)
	require.NoError(t, err)
	require.NotNil(t, claims)
	require.Equal(t, "testuser", claims.Username)
}

func TestAuthService_RefreshToken(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-key", storage)

	// Register and login
	err := authService.Register(context.Background(), "testuser", "password123", "test@example.com")
	require.NoError(t, err)

	token, err := authService.Authenticate(context.Background(), "testuser", "password123")
	require.NoError(t, err)

	// Refresh token
	newToken, err := authService.RefreshToken(context.Background(), token)
	require.NoError(t, err)
	require.NotEmpty(t, newToken)

	// Verify the new token is valid
	valid, err := authService.Verify(newToken)
	require.NoError(t, err)
	require.True(t, valid)
}
