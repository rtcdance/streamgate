package middleware

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// TracingMiddleware returns a gin middleware that creates an OpenTelemetry span
// for every request. If no OTel TracerProvider is configured, spans are no-ops.
func (s *Service) TracingMiddleware() gin.HandlerFunc {
	return otelgin.Middleware("streamgate")
}
