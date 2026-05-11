package middleware

import (
	"net/http"

	"go.uber.org/zap"
)

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

// ServiceChain chains multiple middleware
func ServiceChain(handler http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	// Apply middleware in reverse order
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}
