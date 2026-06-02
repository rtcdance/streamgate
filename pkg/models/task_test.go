package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTask_Creation(t *testing.T) {
	now := time.Now()
	task := &Task{
		ID:         "task123",
		Type:       "transcode",
		Status:     "pending",
		Priority:   5,
		Payload:    map[string]interface{}{"input": "video.mp4"},
		Result:     nil,
		Error:      "",
		RetryCount: 0,
		MaxRetries: 3,
		CreatedAt:  now,
	}

	assert.Equal(t, "task123", task.ID)
	assert.Equal(t, "transcode", task.Type)
	assert.Equal(t, "pending", task.Status)
	assert.Equal(t, 5, task.Priority)
	assert.Equal(t, "video.mp4", task.Payload["input"])
	assert.Equal(t, 0, task.RetryCount)
	assert.Equal(t, 3, task.MaxRetries)
}

func TestTask_ZeroValues(t *testing.T) {
	task := &Task{}

	assert.Equal(t, "", task.ID)
	assert.Equal(t, "", task.Type)
	assert.Equal(t, "", task.Status)
	assert.Equal(t, 0, task.Priority)
	assert.Nil(t, task.Payload)
	assert.Nil(t, task.Result)
	assert.Equal(t, "", task.Error)
	assert.True(t, task.CreatedAt.IsZero())
	assert.True(t, task.StartedAt.IsZero())
	assert.True(t, task.CompletedAt.IsZero())
}

func TestTask_StatusTransitions(t *testing.T) {
	task := &Task{
		ID:     "task123",
		Type:   "transcode",
		Status: "pending",
	}

	task.Status = "running"
	assert.Equal(t, "running", task.Status)

	task.Status = "completed"
	assert.Equal(t, "completed", task.Status)
}

func TestTask_RetryTracking(t *testing.T) {
	task := &Task{
		ID:         "task123",
		Status:     "failed",
		RetryCount: 1,
		MaxRetries: 3,
		Error:      "timeout",
	}

	assert.Equal(t, 1, task.RetryCount)
	assert.True(t, task.RetryCount < task.MaxRetries)
	assert.Equal(t, "timeout", task.Error)

	task.RetryCount++
	task.Status = "retrying"
	assert.Equal(t, 2, task.RetryCount)
	assert.Equal(t, "retrying", task.Status)
}

func TestTask_ProgressTracking(t *testing.T) {
	now := time.Now()
	task := &Task{
		ID:        "task123",
		Status:    "running",
		Payload:   map[string]interface{}{"progress": float64(50)},
		StartedAt: now,
	}

	assert.Equal(t, float64(50), task.Payload["progress"])
	assert.False(t, task.StartedAt.IsZero())
}

func TestTask_JSONMarshaling(t *testing.T) {
	task := &Task{
		ID:         "json-task",
		Type:       "transcode",
		Status:     "pending",
		Priority:   5,
		Payload:    map[string]interface{}{"key": "value"},
		MaxRetries: 3,
	}

	data, err := json.Marshal(task)
	assert.NoError(t, err)

	var decoded Task
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, task.ID, decoded.ID)
	assert.Equal(t, task.Type, decoded.Type)
	assert.Equal(t, task.Status, decoded.Status)
	assert.Equal(t, task.Priority, decoded.Priority)
}

func TestTaskType_Constants(t *testing.T) {
	assert.Equal(t, TaskType("transcode"), TaskTranscode)
	assert.Equal(t, TaskType("upload"), TaskUpload)
	assert.Equal(t, TaskType("process"), TaskProcess)
	assert.Equal(t, TaskType("cleanup"), TaskCleanup)
}

func TestTaskStatus_Constants(t *testing.T) {
	assert.Equal(t, TaskStatus("pending"), StatusPending)
	assert.Equal(t, TaskStatus("running"), StatusRunning)
	assert.Equal(t, TaskStatus("completed"), StatusCompleted)
	assert.Equal(t, TaskStatus("failed"), StatusFailed)
	assert.Equal(t, TaskStatus("retrying"), StatusRetrying)
}

func TestTaskPriority_Constants(t *testing.T) {
	assert.Equal(t, TaskPriority(1), PriorityLow)
	assert.Equal(t, TaskPriority(5), PriorityNormal)
	assert.Equal(t, TaskPriority(10), PriorityHigh)
}
