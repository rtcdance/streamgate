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
	db                storage.DB
	queue             TranscodingQueue
	transcoder        VideoTranscoder
	storage           SegmentStorage
	log               *zap.Logger
	mu                sync.RWMutex
	tasks             map[string]*TranscodingTask
	cancel            context.CancelFunc
	cancelMu          sync.Mutex
	httpClient        *http.Client
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
}

const defaultUploadConcurrency = 5

// NewTranscodingService creates a new transcoding service
func NewTranscodingService(db storage.DB, queue TranscodingQueue, opts ...TranscodingOption) *TranscodingService {
	svc := &TranscodingService{
		db:         db,
		queue:      queue,
		tasks:      make(map[string]*TranscodingTask),
		httpClient: &http.Client{Timeout: 10 * time.Minute, Transport: &http.Transport{MaxIdleConns: 100, MaxIdleConnsPerHost: 20, IdleConnTimeout: 90 * time.Second}},
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

const (
	defaultMaxRetries = 3
	retryDelayBase    = 5 * time.Second
)

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
		task, err := s.queue.Dequeue(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				if log != nil {
					log.Info("TranscodingService: worker stopped", zap.Int("worker", workerID))
				}
				return
			}
			continue
		}

		if task.Metadata == nil {
			task.Metadata = make(map[string]interface{})
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					if log != nil {
						log.Info("TranscodingService: task panic recovered, continuing loop",
							zap.String("task_id", task.ID), zap.Any("panic", r))
					}
					task.Status = "failed"
					task.Error = fmt.Sprintf("panic during processing: %v", r)
					failCtx, failCancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer failCancel()
					if failErr := s.FailTask(failCtx, task.ID, task.Error); failErr != nil {
						if log != nil {
							log.Error("Failed to mark panicked task as failed", zap.String("task_id", task.ID), zap.Error(failErr))
						}
					}
				}
			}()
			s.processTask(ctx, task, log)
		}()

		if task.Status == "failed" {
			retryCount := getRetryCount(task)
			if retryCount < defaultMaxRetries {
				setRetryCount(task, retryCount+1)
				task.Status = "pending"
				task.Error = ""
				if log != nil {
					log.Info("TranscodingService: scheduling task for retry with exponential backoff",
						zap.String("task_id", task.ID), zap.Int("attempt", retryCount+1), zap.Int("max", defaultMaxRetries))
				}
				delay := retryDelayBase * time.Duration(1<<retryCount)
				go func() {
					time.Sleep(delay)
					if err := s.queue.Enqueue(task); err != nil {
						log.Error("Failed to re-enqueue transcoding task, task will be lost",
							zap.String("task_id", task.ID), zap.Error(err))
					}
				}()
			} else {
				if err := s.queue.Nak(task.ID); err != nil {
					log.Error("Failed to Nak transcoding task, it will remain in-flight",
						zap.String("task_id", task.ID), zap.Error(err))
				}
			}
		} else {
			if err := s.queue.Ack(task.ID); err != nil {
				log.Error("Failed to Ack transcoding task, it may be re-delivered",
					zap.String("task_id", task.ID), zap.Error(err))
			}
		}
	}
}

const stuckTaskTimeout = 10 * time.Minute

