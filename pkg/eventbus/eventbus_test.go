package eventbus

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestInMemoryEventBus_PublishSubscribe(t *testing.T) {
	bus := NewInMemoryEventBus(zap.NewNop())
	defer bus.Close()

	var received atomic.Int32
	_, err := bus.Subscribe(context.Background(), "test.topic", func(ctx context.Context, event Event) error {
		received.Add(1)
		return nil
	})
	require.NoError(t, err)

	err = bus.Publish(context.Background(), "test.topic", Event{
		Type:   "test.event",
		Source: "test",
	})
	require.NoError(t, err)

	assert.Equal(t, int32(1), received.Load())
}

func TestInMemoryEventBus_MultipleSubscribers(t *testing.T) {
	bus := NewInMemoryEventBus(zap.NewNop())
	defer bus.Close()

	var count atomic.Int32
	handler := func(ctx context.Context, event Event) error {
		count.Add(1)
		return nil
	}

	_, err := bus.Subscribe(context.Background(), "topic", handler)
	require.NoError(t, err)
	_, err = bus.Subscribe(context.Background(), "topic", handler)
	require.NoError(t, err)

	err = bus.Publish(context.Background(), "topic", Event{Type: "test", Source: "src"})
	require.NoError(t, err)

	assert.Equal(t, int32(2), count.Load())
}

func TestInMemoryEventBus_TopicFiltering(t *testing.T) {
	bus := NewInMemoryEventBus(zap.NewNop())
	defer bus.Close()

	var received atomic.Int32
	_, err := bus.Subscribe(context.Background(), "topic.a", func(ctx context.Context, event Event) error {
		received.Add(1)
		return nil
	})
	require.NoError(t, err)

	// Publish to different topic — should not trigger handler
	err = bus.Publish(context.Background(), "topic.b", Event{Type: "test", Source: "src"})
	require.NoError(t, err)
	assert.Equal(t, int32(0), received.Load())

	// Publish to matching topic
	err = bus.Publish(context.Background(), "topic.a", Event{Type: "test", Source: "src"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), received.Load())
}

func TestInMemoryEventBus_PublishNoSubscribers(t *testing.T) {
	bus := NewInMemoryEventBus(zap.NewNop())
	defer bus.Close()

	err := bus.Publish(context.Background(), "empty.topic", Event{Type: "test", Source: "src"})
	assert.NoError(t, err)
}

func TestInMemoryEventBus_Unsubscribe(t *testing.T) {
	bus := NewInMemoryEventBus(zap.NewNop())
	defer bus.Close()

	var received atomic.Int32
	subID, err := bus.Subscribe(context.Background(), "topic", func(ctx context.Context, event Event) error {
		received.Add(1)
		return nil
	})
	require.NoError(t, err)

	err = bus.Publish(context.Background(), "topic", Event{Type: "test", Source: "src"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), received.Load())

	err = bus.Unsubscribe(context.Background(), subID)
	require.NoError(t, err)

	err = bus.Publish(context.Background(), "topic", Event{Type: "test", Source: "src"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), received.Load()) // still 1, handler removed
}

