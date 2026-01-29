package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
)

// LoggingMiddleware returns a logging middleware
func (s *Service) LoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		c.Next()

		duration := time.Since(startTime)
		s.logger.Info("HTTP Request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", duration.Milliseconds(),
		)
	}
}
