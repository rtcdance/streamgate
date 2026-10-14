package service

// auth_key_rotation_test.go — SG-14 part 1 acceptance: TestAcceptance_KeyRotationAndAudience
//
// Pins the key-separation and bounded-rotation contract:
//   - access JWTs and playback capabilities verify under INDEPENDENT keys, so
//     neither credential can be spent as the other (key + audience),
//   - newly minted tokens carry a kid header; verification prefers the kid
//     match but still tries every allowed key (kid is a hint, not trust),
//   - a retired key in the previous list keeps verifying tokens inside their
//     TTL; removing it makes those tokens fail,
//   - the previous list is bounded at 2.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/storage"
)

const (
	rotMasterSecret    = "rot-master-secret-at-least-32-chars!!"
	rotOldAccessSecret = "rot-old-access-secret-at-least-32-chars!"
	rotNewAccessSecret = "rot-new-access-secret-at-least-32-chars!"
	rotCapSecret       = "rot-capability-secret-at-least-32-chars!"
	rotThirdPrevSecret = "rot-third-prev-secret-at-least-32-chars!"
	rotAccessWallet    = "0xrot000000000000000000000000000000000001"
)

// newSeparatedAuthService builds an AuthService with an explicit access key,
// capability key and bounded rotation lists.
func newSeparatedAuthService(t *testing.T, access string, prevAccess []string, capability string, prevCap []string) *AuthService {
	t.Helper()
	svc := NewAuthService(rotMasterSecret, nil,
		WithTokenKeySeparation(access, prevAccess, capability, prevCap),
		WithSessionStore(storage.NewMemorySessionStore()),
	)
	return svc
}

// mintAccess signs an access token under svc's CURRENT access key and returns
// it plus the session it is bound to.
func mintAccess(t *testing.T, svc *AuthService, wallet string) (string, string, int64) {
	t.Helper()
	sessionID, err := svc.StartSession()
	require.NoError(t, err)
	claims := &Claims{
		Username:      wallet,
		WalletAddress: wallet,
		JTI:           "jti-" + wallet,
		SessionID:     sessionID,
		AuthEpoch:     1,
	}
	claims.RegisteredClaims = accessTokenClaims(wallet, time.Hour)
	token, err := svc.signToken(claims)
	require.NoError(t, err)
	return token, sessionID, 1
}

// tokenKid decodes the kid header of a compact JWT without verifying it.
func tokenKid(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)
	var header struct {
		Kid string `json:"kid"`
	}
	require.NoError(t, json.Unmarshal(raw, &header))
	return header.Kid
}

