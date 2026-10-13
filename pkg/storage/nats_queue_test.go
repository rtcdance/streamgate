package storage

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockJetStream struct {
	publishFn           func(subj string, data []byte, opts ...nats.PubOpt) (*nats.PubAck, error)
	publishMsgFn        func(m *nats.Msg, opts ...nats.PubOpt) (*nats.PubAck, error)
	publishAsyncFn      func(subj string, data []byte, opts ...nats.PubOpt) (nats.PubAckFuture, error)
	publishMsgAsyncFn   func(m *nats.Msg, opts ...nats.PubOpt) (nats.PubAckFuture, error)
	addStreamFn         func(cfg *nats.StreamConfig, opts ...nats.JSOpt) (*nats.StreamInfo, error)
	updateStreamFn      func(cfg *nats.StreamConfig, opts ...nats.JSOpt) (*nats.StreamInfo, error)
	deleteStreamFn      func(name string, opts ...nats.JSOpt) error
	streamInfoFn        func(stream string, opts ...nats.JSOpt) (*nats.StreamInfo, error)
	purgeStreamFn       func(name string, opts ...nats.JSOpt) error
	addConsumerFn       func(stream string, cfg *nats.ConsumerConfig, opts ...nats.JSOpt) (*nats.ConsumerInfo, error)
	updateConsumerFn    func(stream string, cfg *nats.ConsumerConfig, opts ...nats.JSOpt) (*nats.ConsumerInfo, error)
	deleteConsumerFn    func(stream, consumer string, opts ...nats.JSOpt) error
	consumerInfoFn      func(stream, name string, opts ...nats.JSOpt) (*nats.ConsumerInfo, error)
	accountInfoFn       func(opts ...nats.JSOpt) (*nats.AccountInfo, error)
	pullSubscribeFn     func(subj, durable string, opts ...nats.SubOpt) (*nats.Subscription, error)
	subscribeFn         func(subj string, cb nats.MsgHandler, opts ...nats.SubOpt) (*nats.Subscription, error)
	subscribeSyncFn     func(subj string, opts ...nats.SubOpt) (*nats.Subscription, error)
	keyValueFn          func(bucket string) (nats.KeyValue, error)
	createKeyValueFn    func(cfg *nats.KeyValueConfig) (nats.KeyValue, error)
	deleteKeyValueFn    func(bucket string) error
	objectStoreFn       func(bucket string) (nats.ObjectStore, error)
	createObjectStoreFn func(cfg *nats.ObjectStoreConfig) (nats.ObjectStore, error)
	deleteObjectStoreFn func(bucket string) error
}

func (m *mockJetStream) Publish(subj string, data []byte, opts ...nats.PubOpt) (*nats.PubAck, error) {
	if m.publishFn != nil {
		return m.publishFn(subj, data, opts...)
	}
	return &nats.PubAck{}, nil
}
func (m *mockJetStream) PublishMsg(msg *nats.Msg, opts ...nats.PubOpt) (*nats.PubAck, error) {
	if m.publishMsgFn != nil {
		return m.publishMsgFn(msg, opts...)
	}
	return &nats.PubAck{}, nil
}
func (m *mockJetStream) PublishAsync(subj string, data []byte, opts ...nats.PubOpt) (nats.PubAckFuture, error) {
	if m.publishAsyncFn != nil {
		return m.publishAsyncFn(subj, data, opts...)
	}
	return nil, nil
}
func (m *mockJetStream) PublishMsgAsync(msg *nats.Msg, opts ...nats.PubOpt) (nats.PubAckFuture, error) {
	if m.publishMsgAsyncFn != nil {
		return m.publishMsgAsyncFn(msg, opts...)
	}
	return nil, nil
}
func (m *mockJetStream) PublishAsyncPending() int              { return 0 }
func (m *mockJetStream) PublishAsyncComplete() <-chan struct{} { return nil }
func (m *mockJetStream) CleanupPublisher()                     {}

