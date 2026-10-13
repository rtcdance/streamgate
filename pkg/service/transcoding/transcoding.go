package transcoding

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/rtcdance/streamgate/pkg/service/serviceerrors"
	"github.com/rtcdance/streamgate/pkg/storage"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// PostTranscodeHook is called after a transcoding task completes.
type PostTranscodeHook func(ctx context.Context, contentID, profile, outputURL string)

// TranscodingService handles transcoding operations
type TranscodingService struct {
	db         storage.DB
	queue      TranscodingQueue
	transcoder VideoTranscoder
	storage    SegmentStorage
	log        *zap.Logger
	mu         sync.RWMutex
	tasks      map[string]*TranscodingTask
	cancel     context.CancelFunc
	cancelMu   sync.Mutex
	httpClient *http.Client
	// maxInputBytes bounds the ORIGINAL media this service will accept. It is
	// enforced while fetching and while the object is staged, not by trusting a
	// declared length.
	maxInputBytes     int64
	workerCount       int
	uploadConcurrency int
	transcodeHooks    []PostTranscodeHook
	hookMu            sync.Mutex
	wg                sync.WaitGroup

	minWorkers     int
	maxWorkers     int
	currentWorkers int32
	running        int32
	extraCancels   []context.CancelFunc
	extraMu        sync.Mutex
	serviceCtx     context.Context
	serviceCancel  context.CancelFunc
	// queueLimit bounds pending tasks at submit time (covers NATS, whose
	// stream limits are only a backstop). 0 = no service-level check.
	queueLimit int
	// ownerTaskLimit bounds pending/running tasks per owner wallet so one
	// caller cannot monopolize the worker pool. 0 = no limit.
	ownerTaskLimit int

	// leaseOwner identifies this worker in transcoding_tasks.lease_owner.
	// Every writer that did not claim the row (or claims a newer generation)
	// is rejected by the lease predicates.
	leaseOwner string
	// leaseTTL is how long a claim stays valid without a heartbeat; judged by
	// DB time (NOW()), never by app clocks.
	leaseTTL time.Duration
	// heartbeatInterval is how often the running worker renews its lease.
	heartbeatInterval time.Duration
	// retryDelays[i] is the wait before attempt i+2 (2s then 4s by default).
	retryDelays [2]time.Duration
	// maxAttempts is the business cap including the first attempt; enforced
	// from the DB attempt counter, incremented only on a successful claim.
	maxAttempts int
	// outboxInterval is how often pending outbox intents are re-published.
	outboxInterval time.Duration
}

const defaultUploadConcurrency = 5

// DefaultMaxTranscodeInputBytes is the default ceiling on an ORIGINAL. A
// transcode input is untrusted media that gets written to disk and handed to
// FFmpeg, so the default is deliberately modest rather than "as large as the
// upload endpoint allows".
const DefaultMaxTranscodeInputBytes int64 = 100 << 20 // 100 MiB

// ErrInputTooLarge is returned when an input exceeds the configured ceiling. It
// is a distinct error so callers can answer 413 rather than a generic failure.
var ErrInputTooLarge = errors.New("transcode input exceeds the configured size limit")

// ErrQueueFull is returned when the pending-task queue is at capacity. Callers
// map it to a retryable saturation response (503 + Retry-After), never a
// generic 500.
var ErrQueueFull = errors.New("transcode queue is full")

// ErrOwnerTaskLimit is returned when an owner already has the maximum number
// of pending/running tasks. Callers map it to 429 + Retry-After.
var ErrOwnerTaskLimit = errors.New("too many pending transcode tasks for owner")

// WithMaxInputBytes sets the ceiling on an original. A non-positive value leaves
// the default in place: "no limit" is not an option this service offers.
func WithMaxInputBytes(n int64) TranscodingOption {
	return func(s *TranscodingService) {
		if n > 0 {
			s.maxInputBytes = n
		}
	}
}

// SetMaxInputBytes sets the ceiling on an original.
func (s *TranscodingService) SetMaxInputBytes(n int64) {
	if n > 0 {
		s.mu.Lock()
		s.maxInputBytes = n
		s.mu.Unlock()
	}
}

// MaxInputBytes reports the ceiling on an original.
func (s *TranscodingService) MaxInputBytes() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.maxInputBytes <= 0 {
		return DefaultMaxTranscodeInputBytes
	}
	return s.maxInputBytes
}

// NewTranscodingService creates a new transcoding service
func NewTranscodingService(db storage.DB, queue TranscodingQueue, opts ...TranscodingOption) *TranscodingService {
	svc := &TranscodingService{
		db:                db,
		queue:             queue,
		tasks:             make(map[string]*TranscodingTask),
		httpClient:        &http.Client{Timeout: 10 * time.Minute, Transport: &http.Transport{MaxIdleConns: 100, MaxIdleConnsPerHost: 20, IdleConnTimeout: 90 * time.Second}},
		leaseTTL:          defaultLeaseTTL,
		heartbeatInterval: defaultHeartbeatInterval,
		retryDelays:       [2]time.Duration{defaultRetrySecondDelay, defaultRetryThirdDelay},
		maxAttempts:       defaultMaxAttempts,
		outboxInterval:    defaultOutboxInterval,
	}
	svc.serviceCtx, svc.serviceCancel = context.WithCancel(context.Background())
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// TranscodingOption configures a TranscodingService
type TranscodingOption func(*TranscodingService)

// WithTranscoder sets the video transcoder
func WithTranscoder(t VideoTranscoder) TranscodingOption {
	return func(s *TranscodingService) { s.transcoder = t }
}

// WithStorage sets the object storage backend
func WithStorage(st SegmentStorage) TranscodingOption {
	return func(s *TranscodingService) { s.storage = st }
}

func WithLogger(l *zap.Logger) TranscodingOption {
	return func(s *TranscodingService) { s.log = l }
}

// RegisterPostTranscodeHook adds a hook that fires after a transcode completes.
func (s *TranscodingService) RegisterPostTranscodeHook(hook PostTranscodeHook) {
	s.hookMu.Lock()
	defer s.hookMu.Unlock()
	s.transcodeHooks = append(s.transcodeHooks, hook)
}

func WithUploadConcurrency(n int) TranscodingOption {
	return func(s *TranscodingService) {
		if n > 0 {
			s.uploadConcurrency = n
		}
	}
}

func WithWorkerCount(n int) TranscodingOption {
	return func(s *TranscodingService) {
		if n > 0 {
			s.workerCount = n
			if s.minWorkers <= 0 {
				s.minWorkers = n
			}
		}
	}
}

func WithMinWorkers(n int) TranscodingOption {
	return func(s *TranscodingService) {
		if n > 0 {
			s.minWorkers = n
		}
	}
}

func WithMaxWorkers(n int) TranscodingOption {
	return func(s *TranscodingService) {
		if n > 0 {
			s.maxWorkers = n
		}
	}
}

// WithQueueLimit bounds the number of pending tasks at submit time. A submit
// against a full queue returns ErrQueueFull. A non-positive value leaves the
// check disabled.
func WithQueueLimit(n int) TranscodingOption {
	return func(s *TranscodingService) {
		if n > 0 {
			s.queueLimit = n
		}
	}
}

// WithOwnerTaskLimit bounds pending/running tasks per owner wallet. Submitting
// past the limit returns ErrOwnerTaskLimit. A non-positive value disables it.
func WithOwnerTaskLimit(n int) TranscodingOption {
	return func(s *TranscodingService) {
		if n > 0 {
			s.ownerTaskLimit = n
		}
	}
}

const (
	// defaultMaxAttempts is the business retry cap including the first
	// attempt. It is enforced from the DB attempt counter (incremented only
	// on a successful claim), not by transport MaxDeliver.
	defaultMaxAttempts = 3
	// defaultRetrySecondDelay / defaultRetryThirdDelay back attempts 2 and 3.
	defaultRetrySecondDelay = 2 * time.Second
	defaultRetryThirdDelay  = 4 * time.Second
	// defaultLeaseTTL is the DB-side validity window of a claim; a heartbeat
	// renews it every defaultHeartbeatInterval. Both are judged with NOW().
	defaultLeaseTTL          = 30 * time.Second
	defaultHeartbeatInterval = 10 * time.Second
	// defaultOutboxInterval is how often pending outbox intents are retried.
	defaultOutboxInterval = 2 * time.Second
	// nakBusyDelay is used when another worker currently holds the lease or a
	// claim raced; the message comes back shortly and re-reads DB state.
	nakBusyDelay = 5 * time.Second
)

// WithLeaseOwner sets the worker identity written to lease_owner on claim.
// Used by tests for determinism; StartWorker generates one otherwise.
func WithLeaseOwner(owner string) TranscodingOption {
	return func(s *TranscodingService) {
		if owner != "" {
			s.leaseOwner = owner
		}
	}
}

// WithRetryDelays overrides the wait before the second and third attempt.
func WithRetryDelays(second, third time.Duration) TranscodingOption {
	return func(s *TranscodingService) {
		if second > 0 {
			s.retryDelays[0] = second
		}
		if third > 0 {
			s.retryDelays[1] = third
		}
	}
}

// StartWorker starts the background transcoding worker.
// It dequeues tasks and invokes the VideoTranscoder.
// Call StopWorker() to shut down.
func (s *TranscodingService) StartWorker(log *zap.Logger) {
	if s.transcoder == nil {
		if log != nil {
			log.Info("TranscodingService: no transcoder configured, worker not started")
		}
		return
	}
	if s.queue == nil {
		if log != nil {
			log.Info("TranscodingService: no queue configured, worker not started")
		}
		return
	}

	s.cancelMu.Lock()
	if atomic.LoadInt32(&s.running) == 1 {
		// Release lock before StopWorker (which acquires cancelMu internally),
		// then re-acquire for the new worker setup. Double-check running after
		// re-acquisition to handle concurrent StartWorker calls.
		s.cancelMu.Unlock()
		if log != nil {
			log.Info("TranscodingService: worker already running, stopping previous instance")
		}
		s.StopWorker()
		s.cancelMu.Lock()
		if atomic.LoadInt32(&s.running) == 1 {
			// Another StartWorker won the race between unlock and re-lock.
			s.cancelMu.Unlock()
			if log != nil {
				log.Info("TranscodingService: worker started by concurrent call, skipping")
			}
			return
		}
	}
	atomic.StoreInt32(&s.running, 1)

	ctx, cancel := context.WithCancel(context.Background())
	if s.cancel != nil {
		s.cancel()
	}
	s.cancel = cancel
	s.cancelMu.Unlock()

	if s.leaseOwner == "" {
		host, _ := os.Hostname()
		s.leaseOwner = fmt.Sprintf("%s-%d-%s", host, os.Getpid(), uuid.New().String()[:8])
	}

	initWorkers := s.minWorkers
	if initWorkers <= 0 {
		initWorkers = 1
	}
	if s.maxWorkers <= 0 {
		s.maxWorkers = max(initWorkers*4, 8)
	}
	if s.minWorkers <= 0 {
		s.minWorkers = initWorkers
	}

	atomic.StoreInt32(&s.currentWorkers, int32(initWorkers))

	s.recoverStuckTasks(log)

	for i := 0; i < initWorkers; i++ {
		s.wg.Add(1)
		s.startWorkerGoroutine(ctx, i, log)
	}

	s.wg.Add(1)
	s.startAutoScaler(ctx, log)

	if s.db != nil {
		s.wg.Add(1)
		go s.outboxPumpLoop(ctx, log)
	}
}

func (s *TranscodingService) workerLoop(ctx context.Context, workerID int, log *zap.Logger) {
	defer s.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			if log != nil {
				log.Info("TranscodingService: worker panic recovered", zap.Int("worker", workerID), zap.Any("panic", r))
			}
		}
	}()
	if log != nil {
		log.Info("TranscodingService: worker started", zap.Int("worker", workerID))
	}
	for {
		task, handle, err := s.queue.Dequeue(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				if log != nil {
					log.Info("TranscodingService: worker stopped", zap.Int("worker", workerID))
				}
				return
			}
			continue
		}
		if task == nil || handle == nil {
			continue
		}
		s.handleDelivery(ctx, task, handle, log)
	}
}

