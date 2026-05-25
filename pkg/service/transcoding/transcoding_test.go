package transcoding

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/models"
	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockDB struct {
	queryFn    func(ctx context.Context, query string, args ...interface{}) (stg.Rows, error)
	queryRowFn func(ctx context.Context, query string, args ...interface{}) *stg.CancelRow
	execFn     func(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	beginFn    func(ctx context.Context) (*sql.Tx, error)
	inTxFn     func(ctx context.Context, fn func(tx *sql.Tx) error) error
	pingFn     func(ctx context.Context) error
	closeFn    func() error
}

func (m *mockDB) Query(ctx context.Context, query string, args ...interface{}) (stg.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}
func (m *mockDB) QueryRow(ctx context.Context, query string, args ...interface{}) *stg.CancelRow {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, query, args...)
	}
	return stg.NewErrorCancelRow(errors.New("not implemented"))
}
func (m *mockDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if m.execFn != nil {
		return m.execFn(ctx, query, args...)
	}
	return nil, errors.New("not implemented")
}
func (m *mockDB) Begin(ctx context.Context) (*sql.Tx, error) {
	if m.beginFn != nil {
		return m.beginFn(ctx)
	}
	return nil, errors.New("not implemented")
}
func (m *mockDB) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if m.inTxFn != nil {
		return m.inTxFn(ctx, fn)
	}
	return errors.New("not implemented")
}
func (m *mockDB) Ping(ctx context.Context) error {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return nil
}
func (m *mockDB) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

type mockResult struct {
	rowsAffected int64
	lastInsertID int64
	err          error
}

func (m *mockResult) LastInsertId() (int64, error) { return m.lastInsertID, nil }
func (m *mockResult) RowsAffected() (int64, error) { return m.rowsAffected, m.err }

type mockSegmentStorage struct {
	mu                sync.RWMutex
	data              map[string][]byte
	uploadErr         error
	uploadStreamErr   error
	downloadErr       error
	downloadStreamErr error
	deleteErr         error
	existsResult      bool
	existsErr         error
	listResult        []string
	listErr           error
}

func newMockSegmentStorage() *mockSegmentStorage {
	return &mockSegmentStorage{data: make(map[string][]byte)}
}

func (m *mockSegmentStorage) Upload(_ context.Context, bucket, key string, data []byte) error {
	if m.uploadErr != nil {
		return m.uploadErr
	}
	m.mu.Lock()
	m.data[bucket+"/"+key] = data
	m.mu.Unlock()
	return nil
}

func (m *mockSegmentStorage) UploadStream(_ context.Context, bucket, key string, reader io.Reader, size int64) error {
	if m.uploadStreamErr != nil {
		return m.uploadStreamErr
	}
	data, _ := io.ReadAll(reader)
	m.mu.Lock()
	m.data[bucket+"/"+key] = data
	m.mu.Unlock()
	return nil
}

func (m *mockSegmentStorage) UploadWithContentType(_ context.Context, bucket, key string, data []byte, _ string) error {
	if m.uploadErr != nil {
		return m.uploadErr
	}
	m.mu.Lock()
	m.data[bucket+"/"+key] = data
	m.mu.Unlock()
	return nil
}

func (m *mockSegmentStorage) UploadStreamWithContentType(_ context.Context, bucket, key string, reader io.Reader, size int64, _ string) error {
	if m.uploadStreamErr != nil {
		return m.uploadStreamErr
	}
	data, _ := io.ReadAll(reader)
	m.mu.Lock()
	m.data[bucket+"/"+key] = data
	m.mu.Unlock()
	return nil
}

func (m *mockSegmentStorage) Download(_ context.Context, bucket, key string) ([]byte, error) {
	if m.downloadErr != nil {
		return nil, m.downloadErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if d, ok := m.data[bucket+"/"+key]; ok {
		return d, nil
	}
	return nil, errors.New("not found")
}

func (m *mockSegmentStorage) DownloadStream(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	if m.downloadStreamErr != nil {
		return nil, m.downloadStreamErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if d, ok := m.data[bucket+"/"+key]; ok {
		return io.NopCloser(strings.NewReader(string(d))), nil
	}
	return nil, errors.New("not found")
}

func (m *mockSegmentStorage) Delete(_ context.Context, bucket, key string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.mu.Lock()
	delete(m.data, bucket+"/"+key)
	m.mu.Unlock()
	return nil
}

func (m *mockSegmentStorage) ListObjects(_ context.Context, bucket, prefix string) ([]string, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.listResult != nil {
		return m.listResult, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, bucket+"/"+prefix) {
			keys = append(keys, strings.TrimPrefix(k, bucket+"/"))
		}
	}
	return keys, nil
}

func (m *mockSegmentStorage) Exists(_ context.Context, bucket, key string) (bool, error) {
	if m.existsErr != nil {
		return false, m.existsErr
	}
	_, ok := m.data[bucket+"/"+key]
	return ok, nil
}

type mockVideoTranscoder struct {
	transcodeErr error
	progressCalls []float64
}

func (m *mockVideoTranscoder) TranscodeHLS(_ context.Context, _, _, _ string, progressFn func(progress float64)) error {
	if m.transcodeErr != nil {
		return m.transcodeErr
	}
	for _, p := range m.progressCalls {
		progressFn(p)
	}
	return nil
}

func TestNewTranscodingService(t *testing.T) {
	t.Run("with defaults", func(t *testing.T) {
		svc := NewTranscodingService(nil, NewMemoryTranscodingQueue())
		require.NotNil(t, svc)
		assert.NotNil(t, svc.httpClient)
		assert.NotNil(t, svc.tasks)
	})

	t.Run("with options", func(t *testing.T) {
		svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(),
			WithTranscoder(&mockVideoTranscoder{}),
			WithStorage(newMockSegmentStorage()),
			WithLogger(zap.NewNop()),
			WithUploadConcurrency(10),
			WithWorkerCount(4),
			WithMinWorkers(2),
			WithMaxWorkers(8),
		)
		require.NotNil(t, svc)
		assert.Equal(t, 10, svc.uploadConcurrency)
		assert.Equal(t, 4, svc.workerCount)
		assert.Equal(t, 2, svc.minWorkers)
		assert.Equal(t, 8, svc.maxWorkers)
	})

	t.Run("zero values ignored", func(t *testing.T) {
		svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(),
			WithUploadConcurrency(0),
			WithWorkerCount(0),
			WithMinWorkers(0),
			WithMaxWorkers(0),
		)
		require.NotNil(t, svc)
		assert.Equal(t, 0, svc.uploadConcurrency)
		assert.Equal(t, 0, svc.workerCount)
	})
}

