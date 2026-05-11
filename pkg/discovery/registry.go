package discovery

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ServiceInfo represents service information
type ServiceInfo struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Address  string            `json:"address"`
	Port     int               `json:"port"`
	Tags     []string          `json:"tags"`
	Meta     map[string]string `json:"meta"`
	Health   HealthCheck       `json:"health"`
	Status   ServiceStatus     `json:"status"`
	LastSeen time.Time         `json:"last_seen"`
}

// HealthCheck represents health check configuration
type HealthCheck struct {
	HTTP     string        `json:"http"`
	Interval time.Duration `json:"interval"`
	Timeout  time.Duration `json:"timeout"`
}

// ServiceStatus represents service status
type ServiceStatus int

const (
	StatusUnknown ServiceStatus = iota
	StatusHealthy
	StatusUnhealthy
	StatusDraining
)

func (s ServiceStatus) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusUnhealthy:
		return "unhealthy"
	case StatusDraining:
		return "draining"
	default:
		return "unknown"
	}
}

// ServiceRegistry defines the interface for service registry
type ServiceRegistry interface {
	Register(ctx context.Context, service ServiceInfo) error
	Deregister(ctx context.Context, serviceID string) error
	Discover(ctx context.Context, serviceName string) ([]ServiceInfo, error)
	Watch(ctx context.Context, serviceName string) (<-chan []ServiceInfo, error)
	HealthCheck(ctx context.Context) error
}

// MemoryRegistry implements ServiceRegistry using an in-memory store
type MemoryRegistry struct {
	services map[string][]ServiceInfo
	mu       sync.RWMutex
	logger   *zap.Logger
	watchers map[string][]chan []ServiceInfo
}

// NewMemoryRegistry creates a new in-memory registry
func NewMemoryRegistry(logger *zap.Logger) *MemoryRegistry {
	return &MemoryRegistry{
		services: make(map[string][]ServiceInfo),
		logger:   logger,
		watchers: make(map[string][]chan []ServiceInfo),
	}
}

// Register registers a service
func (r *MemoryRegistry) Register(ctx context.Context, service ServiceInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if service.ID == "" {
		service.ID = fmt.Sprintf("%s-%d", service.Name, time.Now().UnixNano())
	}

	service.Status = StatusHealthy
	service.LastSeen = time.Now()

	r.services[service.Name] = append(r.services[service.Name], service)

	r.logger.Info("Service registered",
		zap.String("service", service.Name),
		zap.String("id", service.ID),
		zap.String("address", fmt.Sprintf("%s:%d", service.Address, service.Port)))

	go r.monitorServiceHealth(ctx, service)

	return nil
}

// Deregister removes a service
func (r *MemoryRegistry) Deregister(ctx context.Context, serviceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, services := range r.services {
		for i, svc := range services {
			if svc.ID == serviceID {
				r.services[name] = append(services[:i], services[i+1:]...)
				r.logger.Info("Service deregistered",
					zap.String("service", name),
					zap.String("id", serviceID))
				return nil
			}
		}
	}

	return fmt.Errorf("service '%s' not found", serviceID)
}

// Discover returns all instances of a service
func (r *MemoryRegistry) Discover(ctx context.Context, serviceName string) ([]ServiceInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	services, exists := r.services[serviceName]
	if !exists {
		return nil, fmt.Errorf("service '%s' not found", serviceName)
	}

	healthy := make([]ServiceInfo, 0, len(services))
	for _, svc := range services {
		if svc.Status == StatusHealthy {
			healthy = append(healthy, svc)
		}
	}

	return healthy, nil
}

