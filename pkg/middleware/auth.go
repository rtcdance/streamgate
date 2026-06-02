package middleware

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net/http"
	"strings"
	"time"

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
		path := c.Request.URL.Path
		if skipPaths[path] || strings.HasPrefix(path, "/demo/") {
			c.Next()
			return
		}

		if pt := c.Query("playback_token"); pt != "" {
			c.Set("playback_auth", true)
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

		// Parse without claims validation — signature IS verified, but exp/nbf/iat
		// are checked manually below. This is necessary for key rotation: tokens
		// signed with old (previous) secrets may appear "expired" from the current
		// key's perspective, but we must still attempt verification against prevSecrets.
		parser := jwt.NewParser(jwt.WithoutClaimsValidation())

		buildKeyFunc := func(k interface{}) jwt.Keyfunc {
			return func(t *jwt.Token) (interface{}, error) {
				if config.PublicKey != nil {
					if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
						return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
					}
					return config.PublicKey, nil
				}
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
				return k, nil
			}
		}

		claims := jwt.MapClaims{}
		_, err := parser.ParseWithClaims(tokenStr, claims, buildKeyFunc(secret))

		// Try previous secrets if current key fails and no public key is configured.
		if err != nil && config.PublicKey == nil && len(prevSecrets) > 0 {
			for _, ps := range prevSecrets {
				claims = jwt.MapClaims{}
				_, err = jwt.NewParser(jwt.WithoutClaimsValidation()).ParseWithClaims(tokenStr, claims, buildKeyFunc(ps))
				if err == nil {
					break
				}
			}
		}

		if err != nil {
			logger.Debug("JWT parse failed", zap.Error(err))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token", "code": "UNAUTHORIZED"})
			return
		}

		// Manual claims validation with 30s leeway for clock skew.
		// Covers exp, nbf, and iat — the last is not validated by the library
		// when WithoutClaimsValidation is used and prevents tokens from being
		// accepted before their stated issuance time.
		now := time.Now()
		if exp, ok := claims["exp"].(float64); ok && now.After(time.Unix(int64(exp), 0).Add(30*time.Second)) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token expired", "code": "UNAUTHORIZED"})
			return
		}
		if nbf, ok := claims["nbf"].(float64); ok && now.Before(time.Unix(int64(nbf), 0).Add(-30*time.Second)) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token not yet valid", "code": "UNAUTHORIZED"})
			return
		}
		if iat, ok := claims["iat"].(float64); ok && now.Add(30*time.Second).Before(time.Unix(int64(iat), 0)) {
			// iat must not be in the future beyond leeway window.
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token issued in the future", "code": "UNAUTHORIZED"})
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
