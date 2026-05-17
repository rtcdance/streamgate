package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTranscodingQueue struct {
	lastTask *TranscodingTask
}

func (q *fakeTranscodingQueue) Enqueue(task *TranscodingTask) error {
	q.lastTask = task
	return nil
}

func (q *fakeTranscodingQueue) Dequeue(ctx context.Context) (*TranscodingTask, error) {
	return q.lastTask, nil
}

func (q *fakeTranscodingQueue) GetStatus(taskID string) (string, error) {
	if q.lastTask != nil && q.lastTask.ID == taskID {
		return q.lastTask.Status, nil
	}
	return "", assert.AnError
}

func (q *fakeTranscodingQueue) Ack(taskID string) error {
	return nil
}

func (q *fakeTranscodingQueue) Nak(taskID string) error {
	return nil
}

func (q *fakeTranscodingQueue) Depth() (int, error) {
	return 0, nil
}

func TestTranscodingService_Transcode(t *testing.T) {
	queue := &fakeTranscodingQueue{}
	service := NewTranscodingService(nil, queue)

	taskID, err := service.Transcode(context.Background(), "content-1", "720p", "https://example.com/input.mp4", 5, "")
	require.NoError(t, err)
	assert.NotEmpty(t, taskID)
	require.NotNil(t, queue.lastTask)
	assert.Equal(t, taskID, queue.lastTask.ID)
	assert.Equal(t, "content-1", queue.lastTask.ContentID)
	assert.Equal(t, "720p", queue.lastTask.Profile)
	assert.Equal(t, "pending", queue.lastTask.Status)
	assert.Equal(t, 5, queue.lastTask.Priority)
}

func TestTranscodingService_TranscodeRejectsInvalidProfile(t *testing.T) {
	service := NewTranscodingService(nil, &fakeTranscodingQueue{})

	_, err := service.Transcode(context.Background(), "content-1", "144p", "https://example.com/input.mp4", 1, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid profile")
}

func TestTranscodingService_InMemoryStatusFlow(t *testing.T) {
	queue := &fakeTranscodingQueue{}
	service := NewTranscodingService(nil, queue)

	taskID, err := service.Transcode(context.Background(), "content-2", "1080p", "https://example.com/input.mp4", 7, "")
	require.NoError(t, err)

	task, err := service.GetTranscodingStatus(context.Background(), taskID)
	require.NoError(t, err)
	assert.Equal(t, "pending", task.Status)
	assert.Equal(t, 0, task.Progress)

	require.NoError(t, service.StartTask(context.Background(), taskID))
	task, err = service.GetTranscodingStatus(context.Background(), taskID)
	require.NoError(t, err)
	assert.Equal(t, "processing", task.Status)
	require.NotNil(t, task.StartedAt)

	require.NoError(t, service.UpdateTaskProgress(context.Background(), taskID, 45))
	task, err = service.GetTranscodingStatus(context.Background(), taskID)
	require.NoError(t, err)
	assert.Equal(t, 45, task.Progress)

	require.NoError(t, service.CompleteTask(context.Background(), taskID, "https://example.com/output.m3u8"))
	task, err = service.GetTranscodingStatus(context.Background(), taskID)
	require.NoError(t, err)
	assert.Equal(t, "completed", task.Status)
	assert.Equal(t, 100, task.Progress)
	assert.Equal(t, "https://example.com/output.m3u8", task.OutputURL)
	require.NotNil(t, task.CompletedAt)

	list, err := service.ListTasks(context.Background(), "content-2", "", 10, 0)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, taskID, list[0].ID)

	pending, err := service.GetPendingTasks(context.Background(), 10)
	require.NoError(t, err)
	assert.Len(t, pending, 0)
}

func TestTranscodingService_CancelAndDeleteFallback(t *testing.T) {
	queue := &fakeTranscodingQueue{}
	service := NewTranscodingService(nil, queue)

	taskID, err := service.Transcode(context.Background(), "content-3", "480p", "https://example.com/input.mp4", 3, "")
	require.NoError(t, err)

	require.NoError(t, service.CancelTask(context.Background(), taskID))
	task, err := service.GetTranscodingStatus(context.Background(), taskID)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", task.Status)
	require.NotNil(t, task.CompletedAt)

	require.NoError(t, service.DeleteTask(context.Background(), taskID))
	_, err = service.GetTranscodingStatus(context.Background(), taskID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
}

func TestTranscodingService_ListTasksFilterAndPagination(t *testing.T) {
	queue := &fakeTranscodingQueue{}
	svc := NewTranscodingService(nil, queue)

	id1, err := svc.Transcode(context.Background(), "content-a", "720p", "https://example.com/a1.mp4", 1, "")
	require.NoError(t, err)
	id2, err := svc.Transcode(context.Background(), "content-a", "480p", "https://example.com/a2.mp4", 2, "")
	require.NoError(t, err)
	id3, err := svc.Transcode(context.Background(), "content-b", "1080p", "https://example.com/b1.mp4", 3, "")
	require.NoError(t, err)

	filtered, err := svc.ListTasks(context.Background(), "content-a", "", 10, 0)
	require.NoError(t, err)
	require.Len(t, filtered, 2)
	assert.Equal(t, "content-a", filtered[0].ContentID)
	assert.Equal(t, "content-a", filtered[1].ContentID)

	paged, err := svc.ListTasks(context.Background(), "", "", 1, 1)
	require.NoError(t, err)
	require.Len(t, paged, 1)
	assert.Contains(t, []string{id1, id2, id3}, paged[0].ID)

	emptyPage, err := svc.ListTasks(context.Background(), "", "", 2, 10)
	require.NoError(t, err)
	assert.Empty(t, emptyPage)
}
