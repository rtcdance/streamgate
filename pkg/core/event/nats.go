package event

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// NATSEventBus is a NATS-based implementation of EventBus
type NATSEventBus struct {
	conn          *nats.Conn
	logger        *zap.Logger
	subscriptions map[string]*nats.Subscription
	mu            sync.RWMutex
	url           string
	reconnectWait time.Duration
	maxReconnect  int
}

// NewNATSEventBus creates a new NATS event bus
func NewNATSEventBus(url string, logger *zap.Logger) (*NATSEventBus, error) {
	logger.Info("Connecting to NATS", "url", url)

	// Create NATS connection with reconnect options
	opts := []nats.Option{
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
		nats.ReconnectWait(2 * time.Second),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			logger.Warn("NATS disconnected", "error", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			logger.Info("NATS reconnected", "url", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			logger.Info("NATS connection closed")
		}),
	}

	conn, err := nats.Connect(url, opts...)
	if err != nil {
		logger.Error("Failed to connect to NATS", "error", err)
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	logger.Info("Connected to NATS", "url", conn.ConnectedUrl())

	return &NATSEventBus{
		conn:          conn,
		logger:        logger,
		subscriptions: make(map[string]*nats.Subscription),
		url:           url,
		reconnectWait: 2 * time.Second,
		maxReconnect:  10,
	}, nil
}

// Publish publishes an event via NATS
func (b *NATSEventBus) Publish(ctx context.Context, event *Event) error {
	if b.conn == nil || !b.conn.IsConnected() {
		b.logger.Error("NATS connection not available")
		return fmt.Errorf("NATS connection not available")
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	subject := fmt.Sprintf("streamgate.%s", event.Type)
	if err := b.conn.Publish(subject, data); err != nil {
		b.logger.Error("Failed to publish event", "type", event.Type, "error", err)
		return fmt.Errorf("failed to publish event: %w", err)
	}

	b.logger.Debug("Event published", "type", event.Type, "subject", subject)
	return nil
}

// Subscribe subscribes to events via NATS
func (b *NATSEventBus) Subscribe(ctx context.Context, eventType string, handler EventHandler) error {
	if b.conn == nil || !b.conn.IsConnected() {
		b.logger.Error("NATS connection not available")
		return fmt.Errorf("NATS connection not available")
	}

	subject := fmt.Sprintf("streamgate.%s", eventType)

	sub, err := b.conn.Subscribe(subject, func(msg *nats.Msg) {
		var event Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			b.logger.Error("Failed to unmarshal event", "error", err)
			return
		}

		if err := handler(ctx, &event); err != nil {
			b.logger.Error("Error handling event", "error", err, "type", event.Type)
		}
	})

	if err != nil {
		b.logger.Error("Failed to subscribe", "type", eventType, "error", err)
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.subscriptions[eventType] = sub

	b.logger.Info("Subscribed to events", "type", eventType, "subject", subject)
	return nil
}

// Unsubscribe unsubscribes from events
func (b *NATSEventBus) Unsubscribe(ctx context.Context, eventType string, handler EventHandler) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	sub, exists := b.subscriptions[eventType]
	if !exists {
		return nil
	}

	if err := sub.Unsubscribe(); err != nil {
		return fmt.Errorf("failed to unsubscribe: %w", err)
	}

	delete(b.subscriptions, eventType)

	b.logger.Info("Unsubscribed from events", "type", eventType)
	return nil
}

// Close closes the event bus
func (b *NATSEventBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Unsubscribe from all subscriptions
	for eventType, sub := range b.subscriptions {
		if err := sub.Unsubscribe(); err != nil {
			b.logger.Error("Failed to unsubscribe", "type", eventType, "error", err)
		}
	}

	if b.conn != nil && b.conn.IsConnected() {
		b.conn.Close()
	}

	b.logger.Info("NATS event bus closed")
	return nil
}

// EventTypes defines common event types
const (
	EventFileUploaded       = "file.uploaded"
	EventTranscodingStarted = "transcoding.started"
	EventTranscodingCompleted = "transcoding.completed"
	EventTranscodingFailed  = "transcoding.failed"
	EventStreamingStarted   = "streaming.started"
	EventStreamingStopped   = "streaming.stopped"
	EventMetadataCreated    = "metadata.created"
	EventMetadataUpdated    = "metadata.updated"
	EventMetadataDeleted    = "metadata.deleted"
	EventJobSubmitted       = "job.submitted"
	EventJobCompleted       = "job.completed"
	EventJobFailed          = "job.failed"
	EventAlertTriggered     = "alert.triggered"
	EventAlertResolved      = "alert.resolved"
)

// PublishFileUploaded publishes a file uploaded event
func PublishFileUploaded(ctx context.Context, bus EventBus, fileID string, fileName string, fileSize int64) error {
	event := &Event{
		Type:      EventFileUploaded,
		Source:    "upload-service",
		Timestamp: 0, // TODO: Use current timestamp
		Data: map[string]interface{}{
			"file_id":   fileID,
			"file_name": fileName,
			"file_size": fileSize,
		},
	}

	return bus.Publish(ctx, event)
}

// PublishTranscodingStarted publishes a transcoding started event
func PublishTranscodingStarted(ctx context.Context, bus EventBus, jobID string, inputFile string) error {
	event := &Event{
		Type:      EventTranscodingStarted,
		Source:    "transcoder-service",
		Timestamp: 0, // TODO: Use current timestamp
		Data: map[string]interface{}{
			"job_id":      jobID,
			"input_file": inputFile,
		},
	}

	return bus.Publish(ctx, event)
}

// PublishTranscodingCompleted publishes a transcoding completed event
func PublishTranscodingCompleted(ctx context.Context, bus EventBus, jobID string, outputFile string) error {
	event := &Event{
		Type:      EventTranscodingCompleted,
		Source:    "transcoder-service",
		Timestamp: 0, // TODO: Use current timestamp
		Data: map[string]interface{}{
			"job_id":       jobID,
			"output_file": outputFile,
		},
	}

	return bus.Publish(ctx, event)
}

// PublishJobSubmitted publishes a job submitted event
func PublishJobSubmitted(ctx context.Context, bus EventBus, jobID string, jobType string) error {
	event := &Event{
		Type:      EventJobSubmitted,
		Source:    "worker-service",
		Timestamp: 0, // TODO: Use current timestamp
		Data: map[string]interface{}{
			"job_id":   jobID,
			"job_type": jobType,
		},
	}

	return bus.Publish(ctx, event)
}

// PublishJobCompleted publishes a job completed event
func PublishJobCompleted(ctx context.Context, bus EventBus, jobID string) error {
	event := &Event{
		Type:      EventJobCompleted,
		Source:    "worker-service",
		Timestamp: 0, // TODO: Use current timestamp
		Data: map[string]interface{}{
			"job_id": jobID,
		},
	}

	return bus.Publish(ctx, event)
}

// PublishAlertTriggered publishes an alert triggered event
func PublishAlertTriggered(ctx context.Context, bus EventBus, alertID string, level string, message string) error {
	event := &Event{
		Type:      EventAlertTriggered,
		Source:    "monitor-service",
		Timestamp: 0, // TODO: Use current timestamp
		Data: map[string]interface{}{
			"alert_id": alertID,
			"level":    level,
			"message":  message,
		},
	}

	return bus.Publish(ctx, event)
}
