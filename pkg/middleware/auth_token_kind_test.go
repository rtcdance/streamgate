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

const tokenKindTestSecret = "token-kind-test-secret-at-least-32!"

func signWithClaims(t *testing.T, secret string, method jwt.SigningMethod, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, claims)
	signed, err := tok.SignedString([]byte(secret))
	require.NoError(t, err)
	return signed
}

// baseAccessClaims is a minimal well-formed access token: wallet subject plus
// the issuer/audience the service actually mints.
func baseAccessClaims(extra map[string]interface{}) jwt.MapClaims {
	claims := jwt.MapClaims{
		"wallet_address": "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Unix(),
		"iss":            "streamgate",
		"aud":            "streamgate-api",
	}
	for k, v := range extra {
		claims[k] = v
	}
	return claims
}

func tokenKindRouter(t *testing.T, cfg JWTAuthConfig) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(JWTAuthMiddleware(cfg, zap.NewNop()))
	r.GET("/protected", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func callProtected(t *testing.T, r *gin.Engine, token string) int {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	return w.Code
}

// A playback capability is minted to be spent on the streaming routes only.
// Accepting it here would let a media credential call management APIs.
func TestJWTAuth_RejectsPlaybackCapabilityAsAccessToken(t *testing.T) {
	r := tokenKindRouter(t, JWTAuthConfig{Secret: tokenKindTestSecret})
	token := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256,
		baseAccessClaims(map[string]interface{}{"token_type": "playback"}))

	assert.Equal(t, http.StatusUnauthorized, callProtected(t, r, token))
}

// Unknown credential kinds fail closed rather than being assumed to be access
// tokens, so a future token type cannot silently gain management access.
func TestJWTAuth_RejectsUnknownTokenType(t *testing.T) {
	r := tokenKindRouter(t, JWTAuthConfig{Secret: tokenKindTestSecret})

	for _, kind := range []string{"refresh", "bogus", "PLAYBACK"} {
		token := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256,
			baseAccessClaims(map[string]interface{}{"token_type": kind}))
		assert.Equal(t, http.StatusUnauthorized, callProtected(t, r, token),
			"token_type=%q must be rejected", kind)
	}
}

func TestJWTAuth_AcceptsAccessTokenType(t *testing.T) {
	r := tokenKindRouter(t, JWTAuthConfig{Secret: tokenKindTestSecret})

	for _, kind := range []interface{}{"access", nil} {
		claims := baseAccessClaims(nil)
		if kind != nil {
			claims["token_type"] = kind
		} else {
			delete(claims, "token_type")
		}
		token := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256, claims)
		assert.Equal(t, http.StatusOK, callProtected(t, r, token))
	}
}

func TestJWTAuth_IssuerEnforced(t *testing.T) {
	r := tokenKindRouter(t, JWTAuthConfig{Secret: tokenKindTestSecret, Issuer: "streamgate"})

	wrong := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256,
		baseAccessClaims(map[string]interface{}{"iss": "somewhere-else"}))
	assert.Equal(t, http.StatusUnauthorized, callProtected(t, r, wrong))

	missingClaims := baseAccessClaims(nil)
	delete(missingClaims, "iss")
	missing := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256, missingClaims)
	assert.Equal(t, http.StatusUnauthorized, callProtected(t, r, missing))

	good := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256, baseAccessClaims(nil))
	assert.Equal(t, http.StatusOK, callProtected(t, r, good))
}

func TestJWTAuth_AudienceEnforced(t *testing.T) {
	r := tokenKindRouter(t, JWTAuthConfig{Secret: tokenKindTestSecret, Audience: "streamgate-api"})

	// A string audience.
	good := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256, baseAccessClaims(nil))
	assert.Equal(t, http.StatusOK, callProtected(t, r, good))

	// An array audience containing the expected value.
	arrayAud := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256,
		baseAccessClaims(map[string]interface{}{"aud": []interface{}{"other", "streamgate-api"}}))
	assert.Equal(t, http.StatusOK, callProtected(t, r, arrayAud))

	wrong := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256,
		baseAccessClaims(map[string]interface{}{"aud": "other-service"}))
	assert.Equal(t, http.StatusUnauthorized, callProtected(t, r, wrong))

	missingClaims := baseAccessClaims(nil)
	delete(missingClaims, "aud")
	missing := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256, missingClaims)
	assert.Equal(t, http.StatusUnauthorized, callProtected(t, r, missing))
}

// The algorithm is pinned exactly: HS384 is a valid HMAC token but is not what
// this service issues, so it must not be accepted.
func TestJWTAuth_RejectsNonHS256Algorithm(t *testing.T) {
	r := tokenKindRouter(t, JWTAuthConfig{Secret: tokenKindTestSecret})

	for _, method := range []jwt.SigningMethod{jwt.SigningMethodHS384, jwt.SigningMethodHS512} {
		token := signWithClaims(t, tokenKindTestSecret, method, baseAccessClaims(nil))
		assert.Equal(t, http.StatusUnauthorized, callProtected(t, r, token),
			"%s must be rejected", method.Alg())
	}
}
