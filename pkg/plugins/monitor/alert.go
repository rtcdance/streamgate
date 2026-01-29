package monitor

// Alert represents an alert
type Alert struct {
	ID      string
	Level   string
	Message string
}

// GenerateAlert generates an alert
func GenerateAlert(level, message string) *Alert {
	return &Alert{
		Level:   level,
		Message: message,
	}
}
