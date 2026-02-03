package service

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
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
		return nil, errors.New("user not found")
	}
	userCopy := *user
	return &userCopy, nil
}

func (m *MockAuthStorage) CreateUser(user *User) error {
	if _, exists := m.users[user.Username]; exists {
		return errors.New("user already exists")
	}
	m.users[user.Username] = user
	return nil
}

func (m *MockAuthStorage) UpdateUser(user *User) error {
	if _, exists := m.users[user.Username]; !exists {
		return errors.New("user not found")
	}
	m.users[user.Username] = user
	return nil
}

func TestNewAuthService(t *testing.T) {
	storage := NewMockAuthStorage()
	auth := NewAuthService("test-secret", storage)

	assert.NotNil(t, auth)
	assert.NotNil(t, auth.jwtSecret)
	assert.NotNil(t, auth.storage)
}

func TestAuthService_Authenticate(t *testing.T) {
	storage := NewMockAuthStorage()
	auth := NewAuthService("test-secret", storage)

	t.Run("successful authentication", func(t *testing.T) {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		user := &User{
			ID:        "1",
			Username:  "testuser",
			Password:  string(hashedPassword),
			Email:     "test@example.com",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err = storage.CreateUser(user)
		require.NoError(t, err)

		token, err := auth.Authenticate("testuser", "password123")
		require.NoError(t, err)
		assert.NotEmpty(t, token)
	})

	t.Run("invalid username", func(t *testing.T) {
		_, err := auth.Authenticate("nonexistent", "password")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid username or password")
	})

	t.Run("invalid password", func(t *testing.T) {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		user := &User{
			ID:        "1",
			Username:  "testuser2",
			Password:  string(hashedPassword),
			Email:     "test@example.com",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err = storage.CreateUser(user)
		require.NoError(t, err)

		_, err = auth.Authenticate("testuser2", "wrongpassword")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid username or password")
	})
}

func TestAuthService_AuthenticateWithWallet(t *testing.T) {
	storage := NewMockAuthStorage()
	auth := NewAuthService("test-secret", storage)

	t.Run("wallet authentication", func(t *testing.T) {
		walletAddress := "0x1234567890123456789012345678901234567890"
		signature := "0xabc123"
		message := "test message"

		token, err := auth.AuthenticateWithWallet(walletAddress, signature, message)
		require.NoError(t, err)
		assert.NotEmpty(t, token)

		claims, err := auth.ParseToken(token)
		require.NoError(t, err)
		assert.Equal(t, walletAddress, claims.WalletAddress)
	})
}

func TestAuthService_Verify(t *testing.T) {
	storage := NewMockAuthStorage()
	auth := NewAuthService("test-secret", storage)

	t.Run("verify valid token", func(t *testing.T) {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		user := &User{
			ID:        "1",
			Username:  "testuser",
			Password:  string(hashedPassword),
			Email:     "test@example.com",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err = storage.CreateUser(user)
		require.NoError(t, err)

		token, err := auth.Authenticate("testuser", "password123")
		require.NoError(t, err)

		valid, err := auth.Verify(token)
		require.NoError(t, err)
		assert.True(t, valid)
	})

	t.Run("verify invalid token", func(t *testing.T) {
		valid, err := auth.Verify("invalid.token.here")
		assert.Error(t, err)
		assert.False(t, valid)
	})
}

func TestAuthService_ParseToken(t *testing.T) {
	storage := NewMockAuthStorage()
	auth := NewAuthService("test-secret", storage)

	t.Run("parse valid token", func(t *testing.T) {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		user := &User{
			ID:        "1",
			Username:  "testuser",
			Password:  string(hashedPassword),
			Email:     "test@example.com",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err = storage.CreateUser(user)
		require.NoError(t, err)

		token, err := auth.Authenticate("testuser", "password123")
		require.NoError(t, err)

		claims, err := auth.ParseToken(token)
		require.NoError(t, err)
		assert.Equal(t, "testuser", claims.Username)
		assert.Equal(t, user.ID, claims.Subject)
	})

	t.Run("parse invalid token", func(t *testing.T) {
		_, err := auth.ParseToken("invalid.token.here")
		assert.Error(t, err)
	})
}

func TestAuthService_Register(t *testing.T) {
	storage := NewMockAuthStorage()
	auth := NewAuthService("test-secret", storage)

	t.Run("successful registration", func(t *testing.T) {
		err := auth.Register("newuser", "password123", "new@example.com")
		require.NoError(t, err)

		user, err := storage.GetUser("newuser")
		require.NoError(t, err)
		assert.Equal(t, "newuser", user.Username)
		assert.Equal(t, "new@example.com", user.Email)
		assert.NotEmpty(t, user.Password)
		assert.NotEqual(t, "password123", user.Password)
	})

	t.Run("duplicate registration", func(t *testing.T) {
		err := auth.Register("duplicateuser", "password123", "new@example.com")
		require.NoError(t, err)

		err = auth.Register("duplicateuser", "password456", "another@example.com")
		assert.Error(t, err)
	})
}

func TestAuthService_ChangePassword(t *testing.T) {
	storage := NewMockAuthStorage()
	auth := NewAuthService("test-secret", storage)

	t.Run("successful password change", func(t *testing.T) {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		user := &User{
			ID:        "1",
			Username:  "testuser",
			Password:  string(hashedPassword),
			Email:     "test@example.com",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err = storage.CreateUser(user)
		require.NoError(t, err)

		err = auth.ChangePassword("testuser", "password123", "newpassword456")
		require.NoError(t, err)

		updatedUser, err := storage.GetUser("testuser")
		require.NoError(t, err)
		assert.NotEqual(t, user.Password, updatedUser.Password)
	})

	t.Run("user not found", func(t *testing.T) {
		err := auth.ChangePassword("nonexistent", "old", "new")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user not found")
	})

	t.Run("invalid old password", func(t *testing.T) {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		user := &User{
			ID:        "1",
			Username:  "testuser2",
			Password:  string(hashedPassword),
			Email:     "test@example.com",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err = storage.CreateUser(user)
		require.NoError(t, err)

		err = auth.ChangePassword("testuser2", "wrongpassword", "newpassword")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid old password")
	})
}

func TestAuthService_RefreshToken(t *testing.T) {
	storage := NewMockAuthStorage()
	auth := NewAuthService("test-secret", storage)

	t.Run("successful token refresh", func(t *testing.T) {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		user := &User{
			ID:        "1",
			Username:  "testuser",
			Password:  string(hashedPassword),
			Email:     "test@example.com",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err = storage.CreateUser(user)
		require.NoError(t, err)

		oldToken, err := auth.Authenticate("testuser", "password123")
		require.NoError(t, err)

		newToken, err := auth.RefreshToken(oldToken)
		require.NoError(t, err)
		assert.NotEmpty(t, newToken)
		assert.NotEqual(t, oldToken, newToken)

		oldClaims, _ := auth.ParseToken(oldToken)
		newClaims, err := auth.ParseToken(newToken)
		require.NoError(t, err)
		assert.Equal(t, oldClaims.Username, newClaims.Username)
	})

	t.Run("refresh invalid token", func(t *testing.T) {
		_, err := auth.RefreshToken("invalid.token.here")
		assert.Error(t, err)
	})
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
}
