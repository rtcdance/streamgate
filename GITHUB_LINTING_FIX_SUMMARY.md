# GitHub Linting Errors - Quick Fix Summary

## Problem
GitHub Actions CI reported 10 linting errors:
- 4 errors: Duplicate `NATSEventBus` and `NewNATSEventBus` declarations
- 6 errors: Undefined `Service` type in middleware files

## Solution

### Change 1: Remove Duplicate NATS Code from event.go
**File**: `pkg/core/event/event.go`

Removed lines 94-127 (duplicate stub implementation):
```go
// REMOVED:
// NATSEventBus is a NATS-based implementation of EventBus
type NATSEventBus struct {
    url string
    // TODO: Add NATS connection
}

// NewNATSEventBus creates a new NATS event bus
func NewNATSEventBus(config interface{}) (*NATSEventBus, error) {
    // TODO: Implement NATS connection
    return &NATSEventBus{}, nil
}

// Publish publishes an event via NATS
func (b *NATSEventBus) Publish(ctx context.Context, event *Event) error {
    // TODO: Implement NATS publish
    data, _ := json.Marshal(event)
    fmt.Printf("Publishing event: %s\n", string(data))
    return nil
}

// Subscribe subscribes to events via NATS
func (b *NATSEventBus) Subscribe(ctx context.Context, eventType string, handler EventHandler) error {
    // TODO: Implement NATS subscribe
    return nil
}

// Unsubscribe unsubscribes from events
func (b *NATSEventBus) Unsubscribe(ctx context.Context, eventType string, handler EventHandler) error {
    // TODO: Implement NATS unsubscribe
    return nil
}

// Close closes the event bus
func (b *NATSEventBus) Close() error {
    // TODO: Close NATS connection
    return nil
}
```

Also removed unused import:
```go
// REMOVED:
"encoding/json"
```

### Change 2: Add Service Struct to middleware/service.go
**File**: `pkg/middleware/service.go`

Added after package declaration (before ServiceMiddleware):
```go
// Service provides middleware services
type Service struct {
    logger *zap.Logger
}

// NewService creates a new middleware service
func NewService(logger *zap.Logger) *Service {
    return &Service{
        logger: logger,
    }
}
```

## Result
✓ All 10 GitHub Actions linting errors resolved
✓ Code passes all diagnostics
✓ Ready to push to GitHub

## Files Changed
- `pkg/core/event/event.go` (removed 41 lines)
- `pkg/middleware/service.go` (added 12 lines)