func TestTranscodingService_Transcode(t *testing.T) {
	t.Run("success with in-memory store", func(t *testing.T) {
		queue := NewMemoryTranscodingQueue()
		svc := NewTranscodingService(nil, queue)
		taskID, err := svc.Transcode(context.Background(), "content-1", "720p", "https://example.com/input.mp4", 5, "0xOwner")
		require.NoError(t, err)
		assert.NotEmpty(t, taskID)
	})

	t.Run("invalid profile", func(t *testing.T) {
		svc := NewTranscodingService(nil, NewMemoryTranscodingQueue())
		_, err := svc.Transcode(context.Background(), "content-1", "144p", "https://example.com/input.mp4", 1, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid profile")
	})

	t.Run("with db save", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{}, nil
			},
		}
		queue := NewMemoryTranscodingQueue()
		svc := NewTranscodingService(db, queue)
		taskID, err := svc.Transcode(context.Background(), "content-1", "720p", "https://example.com/input.mp4", 5, "")
		require.NoError(t, err)
		assert.NotEmpty(t, taskID)
	})

	t.Run("db save error", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return nil, errors.New("db error")
			},
		}
		queue := NewMemoryTranscodingQueue()
		svc := NewTranscodingService(db, queue)
		_, err := svc.Transcode(context.Background(), "content-1", "720p", "https://example.com/input.mp4", 5, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to save task")
	})

	t.Run("enqueue error", func(t *testing.T) {
		queue := &failingQueue{enqueueErr: errors.New("queue full")}
		svc := NewTranscodingService(nil, queue)
		_, err := svc.Transcode(context.Background(), "content-1", "720p", "https://example.com/input.mp4", 5, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to enqueue task")
	})

	t.Run("nil queue still creates task", func(t *testing.T) {
		svc := NewTranscodingService(nil, nil)
		taskID, err := svc.Transcode(context.Background(), "content-1", "720p", "https://example.com/input.mp4", 5, "")
		require.NoError(t, err)
		assert.NotEmpty(t, taskID)
	})
}

type failingQueue struct {
	enqueueErr error
}

func (q *failingQueue) Enqueue(_ *models.TranscodingTask) error  { return q.enqueueErr }
func (q *failingQueue) Dequeue(_ context.Context) (*models.TranscodingTask, error) {
	return nil, errors.New("empty")
}
func (q *failingQueue) GetStatus(_ string) (string, error) { return "", nil }
func (q *failingQueue) Ack(_ string) error                  { return nil }
func (q *failingQueue) Nak(_ string) error                  { return nil }
func (q *failingQueue) Depth() (int, error)                 { return 0, nil }

func TestTranscodingService_InMemoryStatusFlow(t *testing.T) {
	queue := NewMemoryTranscodingQueue()
	svc := NewTranscodingService(nil, queue)

	taskID, err := svc.Transcode(context.Background(), "content-2", "1080p", "https://example.com/input.mp4", 7, "")
	require.NoError(t, err)

	task, err := svc.GetTranscodingStatus(context.Background(), taskID)
	require.NoError(t, err)
	assert.Equal(t, "pending", task.Status)
	assert.Equal(t, 0, task.Progress)

	require.NoError(t, svc.StartTask(context.Background(), taskID))
	task, err = svc.GetTranscodingStatus(context.Background(), taskID)
	require.NoError(t, err)
	assert.Equal(t, "processing", task.Status)
	require.NotNil(t, task.StartedAt)

	require.NoError(t, svc.UpdateTaskProgress(context.Background(), taskID, 45))
	task, err = svc.GetTranscodingStatus(context.Background(), taskID)
	require.NoError(t, err)
	assert.Equal(t, 45, task.Progress)

	require.NoError(t, svc.CompleteTask(context.Background(), taskID, "streams/content-2/720p"))
	task, err = svc.GetTranscodingStatus(context.Background(), taskID)
	require.NoError(t, err)
	assert.Equal(t, "completed", task.Status)
	assert.Equal(t, 100, task.Progress)
	require.NotNil(t, task.CompletedAt)

	pending, err := svc.GetPendingTasks(context.Background(), 10)
	require.NoError(t, err)
	assert.Len(t, pending, 0)
}

