package storage

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// ErrRestoreEpochUnavailable means the authoritative restore epoch could not be
// read. Callers MUST treat it as a refusal: an epoch that cannot be checked is
// not an epoch that matches, and every credential minted before a restore has to
// stop verifying the moment the authority says a new epoch exists.
var ErrRestoreEpochUnavailable = errors.New("restore epoch authority is unavailable")

// RestoreEpoch is the recovery epoch together with the moment it was read FROM
// THE AUTHORITY.
//
// It is deliberately not "when this struct was built": every cache layer carries
// the original read time forward, so the total age of an epoch answer is
// measurable no matter how many layers it passed through.
//
// The relation to auth_epoch is a layering, not a duplication: auth_epoch lives
// in Redis and versions ONE login session (a logout moves it), while
// restore_epoch lives in Postgres and versions the WHOLE credential universe (a
// restore regenerates it). A session cannot outlive a restore.
type RestoreEpoch struct {
	Epoch      string
	VerifiedAt time.Time
}

// Age reports how old this epoch answer is.
func (e RestoreEpoch) Age(now time.Time) time.Duration {
	if e.VerifiedAt.IsZero() {
		return time.Duration(1<<62 - 1) // effectively "infinitely old"
	}
	return now.Sub(e.VerifiedAt)
}

// Unknown reports whether the struct carries no epoch at all.
func (e RestoreEpoch) Unknown() bool { return e.Epoch == "" }

// RestoreEpochStore is the authority for the restore epoch.
//
// Rotate is what a restore drill calls before the restored stack takes traffic:
// it generates and commits a NEW epoch, so every credential minted against the
// pre-restore state stops verifying.
type RestoreEpochStore interface {
	// Current returns the committed restore epoch.
	Current(ctx context.Context) (RestoreEpoch, error)
	// Rotate generates, commits and returns a new restore epoch.
	Rotate(ctx context.Context) (RestoreEpoch, error)
}

// RestoreEpochCache is a best-effort copy of the authority's answer. It stores
// the epoch AND the time the authority was read, because the age bound is
// measured from that read, not from the cache lookup.
type RestoreEpochCache interface {
	Get(ctx context.Context) (RestoreEpoch, bool)
	Set(ctx context.Context, e RestoreEpoch) error
	Delete(ctx context.Context) error
}

// PostgresRestoreEpochStore is the authority: a single row in
// auth_control_state.
//
// The single row is created by THIS code using the existing UUID library rather
// than a database expression, so the migration needs no pgcrypto/uuid-ossp
// extension and the value is a real random UUID either way.
type PostgresRestoreEpochStore struct {
	db DB
}

func NewPostgresRestoreEpochStore(db DB) *PostgresRestoreEpochStore {
	return &PostgresRestoreEpochStore{db: db}
}

func (p *PostgresRestoreEpochStore) Current(ctx context.Context) (RestoreEpoch, error) {
	if p.db == nil {
		return RestoreEpoch{}, ErrRestoreEpochUnavailable
	}
	var raw string
	err := p.db.QueryRow(ctx,
		"SELECT restore_epoch FROM auth_control_state WHERE id = 1").Scan(&raw)
	if err != nil {
		if isNoRows(err) {
			return p.ensure(ctx)
		}
		return RestoreEpoch{}, fmt.Errorf("failed to read restore epoch: %w", err)
	}
	return RestoreEpoch{Epoch: normalizeEpoch(raw), VerifiedAt: time.Now()}, nil
}

// ensure creates the single row with a UUID from the application's existing
// library, then re-reads it. Two processes may race the insert; the committed
// value is the authority, never the one this process proposed, so a loser
// returns the winner's epoch rather than a second one.
func (p *PostgresRestoreEpochStore) ensure(ctx context.Context) (RestoreEpoch, error) {
	candidate := uuid.New().String()
	if _, err := p.db.Exec(ctx, `
		INSERT INTO auth_control_state (id, restore_epoch, updated_at)
		VALUES (1, $1, NOW())
		ON CONFLICT (id) DO NOTHING
	`, candidate); err != nil {
		return RestoreEpoch{}, fmt.Errorf("failed to initialise restore epoch: %w", err)
	}
	var raw string
	if err := p.db.QueryRow(ctx,
		"SELECT restore_epoch FROM auth_control_state WHERE id = 1").Scan(&raw); err != nil {
		return RestoreEpoch{}, fmt.Errorf("failed to read restore epoch: %w", err)
	}
	return RestoreEpoch{Epoch: normalizeEpoch(raw), VerifiedAt: time.Now()}, nil
}

