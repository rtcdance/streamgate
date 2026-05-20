package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"streamgate/pkg/models"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/storage"

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
func (s *TranscodingService) StartWorker(log interface {
	Info(msg string, fields ...interface{})
}) {
	if s.transcoder == nil {
		if log != nil {
			log.Info("TranscodingService: no transcoder configured, worker not started")
		}
		return
	}

	s.cancelMu.Lock()
	if atomic.LoadInt32(&s.running) == 1 {
		s.cancelMu.Unlock()
		if log != nil {
			log.Info("TranscodingService: worker already running, stopping previous instance")
		}
		s.StopWorker()
		s.cancelMu.Lock()
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
	for i := 0; i < initWorkers; i++ {
		s.wg.Add(1)
		s.startWorkerGoroutine(ctx, i, log)
	}

	s.wg.Add(1)
	s.startAutoScaler(ctx, log)
}

func (s *TranscodingService) workerLoop(ctx context.Context, workerID int, log interface {
	Info(msg string, fields ...interface{})
}) {
	defer s.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			if log != nil {
				log.Info("TranscodingService: worker panic recovered", "worker", workerID, "panic", r)
			}
		}
	}()
	if log != nil {
		log.Info("TranscodingService: worker started", "worker", workerID)
	}
	for {
		task, err := s.queue.Dequeue(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				if log != nil {
					log.Info("TranscodingService: worker stopped", "worker", workerID)
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
							"task_id", task.ID, "panic", r)
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
					log.Info("TranscodingService: re-enqueuing task for retry",
						"task_id", task.ID, "attempt", retryCount+1, "max", defaultMaxRetries)
				}
				_ = s.queue.Enqueue(task)
			} else {
				_ = s.queue.Nak(task.ID)
			}
		} else {
			_ = s.queue.Ack(task.ID)
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

func (s *TranscodingService) startWorkerGoroutine(ctx context.Context, workerID int, log interface {
	Info(msg string, fields ...interface{})
}) {
	go s.workerLoop(ctx, workerID, log)
}

const (
	scaleCheckInterval = 5 * time.Second
	tasksPerWorker     = 2
)

func (s *TranscodingService) startAutoScaler(parentCtx context.Context, log interface {
	Info(msg string, fields ...interface{})
}) {
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

func (s *TranscodingService) adjustWorkerCount(parentCtx context.Context, log interface {
	Info(msg string, fields ...interface{})
}) {
	depth, err := s.queue.Depth()
	if err != nil {
		return
	}
	monitoring.TranscodingQueueDepth.Set(float64(depth))

	target := s.minWorkers
	needed := (depth + tasksPerWorker - 1) / tasksPerWorker
	if needed > target-s.minWorkers {
		target = s.minWorkers + needed
	}
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
			log.Info("TranscodingService: scaled up workers", "from", current, "to", target, "queue_depth", depth)
		}
	} else if target < current {
		s.extraMu.Lock()
		remove := current - target
		if remove > len(s.extraCancels) {
			remove = len(s.extraCancels)
		}
		for i := 0; i < remove; i++ {
			s.extraCancels[len(s.extraCancels)-1]()
			s.extraCancels = s.extraCancels[:len(s.extraCancels)-1]
		}
		atomic.StoreInt32(&s.currentWorkers, int32(target))
		s.extraMu.Unlock()
		if log != nil {
			log.Info("TranscodingService: scaled down workers", "from", current, "to", target, "queue_depth", depth)
		}
	}
	monitoring.TranscodingWorkersActive.Set(float64(target))
}

