package service

import (
	"fmt"
	"sync"

	"streamgate/pkg/models"
)

type MemoryTranscodingQueue struct {
	mu    sync.RWMutex
	queue []*models.TranscodingTask
	tasks map[string]*models.TranscodingTask
}

func NewMemoryTranscodingQueue() *MemoryTranscodingQueue {
	return &MemoryTranscodingQueue{
		queue: make([]*models.TranscodingTask, 0),
		tasks: make(map[string]*models.TranscodingTask),
	}
}

func (q *MemoryTranscodingQueue) Enqueue(task *models.TranscodingTask) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	taskCopy := *task
	if taskCopy.Metadata == nil {
		taskCopy.Metadata = make(map[string]interface{})
	}
	q.tasks[task.ID] = &taskCopy
	q.queue = append(q.queue, &taskCopy)
	return nil
}

func (q *MemoryTranscodingQueue) Dequeue() (*models.TranscodingTask, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.queue) == 0 {
		return nil, fmt.Errorf("queue empty")
	}

	task := q.queue[0]
	q.queue = q.queue[1:]
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
	q.mu.RLock()
	defer q.mu.RUnlock()

	task, ok := q.tasks[taskID]
	if !ok {
		return "", fmt.Errorf("task not found: %s", taskID)
	}

	return task.Status, nil
}