package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/service"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A segment request carries its capability in the query string, and the JWT
// middleware deliberately steps aside once a capability has been validated. So
// nothing else would notice a caller pairing their own JWT with someone else's
// capability - the capability is honoured while the request claims a different
// identity. This is the check that notices.
func TestRejectMismatchedJWTCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const secret = "test-secret-that-is-at-least-32-chars"
	svc := service.NewAuthService(secret, nil)

	const walletA = "0xAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAa"
	const walletB = "0xBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBb"
	const capability = "capability-token-value"

	mintJWT := func(t *testing.T, wallet string) string {
		t.Helper()
		claims := jwt.MapClaims{
			"wallet_address": wallet,
			"sub":            wallet,
			"iss":            service.AccessTokenIssuer,
			"aud":            []string{service.AccessTokenAudience},
			"iat":            time.Now().Unix(),
			"exp":            time.Now().Add(time.Hour).Unix(),
		}
		signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
		require.NoError(t, err)
		return signed
	}

	run := func(t *testing.T, authHeader, capabilityWallet string) (*gin.Context, *httptest.ResponseRecorder) {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/streaming/c1/segment/0.ts?playback_token="+capability, http.NoBody)
		if authHeader != "" {
			c.Request.Header.Set("Authorization", authHeader)
		}
		rejectMismatchedJWTCapability(c, svc, capabilityWallet)
		return c, w
	}

	t.Run("no authorization header", func(t *testing.T) {
		c, _ := run(t, "", walletA)
		assert.False(t, c.IsAborted(), "nothing to reconcile")
	})

	t.Run("header carries the capability itself", func(t *testing.T) {
		c, _ := run(t, "Bearer "+capability, walletA)
		assert.False(t, c.IsAborted(), "the same credential is not a conflict")
	})

	t.Run("jwt for the same wallet", func(t *testing.T) {
		c, _ := run(t, "Bearer "+mintJWT(t, walletA), walletA)
		assert.False(t, c.IsAborted(), "matching wallets must be allowed")
	})

	t.Run("jwt for a different wallet is refused", func(t *testing.T) {
		c, w := run(t, "Bearer "+mintJWT(t, walletB), walletA)
		assert.True(t, c.IsAborted(), "a mismatched pairing must be refused")
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("unparseable bearer is ignored", func(t *testing.T) {
		c, _ := run(t, "Bearer not-a-jwt", walletA)
		assert.False(t, c.IsAborted(), "a non-access token is not a mismatch")
	})
}