func (m *mockJetStream) Subscribe(subj string, cb nats.MsgHandler, opts ...nats.SubOpt) (*nats.Subscription, error) {
	if m.subscribeFn != nil {
		return m.subscribeFn(subj, cb, opts...)
	}
	return nil, nil
}
func (m *mockJetStream) SubscribeSync(subj string, opts ...nats.SubOpt) (*nats.Subscription, error) {
	if m.subscribeSyncFn != nil {
		return m.subscribeSyncFn(subj, opts...)
	}
	return nil, nil
}
func (m *mockJetStream) ChanSubscribe(subj string, ch chan *nats.Msg, opts ...nats.SubOpt) (*nats.Subscription, error) {
	return nil, nil
}
func (m *mockJetStream) ChanQueueSubscribe(subj, queue string, ch chan *nats.Msg, opts ...nats.SubOpt) (*nats.Subscription, error) {
	return nil, nil
}
func (m *mockJetStream) QueueSubscribe(subj, queue string, cb nats.MsgHandler, opts ...nats.SubOpt) (*nats.Subscription, error) {
	return nil, nil
}
func (m *mockJetStream) QueueSubscribeSync(subj, queue string, opts ...nats.SubOpt) (*nats.Subscription, error) {
	return nil, nil
}
func (m *mockJetStream) PullSubscribe(subj, durable string, opts ...nats.SubOpt) (*nats.Subscription, error) {
	if m.pullSubscribeFn != nil {
		return m.pullSubscribeFn(subj, durable, opts...)
	}
	return nil, nil
}

func (m *mockJetStream) AddStream(cfg *nats.StreamConfig, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
	if m.addStreamFn != nil {
		return m.addStreamFn(cfg, opts...)
	}
	return &nats.StreamInfo{}, nil
}
func (m *mockJetStream) UpdateStream(cfg *nats.StreamConfig, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
	if m.updateStreamFn != nil {
		return m.updateStreamFn(cfg, opts...)
	}
	return &nats.StreamInfo{}, nil
}
func (m *mockJetStream) DeleteStream(name string, opts ...nats.JSOpt) error {
	if m.deleteStreamFn != nil {
		return m.deleteStreamFn(name, opts...)
	}
	return nil
}
func (m *mockJetStream) StreamInfo(stream string, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
	if m.streamInfoFn != nil {
		return m.streamInfoFn(stream, opts...)
	}
	return &nats.StreamInfo{}, nil
}
func (m *mockJetStream) PurgeStream(name string, opts ...nats.JSOpt) error {
	if m.purgeStreamFn != nil {
		return m.purgeStreamFn(name, opts...)
	}
	return nil
}
func (m *mockJetStream) StreamsInfo(opts ...nats.JSOpt) <-chan *nats.StreamInfo { return nil }
func (m *mockJetStream) Streams(opts ...nats.JSOpt) <-chan *nats.StreamInfo     { return nil }
func (m *mockJetStream) StreamNames(opts ...nats.JSOpt) <-chan string           { return nil }
func (m *mockJetStream) GetMsg(name string, seq uint64, opts ...nats.JSOpt) (*nats.RawStreamMsg, error) {
	return nil, nil
}
func (m *mockJetStream) GetLastMsg(name, subject string, opts ...nats.JSOpt) (*nats.RawStreamMsg, error) {
	return nil, nil
}
func (m *mockJetStream) DeleteMsg(name string, seq uint64, opts ...nats.JSOpt) error { return nil }
func (m *mockJetStream) SecureDeleteMsg(name string, seq uint64, opts ...nats.JSOpt) error {
	return nil
}
func (m *mockJetStream) AddConsumer(stream string, cfg *nats.ConsumerConfig, opts ...nats.JSOpt) (*nats.ConsumerInfo, error) {
	if m.addConsumerFn != nil {
		return m.addConsumerFn(stream, cfg, opts...)
	}
	return &nats.ConsumerInfo{}, nil
}
func (m *mockJetStream) UpdateConsumer(stream string, cfg *nats.ConsumerConfig, opts ...nats.JSOpt) (*nats.ConsumerInfo, error) {
	if m.updateConsumerFn != nil {
		return m.updateConsumerFn(stream, cfg, opts...)
	}
	return &nats.ConsumerInfo{}, nil
}
func (m *mockJetStream) DeleteConsumer(stream, consumer string, opts ...nats.JSOpt) error {
	if m.deleteConsumerFn != nil {
		return m.deleteConsumerFn(stream, consumer, opts...)
	}
	return nil
}
func (m *mockJetStream) ConsumerInfo(stream, name string, opts ...nats.JSOpt) (*nats.ConsumerInfo, error) {
	if m.consumerInfoFn != nil {
		return m.consumerInfoFn(stream, name, opts...)
	}
	return &nats.ConsumerInfo{}, nil
}
func (m *mockJetStream) ConsumersInfo(stream string, opts ...nats.JSOpt) <-chan *nats.ConsumerInfo {
	return nil
}
func (m *mockJetStream) Consumers(stream string, opts ...nats.JSOpt) <-chan *nats.ConsumerInfo {
	return nil
}
func (m *mockJetStream) ConsumerNames(stream string, opts ...nats.JSOpt) <-chan string { return nil }
func (m *mockJetStream) AccountInfo(opts ...nats.JSOpt) (*nats.AccountInfo, error) {
	if m.accountInfoFn != nil {
		return m.accountInfoFn(opts...)
	}
	return nil, nil
}
func (m *mockJetStream) StreamNameBySubject(subject string, opts ...nats.JSOpt) (string, error) {
	return "", nil
}