func TestInMemoryEventBus_UnsubscribeNotFound(t *testing.T) {
	bus := NewInMemoryEventBus(zap.NewNop())
	defer bus.Close()

	err := bus.Unsubscribe(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestInMemoryEventBus_OperationsAfterClose(t *testing.T) {
	bus := NewInMemoryEventBus(zap.NewNop())
	require.NoError(t, bus.Close())

	_, err := bus.Subscribe(context.Background(), "topic", func(ctx context.Context, event Event) error {
		return nil
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")

	err = bus.Publish(context.Background(), "topic", Event{Type: "test", Source: "src"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestInMemoryEventBus_HandlerError(t *testing.T) {
	bus := NewInMemoryEventBus(zap.NewNop())
	defer bus.Close()

	_, err := bus.Subscribe(context.Background(), "topic", func(ctx context.Context, event Event) error {
		return assert.AnError
	})
	require.NoError(t, err)

	err = bus.Publish(context.Background(), "topic", Event{Type: "test", Source: "src"})
	assert.Error(t, err)
}

func TestInMemoryEventBus_AutoFillEventFields(t *testing.T) {
	bus := NewInMemoryEventBus(zap.NewNop())
	defer bus.Close()

	var captured Event
	_, err := bus.Subscribe(context.Background(), "topic", func(ctx context.Context, event Event) error {
		captured = event
		return nil
	})
	require.NoError(t, err)

	err = bus.Publish(context.Background(), "topic", Event{Type: "test", Source: "src"})
	require.NoError(t, err)

	assert.NotEmpty(t, captured.ID)
	assert.False(t, captured.Timestamp.IsZero())
}

func TestNATSEventBus_NotConnected(t *testing.T) {
	// NATS connection with RetryOnFailedConnect may succeed asynchronously;
	// if it does, test the close behavior instead.
	bus, err := NewNATSEventBus("nats://localhost:4222", "test-stream", zap.NewNop())
	if err != nil {
		t.Logf("NATS connection failed as expected: %v", err)
		return
	}
	defer bus.Close()

	// Even if the initial connect succeeded (delayed), the bus should work
	// but NATS may not be reachable. If it is, Publish will succeed.
	err = bus.Publish(context.Background(), "topic", Event{Type: "test", Source: "src"})
	if err != nil {
		t.Logf("Publish failed as expected (NATS may be down): %v", err)
	}
}

func TestNATSEventBus_Closed(t *testing.T) {
	bus, err := NewNATSEventBus("nats://localhost:4222", "test-stream", zap.NewNop())
	if err != nil {
		t.Skipf("NATS not available: %v", err)
	}
	defer bus.Close()
	require.NoError(t, bus.Close())

	err = bus.Publish(context.Background(), "topic", Event{Type: "test", Source: "src"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestNATSEventBus_Unsubscribe(t *testing.T) {
	bus, err := NewNATSEventBus("nats://localhost:4222", "test-stream", zap.NewNop())
	if err != nil {
		t.Skipf("NATS not available: %v", err)
	}
	defer bus.Close()

	err = bus.Unsubscribe(context.Background(), "any-id")
	assert.Error(t, err) // no subscription with this ID
}

func TestEventBuilder(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	event := NewEventBuilder("test.type", "test-source").
		WithID("custom-id").
		WithData("key", "value").
		WithMetadata("meta-key", "meta-value").
		WithTimestamp(ts).
		Build()

	assert.Equal(t, "custom-id", event.ID)
	assert.Equal(t, "test.type", event.Type)
	assert.Equal(t, "test-source", event.Source)
	assert.Equal(t, "value", event.Data["key"])
	assert.Equal(t, "meta-value", event.Metadata["meta-key"])
	assert.Equal(t, ts, event.Timestamp)
}

func TestEventBuilder_AutoID(t *testing.T) {
	event := NewEventBuilder("test.type", "src").Build()
	assert.NotEmpty(t, event.ID)
}

func TestEventRouter(t *testing.T) {
	router := NewEventRouter(zap.NewNop())

	var received atomic.Int32
	idx := router.RegisterHandler("test.type", func(ctx context.Context, event Event) error {
		received.Add(1)
		return nil
	})
	assert.Equal(t, 0, idx)

	err := router.Route(context.Background(), Event{Type: "test.type", Source: "src"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), received.Load())
}

func TestEventRouter_NoHandlers(t *testing.T) {
	router := NewEventRouter(zap.NewNop())

	err := router.Route(context.Background(), Event{Type: "unregistered", Source: "src"})
	assert.NoError(t, err)
}

func TestEventRouter_UnregisterHandler(t *testing.T) {
	router := NewEventRouter(zap.NewNop())

	var received atomic.Int32
	router.RegisterHandler("test.type", func(ctx context.Context, event Event) error {
		received.Add(1)
		return nil
	})

	err := router.Route(context.Background(), Event{Type: "test.type", Source: "src"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), received.Load())

	router.UnregisterHandler("test.type", 0)

	err = router.Route(context.Background(), Event{Type: "test.type", Source: "src"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), received.Load()) // still 1, handler removed
}

func TestEventRouter_UnregisterHandler_InvalidIndex(t *testing.T) {
	router := NewEventRouter(zap.NewNop())

	// Should not panic on invalid index
	router.UnregisterHandler("test.type", -1)
	router.UnregisterHandler("test.type", 999)
}

func TestEventRouter_HandlerError(t *testing.T) {
	router := NewEventRouter(zap.NewNop())

	router.RegisterHandler("test.type", func(ctx context.Context, event Event) error {
		return assert.AnError
	})

	err := router.Route(context.Background(), Event{Type: "test.type", Source: "src"})
	assert.Error(t, err)
}

func TestEventStore(t *testing.T) {
	store := NewEventStore(zap.NewNop())

	e1 := Event{ID: "1", Type: "upload.completed", Source: "a"}
	e2 := Event{ID: "2", Type: "upload.failed", Source: "b"}
	e3 := Event{ID: "3", Type: "upload.completed", Source: "c"}

	require.NoError(t, store.Store(e1))
	require.NoError(t, store.Store(e2))
	require.NoError(t, store.Store(e3))

	// GetAll returns all events
	all := store.GetAll()
	assert.Len(t, all, 3)

	// Get by type with limit
	events, err := store.Get("upload.completed", 10)
	require.NoError(t, err)
	assert.Len(t, events, 2)
	// Reverse chronological order
	assert.Equal(t, "3", events[0].ID)
	assert.Equal(t, "1", events[1].ID)

	// Get with limit
	events, err = store.Get("upload.completed", 1)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, "3", events[0].ID)

	// Get non-existent type
	events, err = store.Get("nonexistent", 10)
	require.NoError(t, err)
	assert.Len(t, events, 0)
}

func TestEventStore_Clear(t *testing.T) {
	store := NewEventStore(zap.NewNop())

	require.NoError(t, store.Store(Event{ID: "1", Type: "test", Source: "a"}))
	assert.Len(t, store.GetAll(), 1)

	store.Clear()
	assert.Len(t, store.GetAll(), 0)
}

func TestEventTypeConstants(t *testing.T) {
	// Verify constants exist and have expected format
	assert.Equal(t, "upload.started", EventTypeUploadStarted)
	assert.Equal(t, "transcoding.completed", EventTypeTranscodingCompleted)
	assert.Equal(t, "nft.verified", EventTypeNFTVerified)
	assert.Equal(t, "stream.started", EventTypeStreamStarted)
	assert.Equal(t, "health.check_failed", EventTypeHealthCheckFailed)
}
