package analytics

import (
	"time"
)

// AnalyticsEvent represents a single analytics event
type AnalyticsEvent struct {
	ID        string                 `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	EventType string                 `json:"event_type"`
	ServiceID string                 `json:"service_id"`
	UserID    string                 `json:"user_id,omitempty"`
	Metadata  map[string]interface{} `json:"metadata"`
	Tags      map[string]string      `json:"tags"`
}

// MetricsSnapshot represents a snapshot of system metrics
type MetricsSnapshot struct {
	ID          string    `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	ServiceID   string    `json:"service_id"`
	CPUUsage    float64   `json:"cpu_usage"`
	MemoryUsage float64   `json:"memory_usage"`
	DiskUsage   float64   `json:"disk_usage"`
	RequestRate float64   `json:"request_rate"`
	ErrorRate   float64   `json:"error_rate"`
	Latency     float64   `json:"latency_ms"`
	CacheHitRate float64  `json:"cache_hit_rate"`
}

// UserBehavior represents user behavior analytics
type UserBehavior struct {
	ID              string    `json:"id"`
	Timestamp       time.Time `json:"timestamp"`
	UserID          string    `json:"user_id"`
	Action          string    `json:"action"`
	ContentID       string    `json:"content_id,omitempty"`
	Duration        int64     `json:"duration_ms"`
	Success         bool      `json:"success"`
	ErrorMessage    string    `json:"error_message,omitempty"`
	ClientIP        string    `json:"client_ip"`
	UserAgent       string    `json:"user_agent"`
	SessionID       string    `json:"session_id"`
}

// PerformanceMetric represents performance metrics
type PerformanceMetric struct {
	ID           string    `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	ServiceID    string    `json:"service_id"`
	Operation    string    `json:"operation"`
	Duration     float64   `json:"duration_ms"`
	Success      bool      `json:"success"`
	ErrorType    string    `json:"error_type,omitempty"`
	ResourceUsed float64   `json:"resource_used"`
	Throughput   float64   `json:"throughput"`
}

// BusinessMetric represents business metrics
type BusinessMetric struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	MetricType string   `json:"metric_type"`
	Value     float64   `json:"value"`
	Unit      string    `json:"unit"`
	Dimension map[string]string `json:"dimension"`
}

// AnomalyDetection represents detected anomalies
type AnomalyDetection struct {
	ID          string    `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	ServiceID   string    `json:"service_id"`
	MetricName  string    `json:"metric_name"`
	CurrentValue float64  `json:"current_value"`
	ExpectedValue float64 `json:"expected_value"`
	Deviation   float64   `json:"deviation_percent"`
	Severity    string    `json:"severity"` // low, medium, high, critical
	Description string    `json:"description"`
}

// PredictionResult represents ML prediction results
type PredictionResult struct {
	ID            string    `json:"id"`
	Timestamp     time.Time `json:"timestamp"`
	PredictionType string   `json:"prediction_type"`
	ServiceID     string    `json:"service_id"`
	PredictedValue float64  `json:"predicted_value"`
	Confidence    float64   `json:"confidence"`
	TimeHorizon   string    `json:"time_horizon"` // 5m, 15m, 1h, 1d
	Recommendation string   `json:"recommendation"`
}

// AnalyticsAggregation represents aggregated analytics data
type AnalyticsAggregation struct {
	ID            string    `json:"id"`
	Timestamp     time.Time `json:"timestamp"`
	ServiceID     string    `json:"service_id"`
	Period        string    `json:"period"` // 1m, 5m, 15m, 1h, 1d
	EventCount    int64     `json:"event_count"`
	AvgLatency    float64   `json:"avg_latency_ms"`
	P50Latency    float64   `json:"p50_latency_ms"`
	P95Latency    float64   `json:"p95_latency_ms"`
	P99Latency    float64   `json:"p99_latency_ms"`
	ErrorCount    int64     `json:"error_count"`
	ErrorRate     float64   `json:"error_rate"`
	SuccessRate   float64   `json:"success_rate"`
	Throughput    float64   `json:"throughput"`
}

// DashboardData represents data for dashboard visualization
type DashboardData struct {
	Timestamp       time.Time `json:"timestamp"`
	ServiceMetrics  map[string]*MetricsSnapshot `json:"service_metrics"`
	Aggregations    []*AnalyticsAggregation `json:"aggregations"`
	Anomalies       []*AnomalyDetection `json:"anomalies"`
	Predictions     []*PredictionResult `json:"predictions"`
	TopErrors       []string `json:"top_errors"`
	TopUsers        []string `json:"top_users"`
	SystemHealth    string `json:"system_health"` // healthy, degraded, critical
}