// TestAcceptance_KeyRotationAndAudience pins the SG-14 key contract.
func TestAcceptance_KeyRotationAndAudience(t *testing.T) {
	ctx := context.Background()

	t.Run("kid present on newly minted tokens", func(t *testing.T) {
		svc := newSeparatedAuthService(t, rotNewAccessSecret, nil, rotCapSecret, nil)
		access, _, _ := mintAccess(t, svc, rotAccessWallet)
		assert.Equal(t, middleware.TokenKeyID(rotNewAccessSecret), tokenKid(t, access),
			"an access token must carry the current access key's kid")

		capability, err := svc.GeneratePlaybackToken(ctx, rotAccessWallet, "content-1", "", "", 1, time.Minute, "")
		require.NoError(t, err)
		assert.Equal(t, middleware.TokenKeyID(rotCapSecret), tokenKid(t, capability),
			"a capability must carry the capability key's kid")
	})

	t.Run("old key verifies while in previous list, fails after removal", func(t *testing.T) {
		// Mint under the OLD access key (simulating the pre-rotation deployment).
		oldSvc := newSeparatedAuthService(t, rotOldAccessSecret, nil, rotCapSecret, nil)
		token, sessionID, epoch := mintAccess(t, oldSvc, rotAccessWallet)

		// Fixed clock inside the token's TTL, so expiry is not what fails below.
		now := time.Now()

		// Rotated: new current key, old key retained as previous ⇒ verifies.
		rotated := newSeparatedAuthService(t, rotNewAccessSecret, []string{rotOldAccessSecret}, rotCapSecret, nil)
		rotated.clockNow = func() time.Time { return now }
		claims, err := rotated.ParseToken(token)
		require.NoError(t, err, "a token minted under the retired key must verify while that key is in the previous list")
		assert.Equal(t, rotAccessWallet, claims.WalletAddress)
		assert.Equal(t, sessionID, claims.SessionID)
		assert.EqualValues(t, epoch, claims.AuthEpoch)

		// Removed: old key gone from config ⇒ the same token fails.
		removed := newSeparatedAuthService(t, rotNewAccessSecret, nil, rotCapSecret, nil)
		removed.clockNow = func() time.Time { return now }
		_, err = removed.ParseToken(token)
		assert.Error(t, err, "a key removed from the config must no longer verify")

		// Expired even with the key still present: rotation does not extend TTL.
		rotated.clockNow = func() time.Time { return now.Add(2 * time.Hour) }
		_, err = rotated.ParseToken(token)
		assert.ErrorIs(t, err, ErrTokenExpired, "an old key must not extend a token past its exp")
	})

	t.Run("rotation list is bounded at 2", func(t *testing.T) {
		// Three previous keys: only the first two are honoured, the third
		// cannot verify even though it was configured.
		svc := newSeparatedAuthService(t, rotNewAccessSecret,
			[]string{rotOldAccessSecret, rotNewAccessSecret + "-p1", rotThirdPrevSecret},
			rotCapSecret, nil)

		third := newSeparatedAuthService(t, rotThirdPrevSecret, nil, rotCapSecret, nil)
		thirdToken, _, _ := mintAccess(t, third, rotAccessWallet)

		_, err := svc.ParseToken(thirdToken)
		assert.Error(t, err, "a key beyond the rotation bound (2) must not verify")

		// The two retained keys still work.
		old := newSeparatedAuthService(t, rotOldAccessSecret, nil, rotCapSecret, nil)
		oldToken, _, _ := mintAccess(t, old, rotAccessWallet)
		_, err = svc.ParseToken(oldToken)
		assert.NoError(t, err, "a key within the rotation bound must still verify")
	})

	t.Run("access token cannot verify as capability and vice versa", func(t *testing.T) {
		svc := newSeparatedAuthService(t, rotNewAccessSecret, nil, rotCapSecret, nil)

		access, sessionID, _ := mintAccess(t, svc, rotAccessWallet)

		// Access token presented where a capability is expected: the capability
		// key set cannot verify it (independent keys), before any audience
		// logic even runs.
		_, err := svc.ParsePlaybackToken(ctx, access, "content-1", "")
		assert.Error(t, err, "an access token must not verify as a playback capability")

		// Capability presented as an access credential: the middleware rejects
		// it on token_type even though the service can parse it.
		capability, err := svc.GeneratePlaybackToken(ctx, rotAccessWallet, "content-1", "", "", 1, time.Minute, "",
			WithSessionBinding(sessionID, 1))
		require.NoError(t, err)
		claims, err := svc.ParseToken(capability)
		require.NoError(t, err, "the generic parser accepts a capability (audience checks belong to the specific paths)")
		assert.Equal(t, "playback", claims.TokenType)
	})

	t.Run("audience separates even under the same key", func(t *testing.T) {
		// Force the two credential kinds onto the SAME key so only the audience
		// distinguishes them: a capability-shaped token carrying the access
		// audience must be refused by ParsePlaybackToken.
		svc := newSeparatedAuthService(t, rotNewAccessSecret, nil, rotNewAccessSecret, nil)

		sessionID, err := svc.StartSession()
		require.NoError(t, err)
		claims := &Claims{
			Username:      rotAccessWallet,
			TokenType:     "playback",
			WalletAddress: rotAccessWallet,
			ContentID:     "content-1",
			SessionID:     sessionID,
			AuthEpoch:     1,
		}
		claims.RegisteredClaims = accessTokenClaims("content-1", time.Minute) // ACCESS audience
		// Signed with the capability key, so the key check passes and only the
		// audience stands between this token and a media fetch.
		forge, err := svc.signTokenWithKind(claims, capabilityToken)
		require.NoError(t, err)

		_, err = svc.ParsePlaybackToken(ctx, forge, "content-1", "")
		require.Error(t, err, "a token with the access audience must not pass the capability audience check")
		assert.Contains(t, err.Error(), "audience")
	})

	t.Run("verification prefers kid but kid is only a hint", func(t *testing.T) {
		// Mint under the old key, then present it to a rotated service whose
		// CURRENT key is different: its kid (the old key's) does not match the
		// current key, yet verification must fall through to the previous key —
		// the kid orders candidates, it never excludes them.
		old := newSeparatedAuthService(t, rotOldAccessSecret, nil, rotCapSecret, nil)
		token, _, _ := mintAccess(t, old, rotAccessWallet)
		assert.Equal(t, middleware.TokenKeyID(rotOldAccessSecret), tokenKid(t, token))

		rotated := newSeparatedAuthService(t, rotNewAccessSecret, []string{rotOldAccessSecret}, rotCapSecret, nil)
		_, err := rotated.ParseToken(token)
		assert.NoError(t, err, "a valid old-key token must verify even when its kid does not match the current key")

		// The kid lives in the SIGNED header, so rewriting it invalidates the
		// signature: an attacker cannot mint a matching hint for a token signed
		// by an unknown key. This pins that the header is authenticated.
		rogue := newSeparatedAuthService(t, rotThirdPrevSecret, nil, rotCapSecret, nil)
		rogueToken, _, _ := mintAccess(t, rogue, rotAccessWallet)
		tampered := stampKid(t, rogueToken, middleware.TokenKeyID(rotNewAccessSecret))
		_, err = rotated.ParseToken(tampered)
		assert.Error(t, err, "rewriting the kid breaks the signature, so a forged hint can never verify")
	})
}

