package transcoding_test

import (
	"testing"

	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

// MockQueue implements service.TranscodingQueue for testing
type MockQueue struct {
	tasks map[string]*service.TranscodingTask
}

func NewMockQueue() *MockQueue {
	return &MockQueue{
		tasks: make(map[string]*service.TranscodingTask),
	}
}

func (m *MockQueue) Enqueue(task *service.TranscodingTask) error {
	m.tasks[task.ID] = task
	return nil
}

func (m *MockQueue) Dequeue() (*service.TranscodingTask, error) {
	for _, task := range m.tasks {
		if task.Status == "pending" {
			return task, nil
		}
	}
	return nil, nil
}

func (m *MockQueue) GetStatus(taskID string) (string, error) {
	if task, exists := m.tasks[taskID]; exists {
		return task.Status, nil
	}
	return "", nil
}

func TestTranscodingService_Transcode(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	queue := NewMockQueue()
	transcodingService := service.NewTranscodingService(db.GetDB(), queue)

	// Create transcoding task
	taskID, err := transcodingService.Transcode("content-123", "1080p", "http://localhost:8080/input.mp4", 5)
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, taskID)
}

func TestTranscodingService_GetTranscodingStatus(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	queue := NewMockQueue()
	transcodingService := service.NewTranscodingService(db.GetDB(), queue)

	// Create transcoding task
	taskID, err := transcodingService.Transcode("content-123", "720p", "http://localhost:8080/input.mp4", 5)
	helpers.AssertNoError(t, err)

	// Get task status
	task, err := transcodingService.GetTranscodingStatus(taskID)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, task)
	helpers.AssertEqual(t, "pending", task.Status)
}

func TestTranscodingService_UpdateTaskStatus(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	queue := NewMockQueue()
	transcodingService := service.NewTranscodingService(db.GetDB(), queue)

	// Create transcoding task
	taskID, err := transcodingService.Transcode("content-123", "480p", "http://localhost:8080/input.mp4", 5)
	helpers.AssertNoError(t, err)

	// Update task status
	err = transcodingService.UpdateTaskStatus(taskID, "processing", 50)
	helpers.AssertNoError(t, err)

	// Verify update
	task, err := transcodingService.GetTranscodingStatus(taskID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "processing", task.Status)
	helpers.AssertEqual(t, 50, task.Progress)
}

func TestTranscodingService_ListTasks(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	queue := NewMockQueue()
	transcodingService := service.NewTranscodingService(db.GetDB(), queue)

	// Create multiple tasks
	for i := 0; i < 3; i++ {
		_, err := transcodingService.Transcode("content-123", "1080p", "http://localhost:8080/input.mp4", 5)
		helpers.AssertNoError(t, err)
	}

	// List tasks
	tasks, err := transcodingService.ListTasks("content-123", 10, 0)
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

	queue := NewMockQueue()
	transcodingService := service.NewTranscodingService(db.GetDB(), queue)

	// Create transcoding task
	taskID, err := transcodingService.Transcode("content-123", "1080p", "http://localhost:8080/input.mp4", 5)
	helpers.AssertNoError(t, err)

	// Cancel task
	err = transcodingService.CancelTask(taskID)
	helpers.AssertNoError(t, err)

	// Verify cancellation
	task, err := transcodingService.GetTranscodingStatus(taskID)
	helpers.AssertNoError(t, err)
	helpers.AssertEqual(t, "cancelled", task.Status)
}

func TestTranscodingService_DeleteTask(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	queue := NewMockQueue()
	transcodingService := service.NewTranscodingService(db.GetDB(), queue)

	// Create transcoding task
	taskID, err := transcodingService.Transcode("content-123", "1080p", "http://localhost:8080/input.mp4", 5)
	helpers.AssertNoError(t, err)

	// Delete task
	err = transcodingService.DeleteTask(taskID)
	helpers.AssertNoError(t, err)

	// Verify deletion
	_, err = transcodingService.GetTranscodingStatus(taskID)
	helpers.AssertError(t, err)
}

func TestTranscodingService_Profiles(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	queue := NewMockQueue()
	transcodingService := service.NewTranscodingService(db.GetDB(), queue)

	// Get profile
	profile, err := transcodingService.GetProfile("1080p")
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, profile)
	helpers.AssertEqual(t, "1080p", profile.Name)

	// List all profiles
	profiles := transcodingService.ListProfiles()
	helpers.AssertTrue(t, len(profiles) > 0)
}
