package lifecycle

import "context"

// Lifecycle manages component lifecycle
type Lifecycle struct {
	startHooks []func(context.Context) error
	stopHooks  []func(context.Context) error
}

// NewLifecycle creates a new lifecycle manager
func NewLifecycle() *Lifecycle {
	return &Lifecycle{
		startHooks: make([]func(context.Context) error, 0),
		stopHooks:  make([]func(context.Context) error, 0),
	}
}

// OnStart registers a start hook
func (l *Lifecycle) OnStart(hook func(context.Context) error) {
	l.startHooks = append(l.startHooks, hook)
}

// OnStop registers a stop hook
func (l *Lifecycle) OnStop(hook func(context.Context) error) {
	l.stopHooks = append(l.stopHooks, hook)
}

// Start calls all start hooks
func (l *Lifecycle) Start(ctx context.Context) error {
	for _, hook := range l.startHooks {
		if err := hook(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Stop calls all stop hooks
func (l *Lifecycle) Stop(ctx context.Context) error {
	for _, hook := range l.stopHooks {
		if err := hook(ctx); err != nil {
			return err
		}
	}
	return nil
}