func TestTranscodingService_CancelAndDelete(t *testing.T) {
	queue := NewMemoryTranscodingQueue()
	svc := NewTranscodingService(nil, queue)

	taskID, err := svc.Transcode(context.Background(), "content-3", "480p", "https://example.com/input.mp4", 3, "")
	require.NoError(t, err)

	require.NoError(t, svc.CancelTask(context.Background(), taskID))
	task, err := svc.GetTranscodingStatus(context.Background(), taskID)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", task.Status)
	require.NotNil(t, task.CompletedAt)

	require.NoError(t, svc.DeleteTask(context.Background(), taskID))
	_, err = svc.GetTranscodingStatus(context.Background(), taskID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
}

func TestTranscodingService_CancelTask_CannotCancelCompleted(t *testing.T) {
	queue := NewMemoryTranscodingQueue()
	svc := NewTranscodingService(nil, queue)

	taskID, _ := svc.Transcode(context.Background(), "content-x", "720p", "https://example.com/input.mp4", 1, "")
	_ = svc.CompleteTask(context.Background(), taskID, "out")

	err := svc.CancelTask(context.Background(), taskID)
	require.NoError(t, err)

	task, _ := svc.GetTranscodingStatus(context.Background(), taskID)
	assert.Equal(t, "completed", task.Status)
}

func TestTranscodingService_FailTask(t *testing.T) {
	queue := NewMemoryTranscodingQueue()
	svc := NewTranscodingService(nil, queue)

	taskID, _ := svc.Transcode(context.Background(), "content-f", "720p", "https://example.com/input.mp4", 1, "")
	err := svc.FailTask(context.Background(), taskID, "transcode error")
	require.NoError(t, err)

	task, _ := svc.GetTranscodingStatus(context.Background(), taskID)
	assert.Equal(t, "failed", task.Status)
	assert.Equal(t, "transcode error", task.Error)
	require.NotNil(t, task.CompletedAt)
}

func TestTranscodingService_UpdateTaskStatus(t *testing.T) {
	queue := NewMemoryTranscodingQueue()
	svc := NewTranscodingService(nil, queue)

	taskID, _ := svc.Transcode(context.Background(), "content-u", "720p", "https://example.com/input.mp4", 1, "")
	err := svc.UpdateTaskStatus(context.Background(), taskID, "processing", 50)
	require.NoError(t, err)

	task, _ := svc.GetTranscodingStatus(context.Background(), taskID)
	assert.Equal(t, "processing", task.Status)
	assert.Equal(t, 50, task.Progress)
}

func TestTranscodingService_UpdateTaskStatus_NotFound(t *testing.T) {
	queue := NewMemoryTranscodingQueue()
	svc := NewTranscodingService(nil, queue)
	err := svc.UpdateTaskStatus(context.Background(), "nonexistent", "processing", 50)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
}

func TestTranscodingService_GetTranscodingStatus_NotFound(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue())
	_, err := svc.GetTranscodingStatus(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
}

func TestTranscodingService_ListTasks(t *testing.T) {
	queue := NewMemoryTranscodingQueue()
	svc := NewTranscodingService(nil, queue)

	id1, _ := svc.Transcode(context.Background(), "content-a", "720p", "https://example.com/a1.mp4", 1, "")
	id2, _ := svc.Transcode(context.Background(), "content-a", "480p", "https://example.com/a2.mp4", 2, "")
	_, _ = svc.Transcode(context.Background(), "content-b", "1080p", "https://example.com/b1.mp4", 3, "")

	filtered, err := svc.ListTasks(context.Background(), "content-a", "", 10, 0)
	require.NoError(t, err)
	require.Len(t, filtered, 2)
	assert.Equal(t, "content-a", filtered[0].ContentID)
	assert.Equal(t, "content-a", filtered[1].ContentID)

	paged, err := svc.ListTasks(context.Background(), "", "", 1, 1)
	require.NoError(t, err)
	require.Len(t, paged, 1)
	assert.Contains(t, []string{id1, id2}, paged[0].ID)

	emptyPage, err := svc.ListTasks(context.Background(), "", "", 2, 10)
	require.NoError(t, err)
	assert.Empty(t, emptyPage)

	ownerFiltered, err := svc.ListTasks(context.Background(), "", "nonexistent", 10, 0)
	require.NoError(t, err)
	assert.Empty(t, ownerFiltered)
}

func TestTranscodingService_GetPendingTasks(t *testing.T) {
	queue := NewMemoryTranscodingQueue()
	svc := NewTranscodingService(nil, queue)

	_, _ = svc.Transcode(context.Background(), "content-p1", "720p", "https://example.com/p1.mp4", 1, "")
	taskID, _ := svc.Transcode(context.Background(), "content-p2", "480p", "https://example.com/p2.mp4", 2, "")
	_ = svc.CompleteTask(context.Background(), taskID, "out")

	pending, err := svc.GetPendingTasks(context.Background(), 10)
	require.NoError(t, err)
	assert.Len(t, pending, 1)
	assert.Equal(t, "pending", pending[0].Status)
}

func TestTranscodingService_GetProfile(t *testing.T) {
	svc := NewTranscodingService(nil, nil)

	t.Run("existing profile", func(t *testing.T) {
		profile, err := svc.GetProfile("720p")
		require.NoError(t, err)
		assert.Equal(t, "720p", profile.Name)
		assert.Equal(t, "h264", profile.VideoCodec)
		assert.Equal(t, "1280x720", profile.Resolution)
		assert.Equal(t, 2500, profile.Bitrate)
	})

	t.Run("nonexistent profile", func(t *testing.T) {
		_, err := svc.GetProfile("144p")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "profile not found")
	})
}

