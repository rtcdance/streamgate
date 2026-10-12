package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// ErrRuleVersionUnavailable means the authoritative version could not be read.
// Callers MUST treat it as a refusal: a version that cannot be checked is not a
// version that matches.
var ErrRuleVersionUnavailable = errors.New("rule version authority is unavailable")

// RuleVersion is one content's authorization-rule version together with the
// moment it was read FROM THE AUTHORITY.
//
// VerifiedAt is deliberately not "when this struct was built": every cache layer
// carries the original read time forward, so the total age of a version answer
// is measurable no matter how many layers it passed through.
type RuleVersion struct {
	Version    int64
	VerifiedAt time.Time
}

// Age reports how old this version answer is.
func (v RuleVersion) Age(now time.Time) time.Duration {
	if v.VerifiedAt.IsZero() {
		return time.Duration(1<<62 - 1) // effectively "infinitely old"
	}
	return now.Sub(v.VerifiedAt)
}

// RuleVersionStore is the authority for rule versions.
//
// Bump MUST be committed in the same transaction as the rule change it
// describes; a version that can advance without the rules changing (or the
// reverse) tells a reader nothing.
type RuleVersionStore interface {
	// Bump advances the version for a content and returns the new value.
	Bump(ctx context.Context, contentID string) (int64, error)
	// Read returns the committed version for a content.
	Read(ctx context.Context, contentID string) (RuleVersion, error)
}

// RuleVersionCache is a best-effort copy of the authority's answer. It stores
// the version AND the time the authority was read, because the age bound is
// measured from that read, not from the cache lookup.
type RuleVersionCache interface {
	Get(ctx context.Context, contentID string) (RuleVersion, bool)
	// SetIfNotOlder stores the version unless the cache already holds a NEWER
	// one. A late update that arrives after a newer one must not move the
	// version backwards, or a revoked rule change could be resurrected.
	SetIfNotOlder(ctx context.Context, contentID string, v RuleVersion) error
	Delete(ctx context.Context, contentID string) error
}

// PostgresRuleVersionStore is the authority. Bump is monotonic by construction:
// the increment happens inside the statement, so two concurrent writers cannot
// produce the same version.
type PostgresRuleVersionStore struct {
	db DB
}

func NewPostgresRuleVersionStore(db DB) *PostgresRuleVersionStore {
	return &PostgresRuleVersionStore{db: db}
}

func (p *PostgresRuleVersionStore) Bump(ctx context.Context, contentID string) (int64, error) {
	if p.db == nil {
		return 0, ErrRuleVersionUnavailable
	}
	if contentID == "" {
		return 0, fmt.Errorf("content id is required")
	}
	var version int64
	err := p.db.QueryRow(ctx, `
		INSERT INTO content_rule_versions (content_id, version, updated_at)
		VALUES ($1, 1, NOW())
		ON CONFLICT (content_id) DO UPDATE
			SET version = content_rule_versions.version + 1, updated_at = NOW()
		RETURNING version
	`, contentID).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("failed to bump rule version: %w", err)
	}
	return version, nil
}

func (p *PostgresRuleVersionStore) Read(ctx context.Context, contentID string) (RuleVersion, error) {
	if p.db == nil {
		return RuleVersion{}, ErrRuleVersionUnavailable
	}
	if contentID == "" {
		return RuleVersion{}, fmt.Errorf("content id is required")
	}
	var version int64
	var updatedAt time.Time
	err := p.db.QueryRow(ctx,
		"SELECT version, updated_at FROM content_rule_versions WHERE content_id = $1",
		contentID).Scan(&version, &updatedAt)
	if err != nil {
		// No row means the content has never had a rule written: version 0 is a
		// real answer, not a failure.
		if isNoRows(err) {
			return RuleVersion{Version: 0, VerifiedAt: time.Now()}, nil
		}
		return RuleVersion{}, fmt.Errorf("failed to read rule version: %w", err)
	}
	return RuleVersion{Version: version, VerifiedAt: time.Now()}, nil
}

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no rows")
}

// CachingRuleVersionStore layers a cache in front of the authority while keeping
// the age bound honest.
//
// The rule it enforces: a cached answer is used only while it is younger than
// maxAge, where the age is measured from when the AUTHORITY was read. Reading
// the cache never refreshes that timestamp — otherwise a version that is merely
// being used would look perpetually fresh and a stale answer would live forever.
type CachingRuleVersionStore struct {
	authority RuleVersionStore
	cache     RuleVersionCache
	maxAge    time.Duration
	// now is injectable so the age bound can be tested at its boundary without
	// sleeping.
	now func() time.Time
}

func NewCachingRuleVersionStore(authority RuleVersionStore, cache RuleVersionCache, maxAge time.Duration) *CachingRuleVersionStore {
	if maxAge <= 0 {
		maxAge = DefaultRuleVersionMaxAge
	}
	return &CachingRuleVersionStore{
		authority: authority,
		cache:     cache,
		maxAge:    maxAge,
		now:       time.Now,
	}
}

// DefaultRuleVersionMaxAge is the total age a version answer may reach across
// all layers. It is the policy value SG-06 names.
const DefaultRuleVersionMaxAge = 5 * time.Second

// WithClock replaces the time source (tests only).
func (c *CachingRuleVersionStore) WithClock(now func() time.Time) *CachingRuleVersionStore {
	c.now = now
	return c
}

