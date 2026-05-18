package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func generateTestJWT(secret, walletAddress string, expiresAt time.Time) string {
	claims := jwt.MapClaims{
		"wallet_address": walletAddress,
		"username":       walletAddress,
		"sub":            walletAddress,
		"exp":            expiresAt.Unix(),
		"iat":            time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, _ := token.SignedString([]byte(secret))
	return s
}

func TestJWTAuthMiddleware_ValidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := JWTAuthConfig{Secret: "test-secret-key-at-least-32-chars!"}
	router.Use(JWTAuthMiddleware(config, zap.NewNop()))
	router.GET("/protected", func(c *gin.Context) {
		wallet := GetWalletAddress(c)
		c.JSON(http.StatusOK, gin.H{"wallet_address": wallet})
	})

	token := generateTestJWT("test-secret-key-at-least-32-chars!", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", time.Now().Add(time.Hour))

	req := httptest.NewRequest("GET", "/protected", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18")
}

func TestJWTAuthMiddleware_MissingHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := JWTAuthConfig{Secret: "test-secret-key-at-least-32-chars!"}
	router.Use(JWTAuthMiddleware(config, zap.NewNop()))
	router.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, nil)
	})

	req := httptest.NewRequest("GET", "/protected", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestJWTAuthMiddleware_InvalidFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := JWTAuthConfig{Secret: "test-secret-key-at-least-32-chars!"}
	router.Use(JWTAuthMiddleware(config, zap.NewNop()))
	router.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, nil)
	})

	req := httptest.NewRequest("GET", "/protected", http.NoBody)
	req.Header.Set("Authorization", "Basic abc123")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestJWTAuthMiddleware_ExpiredToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := JWTAuthConfig{Secret: "test-secret-key-at-least-32-chars!"}
	router.Use(JWTAuthMiddleware(config, zap.NewNop()))
	router.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, nil)
	})

	token := generateTestJWT("test-secret-key-at-least-32-chars!", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", time.Now().Add(-time.Hour))

	req := httptest.NewRequest("GET", "/protected", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestJWTAuthMiddleware_WrongSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := JWTAuthConfig{Secret: "correct-secret-at-least-32-chars!!"}
	router.Use(JWTAuthMiddleware(config, zap.NewNop()))
	router.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, nil)
	})

	token := generateTestJWT("wrong-secret-at-least-32-chars!!", "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", time.Now().Add(time.Hour))

	req := httptest.NewRequest("GET", "/protected", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestJWTAuthMiddleware_MissingWalletAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := JWTAuthConfig{Secret: "test-secret-key-at-least-32-chars!"}
	router.Use(JWTAuthMiddleware(config, zap.NewNop()))
	router.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, nil)
	})

	// Token without wallet_address claim
	claims := jwt.MapClaims{
		"username": "testuser",
		"exp":      time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, _ := token.SignedString([]byte("test-secret-key-at-least-32-chars!"))

	req := httptest.NewRequest("GET", "/protected", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestJWTAuthMiddleware_SkipPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := JWTAuthConfig{
		Secret:    "test-secret-key-at-least-32-chars!",
		SkipPaths: []string{"/api/v1/auth/login", "/api/v1/auth/challenge"},
	}
	router.Use(JWTAuthMiddleware(config, zap.NewNop()))
	router.GET("/api/v1/auth/login", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "login"})
	})
	router.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "protected"})
	})

	// Skip path should work without token
	req := httptest.NewRequest("GET", "/api/v1/auth/login", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Protected path should require token
	req2 := httptest.NewRequest("GET", "/protected", http.NoBody)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusUnauthorized, w2.Code)
}

func TestGetWalletAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("with context value", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("wallet_address", "0xABC")
		assert.Equal(t, "0xABC", GetWalletAddress(c))
	})

	t.Run("without context value", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		assert.Equal(t, "", GetWalletAddress(c))
	})
}

func TestGetJWTClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("with context value", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		claims := jwt.MapClaims{"wallet_address": "0xABC"}
		c.Set("jwt_claims", claims)
		result := GetJWTClaims(c)
		require.NotNil(t, result)
		assert.Equal(t, "0xABC", result["wallet_address"])
	})

	t.Run("without context value", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		assert.Nil(t, GetJWTClaims(c))
	})
}
