package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type requestIDKey struct{}

// RequestIDFromCtx extracts the request ID from a context.Context.
// It first checks the context value set by RequestIDMiddleware, then falls
// back to the gin.Context's "request_id" key. Returns empty string if not found.
func RequestIDFromCtx(ctx context.Context) string {
	// Check standard context value first
	if v := ctx.Value(requestIDKey{}); v != nil {
		if id, ok := v.(string); ok {
			return id
		}
	}
	// Check gin context
	if gc, ok := ctx.(*gin.Context); ok {
		if id, exists := gc.Get("request_id"); exists {
			if s, ok := id.(string); ok {
				return s
			}
		}
	}
	return ""
}

// ContextWithRequestID returns a new context with the request ID embedded.
// This is useful for propagating the request ID through service layer calls
// that don't have access to gin.Context.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

// TraceIDMiddleware injects a request-scoped logger with trace_id into the
// Gin context. It reads the request_id set by RequestIDMiddleware (which must
// run first in the chain) and creates a child logger that includes
// trace_id=<request_id> in every log entry.
//
// Downstream handlers can retrieve the enriched logger via:
//
//	logger := c.MustGet("logger").(*zap.Logger)
//	logger.Info("something happened") // will include trace_id field
func (s *Service) TraceIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID, _ := c.Get("request_id")
		var reqIDStr string
		if id, ok := requestID.(string); ok {
			reqIDStr = id
		}

		// Propagate request ID to standard context for service layer access
		if reqIDStr != "" {
			ctx := ContextWithRequestID(c.Request.Context(), reqIDStr)
			c.Request = c.Request.WithContext(ctx)
		}

		var reqLogger *zap.Logger
		if s.logger != nil && reqIDStr != "" {
			reqLogger = s.logger.With(zap.String("trace_id", reqIDStr))
		} else if s.logger != nil {
			reqLogger = s.logger
		}

		if reqLogger != nil {
			c.Set("logger", reqLogger)
		}

		c.Next()
	}
}

// GetLogger extracts the request-scoped logger from the Gin context.
// Falls back to the provided default logger if no scoped logger is found.
func GetLogger(c *gin.Context, fallback *zap.Logger) *zap.Logger {
	if l, ok := c.Get("logger"); ok {
		if logger, ok := l.(*zap.Logger); ok {
			return logger
		}
	}
	return fallback
}
