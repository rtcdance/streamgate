package event

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMemoryEventBus(t *testing.T) {
	t.Run("creates new event bus", func(t *testing.T) {
		bus, err := NewMemoryEventBus()

		require.NoError(t, err)
		assert.NotNil(t, bus)
		assert.NotNil(t, bus.subscriptions)
		assert.Equal(t, defaultMaxConcurrency, bus.maxConcurrency)
	})

	t.Run("creates with custom max concurrency", func(t *testing.T) {
		bus, err := NewMemoryEventBus(WithMaxConcurrency(32))
		require.NoError(t, err)
		assert.Equal(t, 32, bus.maxConcurrency)
		assert.Equal(t, 32, cap(bus.sem))
	})

	t.Run("creates with logger", func(t *testing.T) {
		bus, err := NewMemoryEventBus(WithLogger(nil))
		require.NoError(t, err)
		assert.NotNil(t, bus)
	})
}

func TestMemoryEventBus_Publish(t *testing.T) {
	t.Run("publish to no subscribers", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		event := &Event{
			Type:      "test",
			Source:    "test-source",
			Timestamp: time.Now().Unix(),
			Data:      map[string]interface{}{"key": "value"},
		}

		err = bus.Publish(context.Background(), event)
		assert.NoError(t, err)
	})

	t.Run("publish to single subscriber", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		received := make(chan *Event, 1)
		handler := func(ctx context.Context, event *Event) error {
			received <- event
			return nil
		}

		_, err = bus.Subscribe(context.Background(), "test", handler)
		require.NoError(t, err)

		event := &Event{
			Type:      "test",
			Source:    "test-source",
			Timestamp: time.Now().Unix(),
			Data:      map[string]interface{}{"key": "value"},
		}

		err = bus.Publish(context.Background(), event)
		assert.NoError(t, err)

		select {
		case e := <-received:
			assert.Equal(t, event.Type, e.Type)
			assert.Equal(t, event.Source, e.Source)
		case <-time.After(100 * time.Millisecond):
			assert.Fail(t, "did not receive event")
		}
	})

	t.Run("publish to multiple subscribers", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		var wg sync.WaitGroup
		receivedCount := 0
		mu := sync.Mutex{}

		handler := func(ctx context.Context, event *Event) error {
			mu.Lock()
			receivedCount++
			mu.Unlock()
			wg.Done()
			return nil
		}

		wg.Add(3)
		_, _ = bus.Subscribe(context.Background(), "test", handler)
		_, _ = bus.Subscribe(context.Background(), "test", handler)
		_, _ = bus.Subscribe(context.Background(), "test", handler)

		event := &Event{
			Type:      "test",
			Source:    "test-source",
			Timestamp: time.Now().Unix(),
			Data:      map[string]interface{}{"key": "value"},
		}

		err = bus.Publish(context.Background(), event)
		assert.NoError(t, err)

		wg.Wait()
		assert.Equal(t, 3, receivedCount)
	})

	t.Run("handler returns error", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		handler := func(ctx context.Context, event *Event) error {
			return errors.New("handler error")
		}

		_, err = bus.Subscribe(context.Background(), "test", handler)
		require.NoError(t, err)

		event := &Event{
			Type:      "test",
			Source:    "test-source",
			Timestamp: time.Now().Unix(),
			Data:      map[string]interface{}{"key": "value"},
		}

		err = bus.Publish(context.Background(), event)
		assert.NoError(t, err)
		time.Sleep(10 * time.Millisecond)
	})

	t.Run("semaphore limits concurrency", func(t *testing.T) {
		bus, err := NewMemoryEventBus(WithMaxConcurrency(2))
		require.NoError(t, err)

		var active atomic.Int64
		var maxActive atomic.Int64
		var wg sync.WaitGroup

		handler := func(ctx context.Context, event *Event) error {
			current := active.Add(1)
			for {
				old := maxActive.Load()
				if current <= old || maxActive.CompareAndSwap(old, current) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			active.Add(-1)
			wg.Done()
			return nil
		}

		for i := 0; i < 5; i++ {
			_, _ = bus.Subscribe(context.Background(), "test", handler)
		}

		wg.Add(5)
		event := &Event{Type: "test"}
		err = bus.Publish(context.Background(), event)
		assert.NoError(t, err)

		wg.Wait()
		assert.LessOrEqual(t, maxActive.Load(), int64(2))
	})
}

