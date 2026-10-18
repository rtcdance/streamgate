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

// countingEpochAuthority records how often the authority was consulted and lets
// a test control what it answers. It shares the test's clock so the age
// arithmetic compares one timeline.
type countingEpochAuthority struct {
	mu     sync.Mutex
	epoch  string
	err    error
	reads  int
	rotas  int
	now    func() time.Time
	next   string
	nextOK bool
}

func (c *countingEpochAuthority) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *countingEpochAuthority) Current(_ context.Context) (RestoreEpoch, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	if c.err != nil {
		return RestoreEpoch{}, c.err
	}
	return RestoreEpoch{Epoch: c.epoch, VerifiedAt: c.clock()}, nil
}

func (c *countingEpochAuthority) Rotate(_ context.Context) (RestoreEpoch, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rotas++
	if c.err != nil {
		return RestoreEpoch{}, c.err
	}
	if c.nextOK {
		c.epoch = c.next
	}
	return RestoreEpoch{Epoch: c.epoch, VerifiedAt: c.clock()}, nil
}

func (c *countingEpochAuthority) readCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

// A cached answer younger than the bound is served without touching the
// authority, and reading it does NOT refresh its age.
func TestCachingRestoreEpochStore_CacheHitWithinBound(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return now }
	authority := &countingEpochAuthority{epoch: "aaaaaaaa-0000-4000-8000-000000000001", now: clock}
	cache := NewMemoryRestoreEpochCache()

	store := NewCachingRestoreEpochStore(authority, cache, 5*time.Second).WithClock(clock)

	first, err := store.Current(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "aaaaaaaa-0000-4000-8000-000000000001", first.Epoch)
	assert.Equal(t, 1, authority.readCount())

	// 3s later: still within the bound, served from cache.
	now = now.Add(3 * time.Second)
	second, err := store.Current(context.Background())
	require.NoError(t, err)
	assert.Equal(t, first.Epoch, second.Epoch)
	assert.Equal(t, 1, authority.readCount(), "a fresh-enough cache entry must not re-read the authority")

	// The cached entry's age is still measured from the ORIGINAL authority read,
	// so 3s more (6s total) is past the bound even though it was just read.
	now = now.Add(3 * time.Second)
	_, err = store.Current(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, authority.readCount(),
		"reading the cache must not refresh the age; 6s total is past the 5s bound")
}

// Exactly at the bound is still usable: the bound is a maximum.
func TestCachingRestoreEpochStore_ExactlyAtTheBoundIsUsable(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return now }
	authority := &countingEpochAuthority{epoch: "bbbbbbbb-0000-4000-8000-000000000002", now: clock}

	store := NewCachingRestoreEpochStore(authority, NewMemoryRestoreEpochCache(), 5*time.Second).WithClock(clock)

	_, err := store.Current(context.Background())
	require.NoError(t, err)

	now = now.Add(5 * time.Second)
	_, err = store.Current(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, authority.readCount(), "exactly at the bound must still be a hit")
}

// An unreadable authority is a REFUSAL, not a fallback to the expired copy: an
// epoch that cannot be checked is not an epoch that matches, and serving the
// stale value would keep pre-restore credentials alive past the restore.
func TestCachingRestoreEpochStore_UnreadableAuthorityIsRefused(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return now }
	authority := &countingEpochAuthority{epoch: "cccccccc-0000-4000-8000-000000000003", now: clock}

	store := NewCachingRestoreEpochStore(authority, NewMemoryRestoreEpochCache(), 5*time.Second).WithClock(clock)

	_, err := store.Current(context.Background())
	require.NoError(t, err)

	authority.mu.Lock()
	authority.err = ErrRestoreEpochUnavailable
	authority.mu.Unlock()

	now = now.Add(10 * time.Second)
	_, err = store.Current(context.Background())
	assert.ErrorIs(t, err, ErrRestoreEpochUnavailable,
		"an expired entry must not be served when the authority cannot be read")
}

// Rotate must be visible on the VERY NEXT read, not after the age bound: the
// drill rotates the epoch before the restored stack takes traffic, so a cached
// pre-restore value would keep old credentials alive for up to maxAge.
func TestCachingRestoreEpochStore_RotateIsVisibleImmediately(t *testing.T) {
	oldEpoch := "dddddddd-0000-4000-8000-000000000004"
	newEpoch := "eeeeeeee-0000-4000-8000-000000000005"
	authority := &countingEpochAuthority{epoch: oldEpoch, next: newEpoch, nextOK: true}
	store := NewCachingRestoreEpochStore(authority, NewMemoryRestoreEpochCache(), 5*time.Second)

	before, err := store.Current(context.Background())
	require.NoError(t, err)
	assert.Equal(t, oldEpoch, before.Epoch)

	rotated, err := store.Rotate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, newEpoch, rotated.Epoch)

	after, err := store.Current(context.Background())
	require.NoError(t, err)
	assert.Equal(t, newEpoch, after.Epoch,
		"a rotation must invalidate the cached copy at once, not after the age bound")
	assert.Equal(t, 2, authority.readCount(), "the next read must consult the authority again")
}

