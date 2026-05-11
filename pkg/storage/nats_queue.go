package storage

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
	"streamgate/pkg/models"
)

const (
	jsStreamName    = "TRANSCODING"
	jsStreamSubject = "streamgate.transcoding.tasks"
	jsConsumerName  = "transcoding-worker"
)

// NATSTranscodingQueue implements models.TranscodingQueue backed by NATS JetStream.
// Tasks are published to a durable stream so they survive server restarts.
// Task status is tracked locally since JetStream does not store application-level state.
type NATSTranscodingQueue struct {
	conn     *nats.Conn
	js       nats.JetStreamContext
	sub      *nats.Subscription
	logger   *zap.Logger
	statusMu sync.RWMutex
	statuses map[string]string // taskID → status
}

// NewNATSTranscodingQueue creates a NATS JetStream-backed transcoding queue.
// It ensures the stream and pull consumer exist, then returns a ready queue.
func NewNATSTranscodingQueue(url string, logger *zap.Logger) (*NATSTranscodingQueue, error) {
	nc, err := nats.Connect(url,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
		nats.ReconnectWait(2*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			logger.Warn("NATS disconnected", zap.Error(err))
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			logger.Info("NATS reconnected", zap.String("url", nc.ConnectedUrl()))
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect failed: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream init failed: %w", err)
	}

	q := &NATSTranscodingQueue{
		conn:     nc,
		js:       js,
		logger:   logger,
		statuses: make(map[string]string),
	}

	if err := q.ensureStream(); err != nil {
		nc.Close()
		return nil, err
	}

	// Create pull subscription (also creates the durable consumer)
	q.sub, err = js.PullSubscribe(jsStreamSubject, jsConsumerName)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to create pull subscription: %w", err)
	}

	logger.Info("NATS transcoding queue initialized", zap.String("url", url))
	return q, nil
}

func (q *NATSTranscodingQueue) ensureStream() error {
	_, err := q.js.StreamInfo(jsStreamName)
	if err == nil {
		return nil // stream already exists
	}

	_, err = q.js.AddStream(&nats.StreamConfig{
		Name:     jsStreamName,
		Subjects: []string{jsStreamSubject},
		Storage:  nats.FileStorage,
		MaxMsgs:  10000,
		MaxAge:   24 * time.Hour,
	})
	if err != nil {
		return fmt.Errorf("failed to create JetStream stream: %w", err)
	}
	q.logger.Info("JetStream stream created", zap.String("stream", jsStreamName))
	return nil
}

// Enqueue publishes a task to the NATS JetStream stream.
func (q *NATSTranscodingQueue) Enqueue(task *models.TranscodingTask) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	_, err = q.js.Publish(jsStreamSubject, data)
	if err != nil {
		return fmt.Errorf("failed to publish task to JetStream: %w", err)
	}

	q.statusMu.Lock()
	q.statuses[task.ID] = task.Status
	q.statusMu.Unlock()

	q.logger.Debug("Task enqueued", zap.String("task_id", task.ID))
	return nil
}

// Dequeue pulls the next task from the JetStream consumer.
// Returns an error resembling "queue empty" when no messages are available.
func (q *NATSTranscodingQueue) Dequeue() (*models.TranscodingTask, error) {
	msgs, err := q.sub.Fetch(1, nats.MaxWait(2*time.Second))
	if err != nil {
		return nil, fmt.Errorf("queue empty: %w", err)
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("queue empty")
	}

	msg := msgs[0]
	_ = msg.Ack()

	var task models.TranscodingTask
	if err := json.Unmarshal(msg.Data, &task); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	q.logger.Debug("Task dequeued", zap.String("task_id", task.ID))
	return &task, nil
}

// GetStatus returns the locally-tracked status of a task.
func (q *NATSTranscodingQueue) GetStatus(taskID string) (string, error) {
	q.statusMu.RLock()
	defer q.statusMu.RUnlock()

	status, ok := q.statuses[taskID]
	if !ok {
		return "", fmt.Errorf("task not found: %s", taskID)
	}
	return status, nil
}

// Close closes the NATS connection.
func (q *NATSTranscodingQueue) Close() error {
	if q.sub != nil {
		_ = q.sub.Unsubscribe()
	}
	if q.conn != nil {
		q.conn.Close()
	}
	return nil
}
