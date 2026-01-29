package transcoding_test

import (
	"bytes"
	"context"
	"testing"

	"streamgate/pkg/models"
	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

func TestTranscodingService_CreateTask(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	transcodingService := service.NewTranscodingService(db)

	// Create transcoding task
	task := &models.Task{
		Type:         "transcoding",
		ContentID:    "content-123",
		Status:       "pending",
		InputFormat:  "mp4",
		OutputFormat: "hls",
	}

	err := transcodingService.CreateTask(context.Background(), task)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, task.ID)
}

func TestTranscodingService_GetTaskStatus(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	transcodingService := service.NewTranscodingService(db)

	// Create task
	task := &models.Task{
		Type:         "transcoding",
		ContentID:    "content-123",
		Status:       "pending",
		InputFormat:  "mp4",
		OutputFormat: "hls",
	}

	err := transcodingService.CreateTask(context.Background(), task)
	helpers.AssertNoError(t, err)

	// Get task status
	status, err := transcodingService.GetTaskStatus(context.Background(), task.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, status)
}

func TestTranscodingService_UpdateTaskStatus(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	transcodingService := service.NewTranscodingService(db)

	// Create task
	task := &models.Task{
		Type:         "transcoding",
		ContentID:    "content-123",
		Status:       "pending",
		InputFormat:  "mp4",
		OutputFormat: "hls",
	}

	err := transcodingService.CreateTask(context.Background(), task)
	helpers.AssertNoError(t, err)

	// Update task status
	err = transcodingService.UpdateTaskStatus(context.Background(), task.ID, "processing")
	helpers.AssertNoError(t, err)

	// Verify update
	status, err := transcodingService.GetTaskStatus(context.Background(), task.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "processing", status.Status)
}

func TestTranscodingService_ListTasks(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	transcodingService := service.NewTranscodingService(db)

	// Create multiple tasks
	for i := 0; i < 3; i++ {
		task := &models.Task{
			Type:         "transcoding",
			ContentID:    "content-" + string(rune(i)),
			Status:       "pending",
			InputFormat:  "mp4",
			OutputFormat: "hls",
		}
		err := transcodingService.CreateTask(context.Background(), task)
		helpers.AssertNoError(t, err)
	}

	// List tasks
	tasks, err := transcodingService.ListTasks(context.Background(), 0, 10)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, len(tasks) >= 3)
}

func TestTranscodingService_CancelTask(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	transcodingService := service.NewTranscodingService(db)

	// Create task
	task := &models.Task{
		Type:         "transcoding",
		ContentID:    "content-123",
		Status:       "pending",
		InputFormat:  "mp4",
		OutputFormat: "hls",
	}

	err := transcodingService.CreateTask(context.Background(), task)
	helpers.AssertNoError(t, err)

	// Cancel task
	err = transcodingService.CancelTask(context.Background(), task.ID)
	helpers.AssertNoError(t, err)

	// Verify cancellation
	status, err := transcodingService.GetTaskStatus(context.Background(), task.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "cancelled", status.Status)
}

func TestTranscodingService_RetryTask(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	transcodingService := service.NewTranscodingService(db)

	// Create task
	task := &models.Task{
		Type:         "transcoding",
		ContentID:    "content-123",
		Status:       "failed",
		InputFormat:  "mp4",
		OutputFormat: "hls",
	}

	err := transcodingService.CreateTask(context.Background(), task)
	helpers.AssertNoError(t, err)

	// Retry task
	err = transcodingService.RetryTask(context.Background(), task.ID)
	helpers.AssertNoError(t, err)

	// Verify retry
	status, err := transcodingService.GetTaskStatus(context.Background(), task.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "pending", status.Status)
}

func TestTranscodingService_MultipleFormats(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	transcodingService := service.NewTranscodingService(db)

	// Test multiple output formats
	formats := []string{"hls", "dash", "mp4", "webm"}

	for _, format := range formats {
		task := &models.Task{
			Type:         "transcoding",
			ContentID:    "content-123",
			Status:       "pending",
			InputFormat:  "mp4",
			OutputFormat: format,
		}

		err := transcodingService.CreateTask(context.Background(), task)
		helpers.AssertNoError(t, err)
		helpers.AssertNotNil(t, task.ID)
	}
}
