package service

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/hashicorp/consul/api"
	"go.uber.org/zap"
	"streamgate/pkg/core/config"
)

// ServiceRegistry handles service registration and discovery
type ServiceRegistry interface {
	Register(ctx context.Context, service *ServiceInfo) error
	Deregister(ctx context.Context, serviceID string) error
	Discover(ctx context.Context, serviceName string) ([]*ServiceInfo, error)
	Watch(ctx context.Context, serviceName string) (<-chan []*ServiceInfo, error)
	Health(ctx context.Context) error
}

// ServiceInfo contains service registration information
type ServiceInfo struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Address  string            `json:"address"`
	Port     int               `json:"port"`
	Tags     []string          `json:"tags"`
	Metadata map[string]string `json:"metadata"`
	Check    *HealthCheck      `json:"check"`
}

// HealthCheck contains health check configuration
type HealthCheck struct {
	HTTP     string `json:"http"`
	Interval string `json:"interval"`
	Timeout  string `json:"timeout"`
}

// ConsulRegistry implements ServiceRegistry using Consul
type ConsulRegistry struct {
	config *config.Config
	logger *zap.Logger
	client *api.Client
}

// NewConsulRegistry creates a new Consul registry
func NewConsulRegistry(cfg *config.Config, logger *zap.Logger) (*ConsulRegistry, error) {
	logger.Info("Initializing Consul registry",
		zap.String("address", cfg.Consul.Address),
		zap.Int("port", cfg.Consul.Port))

	// Create Consul client
	consulCfg := api.DefaultConfig()
	consulCfg.Address = fmt.Sprintf("%s:%d", cfg.Consul.Address, cfg.Consul.Port)

	client, err := api.NewClient(consulCfg)
	if err != nil {
		logger.Error("Failed to create Consul client", zap.Error(err))
		return nil, fmt.Errorf("failed to create Consul client: %w", err)
	}

	// Verify connection
	_, err = client.Status().Leader()
	if err != nil {
		logger.Error("Failed to connect to Consul", zap.Error(err))
		return nil, fmt.Errorf("failed to connect to Consul: %w", err)
	}

	logger.Info("Connected to Consul", zap.String("address", consulCfg.Address))

	registry := &ConsulRegistry{
		config: cfg,
		logger: logger,
		client: client,
	}

	return registry, nil
}

// Register registers a service with Consul
func (r *ConsulRegistry) Register(ctx context.Context, service *ServiceInfo) error {
	r.logger.Info("Registering service",
		zap.String("service_id", service.ID),
		zap.String("service_name", service.Name))

	// Build Consul service registration
	registration := &api.AgentServiceRegistration{
		ID:      service.ID,
		Name:    service.Name,
		Address: service.Address,
		Port:    service.Port,
		Tags:    service.Tags,
		Meta:    service.Metadata,
	}

	// Add health check if provided
	if service.Check != nil {
		registration.Check = &api.AgentServiceCheck{
			HTTP:     service.Check.HTTP,
			Interval: service.Check.Interval,
			Timeout:  service.Check.Timeout,
		}
	}

	// Register with Consul
	if err := r.client.Agent().ServiceRegister(registration); err != nil {
		r.logger.Error("Failed to register service",
			zap.String("service_id", service.ID),
			zap.Error(err))
		return fmt.Errorf("failed to register service: %w", err)
	}

	r.logger.Info("Service registered successfully",
		zap.String("service_id", service.ID),
		zap.String("service_name", service.Name))
	return nil
}

// Deregister deregisters a service from Consul
func (r *ConsulRegistry) Deregister(ctx context.Context, serviceID string) error {
	r.logger.Info("Deregistering service", zap.String("service_id", serviceID))

	if err := r.client.Agent().ServiceDeregister(serviceID); err != nil {
		r.logger.Error("Failed to deregister service",
			zap.String("service_id", serviceID),
			zap.Error(err))
		return fmt.Errorf("failed to deregister service: %w", err)
	}

	r.logger.Info("Service deregistered successfully", zap.String("service_id", serviceID))
	return nil
}

// Discover discovers services by name
func (r *ConsulRegistry) Discover(ctx context.Context, serviceName string) ([]*ServiceInfo, error) {
	r.logger.Info("Discovering services", zap.String("service_name", serviceName))

	// Query Consul for services
	entries, _, err := r.client.Health().Service(serviceName, "", true, nil)
	if err != nil {
		r.logger.Error("Failed to discover services",
			zap.String("service_name", serviceName),
			zap.Error(err))
		return nil, fmt.Errorf("failed to discover services: %w", err)
	}

	// Convert Consul entries to ServiceInfo
	services := make([]*ServiceInfo, 0, len(entries))
	for _, entry := range entries {
		service := &ServiceInfo{
			ID:       entry.Service.ID,
			Name:     entry.Service.Service,
			Address:  entry.Service.Address,
			Port:     entry.Service.Port,
			Tags:     entry.Service.Tags,
			Metadata: entry.Service.Meta,
		}
		services = append(services, service)
	}

	r.logger.Info("Services discovered",
		zap.String("service_name", serviceName),
		zap.Int("count", len(services)))
	return services, nil
}

