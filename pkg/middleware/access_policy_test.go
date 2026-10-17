package middleware

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubBlacklistChecker revokes exactly the JTIs it was told to.
type stubBlacklistChecker struct{ revoked map[string]bool }

func (b stubBlacklistChecker) IsTokenRevoked(_ context.Context, jti string) bool {
	return b.revoked[jti]
}

// accessClaims builds a well-formed access token claim set for the shared
// policy: the issuer, audience, purpose and session binding the service mints.
func accessClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"wallet_address": "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		"session_id":     "s1",
		"auth_epoch":     float64(1),
		"token_type":     "access",
		"iss":            "streamgate",
		"aud":            "streamgate-api",
		"jti":            "jti-1",
		"exp":            time.Now().Add(time.Hour).Unix(),
	}
}

func accessPolicy() AccessPolicy {
	return AccessPolicy{
		Issuer:           "streamgate",
		Audience:         "streamgate-api",
		SessionValidator: stubSessionValidator{epochs: map[string]int64{"s1": 1}},
	}
}

func TestAuthorizeAccessClaims_AcceptsAWellFormedAccessToken(t *testing.T) {
	decision, err := AuthorizeAccessClaims(context.Background(), accessPolicy(), accessClaims())
	require.NoError(t, err)
	assert.Equal(t, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", decision.Wallet)
	assert.Equal(t, "s1", decision.SessionID)
	assert.EqualValues(t, 1, decision.AuthEpoch)
}

// A purpose-less credential is refused: the whitelist accepts ONLY "access", so
// an older token that predates the token_type claim is not grandfathered in.
func TestAuthorizeAccessClaims_RefusesAMissingTokenType(t *testing.T) {
	claims := accessClaims()
	delete(claims, "token_type")
	_, err := AuthorizeAccessClaims(context.Background(), accessPolicy(), claims)
	assert.ErrorIs(t, err, ErrAccessNotAccessToken)
}

func TestAuthorizeAccessClaims_RefusesAPlaybackToken(t *testing.T) {
	claims := accessClaims()
	claims["token_type"] = "playback"
	_, err := AuthorizeAccessClaims(context.Background(), accessPolicy(), claims)
	assert.ErrorIs(t, err, ErrAccessNotAccessToken)
}

func TestAuthorizeAccessClaims_RefusesAnIssuerMismatch(t *testing.T) {
	claims := accessClaims()
	claims["iss"] = "somewhere-else"
	_, err := AuthorizeAccessClaims(context.Background(), accessPolicy(), claims)
	assert.ErrorIs(t, err, ErrAccessIssuerMismatch)
}

func TestAuthorizeAccessClaims_RefusesAnAudienceMismatch(t *testing.T) {
	claims := accessClaims()
	claims["aud"] = "other-service"
	_, err := AuthorizeAccessClaims(context.Background(), accessPolicy(), claims)
	assert.ErrorIs(t, err, ErrAccessAudienceMismatch)
}

func TestAuthorizeAccessClaims_RefusesAMissingSession(t *testing.T) {
	claims := accessClaims()
	delete(claims, "session_id")
	_, err := AuthorizeAccessClaims(context.Background(), accessPolicy(), claims)
	assert.ErrorIs(t, err, ErrAccessNotSessionBound)
}

func TestAuthorizeAccessClaims_RefusesAMissingEpoch(t *testing.T) {
	claims := accessClaims()
	delete(claims, "auth_epoch")
	_, err := AuthorizeAccessClaims(context.Background(), accessPolicy(), claims)
	assert.ErrorIs(t, err, ErrAccessNoEpoch)
}

func TestAuthorizeAccessClaims_RefusesAStaleEpoch(t *testing.T) {
	claims := accessClaims()
	claims["auth_epoch"] = float64(2)
	_, err := AuthorizeAccessClaims(context.Background(), accessPolicy(), claims)
	assert.ErrorIs(t, err, ErrAccessSessionInvalid)
}

func TestAuthorizeAccessClaims_RefusesAnUnknownSession(t *testing.T) {
	policy := accessPolicy()
	policy.SessionValidator = stubSessionValidator{epochs: map[string]int64{}}
	_, err := AuthorizeAccessClaims(context.Background(), policy, accessClaims())
	assert.ErrorIs(t, err, ErrAccessSessionInvalid)
}

func TestAuthorizeAccessClaims_RefusesARevokedToken(t *testing.T) {
	policy := accessPolicy()
	policy.Blacklist = stubBlacklistChecker{revoked: map[string]bool{"jti-1": true}}
	_, err := AuthorizeAccessClaims(context.Background(), policy, accessClaims())
	assert.ErrorIs(t, err, ErrAccessRevoked)
}

func TestAuthorizeAccessClaims_RefusesAMissingWallet(t *testing.T) {
	claims := accessClaims()
	delete(claims, "wallet_address")
	_, err := AuthorizeAccessClaims(context.Background(), accessPolicy(), claims)
	assert.ErrorIs(t, err, ErrAccessNoWallet)
}

// The HTTP wrapper must keep the exact status/body the middleware returned
// before the policy moved: the shared validator is a refactor, not a behaviour
// change (apart from the tightened token kind).
func TestJWTAuthMiddleware_PolicyWrapperStatuses(t *testing.T) {
	r := tokenKindRouter(t, JWTAuthConfig{
		Secret:           tokenKindTestSecret,
		Issuer:           "streamgate",
		Audience:         "streamgate-api",
		SessionValidator: stubSessionValidator{epochs: map[string]int64{"s1": 1}},
		Blacklist:        stubBlacklistChecker{revoked: map[string]bool{"revoked-jti": true}},
	})

	good := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256, accessClaims())
	assert.Equal(t, http.StatusOK, callProtected(t, r, good))

	revokedClaims := accessClaims()
	revokedClaims["jti"] = "revoked-jti"
	assert.Equal(t, http.StatusUnauthorized, callProtected(t, r,
		signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256, revokedClaims)))

	notAccess := accessClaims()
	notAccess["token_type"] = "playback"
	assert.Equal(t, http.StatusUnauthorized, callProtected(t, r,
		signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256, notAccess)))
}
