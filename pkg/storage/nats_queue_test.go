package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	return &nats.AccountInfo{}, nil
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

func TestNATSTranscodingQueue_GetStatus_NotFound(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses: make(map[string]statusEntry),
		logger:   zap.NewNop(),
	}

	_, err := q.GetStatus("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
}

func TestNATSTranscodingQueue_GetStatus_Found(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses: map[string]statusEntry{
			"task-1": {status: "pending", updatedAt: time.Now()},
		},
		logger: zap.NewNop(),
	}

	status, err := q.GetStatus("task-1")
	require.NoError(t, err)
	assert.Equal(t, "pending", status)
}

func TestNATSTranscodingQueue_Ack_NotFound(t *testing.T) {
	q := &NATSTranscodingQueue{
		messages: make(map[string]msgEntry),
		logger:   zap.NewNop(),
	}

	err := q.Ack("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "message not found")
}

func TestNATSTranscodingQueue_Nak_NotFound(t *testing.T) {
	q := &NATSTranscodingQueue{
		messages: make(map[string]msgEntry),
		logger:   zap.NewNop(),
	}

	err := q.Nak("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "message not found")
}

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

func TestNATSTranscodingQueue_maybeCleanup_TooEarly(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses:  make(map[string]statusEntry),
		messages:  make(map[string]msgEntry),
		logger:    zap.NewNop(),
		lastClean: time.Now(),
	}

	q.maybeCleanup()
}

func TestNATSTranscodingQueue_maybeCleanup_StaleMessages(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses:  make(map[string]statusEntry),
		messages:  make(map[string]msgEntry),
		logger:    zap.NewNop(),
		lastClean: time.Now().Add(-cleanupInterval - time.Second),
	}

	q.messages["stale-msg"] = msgEntry{
		msg:      nil,
		dequeued: time.Now().Add(-msgStaleTimeout - time.Hour),
	}
	q.messages["fresh-msg"] = msgEntry{
		msg:      nil,
		dequeued: time.Now(),
	}

	q.maybeCleanup()

	_, hasStale := q.messages["stale-msg"]
	assert.False(t, hasStale)

	_, hasFresh := q.messages["fresh-msg"]
	assert.True(t, hasFresh)
}

func TestNATSTranscodingQueue_maybeCleanup_StaleStatuses(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses:  make(map[string]statusEntry),
		messages:  make(map[string]msgEntry),
		logger:    zap.NewNop(),
		lastClean: time.Now().Add(-cleanupInterval - time.Second),
	}

	q.statuses["stale-status"] = statusEntry{
		status:    "completed",
		updatedAt: time.Now().Add(-statusStaleTimeout - time.Hour),
	}
	q.statuses["fresh-status"] = statusEntry{
		status:    "pending",
		updatedAt: time.Now(),
	}

	q.maybeCleanup()

	_, hasStale := q.statuses["stale-status"]
	assert.False(t, hasStale)

	_, hasFresh := q.statuses["fresh-status"]
	assert.True(t, hasFresh)
}

func TestNATSTranscodingQueue_maybeCleanup_ConcurrentSafety(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses:  make(map[string]statusEntry),
		messages:  make(map[string]msgEntry),
		logger:    zap.NewNop(),
		lastClean: time.Now().Add(-cleanupInterval - time.Second),
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q.maybeCleanup()
		}()
	}
	wg.Wait()
}

func TestNATSTranscodingQueue_Enqueue_MarshalError(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
		logger:   zap.NewNop(),
		js:       nil,
	}

	task := &models.TranscodingTask{
		ID:        "test-task",
		ContentID: "content-1",
		Status:    "pending",
	}

	assert.Panics(t, func() {
		_ = q.Enqueue(task)
	})
}

func TestNATSTranscodingQueue_Dequeue_NilSub(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
		logger:   zap.NewNop(),
		sub:      nil,
	}

	_, err := q.Dequeue(context.Background())
	assert.Error(t, err)
}

func TestNATSTranscodingQueue_Depth_NilSub(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
		logger:   zap.NewNop(),
		sub:      nil,
	}

	assert.Panics(t, func() {
		_, _ = q.Depth()
	})
}