func TestMemoryEventBus_Subscribe(t *testing.T) {
	t.Run("subscribe to event type", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		handler := func(ctx context.Context, event *Event) error {
			return nil
		}

		subID, err := bus.Subscribe(context.Background(), "test", handler)
		assert.NoError(t, err)
		assert.NotEmpty(t, subID)

		bus.mu.RLock()
		_, exists := bus.subscriptions[subID]
		bus.mu.RUnlock()
		assert.True(t, exists)
	})

	t.Run("subscribe multiple times", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		handler1 := func(ctx context.Context, event *Event) error { return nil }
		handler2 := func(ctx context.Context, event *Event) error { return nil }
		handler3 := func(ctx context.Context, event *Event) error { return nil }

		id1, _ := bus.Subscribe(context.Background(), "test", handler1)
		id2, _ := bus.Subscribe(context.Background(), "test", handler2)
		id3, _ := bus.Subscribe(context.Background(), "test", handler3)

		bus.mu.RLock()
		count := len(bus.subscriptions)
		bus.mu.RUnlock()

		assert.Equal(t, 3, count)
		assert.NotEqual(t, id1, id2)
		assert.NotEqual(t, id2, id3)
	})

	t.Run("subscribe to different event types", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		handler := func(ctx context.Context, event *Event) error { return nil }

		_, _ = bus.Subscribe(context.Background(), "type1", handler)
		_, _ = bus.Subscribe(context.Background(), "type2", handler)
		_, _ = bus.Subscribe(context.Background(), "type3", handler)

		bus.mu.RLock()
		count := len(bus.subscriptions)
		bus.mu.RUnlock()

		assert.Equal(t, 3, count)
	})
}

func TestMemoryEventBus_Unsubscribe(t *testing.T) {
	t.Run("unsubscribe from non-existent subscription", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		err = bus.Unsubscribe(context.Background(), "sub-999")
		assert.NoError(t, err)
	})

	t.Run("unsubscribe from existing subscription", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		handler := func(ctx context.Context, event *Event) error { return nil }

		subID, err := bus.Subscribe(context.Background(), "test", handler)
		require.NoError(t, err)

		err = bus.Unsubscribe(context.Background(), subID)
		assert.NoError(t, err)

		bus.mu.RLock()
		_, exists := bus.subscriptions[subID]
		bus.mu.RUnlock()
		assert.False(t, exists)
	})

	t.Run("unsubscribe closure by ID works correctly", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		subID, err := bus.Subscribe(context.Background(), "test", func(ctx context.Context, event *Event) error {
			return nil
		})
		require.NoError(t, err)

		err = bus.Unsubscribe(context.Background(), subID)
		assert.NoError(t, err)

		bus.mu.RLock()
		count := len(bus.subscriptions)
		bus.mu.RUnlock()
		assert.Equal(t, 0, count)
	})
}

func TestMemoryEventBus_Close(t *testing.T) {
	t.Run("close event bus", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		err = bus.Close()
		assert.NoError(t, err)
	})

	t.Run("close multiple times", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		err = bus.Close()
		assert.NoError(t, err)

		err = bus.Close()
		assert.NoError(t, err)
	})
}

func TestEvent(t *testing.T) {
	t.Run("create event", func(t *testing.T) {
		event := &Event{
			Type:      "test-type",
			Source:    "test-source",
			Timestamp: time.Now().Unix(),
			Data:      map[string]interface{}{"key": "value"},
		}

		assert.Equal(t, "test-type", event.Type)
		assert.Equal(t, "test-source", event.Source)
		assert.NotNil(t, event.Data)
		assert.Equal(t, "value", event.Data["key"])
	})
}
