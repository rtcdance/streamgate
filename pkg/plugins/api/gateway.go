package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"streamgate/pkg/core"
	"streamgate/pkg/core/config"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/optimization"
	"streamgate/pkg/security"
	"go.uber.org/zap"
)

// GatewayPlugin is the API Gateway plugin
type GatewayPlugin struct {
	name              string
	kernel            *core.Microkernel
	logger            *zap.Logger
	server            *http.Server
	config            *config.Config
	metricsCollector  *monitoring.MetricsCollector
	alertManager      *monitoring.AlertManager
	rateLimiter       *security.RateLimiter
	auditLogger       *security.AuditLogger
	cache             *optimization.LocalCache
}

// NewGatewayPlugin creates a new API Gateway plugin
func NewGatewayPlugin(cfg *config.Config, logger *zap.Logger) *GatewayPlugin {
	return &GatewayPlugin{
		name:   "api-gateway",
		logger: logger,
		config: cfg,
	}
}

// Name returns the plugin name
func (p *GatewayPlugin) Name() string {
	return p.name
}

// Version returns the plugin version
func (p *GatewayPlugin) Version() string {
	return "1.0.0"
}

// Init initializes the plugin
func (p *GatewayPlugin) Init(ctx context.Context, kernel *core.Microkernel) error {
	p.kernel = kernel
	p.logger.Info("Initializing API Gateway plugin")

	// Initialize monitoring
	p.metricsCollector = monitoring.NewMetricsCollector(p.logger)
	p.alertManager = monitoring.NewAlertManager(p.logger)

	// Initialize security
	p.rateLimiter = security.NewRateLimiter(1000, 100, time.Second, p.logger)
	p.auditLogger = security.NewAuditLogger(p.logger)

	// Initialize optimization
	p.cache = optimization.NewLocalCache(10000, 5*time.Minute, p.logger)

	// Register alert rules
	p.registerAlertRules()

	// Register alert handlers
	p.registerAlertHandlers()

	return nil
}

// Start starts the API Gateway
func (p *GatewayPlugin) Start(ctx context.Context) error {
	p.logger.Info("Starting API Gateway", "port", p.config.Server.Port)

	handler := NewHandler(p.kernel, p.logger)

	mux := http.NewServeMux()

	// Health and readiness endpoints
	mux.HandleFunc("/health", handler.HealthHandler)
	mux.HandleFunc("/ready", handler.ReadyHandler)

	// API v1 endpoints
	mux.HandleFunc("/api/v1/health", handler.HealthHandler)

	// Catch-all for 404
	mux.HandleFunc("/", handler.NotFoundHandler)

	p.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", p.config.Server.Port),
		Handler:      mux,
		ReadTimeout:  time.Duration(p.config.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(p.config.Server.WriteTimeout) * time.Second,
	}

	go func() {
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			p.logger.Error("API Gateway server error", "error", err)
		}
	}()

	p.logger.Info("API Gateway started successfully", "port", p.config.Server.Port)
	return nil
}

// Stop stops the API Gateway
func (p *GatewayPlugin) Stop(ctx context.Context) error {
	p.logger.Info("Stopping API Gateway")

	if p.server != nil {
		if err := p.server.Shutdown(ctx); err != nil {
			p.logger.Error("Error shutting down API Gateway", "error", err)
			return err
		}
	}

	// Stop cache cleanup
	if p.cache != nil {
		p.cache.Stop()
	}

	p.logger.Info("API Gateway stopped")
	return nil
}

// Health checks the health of the API Gateway
func (p *GatewayPlugin) Health(ctx context.Context) error {
	if p.server == nil {
		return fmt.Errorf("API Gateway not started")
	}

	// Simple health check - server is running
	return nil
}

// registerAlertRules registers alert rules
func (p *GatewayPlugin) registerAlertRules() {
	// High error rate alert
	p.alertManager.AddRule(&monitoring.AlertRule{
		ID:        "high-error-rate",
		Name:      "High Error Rate",
		Metric:    "error_rate",
		Condition: "gt",
		Threshold: 0.1,
		Duration:  1 * time.Minute,
		Level:     "critical",
		Enabled:   true,
	})

	// High latency alert
	p.alertManager.AddRule(&monitoring.AlertRule{
		ID:        "high-latency",
		Name:      "High Latency",
		Metric:    "request_latency",
		Condition: "gt",
		Threshold: 5000, // 5 seconds in ms
		Duration:  1 * time.Minute,
		Level:     "warning",
		Enabled:   true,
	})
}

// registerAlertHandlers registers alert handlers
func (p *GatewayPlugin) registerAlertHandlers() {
	// Log alert handler
	p.alertManager.RegisterHandler(func(alert *monitoring.Alert) error {
		p.logger.Warn("Alert triggered",
			"alert_id", alert.ID,
			"level", alert.Level,
			"title", alert.Title,
			"message", alert.Message,
		)
		return nil
	})
}
