package storage

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// SessionStore holds the authoritative epoch for each login session.
//
// The epoch is what revocation moves: bumping it invalidates every credential
// issued for that session at once, with no per-token bookkeeping. Availability
// is therefore checked POSITIVELY — a credential is valid only when its session
// exists here and its epoch matches. The absence of a revocation record is not
// evidence of validity.
type SessionStore interface {
	// Create records a new session at epoch 1.
	Create(ctx context.Context, sessionID string) error
	// Epoch reports the current epoch, and whether the session is known.
	Epoch(ctx context.Context, sessionID string) (int64, bool)
	// Bump moves the session to the next epoch and returns it.
	Bump(ctx context.Context, sessionID string) (int64, error)
}

// DefaultSessionTTL bounds how long a session record is kept. It only needs to
// outlive the credentials issued for it.
const DefaultSessionTTL = 7 * 24 * time.Hour

// MemorySessionStore is the fallback when no durable backend is available. It is
// NOT durable: a restart forgets every session, which silently un-revokes them.
type MemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]int64
}

func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{sessions: make(map[string]int64)}
}

func (m *MemorySessionStore) Create(_ context.Context, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sessionID] = 1
	return nil
}

func (m *MemorySessionStore) Epoch(_ context.Context, sessionID string) (int64, bool) {
	if sessionID == "" {
		return 0, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	epoch, ok := m.sessions[sessionID]
	return epoch, ok
}

func (m *MemorySessionStore) Bump(_ context.Context, sessionID string) (int64, error) {
	if sessionID == "" {
		return 0, fmt.Errorf("session id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return 0, fmt.Errorf("session not found: %s", sessionID)
	}
	m.sessions[sessionID]++
	return m.sessions[sessionID], nil
}

// RedisSessionStore keeps sessions in Redis so revocation survives a restart.
// A process-local map cannot do that, and losing the record on restart silently
// un-revokes every session.
type RedisSessionStore struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedisSessionStore(client *redis.Client) (*RedisSessionStore, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client is required")
	}
	return &RedisSessionStore{client: client, ttl: DefaultSessionTTL}, nil
}

func (r *RedisSessionStore) key(sessionID string) string {
	return "streamgate:session:" + sessionID
}

func (r *RedisSessionStore) Create(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	// SetNX so a replayed create cannot reset a session that already exists -
	// resetting the epoch would resurrect credentials that were revoked.
	set, err := r.client.SetNX(ctx, r.key(sessionID), 1, r.ttl).Result()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	if !set {
		return fmt.Errorf("session already exists: %s", sessionID)
	}
	return nil
}

func (r *RedisSessionStore) Epoch(ctx context.Context, sessionID string) (int64, bool) {
	if sessionID == "" {
		return 0, false
	}
	raw, err := r.client.Get(ctx, r.key(sessionID)).Result()
	if err != nil {
		return 0, false
	}
	epoch, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return epoch, true
}

func (r *RedisSessionStore) Bump(ctx context.Context, sessionID string) (int64, error) {
	if sessionID == "" {
		return 0, fmt.Errorf("session id is required")
	}
	// Only bump a session that exists: creating one here would turn a revocation
	// of an unknown session into a live one.
	if _, ok := r.Epoch(ctx, sessionID); !ok {
		return 0, fmt.Errorf("session not found: %s", sessionID)
	}
	epoch, err := r.client.Incr(ctx, r.key(sessionID)).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to bump session epoch: %w", err)
	}
	// Keep the record alive for as long as its credentials could be.
	_ = r.client.Expire(ctx, r.key(sessionID), r.ttl).Err()
	return epoch, nil
}