// handleDelivery drives one queue delivery to a settled outcome: Ack, a
// delayed Nak for retry/busy, or deliberately no settle when the lease was
// lost (the broker then redelivers to a new claimer). Postgres decides every
// branch — the delivery itself never carries authoritative state.
func (s *TranscodingService) handleDelivery(ctx context.Context, task *TranscodingTask, handle models.DeliveryHandle, log *zap.Logger) {
	if task == nil || handle == nil {
		return
	}
	if task.Metadata == nil {
		task.Metadata = make(map[string]interface{})
	}

	if s.db == nil {
		s.handleDeliveryNoDB(ctx, task, handle, log)
		return
	}

	state, err := s.readDelivery(ctx, task.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Row deleted: settle the message with no side effects.
			s.ack(handle, log)
			return
		}
		if log != nil {
			log.Error("failed to read delivery state", zap.String("task_id", task.ID), zap.Error(err))
		}
		s.nak(handle, nakBusyDelay, log)
		return
	}

	switch {
	case state.status == "completed" || state.status == "cancelled":
		// Redelivery of a settled task: Ack immediately, zero side effects.
		s.ack(handle, log)
		return
	case state.status == "failed":
		s.settleAlreadyFailed(ctx, task, handle, log)
		return
	case state.status == "processing" && state.leaseValid:
		// Another live worker holds the claim; come back after its lease.
		s.nak(handle, nakBusyDelay, log)
		return
	case state.attempt >= s.maxAttempts:
		// The attempt cap is already spent (e.g. the worker died after the
		// third claim): straight to the terminal failure path.
		s.settleTerminal(ctx, task, nil,
			fmt.Errorf("attempt cap exhausted after %d attempts", state.attempt), handle, log)
		return
	}

	lease, ok, err := s.claim(ctx, task.ID)
	if err != nil {
		if log != nil {
			log.Error("failed to claim task", zap.String("task_id", task.ID), zap.Error(err))
		}
		s.nak(handle, nakBusyDelay, log)
		return
	}
	if !ok {
		// The row moved under us (another claimer, or the cap moved); the
		// next delivery re-reads DB state.
		s.nak(handle, nakBusyDelay, log)
		return
	}
	s.executeClaimed(ctx, task, handle, lease, log)
}

// handleDeliveryNoDB is the demo-profile path used when no Postgres is
// configured: attempts live in task metadata, and retries ride on a delayed
// re-queue of the SAME message (no sleep-goroutine, no re-publish).
func (s *TranscodingService) handleDeliveryNoDB(ctx context.Context, task *TranscodingTask, handle models.DeliveryHandle, log *zap.Logger) {
	err := s.runProcessTask(ctx, task, log)
	if err == nil {
		s.ack(handle, log)
		return
	}
	attemptsUsed := getRetryCount(task) + 1
	if attemptsUsed >= s.maxAttempts {
		// processTask already recorded status=failed; dead-letter the payload.
		s.publishDLQ(task, log)
		s.ack(handle, log)
		return
	}
	setRetryCount(task, attemptsUsed)
	_ = s.updateTask(task.ID, func(t *TranscodingTask) {
		t.Status = "pending"
		t.Error = ""
	})
	s.nak(handle, s.retryDelayFor(attemptsUsed), log)
}

// executeClaimed runs a claimed task under a heartbeat and settles the
// delivery afterwards.
func (s *TranscodingService) executeClaimed(ctx context.Context, task *TranscodingTask, handle models.DeliveryHandle, lease *taskLease, log *zap.Logger) {
	taskCtx, cancel := context.WithCancel(ctx)
	leaseCtx := withTaskLease(taskCtx, lease)
	stopHeartbeat := s.startHeartbeat(taskCtx, handle, lease, cancel, log)

	err := s.runProcessTask(leaseCtx, task, log)

	stopHeartbeat()
	cancel()

	if err == nil {
		s.ack(handle, log)
		return
	}
	if s.isNonRetryable(err) || lease.attempt >= s.maxAttempts {
		s.settleTerminal(ctx, task, lease, err, handle, log)
		return
	}
	delay := s.retryDelayFor(lease.attempt)
	if relErr := s.releaseForRetry(ctx, lease, err.Error(), delay); relErr != nil {
		// Lease lost or the row moved on: settle nothing — redelivery goes to
		// whoever holds the claim next (or re-reads the terminal state).
		if log != nil {
			log.Warn("not scheduling retry", zap.String("task_id", task.ID), zap.Error(relErr))
		}
		return
	}
	s.nak(handle, delay, log)
}

// runProcessTask wraps processTask with panic recovery so one bad task never
// kills the worker loop.
func (s *TranscodingService) runProcessTask(ctx context.Context, task *TranscodingTask, log *zap.Logger) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if log != nil {
				log.Info("TranscodingService: task panic recovered, continuing loop",
					zap.String("task_id", task.ID), zap.Any("panic", r))
			}
			err = fmt.Errorf("panic during processing: %v", r)
			if taskLeaseFrom(ctx) == nil {
				if failErr := s.FailTask(context.Background(), task.ID, err.Error()); failErr != nil && log != nil {
					log.Error("Failed to mark panicked task as failed", zap.String("task_id", task.ID), zap.Error(failErr))
				}
			}
		}
	}()
	return s.processTask(ctx, task, log)
}

// retryDelayFor returns the backoff after the given attempt just failed:
// attempt 1 waits retryDelays[0] (2s), attempt 2 waits retryDelays[1] (4s).
func (s *TranscodingService) retryDelayFor(attempt int) time.Duration {
	if attempt <= 1 {
		return s.retryDelays[0]
	}
	return s.retryDelays[1]
}

// isNonRetryable marks failures where more attempts cannot help; they go
// straight to the terminal DLQ path.
func (s *TranscodingService) isNonRetryable(err error) bool {
	return errors.Is(err, ErrInputTooLarge)
}

func (s *TranscodingService) ack(handle models.DeliveryHandle, log *zap.Logger) {
	if s.queue == nil {
		return
	}
	if err := s.queue.Ack(handle); err != nil && log != nil {
		log.Error("failed to Ack delivery, it may be redelivered",
			zap.String("task_id", handle.TaskID()), zap.Error(err))
	}
}

func (s *TranscodingService) nak(handle models.DeliveryHandle, delay time.Duration, log *zap.Logger) {
	if s.queue == nil {
		return
	}
	if err := s.queue.Nak(handle, delay); err != nil && log != nil {
		log.Error("failed to Nak delivery, it may remain in-flight",
			zap.String("task_id", handle.TaskID()), zap.Error(err))
	}
}

// settleTerminal writes status=failed and the DLQ outbox intent in ONE
// transaction, publishes the intent, and only after the broker acknowledges
// the publish does it Ack the original message. If the publish fails the
// intent row stays pending (the outbox pump retries) and the message is Nacked
// so the DLQ-before-Ack rule holds on redelivery too.
func (s *TranscodingService) settleTerminal(ctx context.Context, task *TranscodingTask, lease *taskLease, cause error, handle models.DeliveryHandle, log *zap.Logger) {
	payload, merr := json.Marshal(task)
	if merr != nil {
		payload = []byte(fmt.Sprintf(`{"id":%q,"error":%q}`, task.ID, cause.Error()))
	}
	if s.db != nil {
		if err := s.terminalFailTx(ctx, task.ID, lease, cause.Error(), payload); err != nil {
			if log != nil {
				log.Error("terminal failure write failed", zap.String("task_id", task.ID), zap.Error(err))
			}
			s.nak(handle, nakBusyDelay, log)
			return
		}
		// Terminal: whatever this task staged never became visible; clean its
		// own generation only, best-effort.
		s.cleanupTaskStaging(ctx, task.ID)
	}
	if s.queue != nil {
		if err := s.queue.PublishOutbox(models.OutboxKindDLQ, task.ID, payload); err != nil {
			if log != nil {
				log.Error("DLQ publish failed, intent stays recoverable",
					zap.String("task_id", task.ID), zap.Error(err))
			}
			s.nak(handle, nakBusyDelay, log)
			return
		}
		if s.db != nil {
			if err := s.markOutboxDelivered(ctx, task.ID, models.OutboxKindDLQ); err != nil && log != nil {
				log.Warn("failed to mark DLQ intent delivered", zap.String("task_id", task.ID), zap.Error(err))
			}
		}
	}
	s.ack(handle, log)
}

// settleAlreadyFailed honors the DLQ-before-Ack rule when a failed task's
// message is redelivered: Ack only once the stored DLQ intent is published.
func (s *TranscodingService) settleAlreadyFailed(ctx context.Context, task *TranscodingTask, handle models.DeliveryHandle, log *zap.Logger) {
	if s.db == nil || s.queue == nil {
		s.ack(handle, log)
		return
	}
	payload, status, ok, err := s.readOutboxIntent(ctx, task.ID, models.OutboxKindDLQ)
	if err != nil {
		if log != nil {
			log.Error("failed to read DLQ intent", zap.String("task_id", task.ID), zap.Error(err))
		}
		s.nak(handle, nakBusyDelay, log)
		return
	}
	if !ok {
		// Legacy failed row without an intent: nothing to publish.
		s.ack(handle, log)
		return
	}
	if status != "delivered" {
		if err := s.queue.PublishOutbox(models.OutboxKindDLQ, task.ID, payload); err != nil {
			if log != nil {
				log.Error("DLQ publish failed on redelivery", zap.String("task_id", task.ID), zap.Error(err))
			}
			s.nak(handle, nakBusyDelay, log)
			return
		}
		if err := s.markOutboxDelivered(ctx, task.ID, models.OutboxKindDLQ); err != nil && log != nil {
			log.Warn("failed to mark DLQ intent delivered", zap.String("task_id", task.ID), zap.Error(err))
		}
	}
	s.ack(handle, log)
}

// publishDLQ dead-letters the task payload when no DB outbox exists (demo
// profile): the in-memory queue keeps a bounded trace, JetStream publishes to
// the DLQ stream.
func (s *TranscodingService) publishDLQ(task *TranscodingTask, log *zap.Logger) {
	if s.queue == nil {
		return
	}
	payload, err := json.Marshal(task)
	if err != nil {
		return
	}
	if err := s.queue.PublishOutbox(models.OutboxKindDLQ, task.ID, payload); err != nil && log != nil {
		log.Error("failed to publish DLQ payload", zap.String("task_id", task.ID), zap.Error(err))
	}
}

