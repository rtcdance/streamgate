package storage

import (
	"context"
	"sync"
	"time"
)

// MemoryChallengeStore stores challenges in-memory for local development and tests.
type MemoryChallengeStore struct {
	mu         sync.RWMutex
	challenges map[string]*WalletChallenge
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewMemoryChallengeStore creates a new in-memory challenge store.
func NewMemoryChallengeStore() *MemoryChallengeStore {
	m := &MemoryChallengeStore{
		challenges: make(map[string]*WalletChallenge),
		stopCh:     make(chan struct{}),
	}
	m.wg.Add(1)
	go m.cleanupLoop()
	return m
}

// Close stops the background cleanup goroutine.
func (m *MemoryChallengeStore) Close() error {
	select {
	case <-m.stopCh:
	default:
		close(m.stopCh)
	}
	m.wg.Wait()
	return nil
}

func (m *MemoryChallengeStore) cleanupLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.evictExpired()
		}
	}
}

func (m *MemoryChallengeStore) evictExpired() {
	now := time.Now()
	m.mu.Lock()
	for id, ch := range m.challenges {
		if now.After(ch.ExpiresAt) {
			delete(m.challenges, id)
		}
	}
	m.mu.Unlock()
}

// SaveChallenge stores a challenge.
func (m *MemoryChallengeStore) SaveChallenge(ctx context.Context, challenge *WalletChallenge) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	challengeCopy := *challenge
	m.challenges[challenge.ID] = &challengeCopy
	return nil
}

// GetChallenge retrieves a challenge by ID.
func (m *MemoryChallengeStore) GetChallenge(ctx context.Context, id string) (*WalletChallenge, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	challenge, ok := m.challenges[id]
	if !ok {
		return nil, ErrChallengeNotFound
	}
	copyData := *challenge
	return &copyData, nil
}

// MarkChallengeUsed marks a challenge as used in the memory store.
func (m *MemoryChallengeStore) MarkChallengeUsed(ctx context.Context, id string, usedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	challenge, ok := m.challenges[id]
	if !ok {
		return ErrChallengeNotFound
	}
	if !challenge.UsedAt.IsZero() {
		return ErrChallengeUsed
	}
	challenge.UsedAt = usedAt
	return nil
}
