package middleware

import (
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
)

// stubSessionValidator is the positive-existence check: a session is valid only
// if it is known here and the epoch matches.
type stubSessionValidator struct {
	epochs map[string]int64
}

func (s stubSessionValidator) SessionValid(sessionID string, epoch int64) bool {
	current, ok := s.epochs[sessionID]
	return ok && epoch == current
}

// A credential with no session binding predates the scheme, so it is refused
// rather than grandfathered in — otherwise revocation could be bypassed by
// simply presenting an older token.
func TestJWTAuth_RequiresASessionBinding(t *testing.T) {
	r := tokenKindRouter(t, JWTAuthConfig{Secret: tokenKindTestSecret})

	noSession := baseAccessClaims(nil)
	delete(noSession, "session_id")
	token := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256, noSession)
	assert.Equal(t, http.StatusUnauthorized, callProtected(t, r, token))

	noEpoch := baseAccessClaims(nil)
	delete(noEpoch, "auth_epoch")
	token = signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256, noEpoch)
	assert.Equal(t, http.StatusUnauthorized, callProtected(t, r, token))
}

func TestJWTAuth_AcceptsALiveSession(t *testing.T) {
	validator := stubSessionValidator{epochs: map[string]int64{"s1": 3}}
	r := tokenKindRouter(t, JWTAuthConfig{Secret: tokenKindTestSecret, SessionValidator: validator})

	token := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256,
		baseAccessClaims(map[string]interface{}{"session_id": "s1", "auth_epoch": 3}))
	assert.Equal(t, http.StatusOK, callProtected(t, r, token))
}

// The whole point of the epoch: moving it invalidates every credential of the
// session at once, with no per-token bookkeeping.
func TestJWTAuth_RefusesAStaleEpoch(t *testing.T) {
	validator := stubSessionValidator{epochs: map[string]int64{"s1": 4}}
	r := tokenKindRouter(t, JWTAuthConfig{Secret: tokenKindTestSecret, SessionValidator: validator})

	stale := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256,
		baseAccessClaims(map[string]interface{}{"session_id": "s1", "auth_epoch": 3}))
	assert.Equal(t, http.StatusUnauthorized, callProtected(t, r, stale),
		"an epoch bump must invalidate older credentials")
}

// A session that does not exist is not valid, regardless of its epoch. Absence
// of a revocation record is not evidence of validity.
func TestJWTAuth_RefusesAnUnknownSession(t *testing.T) {
	validator := stubSessionValidator{epochs: map[string]int64{}}
	r := tokenKindRouter(t, JWTAuthConfig{Secret: tokenKindTestSecret, SessionValidator: validator})

	unknown := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256,
		baseAccessClaims(map[string]interface{}{"session_id": "never-issued", "auth_epoch": 1}))
	assert.Equal(t, http.StatusUnauthorized, callProtected(t, r, unknown))
}

// The expiry check must still run alongside the session check.
func TestJWTAuth_SessionCheckDoesNotDisplaceExpiry(t *testing.T) {
	validator := stubSessionValidator{epochs: map[string]int64{"s1": 1}}
	r := tokenKindRouter(t, JWTAuthConfig{Secret: tokenKindTestSecret, SessionValidator: validator})

	expired := baseAccessClaims(map[string]interface{}{
		"session_id": "s1", "auth_epoch": 1,
		"exp": time.Now().Add(-time.Hour).Unix(),
	})
	token := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256, expired)
	assert.Equal(t, http.StatusUnauthorized, callProtected(t, r, token))
}