func (m *mockJetStream) KeyValue(bucket string) (nats.KeyValue, error) {
	if m.keyValueFn != nil {
		return m.keyValueFn(bucket)
	}
	return nil, nil
}
func (m *mockJetStream) CreateKeyValue(cfg *nats.KeyValueConfig) (nats.KeyValue, error) {
	if m.createKeyValueFn != nil {
		return m.createKeyValueFn(cfg)
	}
	return nil, nil
}
func (m *mockJetStream) DeleteKeyValue(bucket string) error {
	if m.deleteKeyValueFn != nil {
		return m.deleteKeyValueFn(bucket)
	}
	return nil
}
func (m *mockJetStream) KeyValueStoreNames() <-chan string          { return nil }
func (m *mockJetStream) KeyValueStores() <-chan nats.KeyValueStatus { return nil }

func (m *mockJetStream) ObjectStore(bucket string) (nats.ObjectStore, error) {
	if m.objectStoreFn != nil {
		return m.objectStoreFn(bucket)
	}
	return nil, nil
}
func (m *mockJetStream) CreateObjectStore(cfg *nats.ObjectStoreConfig) (nats.ObjectStore, error) {
	if m.createObjectStoreFn != nil {
		return m.createObjectStoreFn(cfg)
	}
	return nil, nil
}
func (m *mockJetStream) DeleteObjectStore(bucket string) error {
	if m.deleteObjectStoreFn != nil {
		return m.deleteObjectStoreFn(bucket)
	}
	return nil
}
func (m *mockJetStream) ObjectStoreNames(opts ...nats.ObjectOpt) <-chan string { return nil }
func (m *mockJetStream) ObjectStores(opts ...nats.ObjectOpt) <-chan nats.ObjectStoreStatus {
	return nil
}

// fakeJSMessage records settle calls so delivery-handle binding can be tested
// without a live JetStream (*nats.Msg.Ack needs a real subscription).
type fakeJSMessage struct {
	mu         sync.Mutex
	acked      bool
	nacked     bool
	nakDelay   time.Duration
	inProg     bool
	terminated bool
	ackErr     error
}

func (f *fakeJSMessage) Ack(opts ...nats.AckOpt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acked = true
	return f.ackErr
}

func (f *fakeJSMessage) NakWithDelay(delay time.Duration, opts ...nats.AckOpt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nacked = true
	f.nakDelay = delay
	return nil
}