// startHeartbeat renews the DB lease every heartbeatInterval while the task
// runs and mirrors each renewal to the broker with InProgress (which extends
// AckWait). When the renewal reports the lease is gone, the task context is
// cancelled: the worker stops and deliberately does NOT Ack — redelivery goes
// to the new claimer.
func (s *TranscodingService) startHeartbeat(ctx context.Context, handle models.DeliveryHandle, lease *taskLease, onLost context.CancelFunc, log *zap.Logger) func() {
	stop := make(chan struct{})
	var once sync.Once
	done := func() { once.Do(func() { close(stop) }) }
	go func() {
		ticker := time.NewTicker(s.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				held, err := s.renewLease(ctx, lease)
				if err != nil {
					// Transient DB error: keep the lease attempt going on the
					// next tick instead of killing in-flight work.
					if log != nil {
						log.Warn("lease heartbeat failed", zap.String("task_id", lease.taskID), zap.Error(err))
					}
					continue
				}
				if !held {
					if log != nil {
						log.Warn("lease lost, cancelling task",
							zap.String("task_id", lease.taskID), zap.Int("generation", lease.generation))
					}
					onLost()
					return
				}
				if s.queue != nil && handle != nil {
					if err := s.queue.InProgress(handle); err != nil && log != nil {
						log.Debug("delivery InProgress failed", zap.String("task_id", lease.taskID), zap.Error(err))
					}
				}
			}
		}
	}()
	return done
}

// recoverStuckTasks resets rows whose lease expired — judged by DB time
// (NOW()), not by an app-side age heuristic. generation and attempt are
// preserved: recovery never counts as an attempt, and the broker's redelivery
// (or the outbox pump for an intent that was never published) re-claims with a
// fresh generation. Nothing is re-published here — with JetStream the message
// still exists and would only be duplicated.
func (s *TranscodingService) recoverStuckTasks(log *zap.Logger) {
	if s.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := s.db.Exec(ctx, `
		UPDATE transcoding_tasks
		SET status = 'pending', lease_owner = '', lease_expires_at = NULL
		WHERE status = 'processing' AND lease_expires_at IS NOT NULL AND lease_expires_at < NOW()`)
	if err != nil {
		if log != nil {
			log.Error("Failed to recover tasks with expired leases", zap.Error(err))
		}
		return
	}
	rows, _ := result.RowsAffected()
	if rows > 0 && log != nil {
		log.Info("Recovered tasks whose lease expired", zap.Int64("count", rows))
	}
}

// outboxPumpLoop re-publishes intents whose row is still pending: task
// intents whose publish never got a PubAck (including a crash between the
// commit and the publish), and DLQ intents whose publish failed.
func (s *TranscodingService) outboxPumpLoop(ctx context.Context, log *zap.Logger) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.outboxInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := s.pumpOutbox(ctx); err != nil {
				if log != nil {
					log.Debug("outbox pump pass failed", zap.Error(err))
				}
			} else if n > 0 && log != nil {
				log.Info("outbox pump published intents", zap.Int("count", n))
			}
		}
	}
}

// pumpOutbox publishes pending intents and marks each row delivered only after
// the broker acknowledged the publish. A publish failure leaves the row
// pending for the next pass — the intent is the durable retry state.
func (s *TranscodingService) pumpOutbox(ctx context.Context) (int, error) {
	if s.db == nil || s.queue == nil {
		return 0, nil
	}
	rows, err := s.db.Query(ctx,
		`SELECT id, kind, payload FROM outbox WHERE status = 'pending' ORDER BY created_at LIMIT 25`)
	if err != nil {
		return 0, fmt.Errorf("query outbox: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type intent struct {
		id      string
		kind    string
		payload []byte
	}
	var pending []intent
	for rows.Next() {
		var in intent
		if err := rows.Scan(&in.id, &in.kind, &in.payload); err != nil {
			return 0, fmt.Errorf("scan outbox: %w", err)
		}
		pending = append(pending, in)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	published := 0
	for _, in := range pending {
		if err := s.queue.PublishOutbox(in.kind, in.id, in.payload); err != nil {
			s.noteOutboxError(ctx, in.id, in.kind, err)
			continue
		}
		// Mark delivered only after PubAck. If this write fails the row stays
		// pending and the next pass republishes; the message ID makes that a
		// broker-side no-op.
		if err := s.markOutboxDelivered(ctx, in.id, in.kind); err != nil {
			s.noteOutboxError(ctx, in.id, in.kind, err)
			continue
		}
		published++
	}
	return published, nil
}

// StopWorker stops the background worker and waits for all goroutines to exit.
func (s *TranscodingService) StopWorker() {
	s.cancelMu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.cancelMu.Unlock()
	if s.serviceCancel != nil {
		s.serviceCancel()
	}
	s.wg.Wait()
	atomic.StoreInt32(&s.running, 0)
	if s.httpClient != nil {
		if t, ok := s.httpClient.Transport.(*http.Transport); ok {
			t.CloseIdleConnections()
		}
	}
}

func (s *TranscodingService) Close() {
	s.cancelMu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.cancelMu.Unlock()
	if s.serviceCancel != nil {
		s.serviceCancel()
	}
	atomic.StoreInt32(&s.running, 0)

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if s.httpClient != nil {
			if t, ok := s.httpClient.Transport.(*http.Transport); ok {
				t.CloseIdleConnections()
			}
		}
	case <-time.After(30 * time.Second):
		if s.log != nil {
			s.log.Warn("TranscodingService: timed out waiting for goroutines to finish")
		}
		<-done
		if s.httpClient != nil {
			if t, ok := s.httpClient.Transport.(*http.Transport); ok {
				t.CloseIdleConnections()
			}
		}
	}
}

func (s *TranscodingService) startWorkerGoroutine(ctx context.Context, workerID int, log *zap.Logger) {
	go s.workerLoop(ctx, workerID, log)
}

const (
	scaleCheckInterval = 5 * time.Second
	tasksPerWorker     = 2
)

func (s *TranscodingService) startAutoScaler(parentCtx context.Context, log *zap.Logger) {
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(scaleCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-parentCtx.Done():
				return
			case <-ticker.C:
				s.adjustWorkerCount(parentCtx, log)
			}
		}
	}()
}

func (s *TranscodingService) adjustWorkerCount(parentCtx context.Context, log *zap.Logger) {
	depth, err := s.queue.Depth()
	if err != nil {
		return
	}
	monitoring.TranscodingQueueDepth.Set(float64(depth))

	// Count in-progress tasks — never kill workers that are actively transcoding
	s.mu.RLock()
	inProgress := 0
	for _, t := range s.tasks {
		if t.Status == "processing" {
			inProgress++
		}
	}
	s.mu.RUnlock()

	target := s.minWorkers
	if inProgress > target {
		target = inProgress
	}
	needed := (depth + tasksPerWorker - 1) / tasksPerWorker
	target += needed
	if target > s.maxWorkers {
		target = s.maxWorkers
	}
	if target < s.minWorkers {
		target = s.minWorkers
	}

	current := int(atomic.LoadInt32(&s.currentWorkers))

	if target > current {
		s.extraMu.Lock()
		for i := 0; i < target-current; i++ {
			workerID := current + i
			childCtx, cancel := context.WithCancel(parentCtx)
			s.extraCancels = append(s.extraCancels, cancel)
			s.wg.Add(1)
			s.startWorkerGoroutine(childCtx, workerID, log)
		}
		atomic.StoreInt32(&s.currentWorkers, int32(target))
		s.extraMu.Unlock()
		if log != nil {
			log.Info("TranscodingService: scaled up workers", zap.Int("from", current), zap.Int("to", target), zap.Int("queue_depth", depth))
		}
	} else if target < current {
		s.extraMu.Lock()
		remove := current - target
		// Never cancel workers that are actively transcoding
		if inProgress > 0 && remove > current-inProgress {
			remove = current - inProgress
		}
		if remove > len(s.extraCancels) {
			remove = len(s.extraCancels)
		}
		for i := 0; i < remove; i++ {
			s.extraCancels[len(s.extraCancels)-1]()
			s.extraCancels = s.extraCancels[:len(s.extraCancels)-1]
		}
		atomic.StoreInt32(&s.currentWorkers, int32(current-remove))
		s.extraMu.Unlock()
		if log != nil {
			log.Info("TranscodingService: scaled down workers", zap.Int("from", current), zap.Int("to", target), zap.Int("queue_depth", depth))
		}
	}
	monitoring.TranscodingWorkersActive.Set(float64(target))
}

// taskLease is the proof that this worker owns a claim: identity plus the
// generation returned by the claim. Every write that commits a result is
// guarded by lease_owner + generation so a worker whose lease expired (or who
// never claimed) cannot overwrite a newer claimer's outcome.
type taskLease struct {
	taskID     string
	owner      string
	generation int
	attempt    int
}

type leaseCtxKey struct{}

func withTaskLease(ctx context.Context, lease *taskLease) context.Context {
	return context.WithValue(ctx, leaseCtxKey{}, lease)
}

func taskLeaseFrom(ctx context.Context) *taskLease {
	lease, _ := ctx.Value(leaseCtxKey{}).(*taskLease)
	return lease
}

// deliveryState is the DB view of one task as seen by a delivery.
type deliveryState struct {
	status     string
	attempt    int
	leaseValid bool
}

