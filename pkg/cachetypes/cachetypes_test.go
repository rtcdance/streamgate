package cachetypes

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCacheBackendInterface(t *testing.T) {
	var _ CacheBackend = (*mockCache)(nil)
}

type mockCache struct {
	data map[string]interface{}
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string]interface{})}
}

func (m *mockCache) Get(key string) (interface{}, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (m *mockCache) Set(key string, value interface{}) error {
	m.data[key] = value
	return nil
}

func (m *mockCache) SetWithExpiration(key string, value interface{}, ttl time.Duration) error {
	m.data[key] = value
	return nil
}

func (m *mockCache) Delete(key string) error {
	delete(m.data, key)
	return nil
}

func TestMockCache_SetAndGet(t *testing.T) {
	c := newMockCache()

	err := c.Set("key1", "value1")
	assert.NoError(t, err)

	v, err := c.Get("key1")
	assert.NoError(t, err)
	assert.Equal(t, "value1", v)
}

func TestMockCache_GetMissing(t *testing.T) {
	c := newMockCache()

	v, err := c.Get("nonexistent")
	assert.NoError(t, err)
	assert.Nil(t, v)
}

func TestMockCache_SetWithExpiration(t *testing.T) {
	c := newMockCache()

	err := c.SetWithExpiration("key1", "value1", time.Minute)
	assert.NoError(t, err)

	v, err := c.Get("key1")
	assert.NoError(t, err)
	assert.Equal(t, "value1", v)
}

func TestMockCache_Delete(t *testing.T) {
	c := newMockCache()

	_ = c.Set("key1", "value1")
	err := c.Delete("key1")
	assert.NoError(t, err)

	v, err := c.Get("key1")
	assert.NoError(t, err)
	assert.Nil(t, v)
}