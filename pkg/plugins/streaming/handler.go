package streaming

import (
	"encoding/json"
	"net/http"

	"github.com/rtcdance/streamgate/pkg/core"
	"github.com/rtcdance/streamgate/pkg/monitoring"

	"go.uber.org/zap"
)

// StreamingHandler handles streaming requests
type StreamingHandler struct {
	cache            *StreamCache
	logger           *zap.Logger
	kernel           *core.Microkernel
	metricsCollector *monitoring.MetricsCollector
}

// NewStreamingHandler creates a new streaming handler
func NewStreamingHandler(cache *StreamCache, logger *zap.Logger, kernel *core.Microkernel) *StreamingHandler {
	return &StreamingHandler{
		cache:            cache,
		logger:           logger,
		kernel:           kernel,
		metricsCollector: monitoring.NewMetricsCollector(logger),
	}
}

// HealthHandler handles health check requests
func (h *StreamingHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := h.kernel.Health(ctx); err != nil {
		h.logger.Error("Health check failed", zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// ReadyHandler handles readiness check requests
func (h *StreamingHandler) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// GetHLSPlaylistHandler handles HLS playlist requests
func (h *StreamingHandler) GetHLSPlaylistHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("hls_playlist_invalid_method", map[string]string{})
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	contentID := r.URL.Query().Get("content_id")
	if contentID == "" {
		h.metricsCollector.IncrementCounter("hls_playlist_missing_id", map[string]string{})
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing content_id"})
		return
	}

	h.logger.Info("Generating HLS playlist", zap.String("content_id", contentID))

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-VERSION:3\n"))
}

// GetDASHManifestHandler handles DASH manifest requests
func (h *StreamingHandler) GetDASHManifestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	contentID := r.URL.Query().Get("content_id")
	if contentID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing content_id"})
		return
	}

	h.logger.Info("Generating DASH manifest", zap.String("content_id", contentID))

	w.Header().Set("Content-Type", "application/dash+xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><MPD></MPD>`))
}

// GetSegmentHandler handles segment requests
func (h *StreamingHandler) GetSegmentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	contentID := r.URL.Query().Get("content_id")
	segmentID := r.URL.Query().Get("segment_id")

	if contentID == "" || segmentID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing content_id or segment_id"})
		return
	}

	h.logger.Info("Retrieving segment", zap.String("content_id", contentID), zap.String("segment_id", segmentID))

	w.Header().Set("Content-Type", "video/mp2t")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte{})
}

// GetStreamInfoHandler handles stream info requests
func (h *StreamingHandler) GetStreamInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	contentID := r.URL.Query().Get("content_id")
	if contentID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing content_id"})
		return
	}

	h.logger.Info("Getting stream info", zap.String("content_id", contentID))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"content_id": contentID,
		"formats":    []string{"hls", "dash"},
		"bitrates":   []int{1000, 2500, 5000},
	})
}

// NotFoundHandler handles 404 requests
func (h *StreamingHandler) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "NOT_FOUND", "code": "NOT_FOUND", "message": "resource not found"})
}
