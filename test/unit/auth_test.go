package unit_test

import (
	"testing"

	"streamgate/test/helpers"
)

// MockAuthStorage implements AuthStorage for testing
type MockAuthStorage struct {
	users map[string]*User
}

func NewMockAuthStorage() *MockAuthStorage {
	return &MockAuthStorage{
		users: make(map[string]*User),
	}
}

func (m *MockAuthStorage) GetUser(username string) (*User, error) {
	user, exists := m.users[username]
	if !exists {
		return nil, nil
	}
	return user, nil
}

func (m *MockAuthStorage) CreateUser(user *User) error {
	m.users[user.Username] = user
	return nil
}

func (m *MockAuthStorage) UpdateUser(user *User) error {
	m.users[user.Username] = user
	return nil
}

func TestAuthService_Register(t *testing.T) {
	storage := NewMockAuthStorage()
	authService := NewAuthService("test-secret", storage)

	// Register user
	err := authService.Register("testuser", "password123", "test@example.com")
	helpers.AssertNoError(t, err)

	// Verify user was created
	user, err := storage.GetUser("testuser")
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, user)
	helpers.AssertEqual(t, "testuser", user.Username)
	helpers.AssertEqual(t, "test@example.com", user.Email)
}

func TestAuthService_Authenticate(t *testing.T) {
	storage := NewMockAuthStorage()
	authService := NewAuthService("test-secret", storage)

	// Register user
	err := authService.Register("testuser", "password123", "test@example.com")
	helpers.AssertNoError(t, err)

	// Authenticate with correct password
	token, err := authService.Authenticate("testuser", "password123")
	helpers.AssertNoError(t, err)
	helpers.AssertNotEqual(t, "", token)

	// Authenticate with wrong password
	_, err = authService.Authenticate("testuser", "wrongpassword")
	helpers.AssertError(t, err)
}

func TestAuthService_Verify(t *testing.T) {
	storage := NewMockAuthStorage()
	authService := NewAuthService("test-secret", storage)

	// Register and authenticate
	authService.Register("testuser", "password123", "test@example.com")
	token, err := authService.Authenticate("testuser", "password123")
	helpers.AssertNoError(t, err)

	// Verify token
	valid, err := authService.Verify(token)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, valid)

	// Verify invalid token
	valid, err = authService.Verify("invalid-token")
	helpers.AssertError(t, err)
	helpers.AssertFalse(t, valid)
}

func TestAuthService_ParseToken(t *testing.T) {
	storage := NewMockAuthStorage()
	authService := NewAuthService("test-secret", storage)

	// Register and authenticate
	authService.Register("testuser", "password123", "test@example.com")
	token, err := authService.Authenticate("testuser", "password123")
	helpers.AssertNoError(t, err)

	// Parse token
	claims, err := authService.ParseToken(token)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, claims)
	helpers.AssertEqual(t, "testuser", claims.Username)
}

func TestAuthService_RefreshToken(t *testing.T) {
	storage := NewMockAuthStorage()
	authService := NewAuthService("test-secret", storage)

	// Register and authenticate
	authService.Register("testuser", "password123", "test@example.com")
	token, err := authService.Authenticate("testuser", "password123")
	helpers.AssertNoError(t, err)

	// Refresh token
	newToken, err := authService.RefreshToken(token)
	helpers.AssertNoError(t, err)
	helpers.AssertNotEqual(t, "", newToken)
	helpers.AssertNotEqual(t, token, newToken)

	// Verify new token
	valid, err := authService.Verify(newToken)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, valid)
}

func TestAuthService_ChangePassword(t *testing.T) {
	storage := NewMockAuthStorage()
	authService := NewAuthService("test-secret", storage)

	// Register user
	authService.Register("testuser", "oldpassword", "test@example.com")

	// Change password
	err := authService.ChangePassword("testuser", "oldpassword", "newpassword")
	helpers.AssertNoError(t, err)

	// Authenticate with new password
	token, err := authService.Authenticate("testuser", "newpassword")
	helpers.AssertNoError(t, err)
	helpers.AssertNotEqual(t, "", token)

	// Old password should not work
	_, err = authService.Authenticate("testuser", "oldpassword")
	helpers.AssertError(t, err)
}
