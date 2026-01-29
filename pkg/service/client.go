package service

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ClientPool manages gRPC client connections
type ClientPool struct {
	registry ServiceRegistry
	logger   *zap.Logger
	clients  map[string]*grpc.ClientConn
}

// NewClientPool creates a new client pool
func NewClientPool(registry ServiceRegistry, logger *zap.Logger) *ClientPool {
	return &ClientPool{
		registry: registry,
		logger:   logger,
		clients:  make(map[string]*grpc.ClientConn),
	}
}

// GetConnection gets or creates a gRPC connection to a service
func (p *ClientPool) GetConnection(ctx context.Context, serviceName string) (*grpc.ClientConn, error) {
	// Check if connection already exists
	if conn, exists := p.clients[serviceName]; exists {
		return conn, nil
	}

	// Get service address from registry
	address, err := p.getServiceAddress(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get service address: %w", err)
	}

	// Create new connection
	conn, err := grpc.Dial(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to dial service: %w", err)
	}

	p.logger.Info("Created gRPC connection",
		zap.String("service", serviceName),
		zap.String("address", address))

	// Cache connection
	p.clients[serviceName] = conn

	return conn, nil
}

// getServiceAddress gets the address of a service
func (p *ClientPool) getServiceAddress(ctx context.Context, serviceName string) (string, error) {
	services, err := p.registry.Discover(ctx, serviceName)
	if err != nil {
		return "", fmt.Errorf("failed to discover service: %w", err)
	}

	if len(services) == 0 {
		return "", fmt.Errorf("service not found: %s", serviceName)
	}

	// Return first available service
	service := services[0]
	return fmt.Sprintf("%s:%d", service.Address, service.Port), nil
}

// Close closes all connections
func (p *ClientPool) Close() error {
	for serviceName, conn := range p.clients {
		if err := conn.Close(); err != nil {
			p.logger.Error("Failed to close connection",
				zap.String("service", serviceName),
				zap.Error(err))
		}
	}

	p.clients = make(map[string]*grpc.ClientConn)
	p.logger.Info("Closed all gRPC connections")

	return nil
}

// ServiceLocator provides service discovery and location
type ServiceLocator struct {
	registry ServiceRegistry
	logger   *zap.Logger
}

// NewServiceLocator creates a new service locator
func NewServiceLocator(registry ServiceRegistry, logger *zap.Logger) *ServiceLocator {
	return &ServiceLocator{
		registry: registry,
		logger:   logger,
	}
}

// GetUploadService gets the upload service address
func (l *ServiceLocator) GetUploadService(ctx context.Context) (string, error) {
	return l.getServiceAddress(ctx, ServiceUpload)
}

// GetStreamingService gets the streaming service address
func (l *ServiceLocator) GetStreamingService(ctx context.Context) (string, error) {
	return l.getServiceAddress(ctx, ServiceStreaming)
}

// GetMetadataService gets the metadata service address
func (l *ServiceLocator) GetMetadataService(ctx context.Context) (string, error) {
	return l.getServiceAddress(ctx, ServiceMetadata)
}

// GetAuthService gets the auth service address
func (l *ServiceLocator) GetAuthService(ctx context.Context) (string, error) {
	return l.getServiceAddress(ctx, ServiceAuth)
}

// GetCacheService gets the cache service address
func (l *ServiceLocator) GetCacheService(ctx context.Context) (string, error) {
	return l.getServiceAddress(ctx, ServiceCache)
}

// GetTranscoderService gets the transcoder service address
func (l *ServiceLocator) GetTranscoderService(ctx context.Context) (string, error) {
	return l.getServiceAddress(ctx, ServiceTranscoder)
}

// GetWorkerService gets the worker service address
func (l *ServiceLocator) GetWorkerService(ctx context.Context) (string, error) {
	return l.getServiceAddress(ctx, ServiceWorker)
}

// GetMonitorService gets the monitor service address
func (l *ServiceLocator) GetMonitorService(ctx context.Context) (string, error) {
	return l.getServiceAddress(ctx, ServiceMonitor)
}

// getServiceAddress gets the address of a service
func (l *ServiceLocator) getServiceAddress(ctx context.Context, serviceName string) (string, error) {
	services, err := l.registry.Discover(ctx, serviceName)
	if err != nil {
		return "", fmt.Errorf("failed to discover service: %w", err)
	}

	if len(services) == 0 {
		return "", fmt.Errorf("service not found: %s", serviceName)
	}

	// Return first available service
	service := services[0]
	return fmt.Sprintf("%s:%d", service.Address, service.Port), nil
}

// CircuitBreaker implements circuit breaker pattern for service calls
type CircuitBreaker struct {
	maxFailures int
	timeout     int
	failures    int
	state       string // "closed", "open", "half-open"
	logger      *zap.Logger
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(maxFailures int, timeout int, logger *zap.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures: maxFailures,
		timeout:     timeout,
		failures:    0,
		state:       "closed",
		logger:      logger,
	}
}

// Call executes a function with circuit breaker protection
func (cb *CircuitBreaker) Call(fn func() error) error {
	if cb.state == "open" {
		return fmt.Errorf("circuit breaker is open")
	}

	err := fn()
	if err != nil {
		cb.failures++
		if cb.failures >= cb.maxFailures {
			cb.state = "open"
			cb.logger.Warn("Circuit breaker opened", zap.Int("failures", cb.failures))
		}
		return err
	}

	// Reset on success
	if cb.failures > 0 {
		cb.failures = 0
		cb.state = "closed"
		cb.logger.Info("Circuit breaker closed")
	}

	return nil
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() string {
	return cb.state
}
