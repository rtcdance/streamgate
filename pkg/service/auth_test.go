package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/models"
	stg "github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang-jwt/jwt/v4"
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

		token, err := auth.AuthenticateWithWallet(context.Background(), walletAddress, challenge.ID, signature, 11155111)
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

		_, err = auth.AuthenticateWithWallet(context.Background(), walletAddress, challenge.ID, signature, 11155111)
		require.NoError(t, err)

		_, err = auth.AuthenticateWithWallet(context.Background(), walletAddress, challenge.ID, signature, 11155111)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "challenge already used")
	})

	t.Run("concurrent challenge replay only one succeeds", func(t *testing.T) {
		privateKey, err := crypto.GenerateKey()
		require.NoError(t, err)

		walletAddress := verifier.GetAddressFromPrivateKey(privateKey)
		challenge, err := auth.GenerateWalletChallenge(context.Background(), walletAddress, 11155111)
		require.NoError(t, err)

		signature, err := verifier.SignMessage(challenge.Message, privateKey)
		require.NoError(t, err)

		const concurrency = 10
		var wg sync.WaitGroup
		results := make(chan error, concurrency)

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := auth.AuthenticateWithWallet(context.Background(), walletAddress, challenge.ID, signature, 11155111)
				results <- err
			}()
		}
		wg.Wait()
		close(results)

		successCount := 0
		for err := range results {
			if err == nil {
				successCount++
			}
		}
		assert.Equal(t, 1, successCount, "exactly one concurrent request should succeed")
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

		_, err = auth.AuthenticateWithWallet(context.Background(), walletAddress, expiredChallenge.ID, signature, 11155111)
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
		"fp-abc123",
	)
	require.NoError(t, err)

	claims, err := auth.ValidatePlaybackToken(context.Background(), token, "content-1", "fp-abc123")
	require.NoError(t, err)
	assert.Equal(t, "content-1", claims.ContentID)
	assert.Equal(t, "7", claims.TokenID)
	assert.Equal(t, int64(11155111), claims.ChainID)
	assert.Equal(t, "fp-abc123", claims.ClientFingerprint)

	_, err = auth.ValidatePlaybackToken(context.Background(), token, "other-content", "fp-abc123")
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

func TestJWTVerifier_ParseToken_HS256(t *testing.T) {
	secret := "test-secret-that-is-at-least-32-chars"
	auth := NewAuthService(secret, NewMockAuthStorage())

	t.Run("verify token issued by AuthService", func(t *testing.T) {
		token, err := auth.Authenticate(context.Background(), "testuser", "password123")
		if err != nil {
			hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
			auth.storage.CreateUser(context.Background(), &models.User{
				ID:        "1",
				Username:  "testuser",
				Password:  string(hashedPassword),
				Email:     "test@example.com",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			})
			token, err = auth.Authenticate(context.Background(), "testuser", "password123")
		}
		require.NoError(t, err)

		verifier := NewJWTVerifier(secret)
		claims, err := verifier.ParseToken(token)
		require.NoError(t, err)
		assert.Equal(t, "testuser", claims.Username)
	})

	t.Run("invalid token", func(t *testing.T) {
		verifier := NewJWTVerifier(secret)
		_, err := verifier.ParseToken("invalid.token.here")
		assert.Error(t, err)
	})

	t.Run("wrong secret", func(t *testing.T) {
		token, _ := auth.signToken(&Claims{
			Username: "test",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
		})
		verifier := NewJWTVerifier("wrong-secret-that-is-at-least-32-chars")
		_, err := verifier.ParseToken(token)
		assert.Error(t, err)
	})
}

