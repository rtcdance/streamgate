package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// natsSub wraps a NATS subscription with its subscription ID and handler.
type natsSub struct {
	id      string
	sub     *nats.Subscription
	handler EventHandler
	unsub   func()
}

// Event represents a domain event
type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Source    string                 `json:"source"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Metadata  map[string]string      `json:"metadata,omitempty"`
}

// EventHandler handles events
type EventHandler func(ctx context.Context, event Event) error

// EventBus defines the interface for event bus
type EventBus interface {
	Publish(ctx context.Context, topic string, event Event) error
	Subscribe(ctx context.Context, topic string, handler EventHandler) (string, error)
	Unsubscribe(ctx context.Context, subscriptionID string) error
	Close() error
}

// InMemoryEventBus implements an in-memory event bus
type InMemoryEventBus struct {
	subscriptions map[string][]*subscription
	mu            sync.RWMutex
	logger        *zap.Logger
	closed        bool
}

type subscription struct {
	id      string
	topic   string
	handler EventHandler
}

// NewInMemoryEventBus creates a new in-memory event bus
func NewInMemoryEventBus(logger *zap.Logger) *InMemoryEventBus {
	return &InMemoryEventBus{
		subscriptions: make(map[string][]*subscription),
		logger:        logger,
	}
}

// Publish publishes an event to a topic
func (bus *InMemoryEventBus) Publish(ctx context.Context, topic string, event Event) error {
	bus.mu.RLock()
	defer bus.mu.RUnlock()

	if bus.closed {
		return fmt.Errorf("event bus is closed")
	}

	if event.ID == "" {
		event.ID = generateEventID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	bus.logger.Debug("Publishing event",
		zap.String("topic", topic),
		zap.String("type", event.Type),
		zap.String("id", event.ID))

	subs, exists := bus.subscriptions[topic]
	if !exists {
		return nil
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(subs))

	for _, sub := range subs {
		wg.Add(1)
		go func(s *subscription) {
			defer wg.Done()
			if err := s.handler(ctx, event); err != nil {
				bus.logger.Error("Event handler error",
					zap.String("subscription", s.id),
					zap.String("topic", topic),
					zap.Error(err))
				errChan <- err
			}
		}(sub)
	}

	wg.Wait()
	close(errChan)

	var firstErr error
	for err := range errChan {
		if firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// Subscribe subscribes to a topic
func (bus *InMemoryEventBus) Subscribe(ctx context.Context, topic string, handler EventHandler) (string, error) {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	if bus.closed {
		return "", fmt.Errorf("event bus is closed")
	}

	subID := generateSubscriptionID()
	sub := &subscription{
		id:      subID,
		topic:   topic,
		handler: handler,
	}

	bus.subscriptions[topic] = append(bus.subscriptions[topic], sub)

	bus.logger.Info("Subscription created",
		zap.String("subscription", subID),
		zap.String("topic", topic))

	return subID, nil
}

// Unsubscribe removes a subscription
func (bus *InMemoryEventBus) Unsubscribe(ctx context.Context, subscriptionID string) error {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	for topic, subs := range bus.subscriptions {
		for i, sub := range subs {
			if sub.id == subscriptionID {
				bus.subscriptions[topic] = append(subs[:i], subs[i+1:]...)
				bus.logger.Info("Subscription removed",
					zap.String("subscription", subscriptionID),
					zap.String("topic", topic))
				return nil
			}
		}
	}

	return fmt.Errorf("subscription '%s' not found", subscriptionID)
}

// Close closes the event bus
func (bus *InMemoryEventBus) Close() error {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	bus.closed = true
	bus.subscriptions = make(map[string][]*subscription)
	return nil
}

// NATSEventBus implements a NATS-based event bus.
type NATSEventBus struct {
	nc            *nats.Conn
	streamName    string
	subscriptions map[string]*natsSub
	mu            sync.RWMutex
	logger        *zap.Logger
	closed        bool
}

// NewNATSEventBus creates a new NATS event bus with a real connection.
// url is the NATS server URL (e.g. "nats://localhost:4222").
func NewNATSEventBus(url, streamName string, logger *zap.Logger) (*NATSEventBus, error) {
	nc, err := nats.Connect(url, nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
		nats.ReconnectWait(2*time.Second))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	logger.Info("Connected to NATS", zap.String("url", url), zap.String("stream", streamName))

	return &NATSEventBus{
		nc:            nc,
		streamName:    streamName,
		subscriptions: make(map[string]*natsSub),
		logger:        logger,
	}, nil
}

// Publish publishes an event to a NATS subject as JSON.
func (bus *NATSEventBus) Publish(ctx context.Context, topic string, event Event) error {
	bus.mu.RLock()
	closed := bus.closed
	bus.mu.RUnlock()

	if closed {
		return fmt.Errorf("event bus is closed")
	}

	if event.ID == "" {
		event.ID = generateEventID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if err := bus.nc.Publish(topic, data); err != nil {
		return fmt.Errorf("failed to publish to NATS: %w", err)
	}
	bus.nc.FlushWithContext(ctx)

	bus.logger.Debug("Published event to NATS",
		zap.String("topic", topic),
		zap.String("type", event.Type),
		zap.String("id", event.ID),
		zap.Int("bytes", len(data)))

	return nil
}

// Subscribe subscribes to a NATS subject and delivers events to the handler.
func (bus *NATSEventBus) Subscribe(ctx context.Context, topic string, handler EventHandler) (string, error) {
	bus.mu.RLock()
	closed := bus.closed
	bus.mu.RUnlock()

	if closed {
		return "", fmt.Errorf("event bus is closed")
	}

	subID := generateSubscriptionID()
	rawSub, err := bus.nc.Subscribe(topic, func(msg *nats.Msg) {
		var event Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			bus.logger.Error("Failed to unmarshal NATS event",
				zap.String("topic", topic),
				zap.Error(err))
			return
		}
		if err := handler(ctx, event); err != nil {
			bus.logger.Error("NATS event handler error",
				zap.String("subscription", subID),
				zap.String("topic", topic),
				zap.Error(err))
		}
	})
	if err != nil {
		return "", fmt.Errorf("failed to subscribe to NATS: %w", err)
	}

	entry := &natsSub{
		id:      subID,
		sub:     rawSub,
		handler: handler,
		unsub:   func() { _ = rawSub.Unsubscribe() },
	}

	bus.mu.Lock()
	bus.subscriptions[subID] = entry
	bus.mu.Unlock()

	bus.logger.Info("NATS subscription created",
		zap.String("subscription", subID),
		zap.String("topic", topic))

	return subID, nil
}

// Unsubscribe removes a NATS subscription.
func (bus *NATSEventBus) Unsubscribe(ctx context.Context, subscriptionID string) error {
	bus.mu.Lock()
	entry, ok := bus.subscriptions[subscriptionID]
	delete(bus.subscriptions, subscriptionID)
	bus.mu.Unlock()

	if !ok {
		return fmt.Errorf("subscription '%s' not found", subscriptionID)
	}

	entry.unsub()

	bus.logger.Info("NATS subscription removed",
		zap.String("subscription", subscriptionID))

	return nil
}

// Close closes the NATS connection and all subscriptions.
func (bus *NATSEventBus) Close() error {
	bus.mu.Lock()
	bus.closed = true
	for _, entry := range bus.subscriptions {
		entry.unsub()
	}
	bus.subscriptions = make(map[string]*natsSub)
	bus.mu.Unlock()

	bus.nc.Close()
	return nil
}

// EventTypes defines common event types
const (
	EventTypeUploadStarted         = "upload.started"
	EventTypeUploadCompleted       = "upload.completed"
	EventTypeUploadFailed          = "upload.failed"
	EventTypeTranscodingStarted    = "transcoding.started"
	EventTypeTranscodingProgress   = "transcoding.progress"
	EventTypeTranscodingCompleted  = "transcoding.completed"
	EventTypeTranscodingFailed     = "transcoding.failed"
	EventTypeContentPublished      = "content.published"
	EventTypeContentDeleted        = "content.deleted"
	EventTypeNFTVerified           = "nft.verified"
	EventTypeNFTVerificationFailed = "nft.verification_failed"
	EventTypeStreamStarted         = "stream.started"
	EventTypeStreamEnded           = "stream.ended"
	EventTypeUserCreated           = "user.created"
	EventTypeUserDeleted           = "user.deleted"
	EventTypeServiceStarted        = "service.started"
	EventTypeServiceStopped        = "service.stopped"
	EventTypeHealthCheckFailed     = "health.check_failed"
)

// EventBuilder helps build events
type EventBuilder struct {
	event Event
}

// NewEventBuilder creates a new event builder
func NewEventBuilder(eventType, source string) *EventBuilder {
	return &EventBuilder{
		event: Event{
			Type:      eventType,
			Source:    source,
			Timestamp: time.Now(),
			Data:      make(map[string]interface{}),
			Metadata:  make(map[string]string),
		},
	}
}

// WithID sets the event ID
func (eb *EventBuilder) WithID(id string) *EventBuilder {
	eb.event.ID = id
	return eb
}

// WithData adds data to the event
func (eb *EventBuilder) WithData(key string, value interface{}) *EventBuilder {
	eb.event.Data[key] = value
	return eb
}

// WithMetadata adds metadata to the event
func (eb *EventBuilder) WithMetadata(key, value string) *EventBuilder {
	eb.event.Metadata[key] = value
	return eb
}

// WithTimestamp sets the event timestamp
func (eb *EventBuilder) WithTimestamp(timestamp time.Time) *EventBuilder {
	eb.event.Timestamp = timestamp
	return eb
}

// Build builds the event
func (eb *EventBuilder) Build() Event {
	if eb.event.ID == "" {
		eb.event.ID = generateEventID()
	}
	return eb.event
}

// EventRouter routes events to appropriate handlers
type EventRouter struct {
	handlers map[string][]EventHandler
	mu       sync.RWMutex
	logger   *zap.Logger
}

// NewEventRouter creates a new event router
func NewEventRouter(logger *zap.Logger) *EventRouter {
	return &EventRouter{
		handlers: make(map[string][]EventHandler),
		logger:   logger,
	}
}

// Route routes an event to appropriate handlers
func (er *EventRouter) Route(ctx context.Context, event Event) error {
	er.mu.RLock()
	handlers := er.handlers[event.Type]
	er.mu.RUnlock()

	if len(handlers) == 0 {
		er.logger.Debug("No handlers for event type", zap.String("type", event.Type))
		return nil
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(handlers))

	for _, handler := range handlers {
		wg.Add(1)
		go func(h EventHandler) {
			defer wg.Done()
			if err := h(ctx, event); err != nil {
				er.logger.Error("Event handler error",
					zap.String("type", event.Type),
					zap.Error(err))
				errChan <- err
			}
		}(handler)
	}

	wg.Wait()
	close(errChan)

	var firstErr error
	for err := range errChan {
		if firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// RegisterHandler registers a handler for an event type
func (er *EventRouter) RegisterHandler(eventType string, handler EventHandler) int {
	er.mu.Lock()
	defer er.mu.Unlock()

	er.handlers[eventType] = append(er.handlers[eventType], handler)
	index := len(er.handlers[eventType]) - 1

	er.logger.Debug("Event handler registered",
		zap.String("type", eventType))
	return index
}

// UnregisterHandler unregisters a handler by index.
// Since Go func values are not comparable, handlers are matched by index
// returned from RegisterHandler.
func (er *EventRouter) UnregisterHandler(eventType string, handlerIndex int) {
	er.mu.Lock()
	defer er.mu.Unlock()

	handlers := er.handlers[eventType]
	if handlerIndex < 0 || handlerIndex >= len(handlers) {
		return
	}
	er.handlers[eventType] = append(handlers[:handlerIndex], handlers[handlerIndex+1:]...)
}

// EventStore stores events for replay
type EventStore struct {
	events []Event
	mu     sync.RWMutex
	logger *zap.Logger
}

// NewEventStore creates a new event store
func NewEventStore(logger *zap.Logger) *EventStore {
	return &EventStore{
		events: make([]Event, 0),
		logger: logger,
	}
}

// Store stores an event
func (es *EventStore) Store(event Event) error {
	es.mu.Lock()
	defer es.mu.Unlock()

	es.events = append(es.events, event)
	return nil
}

// Get retrieves events by type
func (es *EventStore) Get(eventType string, limit int) ([]Event, error) {
	es.mu.RLock()
	defer es.mu.RUnlock()

	result := make([]Event, 0)
	count := 0

	for i := len(es.events) - 1; i >= 0 && count < limit; i-- {
		if es.events[i].Type == eventType {
			result = append(result, es.events[i])
			count++
		}
	}

	return result, nil
}

// GetAll retrieves all events
func (es *EventStore) GetAll() []Event {
	es.mu.RLock()
	defer es.mu.RUnlock()

	result := make([]Event, len(es.events))
	copy(result, es.events)
	return result
}

// Clear clears all events
func (es *EventStore) Clear() {
	es.mu.Lock()
	defer es.mu.Unlock()

	es.events = make([]Event, 0)
}

var (
	eventIDCounter uint64
	subIDCounter   uint64
	idMu           sync.Mutex
)

func generateEventID() string {
	idMu.Lock()
	counter := eventIDCounter
	eventIDCounter++
	idMu.Unlock()
	return fmt.Sprintf("evt-%d-%d", time.Now().UnixNano(), counter)
}

func generateSubscriptionID() string {
	idMu.Lock()
	counter := subIDCounter
	subIDCounter++
	idMu.Unlock()
	return fmt.Sprintf("sub-%d-%d", time.Now().UnixNano(), counter)
}
