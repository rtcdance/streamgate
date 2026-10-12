package service

import (
	"context"
	"testing"

	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemorySessionStore_Contract(t *testing.T) {
	ctx := context.Background()
	store := stg.NewMemorySessionStore()

	// An unknown session is not valid, whatever epoch is claimed.
	_, ok := store.Epoch(ctx, "never-created")
	assert.False(t, ok)

	require.NoError(t, store.Create(ctx, "s1"))
	epoch, ok := store.Epoch(ctx, "s1")
	require.True(t, ok)
	assert.Equal(t, int64(1), epoch, "a new session starts at epoch 1")

	bumped, err := store.Bump(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), bumped)

	// Bumping an unknown session must not create one: that would turn revoking a
	// nonexistent session into a live one.
	_, err = store.Bump(ctx, "never-created")
	assert.Error(t, err)
}

// A revocation must be visible on the next request, not after the cache window.
func TestAuthService_RevokeSessionIsImmediate(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)

	id, err := svc.StartSession()
	require.NoError(t, err)
	require.True(t, svc.SessionValid(id, 1), "a fresh session must validate")

	newEpoch := svc.RevokeSession(id)
	require.Equal(t, int64(2), newEpoch)

	assert.False(t, svc.SessionValid(id, 1), "credentials from before the revocation must be refused")
	assert.True(t, svc.SessionValid(id, newEpoch), "the session itself is still live")
}

// The store is the authority: a session created outside the service must be
// visible without waiting for a cache entry to expire.
func TestAuthService_ReadsThroughToTheStore(t *testing.T) {
	store := stg.NewMemorySessionStore()
	svc := NewAuthService(signTypeTestSecret, nil, WithSessionStore(store))

	require.NoError(t, store.Create(context.Background(), "external-session"))

	epoch, ok := svc.SessionEpoch("external-session")
	assert.True(t, ok, "an uncached session must be read through to the store")
	assert.Equal(t, int64(1), epoch)
}

// An empty session id is never valid, and must not be looked up.
func TestAuthService_EmptySessionIsInvalid(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)
	_, ok := svc.SessionEpoch("")
	assert.False(t, ok)
	assert.False(t, svc.SessionValid("", 1))
	assert.Equal(t, SessionEpochUnknown, svc.RevokeSession(""))
}

// The default store is in-memory, so the service works without a backend while
// the wiring is explicit about durability.
func TestAuthService_DefaultSessionStore(t *testing.T) {
	svc := NewAuthService(signTypeTestSecret, nil)
	id, err := svc.StartSession()
	require.NoError(t, err)
	assert.True(t, svc.SessionValid(id, 1))
}
