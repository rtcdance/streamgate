package auth_test

import (
	"errors"
	"testing"

	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

// MockAuthStorage implements service.AuthStorage for testing
type MockAuthStorage struct {
	users map[string]*service.User
}

func NewMockAuthStorage() *MockAuthStorage {
	return &MockAuthStorage{
		users: make(map[string]*service.User),
	}
}

func (m *MockAuthStorage) GetUser(username string) (*service.User, error) {
	user, exists := m.users[username]
	if !exists {
		return nil, nil
	}
	return user, nil
}

func (m *MockAuthStorage) CreateUser(user *service.User) error {
	if _, exists := m.users[user.Username]; exists {
		return errors.New("user already exists")
	}
	m.users[user.Username] = user
	return nil
}

func (m *MockAuthStorage) UpdateUser(user *service.User) error {
	m.users[user.Username] = user
	return nil
}

func TestAuthService_RegisterAndLogin(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-key", storage)

	// Test registration
	err := authService.Register("testuser", "password123", "test@example.com")
	helpers.AssertNoError(t, err)

	// Verify user was created
	user, err := storage.GetUser("testuser")
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, user)
	helpers.AssertEqual(t, "testuser", user.Username)

	// Test login
	token, err := authService.Authenticate("testuser", "password123")
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, token)
}

func TestAuthService_InvalidPassword(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-key", storage)

	// Register user
	err := authService.Register("testuser", "password123", "test@example.com")
	helpers.AssertNoError(t, err)

	// Try login with wrong password
	_, err = authService.Authenticate("testuser", "wrongpassword")
	helpers.AssertError(t, err)
}

func TestAuthService_DuplicateEmail(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-key", storage)

	// Register first user
	err := authService.Register("user1", "password123", "test@example.com")
	helpers.AssertNoError(t, err)

	// Try register with same username (username is unique, not email)
	err = authService.Register("user1", "password456", "test2@example.com")
	helpers.AssertError(t, err)
}

func TestAuthService_TokenValidation(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-key", storage)

	// Register and login
	err := authService.Register("testuser", "password123", "test@example.com")
	helpers.AssertNoError(t, err)

	token, err := authService.Authenticate("testuser", "password123")
	helpers.AssertNoError(t, err)

	// Validate token
	valid, err := authService.Verify(token)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, valid)

	// Parse token
	claims, err := authService.ParseToken(token)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, claims)
	helpers.AssertEqual(t, "testuser", claims.Username)
}

func TestAuthService_RefreshToken(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-key", storage)

	// Register and login
	err := authService.Register("testuser", "password123", "test@example.com")
	helpers.AssertNoError(t, err)

	token, err := authService.Authenticate("testuser", "password123")
	helpers.AssertNoError(t, err)

	// Wait a bit to ensure different timestamp
	// Refresh token
	newToken, err := authService.RefreshToken(token)
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, newToken)
	
	// Verify the new token is valid
	valid, err := authService.Verify(newToken)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, valid)
}