// readDelivery reads status/attempt/lease-validity from Postgres. The
// lease-validity predicate is evaluated with NOW() so the answer uses DB time.
func (s *TranscodingService) readDelivery(ctx context.Context, taskID string) (*deliveryState, error) {
	var st deliveryState
	err := s.db.QueryRow(ctx, `
		SELECT status, attempt,
		       (lease_expires_at IS NOT NULL AND lease_expires_at > NOW())
		FROM transcoding_tasks
		WHERE id = $1`, taskID).Scan(&st.status, &st.attempt, &st.leaseValid)
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// claim atomically moves a task to processing under a fresh lease. A row is
// claimable when it is pending, or processing with an ALREADY EXPIRED lease
// (the previous claimer died and recovery/redelivery may arrive in any order);
// both expiry checks use DB time (NOW()). attempt is incremented only here,
// only when the conditional UPDATE actually wins, and only below the business
// cap — so a failed claim never burns an attempt.
func (s *TranscodingService) claim(ctx context.Context, taskID string) (*taskLease, bool, error) {
	var generation, attempt int
	err := s.db.QueryRow(ctx, `
		UPDATE transcoding_tasks
		SET status = 'processing', lease_owner = $2,
		    lease_expires_at = NOW() + ($3::double precision * INTERVAL '1 second'),
		    generation = generation + 1, attempt = attempt + 1,
		    next_attempt_at = NULL, started_at = NOW()
		WHERE id = $1 AND attempt < $4
		  AND (status = 'pending'
		       OR (status = 'processing' AND lease_expires_at IS NOT NULL AND lease_expires_at < NOW()))
		RETURNING generation, attempt`,
		taskID, s.leaseOwner, s.leaseTTL.Seconds(), s.maxAttempts,
	).Scan(&generation, &attempt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("claim task: %w", err)
	}
	return &taskLease{taskID: taskID, owner: s.leaseOwner, generation: generation, attempt: attempt}, true, nil
}

// renewLease extends the claim by leaseTTL. It only succeeds while this exact
// owner+generation still holds a processing row; 0 rows means the lease was
// lost and the worker must stop.
func (s *TranscodingService) renewLease(ctx context.Context, lease *taskLease) (bool, error) {
	result, err := s.db.Exec(ctx, `
		UPDATE transcoding_tasks
		SET lease_expires_at = NOW() + ($3::double precision * INTERVAL '1 second')
		WHERE id = $1 AND lease_owner = $2 AND generation = $4 AND status = 'processing'`,
		lease.taskID, lease.owner, s.leaseTTL.Seconds(), lease.generation)
	if err != nil {
		return false, fmt.Errorf("renew lease: %w", err)
	}
	rows, _ := result.RowsAffected()
	return rows > 0, nil
}

// errLeaseLost reports that a lease-guarded write matched no row: the claim
// was taken over (or the row moved on), so the caller must not settle.
var errLeaseLost = errors.New("task lease lost or state changed")

// releaseForRetry sends the task back to pending for another attempt while
// preserving attempt, last_error and next_attempt_at (DB time + delay).
func (s *TranscodingService) releaseForRetry(ctx context.Context, lease *taskLease, cause string, delay time.Duration) error {
	result, err := s.db.Exec(ctx, `
		UPDATE transcoding_tasks
		SET status = 'pending', last_error = $3,
		    next_attempt_at = NOW() + ($4::double precision * INTERVAL '1 second'),
		    lease_owner = '', lease_expires_at = NULL
		WHERE id = $1 AND status = 'processing' AND lease_owner = $2 AND generation = $5`,
		lease.taskID, lease.owner, cause, delay.Seconds(), lease.generation)
	if err != nil {
		return fmt.Errorf("release task for retry: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return errLeaseLost
	}
	return nil
}

// terminalFailTx writes status=failed AND the DLQ outbox intent in the same
// transaction: a terminal failure without its DLQ intent cannot be committed.
func (s *TranscodingService) terminalFailTx(ctx context.Context, taskID string, lease *taskLease, cause string, payload []byte) error {
	return s.db.InTransaction(ctx, func(tx *sql.Tx) error {
		args := []interface{}{taskID, cause}
		guard := ""
		if lease != nil {
			guard = fmt.Sprintf(" AND lease_owner = $%d AND generation = $%d", len(args)+1, len(args)+2)
			args = append(args, lease.owner, lease.generation)
		}
		result, err := tx.ExecContext(ctx,
			"UPDATE transcoding_tasks SET status = 'failed', error = $2, last_error = $2, completed_at = NOW() WHERE id = $1 AND status IN ('pending', 'processing')"+guard,
			args...)
		if err != nil {
			return fmt.Errorf("failed to mark task failed: %w", err)
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return fmt.Errorf("task %s is not terminally failable (state or lease changed)", taskID)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO outbox (id, kind, payload, status) VALUES ($1, $2, $3, 'pending') ON CONFLICT (kind, id) DO NOTHING",
			taskID, models.OutboxKindDLQ, payload); err != nil {
			return fmt.Errorf("failed to record DLQ intent: %w", err)
		}
		return nil
	})
}

// markOutboxDelivered flips an intent to delivered. Callers must invoke it
// only after the broker acknowledged the publish.
func (s *TranscodingService) markOutboxDelivered(ctx context.Context, id, kind string) error {
	_, err := s.db.Exec(ctx,
		"UPDATE outbox SET status = 'delivered', delivered_at = NOW(), last_error = '' WHERE id = $1 AND kind = $2 AND status = 'pending'",
		id, kind)
	return err
}

func (s *TranscodingService) noteOutboxError(ctx context.Context, id, kind string, cause error) {
	_, err := s.db.Exec(ctx,
		"UPDATE outbox SET attempts = attempts + 1, last_error = $3 WHERE id = $1 AND kind = $2",
		id, kind, cause.Error())
	if err != nil && s.log != nil {
		s.log.Warn("failed to record outbox publish error", zap.String("id", id), zap.Error(err))
	}
}

func (s *TranscodingService) readOutboxIntent(ctx context.Context, id, kind string) ([]byte, string, bool, error) {
	var payload []byte
	var status string
	err := s.db.QueryRow(ctx,
		"SELECT payload, status FROM outbox WHERE id = $1 AND kind = $2", id, kind).
		Scan(&payload, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	return payload, status, true, nil
}

// processTask executes a single transcoding task under the caller's context.
// When the context carries a lease the claim was already taken by
// handleDelivery; any failure is returned so the worker decides between a
// retry release and the terminal DLQ path.
//
//nolint:gocyclo // processTask has complex step logic, refactoring would be too invasive
func (s *TranscodingService) processTask(ctx context.Context, task *TranscodingTask, log *zap.Logger) error {
	task.Status = "processing"
	now := time.Now()
	task.StartedAt = &now
	s.storeTask(task)

	taskCtx, cancel := context.WithTimeout(ctx, 60*time.Minute)
	defer cancel()

	// Create temp dir for output
	outputDir, err := os.MkdirTemp("", "streamgate-transcode-*")
	if err != nil {
		return s.recordProcessFailure(taskCtx, task, fmt.Errorf("failed to create temp dir: %w", err))
	}
	// Cleanup on any exit: remove partial outputs if task didn't complete
	cleanupOutput := true
	defer func() {
		if cleanupOutput {
			_ = os.RemoveAll(outputDir)
		}
	}()

	// Build profile string from task profile name
	// "abr" maps to multi-resolution transcoding handled by FFmpegTranscoder
	profile := task.Profile
	if profile != "abr" {
		if _, ok := DefaultProfiles[profile]; !ok {
			profile = "720p"
		}
	}

	// Resolve input: download HTTP URLs to a local temp file so FFmpeg
	// always has a local path to work with. Skip for non-HTTP paths.
	inputPath := task.InputURL
	var downloadedFile string
	if strings.HasPrefix(inputPath, "http://") || strings.HasPrefix(inputPath, "https://") {
		local, err := s.downloadInputFile(taskCtx, inputPath)
		if err != nil {
			return s.recordProcessFailure(taskCtx, task, fmt.Errorf("failed to download input: %w", err))
		}
		downloadedFile = local
		inputPath = local
		defer func() { _ = os.Remove(downloadedFile) }()
	}

	lease := taskLeaseFrom(ctx)

	// Progress writes are lease-guarded: a worker whose lease expired must not
	// write over the claim that took over.
	var guardMeta, guardPlain string
	var guardMetaArgs, guardPlainArgs []interface{}
	if lease != nil {
		guardMeta = " AND lease_owner = $4 AND generation = $5"
		guardMetaArgs = []interface{}{lease.owner, lease.generation}
		guardPlain = " AND lease_owner = $3 AND generation = $4"
		guardPlainArgs = []interface{}{lease.owner, lease.generation}
	}

	var lastDBProgress int
	err = s.transcoder.TranscodeHLS(taskCtx, inputPath, outputDir, profile, func(variant string, progress float64) {
		if variant != "" {
			if task.Metadata == nil {
				task.Metadata = make(map[string]interface{})
			}
			vp, _ := task.Metadata["variant_progress"].(map[string]interface{})
			if vp == nil {
				vp = make(map[string]interface{})
				task.Metadata["variant_progress"] = vp
			}
			vp[variant] = int(progress)
			total := 0
			count := 0
			for _, v := range vp {
				if f, ok := v.(int); ok {
					total += f
					count++
				}
			}
			if count > 0 {
				task.Progress = total / count
			}
		} else {
			task.Progress = int(progress)
		}
		s.storeTask(task)
		if s.log != nil {
			s.log.Debug("transcode progress",
				zap.String("task_id", task.ID),
				zap.String("variant", variant),
				zap.Float64("progress_pct", progress),
				zap.Int("task_progress", task.Progress))
		}
		if s.db != nil && variant != "" {
			metadataJSON, _ := json.Marshal(task.Metadata)
			if _, err := s.db.Exec(taskCtx,
				"UPDATE transcoding_tasks SET progress = $2, metadata = $3 WHERE id = $1"+guardMeta,
				append([]interface{}{task.ID, task.Progress, metadataJSON}, guardMetaArgs...)...); err != nil && s.log != nil {
				s.log.Warn("failed to update variant progress in DB", zap.String("task_id", task.ID), zap.Error(err))
			}
		} else if s.db != nil && task.Progress-lastDBProgress >= 5 {
			lastDBProgress = task.Progress
			if _, err := s.db.Exec(taskCtx,
				"UPDATE transcoding_tasks SET progress = $2 WHERE id = $1"+guardPlain,
				append([]interface{}{task.ID, task.Progress}, guardPlainArgs...)...); err != nil && s.log != nil {
				s.log.Warn("failed to update progress in DB", zap.String("task_id", task.ID), zap.Error(err))
			}
		}
	})

	if err != nil {
		if log != nil {
			log.Info("TranscodingService: task failed", zap.String("task_id", task.ID), zap.String("error", err.Error()))
		}
		return s.recordProcessFailure(taskCtx, task, err)
	}

	// Upload segments to object storage under this task's generation-private
	// staging prefix. Nothing is visible to the player until the generation is
	// published inside the lease-guarded completion transaction.
	if s.storage != nil {
		generation, genErr := s.ensureOutputGeneration(taskCtx, task)
		if genErr != nil {
			return s.recordProcessFailure(taskCtx, task, fmt.Errorf("failed to allocate output generation: %w", genErr))
		}
		if err := s.uploadSegments(taskCtx, outputDir, task.ContentID, task.Profile, generation); err != nil {
			return s.recordProcessFailure(taskCtx, task, fmt.Errorf("failed to upload segments: %w", err))
		}
		if err := s.validateGenerationArtifacts(taskCtx, task.ContentID, generation); err != nil {
			return s.recordProcessFailure(taskCtx, task, err)
		}

		s.extractAndUploadThumbnail(taskCtx, inputPath, task.ContentID)
	}

	// Mark complete — output is safely in object storage, skip cleanup
	cleanupOutput = false
	outputURL := fmt.Sprintf("streams/%s/%s", task.ContentID, task.Profile)

	// Finalize variant_progress to 100% (FFmpeg progress caps at ~99.x%)
	if task.Metadata != nil {
		if vp, ok := task.Metadata["variant_progress"].(map[string]interface{}); ok {
			for k := range vp {
				vp[k] = 100
			}
			task.Metadata["variant_progress"] = vp
			s.storeTask(task)
			if s.db != nil {
				metadataJSON, _ := json.Marshal(task.Metadata)
				if _, err := s.db.Exec(taskCtx,
					"UPDATE transcoding_tasks SET metadata = $2 WHERE id = $1"+guardPlain,
					append([]interface{}{task.ID, metadataJSON}, guardPlainArgs...)...); err != nil && s.log != nil {
					s.log.Warn("failed to finalize variant progress in DB", zap.String("task_id", task.ID), zap.Error(err))
				}
			}
		}
	}

	if err := s.CompleteTask(taskCtx, task.ID, outputURL); err != nil {
		if s.log != nil {
			s.log.Error("Failed to mark task as completed in DB", zap.String("task_id", task.ID), zap.Error(err))
		}
		return err
	}

	s.hookMu.Lock()
	hooks := make([]PostTranscodeHook, len(s.transcodeHooks))
	copy(hooks, s.transcodeHooks)
	s.hookMu.Unlock()

	for _, hook := range hooks {
		hook(taskCtx, task.ContentID, task.Profile, outputURL)
	}

	if log != nil {
		log.Info("TranscodingService: task completed", zap.String("task_id", task.ID))
	}

	// Best-effort local cleanup (output is already uploaded)
	_ = os.RemoveAll(outputDir)
	return nil
}

// recordProcessFailure persists a pre-completion failure when no worker lease
// is involved (direct calls, demo mode). With a lease the worker decides
// between a retry release and the terminal DLQ path, so nothing is written
// here — the cause is returned instead.
func (s *TranscodingService) recordProcessFailure(ctx context.Context, task *TranscodingTask, cause error) error {
	if taskLeaseFrom(ctx) == nil {
		if failErr := s.FailTask(ctx, task.ID, cause.Error()); failErr != nil && s.log != nil {
			s.log.Error("Failed to mark task as failed", zap.String("task_id", task.ID), zap.Error(failErr))
		}
	}
	return cause
}

// ensureOutputGeneration returns the artifact generation this task writes to,
// allocating max(output_generation)+1 for the content the first time it is
// needed. The allocation is persisted, so every retry of the SAME task reuses
// its staging prefix instead of leaking a new one per attempt, while a new task
// for the same content always gets a fresh generation — identical input with a
// different profile or transcoder version therefore lands at a different
// artifact identity. The contents row is locked for the read-modify-write so
// two concurrent tasks of one content cannot allocate the same generation.
// Without a DB (demo profile) there is no publish state to switch, so the
// legacy flat layout (generation 0) is kept.
func (s *TranscodingService) ensureOutputGeneration(ctx context.Context, task *TranscodingTask) (int64, error) {
	if s.db == nil {
		task.OutputGeneration = 0
		return 0, nil
	}
	var generation int64
	err := s.db.QueryRow(ctx, "SELECT output_generation FROM transcoding_tasks WHERE id = $1", task.ID).Scan(&generation)
	if err == nil && generation > 0 {
		task.OutputGeneration = generation
		return generation, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("read output generation: %w", err)
	}

	allocErr := s.db.InTransaction(ctx, func(tx *sql.Tx) error {
		// Lock the content row so concurrent tasks of the same content
		// serialize their max+1 allocation. A missing row (content deleted
		// mid-flight) just means there is nothing to publish later, so the
		// allocation proceeds without the lock.
		var lockID string
		if lerr := tx.QueryRowContext(ctx, "SELECT id FROM contents WHERE id = $1 FOR UPDATE", task.ContentID).Scan(&lockID); lerr != nil && !errors.Is(lerr, sql.ErrNoRows) {
			return fmt.Errorf("lock content for generation allocation: %w", lerr)
		}
		var next int64
		if merr := tx.QueryRowContext(ctx,
			"SELECT COALESCE(MAX(output_generation), 0) FROM transcoding_tasks WHERE content_id = $1",
			task.ContentID).Scan(&next); merr != nil {
			return fmt.Errorf("read max output generation: %w", merr)
		}
		next++
		if uerr := tx.QueryRowContext(ctx,
			"UPDATE transcoding_tasks SET output_generation = $2 WHERE id = $1 AND output_generation = 0 RETURNING output_generation",
			task.ID, next).Scan(&generation); uerr != nil {
			if errors.Is(uerr, sql.ErrNoRows) {
				// Already allocated by an earlier attempt: re-read below.
				return nil
			}
			return fmt.Errorf("allocate output generation: %w", uerr)
		}
		return nil
	})
	if allocErr != nil {
		return 0, allocErr
	}
	if generation < 1 {
		if rerr := s.db.QueryRow(ctx, "SELECT output_generation FROM transcoding_tasks WHERE id = $1", task.ID).Scan(&generation); rerr != nil {
			return 0, fmt.Errorf("re-read output generation: %w", rerr)
		}
	}
	task.OutputGeneration = generation
	return generation, nil
}

// validateGenerationArtifacts verifies that a fully uploaded generation holds
// at least one playlist and one segment before it may ever be published. The
// check reads the store rather than the upload bookkeeping, so a silently
// dropped object or a partial upload fails the task instead of becoming
// visible. Generation 0 (legacy flat / demo) has no staging prefix and no
// publish switch, so there is nothing to validate.
func (s *TranscodingService) validateGenerationArtifacts(ctx context.Context, contentID string, generation int64) error {
	if generation < 1 {
		return nil
	}
	keys, err := s.storage.ListObjects(ctx, "streamgate", fmt.Sprintf("streams/%s/g%d/", contentID, generation))
	if err != nil {
		return fmt.Errorf("failed to verify generation %d artifacts: %w", generation, err)
	}
	var playlists, segments int
	for _, key := range keys {
		switch {
		case strings.HasSuffix(key, ".m3u8"):
			playlists++
		case strings.HasSuffix(key, ".ts"):
			segments++
		}
	}
	if playlists == 0 || segments == 0 {
		return fmt.Errorf("generation %d incomplete: %d playlists and %d segments listed, at least one of each required",
			generation, playlists, segments)
	}
	return nil
}

// isGenerationDirName reports whether a key segment is a generation-private
// staging directory ("g" followed by digits). Quality subdirectories are
// resolutions or profile names and never take this shape.
func isGenerationDirName(segment string) bool {
	if len(segment) < 2 || segment[0] != 'g' {
		return false
	}
	for i := 1; i < len(segment); i++ {
		if segment[i] < '0' || segment[i] > '9' {
			return false
		}
	}
	return true
}

// cleanupGenerationObjects deletes the artifacts of ONE generation for a
// content: everything under streams/<cid>/g<N>/ when generation >= 1, or the
// legacy flat objects (every first segment that is not a generation directory)
// when generation == 0. Only this generation is ever touched — a failed or
// unpublished task cleans its own staging and never the published generation.
// Cleanup is best-effort: failures are logged and swallowed.
func (s *TranscodingService) cleanupGenerationObjects(ctx context.Context, contentID string, generation int64) {
	if s.storage == nil || contentID == "" {
		return
	}
	const bucket = "streamgate"
	prefix := fmt.Sprintf("streams/%s/", contentID)
	keys, err := s.storage.ListObjects(ctx, bucket, prefix)
	if err != nil {
		if s.log != nil {
			s.log.Warn("failed to list objects for generation cleanup",
				zap.String("content_id", contentID), zap.Int64("generation", generation), zap.Error(err))
		}
		return
	}
	var removed int
	for _, key := range keys {
		rel := strings.TrimPrefix(key, prefix)
		first := rel
		if idx := strings.IndexByte(rel, '/'); idx >= 0 {
			first = rel[:idx]
		}
		match := false
		if generation >= 1 {
			match = strings.HasPrefix(key, fmt.Sprintf("%sg%d/", prefix, generation))
		} else {
			match = !isGenerationDirName(first)
		}
		if !match {
			continue
		}
		if derr := s.storage.Delete(ctx, bucket, key); derr != nil {
			if s.log != nil {
				s.log.Warn("failed to delete object during generation cleanup",
					zap.String("key", key), zap.Error(derr))
			}
			continue
		}
		removed++
	}
	if removed > 0 && s.log != nil {
		s.log.Info("cleaned generation objects",
			zap.String("content_id", contentID), zap.Int64("generation", generation), zap.Int("deleted", removed))
	}
}

// cleanupTaskStaging removes the staging prefix of a task that reached a
// terminal failed state. It reads content and generation from Postgres so the
// cleanup works for every terminal path, including redeliveries whose queue
// payload predates the allocation. No-op when the task never staged anything.
func (s *TranscodingService) cleanupTaskStaging(ctx context.Context, taskID string) {
	if s.db == nil || s.storage == nil {
		return
	}
	var contentID string
	var generation int64
	if err := s.db.QueryRow(ctx,
		"SELECT content_id, output_generation FROM transcoding_tasks WHERE id = $1",
		taskID).Scan(&contentID, &generation); err != nil || generation < 1 || contentID == "" {
		return
	}
	s.cleanupGenerationObjects(ctx, contentID, generation)
}

// uploadSegments walks the output directory and uploads all files to MinIO.
// generation >= 1 routes every object under the task's private staging prefix
// streams/<cid>/g<generation>/; generation 0 keeps the legacy flat layout.
func (s *TranscodingService) uploadSegments(ctx context.Context, outputDir, contentID, profile string, generation int64) error {
	bucket := "streamgate"

	type uploadJob struct {
		path        string
		objectKey   string
		contentType string
	}

	abrMode := profile == "abr"
	basePrefix := fmt.Sprintf("streams/%s", contentID)
	if generation >= 1 {
		basePrefix = fmt.Sprintf("streams/%s/g%d", contentID, generation)
	}

	var jobs []uploadJob
	err := filepath.WalkDir(outputDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		relPath, err := filepath.Rel(outputDir, path)
		if err != nil {
			return err
		}
		// In ABR mode, organize files by resolution extracted from filename.
		// FFmpegTranscoder outputs files named {resolution}.m3u8 and {resolution}_{seq}.ts
		// (e.g. 1280x720.m3u8, 1280x720_000.ts, master.m3u8).
		subdir := profile
		if abrMode {
			subdir = extractResolutionPrefix(relPath)
		}
		objectKey := fmt.Sprintf("%s/%s/%s", basePrefix, subdir, relPath)

		contentType := "application/octet-stream"
		if strings.HasSuffix(path, ".m3u8") {
			contentType = "application/vnd.apple.mpegurl"
		} else if strings.HasSuffix(path, ".ts") {
			contentType = "video/mp2t"
		}

		jobs = append(jobs, uploadJob{path: path, objectKey: objectKey, contentType: contentType})
		return nil
	})
	if err != nil {
		return err
	}

	conc := s.uploadConcurrency
	if conc <= 0 {
		conc = defaultUploadConcurrency
	}
	sem := make(chan struct{}, conc)
	var uploadErr error
	var uploadMu sync.Mutex
	var uploadWg sync.WaitGroup

	uploadCtx, uploadCancel := context.WithCancel(ctx)
	defer uploadCancel()

	for _, job := range jobs {
		uploadWg.Add(1)
		go func(j uploadJob) {
			defer uploadWg.Done()
			select {
			case sem <- struct{}{}:
			case <-uploadCtx.Done():
				return
			}
			defer func() { <-sem }()

			f, err := os.Open(j.path)
			if err != nil {
				uploadMu.Lock()
				if uploadErr == nil {
					uploadErr = fmt.Errorf("open file %s: %w", j.path, err)
					uploadCancel()
				}
				uploadMu.Unlock()
				return
			}
			defer func() { _ = f.Close() }()

			fi, err := f.Stat()
			if err != nil {
				uploadMu.Lock()
				if uploadErr == nil {
					uploadErr = fmt.Errorf("stat file %s: %w", j.path, err)
					uploadCancel()
				}
				uploadMu.Unlock()
				return
			}

			if err := s.storage.UploadStreamWithContentType(ctx, bucket, j.objectKey, f, fi.Size(), j.contentType); err != nil {
				uploadMu.Lock()
				if uploadErr == nil {
					uploadErr = fmt.Errorf("upload %s: %w", j.objectKey, err)
					uploadCancel()
				}
				uploadMu.Unlock()
				return
			}
		}(job)
	}

	uploadWg.Wait()
	return uploadErr
}

// extractResolutionPrefix extracts the resolution subdirectory from an ABR
// output filename. FFmpegTranscoder outputs files like "1280x720_000.ts"
// or "1280x720.m3u8". The resolution prefix is used as the quality subdirectory.
// Returns the original filename unchanged for non-resolution files like "master.m3u8".
func extractResolutionPrefix(filename string) string {
	sep := strings.IndexAny(filename, "_.")
	if sep <= 0 {
		return filename
	}
	resolution := filename[:sep]
	if idx := strings.IndexByte(resolution, 'x'); idx <= 0 || idx == len(resolution)-1 {
		return filename
	}
	parts := strings.SplitN(resolution, "x", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return filename
	}
	for _, p := range parts {
		for _, c := range p {
			if c < '0' || c > '9' {
				return filename
			}
		}
	}
	return resolution
}

func (s *TranscodingService) extractAndUploadThumbnail(ctx context.Context, inputPath, contentID string) {
	thumbPath := filepath.Join(os.TempDir(), fmt.Sprintf("thumb_%s.jpg", contentID))
	defer func() { _ = os.Remove(thumbPath) }()

	args := []string{
		"-i", inputPath,
		"-vframes", "1",
		"-f", "image2",
		"-vf", "scale=320:-1",
		"-y", thumbPath,
	}
	// Args are an argv array — never shell-interpolated. The thumbnail ffmpeg
	// runs in its own process group so a cancelled ctx kills its children too.
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	configureProcGroup(cmd)
	cmd.Cancel = func() error { return killProcGroup(cmd) }
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Run(); err != nil {
		s.log.Warn("Thumbnail extraction failed", zap.String("content_id", contentID), zap.Error(err))
		return
	}

	data, err := os.ReadFile(thumbPath)
	if err != nil {
		s.log.Warn("Thumbnail read failed", zap.String("content_id", contentID), zap.Error(err))
		return
	}

	thumbnailKey := fmt.Sprintf("thumbnails/%s.jpg", contentID)
	if err := s.storage.UploadWithContentType(ctx, "streamgate", thumbnailKey, data, "image/jpeg"); err != nil {
		s.log.Warn("Thumbnail upload failed", zap.String("content_id", contentID), zap.Error(err))
	}
}

// downloadInputFile downloads an HTTP URL to a local temp file and returns
// the path. The caller is responsible for cleaning up the file.
func (s *TranscodingService) downloadInputFile(ctx context.Context, inputURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, inputURL, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download input: %w", err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download input returned status %d", resp.StatusCode)
	}

	// Validate Content-Type to prevent non-video files from being processed
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" {
		mediaType := strings.SplitN(contentType, ";", 2)[0]
		if !strings.HasPrefix(mediaType, "video/") && mediaType != "application/octet-stream" {
			return "", fmt.Errorf("unsupported content type: %s (expected video/* or application/octet-stream)", contentType)
		}
	}

	// A declared length larger than the ceiling is refused BEFORE reading, so an
	// honest server never causes us to buffer an oversized body.
	maxBytes := s.MaxInputBytes()
	if resp.ContentLength > maxBytes {
		return "", fmt.Errorf("%w: declared %d bytes, limit %d", ErrInputTooLarge, resp.ContentLength, maxBytes)
	}

	// inputURL may be a presigned URL with query params
	parsedURL, parseErr := url.Parse(inputURL)
	pathPart := inputURL
	if parseErr == nil {
		pathPart = parsedURL.Path
	}
	ext := filepath.Ext(pathPart)
	if ext == "" {
		ext = ".mp4"
	}
	f, err := os.CreateTemp("", "streamgate-input-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Read one byte PAST the ceiling: reaching it means the body is over the
	// limit, and the object is refused rather than silently truncated to the
	// first maxBytes. A chunked or lying Content-Length is caught here, which is
	// the case the declared-length check above cannot see.
	written, err := io.Copy(f, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("write temp file: %w", err)
	}
	if written > maxBytes {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("%w: read more than %d bytes", ErrInputTooLarge, maxBytes)
	}

	return f.Name(), nil
}

