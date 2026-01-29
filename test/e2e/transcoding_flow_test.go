package e2e_test

import (
	"testing"

	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

type mockTranscodingQueue struct{}

func (m *mockTranscodingQueue) Enqueue(task *service.TranscodingTask) error {
	return nil
}

func (m *mockTranscodingQueue) Dequeue() (*service.TranscodingTask, error) {
	return nil, nil
}

func (m *mockTranscodingQueue) GetStatus(taskID string) (string, error) {
	return "pending", nil
}

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
	queue := &mockTranscodingQueue{}
	uploadService := service.NewUploadService(db.GetDB(), storage, "test-bucket")
	contentService := service.NewContentService(db.GetDB(), storage, nil)
	transcodingService := service.NewTranscodingService(db.GetDB(), queue)

	// Step 1: Upload video file
	videoContent := []byte("fake video content")
	filename := "test_video.mp4"

	uploadID, err := uploadService.Upload(filename, videoContent, "test-user")
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, uploadID)

	// Step 2: Create content entry
	content := &service.Content{
		Title:       "Test Video for Transcoding",
		Description: "A video to be transcoded",
		Type:        "video",
		Duration:    3600,
		Size:        int64(len(videoContent)),
		OwnerID:     "test-user",
	}

	contentID, err := contentService.CreateContent(content)
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, contentID)

	// Step 3: Create transcoding tasks for multiple formats
	formats := []string{"hls", "dash", "mp4"}
	taskIDs := []string{}

	for _, format := range formats {
		taskID, err := transcodingService.Transcode(contentID, format, "test-url", 1)
		helpers.AssertNoError(t, err)
		helpers.AssertNotEmpty(t, taskID)
		taskIDs = append(taskIDs, taskID)
	}

	// Step 4: Verify all tasks were created
	helpers.AssertEqual(t, 3, len(taskIDs))

	// Step 5: Check task statuses
	for _, taskID := range taskIDs {
		task, err := transcodingService.GetTranscodingStatus(taskID)
		helpers.AssertNoError(t, err)
		helpers.AssertEqual(t, "pending", task.Status)
	}

	// Step 6: Simulate task processing
	for _, taskID := range taskIDs {
		err := transcodingService.UpdateTaskStatus(taskID, "processing", 0)
		helpers.AssertNoError(t, err)

		err = transcodingService.CompleteTask(taskID, "test-output-url")
		helpers.AssertNoError(t, err)
	}

	// Step 7: Verify all tasks completed
	for _, taskID := range taskIDs {
		task, err := transcodingService.GetTranscodingStatus(taskID)
		helpers.AssertNoError(t, err)
		helpers.AssertEqual(t, "completed", task.Status)
	}
}

func TestE2E_TranscodingWithRetry(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	queue := &mockTranscodingQueue{}
	transcodingService := service.NewTranscodingService(db.GetDB(), queue)

	// Create task
	taskID, err := transcodingService.Transcode("content-123", "hls", "test-url", 1)
	helpers.AssertNoError(t, err)

	// Simulate failure
	err = transcodingService.FailTask(taskID, "test error")
	helpers.AssertNoError(t, err)

	// Verify failure
	task, err := transcodingService.GetTranscodingStatus(taskID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "failed", task.Status)

	// Retry task
	err = transcodingService.StartTask(taskID)
	helpers.AssertNoError(t, err)

	// Verify retry
	task, err = transcodingService.GetTranscodingStatus(taskID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "processing", task.Status)

	// Complete retry
	err = transcodingService.CompleteTask(taskID, "test-output-url")
	helpers.AssertNoError(t, err)

	// Verify completion
	task, err = transcodingService.GetTranscodingStatus(taskID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "completed", task.Status)
}

func TestE2E_TranscodingCancellation(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	queue := &mockTranscodingQueue{}
	transcodingService := service.NewTranscodingService(db.GetDB(), queue)

	// Create task
	taskID, err := transcodingService.Transcode("content-123", "hls", "test-url", 1)
	helpers.AssertNoError(t, err)

	// Start processing
	err = transcodingService.UpdateTaskStatus(taskID, "processing", 0)
	helpers.AssertNoError(t, err)

	// Cancel task
	err = transcodingService.CancelTask(taskID)
	helpers.AssertNoError(t, err)

	// Verify cancellation
	task, err := transcodingService.GetTranscodingStatus(taskID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "cancelled", task.Status)
}