func TestTranscodingService_ListProfiles(t *testing.T) {
	svc := NewTranscodingService(nil, nil)
	profiles := svc.ListProfiles()
	assert.Len(t, profiles, 4)
	names := make(map[string]bool)
	for _, p := range profiles {
		names[p.Name] = true
	}
	assert.True(t, names["1080p"])
	assert.True(t, names["720p"])
	assert.True(t, names["480p"])
	assert.True(t, names["360p"])
}

func TestDefaultProfiles(t *testing.T) {
	assert.Len(t, DefaultProfiles, 4)
	for name, profile := range DefaultProfiles {
		assert.Equal(t, name, profile.Name)
		assert.Equal(t, "h264", profile.VideoCodec)
		assert.Equal(t, "aac", profile.AudioCodec)
		assert.Equal(t, "hls", profile.Format)
		assert.Equal(t, 30, profile.Framerate)
	}
}

func TestTranscodingService_RegisterPostTranscodeHook(t *testing.T) {
	svc := NewTranscodingService(nil, nil)
	svc.RegisterPostTranscodeHook(func(_ context.Context, _, _, _ string) {})
	assert.Len(t, svc.transcodeHooks, 1)
}

func TestTranscodingService_StartWorker_NoTranscoder(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(), WithLogger(zap.NewNop()))
	svc.StartWorker(zap.NewNop())
	assert.Equal(t, int32(0), atomic.LoadInt32(&svc.running))
}

func TestTranscodingService_StartWorker_WithTranscoder(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(),
		WithTranscoder(&mockVideoTranscoder{}),
		WithLogger(zap.NewNop()),
		WithMinWorkers(1),
		WithMaxWorkers(2),
	)
	svc.StartWorker(zap.NewNop())
	assert.Equal(t, int32(1), atomic.LoadInt32(&svc.running))
	svc.StopWorker()
	assert.Equal(t, int32(0), atomic.LoadInt32(&svc.running))
}

func TestTranscodingService_StartWorker_RestartsExisting(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(),
		WithTranscoder(&mockVideoTranscoder{}),
		WithLogger(zap.NewNop()),
		WithMinWorkers(1),
		WithMaxWorkers(2),
	)
	svc.StartWorker(zap.NewNop())
	assert.Equal(t, int32(1), atomic.LoadInt32(&svc.running))
	svc.StartWorker(zap.NewNop())
	assert.Equal(t, int32(1), atomic.LoadInt32(&svc.running))
	svc.StopWorker()
}

func TestTranscodingService_StopWorker(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(),
		WithTranscoder(&mockVideoTranscoder{}),
		WithLogger(zap.NewNop()),
		WithMinWorkers(1),
	)
	svc.StartWorker(zap.NewNop())
	svc.StopWorker()
	assert.Equal(t, int32(0), atomic.LoadInt32(&svc.running))
}

func TestTranscodingService_Close(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(),
		WithTranscoder(&mockVideoTranscoder{}),
		WithLogger(zap.NewNop()),
		WithMinWorkers(1),
	)
	svc.StartWorker(zap.NewNop())
	svc.Close()
	assert.Equal(t, int32(0), atomic.LoadInt32(&svc.running))
}

func TestTranscodingService_Close_WithoutStart(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(), WithLogger(zap.NewNop()))
	assert.NotPanics(t, func() { svc.Close() })
}

func TestMemoryTranscodingQueue_EnqueueDequeue(t *testing.T) {
	q := NewMemoryTranscodingQueue()

	task := &models.TranscodingTask{
		ID:        "task-1",
		ContentID: "content-1",
		Profile:   "720p",
		Status:    "pending",
	}
	require.NoError(t, q.Enqueue(task))

	depth, err := q.Depth()
	require.NoError(t, err)
	assert.Equal(t, 1, depth)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dequeued, err := q.Dequeue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "task-1", dequeued.ID)
	assert.Equal(t, "720p", dequeued.Profile)

	depth, _ = q.Depth()
	assert.Equal(t, 0, depth)
}

