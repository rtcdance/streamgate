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

var validTaskTransitions = map[string][]string{
	"pending":    {"processing", "cancelled", "failed"},
	"processing": {"completed", "failed", "cancelled"},
	"completed":  {},
	"failed":     {"pending"},
	"cancelled":  {},
}

func IsValidTaskTransition(from, to string) bool {
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
