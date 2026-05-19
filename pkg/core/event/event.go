package event

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
)

type Event struct {
	Type      string                 `json:"type"`
	Source    string                 `json:"source"`
	Timestamp int64                  `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

const (
	EventTypeCacheWarmed         = "cache.warmed"
	EventTypeNFTVerified         = "nft.verified"
	EventTypeServiceRegistered   = "service.registered"
	EventTypeServiceDeregistered = "service.deregistered"
	EventTypeStreamingStarted    = "streaming.started"
	EventTypeStreamingStopped    = "streaming.stopped"
	EventTypeMetadataCreated     = "metadata.created"
	EventTypeMetadataUpdated     = "metadata.updated"
	EventTypeMetadataDeleted     = "metadata.deleted"
	EventTypeJobSubmitted        = "job.submitted"
	EventTypeJobCompleted        = "job.completed"
	EventTypeJobFailed           = "job.failed"
	EventTypeAlertTriggered      = "alert.triggered"
	EventTypeAlertResolved       = "alert.resolved"
)

type EventHandler func(ctx context.Context, event *Event) error

type EventBus interface {
	Publish(ctx context.Context, event *Event) error
	Subscribe(ctx context.Context, eventType string, handler EventHandler) (string, error)
	Unsubscribe(ctx context.Context, subscriptionID string) error
	Close() error
}

const defaultMaxConcurrency = 64

var nextSubscriptionID atomic.Int64

type subscription struct {
	id        string
	eventType string
	handler   EventHandler
}

type MemoryEventBus struct {
	subscriptions  map[string]*subscription
	mu             sync.RWMutex
	wg             sync.WaitGroup
	sem            chan struct{}
	maxConcurrency int
	log            *zap.Logger
}

func NewMemoryEventBus(opts ...MemoryEventBusOption) (*MemoryEventBus, error) {
	b := &MemoryEventBus{
		subscriptions:  make(map[string]*subscription),
		maxConcurrency: defaultMaxConcurrency,
	}
	for _, opt := range opts {
		opt(b)
	}
	b.sem = make(chan struct{}, b.maxConcurrency)
	return b, nil
}

type MemoryEventBusOption func(*MemoryEventBus)

func WithMaxConcurrency(n int) MemoryEventBusOption {
	return func(b *MemoryEventBus) {
		if n > 0 {
			b.maxConcurrency = n
		}
	}
}

func WithLogger(log *zap.Logger) MemoryEventBusOption {
	return func(b *MemoryEventBus) {
		b.log = log
	}
}

func (b *MemoryEventBus) Publish(ctx context.Context, event *Event) error {
	var subs []*subscription
	b.mu.RLock()
	for _, sub := range b.subscriptions {
		if sub.eventType == event.Type {
			subs = append(subs, sub)
		}
	}
	b.mu.RUnlock()

	if len(subs) == 0 {
		return nil
	}

	for _, sub := range subs {
		select {
		case b.sem <- struct{}{}:
		case <-ctx.Done():
			if b.log != nil {
				b.log.Warn("Event publish blocked by slow subscriber, dropping event",
					zap.String("event_type", event.Type))
			}
			return ctx.Err()
		}
		b.wg.Add(1)
		go func(s *subscription) {
			defer b.wg.Done()
			defer func() { <-b.sem }()
			defer func() {
				if r := recover(); r != nil {
					if b.log != nil {
						b.log.Error("Recovered panic in event handler", zap.Any("panic", r), zap.String("event_type", event.Type))
					}
				}
			}()
			if err := s.handler(ctx, event); err != nil {
				if b.log != nil {
					b.log.Error("Error handling event", zap.Error(err), zap.String("event_type", event.Type))
				}
			}
		}(sub)
	}

	return nil
}

func (b *MemoryEventBus) Subscribe(ctx context.Context, eventType string, handler EventHandler) (string, error) {
	id := fmtSubscriptionID()
	sub := &subscription{
		id:        id,
		eventType: eventType,
		handler:   handler,
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.subscriptions[id] = sub
	return id, nil
}

func (b *MemoryEventBus) Unsubscribe(ctx context.Context, subscriptionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.subscriptions, subscriptionID)
	return nil
}

func (b *MemoryEventBus) Close() error {
	b.wg.Wait()
	return nil
}

func fmtSubscriptionID() string {
	return fmt.Sprintf("sub-%d", nextSubscriptionID.Add(1))
}