func TestMemoryTranscodingQueue_DequeueCancelled(t *testing.T) {
	q := NewMemoryTranscodingQueue()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := q.Dequeue(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestMemoryTranscodingQueue_GetStatus(t *testing.T) {
	q := NewMemoryTranscodingQueue()

	_, err := q.GetStatus("nonexistent")
	require.Error(t, err)

	task := &models.TranscodingTask{ID: "task-1", Status: "pending"}
	_ = q.Enqueue(task)

	status, err := q.GetStatus("task-1")
	require.NoError(t, err)
	assert.Equal(t, "pending", status)
}

func TestMemoryTranscodingQueue_AckNak(t *testing.T) {
	q := NewMemoryTranscodingQueue()
	assert.NoError(t, q.Ack("any"))
	assert.NoError(t, q.Nak("any"))
}

func TestMemoryTranscodingQueue_Depth(t *testing.T) {
	q := NewMemoryTranscodingQueue()
	depth, err := q.Depth()
	require.NoError(t, err)
	assert.Equal(t, 0, depth)

	_ = q.Enqueue(&models.TranscodingTask{ID: "t1", Status: "pending"})
	depth, _ = q.Depth()
	assert.Equal(t, 1, depth)
}

func TestGetRetryCount(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]interface{}
		want     int
	}{
		{"nil metadata", nil, 0},
		{"no retry_count", map[string]interface{}{}, 0},
		{"float64 retry_count", map[string]interface{}{"retry_count": float64(2)}, 2},
		{"int retry_count", map[string]interface{}{"retry_count": 3}, 3},
		{"wrong type", map[string]interface{}{"retry_count": "two"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &models.TranscodingTask{Metadata: tt.metadata}
			assert.Equal(t, tt.want, getRetryCount(task))
		})
	}
}

func TestSetRetryCount(t *testing.T) {
	t.Run("nil metadata", func(t *testing.T) {
		task := &models.TranscodingTask{}
		setRetryCount(task, 2)
		assert.Equal(t, 2, task.Metadata["retry_count"])
	})

	t.Run("existing metadata", func(t *testing.T) {
		task := &models.TranscodingTask{Metadata: map[string]interface{}{"key": "val"}}
		setRetryCount(task, 3)
		assert.Equal(t, 3, task.Metadata["retry_count"])
		assert.Equal(t, "val", task.Metadata["key"])
	})
}

func TestTranscodingService_storeTask_Eviction(t *testing.T) {
	svc := NewTranscodingService(nil, nil)
	svc.tasks = make(map[string]*models.TranscodingTask)

	for i := 0; i < 10001; i++ {
		now := time.Now()
		svc.tasks[fmt.Sprintf("task-%d", i)] = &models.TranscodingTask{
			ID:          fmt.Sprintf("task-%d", i),
			Status:      "completed",
			CompletedAt: &now,
			Metadata:    make(map[string]interface{}),
		}
	}

	svc.storeTask(&models.TranscodingTask{
		ID:       "new-task",
		Status:   "pending",
		Metadata: make(map[string]interface{}),
	})
	_, exists := svc.tasks["new-task"]
	assert.True(t, exists)
}

func TestTranscodingService_getTask_NilMetadata(t *testing.T) {
	svc := NewTranscodingService(nil, nil)
	svc.tasks = make(map[string]*models.TranscodingTask)
	svc.tasks["t1"] = &models.TranscodingTask{ID: "t1", Status: "pending"}

	task, err := svc.getTask("t1")
	require.NoError(t, err)
	assert.NotNil(t, task.Metadata)
}

func TestTranscodingService_FailTask_NotFound(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue())
	err := svc.FailTask(context.Background(), "nonexistent", "error")
	require.Error(t, err)
}

func TestTranscodingService_StartTask_NotFound(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue())
	err := svc.StartTask(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
}

func TestTranscodingService_UpdateTaskProgress_NotFound(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue())
	err := svc.UpdateTaskProgress(context.Background(), "nonexistent", 50)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
}

func TestTranscodingService_DeleteTask_NotFound(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue())
	err := svc.DeleteTask(context.Background(), "nonexistent")
	require.NoError(t, err)
}

func TestTranscodingService_CancelTask_NotFound(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue())
	err := svc.CancelTask(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
}

func TestTranscodingService_processTask(t *testing.T) {
	t.Run("successful transcode with storage", func(t *testing.T) {
		svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(),
			WithTranscoder(&mockVideoTranscoder{progressCalls: []float64{25, 50, 100}}),
			WithStorage(newMockSegmentStorage()),
			WithLogger(zap.NewNop()),
		)

		hookCalled := false
		svc.RegisterPostTranscodeHook(func(_ context.Context, contentID, profile, outputURL string) {
			hookCalled = true
			assert.Equal(t, "content-proc", contentID)
			assert.Equal(t, "720p", profile)
			_ = outputURL
		})

		task := &models.TranscodingTask{
			ID:        "task-proc",
			ContentID: "content-proc",
			Profile:   "720p",
			InputURL:  "/tmp/test-input.mp4",
			Metadata:  make(map[string]interface{}),
		}
		svc.storeTask(task)

		svc.processTask(context.Background(), task, zap.NewNop())

		updated, _ := svc.getTask("task-proc")
		assert.Equal(t, "completed", updated.Status)
		assert.Equal(t, 100, updated.Progress)
		assert.True(t, hookCalled)
	})

	t.Run("transcode failure", func(t *testing.T) {
		svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(),
			WithTranscoder(&mockVideoTranscoder{transcodeErr: errors.New("ffmpeg failed")}),
			WithLogger(zap.NewNop()),
		)

		task := &models.TranscodingTask{
			ID:        "task-fail",
			ContentID: "content-fail",
			Profile:   "720p",
			InputURL:  "/tmp/test-input.mp4",
			Metadata:  make(map[string]interface{}),
		}
		svc.storeTask(task)

		svc.processTask(context.Background(), task, zap.NewNop())

		updated, _ := svc.getTask("task-fail")
		assert.Equal(t, "failed", updated.Status)
		assert.Contains(t, updated.Error, "ffmpeg failed")
	})

	t.Run("unknown profile defaults to 720p", func(t *testing.T) {
		svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(),
			WithTranscoder(&mockVideoTranscoder{}),
			WithLogger(zap.NewNop()),
		)

		task := &models.TranscodingTask{
			ID:        "task-unknown",
			ContentID: "content-unknown",
			Profile:   "unknown_profile",
			InputURL:  "/tmp/test-input.mp4",
			Metadata:  make(map[string]interface{}),
		}
		svc.storeTask(task)

		svc.processTask(context.Background(), task, zap.NewNop())

		updated, _ := svc.getTask("task-unknown")
		assert.Equal(t, "completed", updated.Status)
	})
}

