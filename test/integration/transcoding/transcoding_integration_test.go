package transcoding_test

import (
	"context"
	"testing"

	"streamgate/pkg/models"
	"streamgate/pkg/service"
	"streamgate/pkg/storage"
	"streamgate/test/helpers"

	"github.com/stretchr/testify/require"
)

// MockQueue implements models.TranscodingQueue for testing
type MockQueue struct {
	tasks map[string]*models.TranscodingTask
}

func NewMockQueue() *MockQueue {
	return &MockQueue{
		tasks: make(map[string]*models.TranscodingTask),
	}
}

func (m *MockQueue) Enqueue(task *models.TranscodingTask) error {
	m.tasks[task.ID] = task
	return nil
}

func (m *MockQueue) Dequeue(ctx context.Context) (*models.TranscodingTask, error) {
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

func (m *MockQueue) Ack(taskID string) error {
	return nil
}

func (m *MockQueue) Nak(taskID string) error {
	return nil
}

func (m *MockQueue) Depth() (int, error) {
	count := 0
	for _, task := range m.tasks {
		if task.Status == "pending" {
			count++
		}
	}
	return count, nil
}

func newTranscodingService(t *testing.T, db storage.DB) *service.TranscodingService {
	t.Helper()
	queue := NewMockQueue()
	return service.NewTranscodingService(db, queue)
}

func TestTranscodingService_Transcode(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	svc := newTranscodingService(t, db)
	taskID, err := svc.Transcode(context.Background(), "00000000-0000-0000-0000-000000000001", "1080p", "http://localhost:8080/input.mp4", 5, "0xOwner")
	require.NoError(t, err)
	require.NotEmpty(t, taskID)
}

func TestTranscodingService_GetTranscodingStatus(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	svc := newTranscodingService(t, db)
	taskID, err := svc.Transcode(context.Background(), "00000000-0000-0000-0000-000000000002", "720p", "http://localhost:8080/input.mp4", 5, "0xOwner")
	require.NoError(t, err)

	task, err := svc.GetTranscodingStatus(context.Background(), taskID)
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Equal(t, "pending", task.Status)
}

func TestTranscodingService_UpdateTaskStatus(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	svc := newTranscodingService(t, db)
	taskID, err := svc.Transcode(context.Background(), "00000000-0000-0000-0000-000000000003", "480p", "http://localhost:8080/input.mp4", 5, "0xOwner")
	require.NoError(t, err)

	err = svc.UpdateTaskStatus(context.Background(), taskID, "processing", 50)
	require.NoError(t, err)

	task, err := svc.GetTranscodingStatus(context.Background(), taskID)
	require.NoError(t, err)
	require.Equal(t, "processing", task.Status)
	require.Equal(t, 50, task.Progress)
}

func TestTranscodingService_ListTasks(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	svc := newTranscodingService(t, db)

	for i := 0; i < 3; i++ {
		_, err := svc.Transcode(context.Background(), "00000000-0000-0000-0000-000000000001", "1080p", "http://localhost:8080/input.mp4", 5, "0xOwner")
		require.NoError(t, err)
	}

	tasks, err := svc.ListTasks(context.Background(), "00000000-0000-0000-0000-000000000001", "0xOwner", 10, 0)
	require.NoError(t, err)
	helpers.AssertTrue(t, len(tasks) >= 3)
}

func TestTranscodingService_CancelTask(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	svc := newTranscodingService(t, db)
	taskID, err := svc.Transcode(context.Background(), "00000000-0000-0000-0000-000000000004", "1080p", "http://localhost:8080/input.mp4", 5, "0xOwner")
	require.NoError(t, err)

	err = svc.CancelTask(context.Background(), taskID)
	require.NoError(t, err)

	task, err := svc.GetTranscodingStatus(context.Background(), taskID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", task.Status)
}

func TestTranscodingService_DeleteTask(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	svc := newTranscodingService(t, db)
	taskID, err := svc.Transcode(context.Background(), "00000000-0000-0000-0000-000000000005", "1080p", "http://localhost:8080/input.mp4", 5, "0xOwner")
	require.NoError(t, err)

	err = svc.DeleteTask(context.Background(), taskID)
	require.NoError(t, err)

	_, err = svc.GetTranscodingStatus(context.Background(), taskID)
	helpers.AssertError(t, err)
}

func TestTranscodingService_Profiles(t *testing.T) {
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	svc := newTranscodingService(t, db)

	profile, err := svc.GetProfile("1080p")
	require.NoError(t, err)
	require.NotNil(t, profile)
	require.Equal(t, "1080p", profile.Name)

	profiles := svc.ListProfiles()
	helpers.AssertTrue(t, len(profiles) > 0)
}