func (f *fakeJSMessage) InProgress(opts ...nats.AckOpt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inProg = true
	return nil
}

func (f *fakeJSMessage) Term(opts ...nats.AckOpt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.terminated = true
	return nil
}

func (f *fakeJSMessage) settled() (acked, nacked, terminated bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acked, f.nacked, f.terminated
}

// TestAcceptance_DeliveryHandleIsolation proves Ack/Nak/InProgress bind to the
// exact delivery returned by Dequeue. Before per-delivery handles, the NATS
// queue kept a taskID-keyed msg map: a second delivery of the same task
// overwrote the first, so Ack(taskID) consumed the WRONG message. Two
// deliveries of the same task must settle independently.
func TestAcceptance_DeliveryHandleIsolation(t *testing.T) {
	q := &NATSTranscodingQueue{logger: zap.NewNop()}

	taskPayload, err := json.Marshal(&models.TranscodingTask{ID: "task-same", Status: "pending"})
	require.NoError(t, err)

	// Two deliveries of the SAME task (e.g. redelivery while the first
	// attempt is still in flight) — each settles its own message.
	msgA := &fakeJSMessage{}
	msgB := &fakeJSMessage{}
	taskA, handleA, err := q.deliverPayload(taskPayload, msgA, jsStreamName, 1)
	require.NoError(t, err)
	_, handleB, err := q.deliverPayload(taskPayload, msgB, jsStreamName, 2)
	require.NoError(t, err)
	assert.Equal(t, "task-same", taskA.ID)
	require.NotSame(t, handleA, handleB)
	require.Equal(t, handleA.TaskID(), handleB.TaskID(), "both deliveries carry the same task id")

	// Concurrent settlement of two deliveries of the same task must each hit
	// exactly their own message.
	var wg sync.WaitGroup
	for _, h := range []models.DeliveryHandle{handleA, handleB} {
		wg.Add(1)
		go func(h models.DeliveryHandle) {
			defer wg.Done()
			assert.NoError(t, q.Ack(h))
		}(h)
	}
	wg.Wait()

	ackedA, _, _ := msgA.settled()
	ackedB, _, _ := msgB.settled()
	assert.True(t, ackedA, "handle A must ack message A")
	assert.True(t, ackedB, "handle B must ack message B")

	// Nak binds the delay to its own delivery.
	msgC := &fakeJSMessage{}
	msgD := &fakeJSMessage{}
	handleC := &natsDelivery{taskID: "task-same", msg: msgC}
	handleD := &natsDelivery{taskID: "task-same", msg: msgD}
	require.NoError(t, q.Nak(handleC, 2*time.Second))
	_, nackedC, _ := msgC.settled()
	_, nackedD, _ := msgD.settled()
	assert.True(t, nackedC)
	assert.False(t, nackedD, "Nak of handle C must not consume message D")
	assert.Equal(t, 2*time.Second, msgC.nakDelay)

	// InProgress (heartbeat) likewise binds to one delivery only.
	require.NoError(t, q.InProgress(handleD))
	assert.True(t, msgD.inProg)
	assert.False(t, msgA.inProg)
}

func TestNATSTranscodingQueue_Ack_UnknownHandle(t *testing.T) {
	q := &NATSTranscodingQueue{logger: zap.NewNop()}
	err := q.Ack(nil)
	assert.Error(t, err)
	// A plain string is not a NATS delivery handle.
	err = q.Ack(models.DeliveryHandle(stringHandle("task-1")))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong")
}

func TestNATSTranscodingQueue_Nak_UnknownHandle(t *testing.T) {
	q := &NATSTranscodingQueue{logger: zap.NewNop()}
	err := q.Nak(nil, time.Second)
	assert.Error(t, err)
}

func TestNATSTranscodingQueue_InProgress_UnknownHandle(t *testing.T) {
	q := &NATSTranscodingQueue{logger: zap.NewNop()}
	err := q.InProgress(nil)
	assert.Error(t, err)
}

