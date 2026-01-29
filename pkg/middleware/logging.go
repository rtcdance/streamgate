package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// LoggingMiddleware returns a logging middleware
func (s *Service) LoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		c.Next()

		duration := time.Since(startTime)
		s.logger.Info("HTTP Request",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Int64("duration_ms", duration.Milliseconds()),
		)
	}
}
