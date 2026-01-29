package e2e_test

import (
	"testing"

	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

// MockAuthStorage for testing
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
	m.users[user.Username] = user
	return nil
}

func (m *MockAuthStorage) UpdateUser(user *service.User) error {
	m.users[user.Username] = user
	return nil
}

func TestAuthFlow_UserRegistrationAndLogin(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret", storage)

	// Step 1: Register user
	err := authService.Register("testuser", "password123", "test@example.com")
	helpers.AssertNoError(t, err)

	// Step 2: Verify user was created
	user, err := storage.GetUser("testuser")
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, user)
	helpers.AssertEqual(t, "testuser", user.Username)

	// Step 3: Login with correct credentials
	token, err := authService.Authenticate("testuser", "password123")
	helpers.AssertNoError(t, err)
	helpers.AssertNotEqual(t, "", token)

	// Step 4: Verify token
	valid, err := authService.Verify(token)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, valid)

	// Step 5: Parse token to get claims
	claims, err := authService.ParseToken(token)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "testuser", claims.Username)
}

func TestAuthFlow_FailedLogin(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret", storage)

	// Register user
	authService.Register("testuser", "password123", "test@example.com")

	// Try login with wrong password
	_, err := authService.Authenticate("testuser", "wrongpassword")
	helpers.AssertError(t, err)

	// Try login with non-existent user
	_, err = authService.Authenticate("nonexistent", "password123")
	helpers.AssertError(t, err)
}

func TestAuthFlow_TokenRefresh(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret", storage)

	// Register and login
	authService.Register("testuser", "password123", "test@example.com")
	token, err := authService.Authenticate("testuser", "password123")
	helpers.AssertNoError(t, err)

	// Refresh token
	newToken, err := authService.RefreshToken(token)
	helpers.AssertNoError(t, err)
	helpers.AssertNotEqual(t, token, newToken)

	// Verify new token
	valid, err := authService.Verify(newToken)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, valid)
}

func TestAuthFlow_PasswordChange(t *testing.T) {
	// Setup
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret", storage)

	// Register user
	authService.Register("testuser", "oldpassword", "test@example.com")

	// Change password
	err := authService.ChangePassword("testuser", "oldpassword", "newpassword")
	helpers.AssertNoError(t, err)

	// Old password should not work
	_, err = authService.Authenticate("testuser", "oldpassword")
	helpers.AssertError(t, err)

	// New password should work
	token, err := authService.Authenticate("testuser", "newpassword")
	helpers.AssertNoError(t, err)
	helpers.AssertNotEqual(t, "", token)
}
