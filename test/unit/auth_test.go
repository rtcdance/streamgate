package unit_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"streamgate/pkg/models"
	"streamgate/pkg/service"
)

// MockAuthStorage implements AuthStorage for testing
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
	m.users[user.Username] = user
	return nil
}

func (m *MockAuthStorage) UpdateUser(ctx context.Context, user *models.User) error {
	m.users[user.Username] = user
	return nil
}

func TestAuthService_Register(t *testing.T) {
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Register user
	err := authService.Register(context.Background(), "testuser", "password123", "test@example.com")
	require.NoError(t, err)

	// Verify user was created
	user, err := storage.GetUser(context.Background(), "testuser")
	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, "testuser", user.Username)
	require.Equal(t, "test@example.com", user.Email)
}

func TestAuthService_Authenticate(t *testing.T) {
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Register user
	err := authService.Register(context.Background(), "testuser", "password123", "test@example.com")
	require.NoError(t, err)

	// Authenticate with correct password
	token, err := authService.Authenticate(context.Background(), "testuser", "password123")
	require.NoError(t, err)
	require.NotEqual(t, "", token)

	// Authenticate with wrong password
	_, err = authService.Authenticate(context.Background(), "testuser", "wrongpassword")
	require.Error(t, err)
}

func TestAuthService_Verify(t *testing.T) {
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Register and authenticate
	authService.Register(context.Background(), "testuser", "password123", "test@example.com")
	token, err := authService.Authenticate(context.Background(), "testuser", "password123")
	require.NoError(t, err)

	// Verify token
	valid, err := authService.Verify(token)
	require.NoError(t, err)
	require.True(t, valid)

	// Verify invalid token
	valid, err = authService.Verify("invalid-token")
	require.Error(t, err)
	require.False(t, valid)
}

func TestAuthService_ParseToken(t *testing.T) {
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Register and authenticate
	authService.Register(context.Background(), "testuser", "password123", "test@example.com")
	token, err := authService.Authenticate(context.Background(), "testuser", "password123")
	require.NoError(t, err)

	// Parse token
	claims, err := authService.ParseToken(token)
	require.NoError(t, err)
	require.NotNil(t, claims)
	require.Equal(t, "testuser", claims.Username)
}

func TestAuthService_RefreshToken(t *testing.T) {
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Register and authenticate
	authService.Register(context.Background(), "testuser", "password123", "test@example.com")
	token, err := authService.Authenticate(context.Background(), "testuser", "password123")
	require.NoError(t, err)

	// Refresh token
	newToken, err := authService.RefreshToken(context.Background(), token)
	require.NoError(t, err)
	require.NotEqual(t, "", newToken)
	require.NotEqual(t, token, newToken)

	// Verify new token
	valid, err := authService.Verify(newToken)
	require.NoError(t, err)
	require.True(t, valid)
}

func TestAuthService_ChangePassword(t *testing.T) {
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Register user
	authService.Register(context.Background(), "testuser", "oldpassword", "test@example.com")

	// Change password
	err := authService.ChangePassword(context.Background(), "testuser", "oldpassword", "newpassword")
	require.NoError(t, err)

	// Authenticate with new password
	token, err := authService.Authenticate(context.Background(), "testuser", "newpassword")
	require.NoError(t, err)
	require.NotEqual(t, "", token)

	// Old password should not work
	_, err = authService.Authenticate(context.Background(), "testuser", "oldpassword")
	require.Error(t, err)
}