func (p *PostgresRestoreEpochStore) Rotate(ctx context.Context) (RestoreEpoch, error) {
	if p.db == nil {
		return RestoreEpoch{}, ErrRestoreEpochUnavailable
	}
	next := uuid.New().String()
	var raw string
	err := p.db.QueryRow(ctx, `
		INSERT INTO auth_control_state (id, restore_epoch, updated_at)
		VALUES (1, $1, NOW())
		ON CONFLICT (id) DO UPDATE
			SET restore_epoch = EXCLUDED.restore_epoch, updated_at = NOW()
		RETURNING restore_epoch
	`, next).Scan(&raw)
	if err != nil {
		return RestoreEpoch{}, fmt.Errorf("failed to rotate restore epoch: %w", err)
	}
	return RestoreEpoch{Epoch: normalizeEpoch(raw), VerifiedAt: time.Now()}, nil
}

// normalizeEpoch puts a stored value into the canonical form used for
// comparison. PostgreSQL renders uuid as lowercase-with-dashes, and the UUID
// library produces the same form, but the comparison must not depend on that.
func normalizeEpoch(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// MemoryRestoreEpochStore is the fallback authority when no database is
// available. It is process-local, so a restore that rotates the Postgres epoch
// is NOT visible here: in a strict profile the wiring refuses to fall back for
// exactly that reason.
type MemoryRestoreEpochStore struct {
	mu    sync.RWMutex
	epoch string
}

func NewMemoryRestoreEpochStore() *MemoryRestoreEpochStore {
	return &MemoryRestoreEpochStore{epoch: uuid.New().String()}
}

func (m *MemoryRestoreEpochStore) Current(_ context.Context) (RestoreEpoch, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return RestoreEpoch{Epoch: m.epoch, VerifiedAt: time.Now()}, nil
}

func (m *MemoryRestoreEpochStore) Rotate(_ context.Context) (RestoreEpoch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.epoch = uuid.New().String()
	return RestoreEpoch{Epoch: m.epoch, VerifiedAt: time.Now()}, nil
}

// DefaultRestoreEpochMaxAge is the total age a restore-epoch answer may reach
// across all layers. It is the bound SG-06 names, and it matches the positive
// cache bound the rest of the credential policy is computed from.
const DefaultRestoreEpochMaxAge = 5 * time.Second

// CachingRestoreEpochStore layers a cache in front of the authority while
// keeping the age bound honest.
//
// The rule it enforces: a cached answer is used only while it is younger than
// maxAge, where the age is measured from when the AUTHORITY was read. Reading
// the cache never refreshes that timestamp — otherwise an epoch that is merely
// being used would look perpetually fresh and a stale answer would live forever.
type CachingRestoreEpochStore struct {
	authority RestoreEpochStore
	cache     RestoreEpochCache
	maxAge    time.Duration
	// now is injectable so the age bound can be tested at its boundary without
	// sleeping.
	now func() time.Time
}

func NewCachingRestoreEpochStore(authority RestoreEpochStore, cache RestoreEpochCache, maxAge time.Duration) *CachingRestoreEpochStore {
	if maxAge <= 0 {
		maxAge = DefaultRestoreEpochMaxAge
	}
	return &CachingRestoreEpochStore{
		authority: authority,
		cache:     cache,
		maxAge:    maxAge,
		now:       time.Now,
	}
}

// WithClock replaces the time source (tests only).
func (c *CachingRestoreEpochStore) WithClock(now func() time.Time) *CachingRestoreEpochStore {
	c.now = now
	return c
}

// MaxAge reports the bound this store enforces.
func (c *CachingRestoreEpochStore) MaxAge() time.Duration { return c.maxAge }

func (c *CachingRestoreEpochStore) Current(ctx context.Context) (RestoreEpoch, error) {
	if c.cache != nil {
		if cached, ok := c.cache.Get(ctx); ok && !cached.Unknown() && cached.Age(c.now()) <= c.maxAge {
			return cached, nil
		}
	}

	// Missing or too old: re-read the authority. A failure here is a refusal —
	// serving the expired copy would extend the window the plan bounds.
	fresh, err := c.authority.Current(ctx)
	if err != nil {
		return RestoreEpoch{}, err
	}
	if fresh.Unknown() {
		// An authority that answers with no epoch is UNVERIFIABLE. Passing it on
		// would make "empty claim == empty authority" match, authorizing every
		// credential whose epoch could not be compared.
		return RestoreEpoch{}, ErrRestoreEpochUnavailable
	}
	if c.cache != nil {
		if err := c.cache.Set(ctx, fresh); err != nil {
			// The authority answered, so the answer stands; a cache write failure
			// only costs another authority read next time.
			return fresh, nil
		}
	}
	return fresh, nil
}

// Rotate commits a new epoch and immediately drops the cached copy, so the
// rotation is visible here without waiting out the age bound (other processes
// re-read the authority on their next lookup for the same reason).
func (c *CachingRestoreEpochStore) Rotate(ctx context.Context) (RestoreEpoch, error) {
	rotated, err := c.authority.Rotate(ctx)
	if err != nil {
		return RestoreEpoch{}, err
	}
	if c.cache != nil {
		_ = c.cache.Delete(ctx)
	}
	return rotated, nil
}

// MemoryRestoreEpochCache is the fallback cache. It is process-local, so it is
// only a performance layer; correctness comes from the age bound and the
// authority.
type MemoryRestoreEpochCache struct {
	mu    sync.RWMutex
	entry *RestoreEpoch
}

func NewMemoryRestoreEpochCache() *MemoryRestoreEpochCache {
	return &MemoryRestoreEpochCache{}
}

func (m *MemoryRestoreEpochCache) Get(_ context.Context) (RestoreEpoch, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.entry == nil {
		return RestoreEpoch{}, false
	}
	return *m.entry, true
}

func (m *MemoryRestoreEpochCache) Set(_ context.Context, e RestoreEpoch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := e
	m.entry = &copied
	return nil
}

func (m *MemoryRestoreEpochCache) Delete(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entry = nil
	return nil
}

// RedisRestoreEpochCache shares the cached epoch across processes. The value is
// "<epoch>:<verifiedAtUnixNano>" — the read time travels with the epoch so the
// age bound holds across instances rather than being reset by each one.
type RedisRestoreEpochCache struct {
	client *redis.Client
	ttl    time.Duration
}

// RestoreEpochCacheKey is the single shared key: there is exactly one restore
// epoch, not one per subject.
const RestoreEpochCacheKey = "streamgate:restore-epoch"

func NewRedisRestoreEpochCache(client *redis.Client) (*RedisRestoreEpochCache, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client is required")
	}
	// The record only has to outlive the age bound; keeping it longer would just
	// be a longer-lived stale copy.
	return &RedisRestoreEpochCache{client: client, ttl: 10 * DefaultRestoreEpochMaxAge}, nil
}