type stringHandle string

func (h stringHandle) TaskID() string { return string(h) }

func TestNATSTranscodingQueue_Close_Nil(t *testing.T) {
	q := &NATSTranscodingQueue{
		logger: zap.NewNop(),
	}
	err := q.Close()
	assert.NoError(t, err)
}

func TestNATSTranscodingQueue_Close_WithSub(t *testing.T) {
	q := &NATSTranscodingQueue{
		sub:    nil,
		conn:   nil,
		logger: zap.NewNop(),
	}
	err := q.Close()
	assert.NoError(t, err)
}

func TestNATSTranscodingQueue_Enqueue_MarshalError(t *testing.T) {
	q := &NATSTranscodingQueue{
		logger: zap.NewNop(),
		js:     nil,
	}

	task := &models.TranscodingTask{
		ID:        "test-task",
		ContentID: "content-1",
		Status:    "pending",
		Metadata:  map[string]interface{}{"bad": make(chan int)},
	}

	err := q.Enqueue(task)
	require.Error(t, err, "unmarshalable task must be rejected before any publish")
	assert.Contains(t, err.Error(), "failed to marshal task")
}

func TestNATSTranscodingQueue_Dequeue_NilSub(t *testing.T) {
	q := &NATSTranscodingQueue{
		logger: zap.NewNop(),
		sub:    nil,
	}

	_, _, err := q.Dequeue(context.Background())
	assert.Error(t, err)
}

func TestNATSTranscodingQueue_Depth_NilSub(t *testing.T) {
	q := &NATSTranscodingQueue{
		logger: zap.NewNop(),
		sub:    nil,
	}

	assert.Panics(t, func() {
		_, _ = q.Depth()
	})
}

func TestNATSTranscodingQueue_Constants(t *testing.T) {
	assert.Equal(t, "TRANSCODING", jsStreamName)
	assert.Equal(t, "streamgate.transcoding.tasks", jsStreamSubject)
	assert.Equal(t, "transcoding-worker", jsConsumerName)
	assert.Equal(t, "TRANSCODING_DLQ", jsDLQStreamName)
	assert.Equal(t, "streamgate.transcoding.dlq", jsDLQStreamSubject)
	assert.Equal(t, -1, jsMaxDeliver)
	assert.Equal(t, 30*time.Second, jsAckWait)
}

func TestNATSTranscodingQueue_PublishOutbox_TaskSubjectAndMsgID(t *testing.T) {
	var gotSubject, gotMsgID string
	var gotData []byte
	js := &mockJetStream{
		publishMsgFn: func(m *nats.Msg, opts ...nats.PubOpt) (*nats.PubAck, error) {
			gotSubject = m.Subject
			gotMsgID = m.Header.Get(nats.MsgIdHdr)
			gotData = append([]byte(nil), m.Data...)
			if len(opts) == 0 {
				t.Error("publish must carry the MsgId option")
			}
			return &nats.PubAck{}, nil
		},
	}
	q := &NATSTranscodingQueue{logger: zap.NewNop(), js: js}

	payload := []byte(`{"id":"task-1"}`)
	require.NoError(t, q.PublishOutbox(models.OutboxKindTask, "task-1", payload))
	assert.Equal(t, jsStreamSubject, gotSubject)
	assert.Equal(t, "task-1", gotMsgID)
	assert.Equal(t, payload, gotData)
}

func TestNATSTranscodingQueue_PublishOutbox_DLQSubject(t *testing.T) {
	var gotSubject, gotMsgID string
	js := &mockJetStream{
		publishMsgFn: func(m *nats.Msg, opts ...nats.PubOpt) (*nats.PubAck, error) {
			gotSubject = m.Subject
			gotMsgID = m.Header.Get(nats.MsgIdHdr)
			return &nats.PubAck{}, nil
		},
	}
	q := &NATSTranscodingQueue{logger: zap.NewNop(), js: js}

	require.NoError(t, q.PublishOutbox(models.OutboxKindDLQ, "task-9:42", []byte("dead")))
	assert.Equal(t, jsDLQStreamSubject, gotSubject)
	assert.Equal(t, "task-9:42", gotMsgID)
}

