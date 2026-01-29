package worker

// Job represents a background job
type Job struct {
	ID     string
	Type   string
	Status string
	Data   map[string]interface{}
}

// NewJob creates a new job
func NewJob(jobType string, data map[string]interface{}) *Job {
	return &Job{
		Type:   jobType,
		Status: "pending",
		Data:   data,
	}
}