// Watch watches for changes to a service
func (r *MemoryRegistry) Watch(ctx context.Context, serviceName string) (<-chan []ServiceInfo, error) {
	ch := make(chan []ServiceInfo, 10)

	r.mu.Lock()
	r.watchers[serviceName] = append(r.watchers[serviceName], ch)
	// Send initial state if available — copy services while holding lock
	if services, exists := r.services[serviceName]; exists {
		healthy := make([]ServiceInfo, 0, len(services))
		for _, svc := range services {
			if svc.Status == StatusHealthy {
				healthy = append(healthy, svc)
			}
		}
		ch <- healthy
	}
	r.mu.Unlock()

	go func() {
		<-ctx.Done()
		r.mu.Lock()
		defer r.mu.Unlock()
		for i, w := range r.watchers[serviceName] {
			if w == ch {
				r.watchers[serviceName] = append(r.watchers[serviceName][:i], r.watchers[serviceName][i+1:]...)
				close(ch)
				break
			}
		}
	}()

	return ch, nil
}

// HealthCheck checks registry health
func (r *MemoryRegistry) HealthCheck(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.services) == 0 {
		return fmt.Errorf("no services registered")
	}

	return nil
}

// monitorServiceHealth monitors a service's health
func (r *MemoryRegistry) monitorServiceHealth(ctx context.Context, service ServiceInfo) {
	if service.Health.HTTP == "" {
		return
	}

	ticker := time.NewTicker(service.Health.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			healthy := r.checkServiceHealth(service)
			r.updateServiceStatus(service, healthy)
		}
	}
}

