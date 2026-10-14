package storage

// nats_queue_metrics_test.go — SG-14 part 2: queue metrics wiring.
//
// These tests pin QueueAttemptsTotal{queue,outcome} at the settle points (Ack →
// "delivered", Nak → "redelivered", poison → "dlq") and QueueMessageAgeSeconds
// at the dequeue metadata timestamp. They read counters from the default
// registry directly instead of prometheus testutil, which would pull in a new
// module (kylelemons/godebug) — this project adds no dependencies.

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// queueCounter reads one counter series of a family from the default registry.
func queueCounter(t *testing.T, name string, wantLabels map[string]string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			if !queueLabelsMatch(m, wantLabels) {
				continue
			}
			return m.GetCounter().GetValue()
		}
	}
	return 0
}

// queueHistogramCount reads the sample count of one histogram series.
func queueHistogramCount(t *testing.T, name string, wantLabels map[string]string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			if !queueLabelsMatch(m, wantLabels) {
				continue
			}
			return float64(m.GetHistogram().GetSampleCount())
		}
	}
	return 0
}

func queueLabelsMatch(m *dto.Metric, want map[string]string) bool {
	got := map[string]string{}
	for _, lp := range m.GetLabel() {
		got[lp.GetName()] = lp.GetValue()
	}
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

func attemptsLabel() map[string]string { return map[string]string{"queue": "transcoding"} }

func TestNATSQueue_Ack_RecordsDelivered(t *testing.T) {
	q := &NATSTranscodingQueue{logger: zap.NewNop()}
	before := queueCounter(t, "streamgate_queue_attempts_total",
		map[string]string{"queue": "transcoding", "outcome": "delivered"})

	msg := &fakeJSMessage{}
	handle := &natsDelivery{taskID: "task-ack", msg: msg}
	require.NoError(t, q.Ack(handle))

	after := queueCounter(t, "streamgate_queue_attempts_total",
		map[string]string{"queue": "transcoding", "outcome": "delivered"})
	assert.Equal(t, before+1, after, "Ack must count QueueAttemptsTotal{delivered}")
}

func TestNATSQueue_Nak_RecordsRedelivered(t *testing.T) {
	q := &NATSTranscodingQueue{logger: zap.NewNop()}
	before := queueCounter(t, "streamgate_queue_attempts_total",
		map[string]string{"queue": "transcoding", "outcome": "redelivered"})

	msg := &fakeJSMessage{}
	handle := &natsDelivery{taskID: "task-nak", msg: msg}
	require.NoError(t, q.Nak(handle, time.Second))

	after := queueCounter(t, "streamgate_queue_attempts_total",
		map[string]string{"queue": "transcoding", "outcome": "redelivered"})
	assert.Equal(t, before+1, after, "Nak must count QueueAttemptsTotal{redelivered}")
}

func TestNATSQueue_PoisonDelivery_RecordsDLQ(t *testing.T) {
	js := &mockJetStream{
		publishMsgFn: func(m *nats.Msg, opts ...nats.PubOpt) (*nats.PubAck, error) {
			return &nats.PubAck{}, nil
		},
	}
	q := &NATSTranscodingQueue{logger: zap.NewNop(), js: js}
	before := queueCounter(t, "streamgate_queue_attempts_total",
		map[string]string{"queue": "transcoding", "outcome": "dlq"})

	_, _, err := q.deliverPayload([]byte("not-json"), &fakeJSMessage{}, jsStreamName, 7)
	assert.Error(t, err)

	after := queueCounter(t, "streamgate_queue_attempts_total",
		map[string]string{"queue": "transcoding", "outcome": "dlq"})
	assert.Equal(t, before+1, after, "a poisoned message moved to the DLQ must count QueueAttemptsTotal{dlq}")
}

func TestNATSQueue_ObserveQueueAge_FromBrokerTimestamp(t *testing.T) {
	before := queueHistogramCount(t, "streamgate_queue_message_age_seconds", attemptsLabel())

	// A publish timestamp 2s in the past yields one observation.
	observeQueueAge(time.Now().Add(-2 * time.Second))
	after := queueHistogramCount(t, "streamgate_queue_message_age_seconds", attemptsLabel())
	assert.Equal(t, before+1, after, "a usable broker timestamp must record one age observation")
}

func TestNATSQueue_ObserveQueueAge_ZeroTimestampIsSkipped(t *testing.T) {
	before := queueHistogramCount(t, "streamgate_queue_message_age_seconds", attemptsLabel())

	// No publish timestamp → nothing to subtract; the age must NOT be guessed.
	observeQueueAge(time.Time{})
	after := queueHistogramCount(t, "streamgate_queue_message_age_seconds", attemptsLabel())
	assert.Equal(t, before, after, "a missing broker timestamp must not record a (fake) age")
}

func TestNATSQueue_ObserveQueueAge_FutureTimestampSkipped(t *testing.T) {
	before := queueHistogramCount(t, "streamgate_queue_message_age_seconds", attemptsLabel())

	observeQueueAge(time.Now().Add(time.Minute))
	after := queueHistogramCount(t, "streamgate_queue_message_age_seconds", attemptsLabel())
	assert.Equal(t, before, after, "a future timestamp must not record a negative age")
}

// Compile-time guard: the queue interface is unchanged by the metrics wiring.
var _ models.TranscodingQueue = (*NATSTranscodingQueue)(nil)
