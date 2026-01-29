package health

import "context"

// Health represents health status
type Health struct {
	Status string `json:"status"`
}

// Check performs a health check
func Check(ctx context.Context) *Health {
	return &Health{
		Status: "healthy",
	}
}