func TestNATSTranscodingQueue_PublishOutbox_UnknownKind(t *testing.T) {
	q := &NATSTranscodingQueue{logger: zap.NewNop(), js: &mockJetStream{}}
	err := q.PublishOutbox("bogus", "id", nil)
	assert.Error(t, err)
}

func TestNATSTranscodingQueue_PublishOutbox_PublishError(t *testing.T) {
	js := &mockJetStream{
		publishMsgFn: func(m *nats.Msg, opts ...nats.PubOpt) (*nats.PubAck, error) {
			return nil, errors.New("nats down")
		},
	}
	q := &NATSTranscodingQueue{logger: zap.NewNop(), js: js}
	err := q.PublishOutbox(models.OutboxKindTask, "task-1", []byte("{}"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to publish")
}

func TestNATSTranscodingQueue_Enqueue_PublishesWithMsgID(t *testing.T) {
	var gotMsgID string
	js := &mockJetStream{
		publishMsgFn: func(m *nats.Msg, opts ...nats.PubOpt) (*nats.PubAck, error) {
			gotMsgID = m.Header.Get(nats.MsgIdHdr)
			return &nats.PubAck{}, nil
		},
	}
	q := &NATSTranscodingQueue{logger: zap.NewNop(), js: js}

	task := &models.TranscodingTask{
		ID:        "enqueue-ok",
		ContentID: "content-1",
		Status:    "pending",
	}
	require.NoError(t, q.Enqueue(task))
	assert.Equal(t, "enqueue-ok", gotMsgID, "Enqueue must publish with nats.MsgID(taskID)")
}

func TestNATSTranscodingQueue_ensureStream_ExistingStream(t *testing.T) {
	js := &mockJetStream{
		streamInfoFn: func(stream string, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
			return &nats.StreamInfo{}, nil
		},
	}

	q := &NATSTranscodingQueue{
		logger: zap.NewNop(),
		js:     js,
	}

	err := q.ensureStream()
	assert.NoError(t, err)
}

func TestNATSTranscodingQueue_ensureStream_CreateNew(t *testing.T) {
	js := &mockJetStream{
		streamInfoFn: func(stream string, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
			return nil, errors.New("stream not found")
		},
		addStreamFn: func(cfg *nats.StreamConfig, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
			return &nats.StreamInfo{}, nil
		},
	}

	q := &NATSTranscodingQueue{
		logger: zap.NewNop(),
		js:     js,
	}

	err := q.ensureStream()
	assert.NoError(t, err)
}

func TestNATSTranscodingQueue_ensureStream_AddStreamFails(t *testing.T) {
	js := &mockJetStream{
		streamInfoFn: func(stream string, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
			return nil, errors.New("stream not found")
		},
		addStreamFn: func(cfg *nats.StreamConfig, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
			return nil, errors.New("add stream failed")
		},
	}

	q := &NATSTranscodingQueue{
		logger: zap.NewNop(),
		js:     js,
	}

	err := q.ensureStream()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create JetStream stream")
}

func TestNATSTranscodingQueue_ensureDLQStream_Existing(t *testing.T) {
	js := &mockJetStream{
		streamInfoFn: func(stream string, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
			return &nats.StreamInfo{}, nil
		},
	}

	q := &NATSTranscodingQueue{
		logger: zap.NewNop(),
		js:     js,
	}

	q.ensureDLQStream()
}

func TestNATSTranscodingQueue_ensureDLQStream_CreateNew(t *testing.T) {
	streamInfoCalled := false
	js := &mockJetStream{
		streamInfoFn: func(stream string, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
			if stream == jsDLQStreamName && !streamInfoCalled {
				streamInfoCalled = true
				return nil, errors.New("not found")
			}
			return &nats.StreamInfo{}, nil
		},
		addStreamFn: func(cfg *nats.StreamConfig, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
			return &nats.StreamInfo{}, nil
		},
	}

	q := &NATSTranscodingQueue{
		logger: zap.NewNop(),
		js:     js,
	}

	q.ensureDLQStream()
	assert.True(t, streamInfoCalled)
}

func TestNATSTranscodingQueue_ensureDLQStream_AddStreamFails(t *testing.T) {
	js := &mockJetStream{
		streamInfoFn: func(stream string, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
			return nil, errors.New("not found")
		},
		addStreamFn: func(cfg *nats.StreamConfig, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
			return nil, errors.New("add stream failed")
		},
	}

	q := &NATSTranscodingQueue{
		logger: zap.NewNop(),
		js:     js,
	}

	q.ensureDLQStream()
}

func TestNATSTranscodingQueue_ensureMaxDeliverConsumer_ConvergesLegacyConfig(t *testing.T) {
	// A consumer left behind by an older build (MaxDeliver=5, no AckWait)
	// must be updated to MaxDeliver=-1 + AckWait=30s, otherwise messages get
	// silently exhausted by the transport instead of by the business cap.
	var updated *nats.ConsumerConfig
	js := &mockJetStream{
		consumerInfoFn: func(stream, name string, opts ...nats.JSOpt) (*nats.ConsumerInfo, error) {
			return &nats.ConsumerInfo{Config: nats.ConsumerConfig{Durable: name, MaxDeliver: 5}}, nil
		},
		updateConsumerFn: func(stream string, cfg *nats.ConsumerConfig, opts ...nats.JSOpt) (*nats.ConsumerInfo, error) {
			updated = cfg
			return &nats.ConsumerInfo{}, nil
		},
	}

	q := &NATSTranscodingQueue{logger: zap.NewNop(), js: js}
	q.ensureMaxDeliverConsumer()
	require.NotNil(t, updated)
	assert.Equal(t, -1, updated.MaxDeliver)
	assert.Equal(t, 30*time.Second, updated.AckWait)
}

func TestNATSTranscodingQueue_ensureMaxDeliverConsumer_ExistingMatchedConfig(t *testing.T) {
	updateCalled := false
	js := &mockJetStream{
		consumerInfoFn: func(stream, name string, opts ...nats.JSOpt) (*nats.ConsumerInfo, error) {
			return &nats.ConsumerInfo{Config: nats.ConsumerConfig{Durable: name, MaxDeliver: -1, AckWait: 30 * time.Second}}, nil
		},
		updateConsumerFn: func(stream string, cfg *nats.ConsumerConfig, opts ...nats.JSOpt) (*nats.ConsumerInfo, error) {
			updateCalled = true
			return &nats.ConsumerInfo{}, nil
		},
	}

	q := &NATSTranscodingQueue{logger: zap.NewNop(), js: js}
	q.ensureMaxDeliverConsumer()
	assert.False(t, updateCalled)
}

func TestNATSTranscodingQueue_ensureMaxDeliverConsumer_CreateNew(t *testing.T) {
	js := &mockJetStream{
		consumerInfoFn: func(stream, name string, opts ...nats.JSOpt) (*nats.ConsumerInfo, error) {
			return nil, errors.New("not found")
		},
		addConsumerFn: func(stream string, cfg *nats.ConsumerConfig, opts ...nats.JSOpt) (*nats.ConsumerInfo, error) {
			return &nats.ConsumerInfo{}, nil
		},
	}

	q := &NATSTranscodingQueue{
		logger: zap.NewNop(),
		js:     js,
	}

	q.ensureMaxDeliverConsumer()
}

func TestNATSTranscodingQueue_ensureMaxDeliverConsumer_AddFails(t *testing.T) {
	js := &mockJetStream{
		consumerInfoFn: func(stream, name string, opts ...nats.JSOpt) (*nats.ConsumerInfo, error) {
			return nil, errors.New("not found")
		},
		addConsumerFn: func(stream string, cfg *nats.ConsumerConfig, opts ...nats.JSOpt) (*nats.ConsumerInfo, error) {
			return nil, errors.New("add consumer failed")
		},
	}

	q := &NATSTranscodingQueue{
		logger: zap.NewNop(),
		js:     js,
	}

	q.ensureMaxDeliverConsumer()
}

// TestNATSTranscodingQueue_DeliverPoison terms an unparseable payload after
// publishing it to the DLQ keyed by stream:sequence.
func TestNATSTranscodingQueue_DeliverPoison(t *testing.T) {
	var dlqSubject, dlqMsgID string
	var dlqData []byte
	js := &mockJetStream{
		publishMsgFn: func(m *nats.Msg, opts ...nats.PubOpt) (*nats.PubAck, error) {
			dlqSubject = m.Subject
			dlqMsgID = m.Header.Get(nats.MsgIdHdr)
			dlqData = append([]byte(nil), m.Data...)
			return &nats.PubAck{}, nil
		},
	}
	q := &NATSTranscodingQueue{logger: zap.NewNop(), js: js}

	poison := &fakeJSMessage{}
	_, handle, err := q.deliverPayload([]byte("not-json"), poison, jsStreamName, 1)
	assert.Error(t, err)
	assert.Nil(t, handle)
	assert.Equal(t, jsDLQStreamSubject, dlqSubject)
	assert.Equal(t, "TRANSCODING:1", dlqMsgID, "poison DLQ id is stream:sequence")
	assert.Equal(t, []byte("not-json"), dlqData)
	_, _, termed := poison.settled()
	assert.True(t, termed, "poison message must be Terminated, not looped")
}

// TestNATSTranscodingQueue_DeliverPoison_PublishFailureDelays proves a failed
// DLQ publish leaves the poison message redeliverable (delayed Nak, no Term).
func TestNATSTranscodingQueue_DeliverPoison_PublishFailureDelays(t *testing.T) {
	js := &mockJetStream{
		publishMsgFn: func(m *nats.Msg, opts ...nats.PubOpt) (*nats.PubAck, error) {
			return nil, errors.New("dlq down")
		},
	}
	q := &NATSTranscodingQueue{logger: zap.NewNop(), js: js}

	poison := &fakeJSMessage{}
	_, _, err := q.deliverPayload([]byte("not-json"), poison, jsStreamName, 2)
	assert.Error(t, err)
	_, nacked, termed := poison.settled()
	assert.True(t, nacked)
	assert.False(t, termed)
}

func TestNATSTranscodingQueue_Deliver_ValidTask(t *testing.T) {
	q := &NATSTranscodingQueue{logger: zap.NewNop()}
	payload, err := json.Marshal(&models.TranscodingTask{ID: "task-ok", ContentID: "c1", Status: "pending"})
	require.NoError(t, err)

	msg := &fakeJSMessage{}
	task, handle, err := q.deliverPayload(payload, msg, jsStreamName, 3)
	require.NoError(t, err)
	assert.Equal(t, "task-ok", task.ID)
	require.NotNil(t, handle)
	assert.Equal(t, "task-ok", handle.TaskID())
}

func TestNATSTranscodingQueue_Deliver_MissingIDIsPoison(t *testing.T) {
	js := &mockJetStream{}
	q := &NATSTranscodingQueue{logger: zap.NewNop(), js: js}
	// A payload without a task id cannot be claimed or DLQ'd by task id —
	// treat it as poison instead of crashing the worker later.
	_, _, err := q.deliverPayload([]byte(`{"content_id":"c"}`), &fakeJSMessage{}, jsStreamName, 4)
	assert.Error(t, err)
}

func TestNATSTranscodingQueue_NewNATSTranscodingQueue_ConnectionFailed(t *testing.T) {
	_, err := NewNATSTranscodingQueue("nats://localhost:4222", zap.NewNop())
	assert.Error(t, err)
}
