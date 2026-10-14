package middleware

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"go.uber.org/zap"
)

// TokenKeyID returns the short identifier this service stamps in a token's
// `kid` header: the first 8 hex characters of the key's SHA-256. It is a HINT
// used to try the most likely verification key first — never a source of
// trust, because the header is attacker-controlled and every allowed key is
// still tried when the hint does not match.
func TokenKeyID(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:4])
}

// tokenKeyIDHint peeks at a compact JWT's header WITHOUT verifying it, to read
// the kid hint. The result is only ever used to ORDER candidate keys.
func tokenKeyIDHint(tokenString string) string {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ""
	}
	var header struct {
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return ""
	}
	return header.Kid
}

// OrderedKeyCandidates orders the keys a token may verify against: the key
// whose kid matches the token's header hint goes first (rotation fast path),
// then the current key, then the previous ones in configuration order —
// deduplicated, so a hint cannot cause the same key to be tried twice.
// The kid is a HINT, not trust: every allowed key is always tried.
func OrderedKeyCandidates(tokenString, current string, previous []string) []string {
	hint := tokenKeyIDHint(tokenString)
	seen := make(map[string]bool, 1+len(previous))
	out := make([]string, 0, 1+len(previous))
	appendKey := func(k string) {
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, k)
	}
	if hint != "" && TokenKeyID(current) == hint {
		appendKey(current)
	}
	for _, p := range previous {
		if hint != "" && TokenKeyID(p) == hint {
			appendKey(p)
		}
	}
	appendKey(current)
	for _, p := range previous {
		appendKey(p)
	}
	return out
}

// JWTAuthConfig configures the JWT authentication middleware.
// Use Secret for HS256 or PublicKey for RS256.
type JWTAuthConfig struct {
	Secret          string
	PreviousSecrets []string
	PublicKey       *rsa.PublicKey
	SkipPaths       []string
	Blacklist       TokenBlacklistChecker
	// Issuer, when set, is required to match the token's `iss` exactly.
	Issuer string
	// Audience, when set, must be present in the token's `aud`.
	Audience string
	// SessionValidator checks a credential's session POSITIVELY: the session
	// must exist and its epoch must be current. A nil validator still requires
	// the session fields to be present, so a token predating the scheme is
	// refused rather than grandfathered in.
	SessionValidator SessionValidator
}

// SessionValidator reports whether a credential's session is live and its epoch
// still current. *service.AuthService implements it.
type SessionValidator interface {
	SessionValid(sessionID string, epoch int64) bool
}

// TokenBlacklistChecker checks if a JWT ID has been revoked.
type TokenBlacklistChecker interface {
	IsTokenRevoked(ctx context.Context, jti string) bool
}

// parseBearerClaims extracts and verifies the bearer token from the request,
// applying the fixed clock leeway (exp/nbf/iat) and the signature check with
// key rotation. It aborts the request with 401 and returns ok=false when the
// credential does not parse, so callers never have to repeat the rejection.
func parseBearerClaims(c *gin.Context, config JWTAuthConfig, logger *zap.Logger, secret []byte, tokenStr string) (jwt.MapClaims, bool) {
	// Parse without claims validation — signature IS verified, but exp/nbf/iat
	// are checked separately. This is necessary for key rotation: tokens
	// signed with old (previous) secrets may appear "expired" from the current
	// key's perspective, but we must still attempt verification against prevSecrets.
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())

	buildKeyFunc := func(k interface{}) jwt.Keyfunc {
		return func(t *jwt.Token) (interface{}, error) {
			// Pin the algorithm EXACTLY, not just its family: accepting any
			// *SigningMethodHMAC would also accept HS384/HS512, which are
			// not what this service issues.
			if config.PublicKey != nil {
				if t.Method.Alg() != jwt.SigningMethodRS256.Alg() {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
				return config.PublicKey, nil
			}
			if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return k, nil
		}
	}

	claims := jwt.MapClaims{}
	_, err := parser.ParseWithClaims(tokenStr, claims, buildKeyFunc(secret))

	// Try previous secrets if current key fails and no public key is
	// configured. The candidate order prefers the key whose kid matches the
	// token header (rotation fast path) but still tries every allowed key:
	// the kid is a hint, not trust.
	if err != nil && config.PublicKey == nil && len(config.PreviousSecrets) > 0 {
		for _, candidate := range OrderedKeyCandidates(tokenStr, config.Secret, config.PreviousSecrets) {
			claims = jwt.MapClaims{}
			_, err = jwt.NewParser(jwt.WithoutClaimsValidation()).ParseWithClaims(tokenStr, claims, buildKeyFunc([]byte(candidate)))
			if err == nil {
				break
			}
		}
	}

	if err != nil {
		logger.Debug("JWT parse failed", zap.Error(err))
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token", "code": "UNAUTHORIZED"})
		return nil, false
	}

	// Manual claims validation with 30s leeway for clock skew.
	// Covers exp, nbf, and iat — the last is not validated by the library
	// when WithoutClaimsValidation is used and prevents tokens from being
	// accepted before their stated issuance time.
	now := time.Now()
	if exp, ok := claims["exp"].(float64); ok && now.After(time.Unix(int64(exp), 0).Add(30*time.Second)) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token expired", "code": "UNAUTHORIZED"})
		return nil, false
	}
	if nbf, ok := claims["nbf"].(float64); ok && now.Before(time.Unix(int64(nbf), 0).Add(-30*time.Second)) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token not yet valid", "code": "UNAUTHORIZED"})
		return nil, false
	}
	if iat, ok := claims["iat"].(float64); ok && now.Add(30*time.Second).Before(time.Unix(int64(iat), 0)) {
		// iat must not be in the future beyond leeway window.
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token issued in the future", "code": "UNAUTHORIZED"})
		return nil, false
	}
	return claims, true
}

