package event

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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
		bus, err := NewMemoryEventBus(WithLogger(zap.NewNop()))
		require.NoError(t, err)
		assert.NotNil(t, bus)
		assert.NotNil(t, bus.log)
	})

	t.Run("zero max concurrency uses default", func(t *testing.T) {
		bus, err := NewMemoryEventBus(WithMaxConcurrency(0))
		require.NoError(t, err)
		assert.Equal(t, defaultMaxConcurrency, bus.maxConcurrency)
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
		bus, err := NewMemoryEventBus(WithLogger(zap.NewNop()))
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

	t.Run("publish with cancelled context", func(t *testing.T) {
		bus, err := NewMemoryEventBus(WithMaxConcurrency(1))
		require.NoError(t, err)

		blocking := make(chan struct{})
		handler := func(ctx context.Context, event *Event) error {
			<-blocking
			return nil
		}

		_, _ = bus.Subscribe(context.Background(), "test", handler)

		require.NoError(t, bus.Publish(context.Background(), &Event{Type: "test"}))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		event := &Event{Type: "test"}
		err = bus.Publish(ctx, event)
		assert.Error(t, err)
		close(blocking)
		require.NoError(t, bus.Close())
	})

	t.Run("publish only delivers to matching type", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		testReceived := make(chan *Event, 1)
		otherReceived := make(chan *Event, 1)

		_, _ = bus.Subscribe(context.Background(), "test", func(ctx context.Context, e *Event) error {
			testReceived <- e
			return nil
		})
		_, _ = bus.Subscribe(context.Background(), "other", func(ctx context.Context, e *Event) error {
			otherReceived <- e
			return nil
		})

		event := &Event{Type: "test", Source: "src"}
		require.NoError(t, bus.Publish(context.Background(), event))

		select {
		case e := <-testReceived:
			assert.Equal(t, "test", e.Type)
		case <-time.After(100 * time.Millisecond):
			assert.Fail(t, "test subscriber should receive event")
		}

		select {
		case <-otherReceived:
			assert.Fail(t, "other subscriber should not receive event")
		case <-time.After(50 * time.Millisecond):
		}
	})

	t.Run("handler panic is recovered", func(t *testing.T) {
		bus, err := NewMemoryEventBus(WithLogger(zap.NewNop()))
		require.NoError(t, err)

		handler := func(ctx context.Context, event *Event) error {
			panic("handler panic")
		}

		_, _ = bus.Subscribe(context.Background(), "test", handler)

		event := &Event{Type: "test"}
		err = bus.Publish(context.Background(), event)
		assert.NoError(t, err)

		require.NoError(t, bus.Close())
	})
}