// Watch watches for service changes
func (r *ConsulRegistry) Watch(ctx context.Context, serviceName string) (<-chan []*ServiceInfo, error) {
	r.logger.Info("Watching services", zap.String("service_name", serviceName))

	ch := make(chan []*ServiceInfo)

	go func() {
		defer close(ch)

		// Use Consul's blocking queries for watching
		var lastIndex uint64
		for {
			select {
			case <-ctx.Done():
				r.logger.Info("Stopped watching services", zap.String("service_name", serviceName))
				return
			default:
			}

			// Query with blocking
			entries, meta, err := r.client.Health().Service(serviceName, "", true, &api.QueryOptions{
				WaitIndex: lastIndex,
				WaitTime:  5 * 60, // 5 minute timeout
			})

			if err != nil {
				r.logger.Error("Error watching services",
					zap.String("service_name", serviceName),
					zap.Error(err))
				continue
			}

			lastIndex = meta.LastIndex

			// Convert to ServiceInfo
			services := make([]*ServiceInfo, 0, len(entries))
			for _, entry := range entries {
				service := &ServiceInfo{
					ID:       entry.Service.ID,
					Name:     entry.Service.Service,
					Address:  entry.Service.Address,
					Port:     entry.Service.Port,
					Tags:     entry.Service.Tags,
					Metadata: entry.Service.Meta,
				}
				services = append(services, service)
			}

			// Send update
			select {
			case ch <- services:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// Health checks the health of the registry
func (r *ConsulRegistry) Health(ctx context.Context) error {
	// Check Consul connectivity
	_, err := r.client.Status().Leader()
	if err != nil {
		r.logger.Error("Consul health check failed", zap.Error(err))
		return fmt.Errorf("consul health check failed: %w", err)
	}

	r.logger.Debug("Consul health check passed")
	return nil
}

// ServiceClient provides methods to call other services
type ServiceClient struct {
	registry ServiceRegistry
	logger   *zap.Logger
	// TODO: Add gRPC clients for each service
}

// NewServiceClient creates a new service client
func NewServiceClient(registry ServiceRegistry, logger *zap.Logger) *ServiceClient {
	return &ServiceClient{
		registry: registry,
		logger:   logger,
	}
}

// GetServiceAddress gets the address of a service
func (c *ServiceClient) GetServiceAddress(ctx context.Context, serviceName string) (string, error) {
	services, err := c.registry.Discover(ctx, serviceName)
	if err != nil {
		return "", fmt.Errorf("failed to discover service: %w", err)
	}

	if len(services) == 0 {
		return "", fmt.Errorf("service not found: %s", serviceName)
	}

	// Return first available service
	service := services[0]
	return net.JoinHostPort(service.Address, strconv.Itoa(service.Port)), nil
}

// GetAllServiceAddresses gets all addresses of a service
func (c *ServiceClient) GetAllServiceAddresses(ctx context.Context, serviceName string) ([]string, error) {
	services, err := c.registry.Discover(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to discover services: %w", err)
	}

	addresses := make([]string, len(services))
	for i, service := range services {
		addresses[i] = net.JoinHostPort(service.Address, strconv.Itoa(service.Port))
	}

	return addresses, nil
}

// ServiceNames defines all service names
const (
	ServiceAPIGateway = "api-gateway"
	ServiceUpload     = "upload"
	ServiceStreaming  = "streaming"
	ServiceMetadata   = "metadata"
	ServiceAuth       = "auth"
	ServiceCache      = "cache"
	ServiceTranscoder = "transcoder"
	ServiceWorker     = "worker"
	ServiceMonitor    = "monitor"
)

// GetServicePort returns the port for a service
func GetServicePort(serviceName string) int {
	ports := map[string]int{
		ServiceAPIGateway: 9090,
		ServiceUpload:     9091,
		ServiceStreaming:  9093,
		ServiceMetadata:   9005,
		ServiceAuth:       9007,
		ServiceCache:      9006,
		ServiceTranscoder: 9092,
		ServiceWorker:     9008,
		ServiceMonitor:    9009,
	}

	if port, ok := ports[serviceName]; ok {
		return port
	}

	return 8080 // Default port
}
