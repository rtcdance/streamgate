package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// TracingMiddleware returns a tracing middleware
func (s *Service) TracingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := c.GetHeader("X-Trace-ID")
		if traceID == "" {
			traceID = uuid.New().String()
		}

		c.Set("trace_id", traceID)
		c.Header("X-Trace-ID", traceID)

		c.Next()
	}
}
