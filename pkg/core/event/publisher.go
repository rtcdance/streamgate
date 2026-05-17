package event

import (
	"context"
	"sync"
)

type Publisher struct {
	mu          sync.RWMutex
	subscribers map[string][]func(Event)
	wg          sync.WaitGroup
}

func NewPublisher() *Publisher {
	return &Publisher{
		subscribers: make(map[string][]func(Event)),
	}
}

func (p *Publisher) Subscribe(eventType string, handler func(Event)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subscribers[eventType] = append(p.subscribers[eventType], handler)
}

func (p *Publisher) Publish(ctx context.Context, event Event) error {
	p.mu.RLock()
	handlers := p.subscribers[event.Type]
	p.mu.RUnlock()

	for _, handler := range handlers {
		p.wg.Add(1)
		go func(h func(Event)) {
			defer p.wg.Done()
			defer func() {
				_ = recover()
			}()
			h(event)
		}(handler)
	}
	return nil
}

func (p *Publisher) Close() {
	p.wg.Wait()
}