func TestTranscodingService_downloadInputFile_InvalidURL(t *testing.T) {
	svc := NewTranscodingService(nil, nil, WithLogger(zap.NewNop()))
	_, err := svc.downloadInputFile(context.Background(), "http://invalid-host-that-does-not-exist.example/video.mp4")
	require.Error(t, err)
}

func TestTranscodingService_downloadInputFile_BadStatus(t *testing.T) {
	svc := NewTranscodingService(nil, nil, WithLogger(zap.NewNop()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.downloadInputFile(ctx, "http://example.com/video.mp4")
	require.Error(t, err)
}

func TestTranscodingService_uploadSegments_EmptyDir(t *testing.T) {
	svc := NewTranscodingService(nil, nil,
		WithStorage(newMockSegmentStorage()),
		WithLogger(zap.NewNop()),
	)
	dir := t.TempDir()
	err := svc.uploadSegments(context.Background(), dir, "content-1", "720p")
	require.NoError(t, err)
}

func TestTranscodingService_uploadSegments_StorageError(t *testing.T) {
	store := newMockSegmentStorage()
	store.uploadStreamErr = errors.New("storage down")
	svc := NewTranscodingService(nil, nil,
		WithStorage(store),
		WithLogger(zap.NewNop()),
	)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/test.ts", []byte("data"), 0o644))
	err := svc.uploadSegments(context.Background(), dir, "content-1", "720p")
	require.Error(t, err)
}

func TestTranscodingService_uploadSegments_WalkError(t *testing.T) {
	svc := NewTranscodingService(nil, nil,
		WithStorage(newMockSegmentStorage()),
		WithLogger(zap.NewNop()),
	)
	err := svc.uploadSegments(context.Background(), "/nonexistent/path/that/does/not/exist", "content-1", "720p")
	require.Error(t, err)
}

func TestTranscodingService_uploadSegments_SuccessWithFiles(t *testing.T) {
	store := newMockSegmentStorage()
	svc := NewTranscodingService(nil, nil,
		WithStorage(store),
		WithLogger(zap.NewNop()),
		WithUploadConcurrency(2),
	)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/index.m3u8", []byte("#EXTM3U"), 0o644))
	require.NoError(t, os.WriteFile(dir+"/segment0.ts", []byte("ts-data"), 0o644))

	err := svc.uploadSegments(context.Background(), dir, "content-1", "720p")
	require.NoError(t, err)
}

type mockRowScanner struct {
	values []interface{}
	err    error
}

func (m *mockRowScanner) Scan(dest ...interface{}) error {
	if m.err != nil {
		return m.err
	}
	for i, v := range m.values {
		if i >= len(dest) {
			break
		}
		switch d := dest[i].(type) {
		case *string:
			*d = v.(string)
		case *int:
			*d = v.(int)
		case *time.Time:
			*d = v.(time.Time)
		case *[]byte:
			*d = v.([]byte)
		case *sql.NullString:
			ns := v.(sql.NullString)
			*d = ns
		case *sql.NullTime:
			nt := v.(sql.NullTime)
			*d = nt
		default:
			dest[i] = v
		}
	}
	return nil
}

type mockRows struct {
	index    int
	tasks    [][]interface{}
	closed   bool
	scanErr  error
	nextErr  error
}

func (m *mockRows) Next() bool {
	if m.index < len(m.tasks) {
		m.index++
		return true
	}
	return false
}

func (m *mockRows) Scan(dest ...interface{}) error {
	if m.scanErr != nil {
		return m.scanErr
	}
	row := m.tasks[m.index-1]
	for i, v := range row {
		if i >= len(dest) {
			break
		}
		switch d := dest[i].(type) {
		case *string:
			*d = v.(string)
		case *int:
			*d = v.(int)
		case *time.Time:
			*d = v.(time.Time)
		case *[]byte:
			*d = v.([]byte)
		case *sql.NullString:
			*d = v.(sql.NullString)
		case *sql.NullTime:
			*d = v.(sql.NullTime)
		default:
			dest[i] = v
		}
	}
	return nil
}

func (m *mockRows) Close() error {
	m.closed = true
	return nil
}

func (m *mockRows) Err() error {
	return m.nextErr
}

func TestDB_GetTranscodingStatus_Success(t *testing.T) {
	now := time.Now()
	metadataJSON, _ := json.Marshal(map[string]interface{}{"key": "val"})
	scanner := &mockRowScanner{
		values: []interface{}{
			"task-1", "content-1", "720p", "processing", 50, "https://input.mp4",
			"streams/content-1/720p", sql.NullString{String: "", Valid: false}, 5, now,
			sql.NullTime{Time: now, Valid: true}, sql.NullTime{Valid: false}, metadataJSON,
		},
	}
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(scanner)
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	task, err := svc.GetTranscodingStatus(context.Background(), "task-1")
	require.NoError(t, err)
	assert.Equal(t, "task-1", task.ID)
	assert.Equal(t, "processing", task.Status)
	assert.Equal(t, 50, task.Progress)
	require.NotNil(t, task.StartedAt)
	assert.Nil(t, task.CompletedAt)
}

func TestDB_GetTranscodingStatus_NotFound(t *testing.T) {
	scanner := &mockRowScanner{err: sql.ErrNoRows}
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(scanner)
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	_, err := svc.GetTranscodingStatus(context.Background(), "task-404")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
}

func TestDB_GetTranscodingStatus_ScanError(t *testing.T) {
	scanner := &mockRowScanner{err: errors.New("scan failure")}
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(scanner)
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	_, err := svc.GetTranscodingStatus(context.Background(), "task-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to query task")
}

func TestDB_GetTranscodingStatus_MetadataParseError(t *testing.T) {
	now := time.Now()
	scanner := &mockRowScanner{
		values: []interface{}{
			"task-1", "content-1", "720p", "pending", 0, "https://input.mp4",
			"", sql.NullString{Valid: false}, 1, now,
			sql.NullTime{Valid: false}, sql.NullTime{Valid: false}, []byte("invalid-json"),
		},
	}
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(scanner)
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	_, err := svc.GetTranscodingStatus(context.Background(), "task-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse metadata")
}

func TestDB_UpdateTaskStatus_Success(t *testing.T) {
	statusScanner := &mockRowScanner{values: []interface{}{"pending"}}
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(statusScanner)
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.UpdateTaskStatus(context.Background(), "task-1", "processing", 50)
	require.NoError(t, err)
}

func TestDB_UpdateTaskStatus_TaskNotFound(t *testing.T) {
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewErrorCancelRow(sql.ErrNoRows)
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.UpdateTaskStatus(context.Background(), "task-404", "processing", 50)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
}

func TestDB_UpdateTaskStatus_InvalidTransition(t *testing.T) {
	statusScanner := &mockRowScanner{values: []interface{}{"completed"}}
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(statusScanner)
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.UpdateTaskStatus(context.Background(), "task-1", "processing", 50)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid task status transition")
}

func TestDB_UpdateTaskStatus_ConcurrentChange(t *testing.T) {
	statusScanner := &mockRowScanner{values: []interface{}{"pending"}}
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(statusScanner)
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 0}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.UpdateTaskStatus(context.Background(), "task-1", "processing", 50)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task status changed concurrently")
}

func TestDB_UpdateTaskStatus_ExecError(t *testing.T) {
	statusScanner := &mockRowScanner{values: []interface{}{"pending"}}
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(statusScanner)
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return nil, errors.New("db exec error")
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.UpdateTaskStatus(context.Background(), "task-1", "processing", 50)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update task status")
}

func TestDB_StartTask_Success(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.StartTask(context.Background(), "task-1")
	require.NoError(t, err)
}

func TestDB_StartTask_NotPending(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 0}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.StartTask(context.Background(), "task-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task not in pending state")
}

