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

// validTaskTransitions gates the public UpdateTaskStatus API. The worker's
// retry release (processing→pending) is a separate lease-guarded SQL path
// that preserves attempt/last_error/next_attempt_at, so it is intentionally
// NOT part of this API-facing map.
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

// Outbox destination kinds. The outbox row with kind=task carries the task
// message; kind=dlq carries the payload of a terminally failed task.
const (
	OutboxKindTask = "task"
	OutboxKindDLQ  = "dlq"
)

// DeliveryHandle identifies ONE delivery of ONE queue message. Ack/Nak/
// InProgress take the handle returned by Dequeue so they settle exactly that
// delivery — never whatever message happens to be filed under the same task
// ID (a redelivery of the same task must not be consumed by an Ack meant for
// an earlier delivery).
type DeliveryHandle interface {
	TaskID() string
}

// TranscodingQueue defines the interface for a transcoding task queue.
// Implemented by NATSTranscodingQueue (storage) and MemoryTranscodingQueue (service).
// Status is deliberately NOT part of this interface: task status is answered
// by Postgres only, never by a process-local map.
type TranscodingQueue interface {
	Enqueue(task *TranscodingTask) error
	Dequeue(ctx context.Context) (*TranscodingTask, DeliveryHandle, error)
	Ack(h DeliveryHandle) error
	// Nak releases the delivery for redelivery after delay. The retry backoff
	// rides on the same message (NakWithDelay for JetStream, bounded re-queue
	// for the in-memory queue) instead of publishing a new message.
	Nak(h DeliveryHandle, delay time.Duration) error
	// InProgress extends the broker-side delivery timeout while work continues.
	InProgress(h DeliveryHandle) error
	// PublishOutbox publishes a stored outbox payload under a stable message ID
	// to the destination selected by kind (OutboxKindTask / OutboxKindDLQ). It
	// returns only after the broker acknowledged the publish (JetStream
	// PubAck), so the caller may mark the outbox row delivered afterwards.
	PublishOutbox(kind, msgID string, payload []byte) error
	Depth() (int, error)
}