// TranscodingQueue defines the interface for task queue
// VideoTranscoder defines the interface for video transcoding operations.
// Implemented by FFmpegTranscoder in pkg/plugins/transcoder.
type VideoTranscoder interface {
	TranscodeHLS(ctx context.Context, inputPath, outputDir, profile string, progressFn func(variant string, progress float64)) error
}

// SegmentStorage defines the object storage operations needed by TranscodingService.
// This is a subset of storage.ObjectStorage to avoid import cycles.
// All methods accept a context.Context for timeout/cancellation propagation.
type SegmentStorage interface {
	Upload(ctx context.Context, bucket, objectName string, data []byte) error
	UploadStream(ctx context.Context, bucket, objectName string, reader io.Reader, size int64) error
	UploadWithContentType(ctx context.Context, bucket, objectName string, data []byte, contentType string) error
	UploadStreamWithContentType(ctx context.Context, bucket, objectName string, reader io.Reader, size int64, contentType string) error
	Download(ctx context.Context, bucket, objectName string) ([]byte, error)
	DownloadStream(ctx context.Context, bucket, objectName string) (io.ReadCloser, error)
	Delete(ctx context.Context, bucket, objectName string) error
	ListObjects(ctx context.Context, bucket, prefix string) ([]string, error)
	Exists(ctx context.Context, bucket, objectName string) (bool, error)
}