func TestMemoryEventBus_PublishSync(t *testing.T) {
	t.Run("publish sync to no subscribers", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		event := &Event{Type: "test"}
		err = bus.PublishSync(context.Background(), event)
		assert.NoError(t, err)
	})

	t.Run("publish sync to single subscriber", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		var received *Event
		_, _ = bus.Subscribe(context.Background(), "test", func(ctx context.Context, e *Event) error {
			received = e
			return nil
		})

		event := &Event{Type: "test", Source: "sync-src"}
		err = bus.PublishSync(context.Background(), event)
		assert.NoError(t, err)
		assert.Equal(t, "test", received.Type)
		assert.Equal(t, "sync-src", received.Source)
	})

	t.Run("publish sync handler error", func(t *testing.T) {
		bus, err := NewMemoryEventBus(WithLogger(zap.NewNop()))
		require.NoError(t, err)

		_, _ = bus.Subscribe(context.Background(), "test", func(ctx context.Context, e *Event) error {
			return errors.New("sync handler error")
		})

		event := &Event{Type: "test"}
		err = bus.PublishSync(context.Background(), event)
		assert.Error(t, err)
		assert.Equal(t, "sync handler error", err.Error())
	})

	t.Run("publish sync handler panic recovered", func(t *testing.T) {
		bus, err := NewMemoryEventBus(WithLogger(zap.NewNop()))
		require.NoError(t, err)

		_, _ = bus.Subscribe(context.Background(), "test", func(ctx context.Context, e *Event) error {
			panic("sync panic")
		})

		event := &Event{Type: "test"}
		err = bus.PublishSync(context.Background(), event)
		assert.NoError(t, err)
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

	t.Run("unsubscribed handler does not receive events", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		received := make(chan *Event, 1)
		subID, _ := bus.Subscribe(context.Background(), "test", func(ctx context.Context, e *Event) error {
			received <- e
			return nil
		})

		require.NoError(t, bus.Unsubscribe(context.Background(), subID))

		event := &Event{Type: "test"}
		require.NoError(t, bus.Publish(context.Background(), event))

		select {
		case <-received:
			assert.Fail(t, "should not receive event after unsubscribe")
		case <-time.After(50 * time.Millisecond):
		}
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

	t.Run("close waits for in-flight handlers", func(t *testing.T) {
		bus, err := NewMemoryEventBus()
		require.NoError(t, err)

		var handlerDone atomic.Bool
		_, _ = bus.Subscribe(context.Background(), "test", func(ctx context.Context, e *Event) error {
			time.Sleep(50 * time.Millisecond)
			handlerDone.Store(true)
			return nil
		})

		require.NoError(t, bus.Publish(context.Background(), &Event{Type: "test"}))
		require.NoError(t, bus.Close())

		assert.True(t, handlerDone.Load())
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

	t.Run("event JSON marshaling", func(t *testing.T) {
		event := &Event{
			Type:      "test-type",
			Source:    "test-source",
			Timestamp: 1234567890,
			Data:      map[string]interface{}{"key": "value", "num": float64(42)},
		}

		data, err := json.Marshal(event)
		require.NoError(t, err)

		var decoded Event
		require.NoError(t, json.Unmarshal(data, &decoded))
		assert.Equal(t, event.Type, decoded.Type)
		assert.Equal(t, event.Source, decoded.Source)
		assert.Equal(t, event.Timestamp, decoded.Timestamp)
	})

	t.Run("event constants", func(t *testing.T) {
		assert.Equal(t, "cache.warmed", EventTypeCacheWarmed)
		assert.Equal(t, "nft.verified", EventTypeNFTVerified)
		assert.Equal(t, "service.registered", EventTypeServiceRegistered)
		assert.Equal(t, "service.deregistered", EventTypeServiceDeregistered)
		assert.Equal(t, "streaming.started", EventTypeStreamingStarted)
		assert.Equal(t, "streaming.stopped", EventTypeStreamingStopped)
		assert.Equal(t, "metadata.created", EventTypeMetadataCreated)
		assert.Equal(t, "metadata.updated", EventTypeMetadataUpdated)
		assert.Equal(t, "metadata.deleted", EventTypeMetadataDeleted)
		assert.Equal(t, "job.submitted", EventTypeJobSubmitted)
		assert.Equal(t, "job.completed", EventTypeJobCompleted)
		assert.Equal(t, "job.failed", EventTypeJobFailed)
		assert.Equal(t, "alert.triggered", EventTypeAlertTriggered)
		assert.Equal(t, "alert.resolved", EventTypeAlertResolved)
	})
}

func TestSubscriber(t *testing.T) {
	t.Run("new subscriber", func(t *testing.T) {
		handler := func(e Event) {}
		sub := NewSubscriber("test", handler)

		assert.Equal(t, "test", sub.GetEventType())
	})

	t.Run("handle matching event", func(t *testing.T) {
		received := make(chan Event, 1)
		handler := func(e Event) { received <- e }
		sub := NewSubscriber("test", handler)

		sub.Handle(Event{Type: "test", Source: "src"})

		select {
		case e := <-received:
			assert.Equal(t, "test", e.Type)
		case <-time.After(100 * time.Millisecond):
			assert.Fail(t, "should receive event")
		}
	})

	t.Run("ignore non-matching event", func(t *testing.T) {
		received := make(chan Event, 1)
		handler := func(e Event) { received <- e }
		sub := NewSubscriber("test", handler)

		sub.Handle(Event{Type: "other"})

		select {
		case <-received:
			assert.Fail(t, "should not receive event for different type")
		case <-time.After(50 * time.Millisecond):
		}
	})
}

func TestPublisher(t *testing.T) {
	t.Run("creates new publisher", func(t *testing.T) {
		publisher := NewPublisher()
		assert.NotNil(t, publisher)
		assert.NotNil(t, publisher.subscribers)
	})

	t.Run("creates publisher with logger", func(t *testing.T) {
		publisher := NewPublisherWithLogger(zap.NewNop())
		assert.NotNil(t, publisher)
		assert.NotNil(t, publisher.log)
	})

	t.Run("subscribe to event type", func(t *testing.T) {
		publisher := NewPublisher()
		handler := func(event Event) {}
		publisher.Subscribe("test", handler)
		assert.Len(t, publisher.subscribers["test"], 1)
	})

	t.Run("subscribe multiple handlers", func(t *testing.T) {
		publisher := NewPublisher()
		publisher.Subscribe("test", func(event Event) {})
		publisher.Subscribe("test", func(event Event) {})
		publisher.Subscribe("test", func(event Event) {})
		assert.Len(t, publisher.subscribers["test"], 3)
	})

	t.Run("subscribe to different event types", func(t *testing.T) {
		publisher := NewPublisher()
		handler := func(event Event) {}
		publisher.Subscribe("type1", handler)
		publisher.Subscribe("type2", handler)
		publisher.Subscribe("type3", handler)
		assert.Len(t, publisher.subscribers["type1"], 1)
		assert.Len(t, publisher.subscribers["type2"], 1)
		assert.Len(t, publisher.subscribers["type3"], 1)
	})

	t.Run("publish to no subscribers", func(t *testing.T) {
		publisher := NewPublisher()
		err := publisher.Publish(context.Background(), Event{Type: "test"})
		assert.NoError(t, err)
	})

	t.Run("publish to single subscriber", func(t *testing.T) {
		publisher := NewPublisher()
		received := make(chan Event, 1)
		publisher.Subscribe("test", func(event Event) { received <- event })

		event := Event{Type: "test"}
		require.NoError(t, publisher.Publish(context.Background(), event))

		select {
		case e := <-received:
			assert.Equal(t, event.Type, e.Type)
		case <-time.After(100 * time.Millisecond):
			assert.Fail(t, "did not receive event")
		}
	})

	t.Run("publish to multiple subscribers", func(t *testing.T) {
		publisher := NewPublisher()
		var wg sync.WaitGroup
		receivedCount := 0
		mu := sync.Mutex{}

		handler := func(event Event) {
			mu.Lock()
			receivedCount++
			mu.Unlock()
			wg.Done()
		}

		wg.Add(3)
		publisher.Subscribe("test", handler)
		publisher.Subscribe("test", handler)
		publisher.Subscribe("test", handler)

		require.NoError(t, publisher.Publish(context.Background(), Event{Type: "test"}))
		wg.Wait()
		assert.Equal(t, 3, receivedCount)
	})

	t.Run("close publisher", func(t *testing.T) {
		publisher := NewPublisher()
		publisher.Close()
	})

	t.Run("nats event constants", func(t *testing.T) {
		assert.Equal(t, "file.uploaded", EventFileUploaded)
		assert.Equal(t, "transcoding.started", EventTranscodingStarted)
		assert.Equal(t, "transcoding.completed", EventTranscodingCompleted)
		assert.Equal(t, "transcoding.failed", EventTranscodingFailed)
		assert.Equal(t, "streaming.started", EventStreamingStarted)
		assert.Equal(t, "streaming.stopped", EventStreamingStopped)
		assert.Equal(t, "metadata.created", EventMetadataCreated)
		assert.Equal(t, "metadata.updated", EventMetadataUpdated)
		assert.Equal(t, "metadata.deleted", EventMetadataDeleted)
		assert.Equal(t, "job.submitted", EventJobSubmitted)
		assert.Equal(t, "job.completed", EventJobCompleted)
		assert.Equal(t, "job.failed", EventJobFailed)
		assert.Equal(t, "alert.triggered", EventAlertTriggered)
		assert.Equal(t, "alert.resolved", EventAlertResolved)
	})
}

func TestPublishFileUploaded(t *testing.T) {
	bus, err := NewMemoryEventBus()
	require.NoError(t, err)

	received := make(chan *Event, 1)
	_, _ = bus.Subscribe(context.Background(), EventFileUploaded, func(ctx context.Context, e *Event) error {
		received <- e
		return nil
	})

	err = PublishFileUploaded(context.Background(), bus, "file-123", "video.mp4", 1024000)
	require.NoError(t, err)

	select {
	case e := <-received:
		assert.Equal(t, EventFileUploaded, e.Type)
		assert.Equal(t, "upload-service", e.Source)
		assert.Equal(t, "file-123", e.Data["file_id"])
		assert.Equal(t, "video.mp4", e.Data["file_name"])
		assert.Equal(t, int64(1024000), e.Data["file_size"])
	case <-time.After(100 * time.Millisecond):
		assert.Fail(t, "did not receive file uploaded event")
	}
}

func TestPublishTranscodingStarted(t *testing.T) {
	bus, err := NewMemoryEventBus()
	require.NoError(t, err)

	received := make(chan *Event, 1)
	_, _ = bus.Subscribe(context.Background(), EventTranscodingStarted, func(ctx context.Context, e *Event) error {
		received <- e
		return nil
	})

	err = PublishTranscodingStarted(context.Background(), bus, "job-1", "/input/video.mp4")
	require.NoError(t, err)

	select {
	case e := <-received:
		assert.Equal(t, EventTranscodingStarted, e.Type)
		assert.Equal(t, "transcoder-service", e.Source)
		assert.Equal(t, "job-1", e.Data["job_id"])
	case <-time.After(100 * time.Millisecond):
		assert.Fail(t, "did not receive transcoding started event")
	}
}

func TestPublishTranscodingCompleted(t *testing.T) {
	bus, err := NewMemoryEventBus()
	require.NoError(t, err)

	received := make(chan *Event, 1)
	_, _ = bus.Subscribe(context.Background(), EventTranscodingCompleted, func(ctx context.Context, e *Event) error {
		received <- e
		return nil
	})

	err = PublishTranscodingCompleted(context.Background(), bus, "job-1", "/output/video.m3u8")
	require.NoError(t, err)

	select {
	case e := <-received:
		assert.Equal(t, EventTranscodingCompleted, e.Type)
		assert.Equal(t, "job-1", e.Data["job_id"])
		assert.Equal(t, "/output/video.m3u8", e.Data["output_file"])
	case <-time.After(100 * time.Millisecond):
		assert.Fail(t, "did not receive transcoding completed event")
	}
}

func TestPublishJobSubmitted(t *testing.T) {
	bus, err := NewMemoryEventBus()
	require.NoError(t, err)

	received := make(chan *Event, 1)
	_, _ = bus.Subscribe(context.Background(), EventJobSubmitted, func(ctx context.Context, e *Event) error {
		received <- e
		return nil
	})

	err = PublishJobSubmitted(context.Background(), bus, "job-1", "transcode")
	require.NoError(t, err)

	select {
	case e := <-received:
		assert.Equal(t, EventJobSubmitted, e.Type)
		assert.Equal(t, "worker-service", e.Source)
		assert.Equal(t, "job-1", e.Data["job_id"])
		assert.Equal(t, "transcode", e.Data["job_type"])
	case <-time.After(100 * time.Millisecond):
		assert.Fail(t, "did not receive job submitted event")
	}
}

func TestPublishJobCompleted(t *testing.T) {
	bus, err := NewMemoryEventBus()
	require.NoError(t, err)

	received := make(chan *Event, 1)
	_, _ = bus.Subscribe(context.Background(), EventJobCompleted, func(ctx context.Context, e *Event) error {
		received <- e
		return nil
	})

	err = PublishJobCompleted(context.Background(), bus, "job-1")
	require.NoError(t, err)

	select {
	case e := <-received:
		assert.Equal(t, EventJobCompleted, e.Type)
		assert.Equal(t, "job-1", e.Data["job_id"])
	case <-time.After(100 * time.Millisecond):
		assert.Fail(t, "did not receive job completed event")
	}
}

func TestPublishAlertTriggered(t *testing.T) {
	bus, err := NewMemoryEventBus()
	require.NoError(t, err)

	received := make(chan *Event, 1)
	_, _ = bus.Subscribe(context.Background(), EventAlertTriggered, func(ctx context.Context, e *Event) error {
		received <- e
		return nil
	})

	err = PublishAlertTriggered(context.Background(), bus, "alert-1", "critical", "CPU usage high")
	require.NoError(t, err)

	select {
	case e := <-received:
		assert.Equal(t, EventAlertTriggered, e.Type)
		assert.Equal(t, "monitor-service", e.Source)
		assert.Equal(t, "alert-1", e.Data["alert_id"])
		assert.Equal(t, "critical", e.Data["level"])
		assert.Equal(t, "CPU usage high", e.Data["message"])
	case <-time.After(100 * time.Millisecond):
		assert.Fail(t, "did not receive alert triggered event")
	}
}

func TestConcurrentPublishSubscribe(t *testing.T) {
	bus, err := NewMemoryEventBus()
	require.NoError(t, err)

	var receivedCount atomic.Int64
	handler := func(ctx context.Context, event *Event) error {
		receivedCount.Add(1)
		return nil
	}

	_, _ = bus.Subscribe(context.Background(), "concurrent", handler)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = bus.Publish(context.Background(), &Event{Type: "concurrent"})
		}()
	}
	wg.Wait()

	require.NoError(t, bus.Close())
	assert.Equal(t, int64(100), receivedCount.Load())
}