func TestStatusEntry_Fields(t *testing.T) {
	now := time.Now()
	entry := statusEntry{
		status:    "processing",
		updatedAt: now,
	}
	assert.Equal(t, "processing", entry.status)
	assert.Equal(t, now, entry.updatedAt)
}

func TestMsgEntry_Fields(t *testing.T) {
	now := time.Now()
	entry := msgEntry{
		msg:      nil,
		dequeued: now,
	}
	assert.Nil(t, entry.msg)
	assert.Equal(t, now, entry.dequeued)
}

func TestNATSTranscodingQueue_Constants(t *testing.T) {
	assert.Equal(t, "TRANSCODING", jsStreamName)
	assert.Equal(t, "streamgate.transcoding.tasks", jsStreamSubject)
	assert.Equal(t, "transcoding-worker", jsConsumerName)
	assert.Equal(t, "TRANSCODING_DLQ", jsDLQStreamName)
	assert.Equal(t, "streamgate.transcoding.dlq", jsDLQStreamSubject)
	assert.Equal(t, 5, jsMaxDeliver)
	assert.Equal(t, 30*time.Minute, msgStaleTimeout)
	assert.Equal(t, 2*time.Hour, statusStaleTimeout)
	assert.Equal(t, 5*time.Minute, cleanupInterval)
}

func TestNATSTranscodingQueue_Ack_UpdatesStatus(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses: make(map[string]statusEntry),
		messages: map[string]msgEntry{
			"task-1": {msg: nil, dequeued: time.Now()},
		},
		logger: zap.NewNop(),
	}

	err := q.Ack("task-1")
	assert.Error(t, err)
}

func TestNATSTranscodingQueue_Nak_DeletesMessage(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses: make(map[string]statusEntry),
		messages: map[string]msgEntry{
			"task-1": {msg: nil, dequeued: time.Now()},
		},
		logger: zap.NewNop(),
	}

	err := q.Nak("task-1")
	assert.Error(t, err)

	_, exists := q.messages["task-1"]
	assert.False(t, exists)
}

func TestNATSTranscodingQueue_GetStatus_ConcurrentAccess(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses: make(map[string]statusEntry),
		logger:   zap.NewNop(),
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			taskID := "task-concurrent"
			q.statusMu.Lock()
			q.statuses[taskID] = statusEntry{status: "pending", updatedAt: time.Now()}
			q.statusMu.Unlock()

			status, err := q.GetStatus(taskID)
			if err == nil {
				assert.Equal(t, "pending", status)
			}
		}(i)
	}
	wg.Wait()
}

func TestNATSTranscodingQueue_Enqueue_RecordsStatus(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses:  make(map[string]statusEntry),
		messages:  make(map[string]msgEntry),
		logger:    zap.NewNop(),
		lastClean: time.Now(),
		js:        nil,
	}

	task := &models.TranscodingTask{
		ID:        "test-enqueue",
		ContentID: "content-1",
		Status:    "pending",
	}

	assert.Panics(t, func() {
		_ = q.Enqueue(task)
	})
}

func TestWalletChallenge_Fields(t *testing.T) {
	now := time.Now()
	ch := WalletChallenge{
		ID:            "challenge-1",
		WalletAddress: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18",
		ChainID:       1,
		SigningType:   "personal_sign",
		Nonce:         "abc123",
		Message:       "Sign this message",
		IssuedAt:      now,
		ExpiresAt:     now.Add(5 * time.Minute),
	}

	assert.Equal(t, "challenge-1", ch.ID)
	assert.Equal(t, "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD18", ch.WalletAddress)
	assert.Equal(t, int64(1), ch.ChainID)
	assert.Equal(t, "personal_sign", ch.SigningType)
	assert.Equal(t, "abc123", ch.Nonce)
}

