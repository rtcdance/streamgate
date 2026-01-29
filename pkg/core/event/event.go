package event

import (
	"context"
	"fmt"
	"sync"
)

// Event represents an event in the system
type Event struct {
	Type      string                 `json:"type"`
	Source    string                 `json:"source"`
	Timestamp int64                  `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// EventHandler is a function that handles events
type EventHandler func(ctx context.Context, event *Event) error

// EventBus defines the interface for event publishing and subscription
type EventBus interface {
	Publish(ctx context.Context, event *Event) error
	Subscribe(ctx context.Context, eventType string, handler EventHandler) error
	Unsubscribe(ctx context.Context, eventType string, handler EventHandler) error
	Close() error
}

// MemoryEventBus is an in-memory implementation of EventBus
type MemoryEventBus struct {
	handlers map[string][]EventHandler
	mu       sync.RWMutex
}

// NewMemoryEventBus creates a new in-memory event bus
func NewMemoryEventBus() (*MemoryEventBus, error) {
	return &MemoryEventBus{
		handlers: make(map[string][]EventHandler),
	}, nil
}

// Publish publishes an event to all subscribers
func (b *MemoryEventBus) Publish(ctx context.Context, event *Event) error {
	b.mu.RLock()
	handlers, exists := b.handlers[event.Type]
	b.mu.RUnlock()

	if !exists {
		return nil
	}

	for _, handler := range handlers {
		go func(h EventHandler) {
			if err := h(ctx, event); err != nil {
				// Log error but don't fail
				fmt.Printf("Error handling event: %v\n", err)
			}
		}(handler)
	}

	return nil
}

// Subscribe subscribes to events of a specific type
func (b *MemoryEventBus) Subscribe(ctx context.Context, eventType string, handler EventHandler) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.handlers[eventType] = append(b.handlers[eventType], handler)
	return nil
}

// Unsubscribe unsubscribes from events
func (b *MemoryEventBus) Unsubscribe(ctx context.Context, eventType string, handler EventHandler) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	handlers, exists := b.handlers[eventType]
	if !exists {
		return nil
	}

	// Remove handler (simplified - doesn't actually remove, just for demo)
	_ = handlers
	return nil
}

// Close closes the event bus
func (b *MemoryEventBus) Close() error {
	return nil
}
