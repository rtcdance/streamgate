package service_test

import (
	"testing"

	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

// MockServiceStorage for testing
type MockServiceStorage struct {
	users map[string]*service.User
}

func NewMockServiceStorage() *MockServiceStorage {
	return &MockServiceStorage{
		users: make(map[string]*service.User),
	}
}

func (m *MockServiceStorage) GetUser(username string) (*service.User, error) {
	user, exists := m.users[username]
	if !exists {
		return nil, nil
	}
	return user, nil
}

func (m *MockServiceStorage) CreateUser(user *service.User) error {
	m.users[user.Username] = user
	return nil
}

func (m *MockServiceStorage) UpdateUser(user *service.User) error {
	m.users[user.Username] = user
	return nil
}

func TestServiceIntegration_AuthenticationFlow(t *testing.T) {
	// Setup
	storage := NewMockServiceStorage()
	authService := service.NewAuthService("test-secret", storage)

	// Register user
	err := authService.Register("testuser", "password123", "test@example.com")
	helpers.AssertNoError(t, err)

	// Authenticate
	token, err := authService.Authenticate("testuser", "password123")
	helpers.AssertNoError(t, err)

	// Verify token
	valid, err := authService.Verify(token)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, valid)

	// Parse token
	claims, err := authService.ParseToken(token)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "testuser", claims.Username)
}

func TestServiceIntegration_MultipleUsers(t *testing.T) {
	// Setup
	storage := NewMockServiceStorage()
	authService := service.NewAuthService("test-secret", storage)

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
		err := authService.Register(u.username, u.password, u.email)
		helpers.AssertNoError(t, err)
	}

	// Authenticate each user
	for _, u := range users {
		token, err := authService.Authenticate(u.username, u.password)
		helpers.AssertNoError(t, err)
		helpers.AssertNotEqual(t, "", token)

		// Verify token
		valid, err := authService.Verify(token)
		helpers.AssertNoError(t, err)
		helpers.AssertTrue(t, valid)
	}
}

func TestServiceIntegration_PasswordManagement(t *testing.T) {
	// Setup
	storage := NewMockServiceStorage()
	authService := service.NewAuthService("test-secret", storage)

	// Register user
	authService.Register("testuser", "oldpass", "test@example.com")

	// Change password
	err := authService.ChangePassword("testuser", "oldpass", "newpass")
	helpers.AssertNoError(t, err)

	// Old password should fail
	_, err = authService.Authenticate("testuser", "oldpass")
	helpers.AssertError(t, err)

	// New password should work
	token, err := authService.Authenticate("testuser", "newpass")
	helpers.AssertNoError(t, err)
	helpers.AssertNotEqual(t, "", token)
}
