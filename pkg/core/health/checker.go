package health

import "context"

// Checker performs health checks
type Checker struct {
	checks map[string]func(context.Context) error
}

// NewChecker creates a new health checker
func NewChecker() *Checker {
	return &Checker{
		checks: make(map[string]func(context.Context) error),
	}
}

// Register registers a health check
func (c *Checker) Register(name string, check func(context.Context) error) {
	c.checks[name] = check
}

// Check performs all registered health checks
func (c *Checker) Check(ctx context.Context) error {
	for _, check := range c.checks {
		if err := check(ctx); err != nil {
			return err
		}
	}
	return nil
}
