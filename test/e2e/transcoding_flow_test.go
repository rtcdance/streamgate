//go:build e2e

package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/test/helpers"

	"github.com/google/uuid"
)

// createTranscodeContent inserts a real content row. transcoding_tasks.content_id
// carries a foreign key to contents(id), so a synthetic UUID is rejected by
// PostgreSQL — the task must point at a row that exists.
func createTranscodeContent(t *testing.T, db *storage.Database) string {
	t.Helper()
	contentService := service.NewContentService(db, nil, nil)
	// (owner_id, url) is unique on contents; a per-call URL keeps the fixture
	// collision-free even when a previous run left rows behind.
	contentID, err := contentService.CreateContent(context.Background(), &service.Content{
		Title:       "Transcoding fixture",
		Description: "content row for transcoding task foreign keys",
		Type:        "video",
		URL:         "test-url-" + uuid.New().String(),
		OwnerID:     "0xTestOwner",
	})
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, contentID)
	return contentID
}

type mockTranscodingQueue struct{}

type e2eQueueHandle string

func (h e2eQueueHandle) TaskID() string { return string(h) }

func (m *mockTranscodingQueue) Enqueue(task *service.TranscodingTask) error {
	return nil
}

func (m *mockTranscodingQueue) Dequeue(ctx context.Context) (*service.TranscodingTask, service.DeliveryHandle, error) {
	return nil, nil, nil
}

func (m *mockTranscodingQueue) Ack(service.DeliveryHandle) error {
	return nil
}

func (m *mockTranscodingQueue) Nak(service.DeliveryHandle, time.Duration) error {
	return nil
}

func (m *mockTranscodingQueue) InProgress(service.DeliveryHandle) error {
	return nil
}

func (m *mockTranscodingQueue) PublishOutbox(_, _ string, _ []byte) error {
	return nil
}

func (m *mockTranscodingQueue) Depth() (int, error) {
	return 0, nil
}

func TestE2E_TranscodingFlow(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	objStorage := helpers.SetupTestStorage(t)
	if objStorage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, objStorage)

	// Initialize services
	queue := &mockTranscodingQueue{}
	uploadService := service.NewUploadService(db, objStorage, "test-bucket")
	contentService := service.NewContentService(db, objStorage, nil)
	transcodingService := service.NewTranscodingService(db, queue)

	// Step 1: Upload video file
	videoContent := []byte("fake video content")
	filename := "test_video.mp4"
	testUserID := uuid.New().String()

	uploadID, err := uploadService.Upload(context.Background(), filename, videoContent, testUserID)
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, uploadID)

	// Step 2: Create content entry
	content := &service.Content{
		Title:       "Test Video for Transcoding",
		Description: "A video to be transcoded",
		Type:        "video",
		Duration:    3600,
		Size:        int64(len(videoContent)),
		OwnerID:     testUserID,
	}

	contentID, err := contentService.CreateContent(context.Background(), content)
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, contentID)

	// Step 3: Create transcoding tasks for multiple formats
	formats := []string{"1080p", "720p", "480p"}
	taskIDs := []string{}

	for _, format := range formats {
		taskID, err := transcodingService.Transcode(context.Background(), contentID, format, "test-url", 1, "0xTestOwner")
		helpers.AssertNoError(t, err)
		helpers.AssertNotEmpty(t, taskID)
		taskIDs = append(taskIDs, taskID)
	}

	// Step 4: Verify all tasks were created
	helpers.AssertEqual(t, 3, len(taskIDs))

	// Step 5: Check task statuses
	for _, taskID := range taskIDs {
		task, err := transcodingService.GetTranscodingStatus(context.Background(), taskID)
		helpers.AssertNoError(t, err)
		helpers.AssertEqual(t, "pending", task.Status)
	}

	// Step 6: Simulate task processing
	for _, taskID := range taskIDs {
		err := transcodingService.UpdateTaskStatus(context.Background(), taskID, "processing", 0)
		helpers.AssertNoError(t, err)

		err = transcodingService.CompleteTask(context.Background(), taskID, "test-output-url")
		helpers.AssertNoError(t, err)
	}

	// Step 7: Verify all tasks completed
	for _, taskID := range taskIDs {
		task, err := transcodingService.GetTranscodingStatus(context.Background(), taskID)
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
	transcodingService := service.NewTranscodingService(db, queue)
	contentID := createTranscodeContent(t, db)

	// Create task
	taskID, err := transcodingService.Transcode(context.Background(), contentID, "1080p", "test-url", 1, "0xTestOwner")
	helpers.AssertNoError(t, err)

	// Simulate failure
	err = transcodingService.FailTask(context.Background(), taskID, "test error")
	helpers.AssertNoError(t, err)

	// Verify failure
	task, err := transcodingService.GetTranscodingStatus(context.Background(), taskID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "failed", task.Status)

	// Retry task: a failed task is re-armed through the public transition map
	// (failed -> pending, models.validTaskTransitions) before StartTask claims
	// it again. StartTask itself only moves pending -> processing, which is the
	// lease-free equivalent of the worker's claim path.
	err = transcodingService.UpdateTaskStatus(context.Background(), taskID, "pending", 0)
	helpers.AssertNoError(t, err)

	task, err = transcodingService.GetTranscodingStatus(context.Background(), taskID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "pending", task.Status)

	err = transcodingService.StartTask(context.Background(), taskID)
	helpers.AssertNoError(t, err)

	// Verify retry
	task, err = transcodingService.GetTranscodingStatus(context.Background(), taskID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "processing", task.Status)

	// Complete retry
	err = transcodingService.CompleteTask(context.Background(), taskID, "test-output-url")
	helpers.AssertNoError(t, err)

	// Verify completion
	task, err = transcodingService.GetTranscodingStatus(context.Background(), taskID)
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
	transcodingService := service.NewTranscodingService(db, queue)
	contentID := createTranscodeContent(t, db)

	// Create task
	taskID, err := transcodingService.Transcode(context.Background(), contentID, "1080p", "test-url", 1, "0xTestOwner")
	helpers.AssertNoError(t, err)

	// Start processing
	err = transcodingService.UpdateTaskStatus(context.Background(), taskID, "processing", 0)
	helpers.AssertNoError(t, err)

	// Cancel task
	err = transcodingService.CancelTask(context.Background(), taskID)
	helpers.AssertNoError(t, err)

	// Verify cancellation
	task, err := transcodingService.GetTranscodingStatus(context.Background(), taskID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "cancelled", task.Status)
}
