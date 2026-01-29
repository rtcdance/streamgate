package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimitMiddleware returns a rate limit middleware
func (s *Service) RateLimitMiddleware() gin.HandlerFunc {
	limiter := &rateLimiter{
		clients: make(map[string]*clientLimit),
		mu:      &sync.RWMutex{},
	}

	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		if !limiter.Allow(clientIP) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

type rateLimiter struct {
	clients map[string]*clientLimit
	mu      *sync.RWMutex
}

type clientLimit struct {
	count     int
	resetTime time.Time
}

func (rl *rateLimiter) Allow(clientIP string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	limit, exists := rl.clients[clientIP]

	if !exists || now.After(limit.resetTime) {
		rl.clients[clientIP] = &clientLimit{
			count:     1,
			resetTime: now.Add(time.Minute),
		}
		return true
	}

	if limit.count < 100 {
		limit.count++
		return true
	}

	return false
}
