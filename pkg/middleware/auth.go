package middleware

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"go.uber.org/zap"
)

// JWTAuthConfig configures the JWT authentication middleware.
// Use Secret for HS256 or PublicKey for RS256.
type JWTAuthConfig struct {
	Secret          string
	PreviousSecrets []string
	PublicKey       *rsa.PublicKey
	SkipPaths       []string
	Blacklist       TokenBlacklistChecker
}

// TokenBlacklistChecker checks if a JWT ID has been revoked.
type TokenBlacklistChecker interface {
	IsTokenRevoked(ctx context.Context, jti string) bool
}

// JWTAuthMiddleware returns a gin middleware that validates JWT tokens
// and injects wallet_address and jwt_claims into the context.
func JWTAuthMiddleware(config JWTAuthConfig, logger *zap.Logger) gin.HandlerFunc {
	secret := []byte(config.Secret)
	prevSecrets := make([][]byte, len(config.PreviousSecrets))
	for i, s := range config.PreviousSecrets {
		prevSecrets[i] = []byte(s)
	}
	skipPaths := make(map[string]bool, len(config.SkipPaths))
	for _, p := range config.SkipPaths {
		skipPaths[p] = true
	}

	return func(c *gin.Context) {
		if skipPaths[c.Request.URL.Path] {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header", "code": "UNAUTHORIZED"})
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization format, expected Bearer token", "code": "UNAUTHORIZED"})
			return
		}

		tokenStr := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if tokenStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "empty bearer token", "code": "UNAUTHORIZED"})
			return
		}

		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			if config.PublicKey != nil {
				if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
				return config.PublicKey, nil
			}
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return secret, nil
		})

		if err != nil && config.PublicKey == nil && len(prevSecrets) > 0 {
			for _, ps := range prevSecrets {
				claims = jwt.MapClaims{}
				token, err = jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
					if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
					}
					return ps, nil
				})
				if err == nil && token.Valid {
					break
				}
			}
		}

		if err != nil {
			logger.Debug("JWT parse failed", zap.Error(err))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token", "code": "UNAUTHORIZED"})
			return
		}

		if !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token is not valid", "code": "UNAUTHORIZED"})
			return
		}

		if config.Blacklist != nil {
			jti, _ := claims["jti"].(string)
			if jti != "" && config.Blacklist.IsTokenRevoked(c.Request.Context(), jti) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token revoked", "code": "TOKEN_REVOKED"})
				return
			}
		}

		walletAddress, _ := claims["wallet_address"].(string)
		if walletAddress == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token missing wallet address", "code": "UNAUTHORIZED"})
			return
		}

		c.Set("wallet_address", walletAddress)
		c.Set("jwt_claims", claims)
		c.Next()
	}
}

// GetWalletAddress extracts the wallet address from the gin context.
// Returns empty string if not set (e.g. auth middleware not applied).
func GetWalletAddress(c *gin.Context) string {
	addr, _ := c.Get("wallet_address")
	if addr == nil {
		return ""
	}
	return addr.(string)
}

// GetJWTClaims extracts the JWT claims from the gin context.
func GetJWTClaims(c *gin.Context) jwt.MapClaims {
	claims, _ := c.Get("jwt_claims")
	if claims == nil {
		return nil
	}
	return claims.(jwt.MapClaims)
}
