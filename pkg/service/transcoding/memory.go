package transcoding

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/rtcdance/streamgate/pkg/models"
)

type MemoryTranscodingQueue struct {
	mu    sync.Mutex
	cond  *sync.Cond
	queue []*models.TranscodingTask
	tasks map[string]*models.TranscodingTask
	depth int64
}

func NewMemoryTranscodingQueue() *MemoryTranscodingQueue {
	q := &MemoryTranscodingQueue{
		queue: make([]*models.TranscodingTask, 0),
		tasks: make(map[string]*models.TranscodingTask),
	}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *MemoryTranscodingQueue) Enqueue(task *models.TranscodingTask) error {
	q.mu.Lock()
	taskCopy := *task
	if taskCopy.Metadata == nil {
		taskCopy.Metadata = make(map[string]interface{})
	}
	q.tasks[task.ID] = &taskCopy
	q.queue = append(q.queue, &taskCopy)
	q.depth = int64(len(q.queue))
	q.mu.Unlock()
	q.cond.Signal()
	return nil
}

func (q *MemoryTranscodingQueue) Dequeue(ctx context.Context) (*models.TranscodingTask, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for len(q.queue) == 0 {
		if ctx.Err() != nil {
			return nil, ctx.Err()
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

	task := q.queue[0]
	q.queue = q.queue[1:]
	q.depth = int64(len(q.queue))
	taskCopy := *task
	return &taskCopy, nil
}

func (q *MemoryTranscodingQueue) Ack(taskID string) error {
	return nil
}

func (q *MemoryTranscodingQueue) Nak(taskID string) error {
	return nil
}

func (q *MemoryTranscodingQueue) GetStatus(taskID string) (string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, ok := q.tasks[taskID]
	if !ok {
		return "", fmt.Errorf("task not found: %s", taskID)
	}

	return task.Status, nil
}

func (q *MemoryTranscodingQueue) Depth() (int, error) {
	return int(atomic.LoadInt64(&q.depth)), nil
}
