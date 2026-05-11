package monitoring

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Standard Prometheus metrics registered on the default registry.
// The /metrics endpoint in gateway.go uses promhttp.Handler() which serves
// these metrics plus the bridge metrics defined in metrics.go.
var (
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "streamgate_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "route", "status"},
	)
	ServiceRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "streamgate_service_request_duration_ms",
			Help:    "Service request duration in milliseconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"service"},
	)
	HealthCheckTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "streamgate_health_check_total",
			Help: "Total health check invocations",
		},
		[]string{"status"},
	)
	RPCFailoverTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "streamgate_rpc_failover_total",
			Help: "Total number of RPC failover attempts",
		},
		[]string{"operation", "from_rpc", "to_rpc"},
	)
	RPCLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "streamgate_rpc_latency_seconds",
			Help:    "RPC call latency in seconds",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"operation", "rpc_url"},
	)
)

func init() {
	prometheus.MustRegister(HTTPRequestsTotal, ServiceRequestDuration, HealthCheckTotal, RPCFailoverTotal, RPCLatencySeconds)
}
