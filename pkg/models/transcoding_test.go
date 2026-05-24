package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTranscodingTask_Creation(t *testing.T) {
	now := time.Now()
	task := &TranscodingTask{
		ID:          "tt-123",
		ContentID:   "content-456",
		Profile:     "720p",
		Status:      "pending",
		Progress:    0,
		InputURL:    "/input/video.mp4",
		OutputURL:   "/output/video.m3u8",
		Priority:    5,
		OwnerWallet: "0xABC123",
		CreatedAt:   now,
		Metadata:    map[string]interface{}{"codec": "h264"},
	}

	assert.Equal(t, "tt-123", task.ID)
	assert.Equal(t, "content-456", task.ContentID)
	assert.Equal(t, "720p", task.Profile)
	assert.Equal(t, "pending", task.Status)
	assert.Equal(t, 0, task.Progress)
	assert.Equal(t, "/input/video.mp4", task.InputURL)
	assert.Equal(t, "/output/video.m3u8", task.OutputURL)
	assert.Equal(t, 5, task.Priority)
	assert.Equal(t, "0xABC123", task.OwnerWallet)
}

func TestTranscodingTask_ZeroValues(t *testing.T) {
	task := &TranscodingTask{}

	assert.Equal(t, "", task.ID)
	assert.Equal(t, "", task.ContentID)
	assert.Equal(t, "", task.Profile)
	assert.Equal(t, "", task.Status)
	assert.Equal(t, 0, task.Progress)
	assert.Nil(t, task.StartedAt)
	assert.Nil(t, task.CompletedAt)
	assert.Nil(t, task.Metadata)
}

func TestTranscodingTask_ProgressTracking(t *testing.T) {
	task := &TranscodingTask{
		ID:       "tt-progress",
		Status:   "processing",
		Progress: 50,
	}

	assert.Equal(t, 50, task.Progress)

	task.Progress = 100
	task.Status = "completed"
	assert.Equal(t, 100, task.Progress)
	assert.Equal(t, "completed", task.Status)
}

func TestTranscodingTask_ProgressBounds(t *testing.T) {
	tests := []struct {
		name     string
		progress int
		valid    bool
	}{
		{"zero", 0, true},
		{"fifty", 50, true},
		{"hundred", 100, true},
		{"negative", -1, false},
		{"over hundred", 101, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := tt.progress >= 0 && tt.progress <= 100
			assert.Equal(t, tt.valid, valid)
		})
	}
}

func TestTranscodingTask_ErrorField(t *testing.T) {
	task := &TranscodingTask{
		ID:     "tt-err",
		Status: "failed",
		Error:  "ffmpeg exited with code 1",
	}

	assert.Equal(t, "ffmpeg exited with code 1", task.Error)
}

func TestTranscodingTask_JSONMarshaling(t *testing.T) {
	now := time.Now()
	task := &TranscodingTask{
		ID:        "json-tt",
		ContentID: "content-json",
		Profile:   "1080p",
		Status:    "pending",
		Progress:  0,
		InputURL:  "/input/test.mp4",
		Metadata:  map[string]interface{}{"format": "hls"},
		CreatedAt: now,
	}

	data, err := json.Marshal(task)
	assert.NoError(t, err)

	var decoded TranscodingTask
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, task.ID, decoded.ID)
	assert.Equal(t, task.ContentID, decoded.ContentID)
	assert.Equal(t, task.Profile, decoded.Profile)
	assert.Equal(t, task.Status, decoded.Status)
}

func TestTranscodingTaskStatus_Constants(t *testing.T) {
	assert.Equal(t, TranscodingTaskStatus("pending"), TaskStatusPending)
	assert.Equal(t, TranscodingTaskStatus("processing"), TaskStatusProcessing)
	assert.Equal(t, TranscodingTaskStatus("completed"), TaskStatusCompleted)
	assert.Equal(t, TranscodingTaskStatus("failed"), TaskStatusFailed)
	assert.Equal(t, TranscodingTaskStatus("cancelled"), TaskStatusCancelled)
}

func TestIsValidTaskTransition(t *testing.T) {
	tests := []struct {
		name    string
		from    TranscodingTaskStatus
		to      TranscodingTaskStatus
		isValid bool
	}{
		{"pending to processing", TaskStatusPending, TaskStatusProcessing, true},
		{"pending to cancelled", TaskStatusPending, TaskStatusCancelled, true},
		{"pending to failed", TaskStatusPending, TaskStatusFailed, true},
		{"pending to completed", TaskStatusPending, TaskStatusCompleted, false},
		{"processing to completed", TaskStatusProcessing, TaskStatusCompleted, true},
		{"processing to failed", TaskStatusProcessing, TaskStatusFailed, true},
		{"processing to cancelled", TaskStatusProcessing, TaskStatusCancelled, true},
		{"processing to pending", TaskStatusProcessing, TaskStatusPending, false},
		{"completed to processing", TaskStatusCompleted, TaskStatusProcessing, false},
		{"completed to failed", TaskStatusCompleted, TaskStatusFailed, false},
		{"failed to pending", TaskStatusFailed, TaskStatusPending, true},
		{"failed to completed", TaskStatusFailed, TaskStatusCompleted, false},
		{"cancelled to pending", TaskStatusCancelled, TaskStatusPending, false},
		{"same status pending", TaskStatusPending, TaskStatusPending, true},
		{"same status processing", TaskStatusProcessing, TaskStatusProcessing, true},
		{"same status completed", TaskStatusCompleted, TaskStatusCompleted, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidTaskTransition(tt.from, tt.to)
			assert.Equal(t, tt.isValid, result)
		})
	}
}

func TestTranscodingTask_StartedAtNil(t *testing.T) {
	task := &TranscodingTask{ID: "nil-test"}
	assert.Nil(t, task.StartedAt)
	assert.Nil(t, task.CompletedAt)
}

func TestTranscodingTask_StartedAtSet(t *testing.T) {
	now := time.Now()
	task := &TranscodingTask{
		ID:        "started-test",
		StartedAt: &now,
	}
	assert.NotNil(t, task.StartedAt)
	assert.Equal(t, now, *task.StartedAt)
}
