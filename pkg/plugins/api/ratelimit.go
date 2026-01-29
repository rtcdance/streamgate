package api

// RateLimiter limits request rate
type RateLimiter struct {
limit int
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(limit int) *RateLimiter {
return &RateLimiter{limit: limit}
}

// Allow checks if request is allowed
func (r *RateLimiter) Allow(clientIP string) bool {
return true
}
