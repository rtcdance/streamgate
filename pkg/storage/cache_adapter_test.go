package storage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCacheAdapter(t *testing.T) {
	cs := NewCacheStorage(100)
	adapter := NewCacheAdapter(cs)
	require.NotNil(t, adapter)
	require.NotNil(t, adapter.inner)
}

func TestCacheAdapter_Get(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()
	adapter := NewCacheAdapter(cs)

	err := adapter.Set(context.Background(), "key1", "value1")
	require.NoError(t, err)

	val, err := adapter.Get(context.Background(), "key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", val)
}

func TestCacheAdapter_Get_Miss(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()
	adapter := NewCacheAdapter(cs)

	_, err := adapter.Get(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestCacheAdapter_SetWithExpiration(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()
	adapter := NewCacheAdapter(cs)

	err := adapter.SetWithExpiration(context.Background(), "expkey", "expvalue", 50*time.Millisecond)
	require.NoError(t, err)

	val, err := adapter.Get(context.Background(), "expkey")
	require.NoError(t, err)
	assert.Equal(t, "expvalue", val)

	time.Sleep(100 * time.Millisecond)

	_, err = adapter.Get(context.Background(), "expkey")
	assert.Error(t, err)
}

func TestCacheAdapter_Delete(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()
	adapter := NewCacheAdapter(cs)

	err := adapter.Set(context.Background(), "key1", "value1")
	require.NoError(t, err)

	err = adapter.Delete(context.Background(), "key1")
	require.NoError(t, err)

	_, err = adapter.Get(context.Background(), "key1")
	assert.Error(t, err)
}

func TestCacheAdapter_Exists(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()
	adapter := NewCacheAdapter(cs)

	err := adapter.Set(context.Background(), "key1", "value1")
	require.NoError(t, err)

	exists, err := adapter.Exists(context.Background(), "key1")
	assert.NoError(t, err)
	assert.True(t, exists)

	exists, err = adapter.Exists(context.Background(), "nonexistent")
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestCacheAdapter_Exists_Expired(t *testing.T) {
	cs := NewCacheStorage(100)
	defer cs.Close()
	adapter := NewCacheAdapter(cs)

	err := adapter.SetWithExpiration(context.Background(), "expkey", "expvalue", 10*time.Millisecond)
	require.NoError(t, err)

	time.Sleep(20 * time.Millisecond)

	exists, err := adapter.Exists(context.Background(), "expkey")
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestCacheAdapter_Close(t *testing.T) {
	cs := NewCacheStorage(100)
	adapter := NewCacheAdapter(cs)

	err := adapter.Close()
	assert.NoError(t, err)

	assert.True(t, cs.closed)
}

func TestCacheAdapter_ImplementsInterface(t *testing.T) {
	var _ Cache = NewCacheAdapter(NewCacheStorage(100))
}
