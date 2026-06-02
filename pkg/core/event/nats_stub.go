//go:build !nats_integration

package event

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

type NATSEventBus struct{}

func NewNATSEventBus(url string, logger *zap.Logger) (*NATSEventBus, error) {
	return nil, fmt.Errorf("NATS not available: build with -tags nats_integration")
}

func (b *NATSEventBus) Publish(ctx context.Context, event *Event) error {
	return fmt.Errorf("NATS not available")
}

func (b *NATSEventBus) Subscribe(ctx context.Context, eventType string, handler EventHandler) (string, error) {
	return "", fmt.Errorf("NATS not available")
}

func (b *NATSEventBus) Unsubscribe(ctx context.Context, subscriptionID string) error {
	return fmt.Errorf("NATS not available")
}

func (b *NATSEventBus) Close() error {
	return nil
}

const (
	EventFileUploaded         = "file.uploaded"
	EventTranscodingStarted   = "transcoding.started"
	EventTranscodingCompleted = "transcoding.completed"
	EventTranscodingFailed    = "transcoding.failed"
	EventStreamingStarted     = "streaming.started"
	EventStreamingStopped     = "streaming.stopped"
	EventMetadataCreated      = "metadata.created"
	EventMetadataUpdated      = "metadata.updated"
	EventMetadataDeleted      = "metadata.deleted"
	EventJobSubmitted         = "job.submitted"
	EventJobCompleted         = "job.completed"
	EventJobFailed            = "job.failed"
	EventAlertTriggered       = "alert.triggered"
	EventAlertResolved        = "alert.resolved"
)

func PublishFileUploaded(ctx context.Context, bus EventBus, fileID, fileName string, fileSize int64) error {
	event := &Event{
		Type:      EventFileUploaded,
		Source:    "upload-service",
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"file_id":   fileID,
			"file_name": fileName,
			"file_size": fileSize,
		},
	}

	return bus.Publish(ctx, event)
}

func PublishTranscodingStarted(ctx context.Context, bus EventBus, jobID, inputFile string) error {
	event := &Event{
		Type:      EventTranscodingStarted,
		Source:    "transcoder-service",
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"job_id":     jobID,
			"input_file": inputFile,
		},
	}

	return bus.Publish(ctx, event)
}

func PublishTranscodingCompleted(ctx context.Context, bus EventBus, jobID, outputFile string) error {
	event := &Event{
		Type:      EventTranscodingCompleted,
		Source:    "transcoder-service",
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"job_id":      jobID,
			"output_file": outputFile,
		},
	}

	return bus.Publish(ctx, event)
}

func PublishJobSubmitted(ctx context.Context, bus EventBus, jobID, jobType string) error {
	event := &Event{
		Type:      EventJobSubmitted,
		Source:    "worker-service",
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"job_id":   jobID,
			"job_type": jobType,
		},
	}

	return bus.Publish(ctx, event)
}

func PublishJobCompleted(ctx context.Context, bus EventBus, jobID string) error {
	event := &Event{
		Type:      EventJobCompleted,
		Source:    "worker-service",
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"job_id": jobID,
		},
	}

	return bus.Publish(ctx, event)
}

func PublishAlertTriggered(ctx context.Context, bus EventBus, alertID, level, message string) error {
	event := &Event{
		Type:      EventAlertTriggered,
		Source:    "monitor-service",
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"alert_id": alertID,
			"level":    level,
			"message":  message,
		},
	}

	return bus.Publish(ctx, event)
}
