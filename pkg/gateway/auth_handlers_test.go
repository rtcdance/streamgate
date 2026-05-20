package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"streamgate/pkg/core/config"
	"streamgate/pkg/middleware"
	"streamgate/pkg/models"
	"streamgate/pkg/service"
	"streamgate/pkg/storage"
)

// mockAuthStorage implements service.AuthStorage for gateway tests
type mockAuthStorage struct {
	users map[string]*models.User
}

func newMockAuthStorage() *mockAuthStorage {
	return &mockAuthStorage{users: make(map[string]*models.User)}
}

func (m *mockAuthStorage) GetUser(_ context.Context, username string) (*models.User, error) {
	u, ok := m.users[username]
	if !ok {
		return nil, errors.New("user not found")
	}
	cp := *u
	return &cp, nil
}

func (m *mockAuthStorage) CreateUser(_ context.Context, user *models.User) error {
	if _, ok := m.users[user.Username]; ok {
		return errors.New("user already exists")
	}
	m.users[user.Username] = user
	return nil
}

func (m *mockAuthStorage) UpdateUser(_ context.Context, user *models.User) error {
	m.users[user.Username] = user
	return nil
}

func setupAuthRouter() (*gin.Engine, *service.AuthService) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	sigVerifier := service.NewMultiChainSignatureVerifier(zap.NewNop(), nil)
	authService := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-testing-",
		newMockAuthStorage(),
		sigVerifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)

	cfg := config.DefaultConfig()
	authRL := middleware.NewRateLimiter(middleware.RateLimitConfig{
		RequestsPerMinute: 10,
		WindowSize:        time.Minute,
		CleanupInterval:   5 * time.Minute,
	}, nil)
	RegisterAuthRoutes(r, zap.NewNop(), cfg, authService, authRL)
	return r, authService
}

func TestAuthHandlers_Challenge(t *testing.T) {
	r, _ := setupAuthRouter()

	t.Run("missing body", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/challenge", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("EVM challenge", func(t *testing.T) {
		privateKey, err := crypto.GenerateKey()
		require.NoError(t, err)
		address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()

		body, _ := json.Marshal(map[string]interface{}{
			"wallet":   address,
			"chain_id": 11155111,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/challenge", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NotEmpty(t, resp["challenge_id"])
		assert.Equal(t, address, resp["wallet"])
		assert.Equal(t, float64(11155111), resp["chain_id"])
	})

	t.Run("Solana challenge", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"wallet":   "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
			"chain_id": -1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/challenge", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", resp["wallet"])
		assert.Equal(t, float64(-1), resp["chain_id"])
	})

	t.Run("invalid Solana address on Solana chain", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"wallet":   "0xbadaddress",
			"chain_id": -1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/challenge", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestAuthHandlers_Register(t *testing.T) {
	r, _ := setupAuthRouter()

	t.Run("successful registration", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"username": "testuser",
			"password": "password123",
			"email":    "test@example.com",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("missing fields", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"username": "no"})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("duplicate user", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"username": "duplicate",
			"password": "password123",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)

		w2 := httptest.NewRecorder()
		req2, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
		req2.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusConflict, w2.Code)
	})
}

func TestAuthHandlers_Logout(t *testing.T) {
	r, authService := setupAuthRouter()

	t.Run("missing token", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/logout", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("with valid token", func(t *testing.T) {
		_ = authService.Register(context.Background(), "logoutuser", "pass123", "logout@test.com")
		token, err := authService.Authenticate(context.Background(), "logoutuser", "pass123")
		require.NoError(t, err)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/logout", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestAuthHandlers_Verify(t *testing.T) {
	r, authService := setupAuthRouter()

	t.Run("missing token", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/verify", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("valid token", func(t *testing.T) {
		_ = authService.Register(context.Background(), "verifyuser", "pass123", "verify@test.com")
		token, err := authService.Authenticate(context.Background(), "verifyuser", "pass123")
		require.NoError(t, err)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/verify", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, true, resp["valid"])
	})
}

func TestAuthHandlers_Refresh(t *testing.T) {
	r, authService := setupAuthRouter()

	t.Run("missing body", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/refresh", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("empty token", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"token": ""})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("valid token refresh", func(t *testing.T) {
		_ = authService.Register(context.Background(), "refreshuser", "pass123", "refresh@test.com")
		token, err := authService.Authenticate(context.Background(), "refreshuser", "pass123")
		require.NoError(t, err)

		body, _ := json.Marshal(map[string]string{"token": token})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NotEmpty(t, resp["token"])
		assert.NotEqual(t, token, resp["token"])
	})
}

func TestAuthHandlers_Profile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	sigVerifier := service.NewMultiChainSignatureVerifier(zap.NewNop(), nil)
	authService := service.NewAuthServiceWithDeps(
		"test-jwt-secret-key-for-testing-",
		newMockAuthStorage(),
		sigVerifier,
		storage.NewMemoryChallengeStore(),
		5*time.Minute,
		storage.NewMemoryTokenBlacklist(),
	)

	protected := r.Group("/")
	protected.Use(func(c *gin.Context) {
		c.Set("wallet_address", "0xTestWallet")
		c.Next()
	})
	RegisterAuthProtectedRoutes(protected, zap.NewNop(), authService)

	t.Run("profile returns wallet address", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/auth/profile", http.NoBody)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "0xTestWallet", resp["wallet_address"])
	})
}
