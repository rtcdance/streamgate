package event

import "context"

// Publisher publishes events
type Publisher struct {
	subscribers map[string][]func(Event)
}

// NewPublisher creates a new publisher
func NewPublisher() *Publisher {
	return &Publisher{
		subscribers: make(map[string][]func(Event)),
	}
}

// Subscribe subscribes to events
func (p *Publisher) Subscribe(eventType string, handler func(Event)) {
	p.subscribers[eventType] = append(p.subscribers[eventType], handler)
}

// Publish publishes an event
func (p *Publisher) Publish(ctx context.Context, event Event) error {
	if handlers, ok := p.subscribers[event.Type]; ok {
		for _, handler := range handlers {
			go handler(event)
		}
	}
	return nil
}