func TestWalletChallenge_JSON_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	ch := WalletChallenge{
		ID:            "challenge-json",
		WalletAddress: "0xABC",
		ChainID:       1,
		SigningType:   "eip712",
		Nonce:         "nonce123",
		Message:       "Sign this",
		IssuedAt:      now,
		ExpiresAt:     now.Add(5 * time.Minute),
	}

	data, err := json.Marshal(ch)
	require.NoError(t, err)

	var decoded WalletChallenge
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, ch.ID, decoded.ID)
	assert.Equal(t, ch.WalletAddress, decoded.WalletAddress)
	assert.Equal(t, ch.ChainID, decoded.ChainID)
	assert.Equal(t, ch.SigningType, decoded.SigningType)
	assert.Equal(t, ch.Nonce, decoded.Nonce)
	assert.Equal(t, ch.Message, decoded.Message)
}

func TestNATSTranscodingQueue_Enqueue_RecordsStatusOnSuccess(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses:  make(map[string]statusEntry),
		messages:  make(map[string]msgEntry),
		logger:    zap.NewNop(),
		lastClean: time.Now(),
		js:        nil,
	}

	task := &models.TranscodingTask{
		ID:        "enqueue-status-test",
		ContentID: "content-1",
		Status:    "queued",
	}

	assert.Panics(t, func() {
		_ = q.Enqueue(task)
	})
}

func TestNATSTranscodingQueue_Close_WithSubAndConn(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
		logger:   zap.NewNop(),
		sub:      nil,
		conn:     nil,
	}

	err := q.Close()
	assert.NoError(t, err)
}

func TestNATSTranscodingQueue_maybeCleanup_ResetsLastClean(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses:  make(map[string]statusEntry),
		messages:  make(map[string]msgEntry),
		logger:    zap.NewNop(),
		lastClean: time.Now().Add(-cleanupInterval - time.Second),
	}

	q.maybeCleanup()

	q.cleanupMu.Lock()
	afterClean := q.lastClean
	q.cleanupMu.Unlock()

	assert.True(t, afterClean.After(time.Now().Add(-time.Second)))
}

func TestNATSTranscodingQueue_maybeCleanup_SkipsWhenRecent(t *testing.T) {
	now := time.Now()
	q := &NATSTranscodingQueue{
		statuses:  make(map[string]statusEntry),
		messages:  make(map[string]msgEntry),
		logger:    zap.NewNop(),
		lastClean: now,
	}

	q.maybeCleanup()

	q.cleanupMu.Lock()
	stillNow := q.lastClean
	q.cleanupMu.Unlock()

	assert.Equal(t, now, stillNow)
}

func TestNATSTranscodingQueue_GetStatus_MultipleEntries(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses: map[string]statusEntry{
			"task-1": {status: "pending", updatedAt: time.Now()},
			"task-2": {status: "processing", updatedAt: time.Now()},
			"task-3": {status: "completed", updatedAt: time.Now()},
		},
		logger: zap.NewNop(),
	}

	tests := []struct {
		id      string
		want    string
		wantErr bool
	}{
		{"task-1", "pending", false},
		{"task-2", "processing", false},
		{"task-3", "completed", false},
		{"task-99", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got, err := q.GetStatus(tt.id)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestNATSTranscodingQueue_Ack_DeletesFromMessages(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses: make(map[string]statusEntry),
		messages: map[string]msgEntry{
			"task-ack": {msg: nil, dequeued: time.Now()},
		},
		logger: zap.NewNop(),
	}

	err := q.Ack("task-ack")
	assert.Error(t, err)

	_, exists := q.messages["task-ack"]
	assert.False(t, exists)
}

func TestNATSTranscodingQueue_Ack_UpdatesStatusToCompleted(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses: map[string]statusEntry{
			"task-complete": {status: "processing", updatedAt: time.Now()},
		},
		messages: map[string]msgEntry{
			"task-complete": {msg: nil, dequeued: time.Now()},
		},
		logger: zap.NewNop(),
	}

	err := q.Ack("task-complete")
	assert.Error(t, err)

	_, exists := q.messages["task-complete"]
	assert.False(t, exists)
}

