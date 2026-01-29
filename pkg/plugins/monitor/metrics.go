package monitor

// Metrics represents system metrics
type Metrics struct {
	CPUUsage    float64
	MemoryUsage float64
	RequestCount int64
	ErrorCount  int64
}

// CollectMetrics collects system metrics
func CollectMetrics() *Metrics {
	return &Metrics{
		CPUUsage:     0,
		MemoryUsage:  0,
		RequestCount: 0,
		ErrorCount:   0,
	}
}