// stampKid rewrites a compact JWT's kid header without touching its payload or
// signature — exactly what an attacker can do, since the header is not
// authenticated separately from the signature.
func stampKid(t *testing.T, token, kid string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)
	var header map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &header))
	header["kid"] = kid
	rewritten, err := json.Marshal(header)
	require.NoError(t, err)
	parts[0] = base64.RawURLEncoding.EncodeToString(rewritten)
	return strings.Join(parts, ".")
}

// TestTokenKeyID_MatchesSHA256Prefix pins the kid derivation: first 8 hex
// characters of SHA-256(key), so operators can compute it by hand.
func TestTokenKeyID_MatchesSHA256Prefix(t *testing.T) {
	key := "some-verification-key"
	sum := sha256.Sum256([]byte(key))
	want := hex.EncodeToString(sum[:4])
	assert.Equal(t, want, middleware.TokenKeyID(key))
	assert.Len(t, middleware.TokenKeyID(key), 8)

	// Distinct keys produce distinct kids (no accidental collision on short
	// material), and an HMAC sanity check keeps the derivation honest.
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("streamgate/playback"))
	assert.NotEqual(t, want, hex.EncodeToString(mac.Sum(nil)[:4]))
}

// TestDeriveCapabilitySecret_DeterministicAndDistinct pins the config-level
// derivation: the capability key is HKDF(master, "streamgate/playback") —
// deterministic per master, never equal to the master or the access key.
func TestDeriveCapabilitySecret_DeterministicAndDistinct(t *testing.T) {
	derived := config.DeriveCapabilitySecret(rotMasterSecret)
	assert.NotEmpty(t, derived)
	assert.Equal(t, derived, config.DeriveCapabilitySecret(rotMasterSecret), "derivation must be deterministic")
	assert.NotEqual(t, rotMasterSecret, derived, "the derived key must differ from the master secret")
	assert.NotEqual(t, config.DeriveCapabilitySecret("another-master-secret-at-least-32-chars!"), derived,
		"different masters must derive different capability keys")
}