// TranscodingTask is an alias for models.TranscodingTask to avoid breaking callers.
type TranscodingTask = models.TranscodingTask

// TranscodingQueue is an alias for models.TranscodingQueue to avoid breaking callers.
type TranscodingQueue = models.TranscodingQueue

// TranscodingProfile represents a transcoding profile
type TranscodingProfile struct {
	Name       string `json:"name"`
	VideoCodec string `json:"video_codec"`
	AudioCodec string `json:"audio_codec"`
	Resolution string `json:"resolution"`
	Bitrate    int    `json:"bitrate"`
	Framerate  int    `json:"framerate"`
	Format     string `json:"format"`
}

// Predefined transcoding profiles
var DefaultProfiles = map[string]TranscodingProfile{
	"1080p": {
		Name:       "1080p",
		VideoCodec: "h264",
		AudioCodec: "aac",
		Resolution: "1920x1080",
		Bitrate:    5000,
		Framerate:  30,
		Format:     "hls",
	},
	"720p": {
		Name:       "720p",
		VideoCodec: "h264",
		AudioCodec: "aac",
		Resolution: "1280x720",
		Bitrate:    2500,
		Framerate:  30,
		Format:     "hls",
	},
	"480p": {
		Name:       "480p",
		VideoCodec: "h264",
		AudioCodec: "aac",
		Resolution: "854x480",
		Bitrate:    1000,
		Framerate:  30,
		Format:     "hls",
	},
	"360p": {
		Name:       "360p",
		VideoCodec: "h264",
		AudioCodec: "aac",
		Resolution: "640x360",
		Bitrate:    500,
		Framerate:  30,
		Format:     "hls",
	},
}

// Transcode creates a transcoding task
func (s *TranscodingService) Transcode(ctx context.Context, contentID, profile, inputURL string, priority int, ownerWallet string) (string, error) {
	// Validate profile
	if _, exists := DefaultProfiles[profile]; !exists && profile != "abr" {
		return "", fmt.Errorf("invalid profile: %s", profile)
	}

	// Saturation bounds are checked before anything is persisted so a refused
	// submit never leaves an orphan task behind.
	if s.ownerTaskLimit > 0 && ownerWallet != "" {
		count, err := s.countOwnerActiveTasks(ctx, ownerWallet)
		if err != nil {
			if s.log != nil {
				s.log.Warn("failed to count owner tasks, owner limit not enforced",
					zap.String("owner", ownerWallet), zap.Error(err))
			}
		} else if count >= s.ownerTaskLimit {
			return "", fmt.Errorf("%w: %d pending or running tasks already, limit %d",
				ErrOwnerTaskLimit, count, s.ownerTaskLimit)
		}
	}
	if s.queueLimit > 0 && s.queue != nil {
		depth, err := s.queue.Depth()
		if err == nil && depth >= s.queueLimit {
			return "", fmt.Errorf("%w: %d tasks pending, limit %d", ErrQueueFull, depth, s.queueLimit)
		}
	}

	// Generate task ID
	taskID := uuid.New().String()

	// Create task
	task := &TranscodingTask{
		ID:          taskID,
		ContentID:   contentID,
		Profile:     profile,
		Status:      "pending",
		Progress:    0,
		InputURL:    inputURL,
		Priority:    priority,
		OwnerWallet: ownerWallet,
		CreatedAt:   time.Now(),
		Metadata:    make(map[string]interface{}),
	}

	// Persist the task together with its enqueue intent in ONE transaction;
	// the message is published afterwards (outbox pattern), so a crash can
	// never leave a committed task without a publishable message.
	if s.db != nil {
		if err := s.saveTask(ctx, task); err != nil {
			if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
				s.log.Debug("transcode task already exists for content",
					zap.String("content_id", contentID),
					zap.String("profile", profile))
				existingID, findErr := s.findTaskByContentAndProfile(ctx, contentID, profile)
				if findErr == nil && existingID != "" {
					return existingID, nil
				}
				return "", nil
			}
			return "", fmt.Errorf("failed to save task: %w", err)
		}
		// Best-effort immediate publish; on failure the intent row stays
		// pending and the outbox pump retries. The publish returns only after
		// the broker acknowledged, and only then is the intent delivered.
		if s.queue != nil {
			if payload, merr := json.Marshal(task); merr == nil {
				s.publishIntent(ctx, task.ID, models.OutboxKindTask, payload)
			}
		}
	} else {
		s.storeTask(task)
		// Memory-only demo mode: there is no outbox table, enqueue directly.
		if s.queue != nil {
			if err := s.queue.Enqueue(task); err != nil {
				return "", fmt.Errorf("failed to enqueue task: %w", err)
			}
		}
	}

	return taskID, nil
}

// publishIntent publishes one outbox intent immediately. Success marks the row
// delivered (only after the broker acknowledged); failure leaves the row
// pending for the outbox pump.
func (s *TranscodingService) publishIntent(ctx context.Context, id, kind string, payload []byte) {
	if s.queue == nil {
		return
	}
	if err := s.queue.PublishOutbox(kind, id, payload); err != nil {
		s.noteOutboxError(ctx, id, kind, err)
		return
	}
	if err := s.markOutboxDelivered(ctx, id, kind); err != nil && s.log != nil {
		s.log.Warn("failed to mark outbox intent delivered", zap.String("id", id), zap.Error(err))
	}
}

