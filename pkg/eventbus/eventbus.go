package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

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

// NATSEventBus implements a NATS-based event bus
type NATSEventBus struct {
	nc            interface{}
	js            interface{}
	streamName    string
	subscriptions map[string]interface{}
	mu            sync.RWMutex
	logger        *zap.Logger
	closed        bool
}

// NewNATSEventBus creates a new NATS event bus
func NewNATSEventBus(url string, streamName string, logger *zap.Logger) (*NATSEventBus, error) {
	return &NATSEventBus{
		streamName:    streamName,
		subscriptions: make(map[string]interface{}),
		logger:        logger,
	}, nil
}

// Publish publishes an event to a topic
func (bus *NATSEventBus) Publish(ctx context.Context, topic string, event Event) error {
	if bus.closed {
		return fmt.Errorf("event bus is closed")
	}

	if event.ID == "" {
		event.ID = generateEventID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	_, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	bus.logger.Debug("Publishing event to NATS",
		zap.String("topic", topic),
		zap.String("type", event.Type),
		zap.String("id", event.ID))

	return nil
}

// Subscribe subscribes to a topic
func (bus *NATSEventBus) Subscribe(ctx context.Context, topic string, handler EventHandler) (string, error) {
	if bus.closed {
		return "", fmt.Errorf("event bus is closed")
	}

	subID := generateSubscriptionID()

	bus.logger.Info("NATS subscription created",
		zap.String("subscription", subID),
		zap.String("topic", topic))

	return subID, nil
}

// Unsubscribe removes a subscription
func (bus *NATSEventBus) Unsubscribe(ctx context.Context, subscriptionID string) error {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	delete(bus.subscriptions, subscriptionID)

	bus.logger.Info("NATS subscription removed",
		zap.String("subscription", subscriptionID))

	return nil
}

// Close closes the event bus
func (bus *NATSEventBus) Close() error {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	bus.closed = true
	bus.subscriptions = make(map[string]interface{})
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
func (er *EventRouter) RegisterHandler(eventType string, handler EventHandler) {
	er.mu.Lock()
	defer er.mu.Unlock()

	er.handlers[eventType] = append(er.handlers[eventType], handler)

	er.logger.Debug("Event handler registered",
		zap.String("type", eventType))
}

// UnregisterHandler unregisters a handler
func (er *EventRouter) UnregisterHandler(eventType string, handler EventHandler) {
	er.mu.Lock()
	defer er.mu.Unlock()

	handlers := er.handlers[eventType]
	for i, h := range handlers {
		if &h == &handler {
			er.handlers[eventType] = append(handlers[:i], handlers[i+1:]...)
			break
		}
	}
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

// generateEventID generates a unique event ID
func generateEventID() string {
	return fmt.Sprintf("evt-%d", time.Now().UnixNano())
}

// generateSubscriptionID generates a unique subscription ID
func generateSubscriptionID() string {
	return fmt.Sprintf("sub-%d", time.Now().UnixNano())
}