// authorizeAccessClaims applies the access-token policy to parsed claims:
// credential kind, issuer, audience, live session binding, revocation and the
// wallet claim. Every rejection aborts with 401 and returns ok=false. On
// success it returns the session binding the caller must install on the
// request.
func authorizeAccessClaims(c *gin.Context, config JWTAuthConfig, claims jwt.MapClaims) (sessionID string, epoch int64, ok bool) {
	// A playback capability is NOT an access token. It is issued to be spent
	// on the streaming routes only (where PlaybackTokenAuth validates it),
	// so it must never authorise a management API call through this
	// middleware. Whitelist the acceptable kinds rather than blacklisting
	// the known-bad ones, so a future credential type is rejected by
	// default instead of silently accepted.
	switch tokenType, _ := claims["token_type"].(string); tokenType {
	case "", "access":
	default:
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": "token is not an access token", "code": "UNAUTHORIZED",
		})
		return "", 0, false
	}

	if config.Issuer != "" {
		iss, _ := claims["iss"].(string)
		if iss != config.Issuer {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "token issuer mismatch", "code": "UNAUTHORIZED",
			})
			return "", 0, false
		}
	}

	if config.Audience != "" && !audienceContains(claims["aud"], config.Audience) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": "token audience mismatch", "code": "UNAUTHORIZED",
		})
		return "", 0, false
	}

	// Every credential must be bound to a live session. A token without the
	// fields predates the scheme, so it is refused outright rather than
	// grandfathered in — otherwise revocation could be bypassed by simply
	// presenting an older token.
	sessionID, _ = claims["session_id"].(string)
	if sessionID == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": "token is not bound to a session", "code": "UNAUTHORIZED",
		})
		return "", 0, false
	}
	epoch, hasEpoch := numericClaim(claims["auth_epoch"])
	if !hasEpoch {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": "token carries no auth epoch", "code": "UNAUTHORIZED",
		})
		return "", 0, false
	}
	if config.SessionValidator != nil && !config.SessionValidator.SessionValid(sessionID, epoch) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": "session is no longer valid", "code": "UNAUTHORIZED",
		})
		return "", 0, false
	}

	if config.Blacklist != nil {
		jti, _ := claims["jti"].(string)
		if jti != "" && config.Blacklist.IsTokenRevoked(c.Request.Context(), jti) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token revoked", "code": "TOKEN_REVOKED"})
			return "", 0, false
		}
	}

	walletAddress, _ := claims["wallet_address"].(string)
	if walletAddress == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token missing wallet address", "code": "UNAUTHORIZED"})
		return "", 0, false
	}
	return sessionID, epoch, true
}

// JWTAuthMiddleware returns a gin middleware that validates JWT tokens
// and injects wallet_address and jwt_claims into the context.
func JWTAuthMiddleware(config JWTAuthConfig, logger *zap.Logger) gin.HandlerFunc {
	secret := []byte(config.Secret)
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

		// playback_token presence alone must NEVER skip JWT. On streaming routes,
		// PlaybackTokenAuth runs BEFORE this middleware and sets playback_auth
		// only after cryptographically validating the query token. HLS.js cannot
		// send Authorization headers on media requests, so a validated playback
		// token is accepted in lieu of a JWT — but only on routes where that
		// middleware actually ran.
		if c.GetBool("playback_auth") {
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

		claims, ok := parseBearerClaims(c, config, logger, secret, tokenStr)
		if !ok {
			return
		}

		sessionID, epoch, ok := authorizeAccessClaims(c, config, claims)
		if !ok {
			return
		}

		c.Set("wallet_address", claims["wallet_address"].(string))
		c.Set("jwt_claims", claims)
		SetSessionBinding(c, sessionID, epoch)
		c.Next()
	}
}

// SetSessionBinding records which session (and epoch) a request is acting under.
// Both the JWT path and the validated-capability path set it, so a capability
// minted from this request can inherit the same session and be revoked with it.
func SetSessionBinding(c *gin.Context, sessionID string, epoch int64) {
	if sessionID == "" {
		return
	}
	c.Set("session_id", sessionID)
	c.Set("auth_epoch", epoch)
}

// GetSessionBinding returns the session a request is acting under, or empty if
// the request carries none. An empty session is not an error here: callers that
// need one must decide what to do about its absence.
func GetSessionBinding(c *gin.Context) (sessionID string, epoch int64) {
	rawSession, _ := c.Get("session_id")
	rawEpoch, _ := c.Get("auth_epoch")
	sessionID, _ = rawSession.(string)
	epoch, _ = rawEpoch.(int64)
	return sessionID, epoch
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

// numericClaim reads a JSON number claim, which jwt decodes as float64.
func numericClaim(raw interface{}) (int64, bool) {
	switch v := raw.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	}
	return 0, false
}

// audienceContains reports whether a token's `aud` claim includes want.
// JWT allows `aud` to be a single string or an array of strings.
func audienceContains(raw interface{}, want string) bool {
	switch v := raw.(type) {
	case string:
		return v == want
	case []string:
		for _, s := range v {
			if s == want {
				return true
			}
		}
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}