func (r *RedisRestoreEpochCache) Get(ctx context.Context) (RestoreEpoch, bool) {
	raw, err := r.client.Get(ctx, RestoreEpochCacheKey).Result()
	if err != nil {
		return RestoreEpoch{}, false
	}
	epoch, verifiedAt, err := parseRestoreEpochValue(raw)
	if err != nil {
		return RestoreEpoch{}, false
	}
	return RestoreEpoch{Epoch: epoch, VerifiedAt: verifiedAt}, true
}

func (r *RedisRestoreEpochCache) Set(ctx context.Context, e RestoreEpoch) error {
	if err := r.client.Set(ctx, RestoreEpochCacheKey, formatRestoreEpochValue(e), r.ttl).Err(); err != nil {
		return fmt.Errorf("failed to cache restore epoch: %w", err)
	}
	return nil
}

func (r *RedisRestoreEpochCache) Delete(ctx context.Context) error {
	return r.client.Del(ctx, RestoreEpochCacheKey).Err()
}

func formatRestoreEpochValue(e RestoreEpoch) string {
	return e.Epoch + ":" + strconv.FormatInt(e.VerifiedAt.UnixNano(), 10)
}

func parseRestoreEpochValue(raw string) (string, time.Time, error) {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", time.Time{}, fmt.Errorf("malformed restore epoch value")
	}
	nanos, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", time.Time{}, err
	}
	return parts[0], time.Unix(0, nanos), nil
}
