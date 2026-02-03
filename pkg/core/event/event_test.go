package event

import (
	"context"
	"errors"
	"sync"
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
		assert.NotNil(t, bus.handlers)
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

		err = bus.Subscribe(context.Background(), "test", handler)
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
		bus.Subscribe(context.Background(), "test", handler)
		bus.Subscribe(context.Background(), "test", handler)
		bus.Subscribe(context.Background(), "test", handler)

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

		err = bus.Subscribe(context.Background(), "test", handler)
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
}

func TestMemoryEventBus_Subscribe(t *testing.T) {
	t.Run("subscribe to event type", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		handler := func(ctx context.Context, event *Event) error {
			return nil
		}

		err = bus.Subscribe(context.Background(), "test", handler)
		assert.NoError(t, err)

		bus.mu.RLock()
		handlers := bus.handlers["test"]
		bus.mu.RUnlock()

		assert.Len(t, handlers, 1)
	})

	t.Run("subscribe multiple times", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		handler1 := func(ctx context.Context, event *Event) error { return nil }
		handler2 := func(ctx context.Context, event *Event) error { return nil }
		handler3 := func(ctx context.Context, event *Event) error { return nil }

		bus.Subscribe(context.Background(), "test", handler1)
		bus.Subscribe(context.Background(), "test", handler2)
		bus.Subscribe(context.Background(), "test", handler3)

		bus.mu.RLock()
		handlers := bus.handlers["test"]
		bus.mu.RUnlock()

		assert.Len(t, handlers, 3)
	})

	t.Run("subscribe to different event types", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		handler := func(ctx context.Context, event *Event) error { return nil }

		bus.Subscribe(context.Background(), "type1", handler)
		bus.Subscribe(context.Background(), "type2", handler)
		bus.Subscribe(context.Background(), "type3", handler)

		bus.mu.RLock()
		assert.Len(t, bus.handlers["type1"], 1)
		assert.Len(t, bus.handlers["type2"], 1)
		assert.Len(t, bus.handlers["type3"], 1)
		bus.mu.RUnlock()
	})
}

func TestMemoryEventBus_Unsubscribe(t *testing.T) {
	t.Run("unsubscribe from non-existent type", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		handler := func(ctx context.Context, event *Event) error { return nil }

		err = bus.Unsubscribe(context.Background(), "test", handler)
		assert.NoError(t, err)
	})

	t.Run("unsubscribe from existing type", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		handler := func(ctx context.Context, event *Event) error { return nil }

		err = bus.Subscribe(context.Background(), "test", handler)
		require.NoError(t, err)

		err = bus.Unsubscribe(context.Background(), "test", handler)
		assert.NoError(t, err)
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
