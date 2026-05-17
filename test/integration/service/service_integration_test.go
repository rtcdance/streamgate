package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"streamgate/pkg/models"
	"streamgate/pkg/service"
)

// MockServiceStorage for testing
type MockServiceStorage struct {
	users map[string]*models.User
}

func NewMockServiceStorage() *MockServiceStorage {
	return &MockServiceStorage{
		users: make(map[string]*models.User),
	}
}

func (m *MockServiceStorage) GetUser(ctx context.Context, username string) (*models.User, error) {
	user, exists := m.users[username]
	if !exists {
		return nil, nil
	}
	return user, nil
}

func (m *MockServiceStorage) CreateUser(ctx context.Context, user *models.User) error {
	m.users[user.Username] = user
	return nil
}

func (m *MockServiceStorage) UpdateUser(ctx context.Context, user *models.User) error {
	m.users[user.Username] = user
	return nil
}

func TestServiceIntegration_AuthenticationFlow(t *testing.T) {
	// Setup
	storage := NewMockServiceStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Register user
	err := authService.Register(context.Background(), "testuser", "password123", "test@example.com")
	require.NoError(t, err)

	// Authenticate
	token, err := authService.Authenticate(context.Background(), "testuser", "password123")
	require.NoError(t, err)

	// Verify token
	valid, err := authService.Verify(token)
	require.NoError(t, err)
	require.True(t, valid)

	// Parse token
	claims, err := authService.ParseToken(token)
	require.NoError(t, err)
	require.Equal(t, "testuser", claims.Username)
}

func TestServiceIntegration_MultipleUsers(t *testing.T) {
	// Setup
	storage := NewMockServiceStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Register multiple users
	users := []struct {
		username string
		password string
		email    string
	}{
		{"user1", "pass1", "user1@example.com"},
		{"user2", "pass2", "user2@example.com"},
		{"user3", "pass3", "user3@example.com"},
	}

	for _, u := range users {
		err := authService.Register(context.Background(), u.username, u.password, u.email)
		require.NoError(t, err)
	}

	// Authenticate each user
	for _, u := range users {
		token, err := authService.Authenticate(context.Background(), u.username, u.password)
		require.NoError(t, err)
		require.NotEqual(t, "", token)

		// Verify token
		valid, err := authService.Verify(token)
		require.NoError(t, err)
		require.True(t, valid)
	}
}

func TestServiceIntegration_PasswordManagement(t *testing.T) {
	// Setup
	storage := NewMockServiceStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Register user
	authService.Register(context.Background(), "testuser", "oldpass", "test@example.com")

	// Change password
	err := authService.ChangePassword(context.Background(), "testuser", "oldpass", "newpass")
	require.NoError(t, err)

	// Old password should fail
	_, err = authService.Authenticate(context.Background(), "testuser", "oldpass")
	require.Error(t, err)

	// New password should work
	token, err := authService.Authenticate(context.Background(), "testuser", "newpass")
	require.NoError(t, err)
	require.NotEqual(t, "", token)
}
