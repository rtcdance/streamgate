package lifecycle

import (
	"context"
	"sync"
)

type Lifecycle struct {
	mu         sync.RWMutex
	startHooks []func(context.Context) error
	stopHooks  []func(context.Context) error
}

func NewLifecycle() *Lifecycle {
	return &Lifecycle{
		startHooks: make([]func(context.Context) error, 0),
		stopHooks:  make([]func(context.Context) error, 0),
	}
}

func (l *Lifecycle) OnStart(hook func(context.Context) error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.startHooks = append(l.startHooks, hook)
}

func (l *Lifecycle) OnStop(hook func(context.Context) error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopHooks = append(l.stopHooks, hook)
}

func (l *Lifecycle) Start(ctx context.Context) error {
	l.mu.RLock()
	hooks := make([]func(context.Context) error, len(l.startHooks))
	copy(hooks, l.startHooks)
	l.mu.RUnlock()

	for _, hook := range hooks {
		if err := hook(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (l *Lifecycle) Stop(ctx context.Context) error {
	l.mu.RLock()
	hooks := make([]func(context.Context) error, len(l.stopHooks))
	copy(hooks, l.stopHooks)
	l.mu.RUnlock()

	var firstErr error
	for i := len(hooks) - 1; i >= 0; i-- {
		if err := hooks[i](ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
