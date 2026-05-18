package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// ClientPool manages gRPC client connections
type ClientPool struct {
	registry    ServiceRegistry
	logger      *zap.Logger
	clients     map[string]*grpc.ClientConn
	mu          sync.RWMutex
	tlsConfig   *tls.Config
	rrCounter   atomic.Uint64
}

// NewClientPool creates a new client pool with insecure (plaintext) connections
func NewClientPool(registry ServiceRegistry, logger *zap.Logger) *ClientPool {
	return &ClientPool{
		registry:  registry,
		logger:    logger,
		clients:   make(map[string]*grpc.ClientConn),
		tlsConfig: nil,
	}
}

// NewClientPoolWithTLS creates a new client pool with TLS/mTLS enabled
func NewClientPoolWithTLS(registry ServiceRegistry, logger *zap.Logger, tlsCfg *tls.Config) *ClientPool {
	return &ClientPool{
		registry:  registry,
		logger:    logger,
		clients:   make(map[string]*grpc.ClientConn),
		tlsConfig: tlsCfg,
	}
}

// GetConnection gets or creates a gRPC connection to a service
func (p *ClientPool) GetConnection(ctx context.Context, serviceName string) (*grpc.ClientConn, error) {
	p.mu.RLock()
	conn, exists := p.clients[serviceName]
	p.mu.RUnlock()
	if exists {
		if conn.GetState().String() != "SHUTDOWN" {
			return conn, nil
		}
		p.mu.Lock()
		delete(p.clients, serviceName)
		_ = conn.Close()
		p.mu.Unlock()
	}

	address, err := p.getServiceAddress(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get service address: %w", err)
	}

	opts := []grpc.DialOption{
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	}
	if p.tlsConfig != nil {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(p.tlsConfig)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	newConn, err := grpc.Dial(address, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial service: %w", err)
	}

	p.logger.Info("Created gRPC connection",
		zap.String("service", serviceName),
		zap.String("address", address))

	p.mu.Lock()
	if existing, ok := p.clients[serviceName]; ok {
		p.mu.Unlock()
		_ = newConn.Close()
		return existing, nil
	}
	p.clients[serviceName] = newConn
	p.mu.Unlock()

	return newConn, nil
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

	idx := p.rrCounter.Add(1) - 1
	service := services[idx%uint64(len(services))]
	return net.JoinHostPort(service.Address, strconv.Itoa(service.Port)), nil
}

// Close closes all connections
func (p *ClientPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

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
	registry  ServiceRegistry
	logger    *zap.Logger
	rrCounter atomic.Uint64
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

	idx := l.rrCounter.Add(1) - 1
	service := services[idx%uint64(len(services))]
	return net.JoinHostPort(service.Address, strconv.Itoa(service.Port)), nil
}

// CircuitBreaker implements circuit breaker pattern for service calls
type CircuitBreaker struct {
	maxFailures   int
	timeout       time.Duration
	failures      int
	state         string       // "closed", "open", "half-open"
	halfOpenTrial atomic.Int32 // 1 = trial call in progress, 0 = no trial
	logger        *zap.Logger
	mu            sync.Mutex
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(maxFailures, timeout int, logger *zap.Logger) *CircuitBreaker {
	dur := time.Duration(timeout) * time.Second
	if dur <= 0 {
		dur = 30 * time.Second
	}
	return &CircuitBreaker{
		maxFailures: maxFailures,
		timeout:     dur,
		failures:    0,
		state:       "closed",
		logger:      logger,
	}
}

// Call executes a function with circuit breaker protection
func (cb *CircuitBreaker) Call(fn func() error) error {
	cb.mu.Lock()
	if cb.state == "open" {
		cb.mu.Unlock()
		return fmt.Errorf("circuit breaker is open")
	}
	isHalfOpen := cb.state == "half-open"
	if isHalfOpen {
		// Only one caller is allowed to proceed as the trial call.
		// All others get fast-failed while the trial is in progress.
		if !cb.halfOpenTrial.CompareAndSwap(0, 1) {
			cb.mu.Unlock()
			return fmt.Errorf("circuit breaker is half-open (trial in progress)")
		}
	}
	cb.mu.Unlock()

	err := fn()
	if err != nil {
		cb.mu.Lock()
		if isHalfOpen {
			// Trial call failed in half-open → back to open
			cb.state = "open"
			cb.halfOpenTrial.Store(0)
			cb.scheduleRecovery()
			cb.logger.Warn("Circuit breaker reopened (half-open trial failed)")
		} else {
			cb.failures++
			if cb.failures >= cb.maxFailures {
				cb.state = "open"
				cb.scheduleRecovery()
				cb.logger.Warn("Circuit breaker opened", zap.Int("failures", cb.failures))
			}
		}
		cb.mu.Unlock()
		return err
	}

	// Success
	cb.mu.Lock()
	if isHalfOpen {
		// Trial call succeeded in half-open → close
		cb.failures = 0
		cb.state = "closed"
		cb.halfOpenTrial.Store(0)
		cb.logger.Info("Circuit breaker closed (half-open trial succeeded)")
	} else if cb.failures > 0 {
		cb.failures = 0
		cb.state = "closed"
		cb.logger.Info("Circuit breaker closed")
	}
	cb.mu.Unlock()

	return nil
}

// scheduleRecovery schedules a transition from open to half-open after timeout.
// Must be called while holding cb.mu.
func (cb *CircuitBreaker) scheduleRecovery() {
	time.AfterFunc(cb.timeout, func() {
		cb.mu.Lock()
		if cb.state == "open" {
			cb.state = "half-open"
			cb.logger.Info("Circuit breaker transitioned to half-open")
		}
		cb.mu.Unlock()
	})
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}
