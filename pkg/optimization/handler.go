package optimization

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// Handler provides HTTP handlers for optimization
type Handler struct {
	service *Service
}

// NewHandler creates a new optimization handler
func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

// GetCacheStatsHandler returns cache statistics
func (h *Handler) GetCacheStatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := h.service.GetCacheStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// GetSlowQueriesHandler returns slow queries
func (h *Handler) GetSlowQueriesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	queries := h.service.GetSlowQueries(limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(queries)
}

// GetQueryMetricsHandler returns query metrics
func (h *Handler) GetQueryMetricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	metrics := h.service.GetQueryMetrics(limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// GetQueryStatsHandler returns query statistics
func (h *Handler) GetQueryStatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("query")
	if query == "" {
		http.Error(w, "Missing query parameter", http.StatusBadRequest)
		return
	}

	stats := h.service.GetQueryStats(query)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// GetIndexMetricsHandler returns index metrics
func (h *Handler) GetIndexMetricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	indexName := r.URL.Query().Get("index_name")
	if indexName == "" {
		// Return all indexes
		metrics := h.service.GetAllIndexMetrics()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metrics)
		return
	}

	metric := h.service.GetIndexMetrics(indexName)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metric)
}

// GetUnusedIndexesHandler returns unused indexes
func (h *Handler) GetUnusedIndexesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	indexes := h.service.GetUnusedIndexes()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(indexes)
}

// GetDuplicateIndexesHandler returns duplicate indexes
func (h *Handler) GetDuplicateIndexesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	indexes := h.service.GetDuplicateIndexes()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(indexes)
}

// GetFragmentedIndexesHandler returns fragmented indexes
func (h *Handler) GetFragmentedIndexesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	threshold := 30.0
	if thresholdStr := r.URL.Query().Get("threshold"); thresholdStr != "" {
		if t, err := strconv.ParseFloat(thresholdStr, 64); err == nil {
			threshold = t
		}
	}

	indexes := h.service.GetFragmentedIndexes(threshold)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(indexes)
}

// GetOptimizationRecommendationsHandler returns optimization recommendations
func (h *Handler) GetOptimizationRecommendationsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	recommendations := h.service.GetOptimizationRecommendations()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(recommendations)
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

// GetMemoryMetricsHandler returns memory metrics
func (h *Handler) GetMemoryMetricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	metrics := h.service.GetMemoryMetrics(limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// GetCPUMetricsHandler returns CPU metrics
func (h *Handler) GetCPUMetricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	metrics := h.service.GetCPUMetrics(limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// GetMemoryStatsHandler returns memory statistics
func (h *Handler) GetMemoryStatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := h.service.GetMemoryStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// GetCPUStatsHandler returns CPU statistics
func (h *Handler) GetCPUStatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := h.service.GetCPUStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// GetMemoryTrendsHandler returns memory trends
func (h *Handler) GetMemoryTrendsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	trends := h.service.GetMemoryTrends()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(trends)
}

// GetCPUTrendsHandler returns CPU trends
func (h *Handler) GetCPUTrendsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	trends := h.service.GetCPUTrends()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(trends)
}

// ForceGCHandler forces garbage collection
func (h *Handler) ForceGCHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.service.ForceGC()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "gc_triggered"})
}
