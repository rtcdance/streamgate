package worker

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// JobStatus represents the status of a job
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusQueued    JobStatus = "queued"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

// JobPriority represents the priority of a job
type JobPriority int

const (
	JobPriorityLow    JobPriority = 1
	JobPriorityMedium JobPriority = 2
	JobPriorityHigh   JobPriority = 3
	JobPriorityUrgent JobPriority = 4
)

// Job represents a background job
type Job struct {
	ID          string
	Name        string
	Type        string
	Priority    JobPriority
	Status      JobStatus
	Payload     interface{}
	Result      interface{}
	Error       string
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	ScheduledAt *time.Time
	Timeout     time.Duration
	MaxRetries  int
	RetryCount  int
	WorkerID    string
	Progress    float64
	Metadata    map[string]interface{}
}

// NewJob creates a new job
func NewJob(jobType string, data map[string]interface{}) *Job {
	return &Job{
		Type:    jobType,
		Status:  JobStatusPending,
		Payload: data,
	}
}

// JobExecutor executes jobs
type JobExecutor interface {
	Execute(ctx context.Context, job *Job) (interface{}, error)
	CanExecute(jobType string) bool
}

// Worker represents a job worker
type Worker struct {
	ID              string
	Status          WorkerStatus
	CurrentJob      *Job
	CompletedJobs   int64
	FailedJobs      int64
	TotalProcessing time.Duration
	LastHeartbeat   time.Time
	mu              sync.RWMutex
}

// WorkerStatus represents the status of a worker
type WorkerStatus string

const (
	WorkerStatusIdle      WorkerStatus = "idle"
	WorkerStatusBusy      WorkerStatus = "busy"
	WorkerStatusUnhealthy WorkerStatus = "unhealthy"
)

// NewWorker creates a new worker
func NewWorker(id string, logger *zap.Logger) *Worker {
	return &Worker{
		ID:            id,
		Status:        WorkerStatusIdle,
		LastHeartbeat: time.Now(),
	}
}

// RecordJob records a job execution
func (w *Worker) RecordJob(duration time.Duration, success bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.TotalProcessing += duration
	w.LastHeartbeat = time.Now()

	if success {
		w.CompletedJobs++
	} else {
		w.FailedJobs++
	}
}

// GetStats returns worker statistics
func (w *Worker) GetStats() map[string]interface{} {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return map[string]interface{}{
		"id":               w.ID,
		"status":           w.Status,
		"completed_jobs":   w.CompletedJobs,
		"failed_jobs":      w.FailedJobs,
		"total_processing": w.TotalProcessing.String(),
		"last_heartbeat":   w.LastHeartbeat,
	}
}

// PriorityQueue implements a priority queue for jobs
type PriorityQueue struct {
	items []*Job
	mu    sync.Mutex
}

// NewPriorityQueue creates a new priority queue
func NewPriorityQueue(size int) *PriorityQueue {
	return &PriorityQueue{
		items: make([]*Job, 0, size),
	}
}

// Enqueue adds a job to the queue
func (pq *PriorityQueue) Enqueue(job *Job) error {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	heap.Push(pq, job)
	return nil
}

// Dequeue removes and returns the highest priority job
func (pq *PriorityQueue) Dequeue(ctx context.Context) (*Job, error) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	for len(pq.items) == 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			pq.mu.Unlock()
			time.Sleep(100 * time.Millisecond)
			pq.mu.Lock()
		}
	}

	return heap.Pop(pq).(*Job), nil
}

// Len returns the number of items in the queue
func (pq *PriorityQueue) Len() int {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return len(pq.items)
}

// Clear removes all items from the queue
func (pq *PriorityQueue) Clear() {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	pq.items = pq.items[:0]
}

// Peek returns the highest priority job without removing it
func (pq *PriorityQueue) Peek() (*Job, bool) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if len(pq.items) == 0 {
		return nil, false
	}

	return pq.items[0], true
}

// Remove removes a job from the queue
func (pq *PriorityQueue) Remove(jobID string) bool {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	for i, job := range pq.items {
		if job.ID == jobID {
			heap.Remove(pq, i)
			return true
		}
	}

	return false
}

// heap.Interface implementation

