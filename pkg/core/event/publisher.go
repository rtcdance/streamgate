package event

import (
	"context"
	"sync"

	"go.uber.org/zap"
)

type Publisher struct {
	mu             sync.RWMutex
	subscribers    map[string][]func(Event)
	wg             sync.WaitGroup
	sem            chan struct{}
	log            *zap.Logger
	maxConcurrency int
}

func NewPublisher() *Publisher {
	return &Publisher{
		subscribers:    make(map[string][]func(Event)),
		maxConcurrency: defaultMaxConcurrency,
		log:            zap.NewNop(),
	}
}

func NewPublisherWithLogger(log *zap.Logger) *Publisher {
	p := NewPublisher()
	p.log = log
	return p
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

	if len(handlers) == 0 {
		return nil
	}

	if p.sem == nil {
		p.sem = make(chan struct{}, p.maxConcurrency)
	}

	for _, handler := range handlers {
		select {
		case p.sem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}

		p.wg.Add(1)
		go func(h func(Event)) {
			defer p.wg.Done()
			defer func() { <-p.sem }()
			defer func() {
				if r := recover(); r != nil {
					p.log.Error("Recovered panic in event publisher handler",
						zap.Any("panic", r),
						zap.String("event_type", event.Type))
				}
			}()
			h(event)
		}(handler)
	}
	return nil
}

func (p *Publisher) Close() {
	p.wg.Wait()
}
