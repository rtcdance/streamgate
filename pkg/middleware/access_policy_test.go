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
// policy: the issuer, audience, purpose, session binding and recovery epoch the
// service mints.
func accessClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"wallet_address": "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		"session_id":     "s1",
		"auth_epoch":     float64(1),
		"restore_epoch":  "restore-epoch-1",
		"token_type":     "access",
		"iss":            "streamgate",
		"aud":            "streamgate-api",
		"jti":            "jti-1",
		"exp":            time.Now().Add(time.Hour).Unix(),
	}
}

// stubRestoreEpochValidator answers with a fixed epoch, or with nothing at all
// when the authority is supposed to be unreadable.
type stubRestoreEpochValidator struct {
	epoch string
	ok    bool
}

func (s stubRestoreEpochValidator) RestoreEpoch() (string, bool) { return s.epoch, s.ok }

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

// A credential with no recovery epoch predates the scheme and must be refused
// even when no authority is wired: treating "absent" as "current" would
// grandfather in exactly the credentials a restore exists to invalidate.
func TestAuthorizeAccessClaims_RefusesAMissingRestoreEpoch(t *testing.T) {
	claims := accessClaims()
	delete(claims, "restore_epoch")
	_, err := AuthorizeAccessClaims(context.Background(), accessPolicy(), claims)
	assert.ErrorIs(t, err, ErrAccessNoRestoreEpoch)
}

func TestAuthorizeAccessClaims_RefusesAStaleRestoreEpoch(t *testing.T) {
	policy := accessPolicy()
	policy.RestoreEpochValidator = stubRestoreEpochValidator{epoch: "restore-epoch-2", ok: true}

	_, err := AuthorizeAccessClaims(context.Background(), policy, accessClaims())
	assert.ErrorIs(t, err, ErrAccessRestoreEpochStale,
		"a credential minted before the restore must stop verifying")
}

// An authority that cannot be read is a refusal, never a wildcard.
func TestAuthorizeAccessClaims_RefusesAnUnreadableRestoreEpoch(t *testing.T) {
	policy := accessPolicy()
	policy.RestoreEpochValidator = stubRestoreEpochValidator{ok: false}

	_, err := AuthorizeAccessClaims(context.Background(), policy, accessClaims())
	assert.ErrorIs(t, err, ErrAccessRestoreEpochUnavailable)
}

func TestAuthorizeAccessClaims_AcceptsAMatchingRestoreEpoch(t *testing.T) {
	policy := accessPolicy()
	policy.RestoreEpochValidator = stubRestoreEpochValidator{epoch: "restore-epoch-1", ok: true}

	decision, err := AuthorizeAccessClaims(context.Background(), policy, accessClaims())
	assert.NoError(t, err)
	assert.Equal(t, "s1", decision.SessionID)
}

// The comparison must not depend on the case/whitespace form a particular store
// happens to render the UUID in.
func TestAuthorizeAccessClaims_RestoreEpochComparisonIsCaseInsensitive(t *testing.T) {
	policy := accessPolicy()
	policy.RestoreEpochValidator = stubRestoreEpochValidator{epoch: "RESTORE-EPOCH-1", ok: true}

	_, err := AuthorizeAccessClaims(context.Background(), policy, accessClaims())
	assert.NoError(t, err)
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
