package models

import "time"

// Task represents a background task
type Task struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Status      string                 `json:"status"`
	Priority    int                    `json:"priority"`
	Payload     map[string]interface{} `json:"payload"`
	Result      map[string]interface{} `json:"result"`
	Error       string                 `json:"error"`
	RetryCount  int                    `json:"retry_count"`
	MaxRetries  int                    `json:"max_retries"`
	CreatedAt   time.Time              `json:"created_at"`
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt time.Time              `json:"completed_at"`
}

// TaskType defines task types
type TaskType string

const (
	TaskTranscode TaskType = "transcode"
	TaskUpload    TaskType = "upload"
	TaskProcess   TaskType = "process"
	TaskCleanup   TaskType = "cleanup"
)

// TaskStatus defines task status
type TaskStatus string

const (
	StatusPending   TaskStatus = "pending"
	StatusRunning   TaskStatus = "running"
	StatusCompleted TaskStatus = "completed"
	StatusFailed    TaskStatus = "failed"
	StatusRetrying  TaskStatus = "retrying"
)

// TaskPriority defines task priority
type TaskPriority int

const (
	PriorityLow    TaskPriority = 1
	PriorityNormal TaskPriority = 5
	PriorityHigh   TaskPriority = 10
)