// processTask executes a single transcoding task
func (s *TranscodingService) processTask(ctx context.Context, task *TranscodingTask, log interface {
	Info(msg string, fields ...interface{})
}) {
	// Mark as processing
	task.Status = "processing"
	now := time.Now()
	task.StartedAt = &now
	s.storeTask(task)
	if s.db != nil {
		result, execErr := s.db.Exec(ctx, "UPDATE transcoding_tasks SET status = $2, started_at = $3 WHERE id = $1 AND status = 'pending'", task.ID, "processing", now)
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
		if failErr := s.FailTask(ctx, task.ID, fmt.Sprintf("failed to create temp dir: %v", err)); failErr != nil {
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
	profile := task.Profile
	if _, ok := DefaultProfiles[profile]; !ok {
		profile = "720p"
	}

	// Add a per-task timeout to prevent runaway FFmpeg processes.
	// Each profile's segment duration is ~6s; allow 5 minutes per profile as safety.
	taskCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

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

	// Run transcode with progress tracking
	err = s.transcoder.TranscodeHLS(taskCtx, inputPath, outputDir, profile, func(progress float64) {
		task.Progress = int(progress)
		s.storeTask(task)
	})

	if err != nil {
		if failErr := s.FailTask(taskCtx, task.ID, err.Error()); failErr != nil {
			s.log.Error("Failed to mark task as failed in DB", zap.String("task_id", task.ID), zap.Error(failErr))
		}
		if log != nil {
			log.Info("TranscodingService: task failed", "task_id", task.ID, "error", err.Error())
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
		log.Info("TranscodingService: task completed", "task_id", task.ID)
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

	var jobs []uploadJob
	err := filepath.WalkDir(outputDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		relPath, err := filepath.Rel(outputDir, path)
		if err != nil {
			return err
		}
		objectKey := fmt.Sprintf("streams/%s/%s/%s", contentID, profile, relPath)

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

	for _, job := range jobs {
		uploadWg.Add(1)
		go func(j uploadJob) {
			defer uploadWg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			f, err := os.Open(j.path)
			if err != nil {
				uploadMu.Lock()
				if uploadErr == nil {
					uploadErr = fmt.Errorf("open file %s: %w", j.path, err)
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
				}
				uploadMu.Unlock()
				return
			}

			if err := s.storage.UploadStreamWithContentType(ctx, bucket, j.objectKey, f, fi.Size(), j.contentType); err != nil {
				uploadMu.Lock()
				if uploadErr == nil {
					uploadErr = fmt.Errorf("upload %s: %w", j.objectKey, err)
				}
				uploadMu.Unlock()
				return
			}
		}(job)
	}

	uploadWg.Wait()
	return uploadErr
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

	// Create temp file with appropriate extension
	ext := filepath.Ext(inputURL)
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
	TranscodeHLS(ctx context.Context, inputPath, outputDir, profile string, progressFn func(progress float64)) error
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
	if _, exists := DefaultProfiles[profile]; !exists {
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
		       error, priority, created_at, started_at, completed_at, metadata
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
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("task not found %s: %w", taskID, ErrNotFound)
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

	// Parse metadata
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &task.Metadata); err != nil {
			return nil, fmt.Errorf("failed to parse metadata: %w", err)
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
		})
	}

	return s.db.InTransaction(ctx, func(tx *sql.Tx) error {
		var contentID string
		var currentStatus string
		if err := tx.QueryRowContext(ctx, "SELECT content_id, status FROM transcoding_tasks WHERE id = $1", taskID).Scan(&contentID, &currentStatus); err != nil {
			return fmt.Errorf("failed to get task: %w", err)
		}
		if currentStatus != "processing" {
			return fmt.Errorf("invalid task state transition: %s -> completed", currentStatus)
		}

		if _, err := tx.ExecContext(ctx,
			"UPDATE transcoding_tasks SET status = $2, progress = $3, output_url = $4, completed_at = $5 WHERE id = $1 AND status = 'processing'",
			taskID, "completed", 100, outputURL, time.Now()); err != nil {
			return fmt.Errorf("failed to complete task: %w", err)
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

	query := "UPDATE transcoding_tasks SET status = $2, error = $3, completed_at = $4 WHERE id = $1 AND status IN ('pending', 'processing')"
	result, err := s.db.Exec(ctx, query, taskID, "failed", errorMsg, time.Now())
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

	query := `
		SELECT id, content_id, profile, status, progress, input_url, output_url,
		       error, priority, created_at, started_at, completed_at, metadata
		FROM transcoding_tasks
		WHERE content_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.Query(ctx, query, contentID, limit, offset)
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
		                              output_url, error, priority, created_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
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
	)

	return err
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
