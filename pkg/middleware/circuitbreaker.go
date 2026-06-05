package middleware

import (
	"errors"
	"net/http"

	"github.com/rtcdance/streamgate/pkg/resilience"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type CircuitBreakerState = resilience.CircuitBreakerState
type CircuitBreakerConfig = resilience.CircuitBreakerConfig
type CircuitBreakerStats = resilience.CircuitBreakerStats
type CircuitBreaker = resilience.CircuitBreaker
type CircuitBreakerManager = resilience.CircuitBreakerManager

const (
	StateClosed   = resilience.StateClosed
	StateOpen     = resilience.StateOpen
	StateHalfOpen = resilience.StateHalfOpen
)

func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return resilience.DefaultCircuitBreakerConfig()
}

func NewCircuitBreaker(name string, config CircuitBreakerConfig, logger *zap.Logger) *CircuitBreaker {
	return resilience.NewCircuitBreaker(name, config, logger)
}

func NewCircuitBreakerManager(logger *zap.Logger) *CircuitBreakerManager {
	return resilience.NewCircuitBreakerManager(logger)
}

func (s *Service) CircuitBreakerMiddleware(name string, config CircuitBreakerConfig) gin.HandlerFunc {
	cb := s.cbManager.GetOrCreate(name, config)

	return func(c *gin.Context) {
		err := cb.Execute(c.Request.Context(), func() error {
			c.Next()
			if c.Writer.Status() >= 500 {
				return errors.New("server error")
			}
			return nil
		})

		if err != nil {
			if !c.Writer.Written() {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error":   "Service temporarily unavailable",
					"circuit": name,
					"state":   cb.State().String(),
				})
			}
			c.Abort()
		}
	}
}
