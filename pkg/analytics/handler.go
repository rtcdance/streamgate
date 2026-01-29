package analytics

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// Handler provides HTTP handlers for analytics
type Handler struct {
	service   *Service
	predictor *Predictor
}

// NewHandler creates a new analytics handler
func NewHandler(service *Service, predictor *Predictor) *Handler {
	return &Handler{
		service:   service,
		predictor: predictor,
	}
}

// RecordEventHandler handles event recording
func (h *Handler) RecordEventHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		EventType string                 `json:"event_type"`
		ServiceID string                 `json:"service_id"`
		UserID    string                 `json:"user_id"`
		Metadata  map[string]interface{} `json:"metadata"`
		Tags      map[string]string      `json:"tags"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	h.service.RecordEvent(req.EventType, req.ServiceID, req.UserID, req.Metadata, req.Tags)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "recorded"})
}

// RecordMetricsHandler handles metrics recording
func (h *Handler) RecordMetricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ServiceID    string  `json:"service_id"`
		CPUUsage     float64 `json:"cpu_usage"`
		MemoryUsage  float64 `json:"memory_usage"`
		DiskUsage    float64 `json:"disk_usage"`
		RequestRate  float64 `json:"request_rate"`
		ErrorRate    float64 `json:"error_rate"`
		Latency      float64 `json:"latency"`
		CacheHitRate float64 `json:"cache_hit_rate"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	h.service.RecordMetrics(req.ServiceID, req.CPUUsage, req.MemoryUsage, req.DiskUsage, req.RequestRate, req.ErrorRate, req.Latency, req.CacheHitRate)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "recorded"})
}

// GetAggregationsHandler returns aggregations for a service
func (h *Handler) GetAggregationsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	serviceID := r.URL.Query().Get("service_id")
	if serviceID == "" {
		http.Error(w, "Missing service_id", http.StatusBadRequest)
		return
	}

	aggregations := h.service.GetAggregations(serviceID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(aggregations)
}

// GetAnomaliesHandler returns anomalies for a service
func (h *Handler) GetAnomaliesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	serviceID := r.URL.Query().Get("service_id")
	if serviceID == "" {
		http.Error(w, "Missing service_id", http.StatusBadRequest)
		return
	}

	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	anomalies := h.service.GetAnomalies(serviceID, limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(anomalies)
}

// GetPredictionsHandler returns predictions for a service
func (h *Handler) GetPredictionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	serviceID := r.URL.Query().Get("service_id")
	if serviceID == "" {
		http.Error(w, "Missing service_id", http.StatusBadRequest)
		return
	}

	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	predictions := h.predictor.GetPredictions(serviceID, limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(predictions)
}

// GetDashboardHandler returns dashboard data
func (h *Handler) GetDashboardHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	serviceID := r.URL.Query().Get("service_id")
	if serviceID == "" {
		http.Error(w, "Missing service_id", http.StatusBadRequest)
		return
	}

	data := h.service.GetDashboardData(serviceID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// HealthHandler returns health status
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}
