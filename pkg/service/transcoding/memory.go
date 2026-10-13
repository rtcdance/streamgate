package transcoding

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rtcdance/streamgate/pkg/models"
)

// memItem is one queued task. availableAt implements the delayed re-queue for
// Nak: the item becomes visible again only after the backoff elapses.
type memItem struct {
	task        *models.TranscodingTask
	availableAt time.Time
}

// memoryDelivery is the in-memory queue's per-delivery handle. Each Dequeue
// returns a distinct handle bound to the popped item; Ack/Nak act only on the
// delivery they are given.
type memoryDelivery struct {
	q       *MemoryTranscodingQueue
	item    *memItem
	settled bool
}

func (d *memoryDelivery) TaskID() string { return d.item.task.ID }

// MemoryTranscodingQueue is the fallback queue used when JetStream is not
// available. It is a demo-profile implementation: delayed retries are honored
// in-process (bounded re-queue honoring the Nak delay) and are not a durable
// scheduler — process death loses queued messages.
type MemoryTranscodingQueue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	queue    []*memItem
	inflight map[*memoryDelivery]struct{}
	depth    int64
	maxDepth int
	// dlq keeps terminally failed payloads so a demo run leaves a trace of
	// what was dead-lettered (there is no broker DLQ stream to publish to).
	dlq [][]byte
}

// DefaultMemoryQueueMaxDepth bounds the in-memory queue so a burst of
// submissions cannot grow the process without limit.
const DefaultMemoryQueueMaxDepth = 100

// defaultMemoryDLQCap bounds the in-process DLQ trace.
const defaultMemoryDLQCap = 100

func NewMemoryTranscodingQueue() *MemoryTranscodingQueue {
	return NewMemoryTranscodingQueueWithLimit(DefaultMemoryQueueMaxDepth)
}

// NewMemoryTranscodingQueueWithLimit creates an in-memory queue that refuses
// to accept more than maxDepth pending tasks. A non-positive maxDepth means
// unbounded.
func NewMemoryTranscodingQueueWithLimit(maxDepth int) *MemoryTranscodingQueue {
	q := &MemoryTranscodingQueue{
		queue:    make([]*memItem, 0),
		inflight: make(map[*memoryDelivery]struct{}),
		maxDepth: maxDepth,
	}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *MemoryTranscodingQueue) Enqueue(task *models.TranscodingTask) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}
	return q.PublishOutbox(models.OutboxKindTask, task.ID, data)
}

func (q *MemoryTranscodingQueue) PublishOutbox(kind, _ string, payload []byte) error {
	switch kind {
	case models.OutboxKindTask:
		var task models.TranscodingTask
		if err := json.Unmarshal(payload, &task); err != nil {
			return fmt.Errorf("failed to unmarshal task payload: %w", err)
		}
		return q.push(&memItem{task: &task})
	case models.OutboxKindDLQ:
		q.mu.Lock()
		if len(q.dlq) >= defaultMemoryDLQCap {
			q.dlq = q.dlq[1:]
		}
		q.dlq = append(q.dlq, append([]byte(nil), payload...))
		q.mu.Unlock()
		return nil
	default:
		return fmt.Errorf("unknown outbox kind: %s", kind)
	}
}

// push inserts an item at the back of the queue. Re-queued retries (from Nak)
// bypass the depth cap: they are work that was already accepted.
func (q *MemoryTranscodingQueue) push(item *memItem) error {
	q.mu.Lock()
	if q.maxDepth > 0 && len(q.queue) >= q.maxDepth {
		depth := len(q.queue)
		q.mu.Unlock()
		return fmt.Errorf("%w: depth %d at capacity %d", ErrQueueFull, depth, q.maxDepth)
	}
	if item.task.Metadata == nil {
		item.task.Metadata = make(map[string]interface{})
	}
	q.queue = append(q.queue, item)
	q.depth = int64(len(q.queue))
	q.mu.Unlock()
	q.cond.Signal()
	return nil
}

func (q *MemoryTranscodingQueue) Dequeue(ctx context.Context) (*models.TranscodingTask, models.DeliveryHandle, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		now := time.Now()
		idx := -1
		var waitUntil time.Time
		for i, item := range q.queue {
			if !item.availableAt.After(now) {
				idx = i
				break
			}
			if waitUntil.IsZero() || item.availableAt.Before(waitUntil) {
				waitUntil = item.availableAt
			}
		}
		if idx >= 0 {
			item := q.queue[idx]
			q.queue = append(q.queue[:idx], q.queue[idx+1:]...)
			q.depth = int64(len(q.queue))
			d := &memoryDelivery{q: q, item: item}
			q.inflight[d] = struct{}{}
			taskCopy := *item.task
			return &taskCopy, d, nil
		}
		if len(q.queue) > 0 {
			// Every queued item is delayed (Nak backoff): wake when the
			// earliest becomes available instead of spinning.
			delay := time.Until(waitUntil)
			if delay <= 0 {
				delay = time.Millisecond
			}
			if delay > time.Second {
				delay = time.Second
			}
			timer := time.AfterFunc(delay, q.cond.Broadcast)
			q.cond.Wait()
			timer.Stop()
			continue
		}
		done := make(chan struct{}, 1)
		go func() {
			select {
			case <-ctx.Done():
				q.cond.Broadcast()
			case <-done:
			}
		}()
		q.cond.Wait()
		close(done)
	}
}

// settle validates the handle belongs to this queue and is not already
// settled, then removes it from the in-flight set.
func (q *MemoryTranscodingQueue) settle(h models.DeliveryHandle) (*memoryDelivery, error) {
	d, ok := h.(*memoryDelivery)
	if !ok || d == nil || d.q != q {
		return nil, fmt.Errorf("delivery handle does not belong to the in-memory queue")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, live := q.inflight[d]; !live || d.settled {
		return nil, fmt.Errorf("delivery for task %s is not in flight", d.TaskID())
	}
	delete(q.inflight, d)
	d.settled = true
	return d, nil
}

func (q *MemoryTranscodingQueue) Ack(h models.DeliveryHandle) error {
	_, err := q.settle(h)
	return err
}

// Nak re-queues the delivery after delay. The item keeps its identity, so the
// redelivery sees the same task (attempt counters live in the DB or in task
// metadata, never in the queue).
func (q *MemoryTranscodingQueue) Nak(h models.DeliveryHandle, delay time.Duration) error {
	d, err := q.settle(h)
	if err != nil {
		return err
	}
	if delay < 0 {
		delay = 0
	}
	d.item.availableAt = time.Now().Add(delay)

	q.mu.Lock()
	q.queue = append(q.queue, d.item)
	q.depth = int64(len(q.queue))
	q.mu.Unlock()
	q.cond.Signal()
	return nil
}

func (q *MemoryTranscodingQueue) InProgress(h models.DeliveryHandle) error {
	_, err := q.settleInProgress(h)
	return err
}

func (q *MemoryTranscodingQueue) settleInProgress(h models.DeliveryHandle) (*memoryDelivery, error) {
	d, ok := h.(*memoryDelivery)
	if !ok || d == nil || d.q != q {
		return nil, fmt.Errorf("delivery handle does not belong to the in-memory queue")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, live := q.inflight[d]; !live {
		return nil, fmt.Errorf("delivery for task %s is not in flight", d.TaskID())
	}
	return d, nil
}

func (q *MemoryTranscodingQueue) Depth() (int, error) {
	return int(atomic.LoadInt64(&q.depth)), nil
}