func TestJWTVerifier_WithRSAPublicKey(t *testing.T) {
	t.Run("RS256 verification", func(t *testing.T) {
		privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
			AuthServiceOption(WithRSASigning(privateKey)),
		)

		token, err := auth.signToken(&Claims{
			Username: "rs256user",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				NotBefore: jwt.NewNumericDate(time.Now()),
			},
		})
		require.NoError(t, err)

		verifier := NewJWTVerifier("unused", WithRSAPublicKey(&privateKey.PublicKey))
		claims, err := verifier.ParseToken(token)
		require.NoError(t, err)
		assert.Equal(t, "rs256user", claims.Username)
	})

	t.Run("RS256 wrong key", func(t *testing.T) {
		privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
		wrongKey, _ := rsa.GenerateKey(rand.Reader, 2048)

		auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
			AuthServiceOption(WithRSASigning(privateKey)),
		)

		token, err := auth.signToken(&Claims{
			Username: "rs256user",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				NotBefore: jwt.NewNumericDate(time.Now()),
			},
		})
		require.NoError(t, err)

		verifier := NewJWTVerifier("unused", WithRSAPublicKey(&wrongKey.PublicKey))
		_, err = verifier.ParseToken(token)
		assert.Error(t, err)
	})
}

func TestAuthService_WithRSASigning(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage(),
		AuthServiceOption(WithRSASigning(privateKey)),
	)
	assert.Equal(t, JWTRS256, auth.signingType)
	assert.NotNil(t, auth.privateKey)
	assert.NotNil(t, auth.publicKey)
}

func TestAuthService_Close(t *testing.T) {
	t.Run("close without blacklist", func(t *testing.T) {
		auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())
		assert.NotPanics(t, func() {
			auth.Close()
		})
	})
}

