package auth

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
	"streamgate/pkg/core"
	"streamgate/pkg/monitoring"
)

// AuthHandler handles authentication requests
type AuthHandler struct {
	verifier         *AuthVerifier
	logger           *zap.Logger
	kernel           *core.Microkernel
	metricsCollector *monitoring.MetricsCollector
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(verifier *AuthVerifier, logger *zap.Logger, kernel *core.Microkernel) *AuthHandler {
	return &AuthHandler{
		verifier:         verifier,
		logger:           logger,
		kernel:           kernel,
		metricsCollector: monitoring.NewMetricsCollector(logger),
	}
}

// HealthHandler handles health check requests
func (h *AuthHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := h.kernel.Health(ctx); err != nil {
		h.logger.Error("Health check failed", zap.Error(err))
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
	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("verify_signature_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit (strict for auth)

	ctx := r.Context()

	var req struct {
		Address   string `json:"address"`
		Message   string `json:"message"`
		Signature string `json:"signature"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("Failed to decode request", zap.Error(err))
		h.metricsCollector.IncrementCounter("verify_signature_decode_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}

	valid, err := h.verifier.VerifySignature(ctx, req.Address, req.Message, req.Signature)
	if err != nil {
		h.logger.Error("Failed to verify signature", zap.Error(err))
		h.metricsCollector.IncrementCounter("verify_signature_failed", map[string]string{})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "verification failed"})
		return
	}

	// Record metrics
	if valid {
		h.metricsCollector.IncrementCounter("verify_signature_success", map[string]string{})
	} else {
		h.metricsCollector.IncrementCounter("verify_signature_invalid", map[string]string{})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"valid": valid,
	})
}

// VerifyNFTHandler handles NFT verification requests
func (h *AuthHandler) VerifyNFTHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("verify_nft_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit

	ctx := r.Context()

	var req struct {
		Address         string `json:"address"`
		ContractAddress string `json:"contract_address"`
		TokenID         string `json:"token_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("Failed to decode request", zap.Error(err))
		h.metricsCollector.IncrementCounter("verify_nft_decode_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}

	valid, err := h.verifier.VerifyNFT(ctx, req.Address, req.ContractAddress, req.TokenID)
	if err != nil {
		h.logger.Error("Failed to verify NFT", zap.Error(err))
		h.metricsCollector.IncrementCounter("verify_nft_failed", map[string]string{})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "verification failed"})
		return
	}

	// Record metrics
	if valid {
		h.metricsCollector.IncrementCounter("verify_nft_success", map[string]string{})
	} else {
		h.metricsCollector.IncrementCounter("verify_nft_invalid", map[string]string{})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"valid": valid,
	})
}

// VerifyTokenHandler handles token verification requests
func (h *AuthHandler) VerifyTokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("verify_token_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit

	ctx := r.Context()

	var req struct {
		Token string `json:"token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("Failed to decode request", zap.Error(err))
		h.metricsCollector.IncrementCounter("verify_token_decode_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}

	valid, err := h.verifier.VerifyToken(ctx, req.Token)
	if err != nil {
		h.logger.Error("Failed to verify token", zap.Error(err))
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"valid": valid,
	})
}

// GetChallengeHandler handles challenge generation requests
func (h *AuthHandler) GetChallengeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("get_challenge_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	// Check rate limit

	ctx := r.Context()

	var req struct {
		Address string `json:"address"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("Failed to decode request", zap.Error(err))
		h.metricsCollector.IncrementCounter("get_challenge_decode_error", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}

	challenge, err := h.verifier.GetChallenge(ctx, req.Address)
	if err != nil {
		h.logger.Error("Failed to generate challenge", zap.Error(err))
		h.metricsCollector.IncrementCounter("get_challenge_failed", map[string]string{})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to generate challenge"})
		return
	}

	// Record metrics
	h.metricsCollector.IncrementCounter("get_challenge_success", map[string]string{})

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
