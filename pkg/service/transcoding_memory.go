package service

import (
	"fmt"
	"sync"
)

// MemoryTranscodingQueue is a simple in-memory implementation of TranscodingQueue.
type MemoryTranscodingQueue struct {
	mu    sync.RWMutex
	queue []*TranscodingTask
	tasks map[string]*TranscodingTask
}

// NewMemoryTranscodingQueue creates a new in-memory transcoding queue.
func NewMemoryTranscodingQueue() *MemoryTranscodingQueue {
	return &MemoryTranscodingQueue{
		queue: make([]*TranscodingTask, 0),
		tasks: make(map[string]*TranscodingTask),
	}
}

// Enqueue adds a task to the in-memory queue.
func (q *MemoryTranscodingQueue) Enqueue(task *TranscodingTask) error {
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

// Dequeue removes the next task from the in-memory queue.
func (q *MemoryTranscodingQueue) Dequeue() (*TranscodingTask, error) {
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

// GetStatus returns the current status of a task.
func (q *MemoryTranscodingQueue) GetStatus(taskID string) (string, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	task, ok := q.tasks[taskID]
	if !ok {
		return "", fmt.Errorf("task not found: %s", taskID)
	}

	return task.Status, nil
}
