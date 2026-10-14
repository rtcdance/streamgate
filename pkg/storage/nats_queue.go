package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/rtcdance/streamgate/pkg/monitoring"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	jsStreamName       = "TRANSCODING"
	jsStreamSubject    = "streamgate.transcoding.tasks"
	jsConsumerName     = "transcoding-worker"
	jsDLQStreamName    = "TRANSCODING_DLQ"
	jsDLQStreamSubject = "streamgate.transcoding.dlq"
	// jsMaxDeliver is -1 (unlimited): the transport must not silently exhaust
	// messages. The business cap (3 attempts) is enforced from the DB attempt
	// counter, and terminal failure is settled through the explicit DLQ.
	jsMaxDeliver = -1
	// jsAckWait is the broker-side redelivery timer. It must be longer than the
	// DB lease TTL (30s) so an in-flight worker heartbeats before redelivery.
	jsAckWait = 30 * time.Second
	// poisonRetryDelay is how long a poison message waits before the next
	// attempt to publish it to the DLQ (publish failure leaves it redeliverable).
	poisonRetryDelay = 5 * time.Second
	// queueName labels the transcoding queue in the SG-07 metrics families. It
	// is a fixed string (low cardinality), never a message/task identifier.
	queueName = "transcoding"
)

// jsMessage is the slice of *nats.Msg the queue settles. It exists so tests can
// verify that Ack/Nak/InProgress bind to the exact delivery without a live
// JetStream (a bare *nats.Msg.Ack panics without a subscription).
type jsMessage interface {
	Ack(opts ...nats.AckOpt) error
	NakWithDelay(delay time.Duration, opts ...nats.AckOpt) error
	InProgress(opts ...nats.AckOpt) error
	Term(opts ...nats.AckOpt) error
}

// natsDelivery is the per-delivery handle for the JetStream queue. Each
// Dequeue call produces its own handle bound to its own message; there is no
// taskID-keyed lookup, so an Ack for one delivery can never consume another.
type natsDelivery struct {
	taskID string
	msg    jsMessage
}

func (d *natsDelivery) TaskID() string { return d.taskID }

// observeQueueAge records publish→delivery latency from the broker's message
// metadata timestamp (msg.Metadata().Timestamp, stamped by JetStream). Without
// a usable metadata timestamp there is no publish instant to subtract, so no
// age is recorded — the value is never guessed.
func observeQueueAge(publishedAt time.Time) {
	if publishedAt.IsZero() {
		return
	}
	age := time.Since(publishedAt)
	if age < 0 {
		return
	}
	monitoring.QueueMessageAgeSeconds.WithLabelValues(queueName).Observe(age.Seconds())
}

func asNATSDelivery(h models.DeliveryHandle) (*natsDelivery, error) {
	d, ok := h.(*natsDelivery)
	if !ok || d == nil || d.msg == nil {
		return nil, fmt.Errorf("delivery handle does not belong to the NATS queue")
	}
	return d, nil
}

type NATSTranscodingQueue struct {
	conn   *nats.Conn
	js     nats.JetStreamContext
	sub    *nats.Subscription
	logger *zap.Logger
}

func NewNATSTranscodingQueue(url string, logger *zap.Logger) (*NATSTranscodingQueue, error) {
	nc, err := nats.Connect(url,
		nats.Timeout(5*time.Second),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(3),
		nats.ReconnectWait(1*time.Second),
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
		conn:   nc,
		js:     js,
		logger: logger,
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
	if err != nil {
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
	}

	q.ensureDLQStream()
	q.ensureMaxDeliverConsumer()

	return nil
}

func (q *NATSTranscodingQueue) ensureDLQStream() {
	_, err := q.js.StreamInfo(jsDLQStreamName)
	if err == nil {
		return
	}
	_, err = q.js.AddStream(&nats.StreamConfig{
		Name:     jsDLQStreamName,
		Subjects: []string{jsDLQStreamSubject},
		Storage:  nats.FileStorage,
		MaxMsgs:  5000,
		MaxAge:   7 * 24 * time.Hour,
	})
	if err != nil {
		q.logger.Warn("Failed to create DLQ stream", zap.Error(err))
	} else {
		q.logger.Info("DLQ stream created", zap.String("stream", jsDLQStreamName))
	}
}

func (q *NATSTranscodingQueue) ensureMaxDeliverConsumer() {
	want := &nats.ConsumerConfig{
		Durable:    jsConsumerName,
		MaxDeliver: jsMaxDeliver,
		AckWait:    jsAckWait,
	}
	info, err := q.js.ConsumerInfo(jsStreamName, jsConsumerName)
	if err == nil {
		// A consumer left behind by an older build (MaxDeliver=5) would keep
		// silently exhausting messages; converge it to the current policy.
		if info.Config.MaxDeliver != jsMaxDeliver || info.Config.AckWait != jsAckWait {
			if _, uerr := q.js.UpdateConsumer(jsStreamName, want); uerr != nil {
				q.logger.Warn("Failed to update consumer MaxDeliver/AckWait", zap.Error(uerr))
			}
		}
		return
	}
	if _, err := q.js.AddConsumer(jsStreamName, want); err != nil {
		q.logger.Warn("Failed to configure consumer MaxDeliver", zap.Error(err))
	}
}

// PublishOutbox publishes an outbox payload with a stable message ID. The
// message ID is set both as the JetStream dedupe key (nats.MsgId) and as an
// explicit header before the call so the dedupe key is visible end to end.
// Redelivery/duplicate publish is therefore idempotent at the broker; the DB
// state machine remains the real guarantee.
func (q *NATSTranscodingQueue) PublishOutbox(kind, msgID string, payload []byte) error {
	subject := jsStreamSubject
	switch kind {
	case models.OutboxKindTask:
		subject = jsStreamSubject
	case models.OutboxKindDLQ:
		subject = jsDLQStreamSubject
	default:
		return fmt.Errorf("unknown outbox kind: %s", kind)
	}

	msg := &nats.Msg{
		Subject: subject,
		Data:    payload,
		Header:  make(nats.Header),
	}
	if msgID != "" {
		msg.Header.Set(nats.MsgIdHdr, msgID)
	}
	if _, err := q.js.PublishMsg(msg, nats.MsgId(msgID)); err != nil {
		return fmt.Errorf("failed to publish %s message to JetStream: %w", kind, err)
	}
	return nil
}

func (q *NATSTranscodingQueue) Enqueue(task *models.TranscodingTask) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}
	return q.PublishOutbox(models.OutboxKindTask, task.ID, data)
}

