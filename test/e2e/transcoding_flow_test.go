//go:build e2e

package e2e_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/test/helpers"

	"github.com/google/uuid"
	"go.uber.org/zap"
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

// flakyTranscoder fails its first attempt with a retryable error and succeeds
// afterwards, so the retry under test is the worker's own release-and-redeliver
// path rather than a status edit made by the test.
type flakyTranscoder struct {
	mu    sync.Mutex
	calls int
}

func (x *flakyTranscoder) TranscodeHLS(ctx context.Context, inputPath, outputDir, profile string, progressFn func(variant string, progress float64)) error {
	x.mu.Lock()
	x.calls++
	attempt := x.calls
	x.mu.Unlock()

	if attempt == 1 {
		return errors.New("ffmpeg transient failure")
	}
	if progressFn != nil {
		progressFn("", 100)
	}
	return nil
}

func (x *flakyTranscoder) callCount() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.calls
}

// taskRow is the slice of transcoding_tasks that distinguishes the worker's
// retry release from a manual status edit: attempt, lease and last_error are
// written by the lease-guarded SQL paths only.
type taskRow struct {
	status      string
	attempt     int
	leaseOwner  string
	lastError   string
	nextAttempt sql.NullTime
}

func readTaskRow(t *testing.T, db *storage.Database, taskID string) (taskRow, error) {
	t.Helper()
	var row taskRow
	err := db.QueryRow(context.Background(), `
		SELECT status, attempt, lease_owner, last_error, next_attempt_at
		FROM transcoding_tasks WHERE id = $1`, taskID).
		Scan(&row.status, &row.attempt, &row.leaseOwner, &row.lastError, &row.nextAttempt)
	return row, err
}

// waitForTaskRow polls until cond accepts the row. The retry schedule itself is
// DB time (next_attempt_at); polling only waits for the worker to get there.
func waitForTaskRow(t *testing.T, db *storage.Database, taskID, what string, cond func(taskRow) bool) taskRow {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last taskRow
	for time.Now().Before(deadline) {
		row, err := readTaskRow(t, db, taskID)
		if err == nil {
			last = row
			if cond(row) {
				return row
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s: status=%q attempt=%d lease_owner=%q last_error=%q",
		what, last.status, last.attempt, last.leaseOwner, last.lastError)
	return last
}

// TestE2E_TranscodingWithRetry drives the production retry entry: the worker
// claims the task, attempt 1 fails inside the worker, releaseForRetry puts the
// row back to pending (attempt preserved, lease cleared, next_attempt_at set)
// and the Nak backoff redelivers the same message. Attempt 2 completes.
//
// A task that reaches "failed" is terminal for the public API: StartTask only
// claims pending rows and the transition map refuses failed -> completed. The
// test pins that refusal so "retry" can never mean a test-side status edit.
func TestE2E_TranscodingWithRetry(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	queue := service.NewMemoryTranscodingQueue()
	xcoder := &flakyTranscoder{}
	transcodingService := service.NewTranscodingService(db, queue, service.WithTranscoder(xcoder))
	defer transcodingService.StopWorker()

	contentID := createTranscodeContent(t, db)
	taskID, err := transcodingService.Transcode(context.Background(), contentID, "1080p", "test-url", 1, "0xTestOwner")
	helpers.AssertNoError(t, err)

	// Nothing claims a task until the worker runs, exactly as in production.
	transcodingService.StartWorker(zap.NewNop())

	released := waitForTaskRow(t, db, taskID, "worker retry release", func(row taskRow) bool {
		return row.status == "pending" && row.attempt == 1
	})
	helpers.AssertEqual(t, "", released.leaseOwner)
	helpers.AssertEqual(t, "ffmpeg transient failure", released.lastError)
	helpers.AssertTrue(t, released.nextAttempt.Valid)

	completed := waitForTaskRow(t, db, taskID, "completion after retry", func(row taskRow) bool {
		return row.status == "completed"
	})
	helpers.AssertEqual(t, 2, completed.attempt)
	helpers.AssertEqual(t, 2, xcoder.callCount())

	task, err := transcodingService.GetTranscodingStatus(context.Background(), taskID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "completed", task.Status)

	var contentStatus string
	err = db.QueryRow(context.Background(), "SELECT status FROM contents WHERE id = $1", contentID).Scan(&contentStatus)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "ready", contentStatus)

	transcodingService.StopWorker()
	failedTaskID, err := transcodingService.Transcode(context.Background(), contentID, "720p", "test-url", 1, "0xTestOwner")
	helpers.AssertNoError(t, err)
	err = transcodingService.FailTask(context.Background(), failedTaskID, "terminal failure")
	helpers.AssertNoError(t, err)

	err = transcodingService.StartTask(context.Background(), failedTaskID)
	helpers.AssertError(t, err)
	helpers.AssertTrue(t, strings.Contains(err.Error(), "task not in pending state"))

	err = transcodingService.UpdateTaskStatus(context.Background(), failedTaskID, "completed", 100)
	helpers.AssertError(t, err)
	helpers.AssertTrue(t, strings.Contains(err.Error(), "invalid task status transition: failed -> completed"))

	failed, err := readTaskRow(t, db, failedTaskID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "failed", failed.status)
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
