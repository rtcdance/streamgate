package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"streamgate/pkg/models"
	"streamgate/pkg/web3"
	stg "streamgate/pkg/storage"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
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

func (m *MockAuthStorage) GetUser(_ context.Context, username string) (*models.User, error) {
	user, exists := m.users[username]
	if !exists {
		return nil, errors.New("user not found")
	}
	userCopy := *user
	return &userCopy, nil
}

func (m *MockAuthStorage) CreateUser(_ context.Context, user *models.User) error {
	if _, exists := m.users[user.Username]; exists {
		return errors.New("user already exists")
	}
	m.users[user.Username] = user
	return nil
}

func (m *MockAuthStorage) UpdateUser(_ context.Context, user *models.User) error {
	if _, exists := m.users[user.Username]; !exists {
		return errors.New("user not found")
	}
	m.users[user.Username] = user
	return nil
}

func TestNewAuthService(t *testing.T) {
	storage := NewMockAuthStorage()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	assert.NotNil(t, auth)
	assert.NotNil(t, auth.jwtSecret)
	assert.NotNil(t, auth.storage)
}

func TestAuthService_Authenticate(t *testing.T) {
	storage := NewMockAuthStorage()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	t.Run("successful authentication", func(t *testing.T) {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		user := &models.User{
			ID:        "1",
			Username:  "testuser",
			Password:  string(hashedPassword),
			Email:     "test@example.com",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err = storage.CreateUser(context.Background(), user)
		require.NoError(t, err)

		token, err := auth.Authenticate(context.Background(), "testuser", "password123")
		require.NoError(t, err)
		assert.NotEmpty(t, token)
	})

	t.Run("invalid username", func(t *testing.T) {
		_, err := auth.Authenticate(context.Background(), "nonexistent", "password")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidCredential)
		assert.NotContains(t, err.Error(), "username")
	})

	t.Run("invalid password", func(t *testing.T) {
		_ = auth.Register(context.Background(), "testuser2", "password123", "test2@example.com")
		_, err := auth.Authenticate(context.Background(), "testuser2", "wrongpassword")
		assert.ErrorIs(t, err, ErrInvalidCredential)
	})
}

func TestAuthService_AuthenticateWithWallet(t *testing.T) {
	storage := NewMockAuthStorage()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", storage)
	verifier := web3.NewSignatureVerifier(zap.NewNop())
	auth.signatureVerifier = verifier
	store, ok := auth.challengeStore.(*stg.MemoryChallengeStore)
	require.True(t, ok)

	t.Run("wallet authentication with challenge", func(t *testing.T) {
		privateKey, err := crypto.GenerateKey()
		require.NoError(t, err)

		walletAddress := verifier.GetAddressFromPrivateKey(privateKey)
		challenge, err := auth.GenerateWalletChallenge(context.Background(), walletAddress, 11155111)
		require.NoError(t, err)
		assert.Equal(t, walletAddress, challenge.WalletAddress)

		signature, err := verifier.SignMessage(challenge.Message, privateKey)
		require.NoError(t, err)

		token, err := auth.AuthenticateWithWallet(context.Background(), walletAddress, challenge.ID, signature)
		require.NoError(t, err)
		assert.NotEmpty(t, token)

		claims, err := auth.ParseToken(token)
		require.NoError(t, err)
		assert.Equal(t, walletAddress, claims.WalletAddress)
	})

	t.Run("challenge replay fails", func(t *testing.T) {
		privateKey, err := crypto.GenerateKey()
		require.NoError(t, err)

		walletAddress := verifier.GetAddressFromPrivateKey(privateKey)
		challenge, err := auth.GenerateWalletChallenge(context.Background(), walletAddress, 11155111)
		require.NoError(t, err)

		signature, err := verifier.SignMessage(challenge.Message, privateKey)
		require.NoError(t, err)

		_, err = auth.AuthenticateWithWallet(context.Background(), walletAddress, challenge.ID, signature)
		require.NoError(t, err)

		_, err = auth.AuthenticateWithWallet(context.Background(), walletAddress, challenge.ID, signature)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "challenge already used")
	})

	t.Run("expired challenge fails", func(t *testing.T) {
		privateKey, err := crypto.GenerateKey()
		require.NoError(t, err)

		walletAddress := verifier.GetAddressFromPrivateKey(privateKey)
		expiredChallenge := &stg.WalletChallenge{
			ID:            "expired-challenge",
			WalletAddress: walletAddress,
			ChainID:       11155111,
			Nonce:         "nonce-expired",
			Message:       "expired message",
			IssuedAt:      time.Now().Add(-10 * time.Minute).UTC(),
			ExpiresAt:     time.Now().Add(-time.Minute).UTC(),
		}
		require.NoError(t, store.SaveChallenge(context.Background(), expiredChallenge))

		signature, err := verifier.SignMessage(expiredChallenge.Message, privateKey)
		require.NoError(t, err)

		_, err = auth.AuthenticateWithWallet(context.Background(), walletAddress, expiredChallenge.ID, signature)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "challenge expired")
	})
}