// checkServiceHealth checks if a service is healthy by making an HTTP GET request.
func (r *MemoryRegistry) checkServiceHealth(service ServiceInfo) bool {
	if service.Address == "" {
		return true
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(service.Address)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode < 500
}

// updateServiceStatus updates a service's status in the registry map.
func (r *MemoryRegistry) updateServiceStatus(service ServiceInfo, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	newStatus := StatusHealthy
	if !healthy {
		newStatus = StatusUnhealthy
	}

	// Find and update the service entry directly in the map
	found := false
	for i := range r.services[service.Name] {
		if r.services[service.Name][i].ID != service.ID {
			continue
		}
		if r.services[service.Name][i].Status == newStatus {
			return
		}
		r.services[service.Name][i].Status = newStatus
		r.services[service.Name][i].LastSeen = time.Now()
		found = true
		break
	}

	if !found {
		return
	}

	r.logger.Info("Service status changed",
		zap.String("service", service.Name),
		zap.String("id", service.ID),
		zap.String("status", newStatus.String()))

	r.notifyWatchers(service.Name)
}

// notifyWatchers notifies all watchers of a service.
// Caller must hold r.mu.
func (r *MemoryRegistry) notifyWatchers(serviceName string) {
	services := r.services[serviceName]
	healthy := make([]ServiceInfo, 0, len(services))
	for _, svc := range services {
		if svc.Status == StatusHealthy {
			healthy = append(healthy, svc)
		}
	}

	for _, ch := range r.watchers[serviceName] {
		select {
		case ch <- healthy:
		default:
		}
	}
}

// LoadBalancer defines load balancing strategies
type LoadBalancer interface {
	GetInstance(ctx context.Context, serviceName string) (*ServiceInfo, error)
}

// LoadBalanceStrategy represents load balancing strategies
type LoadBalanceStrategy int

const (
	RoundRobin LoadBalanceStrategy = iota
	Random
	LeastConn
	WeightedRoundRobin
)

// LoadBalancerImpl implements load balancing
type LoadBalancerImpl struct {
	registry ServiceRegistry
	strategy LoadBalanceStrategy
	counters map[string]int
	mu       sync.RWMutex
	logger   *zap.Logger
}

// NewLoadBalancer creates a new load balancer
func NewLoadBalancer(registry ServiceRegistry, strategy LoadBalanceStrategy, logger *zap.Logger) *LoadBalancerImpl {
	return &LoadBalancerImpl{
		registry: registry,
		strategy: strategy,
		counters: make(map[string]int),
		logger:   logger,
	}
}

// GetInstance returns a service instance based on strategy
func (lb *LoadBalancerImpl) GetInstance(ctx context.Context, serviceName string) (*ServiceInfo, error) {
	instances, err := lb.registry.Discover(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	if len(instances) == 0 {
		return nil, fmt.Errorf("no healthy instances for service '%s'", serviceName)
	}

	switch lb.strategy {
	case RoundRobin:
		return lb.roundRobin(serviceName, instances)
	case Random:
		return lb.random(instances)
	case LeastConn:
		return lb.leastConn(instances)
	case WeightedRoundRobin:
		return lb.weightedRoundRobin(serviceName, instances)
	default:
		return lb.roundRobin(serviceName, instances)
	}
}

// roundRobin implements round-robin strategy
func (lb *LoadBalancerImpl) roundRobin(serviceName string, instances []ServiceInfo) (*ServiceInfo, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	idx := lb.counters[serviceName] % len(instances)
	lb.counters[serviceName]++

	return &instances[idx], nil
}

// random implements random strategy
func (lb *LoadBalancerImpl) random(instances []ServiceInfo) (*ServiceInfo, error) {
	idx := time.Now().UnixNano() % int64(len(instances))
	return &instances[idx], nil
}

// leastConn implements least connections strategy
func (lb *LoadBalancerImpl) leastConn(instances []ServiceInfo) (*ServiceInfo, error) {
	minIdx := 0
	minConns := int64(^uint64(0) >> 1)

	for i, svc := range instances {
		conns := int64(0)
		if connStr, ok := svc.Meta["connections"]; ok {
			_, _ = fmt.Sscanf(connStr, "%d", &conns)
		}

		if conns < minConns {
			minConns = conns
			minIdx = i
		}
	}

	return &instances[minIdx], nil
}

// weightedRoundRobin implements weighted round-robin strategy
func (lb *LoadBalancerImpl) weightedRoundRobin(serviceName string, instances []ServiceInfo) (*ServiceInfo, error) {
	weights := make([]int, len(instances))
	totalWeight := 0

	for i, svc := range instances {
		weight := 1
		if w, ok := svc.Meta["weight"]; ok {
			_, _ = fmt.Sscanf(w, "%d", &weight)
		}
		weights[i] = weight
		totalWeight += weight
	}

	lb.mu.Lock()
	defer lb.mu.Unlock()

	current := lb.counters[serviceName] % totalWeight
	lb.counters[serviceName]++

	cumWeight := 0
	for i, w := range weights {
		cumWeight += w
		if current < cumWeight {
			return &instances[i], nil
		}
	}

	return &instances[0], nil
}

// ServiceDiscovery combines registry and load balancer
type ServiceDiscovery struct {
	registry     ServiceRegistry
	loadBalancer LoadBalancer
	logger       *zap.Logger
}

// NewServiceDiscovery creates a new service discovery
func NewServiceDiscovery(registry ServiceRegistry, strategy LoadBalanceStrategy, logger *zap.Logger) *ServiceDiscovery {
	return &ServiceDiscovery{
		registry:     registry,
		loadBalancer: NewLoadBalancer(registry, strategy, logger),
		logger:       logger,
	}
}

// GetService returns a service instance
func (sd *ServiceDiscovery) GetService(ctx context.Context, serviceName string) (*ServiceInfo, error) {
	return sd.loadBalancer.GetInstance(ctx, serviceName)
}

// GetAllServices returns all instances of a service
func (sd *ServiceDiscovery) GetAllServices(ctx context.Context, serviceName string) ([]ServiceInfo, error) {
	return sd.registry.Discover(ctx, serviceName)
}

// WatchServices watches for changes to a service
func (sd *ServiceDiscovery) WatchServices(ctx context.Context, serviceName string) (<-chan []ServiceInfo, error) {
	return sd.registry.Watch(ctx, serviceName)
}

// RegisterService registers a service
func (sd *ServiceDiscovery) RegisterService(ctx context.Context, service ServiceInfo) error {
	return sd.registry.Register(ctx, service)
}

// DeregisterService removes a service
func (sd *ServiceDiscovery) DeregisterService(ctx context.Context, serviceID string) error {
	return sd.registry.Deregister(ctx, serviceID)
}
