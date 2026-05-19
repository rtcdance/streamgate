package monitoring

import (
	"net/url"
	"strings"

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
	HTTPRequestErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "streamgate_http_errors_total",
			Help: "Total number of HTTP 5xx errors by route",
		},
		[]string{"method", "route"},
	)
	ServiceRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "streamgate_service_request_duration_ms",
			Help:    "Service request duration in milliseconds",
			Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000},
		},
		[]string{"service", "status"},
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
		[]string{"operation", "from_provider", "to_provider"},
	)
	RPCLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "streamgate_rpc_latency_seconds",
			Help:    "RPC call latency in seconds",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"operation", "rpc_provider"},
	)
)

func init() {
	register := func(c prometheus.Collector) {
		err := prometheus.Register(c)
		if err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); ok {
				return
			}
			panic(err)
		}
	}
	register(HTTPRequestsTotal)
	register(HTTPRequestErrorsTotal)
	register(ServiceRequestDuration)
	register(HealthCheckTotal)
	register(RPCFailoverTotal)
	register(RPCLatencySeconds)
}

// RPCProviderFromURL extracts a stable provider identifier from an RPC URL.
// It strips scheme, path, and port, returning only the hostname to prevent
// cardinality explosion from per-URL labels.
func RPCProviderFromURL(rpcURL string) string {
	u, err := url.Parse(rpcURL)
	if err != nil || u.Host == "" {
		return rpcURL
	}
	host := u.Hostname()
	parts := strings.Split(host, ".")
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return host
}