func (pq *PriorityQueue) Less(i, j int) bool {
	// Higher priority comes first
	return pq.items[i].Priority > pq.items[j].Priority ||
		(pq.items[i].Priority == pq.items[j].Priority &&
			pq.items[i].CreatedAt.Before(pq.items[j].CreatedAt))
}

func (pq *PriorityQueue) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
}

func (pq *PriorityQueue) Push(x interface{}) {
	item := x.(*Job)
	pq.items = append(pq.items, item)
}

func (pq *PriorityQueue) Pop() interface{} {
	old := pq.items
	n := len(old)
	item := old[n-1]
	pq.items = old[0 : n-1]
	return item
}

// JobExecutorFunc is a function that executes a job
type JobExecutorFunc func(ctx context.Context, job *Job) (interface{}, error)

// FuncExecutor wraps a function as a JobExecutor
type FuncExecutor struct {
	jobType string
	execute JobExecutorFunc
}

// NewFuncExecutor creates a new function executor
func NewFuncExecutor(jobType string, execute JobExecutorFunc) *FuncExecutor {
	return &FuncExecutor{
		jobType: jobType,
		execute: execute,
	}
}

// Execute executes the job
func (fe *FuncExecutor) Execute(ctx context.Context, job *Job) (interface{}, error) {
	return fe.execute(ctx, job)
}

// CanExecute checks if this executor can handle the job type
func (fe *FuncExecutor) CanExecute(jobType string) bool {
	return fe.jobType == jobType || fe.jobType == "*"
}

// MultiExecutor can execute multiple job types
type MultiExecutor struct {
	executors map[string]JobExecutor
}

// NewMultiExecutor creates a new multi-executor
func NewMultiExecutor() *MultiExecutor {
	return &MultiExecutor{
		executors: make(map[string]JobExecutor),
	}
}

// Register registers an executor for a job type
func (me *MultiExecutor) Register(jobType string, executor JobExecutor) {
	me.executors[jobType] = executor
}

// Execute executes a job
func (me *MultiExecutor) Execute(ctx context.Context, job *Job) (interface{}, error) {
	executor, exists := me.executors[job.Type]
	if !exists {
		return nil, fmt.Errorf("no executor found for job type: %s", job.Type)
	}

	return executor.Execute(ctx, job)
}

// CanExecute checks if this executor can handle the job type
func (me *MultiExecutor) CanExecute(jobType string) bool {
	_, exists := me.executors[jobType]
	return exists
}

// RetryPolicy defines retry behavior
type RetryPolicy struct {
	MaxRetries      int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	BackoffFactor   float64
	RetryableErrors map[string]bool
}

// DefaultRetryPolicy returns a default retry policy
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxRetries:    3,
		InitialDelay:  1 * time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		RetryableErrors: map[string]bool{
			"timeout":          true,
			"connection error": true,
			"temporary":        true,
		},
	}
}

// ShouldRetry determines if a job should be retried
func (rp *RetryPolicy) ShouldRetry(err error, retryCount int) bool {
	if err == nil {
		return false
	}

	if retryCount >= rp.MaxRetries {
		return false
	}

	errMsg := err.Error()
	for retryableErr := range rp.RetryableErrors {
		if contains(errMsg, retryableErr) {
			return true
		}
	}

	return false
}

// GetDelay calculates the delay before next retry
func (rp *RetryPolicy) GetDelay(retryCount int) time.Duration {
	delay := rp.InitialDelay

	for i := 0; i < retryCount; i++ {
		delay = time.Duration(float64(delay) * rp.BackoffFactor)
	}

	if delay > rp.MaxDelay {
		delay = rp.MaxDelay
	}

	return delay
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 1; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// JobContext provides context for job execution
type JobContext struct {
	Job      *Job
	Logger   *zap.Logger
	Metadata map[string]interface{}
}

// NewJobContext creates a new job context
func NewJobContext(job *Job, logger *zap.Logger) *JobContext {
	return &JobContext{
		Job:      job,
		Logger:   logger,
		Metadata: make(map[string]interface{}),
	}
}

// WithMetadata adds metadata to the context
func (jc *JobContext) WithMetadata(key string, value interface{}) *JobContext {
	jc.Metadata[key] = value
	return jc
}

// GetMetadata gets metadata from the context
func (jc *JobContext) GetMetadata(key string) (interface{}, bool) {
	val, ok := jc.Metadata[key]
	return val, ok
}