func TestAuthService_ParseToken_Expired(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	claims := &Claims{
		Username: "expired",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	_, err = auth.ParseToken(token)
	assert.Error(t, err)
}

func TestAuthService_ParseToken_NotYetValid(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	claims := &Claims{
		Username: "future",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	_, err = auth.ParseToken(token)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet valid")
}

func TestAuthService_RefreshToken_GracePeriod(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	claims := &Claims{
		Username: "grace",
		JTI:      generateID(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-2 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	newToken, err := auth.RefreshToken(context.Background(), token)
	require.NoError(t, err)
	assert.NotEmpty(t, newToken)
	assert.NotEqual(t, token, newToken)
}

func TestAuthService_RefreshToken_ExpiredBeyondGrace(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	claims := &Claims{
		Username: "too-old",
		JTI:      generateID(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-10 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	_, err = auth.RefreshToken(context.Background(), token)
	assert.Error(t, err)
}

func TestAuthService_RefreshToken_MissingJTI(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	claims := &Claims{
		Username: "no-jti",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := auth.signToken(claims)
	require.NoError(t, err)

	_, err = auth.RefreshToken(context.Background(), token)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing jti")
}

func TestAuthService_VerifyToken(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	t.Run("valid token", func(t *testing.T) {
		claims := &Claims{
			WalletAddress: "verify-user",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				NotBefore: jwt.NewNumericDate(time.Now()),
			},
		}
		token, err := auth.signToken(claims)
		require.NoError(t, err)

		result, err := auth.VerifyToken(context.Background(), token)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.Equal(t, "verify-user", result.WalletAddress)
	})

	t.Run("expired token", func(t *testing.T) {
		claims := &Claims{
			Username: "expired",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
				IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Hour)),
				NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			},
		}
		token, err := auth.signToken(claims)
		require.NoError(t, err)

		result, err := auth.VerifyToken(context.Background(), token)
		assert.Error(t, err)
		assert.False(t, result.Valid)
	})

	t.Run("invalid token", func(t *testing.T) {
		result, err := auth.VerifyToken(context.Background(), "invalid.token")
		assert.Error(t, err)
		assert.False(t, result.Valid)
	})
}

func TestAuthService_RevokeToken(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	t.Run("invalid token is silently accepted", func(t *testing.T) {
		err := auth.RevokeToken(context.Background(), "invalid.token")
		assert.NoError(t, err)
	})

	t.Run("valid token without blacklist", func(t *testing.T) {
		claims := &Claims{
			Username: "revoke-user",
			JTI:      "jti-123",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				NotBefore: jwt.NewNumericDate(time.Now()),
			},
		}
		token, err := auth.signToken(claims)
		require.NoError(t, err)

		err = auth.RevokeToken(context.Background(), token)
		assert.NoError(t, err)
	})
}

func TestAuthService_IsTokenRevoked_AuthTest(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	t.Run("no blacklist", func(t *testing.T) {
		assert.False(t, auth.IsTokenRevoked(context.Background(), "jti"))
	})

	t.Run("empty jti", func(t *testing.T) {
		assert.False(t, auth.IsTokenRevoked(context.Background(), ""))
	})
}

func TestAuthService_GeneratePlaybackToken_AuthTest(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	t.Run("default TTL", func(t *testing.T) {
		token, err := auth.GeneratePlaybackToken(
			context.Background(),
			"0x1234567890123456789012345678901234567890",
			"content-1",
			"0xcontract",
			"7",
			1,
			0,
			"fp-abc",
		)
		require.NoError(t, err)
		assert.NotEmpty(t, token)
	})

	t.Run("custom TTL", func(t *testing.T) {
		token, err := auth.GeneratePlaybackToken(
			context.Background(),
			"0x1234567890123456789012345678901234567890",
			"content-1",
			"0xcontract",
			"7",
			1,
			5*time.Minute,
			"fp-abc",
		)
		require.NoError(t, err)
		assert.NotEmpty(t, token)
	})
}

func TestAuthService_ValidatePlaybackToken_FingerprintMismatch(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	token, err := auth.GeneratePlaybackToken(
		context.Background(),
		"0x1234567890123456789012345678901234567890",
		"content-1",
		"0xcontract",
		"7",
		1,
		time.Minute,
		"fp-correct",
	)
	require.NoError(t, err)

	_, err = auth.ValidatePlaybackToken(context.Background(), token, "content-1", "fp-wrong")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fingerprint mismatch")
}

func TestAuthService_ValidatePlaybackToken_WalletMismatch(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	token, err := auth.GeneratePlaybackToken(
		context.Background(),
		"0x1234567890123456789012345678901234567890",
		"content-1",
		"0xcontract",
		"7",
		1,
		time.Minute,
		"fp-abc",
	)
	require.NoError(t, err)

	_, err = auth.ValidatePlaybackToken(context.Background(), token, "content-1", "fp-abc", "0xdifferent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "wallet mismatch")
}

func TestIsValidSolanaAddress_AuthPackage(t *testing.T) {
	tests := []struct {
		addr  string
		valid bool
	}{
		{"0x1234567890123456789012345678901234567890", false},
		{"short", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.valid, IsValidSolanaAddress(tt.addr), "IsValidSolanaAddress(%q)", tt.addr)
	}
}

func TestAuthService_GenerateWalletChallenge_InvalidAddress(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	t.Run("invalid EVM address", func(t *testing.T) {
		_, err := auth.GenerateWalletChallenge(context.Background(), "not-an-address", 1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid wallet address")
	})

	t.Run("invalid Solana address", func(t *testing.T) {
		_, err := auth.GenerateWalletChallenge(context.Background(), "short", -1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Solana wallet address")
	})
}

func TestAuthService_AuthenticateWithWallet_MissingParams(t *testing.T) {
	auth := NewAuthService("test-secret-that-is-at-least-32-chars", NewMockAuthStorage())

	t.Run("empty challenge ID", func(t *testing.T) {
		_, err := auth.AuthenticateWithWallet(context.Background(), "0xabc", "", "sig", 1)
		assert.ErrorIs(t, err, ErrInvalidRequest)
	})

	t.Run("empty signature", func(t *testing.T) {
		_, err := auth.AuthenticateWithWallet(context.Background(), "0xabc", "challenge-id", "", 1)
		assert.ErrorIs(t, err, ErrInvalidRequest)
	})
}