// countOwnerActiveTasks counts an owner's pending or processing tasks, from
// Postgres when available, otherwise from the in-memory task map.
func (s *TranscodingService) countOwnerActiveTasks(ctx context.Context, ownerWallet string) (int, error) {
	if s.db != nil {
		var count int
		err := s.db.QueryRow(ctx,
			"SELECT COUNT(*) FROM transcoding_tasks WHERE owner_wallet = $1 AND status IN ('pending','processing')",
			ownerWallet).Scan(&count)
		if err != nil {
			return 0, fmt.Errorf("count owner tasks: %w", err)
		}
		return count, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, t := range s.tasks {
		if t.OwnerWallet == ownerWallet && (t.Status == "pending" || t.Status == "processing") {
			count++
		}
	}
	return count, nil
}

// GetTranscodingStatus gets transcoding task status
func (s *TranscodingService) GetTranscodingStatus(ctx context.Context, taskID string) (*TranscodingTask, error) {
	if s.db == nil {
		return s.getTask(taskID)
	}

	query := `
		SELECT id, content_id, profile, status, progress, input_url, output_url, 
		       error, priority, created_at, started_at, completed_at, metadata, owner_wallet
		FROM transcoding_tasks
		WHERE id = $1
	`

	var task TranscodingTask
	var startedAt, completedAt sql.NullTime
	var taskErr sql.NullString
	var metadataJSON []byte

	err := s.db.QueryRow(ctx, query, taskID).Scan(
		&task.ID,
		&task.ContentID,
		&task.Profile,
		&task.Status,
		&task.Progress,
		&task.InputURL,
		&task.OutputURL,
		&taskErr,
		&task.Priority,
		&task.CreatedAt,
		&startedAt,
		&completedAt,
		&metadataJSON,
		&task.OwnerWallet,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("task not found %s: %w", taskID, serviceerrors.ErrNotFound)
	} else if err != nil {
		return nil, fmt.Errorf("failed to query task: %w", err)
	}

	task.Error = taskErr.String

	// Handle nullable timestamps
	if startedAt.Valid {
		task.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		task.CompletedAt = &completedAt.Time
	}

	// Parse metadata from DB
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &task.Metadata); err != nil {
			return nil, fmt.Errorf("failed to parse metadata: %w", err)
		}
	}

	// Merge live variant_progress from the in-memory mirror. This is EPHEMERAL
	// progress only, never status: the status/progress columns above are read
	// from Postgres, the sole authority.
	if memTask, memErr := s.getTask(taskID); memErr == nil && memTask.Metadata != nil {
		if vp, ok := memTask.Metadata["variant_progress"]; ok {
			if task.Metadata == nil {
				task.Metadata = make(map[string]interface{})
			}
			task.Metadata["variant_progress"] = vp
			if task.Status == "processing" {
				task.Progress = memTask.Progress
			}
		}
	}

	return &task, nil
}

// UpdateTaskStatus updates task status
func (s *TranscodingService) UpdateTaskStatus(ctx context.Context, taskID, status string, progress int) error {
	if s.db == nil {
		return s.updateTask(taskID, func(task *TranscodingTask) {
			task.Status = status
			task.Progress = progress
		})
	}

	var currentStatus string
	if err := s.db.QueryRow(ctx, "SELECT status FROM transcoding_tasks WHERE id = $1", taskID).Scan(&currentStatus); err != nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	if !models.IsValidTaskTransition(models.TranscodingTaskStatus(currentStatus), models.TranscodingTaskStatus(status)) {
		return fmt.Errorf("invalid task status transition: %s -> %s", currentStatus, status)
	}

	query := "UPDATE transcoding_tasks SET status = $2, progress = $3 WHERE id = $1 AND status = $4"
	result, err := s.db.Exec(ctx, query, taskID, status, progress, currentStatus)
	if err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("task status changed concurrently, please retry")
	}
	return nil
}

// UpdateTaskProgress updates task progress
func (s *TranscodingService) UpdateTaskProgress(ctx context.Context, taskID string, progress int) error {
	if s.db == nil {
		return s.updateTask(taskID, func(task *TranscodingTask) {
			task.Progress = progress
		})
	}

	query := "UPDATE transcoding_tasks SET progress = $2 WHERE id = $1"
	_, err := s.db.Exec(ctx, query, taskID, progress)
	if err != nil {
		return fmt.Errorf("failed to update task progress: %w", err)
	}
	return nil
}

// StartTask marks a task as started
func (s *TranscodingService) StartTask(ctx context.Context, taskID string) error {
	if s.db == nil {
		return s.updateTask(taskID, func(task *TranscodingTask) {
			task.Status = "processing"
			now := time.Now()
			task.StartedAt = &now
		})
	}

	query := "UPDATE transcoding_tasks SET status = $2, started_at = $3 WHERE id = $1 AND status = 'pending'"
	result, err := s.db.Exec(ctx, query, taskID, "processing", time.Now())
	if err != nil {
		return fmt.Errorf("failed to start task: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("task not in pending state")
	}
	return nil
}

// CompleteTask marks a task as completed. When the task carries an output
// generation, the same lease-guarded transaction also performs THE publish:
// contents.published_generation is switched forward to the validated staging
// generation (never backward, never for a deleted content). Cleanup of the
// superseded or unpublished artifacts happens best-effort after the commit.
func (s *TranscodingService) CompleteTask(ctx context.Context, taskID, outputURL string) error {
	if s.db == nil {
		return s.updateTask(taskID, func(task *TranscodingTask) {
			task.Status = "completed"
			task.Progress = 100
			task.OutputURL = outputURL
			now := time.Now()
			task.CompletedAt = &now
			if task.Metadata != nil {
				if vp, ok := task.Metadata["variant_progress"].(map[string]interface{}); ok {
					for variant := range vp {
						vp[variant] = 100
					}
				}
			}
		})
	}

	completeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// The commit is lease-guarded when called from a worker: only the current
	// claimer (owner + generation) may complete the task.
	lease := taskLeaseFrom(ctx)

	var (
		contentID        string
		profile          string
		outputGeneration int64
		didPublish       bool
		prevGeneration   int64
		publishedNow     int64
		rowExists        bool
	)

	err := s.db.InTransaction(completeCtx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, "SELECT content_id, profile, output_generation FROM transcoding_tasks WHERE id = $1", taskID).
			Scan(&contentID, &profile, &outputGeneration); err != nil {
			return fmt.Errorf("failed to get task: %w", err)
		}

		var metadataJSON []byte
		if memTask, memErr := s.getTask(taskID); memErr == nil && memTask.Metadata != nil {
			finalMeta := make(map[string]interface{})
			for k, v := range memTask.Metadata {
				finalMeta[k] = v
			}
			if vp, ok := finalMeta["variant_progress"].(map[string]interface{}); ok {
				for variant := range vp {
					vp[variant] = 100
				}
			}
			metadataJSON, _ = json.Marshal(finalMeta)
		}

		query := "UPDATE transcoding_tasks SET status = 'completed', progress = 100, output_url = $2, completed_at = $3"
		args := []interface{}{taskID, outputURL, time.Now()}
		if metadataJSON != nil {
			query += ", metadata = $4"
			args = append(args, metadataJSON)
		}
		query += " WHERE id = $1 AND status = 'processing'"
		if lease != nil {
			next := len(args) + 1
			query += fmt.Sprintf(" AND lease_owner = $%d AND generation = $%d", next, next+1)
			args = append(args, lease.owner, lease.generation)
		}
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("failed to complete task: %w", err)
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return fmt.Errorf("task %s is not completable (state or lease changed)", taskID)
		}

		// THE publish: the staging generation becomes the served generation in
		// one row flip, inside the same transaction as the lease-guarded
		// completion. It is monotonically forward-only (a slower, older task
		// never republished over a newer generation) and conditional on the
		// content still existing — deleting the content makes this a no-op, so
		// an in-flight worker cannot resurrect it.
		if contentID != "" && outputGeneration >= 1 {
			pubErr := tx.QueryRowContext(ctx, `
				WITH prev AS (SELECT published_generation AS g FROM contents WHERE id = $1 FOR UPDATE)
				UPDATE contents c
				SET published_generation = $2, published_profile = $3, updated_at = NOW()
				FROM prev
				WHERE c.id = $1 AND prev.g < $2
				RETURNING prev.g`, contentID, outputGeneration, profile).Scan(&prevGeneration)
			switch {
			case pubErr == nil:
				didPublish = true
			case errors.Is(pubErr, sql.ErrNoRows):
				// No row to switch (content deleted) or already at/after this
				// generation: publish is a deliberate no-op.
			default:
				return fmt.Errorf("failed to publish generation: %w", pubErr)
			}
			// Read the post-switch state: it tells the caller which generation
			// is (still) published when the flip above was a no-op, and whether
			// the content row exists at all.
			switch perr := tx.QueryRowContext(ctx, "SELECT published_generation FROM contents WHERE id = $1", contentID).Scan(&publishedNow); {
			case perr == nil:
				rowExists = true
			case errors.Is(perr, sql.ErrNoRows):
				rowExists = false
			default:
				return fmt.Errorf("failed to read published generation: %w", perr)
			}
		}

		if contentID != "" {
			var pendingCount int
			var sourceURL string
			if err := tx.QueryRowContext(ctx,
				"SELECT COUNT(*) FROM transcoding_tasks WHERE content_id = $1 AND status IN ('pending', 'processing')",
				contentID).Scan(&pendingCount); err != nil {
				s.log.Warn("Failed to check pending tasks", zap.Error(err))
			}
			if err := tx.QueryRowContext(ctx, "SELECT url FROM contents WHERE id = $1", contentID).Scan(&sourceURL); err != nil {
				s.log.Warn("Failed to fetch content source URL for cleanup", zap.String("content_id", contentID), zap.Error(err))
			}
			if pendingCount == 0 {
				if _, err := tx.ExecContext(ctx,
					"UPDATE contents SET status = $2, updated_at = $3 WHERE id = $1",
					contentID, "ready", time.Now()); err != nil {
					return fmt.Errorf("failed to update content status: %w", err)
				}
				if s.storage != nil && sourceURL != "" {
					storageKey := strings.TrimPrefix(sourceURL, "/streamgate/")
					if storageKey != "" && storageKey != sourceURL {
						if err := s.storage.Delete(ctx, "streamgate", storageKey); err != nil {
							s.log.Warn("Failed to delete source file after all profiles completed",
								zap.String("content_id", contentID),
								zap.String("key", storageKey),
								zap.Error(err))
						}
					}
				}
			}
		}

		if contentID != "" && s.storage != nil {
			thumbnailKey := fmt.Sprintf("thumbnails/%s.jpg", contentID)
			thumbnailURL := fmt.Sprintf("/streamgate/%s", thumbnailKey)
			if _, err := tx.ExecContext(ctx,
				"UPDATE contents SET thumbnail_url = $2, updated_at = $3 WHERE id = $1",
				contentID, thumbnailURL, time.Now()); err != nil {
				s.log.Warn("Failed to update thumbnail URL", zap.Error(err))
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Post-commit, best-effort cleanup. Only ever this task's own artifacts or
	// the generation it superseded — never anything else.
	if outputGeneration >= 1 && s.storage != nil {
		if didPublish {
			// The superseded generation (0 = legacy flat layout) is no longer
			// resolvable by any player; drop it.
			s.cleanupGenerationObjects(ctx, contentID, prevGeneration)
		} else if !rowExists || publishedNow != outputGeneration {
			// Publish was a no-op: the content row is gone, or a newer
			// generation already superseded this one, so this generation was
			// never visible — drop it. When publishedNow == outputGeneration a
			// sibling task published this very generation; keeping it is what
			// the equality check above preserves.
			s.cleanupGenerationObjects(ctx, contentID, outputGeneration)
		}
	}
	return nil
}

// FailTask marks a task as failed
func (s *TranscodingService) FailTask(ctx context.Context, taskID, errorMsg string) error {
	if s.db == nil {
		return s.updateTask(taskID, func(task *TranscodingTask) {
			task.Status = "failed"
			task.Error = errorMsg
			now := time.Now()
			task.CompletedAt = &now
		})
	}

	failCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := "UPDATE transcoding_tasks SET status = $2, error = $3, completed_at = $4 WHERE id = $1 AND status IN ('pending', 'processing')"
	result, err := s.db.Exec(failCtx, query, taskID, "failed", errorMsg, time.Now())
	if err != nil {
		return fmt.Errorf("failed to fail task: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("task not in a failable state: %s", taskID)
	}
	// Terminal: whatever this task staged never became visible, so clean its
	// own generation only.
	s.cleanupTaskStaging(failCtx, taskID)
	return nil
}

// ListTasks lists transcoding tasks
func (s *TranscodingService) ListTasks(ctx context.Context, contentID, ownerWallet string, limit, offset int) ([]*TranscodingTask, error) {
	if s.db == nil {
		return s.listTasks(contentID, ownerWallet, limit, offset), nil
	}

	baseQuery := `
		SELECT id, content_id, profile, status, progress, input_url, output_url,
		       error, priority, created_at, started_at, completed_at, metadata, owner_wallet
		FROM transcoding_tasks
	`
	var conditions []string
	var args []interface{}
	argIdx := 1

	if contentID != "" {
		conditions = append(conditions, fmt.Sprintf("content_id = $%d", argIdx))
		args = append(args, contentID)
		argIdx++
	}
	if ownerWallet != "" {
		conditions = append(conditions, fmt.Sprintf("owner_wallet = $%d", argIdx))
		args = append(args, ownerWallet)
		argIdx++
	}

	query := baseQuery
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tasks := make([]*TranscodingTask, 0)
	for rows.Next() {
		var task TranscodingTask
		var startedAt, completedAt sql.NullTime
		var taskErr sql.NullString
		var metadataJSON []byte

		err := rows.Scan(
			&task.ID,
			&task.ContentID,
			&task.Profile,
			&task.Status,
			&task.Progress,
			&task.InputURL,
			&task.OutputURL,
			&taskErr,
			&task.Priority,
			&task.CreatedAt,
			&startedAt,
			&completedAt,
			&metadataJSON,
			&task.OwnerWallet,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}

		task.Error = taskErr.String

		// Handle nullable timestamps
		if startedAt.Valid {
			task.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			task.CompletedAt = &completedAt.Time
		}

		// Parse metadata
		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &task.Metadata); err != nil {
				return nil, fmt.Errorf("failed to parse metadata: %w", err)
			}
		}

		tasks = append(tasks, &task)
	}

	return tasks, nil
}

// CancelTask cancels a transcoding task
func (s *TranscodingService) CancelTask(ctx context.Context, taskID string) error {
	if s.db == nil {
		return s.updateTask(taskID, func(task *TranscodingTask) {
			if task.Status == "pending" || task.Status == "processing" {
				task.Status = "cancelled"
				now := time.Now()
				task.CompletedAt = &now
			}
		})
	}

	query := "UPDATE transcoding_tasks SET status = $2 WHERE id = $1 AND status IN ('pending', 'processing')"
	result, err := s.db.Exec(ctx, query, taskID, "cancelled")
	if err != nil {
		return fmt.Errorf("failed to cancel task: %w", err)
	}

	rowsAffected, errRA := result.RowsAffected()
	if errRA != nil {
		return errRA
	}
	if rowsAffected == 0 {
		return fmt.Errorf("task cannot be cancelled: %s", taskID)
	}

	return nil
}

// DeleteTask deletes a transcoding task
func (s *TranscodingService) DeleteTask(ctx context.Context, taskID string) error {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.tasks, taskID)
		return nil
	}

	query := "DELETE FROM transcoding_tasks WHERE id = $1"
	_, err := s.db.Exec(ctx, query, taskID)
	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}
	return nil
}