func TestNATSTranscodingQueue_Enqueue_Success(t *testing.T) {
	js := &mockJetStream{}

	q := &NATSTranscodingQueue{
		statuses:  make(map[string]statusEntry),
		messages:  make(map[string]msgEntry),
		logger:    zap.NewNop(),
		lastClean: time.Now(),
		js:        js,
	}

	task := &models.TranscodingTask{
		ID:        "enqueue-ok",
		ContentID: "content-1",
		Status:    "queued",
	}

	err := q.Enqueue(task)
	require.NoError(t, err)

	q.statusMu.RLock()
	entry, ok := q.statuses["enqueue-ok"]
	q.statusMu.RUnlock()
	assert.True(t, ok)
	assert.Equal(t, "queued", entry.status)
}

func TestNATSTranscodingQueue_Enqueue_PublishError(t *testing.T) {
	js := &mockJetStream{
		publishFn: func(subj string, data []byte, opts ...nats.PubOpt) (*nats.PubAck, error) {
			return nil, errors.New("publish failed")
		},
	}

	q := &NATSTranscodingQueue{
		statuses:  make(map[string]statusEntry),
		messages:  make(map[string]msgEntry),
		logger:    zap.NewNop(),
		lastClean: time.Now(),
		js:        js,
	}

	task := &models.TranscodingTask{
		ID:        "enqueue-fail",
		ContentID: "content-1",
		Status:    "queued",
	}

	err := q.Enqueue(task)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to publish task to JetStream")
}

func TestNATSTranscodingQueue_Enqueue_TriggersCleanup(t *testing.T) {
	js := &mockJetStream{}

	q := &NATSTranscodingQueue{
		statuses:  make(map[string]statusEntry),
		messages:  make(map[string]msgEntry),
		logger:    zap.NewNop(),
		lastClean: time.Now().Add(-cleanupInterval - time.Second),
		js:        js,
	}

	q.statuses["old"] = statusEntry{
		status:    "completed",
		updatedAt: time.Now().Add(-statusStaleTimeout - time.Hour),
	}

	task := &models.TranscodingTask{
		ID:        "enqueue-cleanup",
		ContentID: "content-1",
		Status:    "queued",
	}

	err := q.Enqueue(task)
	require.NoError(t, err)

	q.statusMu.RLock()
	_, hasOld := q.statuses["old"]
	_, hasNew := q.statuses["enqueue-cleanup"]
	q.statusMu.RUnlock()
	assert.False(t, hasOld)
	assert.True(t, hasNew)
}

func TestNATSTranscodingQueue_ensureStream_ExistingStream(t *testing.T) {
	js := &mockJetStream{
		streamInfoFn: func(stream string, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
			return &nats.StreamInfo{}, nil
		},
	}

	q := &NATSTranscodingQueue{
		logger:   zap.NewNop(),
		js:       js,
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
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
		logger:   zap.NewNop(),
		js:       js,
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
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
		logger:   zap.NewNop(),
		js:       js,
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
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
		logger:   zap.NewNop(),
		js:       js,
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
	}

	q.ensureDLQStream()
}

func TestNATSTranscodingQueue_ensureDLQStream_CreateNew(t *testing.T) {
	streamInfoCalled := false
	js := &mockJetStream{
		streamInfoFn: func(stream string, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
			if !streamInfoCalled {
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
		logger:   zap.NewNop(),
		js:       js,
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
	}

	q.ensureDLQStream()
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
		logger:   zap.NewNop(),
		js:       js,
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
	}

	q.ensureDLQStream()
}

func TestNATSTranscodingQueue_ensureMaxDeliverConsumer_Existing(t *testing.T) {
	js := &mockJetStream{
		consumerInfoFn: func(stream, name string, opts ...nats.JSOpt) (*nats.ConsumerInfo, error) {
			return &nats.ConsumerInfo{}, nil
		},
	}

	q := &NATSTranscodingQueue{
		logger:   zap.NewNop(),
		js:       js,
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
	}

	q.ensureMaxDeliverConsumer()
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
		logger:   zap.NewNop(),
		js:       js,
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
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
		logger:   zap.NewNop(),
		js:       js,
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
	}

	q.ensureMaxDeliverConsumer()
}

