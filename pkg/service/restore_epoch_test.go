package service

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/rtcdance/streamgate/pkg/middleware"
	stg "github.com/rtcdance/streamgate/pkg/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unreadableEpochAuthority stands in for a Postgres that cannot be read. An
// authority that cannot be read must be a REFUSAL, never a wildcard.
type unreadableEpochAuthority struct{}

func (unreadableEpochAuthority) Current(context.Context) (stg.RestoreEpoch, error) {
	return stg.RestoreEpoch{}, stg.ErrRestoreEpochUnavailable
}

func (unreadableEpochAuthority) Rotate(context.Context) (stg.RestoreEpoch, error) {
	return stg.RestoreEpoch{}, stg.ErrRestoreEpochUnavailable
}

func newRestoreEpochTestService(t *testing.T, authority stg.RestoreEpochStore) (*AuthService, *stg.MemoryRestoreEpochStore) {
	t.Helper()
	store, ok := authority.(*stg.MemoryRestoreEpochStore)
	require.True(t, ok, "the test authority must be rotatable")
	svc := NewAuthService(signTypeTestSecret, nil, WithRestoreEpochStore(store))
	return svc, store
}

// Every credential the service mints carries the current recovery epoch, so a
// later restore has something to compare against.
func TestAuthService_WalletLoginCarriesTheRestoreEpoch(t *testing.T) {
	svc, store := newRestoreEpochTestService(t, stg.NewMemoryRestoreEpochStore())

	token, err := svc.generateWalletToken("0xwallet")
	require.NoError(t, err)

	current, err := store.Current(context.Background())
	require.NoError(t, err)

	claims := decodeUnverifiedClaims(t, token)
	assert.Equal(t, current.Epoch, claims["restore_epoch"],
		"an access credential must carry the authoritative recovery epoch")
}

// A refresh is still an issuance: presenting a credential minted BEFORE the
// restore must not re-mint a credential that outlives it.
func TestAuthService_RefreshRefusesACredentialFromBeforeTheRestore(t *testing.T) {
	svc, store := newRestoreEpochTestService(t, stg.NewMemoryRestoreEpochStore())

	token, err := svc.generateWalletToken("0xwallet")
	require.NoError(t, err)

	rotated, err := store.Rotate(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, rotated.Epoch, decodeUnverifiedClaims(t, token)["restore_epoch"])

	_, err = svc.RefreshToken(context.Background(), token)
	assert.ErrorIs(t, err, ErrRestoreEpochStale)
}

// Presence is required, not defaulted: a credential minted before the scheme
// existed carries no epoch and must not be grandfathered in.
func TestAuthService_RefreshRefusesACredentialWithNoRestoreEpoch(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)

	sessionID, err := svc.StartSession()
	require.NoError(t, err)

	claims := &Claims{
		Username:  "legacy",
		JTI:       "legacy-jti",
		SessionID: sessionID,
		AuthEpoch: 1,
		// No RestoreEpoch: this is the pre-scheme credential shape.
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := svc.signToken(claims)
	require.NoError(t, err)

	_, err = svc.RefreshToken(context.Background(), token)
	assert.ErrorIs(t, err, ErrRestoreEpochClaimMissing)
}

// A capability already handed to a player must stop working the moment the
// recovery epoch moves — that is the whole point of carrying it on media
// credentials, which cannot re-read the database per segment.
func TestAuthService_ParsePlaybackTokenRefusesACapabilityFromBeforeTheRestore(t *testing.T) {
	svc, store := newRestoreEpochTestService(t, stg.NewMemoryRestoreEpochStore())

	resolver := &countingSnapshotResolver{script: []PolicySnapshot{
		{ContentID: "c1", Rules: []middleware.GatingRule{authzRule(1)}, Version: 0, VerifiedAt: time.Now()},
	}}
	h := newPlaybackTestAuthorizer(t, &quantityVerifier{quantity: big.NewInt(1)}, resolver)

	authz, err := h.AuthorizePlayback(context.Background(), "c1", capSessionWallet, SessionBinding{SessionID: newSessionID(t, svc), AuthEpoch: 1})
	require.NoError(t, err)

	token, err := svc.IssueCapability(context.Background(), authz, time.Minute, "")
	require.NoError(t, err)

	// Before the restore: the capability is accepted.
	_, err = svc.ParsePlaybackToken(context.Background(), token, "c1", "")
	require.NoError(t, err)

	_, err = store.Rotate(context.Background())
	require.NoError(t, err)

	_, err = svc.ParsePlaybackToken(context.Background(), token, "c1", "")
	assert.ErrorIs(t, err, ErrRestoreEpochStale,
		"a capability minted before the restore must be refused after it")
}

// Nothing is minted without a readable authority: a credential with no recovery
// epoch could never be invalidated by a restore.
func TestAuthService_MintingFailsWhenTheRestoreEpochIsUnreadable(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil, WithRestoreEpochStore(unreadableEpochAuthority{}))

	_, err := svc.generateWalletToken("0xwallet")
	assert.ErrorIs(t, err, ErrRestoreEpochUnreadable)

	resolver := &countingSnapshotResolver{script: []PolicySnapshot{
		{ContentID: "c1", Rules: []middleware.GatingRule{authzRule(1)}, Version: 0, VerifiedAt: time.Now()},
	}}
	h := newPlaybackTestAuthorizer(t, &quantityVerifier{quantity: big.NewInt(1)}, resolver)
	authz, err := h.AuthorizePlayback(context.Background(), "c1", capSessionWallet, SessionBinding{SessionID: newSessionID(t, svc), AuthEpoch: 1})
	require.NoError(t, err)

	_, err = svc.IssueCapability(context.Background(), authz, time.Minute, "")
	assert.ErrorIs(t, err, ErrRestoreEpochUnreadable)
}

// The access-service check is the same rule the transports apply, so a caller
// can ask the service directly and get the identical verdict.
func TestAuthService_RequireCurrentRestoreEpoch(t *testing.T) {
	svc, store := newRestoreEpochTestService(t, stg.NewMemoryRestoreEpochStore())

	current, err := store.Current(context.Background())
	require.NoError(t, err)

	require.NoError(t, svc.requireCurrentRestoreEpoch(current.Epoch),
		"the current epoch must be accepted")
	assert.ErrorIs(t, svc.requireCurrentRestoreEpoch(""), ErrRestoreEpochClaimMissing)
	assert.ErrorIs(t, svc.requireCurrentRestoreEpoch("00000000-0000-4000-8000-000000000000"), ErrRestoreEpochStale)

	unreadable := NewAuthService(signTypeTestSecret, nil, WithRestoreEpochStore(unreadableEpochAuthority{}))
	assert.ErrorIs(t, unreadable.requireCurrentRestoreEpoch(current.Epoch), ErrRestoreEpochUnreadable)
}

var _ = errors.New