func TestDB_StartTask_ExecError(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.StartTask(context.Background(), "task-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start task")
}

func TestDB_FailTask_Success(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.FailTask(context.Background(), "task-1", "transcode error")
	require.NoError(t, err)
}

func TestDB_FailTask_NotFailable(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 0}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.FailTask(context.Background(), "task-1", "error msg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task not in a failable state")
}

func TestDB_FailTask_ExecError(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.FailTask(context.Background(), "task-1", "error msg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fail task")
}

func TestDB_DeleteTask_Success(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.DeleteTask(context.Background(), "task-1")
	require.NoError(t, err)
}

func TestDB_DeleteTask_ExecError(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.DeleteTask(context.Background(), "task-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete task")
}

func TestDB_GetPendingTasks_Success(t *testing.T) {
	now := time.Now()
	metadataJSON, _ := json.Marshal(map[string]interface{}{})
	rowsData := [][]interface{}{
		{
			"task-1", "content-1", "720p", "pending", 0, "https://input.mp4",
			"", sql.NullString{Valid: false}, 5, now,
			sql.NullTime{Valid: false}, sql.NullTime{Valid: false}, metadataJSON,
		},
		{
			"task-2", "content-2", "480p", "pending", 0, "https://input2.mp4",
			"", sql.NullString{Valid: false}, 3, now,
			sql.NullTime{Valid: false}, sql.NullTime{Valid: false}, metadataJSON,
		},
	}
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return &mockRows{tasks: rowsData}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	tasks, err := svc.GetPendingTasks(context.Background(), 10)
	require.NoError(t, err)
	assert.Len(t, tasks, 2)
	assert.Equal(t, "task-1", tasks[0].ID)
	assert.Equal(t, "pending", tasks[0].Status)
	assert.Equal(t, "task-2", tasks[1].ID)
}

