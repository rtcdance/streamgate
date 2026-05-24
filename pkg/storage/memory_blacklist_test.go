package storage

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryTokenBlacklist_RevokeAndCheck(t *testing.T) {
	bl := NewMemoryTokenBlacklist()
	defer bl.Close()

	assert.False(t, bl.IsRevoked(context.Background(), "jti-1"))

	err := bl.Revoke(context.Background(), "jti-1", time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.True(t, bl.IsRevoked(context.Background(), "jti-1"))
	assert.False(t, bl.IsRevoked(context.Background(), "jti-2"))
}

func TestMemoryTokenBlacklist_ExpiredEntry(t *testing.T) {
	bl := NewMemoryTokenBlacklist()
	defer bl.Close()

	err := bl.Revoke(context.Background(), "expired-jti", time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.False(t, bl.IsRevoked(context.Background(), "expired-jti"))
}

func TestMemoryTokenBlacklist_Close(t *testing.T) {
	bl := NewMemoryTokenBlacklist()
	require.NoError(t, bl.Close())
}

func TestMemoryTokenBlacklist_MultipleRevocations(t *testing.T) {
	bl := NewMemoryTokenBlacklist()
	defer bl.Close()

	for i := 0; i < 10; i++ {
		err := bl.Revoke(context.Background(), "jti-"+string(rune('0'+i)), time.Now().Add(time.Hour))
		require.NoError(t, err)
	}

	for i := 0; i < 10; i++ {
		assert.True(t, bl.IsRevoked(context.Background(), "jti-"+string(rune('0'+i))))
	}
	assert.False(t, bl.IsRevoked(context.Background(), "jti-missing"))
}

func TestMemoryTokenBlacklist_Overwrite(t *testing.T) {
	bl := NewMemoryTokenBlacklist()
	defer bl.Close()

	err := bl.Revoke(context.Background(), "jti-1", time.Now().Add(time.Hour))
	require.NoError(t, err)

	err = bl.Revoke(context.Background(), "jti-1", time.Now().Add(-time.Hour))
	require.NoError(t, err)

	assert.False(t, bl.IsRevoked(context.Background(), "jti-1"))
}

func TestMemoryTokenBlacklist_EvictExpired(t *testing.T) {
	bl := NewMemoryTokenBlacklist()
	defer bl.Close()

	bl.mu.Lock()
	bl.entries["expired-1"] = time.Now().Add(-time.Hour)
	bl.entries["expired-2"] = time.Now().Add(-2 * time.Hour)
	bl.entries["valid-1"] = time.Now().Add(time.Hour)
	bl.mu.Unlock()

	bl.evictExpired()

	bl.mu.RLock()
	_, hasExpired1 := bl.entries["expired-1"]
	_, hasExpired2 := bl.entries["expired-2"]
	_, hasValid1 := bl.entries["valid-1"]
	bl.mu.RUnlock()

	assert.False(t, hasExpired1)
	assert.False(t, hasExpired2)
	assert.True(t, hasValid1)
}

func TestMemoryTokenBlacklist_EvictExpired_AllValid(t *testing.T) {
	bl := NewMemoryTokenBlacklist()
	defer bl.Close()

	bl.mu.Lock()
	bl.entries["valid-1"] = time.Now().Add(time.Hour)
	bl.entries["valid-2"] = time.Now().Add(2 * time.Hour)
	bl.mu.Unlock()

	bl.evictExpired()

	bl.mu.RLock()
	count := len(bl.entries)
	bl.mu.RUnlock()

	assert.Equal(t, 2, count)
}

func TestMemoryTokenBlacklist_EvictExpired_Empty(t *testing.T) {
	bl := NewMemoryTokenBlacklist()
	defer bl.Close()

	assert.NotPanics(t, bl.evictExpired)
}

func TestMemoryTokenBlacklist_IsRevoked_ExpiredEntry(t *testing.T) {
	bl := NewMemoryTokenBlacklist()
	defer bl.Close()

	bl.mu.Lock()
	bl.entries["expired-jti"] = time.Now().Add(-time.Hour)
	bl.mu.Unlock()

	assert.False(t, bl.IsRevoked(context.Background(), "expired-jti"))

	bl.mu.RLock()
	_, exists := bl.entries["expired-jti"]
	bl.mu.RUnlock()
	assert.False(t, exists)
}

func TestMemoryTokenBlacklist_ConcurrentRevokeAndCheck(t *testing.T) {
	bl := NewMemoryTokenBlacklist()
	defer bl.Close()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			jti := fmt.Sprintf("jti-%d", id)
			_ = bl.Revoke(context.Background(), jti, time.Now().Add(time.Hour))
		}(i)
		go func(id int) {
			defer wg.Done()
			jti := fmt.Sprintf("jti-%d", id)
			_ = bl.IsRevoked(context.Background(), jti)
		}(i)
	}
	wg.Wait()
}