func (s *TranscodingService) recoverStuckTasks(log *zap.Logger) {
	if s.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := s.db.Query(ctx,
		"SELECT id, content_id, profile, input_url, priority, owner_wallet FROM transcoding_tasks WHERE status = 'processing' AND started_at < $1",
		time.Now().Add(-stuckTaskTimeout))
	if err != nil {
		if log != nil {
			log.Error("Failed to query stuck transcoding tasks", zap.Error(err))
		}
		return
	}
	defer rows.Close()

	var recovered []*TranscodingTask
	for rows.Next() {
		var t TranscodingTask
		if err := rows.Scan(&t.ID, &t.ContentID, &t.Profile, &t.InputURL, &t.Priority, &t.OwnerWallet); err != nil {
			continue
		}
		t.Status = "pending"
		t.Metadata = make(map[string]interface{})
		recovered = append(recovered, &t)
	}

	if len(recovered) == 0 {
		return
	}

	_, err = s.db.Exec(ctx,
		"UPDATE transcoding_tasks SET status = 'pending', started_at = NULL WHERE status = 'processing' AND started_at < $1",
		time.Now().Add(-stuckTaskTimeout))
	if err != nil {
		if log != nil {
			log.Error("Failed to reset stuck transcoding tasks", zap.Error(err))
		}
		return
	}

	if log != nil {
		log.Info("Recovered stuck transcoding tasks", zap.Int("count", len(recovered)))
	}

	if s.queue != nil {
		for _, t := range recovered {
			if qErr := s.queue.Enqueue(t); qErr != nil {
				if log != nil {
					log.Warn("Failed to re-enqueue recovered task", zap.String("task_id", t.ID), zap.Error(qErr))
				}
			}
		}
	}
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

// processTask executes a single transcoding task
//
//nolint:gocyclo // processTask has complex step logic, refactoring would be too invasive
func (s *TranscodingService) processTask(_ context.Context, task *TranscodingTask, log *zap.Logger) {
	task.Status = "processing"
	now := time.Now()
	task.StartedAt = &now
	s.storeTask(task)

	taskCtx, cancel := context.WithTimeout(s.serviceCtx, 60*time.Minute)
	defer cancel()

	if s.db != nil {
		result, execErr := s.db.Exec(taskCtx, "UPDATE transcoding_tasks SET status = $2, started_at = $3 WHERE id = $1 AND status = 'pending'", task.ID, "processing", now)
		if execErr != nil {
			s.log.Error("Failed to update task status to processing", zap.String("task_id", task.ID), zap.Error(execErr))
		} else if rows, _ := result.RowsAffected(); rows == 0 {
			s.log.Warn("Task was already being processed by another worker", zap.String("task_id", task.ID))
			return
		}
	}

	// Create temp dir for output
	outputDir, err := os.MkdirTemp("", "streamgate-transcode-*")
	if err != nil {
		if failErr := s.FailTask(taskCtx, task.ID, fmt.Sprintf("failed to create temp dir: %v", err)); failErr != nil {
			s.log.Error("Failed to mark task as failed in DB", zap.String("task_id", task.ID), zap.Error(failErr))
		}
		return
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
			if failErr := s.FailTask(taskCtx, task.ID, fmt.Sprintf("failed to download input: %v", err)); failErr != nil {
				s.log.Error("Failed to mark task as failed in DB", zap.String("task_id", task.ID), zap.Error(failErr))
			}
			return
		}
		downloadedFile = local
		inputPath = local
		defer func() { _ = os.Remove(downloadedFile) }()
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
			if _, err := s.db.Exec(context.Background(),
				"UPDATE transcoding_tasks SET progress = $2, metadata = $3 WHERE id = $1", task.ID, task.Progress, metadataJSON); err != nil && s.log != nil {
				s.log.Warn("failed to update variant progress in DB", zap.String("task_id", task.ID), zap.Error(err))
			}
		} else if s.db != nil && task.Progress-lastDBProgress >= 5 {
			lastDBProgress = task.Progress
			if _, err := s.db.Exec(context.Background(),
				"UPDATE transcoding_tasks SET progress = $2 WHERE id = $1", task.ID, task.Progress); err != nil && s.log != nil {
				s.log.Warn("failed to update progress in DB", zap.String("task_id", task.ID), zap.Error(err))
			}
		}
	})

	if err != nil {
		if failErr := s.FailTask(taskCtx, task.ID, err.Error()); failErr != nil {
			s.log.Error("Failed to mark task as failed in DB", zap.String("task_id", task.ID), zap.Error(failErr))
		}
		if log != nil {
			log.Info("TranscodingService: task failed", zap.String("task_id", task.ID), zap.String("error", err.Error()))
		}
		return
	}

	// Upload segments to object storage
	if s.storage != nil {
		if err := s.uploadSegments(taskCtx, outputDir, task.ContentID, task.Profile); err != nil {
			if failErr := s.FailTask(taskCtx, task.ID, fmt.Sprintf("failed to upload segments: %v", err)); failErr != nil {
				s.log.Error("Failed to mark task as failed in DB", zap.String("task_id", task.ID), zap.Error(failErr))
			}
			return
		}

		s.extractAndUploadThumbnail(taskCtx, inputPath, task.ContentID)
	}

	// Mark complete — output is safely in object storage, skip cleanup
	cleanupOutput = false
	outputURL := fmt.Sprintf("streams/%s/%s", task.ContentID, task.Profile)
	if err := s.CompleteTask(taskCtx, task.ID, outputURL); err != nil {
		s.log.Error("Failed to mark task as completed in DB", zap.String("task_id", task.ID), zap.Error(err))
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
}

