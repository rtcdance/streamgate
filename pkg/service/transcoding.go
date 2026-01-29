package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TranscodingService handles transcoding operations
type TranscodingService struct {
	db    *sql.DB
	queue TranscodingQueue
}

// TranscodingQueue defines the interface for task queue
type TranscodingQueue interface {
	Enqueue(task *TranscodingTask) error
	Dequeue() (*TranscodingTask, error)
	GetStatus(taskID string) (string, error)
}

// TranscodingTask represents a transcoding task
type TranscodingTask struct {
	ID          string                 `json:"id"`
	ContentID   string                 `json:"content_id"`
	Profile     string                 `json:"profile"`
	Status      string                 `json:"status"`   // pending, processing, completed, failed
	Progress    int                    `json:"progress"` // 0-100
	InputURL    string                 `json:"input_url"`
	OutputURL   string                 `json:"output_url"`
	Error       string                 `json:"error,omitempty"`
	Priority    int                    `json:"priority"`
	CreatedAt   time.Time              `json:"created_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Metadata    map[string]interface{} `json:"metadata"`
}

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

// NewTranscodingService creates a new transcoding service
func NewTranscodingService(db *sql.DB, queue TranscodingQueue) *TranscodingService {
	return &TranscodingService{
		db:    db,
		queue: queue,
	}
}

// Transcode creates a transcoding task
func (s *TranscodingService) Transcode(contentID, profile string, inputURL string, priority int) (string, error) {
	// Validate profile
	if _, exists := DefaultProfiles[profile]; !exists {
		return "", fmt.Errorf("invalid profile: %s", profile)
	}

	// Generate task ID
	taskID := uuid.New().String()

	// Create task
	task := &TranscodingTask{
		ID:        taskID,
		ContentID: contentID,
		Profile:   profile,
		Status:    "pending",
		Progress:  0,
		InputURL:  inputURL,
		Priority:  priority,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	// Save to database
	if err := s.saveTask(task); err != nil {
		return "", fmt.Errorf("failed to save task: %w", err)
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
func (s *TranscodingService) GetTranscodingStatus(taskID string) (*TranscodingTask, error) {
	query := `
		SELECT id, content_id, profile, status, progress, input_url, output_url, 
		       error, priority, created_at, started_at, completed_at, metadata
		FROM transcoding_tasks
		WHERE id = $1
	`

	var task TranscodingTask
	var startedAt, completedAt sql.NullTime
	var metadataJSON []byte

	err := s.db.QueryRow(query, taskID).Scan(
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

	if err == sql.ErrNoRows {
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
func (s *TranscodingService) UpdateTaskStatus(taskID, status string, progress int) error {
	query := "UPDATE transcoding_tasks SET status = $2, progress = $3 WHERE id = $1"
	_, err := s.db.Exec(query, taskID, status, progress)
	if err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}
	return nil
}

// UpdateTaskProgress updates task progress
func (s *TranscodingService) UpdateTaskProgress(taskID string, progress int) error {
	query := "UPDATE transcoding_tasks SET progress = $2 WHERE id = $1"
	_, err := s.db.Exec(query, taskID, progress)
	if err != nil {
		return fmt.Errorf("failed to update task progress: %w", err)
	}
	return nil
}

// StartTask marks a task as started
func (s *TranscodingService) StartTask(taskID string) error {
	query := "UPDATE transcoding_tasks SET status = $2, started_at = $3 WHERE id = $1"
	_, err := s.db.Exec(query, taskID, "processing", time.Now())
	if err != nil {
		return fmt.Errorf("failed to start task: %w", err)
	}
	return nil
}

// CompleteTask marks a task as completed
func (s *TranscodingService) CompleteTask(taskID, outputURL string) error {
	query := "UPDATE transcoding_tasks SET status = $2, progress = $3, output_url = $4, completed_at = $5 WHERE id = $1"
	_, err := s.db.Exec(query, taskID, "completed", 100, outputURL, time.Now())
	if err != nil {
		return fmt.Errorf("failed to complete task: %w", err)
	}
	return nil
}

// FailTask marks a task as failed
func (s *TranscodingService) FailTask(taskID, errorMsg string) error {
	query := "UPDATE transcoding_tasks SET status = $2, error = $3, completed_at = $4 WHERE id = $1"
	_, err := s.db.Exec(query, taskID, "failed", errorMsg, time.Now())
	if err != nil {
		return fmt.Errorf("failed to fail task: %w", err)
	}
	return nil
}

// ListTasks lists transcoding tasks
func (s *TranscodingService) ListTasks(contentID string, limit, offset int) ([]*TranscodingTask, error) {
	query := `
		SELECT id, content_id, profile, status, progress, input_url, output_url,
		       error, priority, created_at, started_at, completed_at, metadata
		FROM transcoding_tasks
		WHERE content_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.Query(query, contentID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer rows.Close()

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
func (s *TranscodingService) CancelTask(taskID string) error {
	query := "UPDATE transcoding_tasks SET status = $2 WHERE id = $1 AND status IN ('pending', 'processing')"
	result, err := s.db.Exec(query, taskID, "cancelled")
	if err != nil {
		return fmt.Errorf("failed to cancel task: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("task cannot be cancelled: %s", taskID)
	}

	return nil
}

// DeleteTask deletes a transcoding task
func (s *TranscodingService) DeleteTask(taskID string) error {
	query := "DELETE FROM transcoding_tasks WHERE id = $1"
	_, err := s.db.Exec(query, taskID)
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
func (s *TranscodingService) saveTask(task *TranscodingTask) error {
	metadataJSON, err := json.Marshal(task.Metadata)
	if err != nil {
		return fmt.Errorf("failed to serialize metadata: %w", err)
	}

	query := `
		INSERT INTO transcoding_tasks (id, content_id, profile, status, progress, input_url, 
		                              output_url, error, priority, created_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, err = s.db.Exec(query,
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
func (s *TranscodingService) GetPendingTasks(limit int) ([]*TranscodingTask, error) {
	query := `
		SELECT id, content_id, profile, status, progress, input_url, output_url,
		       error, priority, created_at, started_at, completed_at, metadata
		FROM transcoding_tasks
		WHERE status = 'pending'
		ORDER BY priority DESC, created_at ASC
		LIMIT $1
	`

	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending tasks: %w", err)
	}
	defer rows.Close()

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
