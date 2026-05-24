package solana

import (
	"fmt"
	"sync"
	"time"
)

type mockCache struct {
	mu   sync.RWMutex
	data map[string]interface{}
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string]interface{})}
}

func (m *mockCache) Get(key string) (interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("cache miss")
	}
	return val, nil
}

func (m *mockCache) Set(key string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *mockCache) SetWithExpiration(key string, value interface{}, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *mockCache) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}
