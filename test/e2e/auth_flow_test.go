package e2e_test

import (
	"context"
	"testing"

	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/rtcdance/streamgate/pkg/service"

	"github.com/stretchr/testify/require"
)

// MockAuthStorage for testing
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

func TestAuthFlow_UserRegistrationAndLogin(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Step 1: Register user
	err := authService.Register(context.Background(), "testuser", "password123", "test@example.com")
	require.NoError(t, err)

	// Step 2: Verify user was created
	user, err := storage.GetUser(context.Background(), "testuser")
	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, "testuser", user.Username)

	// Step 3: Login with correct credentials
	token, err := authService.Authenticate(context.Background(), "testuser", "password123")
	require.NoError(t, err)
	require.NotEqual(t, "", token)

	// Step 4: Verify token
	valid, err := authService.Verify(token)
	require.NoError(t, err)
	require.True(t, valid)

	// Step 5: Parse token to get claims
	claims, err := authService.ParseToken(token)
	require.NoError(t, err)
	require.Equal(t, "testuser", claims.Username)
}

func TestAuthFlow_FailedLogin(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Register user
	_ = authService.Register(context.Background(), "testuser", "password123", "test@example.com")

	_, err := authService.Authenticate(context.Background(), "testuser", "wrongpassword")
	require.Error(t, err)

	// Try login with non-existent user
	_, err = authService.Authenticate(context.Background(), "nonexistent", "password123")
	require.Error(t, err)
}

func TestAuthFlow_TokenRefresh(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Register and login
	_ = authService.Register(context.Background(), "testuser", "password123", "test@example.com")
	token, err := authService.Authenticate(context.Background(), "testuser", "password123")
	require.NoError(t, err)

	// Refresh token
	newToken, err := authService.RefreshToken(context.Background(), token)
	require.NoError(t, err)
	require.NotEqual(t, token, newToken)

	// Verify new token
	valid, err := authService.Verify(newToken)
	require.NoError(t, err)
	require.True(t, valid)
}

func TestAuthFlow_PasswordChange(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Register user
	_ = authService.Register(context.Background(), "testuser", "oldpassword", "test@example.com")

	// Change password
	err := authService.ChangePassword(context.Background(), "testuser", "oldpassword", "newpassword")
	require.NoError(t, err)

	// Old password should not work
	_, err = authService.Authenticate(context.Background(), "testuser", "oldpassword")
	require.Error(t, err)

	// New password should work
	token, err := authService.Authenticate(context.Background(), "testuser", "newpassword")
	require.NoError(t, err)
	require.NotEqual(t, "", token)
}