// MaxAge reports the bound this store enforces.
func (c *CachingRuleVersionStore) MaxAge() time.Duration { return c.maxAge }

func (c *CachingRuleVersionStore) Read(ctx context.Context, contentID string) (RuleVersion, error) {
	if c.cache != nil {
		if cached, ok := c.cache.Get(ctx, contentID); ok && cached.Age(c.now()) <= c.maxAge {
			return cached, nil
		}
	}

	// Missing or too old: re-read the authority. A failure here is a refusal —
	// serving the expired copy would extend the window the plan bounds.
	fresh, err := c.authority.Read(ctx, contentID)
	if err != nil {
		return RuleVersion{}, err
	}
	if c.cache != nil {
		if err := c.cache.SetIfNotOlder(ctx, contentID, fresh); err != nil {
			// The authority answered, so the answer stands; a cache write failure
			// only costs another authority read next time.
			return fresh, nil
		}
	}
	return fresh, nil
}

func (c *CachingRuleVersionStore) Bump(ctx context.Context, contentID string) (int64, error) {
	version, err := c.authority.Bump(ctx, contentID)
	if err != nil {
		return 0, err
	}
	c.Publish(ctx, contentID, version)
	return version, nil
}

// Publish records a version that was committed ELSEWHERE — by a rule write that
// bumped it inside its own SQL statement, for instance — so other processes stop
// using the previous rule set without waiting for their copy to age out.
//
// It is deliberately separate from Bump: publishing must never advance the
// authority, or a write path that already bumped in its own statement would
// count twice.
func (c *CachingRuleVersionStore) Publish(ctx context.Context, contentID string, version int64) {
	if c.cache == nil || contentID == "" {
		return
	}
	// SetIfNotOlder keeps a late write from undoing a newer one.
	_ = c.cache.SetIfNotOlder(ctx, contentID, RuleVersion{Version: version, VerifiedAt: c.now()})
}

// MemoryRuleVersionCache is the fallback cache. It is process-local, so it is
// only a performance layer; correctness comes from the age bound and the
// authority.
type MemoryRuleVersionCache struct {
	mu      sync.RWMutex
	entries map[string]RuleVersion
}

func NewMemoryRuleVersionCache() *MemoryRuleVersionCache {
	return &MemoryRuleVersionCache{entries: make(map[string]RuleVersion)}
}

func (m *MemoryRuleVersionCache) Get(_ context.Context, contentID string) (RuleVersion, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.entries[contentID]
	return v, ok
}

func (m *MemoryRuleVersionCache) SetIfNotOlder(_ context.Context, contentID string, v RuleVersion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.entries[contentID]; ok && existing.Version > v.Version {
		return nil
	}
	m.entries[contentID] = v
	return nil
}

func (m *MemoryRuleVersionCache) Delete(_ context.Context, contentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, contentID)
	return nil
}

// RedisRuleVersionCache shares the cached version across processes. The value is
// "<version>:<verifiedAtUnixNano>" — the read time travels with the version so
// the age bound holds across instances rather than being reset by each one.
type RedisRuleVersionCache struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedisRuleVersionCache(client *redis.Client) (*RedisRuleVersionCache, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client is required")
	}
	// The record only has to outlive the age bound; keeping it longer would just
	// be a longer-lived stale copy.
	return &RedisRuleVersionCache{client: client, ttl: 10 * DefaultRuleVersionMaxAge}, nil
}

func (r *RedisRuleVersionCache) key(contentID string) string {
	return "streamgate:rulever:" + contentID
}

func (r *RedisRuleVersionCache) Get(ctx context.Context, contentID string) (RuleVersion, bool) {
	raw, err := r.client.Get(ctx, r.key(contentID)).Result()
	if err != nil {
		return RuleVersion{}, false
	}
	version, verifiedAt, err := parseRuleVersionValue(raw)
	if err != nil {
		return RuleVersion{}, false
	}
	return RuleVersion{Version: version, VerifiedAt: verifiedAt}, true
}

// setIfNotOlderScript writes only when the stored version is not newer than the
// one being written. Doing this in Lua keeps the comparison and the write
// atomic, so two concurrent writers cannot interleave into a backwards move.
var setIfNotOlderScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if current then
    local stored = tonumber(string.match(current, '^(%-?%d+):'))
    if stored and stored > tonumber(ARGV[1]) then
        return 0
    end
end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
return 1
`)

func (r *RedisRuleVersionCache) SetIfNotOlder(ctx context.Context, contentID string, v RuleVersion) error {
	value := formatRuleVersionValue(v)
	err := setIfNotOlderScript.Run(ctx, r.client, []string{r.key(contentID)},
		v.Version, value, r.ttl.Milliseconds()).Err()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("failed to cache rule version: %w", err)
	}
	return nil
}

func (r *RedisRuleVersionCache) Delete(ctx context.Context, contentID string) error {
	return r.client.Del(ctx, r.key(contentID)).Err()
}

func formatRuleVersionValue(v RuleVersion) string {
	return strconv.FormatInt(v.Version, 10) + ":" + strconv.FormatInt(v.VerifiedAt.UnixNano(), 10)
}

func parseRuleVersionValue(raw string) (int64, time.Time, error) {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return 0, time.Time{}, fmt.Errorf("malformed rule version value")
	}
	version, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, time.Time{}, err
	}
	nanos, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, time.Time{}, err
	}
	return version, time.Unix(0, nanos), nil
}
