package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"streamgate/pkg/models"
	"streamgate/pkg/storage"
)

// TranscodingService handles transcoding operations
type TranscodingService struct {
	db         storage.DB
	queue      TranscodingQueue
	transcoder VideoTranscoder
	storage    SegmentStorage
	mu         sync.RWMutex
	tasks      map[string]*TranscodingTask
	cancel     context.CancelFunc
	cancelMu   sync.Mutex // protects cancel field from data race
}

// NewTranscodingService creates a new transcoding service
func NewTranscodingService(db storage.DB, queue TranscodingQueue, opts ...TranscodingOption) *TranscodingService {
	svc := &TranscodingService{
		db:    db,
		queue: queue,
		tasks: make(map[string]*TranscodingTask),
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

// StartWorker starts the background transcoding worker.
// It dequeues tasks and invokes the VideoTranscoder.
// Call StopWorker() to shut down.
func (s *TranscodingService) StartWorker(log interface{ Info(msg string, fields ...interface{}) }) {
	if s.transcoder == nil {
		if log != nil {
			log.Info("TranscodingService: no transcoder configured, worker not started")
		}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelMu.Lock()
	// Stop any previously running worker before starting a new one
	if s.cancel != nil {
		s.cancel()
	}
	s.cancel = cancel
	s.cancelMu.Unlock()

	go func() {
		if log != nil {
			log.Info("TranscodingService: worker started")
		}
		for {
			select {
			case <-ctx.Done():
				if log != nil {
					log.Info("TranscodingService: worker stopped")
				}
				return
			default:
			}

			task, err := s.queue.Dequeue()
			if err != nil || task == nil {
				time.Sleep(2 * time.Second)
				continue
			}

			s.processTask(ctx, task, log)
		}
	}()
}

// StopWorker stops the background worker
func (s *TranscodingService) StopWorker() {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

// processTask executes a single transcoding task
func (s *TranscodingService) processTask(ctx context.Context, task *TranscodingTask, log interface{ Info(msg string, fields ...interface{}) }) {
	// Mark as processing
	task.Status = "processing"
	now := time.Now()
	task.StartedAt = &now
	s.storeTask(task)

	// Create temp dir for output
	outputDir, err := os.MkdirTemp("", "streamgate-transcode-*")
	if err != nil {
		task.Status = "failed"
		task.Error = fmt.Sprintf("failed to create temp dir: %v", err)
		s.storeTask(task)
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

	// Run transcode with progress tracking
	err = s.transcoder.TranscodeToHLS(taskCtx, task.InputURL, outputDir, profile, func(progress float64) {
		task.Progress = int(progress)
		s.storeTask(task)
	})

	if err != nil {
		task.Status = "failed"
		task.Error = err.Error()
		s.storeTask(task)
		if log != nil {
			log.Info("TranscodingService: task failed", "task_id", task.ID, "error", err.Error())
		}
		return
	}

	// Upload segments to object storage
	if s.storage != nil {
		if err := s.uploadSegments(taskCtx, outputDir, task.ContentID, task.Profile); err != nil {
			task.Status = "failed"
			task.Error = fmt.Sprintf("failed to upload segments: %v", err)
			s.storeTask(task)
			return
		}
	}

	// Mark complete — output is safely in object storage, skip cleanup
	cleanupOutput = false
	task.Status = "completed"
	task.Progress = 100
	completedAt := time.Now()
	task.CompletedAt = &completedAt
	if s.storage != nil {
		task.OutputURL = fmt.Sprintf("streams/%s/%s", task.ContentID, task.Profile)
	}
	s.storeTask(task)

	if log != nil {
		log.Info("TranscodingService: task completed", "task_id", task.ID)
	}

	// Best-effort local cleanup (output is already uploaded)
	_ = os.RemoveAll(outputDir)
}

// uploadSegments walks the output directory and uploads all files to MinIO
func (s *TranscodingService) uploadSegments(ctx context.Context, outputDir, contentID, profile string) error {
	bucket := "streamgate"
	return filepath.WalkDir(outputDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		relPath, err := filepath.Rel(outputDir, path)
		if err != nil {
			return err
		}
		objectKey := fmt.Sprintf("streams/%s/%s/%s", contentID, profile, relPath)

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}

		contentType := "application/octet-stream"
		if strings.HasSuffix(path, ".m3u8") {
			contentType = "application/vnd.apple.mpegurl"
		} else if strings.HasSuffix(path, ".ts") {
			contentType = "video/mp2t"
		}

		if err := s.storage.UploadWithContentType(ctx, bucket, objectKey, data, contentType); err != nil {
			return fmt.Errorf("upload %s: %w", objectKey, err)
		}
		return nil
	})
}

// TranscodingQueue defines the interface for task queue
// VideoTranscoder defines the interface for video transcoding operations.
// Implemented by FFmpegTranscoder in pkg/plugins/transcoder.
type VideoTranscoder interface {
	TranscodeToHLS(ctx context.Context, inputPath, outputDir, profile string, progressFn func(progress float64)) error
}

// SegmentStorage defines the object storage operations needed by TranscodingService.
// This is a subset of storage.ObjectStorage to avoid import cycles.
// All methods accept a context.Context for timeout/cancellation propagation.
type SegmentStorage interface {
	Upload(ctx context.Context, bucket, objectName string, data []byte) error
	UploadStream(ctx context.Context, bucket, objectName string, reader io.Reader, size int64) error
	UploadWithContentType(ctx context.Context, bucket, objectName string, data []byte, contentType string) error
	Download(ctx context.Context, bucket, objectName string) ([]byte, error)
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
		Format:     "mp4",
	},
	"720p": {
		Name:       "720p",
		VideoCodec: "h264",
		AudioCodec: "aac",
		Resolution: "1280x720",
		Bitrate:    2500,
		Framerate:  30,
		Format:     "mp4",
	},
	"480p": {
		Name:       "480p",
		VideoCodec: "h264",
		AudioCodec: "aac",
		Resolution: "854x480",
		Bitrate:    1000,
		Framerate:  30,
		Format:     "mp4",
	},
	"360p": {
		Name:       "360p",
		VideoCodec: "h264",
		AudioCodec: "aac",
		Resolution: "640x360",
		Bitrate:    500,
		Framerate:  30,
		Format:     "mp4",
	},
}

// NewTranscodingServiceLegacy creates a new transcoding service without options (backwards compatible)
func NewTranscodingServiceLegacy(db storage.DB, queue TranscodingQueue) *TranscodingService {
	return &TranscodingService{
		db:    db,
		queue: queue,
		tasks: make(map[string]*TranscodingTask),
	}
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
	var metadataJSON []byte

	err := s.db.QueryRow(ctx, query, taskID).Scan(
		&task.ID,
		&task.ContentID,
		&task.Profile,
		&task.Status,
		&task.Progress,
		&task.InputURL,
		&task.OutputURL,
		&task.Error,
		&task.Priority,
		&task.CreatedAt,
		&startedAt,
		&completedAt,
		&metadataJSON,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("task not found: %s", taskID)
	} else if err != nil {
		return nil, fmt.Errorf("failed to query task: %w", err)
	}

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

	query := "UPDATE transcoding_tasks SET status = $2, progress = $3 WHERE id = $1"
	_, err := s.db.Exec(ctx, query, taskID, status, progress)
	if err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
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

	query := "UPDATE transcoding_tasks SET status = $2, started_at = $3 WHERE id = $1"
	_, err := s.db.Exec(ctx, query, taskID, "processing", time.Now())
	if err != nil {
		return fmt.Errorf("failed to start task: %w", err)
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

	query := "UPDATE transcoding_tasks SET status = $2, progress = $3, output_url = $4, completed_at = $5 WHERE id = $1"
	_, err := s.db.Exec(ctx, query, taskID, "completed", 100, outputURL, time.Now())
	if err != nil {
		return fmt.Errorf("failed to complete task: %w", err)
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

	query := "UPDATE transcoding_tasks SET status = $2, error = $3, completed_at = $4 WHERE id = $1"
	_, err := s.db.Exec(ctx, query, taskID, "failed", errorMsg, time.Now())
	if err != nil {
		return fmt.Errorf("failed to fail task: %w", err)
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
		var metadataJSON []byte

		err := rows.Scan(
			&task.ID,
			&task.ContentID,
			&task.Profile,
			&task.Status,
			&task.Progress,
			&task.InputURL,
			&task.OutputURL,
			&task.Error,
			&task.Priority,
			&task.CreatedAt,
			&startedAt,
			&completedAt,
			&metadataJSON,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}

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

	rowsAffected, errRA := result.RowsAffected(); if errRA != nil { return errRA }
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
		var metadataJSON []byte

		err := rows.Scan(
			&task.ID,
			&task.ContentID,
			&task.Profile,
			&task.Status,
			&task.Progress,
			&task.InputURL,
			&task.OutputURL,
			&task.Error,
			&task.Priority,
			&task.CreatedAt,
			&startedAt,
			&completedAt,
			&metadataJSON,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}

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
