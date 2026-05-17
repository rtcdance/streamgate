package health

import (
	"context"
	"sync"
)

type Checker struct {
	mu     sync.RWMutex
	checks map[string]func(context.Context) error
}

func NewChecker() *Checker {
	return &Checker{
		checks: make(map[string]func(context.Context) error),
	}
}

func (c *Checker) Register(name string, check func(context.Context) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checks[name] = check
}

func (c *Checker) Check(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, check := range c.checks {
		if err := check(ctx); err != nil {
			return err
		}
	}
	return nil
}