// GetProfile gets a transcoding profile
func (s *TranscodingService) GetProfile(name string) (*TranscodingProfile, error) {
	profile, exists := DefaultProfiles[name]
	if !exists {
		return nil, fmt.Errorf("profile not found: %s", name)
	}
	return &profile, nil
}

// ListProfiles lists all available transcoding profiles
func (s *TranscodingService) ListProfiles() []TranscodingProfile {
	profiles := make([]TranscodingProfile, 0, len(DefaultProfiles))
	for _, profile := range DefaultProfiles {
		profiles = append(profiles, profile)
	}
	return profiles
}

// saveTask writes the task row AND its enqueue intent in the same transaction:
// task creation and "there is a message to publish" are one atomic fact.
func (s *TranscodingService) saveTask(ctx context.Context, task *TranscodingTask) error {
	metadataJSON, err := json.Marshal(task.Metadata)
	if err != nil {
		return fmt.Errorf("failed to serialize metadata: %w", err)
	}
	var payload []byte
	if s.queue != nil {
		payload, err = json.Marshal(task)
		if err != nil {
			return fmt.Errorf("failed to serialize task payload: %w", err)
		}
	}

	return s.db.InTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO transcoding_tasks (id, content_id, profile, status, progress, input_url, 
			                              output_url, error, priority, created_at, metadata, owner_wallet)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			task.ID,
			task.ContentID,
			task.Profile,
			task.Status,
			task.Progress,
			task.InputURL,
			task.OutputURL,
			task.Error,
			task.Priority,
			task.CreatedAt,
			metadataJSON,
			task.OwnerWallet,
		); err != nil {
			return err
		}
		if s.queue != nil {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO outbox (id, kind, payload, status) VALUES ($1, $2, $3, 'pending') ON CONFLICT (kind, id) DO NOTHING",
				task.ID, models.OutboxKindTask, payload); err != nil {
				return fmt.Errorf("failed to record outbox intent: %w", err)
			}
		}
		return nil
	})
}

func (s *TranscodingService) findTaskByContentAndProfile(ctx context.Context, contentID, profile string) (string, error) {
	if s.db == nil {
		for _, t := range s.listTasks(contentID, "", 10, 0) {
			if t.Profile == profile {
				return t.ID, nil
			}
		}
		return "", fmt.Errorf("task not found in memory")
	}
	var taskID string
	err := s.db.QueryRow(ctx,
		`SELECT id FROM transcoding_tasks WHERE content_id = $1 AND profile = $2 ORDER BY created_at DESC LIMIT 1`,
		contentID, profile,
	).Scan(&taskID)
	if err != nil {
		return "", err
	}
	return taskID, nil
}

// GetPendingTasks gets all pending tasks
func (s *TranscodingService) GetPendingTasks(ctx context.Context, limit int) ([]*TranscodingTask, error) {
	if s.db == nil {
		tasks := s.listTasks("", "", limit, 0)
		pending := make([]*TranscodingTask, 0, len(tasks))
		for _, task := range tasks {
			if task.Status == "pending" {
				pending = append(pending, task)
			}
		}
		return pending, nil
	}

	query := `
		SELECT id, content_id, profile, status, progress, input_url, output_url,
		       error, priority, created_at, started_at, completed_at, metadata
		FROM transcoding_tasks
		WHERE status = 'pending'
		ORDER BY priority DESC, created_at ASC
		LIMIT $1
	`

	rows, err := s.db.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tasks := make([]*TranscodingTask, 0)
	for rows.Next() {
		var task TranscodingTask
		var startedAt, completedAt sql.NullTime
		var taskErr sql.NullString
		var metadataJSON []byte

		err := rows.Scan(
			&task.ID,
			&task.ContentID,
			&task.Profile,
			&task.Status,
			&task.Progress,
			&task.InputURL,
			&task.OutputURL,
			&taskErr,
			&task.Priority,
			&task.CreatedAt,
			&startedAt,
			&completedAt,
			&metadataJSON,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}

		task.Error = taskErr.String

		// Handle nullable timestamps
		if startedAt.Valid {
			task.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			task.CompletedAt = &completedAt.Time
		}

		// Parse metadata
		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &task.Metadata); err != nil {
				return nil, fmt.Errorf("failed to parse metadata: %w", err)
			}
		}

		tasks = append(tasks, &task)
	}

	return tasks, nil
}

func (s *TranscodingService) storeTask(task *TranscodingTask) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.tasks) > 10000 {
		now := time.Now()
		for id, t := range s.tasks {
			if (t.Status == "completed" || t.Status == "failed" || t.Status == "cancelled") &&
				t.CompletedAt != nil && now.Sub(*t.CompletedAt) > 1*time.Hour {
				delete(s.tasks, id)
			}
		}
	}

	taskCopy := *task
	if taskCopy.Metadata == nil {
		taskCopy.Metadata = make(map[string]interface{})
	}
	s.tasks[task.ID] = &taskCopy
}

func (s *TranscodingService) getTask(taskID string) (*TranscodingTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}

	taskCopy := *task
	if taskCopy.Metadata == nil {
		taskCopy.Metadata = make(map[string]interface{})
	}
	return &taskCopy, nil
}

func (s *TranscodingService) updateTask(taskID string, update func(task *TranscodingTask)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return fmt.Errorf("task not found: %s", taskID)
	}

	update(task)
	return nil
}

func (s *TranscodingService) listTasks(contentID, ownerWallet string, limit, offset int) []*TranscodingTask {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]*TranscodingTask, 0)
	for _, task := range s.tasks {
		if contentID != "" && task.ContentID != contentID {
			continue
		}
		if ownerWallet != "" && task.OwnerWallet != ownerWallet {
			continue
		}
		taskCopy := *task
		if taskCopy.Metadata == nil {
			taskCopy.Metadata = make(map[string]interface{})
		}
		tasks = append(tasks, &taskCopy)
	}

	if offset >= len(tasks) {
		return []*TranscodingTask{}
	}

	end := offset + limit
	if end > len(tasks) {
		end = len(tasks)
	}
	return tasks[offset:end]
}

func getRetryCount(task *TranscodingTask) int {
	if task.Metadata == nil {
		return 0
	}
	v, ok := task.Metadata["retry_count"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func setRetryCount(task *TranscodingTask, count int) {
	if task.Metadata == nil {
		task.Metadata = make(map[string]interface{})
	}
	task.Metadata["retry_count"] = count
}
