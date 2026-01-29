package auth

import (
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/security"
)

// AuthHandler handles authentication requests
type AuthHandler struct {
	verifier         *SignatureVerifier
	logger           *zap.Logger
	kernel           *core.Microkernel
	metricsCollector *monitoring.MetricsCollector
	rateLimiter      *security.RateLimiter
	auditLogger      *security.AuditLogger
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(verifier *SignatureVerifier, logger *zap.Logger, kernel *core.Microkernel) *AuthHandler {
	return &AuthHandler{
		verifier:         verifier,
		logger:           logger,
		kernel:           kernel,
		metricsCollector: monitoring.NewMetricsCollector(logger),
		rateLimiter:      security.NewRateLimiter(50, 5, time.Second, logger),
		auditLogger:      security.NewAuditLogger(logger),
	}
}

// HealthHandler handles health check requests
func (h *AuthHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := h.kernel.Health(ctx); err != nil {
		h.logger.Error("Health check failed", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// ReadyHandler handles readiness check requests
func (h *AuthHandler) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// VerifySignatureHandler handles signature verification requests
func (h *AuthHandler) VerifySignatureHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("verify_signature_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit (strict for auth)
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("verify_signature_rate_limit_exceeded", map[string]string{})
		h.auditLogger.LogEvent("auth", clientIP, "verify_signature", "unknown", "rate_limit_exceeded", nil)
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	ctx := r.Context()

	var req struct {
		Address   string `json:"address"`
		Message   string `json:"message"`
		Signature string `json:"signature"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("Failed to decode request", "error", err)
		h.metricsCollector.IncrementCounter("verify_signature_decode_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}

	valid, err := h.verifier.VerifySignature(ctx, req.Address, req.Message, req.Signature)
	if err != nil {
		h.logger.Error("Failed to verify signature", "error", err)
		h.metricsCollector.IncrementCounter("verify_signature_failed", map[string]string{})
		h.auditLogger.LogEvent("auth", clientIP, "verify_signature", req.Address, "failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "verification failed"})
		return
	}

	// Record metrics
	if valid {
		h.metricsCollector.IncrementCounter("verify_signature_success", map[string]string{})
		h.auditLogger.LogEvent("auth", clientIP, "verify_signature", req.Address, "success", nil)
	} else {
		h.metricsCollector.IncrementCounter("verify_signature_invalid", map[string]string{})
		h.auditLogger.LogEvent("auth", clientIP, "verify_signature", req.Address, "invalid", nil)
	}
	h.metricsCollector.RecordTimer("verify_signature_latency", time.Since(startTime), map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"valid": valid,
	})
}

// VerifyNFTHandler handles NFT verification requests
func (h *AuthHandler) VerifyNFTHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("verify_nft_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("verify_nft_rate_limit_exceeded", map[string]string{})
		h.auditLogger.LogEvent("auth", clientIP, "verify_nft", "unknown", "rate_limit_exceeded", nil)
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	ctx := r.Context()

	var req struct {
		Address         string `json:"address"`
		ContractAddress string `json:"contract_address"`
		TokenID         string `json:"token_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("Failed to decode request", "error", err)
		h.metricsCollector.IncrementCounter("verify_nft_decode_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}

	valid, err := h.verifier.VerifyNFT(ctx, req.Address, req.ContractAddress, req.TokenID)
	if err != nil {
		h.logger.Error("Failed to verify NFT", "error", err)
		h.metricsCollector.IncrementCounter("verify_nft_failed", map[string]string{})
		h.auditLogger.LogEvent("auth", clientIP, "verify_nft", req.Address, "failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "verification failed"})
		return
	}

	// Record metrics
	if valid {
		h.metricsCollector.IncrementCounter("verify_nft_success", map[string]string{})
		h.auditLogger.LogEvent("auth", clientIP, "verify_nft", req.Address, "success", nil)
	} else {
		h.metricsCollector.IncrementCounter("verify_nft_invalid", map[string]string{})
		h.auditLogger.LogEvent("auth", clientIP, "verify_nft", req.Address, "invalid", nil)
	}
	h.metricsCollector.RecordTimer("verify_nft_latency", time.Since(startTime), map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"valid": valid,
	})
}

// VerifyTokenHandler handles token verification requests
func (h *AuthHandler) VerifyTokenHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("verify_token_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("verify_token_rate_limit_exceeded", map[string]string{})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	ctx := r.Context()

	var req struct {
		Token string `json:"token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("Failed to decode request", "error", err)
		h.metricsCollector.IncrementCounter("verify_token_decode_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}

	valid, err := h.verifier.VerifyToken(ctx, req.Token)
	if err != nil {
		h.logger.Error("Failed to verify token", "error", err)
		h.metricsCollector.IncrementCounter("verify_token_failed", map[string]string{})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "verification failed"})
		return
	}

	// Record metrics
	if valid {
		h.metricsCollector.IncrementCounter("verify_token_success", map[string]string{})
	} else {
		h.metricsCollector.IncrementCounter("verify_token_invalid", map[string]string{})
	}
	h.metricsCollector.RecordTimer("verify_token_latency", time.Since(startTime), map[string]string{})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"valid": valid,
	})
}

// GetChallengeHandler handles challenge generation requests
func (h *AuthHandler) GetChallengeHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientIP := r.RemoteAddr

	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("get_challenge_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow(clientIP) {
		h.metricsCollector.IncrementCounter("get_challenge_rate_limit_exceeded", map[string]string{})
		h.auditLogger.LogEvent("auth", clientIP, "get_challenge", "unknown", "rate_limit_exceeded", nil)
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		return
	}

	ctx := r.Context()

	var req struct {
		Address string `json:"address"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("Failed to decode request", "error", err)
		h.metricsCollector.IncrementCounter("get_challenge_decode_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}

	challenge, err := h.verifier.GetChallenge(ctx, req.Address)
	if err != nil {
		h.logger.Error("Failed to generate challenge", "error", err)
		h.metricsCollector.IncrementCounter("get_challenge_failed", map[string]string{})
		h.auditLogger.LogEvent("auth", clientIP, "get_challenge", req.Address, "failed", map[string]interface{}{"error": err.Error()})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate challenge"})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("get_challenge_success", map[string]string{})
	h.metricsCollector.RecordTimer("get_challenge_latency", time.Since(startTime), map[string]string{})
	h.auditLogger.LogEvent("auth", clientIP, "get_challenge", req.Address, "success", nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"challenge": challenge,
	})
}

// NotFoundHandler handles 404 requests
func (h *AuthHandler) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}
