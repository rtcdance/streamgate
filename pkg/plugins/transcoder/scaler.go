package transcoder

// AutoScaler handles auto-scaling
type AutoScaler struct{}

// Scale scales workers
func (s *AutoScaler) Scale(targetCount int) error {
	return nil
}

// GetMetrics gets scaling metrics
func (s *AutoScaler) GetMetrics() map[string]interface{} {
	return map[string]interface{}{}
}
