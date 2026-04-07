package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/service"
)

// Handler handles HTTP requests
type Handler struct {
	kernel           *core.Microkernel
	logger           *zap.Logger
	metricsCollector *monitoring.MetricsCollector
	alertManager     *monitoring.AlertManager
	serviceMetrics   *monitoring.ServiceMetricsTracker
	metricsHandler   *monitoring.PrometheusMetricsHandler
	web3Service      *service.Web3Service
	authService      *service.AuthService
	nftCache         map[string]cachedNFTAccess
	cacheMu          sync.RWMutex
	initErr          error
}

type cachedNFTAccess struct {
	HasNFT    bool
	Balance   int64
	ExpiresAt time.Time
}

// NewHandler creates a new HTTP handler
func NewHandler(kernel *core.Microkernel, logger *zap.Logger) *Handler {
	handler := &Handler{
		kernel:           kernel,
		logger:           logger,
		metricsCollector: monitoring.NewMetricsCollector(logger),
		alertManager:     monitoring.NewAlertManager(logger),
		nftCache:         make(map[string]cachedNFTAccess),
	}
	handler.serviceMetrics = monitoring.NewServiceMetricsTracker(logger)
	exporter := monitoring.NewPrometheusExporter(handler.metricsCollector, handler.serviceMetrics, logger)
	handler.metricsHandler = monitoring.NewPrometheusMetricsHandler(exporter, logger)

	cfg := kernel.GetConfig()
	web3Service, err := service.NewWeb3Service(cfg, logger)
	if err != nil {
		handler.initErr = err
		return handler
	}
	handler.web3Service = web3Service

	challengeTTL := 5 * time.Minute
	if cfg.Auth.NonceExpiry != "" {
		if parsed, err := time.ParseDuration(cfg.Auth.NonceExpiry); err == nil && parsed > 0 {
			challengeTTL = parsed
		}
	}
	var challengeStore service.ChallengeStore
	redisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	if store, err := service.NewRedisChallengeStore(redisAddr, challengeTTL); err == nil {
		challengeStore = store
	} else {
		logger.Warn("Falling back to in-memory challenge store", zap.Error(err))
	}
	handler.authService = service.NewAuthServiceWithDeps(cfg.Auth.JWTSecret, nil, nil, challengeStore, challengeTTL)

	return handler
}

// HealthHandler handles health check requests
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := h.kernel.Health(ctx); err != nil {
		h.logger.Error("Health check failed", zap.Error(err))
		h.metricsCollector.IncrementCounter("health_check_failed", map[string]string{})
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "error": err.Error()})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("health_check_success", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "healthy"})
}

// ReadyHandler handles readiness check requests
func (h *Handler) ReadyHandler(w http.ResponseWriter, r *http.Request) {

	// Check rate limit
	h.metricsCollector.IncrementCounter("ready_check", map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// NotFoundHandler handles 404 requests
func (h *Handler) NotFoundHandler(w http.ResponseWriter, r *http.Request) {

	// Check rate limit

	h.metricsCollector.IncrementCounter("not_found", map[string]string{"path": r.URL.Path})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (h *Handler) ensureServices(w http.ResponseWriter) bool {
	if h.initErr == nil {
		return true
	}
	h.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": h.initErr.Error()})
	return false
}

func (h *Handler) defaultChainID() int64 {
	if h.kernel != nil && h.kernel.GetConfig() != nil && h.kernel.GetConfig().Web3.ChainID != 0 {
		return h.kernel.GetConfig().Web3.ChainID
	}
	return 11155111
}

func (h *Handler) recordRequest(service string, startedAt time.Time, success bool) {
	if h.serviceMetrics == nil {
		return
	}
	h.serviceMetrics.RecordRequest(service, time.Since(startedAt).Milliseconds(), success)
}

func (h *Handler) getCachedNFTAccess(key string) (cachedNFTAccess, bool) {
	h.cacheMu.RLock()
	entry, ok := h.nftCache[key]
	h.cacheMu.RUnlock()
	if !ok {
		return cachedNFTAccess{}, false
	}
	if time.Now().After(entry.ExpiresAt) {
		h.cacheMu.Lock()
		delete(h.nftCache, key)
		h.cacheMu.Unlock()
		return cachedNFTAccess{}, false
	}
	return entry, true
}

func (h *Handler) setCachedNFTAccess(key string, entry cachedNFTAccess) {
	h.cacheMu.Lock()
	defer h.cacheMu.Unlock()
	h.nftCache[key] = entry
}

// MetricsHandler serves Prometheus metrics.
func (h *Handler) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	if h.metricsHandler == nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "metrics unavailable"})
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.metricsHandler.ServeMetrics()))
}
