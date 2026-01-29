package transcoder

// TaskQueue manages transcoding tasks
type TaskQueue struct{}

// Enqueue enqueues a task
func (q *TaskQueue) Enqueue(task interface{}) error {
	return nil
}

// Dequeue dequeues a task
func (q *TaskQueue) Dequeue() (interface{}, error) {
	return nil, nil
}
