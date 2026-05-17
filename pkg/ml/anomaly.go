package ml

import "time"

type Anomaly struct {
	ID             string
	Type           string
	Severity       string
	Score          float64
	Timestamp      time.Time
	Description    string
	RootCause      string
	Recommendation string
}
