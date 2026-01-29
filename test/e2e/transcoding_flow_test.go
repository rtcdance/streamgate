package e2e_test

import (
	"bytes"
	"context"
	"testing"

	"streamgate/pkg/models"
	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

func TestE2E_TranscodingFlow(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	storage := helpers.SetupTestStorage(t)
	if storage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, storage)

	// Initialize services
	uploadService := service.NewUploadService(db, storage)
	contentService := service.NewContentService(db)
	transcodingService := service.NewTranscodingService(db)

	// Step 1: Upload video file
	videoContent := []byte("fake video content")
	filename := "test_video.mp4"

	upload, err := uploadService.Upload(context.Background(), filename, bytes.NewReader(videoContent))
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, upload)

	// Step 2: Create content entry
	content := &models.Content{
		Title:       "Test Video for Transcoding",
		Description: "A video to be transcoded",
		Type:        "video",
		Duration:    3600,
		FileSize:    int64(len(videoContent)),
		UploadID:    upload.ID,
	}

	err = contentService.Create(context.Background(), content)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, content.ID)

	// Step 3: Create transcoding tasks for multiple formats
	formats := []string{"hls", "dash", "mp4"}
	taskIDs := []string{}

	for _, format := range formats {
		task := &models.Task{
			Type:         "transcoding",
			ContentID:    content.ID,
			Status:       "pending",
			InputFormat:  "mp4",
			OutputFormat: format,
		}

		err := transcodingService.CreateTask(context.Background(), task)
		helpers.AssertNoError(t, err)
		helpers.AssertNotNil(t, task.ID)
		taskIDs = append(taskIDs, task.ID)
	}

	// Step 4: Verify all tasks were created
	helpers.AssertEqual(t, 3, len(taskIDs))

	// Step 5: Check task statuses
	for _, taskID := range taskIDs {
		status, err := transcodingService.GetTaskStatus(context.Background(), taskID)
		helpers.AssertNoError(t, err)
		helpers.AssertEqual(t, "pending", status.Status)
	}

	// Step 6: Simulate task processing
	for _, taskID := range taskIDs {
		err := transcodingService.UpdateTaskStatus(context.Background(), taskID, "processing")
		helpers.AssertNoError(t, err)

		err = transcodingService.UpdateTaskStatus(context.Background(), taskID, "completed")
		helpers.AssertNoError(t, err)
	}

	// Step 7: Verify all tasks completed
	for _, taskID := range taskIDs {
		status, err := transcodingService.GetTaskStatus(context.Background(), taskID)
		helpers.AssertNoError(t, err)
		helpers.AssertEqual(t, "completed", status.Status)
	}
}

func TestE2E_TranscodingWithRetry(t *testing.T) {
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

	// Simulate failure
	err = transcodingService.UpdateTaskStatus(context.Background(), task.ID, "failed")
	helpers.AssertNoError(t, err)

	// Verify failure
	status, err := transcodingService.GetTaskStatus(context.Background(), task.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "failed", status.Status)

	// Retry task
	err = transcodingService.RetryTask(context.Background(), task.ID)
	helpers.AssertNoError(t, err)

	// Verify retry
	status, err = transcodingService.GetTaskStatus(context.Background(), task.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "pending", status.Status)

	// Complete retry
	err = transcodingService.UpdateTaskStatus(context.Background(), task.ID, "processing")
	helpers.AssertNoError(t, err)

	err = transcodingService.UpdateTaskStatus(context.Background(), task.ID, "completed")
	helpers.AssertNoError(t, err)

	// Verify completion
	status, err = transcodingService.GetTaskStatus(context.Background(), task.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "completed", status.Status)
}

func TestE2E_TranscodingCancellation(t *testing.T) {
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

	// Start processing
	err = transcodingService.UpdateTaskStatus(context.Background(), task.ID, "processing")
	helpers.AssertNoError(t, err)

	// Cancel task
	err = transcodingService.CancelTask(context.Background(), task.ID)
	helpers.AssertNoError(t, err)

	// Verify cancellation
	status, err := transcodingService.GetTaskStatus(context.Background(), task.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "cancelled", status.Status)
}
