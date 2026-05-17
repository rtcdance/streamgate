package analytics

import "time"

type AnalyticsEvent struct {
	ID        string
	Timestamp time.Time
	EventType string
	ServiceID string
	UserID    string
	Metadata  map[string]interface{}
	Tags      map[string]string
}

type MetricsSnapshot struct {
	ID           string
	Timestamp    time.Time
	ServiceID    string
	CPUUsage     float64
	MemoryUsage  float64
	DiskUsage    float64
	RequestRate  float64
	ErrorRate    float64
	Latency      float64
	CacheHitRate float64
}

type UserBehavior struct {
	ID           string
	Timestamp    time.Time
	UserID       string
	Action       string
	ContentID    string
	Duration     int64
	Success      bool
	ErrorMessage string
	ClientIP     string
	UserAgent    string
	SessionID    string
}

type PerformanceMetric struct {
	ID           string
	Timestamp    time.Time
	ServiceID    string
	Operation    string
	Duration     float64
	Success      bool
	ErrorType    string
	ResourceUsed float64
	Throughput   float64
}

type BusinessMetric struct {
	ID         string
	Timestamp  time.Time
	MetricType string
	Value      float64
	Unit       string
	Dimension  map[string]string
}

type AnalyticsAggregation struct {
	ID          string
	Timestamp   time.Time
	ServiceID   string
	Period      string
	EventCount  int64
	AvgLatency  float64
	P50Latency  float64
	P95Latency  float64
	P99Latency  float64
	ErrorCount  int64
	ErrorRate   float64
	SuccessRate float64
	Throughput  float64
}
