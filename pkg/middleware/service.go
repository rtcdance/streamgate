package middleware

import (
	"context"
	"net/http"

	"go.uber.org/zap"
)

type Service struct {
	logger      *zap.Logger
	cbManager   *CircuitBreakerManager
	rateLimiter RateLimiter
	redisClient RedisClient
}

func NewService(logger *zap.Logger) *Service {
	return &Service{
		logger:      logger,
		cbManager:   NewCircuitBreakerManager(logger),
		rateLimiter: NewRateLimiter(DefaultRateLimitConfig(), nil),
	}
}

func NewServiceWithRedis(logger *zap.Logger, redisClient RedisClient) *Service {
	return &Service{
		logger:      logger,
		cbManager:   NewCircuitBreakerManager(logger),
		rateLimiter: NewRateLimiter(DefaultRateLimitConfig(), redisClient),
		redisClient: redisClient,
	}
}

func (s *Service) CircuitBreakerManager() *CircuitBreakerManager {
	return s.cbManager
}

func (s *Service) DependencyCircuitBreaker(name string, config CircuitBreakerConfig) *CircuitBreaker {
	return s.cbManager.GetOrCreate(name, config)
}

func (s *Service) ExecuteWithCB(ctx context.Context, name string, config CircuitBreakerConfig, fn func() error) error {
	cb := s.cbManager.GetOrCreate(name, config)
	return cb.Execute(ctx, fn)
}

func (s *Service) AllCircuitBreakerStats() map[string]CircuitBreakerStats {
	return s.cbManager.GetAllStats()
}

func (s *Service) Close() {
	if s.rateLimiter != nil {
		s.rateLimiter.Stop()
	}
}

func ServiceChain(handler http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}
