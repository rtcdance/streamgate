package debug

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// Handler provides HTTP handlers for debugging
type Handler struct {
	service *Service
}

// NewHandler creates a new debug handler
func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

// SetBreakpointHandler handles breakpoint setting
func (h *Handler) SetBreakpointHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Location  string `json:"location"`
		Condition string `json:"condition"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	id := h.service.SetBreakpoint(req.Location, req.Condition)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

// GetBreakpointsHandler returns all breakpoints
func (h *Handler) GetBreakpointsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	breakpoints := h.service.GetBreakpoints()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(breakpoints)
}

// WatchVariableHandler handles variable watching
func (h *Handler) WatchVariableHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name  string      `json:"name"`
		Value interface{} `json:"value"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	id := h.service.WatchVariable(req.Name, req.Value)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

// GetWatchVariablesHandler returns all watched variables
func (h *Handler) GetWatchVariablesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	variables := h.service.GetWatchVariables()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(variables)
}

// GetTracesHandler returns debug traces
func (h *Handler) GetTracesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	traces := h.service.GetTraces(limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(traces)
}

// GetLogsHandler returns debug logs
func (h *Handler) GetLogsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	level := r.URL.Query().Get("level")
	var logs []*DebugLog

	if level != "" {
		logs = h.service.GetLogsByLevel(level, limit)
	} else {
		logs = h.service.GetLogs(limit)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

// GetMemProfilesHandler returns memory profiles
func (h *Handler) GetMemProfilesHandler(w http.ResponseWriter, r *http.Request) {
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

	profiles := h.service.GetMemProfiles(limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profiles)
}

// GetGoroutineProfilesHandler returns goroutine profiles
func (h *Handler) GetGoroutineProfilesHandler(w http.ResponseWriter, r *http.Request) {
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

	profiles := h.service.GetGoroutineProfiles(limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profiles)
}

// GetOptimizationRecommendationsHandler returns optimization recommendations
func (h *Handler) GetOptimizationRecommendationsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	recommendations := h.service.GetOptimizationRecommendations()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"recommendations": recommendations,
		"memory_leak":     h.service.DetectMemoryLeak(),
		"goroutine_leak":  h.service.DetectGoroutineLeak(),
	})
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
