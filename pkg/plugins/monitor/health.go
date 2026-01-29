package monitor

// HealthStatus represents health status
type HealthStatus struct {
	Status string
}

// GetHealth returns health status
func GetHealth() *HealthStatus {
	return &HealthStatus{
		Status: "healthy",
	}
}