// An empty cached epoch is never an answer: treating it as "current" would
// authorize every credential whose claim could not be compared, so it must be
// ignored in favour of the authority.
func TestCachingRestoreEpochStore_AnEmptyCachedEpochIsIgnored(t *testing.T) {
	realEpoch := "ffffffff-0000-4000-8000-000000000006"
	authority := &countingEpochAuthority{epoch: realEpoch}

	cache := NewMemoryRestoreEpochCache()
	require.NoError(t, cache.Set(context.Background(), RestoreEpoch{Epoch: "", VerifiedAt: time.Now()}))
	store := NewCachingRestoreEpochStore(authority, cache, 5*time.Second)

	got, err := store.Current(context.Background())
	require.NoError(t, err)
	assert.Equal(t, realEpoch, got.Epoch, "an empty cached epoch must be ignored in favour of the authority")
	assert.Equal(t, 1, authority.readCount())
}

// An authority that answers with no epoch is UNVERIFIABLE and must be refused:
// "empty claim == empty authority" would match and authorize everything.
func TestCachingRestoreEpochStore_AnEmptyAuthorityAnswerIsRefused(t *testing.T) {
	store := NewCachingRestoreEpochStore(&countingEpochAuthority{epoch: ""}, NewMemoryRestoreEpochCache(), 5*time.Second)

	_, err := store.Current(context.Background())
	assert.ErrorIs(t, err, ErrRestoreEpochUnavailable)
}

// A zero/negative bound falls back to the policy value instead of disabling the
// check.
func TestCachingRestoreEpochStore_ZeroBoundUsesThePolicy(t *testing.T) {
	store := NewCachingRestoreEpochStore(&countingEpochAuthority{epoch: "x"}, NewMemoryRestoreEpochCache(), 0)
	assert.Equal(t, DefaultRestoreEpochMaxAge, store.MaxAge())
	assert.Equal(t, 5*time.Second, DefaultRestoreEpochMaxAge, "the policy bound the plan names")
}

// The epoch value round-trips through its serialised form with the authority
// read time intact, since that time is what the age bound is measured from.
func TestRestoreEpochValue_RoundTripsTheReadTime(t *testing.T) {
	at := time.Unix(1_700_000_000, 123456789)
	epoch := "11111111-0000-4000-8000-000000000007"
	raw := formatRestoreEpochValue(RestoreEpoch{Epoch: epoch, VerifiedAt: at})

	gotEpoch, gotAt, err := parseRestoreEpochValue(raw)
	require.NoError(t, err)
	assert.Equal(t, epoch, gotEpoch)
	assert.True(t, gotAt.Equal(at), "the authority read time must survive the round trip")
}

func TestRestoreEpochValue_RejectsMalformedInput(t *testing.T) {
	for _, raw := range []string{"", ":123", "noseparator", "epoch:notanumber"} {
		_, _, err := parseRestoreEpochValue(raw)
		assert.Error(t, err, "malformed value %q must be rejected", raw)
	}
}

// The Postgres authority refuses to answer without a database rather than
// returning an epoch that was never read.
func TestPostgresRestoreEpochStore_WithoutADatabaseIsUnavailable(t *testing.T) {
	store := NewPostgresRestoreEpochStore(nil)

	_, err := store.Current(context.Background())
	assert.ErrorIs(t, err, ErrRestoreEpochUnavailable)

	_, err = store.Rotate(context.Background())
	assert.ErrorIs(t, err, ErrRestoreEpochUnavailable)
}

// The in-memory authority is a real authority inside one process: a rotation
// changes what it answers.
func TestMemoryRestoreEpochStore_RotateChangesTheEpoch(t *testing.T) {
	store := NewMemoryRestoreEpochStore()

	first, err := store.Current(context.Background())
	require.NoError(t, err)
	require.False(t, first.Unknown(), "the fallback authority must still carry a real UUID")

	rotated, err := store.Rotate(context.Background())
	require.NoError(t, err)
	assert.NotEqual(t, first.Epoch, rotated.Epoch, "a rotation must produce a new epoch")

	second, err := store.Current(context.Background())
	require.NoError(t, err)
	assert.Equal(t, rotated.Epoch, second.Epoch)
}

// normalizeEpoch must make the comparison form independent of the storage
// format, or a restore could be missed by a case/whitespace difference.
func TestNormalizeEpoch_IsCaseAndWhitespaceInsensitive(t *testing.T) {
	assert.Equal(t, "abc-def", normalizeEpoch("  ABC-DEF  "))
	assert.Equal(t, "abc-def", normalizeEpoch("abc-def"))
}

var _ = errors.New