func (q *NATSTranscodingQueue) Dequeue(ctx context.Context) (*models.TranscodingTask, models.DeliveryHandle, error) {
	msgs, err := q.sub.Fetch(1, nats.Context(ctx))
	if err != nil {
		if errors.Is(err, nats.ErrTimeout) {
			return nil, nil, fmt.Errorf("queue empty: %w", err)
		}
		return nil, nil, fmt.Errorf("dequeue failed: %w", err)
	}
	if len(msgs) == 0 {
		return nil, nil, fmt.Errorf("queue empty")
	}
	msg := msgs[0]
	stream, seq := "unknown", uint64(0)
	if meta, merr := msg.Metadata(); merr == nil && meta != nil {
		stream, seq = meta.Stream, meta.Sequence.Stream
		observeQueueAge(meta.Timestamp)
	}
	return q.deliverPayload(msg.Data, msg, stream, seq)
}

// deliverPayload turns one fetched message payload into a task plus its
// delivery handle. A payload that does not parse (or has no task id) is
// poison: it is published to the DLQ keyed by stream:sequence and Terminated
// so it cannot loop forever with MaxDeliver=-1.
func (q *NATSTranscodingQueue) deliverPayload(data []byte, msg jsMessage, stream string, seq uint64) (*models.TranscodingTask, models.DeliveryHandle, error) {
	var task models.TranscodingTask
	if err := json.Unmarshal(data, &task); err != nil {
		q.handlePoison(data, msg, stream, seq)
		return nil, nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}
	if task.ID == "" {
		q.handlePoison(data, msg, stream, seq)
		return nil, nil, fmt.Errorf("task message has no id")
	}
	return &task, &natsDelivery{taskID: task.ID, msg: msg}, nil
}

func (q *NATSTranscodingQueue) handlePoison(data []byte, msg jsMessage, stream string, seq uint64) {
	msgID := fmt.Sprintf("%s:%d", stream, seq)
	if err := q.PublishOutbox(models.OutboxKindDLQ, msgID, data); err != nil {
		// Leave the message unsettled and delay it: the DLQ publish is retried
		// on redelivery instead of dropping the poison payload.
		q.logger.Error("Failed to publish poison message to DLQ", zap.String("msg_id", msgID), zap.Error(err))
		_ = msg.NakWithDelay(poisonRetryDelay)
		return
	}
	q.logger.Warn("Poison message moved to DLQ", zap.String("msg_id", msgID))
	if err := msg.Term(); err != nil {
		q.logger.Warn("Failed to term poison message", zap.String("msg_id", msgID), zap.Error(err))
	}
	monitoring.QueueAttemptsTotal.WithLabelValues(queueName, "dlq").Inc()
}

func (q *NATSTranscodingQueue) Ack(h models.DeliveryHandle) error {
	d, err := asNATSDelivery(h)
	if err != nil {
		return err
	}
	if err := d.msg.Ack(); err != nil {
		return fmt.Errorf("failed to ack task %s: %w", d.taskID, err)
	}
	monitoring.QueueAttemptsTotal.WithLabelValues(queueName, "delivered").Inc()
	return nil
}

func (q *NATSTranscodingQueue) Nak(h models.DeliveryHandle, delay time.Duration) error {
	d, err := asNATSDelivery(h)
	if err != nil {
		return err
	}
	if err := d.msg.NakWithDelay(delay); err != nil {
		return fmt.Errorf("failed to nak task %s: %w", d.taskID, err)
	}
	// A Nak hands the message back for another attempt: that delivery will be
	// redelivered, which is the outcome this counts.
	monitoring.QueueAttemptsTotal.WithLabelValues(queueName, "redelivered").Inc()
	return nil
}

func (q *NATSTranscodingQueue) InProgress(h models.DeliveryHandle) error {
	d, err := asNATSDelivery(h)
	if err != nil {
		return err
	}
	if err := d.msg.InProgress(); err != nil {
		return fmt.Errorf("failed to mark task %s in progress: %w", d.taskID, err)
	}
	return nil
}

func (q *NATSTranscodingQueue) Depth() (int, error) {
	info, err := q.sub.ConsumerInfo()
	if err != nil {
		return 0, fmt.Errorf("failed to get consumer info: %w", err)
	}
	return int(info.NumPending), nil
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
