package models

import (
	"context"
	"time"
)

// TranscodingTask represents a transcoding task.
// Defined in the models package to avoid storage→service layer inversion.
type TranscodingTask struct {
	ID          string                 `json:"id"`
	ContentID   string                 `json:"content_id"`
	Profile     string                 `json:"profile"`
	Status      string                 `json:"status"`   // pending, processing, completed, failed, cancelled
	Progress    int                    `json:"progress"` // 0-100
	InputURL    string                 `json:"input_url"`
	OutputURL   string                 `json:"output_url"`
	Error       string                 `json:"error,omitempty"`
	Priority    int                    `json:"priority"`
	OwnerWallet string                 `json:"owner_wallet,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Metadata    map[string]interface{} `json:"metadata"`
}

type TranscodingTaskStatus string

const (
	TaskStatusPending    TranscodingTaskStatus = "pending"
	TaskStatusProcessing TranscodingTaskStatus = "processing"
	TaskStatusCompleted  TranscodingTaskStatus = "completed"
	TaskStatusFailed     TranscodingTaskStatus = "failed"
	TaskStatusCancelled  TranscodingTaskStatus = "cancelled"
)

var validTaskTransitions = map[TranscodingTaskStatus][]TranscodingTaskStatus{
	TaskStatusPending:    {TaskStatusProcessing, TaskStatusCancelled, TaskStatusFailed},
	TaskStatusProcessing: {TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled},
	TaskStatusCompleted:  {},
	TaskStatusFailed:     {TaskStatusPending},
	TaskStatusCancelled:  {},
}

func IsValidTaskTransition(from, to TranscodingTaskStatus) bool {
	if from == to {
		return true
	}
	allowed, ok := validTaskTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// TranscodingQueue defines the interface for a transcoding task queue.
// Implemented by NATSTranscodingQueue (storage) and MemoryTranscodingQueue (service).
type TranscodingQueue interface {
	Enqueue(task *TranscodingTask) error
	Dequeue(ctx context.Context) (*TranscodingTask, error)
	GetStatus(taskID string) (string, error)
	Ack(taskID string) error
	Nak(taskID string) error
	Depth() (int, error)
}