// uploadSegments walks the output directory and uploads all files to MinIO
func (s *TranscodingService) uploadSegments(ctx context.Context, outputDir, contentID, profile string) error {
	bucket := "streamgate"

	type uploadJob struct {
		path        string
		objectKey   string
		contentType string
	}

	abrMode := profile == "abr"

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
		objectKey := fmt.Sprintf("streams/%s/%s/%s", contentID, subdir, relPath)

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
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
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

	if _, err := io.Copy(f, io.LimitReader(resp.Body, 2<<30)); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("write temp file: %w", err)
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

	// Save to database when persistence is available.
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
	} else {
		s.storeTask(task)
	}

	// Enqueue task
	if s.queue != nil {
		if err := s.queue.Enqueue(task); err != nil {
			return "", fmt.Errorf("failed to enqueue task: %w", err)
		}
	}

	return taskID, nil
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

	// Merge live variant_progress from in-memory store (DB metadata is stale during processing)
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

// CompleteTask marks a task as completed
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

	completeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return s.db.InTransaction(completeCtx, func(tx *sql.Tx) error {
		var contentID string
		var currentStatus string
		if err := tx.QueryRowContext(ctx, "SELECT content_id, status FROM transcoding_tasks WHERE id = $1", taskID).Scan(&contentID, &currentStatus); err != nil {
			return fmt.Errorf("failed to get task: %w", err)
		}
		if currentStatus != "processing" {
			return fmt.Errorf("invalid task state transition: %s -> completed", currentStatus)
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

		if metadataJSON != nil {
			if _, err := tx.ExecContext(ctx,
				"UPDATE transcoding_tasks SET status = $2, progress = $3, output_url = $4, completed_at = $5, metadata = $6 WHERE id = $1 AND status = 'processing'",
				taskID, "completed", 100, outputURL, time.Now(), metadataJSON); err != nil {
				return fmt.Errorf("failed to complete task: %w", err)
			}
		} else {
			if _, err := tx.ExecContext(ctx,
				"UPDATE transcoding_tasks SET status = $2, progress = $3, output_url = $4, completed_at = $5 WHERE id = $1 AND status = 'processing'",
				taskID, "completed", 100, outputURL, time.Now()); err != nil {
				return fmt.Errorf("failed to complete task: %w", err)
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

// saveTask saves a task to database
func (s *TranscodingService) saveTask(ctx context.Context, task *TranscodingTask) error {
	metadataJSON, err := json.Marshal(task.Metadata)
	if err != nil {
		return fmt.Errorf("failed to serialize metadata: %w", err)
	}

	query := `
		INSERT INTO transcoding_tasks (id, content_id, profile, status, progress, input_url, 
		                              output_url, error, priority, created_at, metadata, owner_wallet)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	_, err = s.db.Exec(ctx, query,
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
	)

	return err
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