func TestDB_GetPendingTasks_QueryError(t *testing.T) {
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return nil, errors.New("db query error")
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	_, err := svc.GetPendingTasks(context.Background(), 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to query pending tasks")
}

func TestDB_GetPendingTasks_ScanError(t *testing.T) {
	now := time.Now()
	rowsData := [][]interface{}{
		{
			"task-1", "content-1", "720p", "pending", 0, "https://input.mp4",
			"", sql.NullString{Valid: false}, 5, now,
			sql.NullTime{Valid: false}, sql.NullTime{Valid: false}, []byte("{}"),
		},
	}
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return &mockRows{tasks: rowsData, scanErr: errors.New("scan error")}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	_, err := svc.GetPendingTasks(context.Background(), 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to scan task")
}

func TestDB_GetPendingTasks_MetadataParseError(t *testing.T) {
	now := time.Now()
	rowsData := [][]interface{}{
		{
			"task-1", "content-1", "720p", "pending", 0, "https://input.mp4",
			"", sql.NullString{Valid: false}, 5, now,
			sql.NullTime{Valid: false}, sql.NullTime{Valid: false}, []byte("bad-json"),
		},
	}
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return &mockRows{tasks: rowsData}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	_, err := svc.GetPendingTasks(context.Background(), 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse metadata")
}

func TestDB_CancelTask_Success(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.CancelTask(context.Background(), "task-1")
	require.NoError(t, err)
}

func TestDB_CancelTask_NotCancellable(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 0}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.CancelTask(context.Background(), "task-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task cannot be cancelled")
}

func TestDB_CancelTask_ExecError(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.CancelTask(context.Background(), "task-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to cancel task")
}

func TestDB_CancelTask_RowsAffectedError(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1, err: errors.New("rows affected error")}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.CancelTask(context.Background(), "task-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rows affected error")
}

func TestDB_UpdateTaskProgress_Success(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return &mockResult{rowsAffected: 1}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.UpdateTaskProgress(context.Background(), "task-1", 75)
	require.NoError(t, err)
}

func TestDB_UpdateTaskProgress_ExecError(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return nil, errors.New("db error")
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	err := svc.UpdateTaskProgress(context.Background(), "task-1", 75)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update task progress")
}

func TestDB_ListTasks_Success(t *testing.T) {
	now := time.Now()
	metadataJSON, _ := json.Marshal(map[string]interface{}{})
	rowsData := [][]interface{}{
		{
			"task-1", "content-1", "720p", "completed", 100, "https://input.mp4",
			"streams/out", sql.NullString{Valid: false}, 5, now,
			sql.NullTime{Time: now, Valid: true}, sql.NullTime{Time: now, Valid: true}, metadataJSON,
		},
		{
			"task-2", "content-1", "480p", "pending", 0, "https://input2.mp4",
			"", sql.NullString{Valid: false}, 3, now,
			sql.NullTime{Valid: false}, sql.NullTime{Valid: false}, metadataJSON,
		},
	}
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return &mockRows{tasks: rowsData}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	tasks, err := svc.ListTasks(context.Background(), "content-1", "", 10, 0)
	require.NoError(t, err)
	assert.Len(t, tasks, 2)
	assert.Equal(t, "task-1", tasks[0].ID)
	assert.Equal(t, "completed", tasks[0].Status)
	require.NotNil(t, tasks[0].StartedAt)
	require.NotNil(t, tasks[0].CompletedAt)
	assert.Equal(t, "task-2", tasks[1].ID)
	assert.Equal(t, "pending", tasks[1].Status)
	assert.Nil(t, tasks[1].StartedAt)
	assert.Nil(t, tasks[1].CompletedAt)
}

func TestDB_ListTasks_QueryError(t *testing.T) {
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return nil, errors.New("db query error")
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	_, err := svc.ListTasks(context.Background(), "content-1", "", 10, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to query tasks")
}

func TestDB_ListTasks_ScanError(t *testing.T) {
	now := time.Now()
	rowsData := [][]interface{}{
		{
			"task-1", "content-1", "720p", "pending", 0, "https://input.mp4",
			"", sql.NullString{Valid: false}, 5, now,
			sql.NullTime{Valid: false}, sql.NullTime{Valid: false}, []byte("{}"),
		},
	}
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return &mockRows{tasks: rowsData, scanErr: errors.New("scan error")}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	_, err := svc.ListTasks(context.Background(), "content-1", "", 10, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to scan task")
}

func TestDB_ListTasks_MetadataParseError(t *testing.T) {
	now := time.Now()
	rowsData := [][]interface{}{
		{
			"task-1", "content-1", "720p", "pending", 0, "https://input.mp4",
			"", sql.NullString{Valid: false}, 5, now,
			sql.NullTime{Valid: false}, sql.NullTime{Valid: false}, []byte("bad-json"),
		},
	}
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return &mockRows{tasks: rowsData}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	_, err := svc.ListTasks(context.Background(), "content-1", "", 10, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse metadata")
}

func TestDB_GetTranscodingStatus_WithNullables(t *testing.T) {
	now := time.Now()
	completedAt := now.Add(5 * time.Minute)
	scanner := &mockRowScanner{
		values: []interface{}{
			"task-1", "content-1", "720p", "completed", 100, "https://input.mp4",
			"streams/out", sql.NullString{String: "some error", Valid: true}, 5, now,
			sql.NullTime{Time: now, Valid: true}, sql.NullTime{Time: completedAt, Valid: true},
			[]byte("{}"),
		},
	}
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) *stg.CancelRow {
			return stg.NewTestCancelRow(scanner)
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	task, err := svc.GetTranscodingStatus(context.Background(), "task-1")
	require.NoError(t, err)
	assert.Equal(t, "some error", task.Error)
	require.NotNil(t, task.StartedAt)
	require.NotNil(t, task.CompletedAt)
}
