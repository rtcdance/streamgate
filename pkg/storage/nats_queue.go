package storage

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"streamgate/pkg/models"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	jsStreamName    = "TRANSCODING"
	jsStreamSubject = "streamgate.transcoding.tasks"
	jsConsumerName  = "transcoding-worker"

	msgStaleTimeout   = 30 * time.Minute
	statusStaleTimeout = 2 * time.Hour
	cleanupInterval    = 5 * time.Minute
)

type NATSTranscodingQueue struct {
	conn      *nats.Conn
	js        nats.JetStreamContext
	sub       *nats.Subscription
	logger    *zap.Logger
	statusMu  sync.RWMutex
	statuses  map[string]statusEntry
	msgMu     sync.RWMutex
	messages  map[string]msgEntry
	cleanupMu sync.Mutex
	lastClean time.Time
}

type statusEntry struct {
	status    string
	updatedAt time.Time
}

type msgEntry struct {
	msg      *nats.Msg
	dequeued time.Time
}

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
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
	}

	if err := q.ensureStream(); err != nil {
		nc.Close()
		return nil, err
	}

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
		return nil
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
	q.statuses[task.ID] = statusEntry{status: task.Status, updatedAt: time.Now()}
	q.statusMu.Unlock()

	q.maybeCleanup()

	q.logger.Debug("Task enqueued", zap.String("task_id", task.ID))
	return nil
}

func (q *NATSTranscodingQueue) Dequeue() (*models.TranscodingTask, error) {
	msgs, err := q.sub.Fetch(1, nats.MaxWait(2*time.Second))
	if err != nil {
		return nil, fmt.Errorf("queue empty: %w", err)
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("queue empty")
	}

	msg := msgs[0]

	var task models.TranscodingTask
	if err := json.Unmarshal(msg.Data, &task); err != nil {
		_ = msg.Nak()
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	q.msgMu.Lock()
	q.messages[task.ID] = msgEntry{msg: msg, dequeued: time.Now()}
	q.msgMu.Unlock()

	q.logger.Debug("Task dequeued", zap.String("task_id", task.ID))
	return &task, nil
}

func (q *NATSTranscodingQueue) Ack(taskID string) error {
	q.msgMu.Lock()
	entry, ok := q.messages[taskID]
	delete(q.messages, taskID)
	q.msgMu.Unlock()

	if !ok {
		return fmt.Errorf("message not found for task %s", taskID)
	}

	if err := entry.msg.Ack(); err != nil {
		return fmt.Errorf("failed to ack task %s: %w", taskID, err)
	}

	q.statusMu.Lock()
	q.statuses[taskID] = statusEntry{status: "completed", updatedAt: time.Now()}
	q.statusMu.Unlock()

	q.logger.Debug("Task acked", zap.String("task_id", taskID))
	return nil
}

func (q *NATSTranscodingQueue) Nak(taskID string) error {
	q.msgMu.Lock()
	entry, ok := q.messages[taskID]
	delete(q.messages, taskID)
	q.msgMu.Unlock()

	if !ok {
		return fmt.Errorf("message not found for task %s", taskID)
	}

	if err := entry.msg.Nak(); err != nil {
		return fmt.Errorf("failed to nak task %s: %w", taskID, err)
	}

	q.logger.Debug("Task nacked for retry", zap.String("task_id", taskID))
	return nil
}

func (q *NATSTranscodingQueue) GetStatus(taskID string) (string, error) {
	q.statusMu.RLock()
	defer q.statusMu.RUnlock()

	entry, ok := q.statuses[taskID]
	if !ok {
		return "", fmt.Errorf("task not found: %s", taskID)
	}
	return entry.status, nil
}

func (q *NATSTranscodingQueue) Close() error {
	if q.sub != nil {
		_ = q.sub.Unsubscribe()
	}
	if q.conn != nil {
		q.conn.Close()
	}
	return nil
}

func (q *NATSTranscodingQueue) maybeCleanup() {
	q.cleanupMu.Lock()
	if time.Since(q.lastClean) < cleanupInterval {
		q.cleanupMu.Unlock()
		return
	}
	q.lastClean = time.Now()
	q.cleanupMu.Unlock()

	now := time.Now()

	q.msgMu.Lock()
	for id, entry := range q.messages {
		if now.Sub(entry.dequeued) > msgStaleTimeout {
			delete(q.messages, id)
		}
	}
	q.msgMu.Unlock()

	q.statusMu.Lock()
	for id, entry := range q.statuses {
		if now.Sub(entry.updatedAt) > statusStaleTimeout {
			delete(q.statuses, id)
		}
	}
	q.statusMu.Unlock()
}