func TestNATSTranscodingQueue_Dequeue_UnmarshalError(t *testing.T) {
	msg := &nats.Msg{
		Subject: jsStreamSubject,
		Data:    []byte("invalid-json"),
	}

	q := &NATSTranscodingQueue{
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
		logger:   zap.NewNop(),
	}

	q.msgMu.Lock()
	q.messages["test"] = msgEntry{msg: msg, dequeued: time.Now()}
	q.msgMu.Unlock()

	var task models.TranscodingTask
	err := json.Unmarshal([]byte("invalid-json"), &task)
	assert.Error(t, err)
}

func TestNATSTranscodingQueue_Ack_WithMsg(t *testing.T) {
	msg := &nats.Msg{
		Subject: jsStreamSubject,
		Data:    []byte(`{"id":"task-ack-msg"}`),
	}

	q := &NATSTranscodingQueue{
		statuses: make(map[string]statusEntry),
		messages: map[string]msgEntry{
			"task-ack-msg": {msg: msg, dequeued: time.Now()},
		},
		logger: zap.NewNop(),
	}

	err := q.Ack("task-ack-msg")
	assert.Error(t, err)

	_, exists := q.messages["task-ack-msg"]
	assert.False(t, exists)
}

func TestNATSTranscodingQueue_Nak_WithMsg(t *testing.T) {
	msg := &nats.Msg{
		Subject: jsStreamSubject,
		Data:    []byte(`{"id":"task-nak-msg"}`),
	}

	q := &NATSTranscodingQueue{
		statuses: make(map[string]statusEntry),
		messages: map[string]msgEntry{
			"task-nak-msg": {msg: msg, dequeued: time.Now()},
		},
		logger: zap.NewNop(),
	}

	err := q.Nak("task-nak-msg")
	assert.Error(t, err)

	_, exists := q.messages["task-nak-msg"]
	assert.False(t, exists)
}

func TestNATSTranscodingQueue_Close_WithNilSubAndConn(t *testing.T) {
	q := &NATSTranscodingQueue{
		statuses: make(map[string]statusEntry),
		messages: make(map[string]msgEntry),
		logger:   zap.NewNop(),
		sub:      nil,
		conn:     nil,
	}

	err := q.Close()
	assert.NoError(t, err)
}

func TestNATSTranscodingQueue_NewNATSTranscodingQueue_ConnectionFailed(t *testing.T) {
	_, err := NewNATSTranscodingQueue("nats://localhost:4222", zap.NewNop())
	assert.Error(t, err)
}

func TestNATSTranscodingQueue_Enqueue_ConcurrentEnqueues(t *testing.T) {
	js := &mockJetStream{}

	q := &NATSTranscodingQueue{
		statuses:  make(map[string]statusEntry),
		messages:  make(map[string]msgEntry),
		logger:    zap.NewNop(),
		lastClean: time.Now(),
		js:        js,
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			task := &models.TranscodingTask{
				ID:        fmt.Sprintf("concurrent-%d", idx),
				ContentID: "content-1",
				Status:    "queued",
			}
			_ = q.Enqueue(task)
		}(i)
	}
	wg.Wait()

	q.statusMu.RLock()
	count := len(q.statuses)
	q.statusMu.RUnlock()
	assert.Equal(t, 50, count)
}

func TestNATSTranscodingQueue_Enqueue_MultipleTasks(t *testing.T) {
	js := &mockJetStream{}

	q := &NATSTranscodingQueue{
		statuses:  make(map[string]statusEntry),
		messages:  make(map[string]msgEntry),
		logger:    zap.NewNop(),
		lastClean: time.Now(),
		js:        js,
	}

	tasks := []struct {
		id     string
		status string
	}{
		{"task-a", "queued"},
		{"task-b", "processing"},
		{"task-c", "pending"},
	}

	for _, tt := range tasks {
		task := &models.TranscodingTask{
			ID:        tt.id,
			ContentID: "content-1",
			Status:    tt.status,
		}
		err := q.Enqueue(task)
		require.NoError(t, err)
	}

	for _, tt := range tasks {
		status, err := q.GetStatus(tt.id)
		require.NoError(t, err)
		assert.Equal(t, tt.status, status)
	}
}
