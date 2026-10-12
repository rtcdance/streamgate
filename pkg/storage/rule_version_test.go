package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingAuthority records how often the authority was consulted and lets a
// test control what it answers.
//
// It shares the test's clock: VerifiedAt must come from the SAME time source as
// the store under test, or the age arithmetic compares two unrelated timelines.
type countingAuthority struct {
	mu      sync.Mutex
	version int64
	err     error
	reads   int
	bumps   int
	now     func() time.Time
}

func (c *countingAuthority) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *countingAuthority) Read(_ context.Context, _ string) (RuleVersion, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	if c.err != nil {
		return RuleVersion{}, c.err
	}
	return RuleVersion{Version: c.version, VerifiedAt: c.clock()}, nil
}

func (c *countingAuthority) Bump(_ context.Context, _ string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bumps++
	c.version++
	return c.version, nil
}

func (c *countingAuthority) readCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

// A cached answer younger than the bound is served without touching the
// authority, and reading it does NOT refresh its age — otherwise a version that
// is merely being used would look perpetually fresh.
func TestCachingRuleVersionStore_CacheHitWithinBound(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return now }
	authority := &countingAuthority{version: 7, now: clock}
	cache := NewMemoryRuleVersionCache()

	store := NewCachingRuleVersionStore(authority, cache, 5*time.Second).WithClock(clock)

	first, err := store.Read(context.Background(), "content-1")
	require.NoError(t, err)
	assert.EqualValues(t, 7, first.Version)
	assert.Equal(t, 1, authority.readCount())

	// 3s later: still within the bound, served from cache.
	now = now.Add(3 * time.Second)
	second, err := store.Read(context.Background(), "content-1")
	require.NoError(t, err)
	assert.EqualValues(t, 7, second.Version)
	assert.Equal(t, 1, authority.readCount(), "a fresh-enough cache entry must not re-read the authority")

	// The cached entry's age is still measured from the ORIGINAL authority read,
	// so 3s more (6s total) is past the bound even though it was just read.
	now = now.Add(3 * time.Second)
	_, err = store.Read(context.Background(), "content-1")
	require.NoError(t, err)
	assert.Equal(t, 2, authority.readCount(),
		"reading the cache must not refresh the age; 6s total is past the 5s bound")
}

// Exactly at the bound is still usable: the bound is a maximum, and rejecting
// the documented value would make it unusable.
func TestCachingRuleVersionStore_ExactlyAtTheBoundIsUsable(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return now }
	authority := &countingAuthority{version: 3, now: clock}
	cache := NewMemoryRuleVersionCache()

	store := NewCachingRuleVersionStore(authority, cache, 5*time.Second).WithClock(clock)

	_, err := store.Read(context.Background(), "content-1")
	require.NoError(t, err)

	now = now.Add(5 * time.Second)
	_, err = store.Read(context.Background(), "content-1")
	require.NoError(t, err)
	assert.Equal(t, 1, authority.readCount(), "exactly at the bound must still be a hit")
}

// An unreadable authority is a REFUSAL, not a fallback to the expired copy:
// serving the stale value would extend the window the bound exists to cap.
func TestCachingRuleVersionStore_UnreadableAuthorityIsRefused(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return now }
	authority := &countingAuthority{version: 4, now: clock}
	cache := NewMemoryRuleVersionCache()

	store := NewCachingRuleVersionStore(authority, cache, 5*time.Second).WithClock(clock)

	_, err := store.Read(context.Background(), "content-1")
	require.NoError(t, err)

	authority.mu.Lock()
	authority.err = ErrRuleVersionUnavailable
	authority.mu.Unlock()

	now = now.Add(10 * time.Second)
	_, err = store.Read(context.Background(), "content-1")
	assert.ErrorIs(t, err, ErrRuleVersionUnavailable,
		"an expired entry must not be served when the authority cannot be read")
}

// A late write must not move the cached version backwards, or a newer rule
// change would be undone by an older one arriving afterwards.
func TestRuleVersionCache_RefusesToGoBackwards(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryRuleVersionCache()

	require.NoError(t, cache.SetIfNotOlder(ctx, "content-1", RuleVersion{Version: 5, VerifiedAt: time.Now()}))
	require.NoError(t, cache.SetIfNotOlder(ctx, "content-1", RuleVersion{Version: 4, VerifiedAt: time.Now()}))

	got, ok := cache.Get(ctx, "content-1")
	require.True(t, ok)
	assert.EqualValues(t, 5, got.Version, "an older version must not overwrite a newer one")

	// A newer version does move it forward.
	require.NoError(t, cache.SetIfNotOlder(ctx, "content-1", RuleVersion{Version: 6, VerifiedAt: time.Now()}))
	got, _ = cache.Get(ctx, "content-1")
	assert.EqualValues(t, 6, got.Version)
}

// Bumping publishes the committed version so the next read is a cache hit.
func TestCachingRuleVersionStore_BumpPublishesTheCommittedVersion(t *testing.T) {
	authority := &countingAuthority{version: 0}
	cache := NewMemoryRuleVersionCache()
	store := NewCachingRuleVersionStore(authority, cache, 5*time.Second)

	version, err := store.Bump(context.Background(), "content-1")
	require.NoError(t, err)
	assert.EqualValues(t, 1, version)

	got, err := store.Read(context.Background(), "content-1")
	require.NoError(t, err)
	assert.EqualValues(t, 1, got.Version)
	assert.Equal(t, 0, authority.readCount(), "the bump published the version, so no re-read is needed")
}

// The version value round-trips through its serialised form with the authority
// read time intact, since that time is what the age bound is measured from.
func TestRuleVersionValue_RoundTripsTheReadTime(t *testing.T) {
	at := time.Unix(1_700_000_000, 123456789)
	raw := formatRuleVersionValue(RuleVersion{Version: 42, VerifiedAt: at})

	version, verifiedAt, err := parseRuleVersionValue(raw)
	require.NoError(t, err)
	assert.EqualValues(t, 42, version)
	assert.True(t, verifiedAt.Equal(at), "the authority read time must survive the round trip")
}

func TestRuleVersionValue_RejectsMalformedInput(t *testing.T) {
	for _, raw := range []string{"", "42", "abc:def", "42:"} {
		_, _, err := parseRuleVersionValue(raw)
		assert.Error(t, err, "malformed value %q must be rejected", raw)
	}
}

// The Postgres authority refuses to answer without a database rather than
// returning a version that was never read.
func TestPostgresRuleVersionStore_WithoutADatabaseIsUnavailable(t *testing.T) {
	store := NewPostgresRuleVersionStore(nil)

	_, err := store.Read(context.Background(), "content-1")
	assert.ErrorIs(t, err, ErrRuleVersionUnavailable)

	_, err = store.Bump(context.Background(), "content-1")
	assert.ErrorIs(t, err, ErrRuleVersionUnavailable)
}

// A zero/negative bound falls back to the policy value instead of disabling the
// check — a store built without configuration must not have no bound at all.
func TestCachingRuleVersionStore_ZeroBoundUsesThePolicy(t *testing.T) {
	store := NewCachingRuleVersionStore(&countingAuthority{}, NewMemoryRuleVersionCache(), 0)
	assert.Equal(t, DefaultRuleVersionMaxAge, store.MaxAge())
}

var _ = errors.New