func TestMemoryChallengeStore_ChallengeLifecycle(t *testing.T) {
	store := stg.NewMemoryChallengeStore()
	challenge := &stg.WalletChallenge{
		ID:            "challenge-1",
		WalletAddress: "0x1234567890123456789012345678901234567890",
		Nonce:         "nonce",
		Message:       "message",
		IssuedAt:      time.Now(),
		ExpiresAt:     time.Now().Add(time.Minute),
	}

	require.NoError(t, store.SaveChallenge(context.Background(), challenge))

	loaded, err := store.GetChallenge(context.Background(), challenge.ID)
	require.NoError(t, err)
	assert.Equal(t, challenge.WalletAddress, loaded.WalletAddress)
	assert.True(t, loaded.UsedAt.IsZero())

	usedAt := time.Now().UTC()
	require.NoError(t, store.MarkChallengeUsed(context.Background(), challenge.ID, usedAt))

	loaded, err = store.GetChallenge(context.Background(), challenge.ID)
	require.NoError(t, err)
	assert.Equal(t, usedAt.Unix(), loaded.UsedAt.Unix())
}

func TestAuthService_PlaybackTokenLifecycle(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	token, err := auth.GeneratePlaybackToken(
		context.Background(),
		"0x1234567890123456789012345678901234567890",
		"content-1",
		"0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f",
		"7",
		11155111,
		time.Minute,
	)
	require.NoError(t, err)

	claims, err := auth.ValidatePlaybackToken(context.Background(), token, "content-1")
	require.NoError(t, err)
	assert.Equal(t, "content-1", claims.ContentID)
	assert.Equal(t, "7", claims.TokenID)
	assert.Equal(t, int64(11155111), claims.ChainID)

	_, err = auth.ValidatePlaybackToken(context.Background(), token, "other-content")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content mismatch")
}

func TestAuthService_Verify(t *testing.T) {
	storage := NewMockAuthStorage()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	t.Run("verify valid token", func(t *testing.T) {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		user := &models.User{
			ID:        "1",
			Username:  "testuser",
			Password:  string(hashedPassword),
			Email:     "test@example.com",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err = storage.CreateUser(context.Background(), user)
		require.NoError(t, err)

		token, err := auth.Authenticate(context.Background(), "testuser", "password123")
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
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	t.Run("parse valid token", func(t *testing.T) {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		user := &models.User{
			ID:        "1",
			Username:  "testuser",
			Password:  string(hashedPassword),
			Email:     "test@example.com",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err = storage.CreateUser(context.Background(), user)
		require.NoError(t, err)

		token, err := auth.Authenticate(context.Background(), "testuser", "password123")
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
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	t.Run("successful registration", func(t *testing.T) {
		err := auth.Register(context.Background(), "newuser", "password123", "new@example.com")
		require.NoError(t, err)

		user, err := storage.GetUser(context.Background(), "newuser")
		require.NoError(t, err)
		assert.Equal(t, "newuser", user.Username)
		assert.Equal(t, "new@example.com", user.Email)
		assert.NotEmpty(t, user.Password)
		assert.NotEqual(t, "password123", user.Password)
	})

	t.Run("duplicate registration", func(t *testing.T) {
		err := auth.Register(context.Background(), "duplicateuser", "password123", "new@example.com")
		require.NoError(t, err)

		err = auth.Register(context.Background(), "duplicateuser", "password456", "another@example.com")
		assert.Error(t, err)
	})
}

func TestAuthService_ChangePassword(t *testing.T) {
	storage := NewMockAuthStorage()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	t.Run("successful password change", func(t *testing.T) {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		user := &models.User{
			ID:        "1",
			Username:  "testuser",
			Password:  string(hashedPassword),
			Email:     "test@example.com",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err = storage.CreateUser(context.Background(), user)
		require.NoError(t, err)

		err = auth.ChangePassword(context.Background(), "testuser", "password123", "newpassword456")
		require.NoError(t, err)

		updatedUser, err := storage.GetUser(context.Background(), "testuser")
		require.NoError(t, err)
		assert.NotEqual(t, user.Password, updatedUser.Password)
	})

	t.Run("user not found", func(t *testing.T) {
		err := auth.ChangePassword(context.Background(), "nonexistent", "old", "new")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user not found")
	})

	t.Run("invalid old password", func(t *testing.T) {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		user := &models.User{
			ID:        "1",
			Username:  "testuser2",
			Password:  string(hashedPassword),
			Email:     "test@example.com",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err = storage.CreateUser(context.Background(), user)
		require.NoError(t, err)

		err = auth.ChangePassword(context.Background(), "testuser2", "wrongpassword", "newpassword")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidCredential)
	})
}

func TestAuthService_RefreshToken(t *testing.T) {
	storage := NewMockAuthStorage()
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	t.Run("successful token refresh", func(t *testing.T) {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		require.NoError(t, err)

		user := &models.User{
			ID:        "1",
			Username:  "testuser",
			Password:  string(hashedPassword),
			Email:     "test@example.com",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err = storage.CreateUser(context.Background(), user)
		require.NoError(t, err)

		oldToken, err := auth.Authenticate(context.Background(), "testuser", "password123")
		require.NoError(t, err)

		newToken, err := auth.RefreshToken(context.Background(), oldToken)
		require.NoError(t, err)
		assert.NotEmpty(t, newToken)
		assert.NotEqual(t, oldToken, newToken)

		oldClaims, _ := auth.ParseToken(oldToken)
		newClaims, err := auth.ParseToken(newToken)
		require.NoError(t, err)
		assert.Equal(t, oldClaims.Username, newClaims.Username)
	})

	t.Run("refresh invalid token", func(t *testing.T) {
		_, err := auth.RefreshToken(context.Background(), "invalid.token.here")
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
