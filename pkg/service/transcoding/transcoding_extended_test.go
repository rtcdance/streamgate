package transcoding

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rtcdance/streamgate/pkg/models"
	stg "github.com/rtcdance/streamgate/pkg/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestTranscodingService_Transcode_WithOwnerWallet(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue())
	taskID, err := svc.Transcode(context.Background(), "content-1", "720p", "https://example.com/input.mp4", 5, "0xOwner123")
	require.NoError(t, err)
	assert.NotEmpty(t, taskID)

	task, err := svc.GetTranscodingStatus(context.Background(), taskID)
	require.NoError(t, err)
	assert.Equal(t, "0xOwner123", task.OwnerWallet)
}

func TestTranscodingService_CompleteTask_DB_InTxError(t *testing.T) {
	db := &mockDB{
		inTxFn: func(_ context.Context, _ func(tx *sql.Tx) error) error {
			return errors.New("tx error")
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))

	err := svc.CompleteTask(context.Background(), "task-1", "streams/out")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tx error")
}

func TestTranscodingService_saveTask_MetadataError(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue())
	task := &models.TranscodingTask{
		ID:        "task-1",
		ContentID: "content-1",
		Profile:   "720p",
		Status:    "pending",
		Metadata:  make(map[string]interface{}),
	}
	svc.storeTask(task)
	assert.NotNil(t, svc.tasks["task-1"])
}

func TestTranscodingService_processTask_WithDB(t *testing.T) {
	t.Run("db update rows zero skips processing", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{rowsAffected: 0}, nil
			},
		}
		svc := NewTranscodingService(db, NewMemoryTranscodingQueue(),
			WithTranscoder(&mockVideoTranscoder{}),
			WithLogger(zap.NewNop()),
		)

		task := &models.TranscodingTask{
			ID:        "task-dbskip",
			ContentID: "content-skip",
			Profile:   "720p",
			InputURL:  "/tmp/test-input.mp4",
			Metadata:  make(map[string]interface{}),
		}
		svc.storeTask(task)

		svc.processTask(context.Background(), task, zap.NewNop())

		updated, _ := svc.getTask("task-dbskip")
		assert.Equal(t, "processing", updated.Status)
	})

	t.Run("db update rows 1 continues processing", func(t *testing.T) {
		db := &mockDB{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
				return &mockResult{rowsAffected: 1}, nil
			},
		}
		svc := NewTranscodingService(db, NewMemoryTranscodingQueue(),
			WithTranscoder(&mockTranscoderWithFiles{progressCalls: []float64{100}}),
			WithStorage(newMockSegmentStorage()),
			WithLogger(zap.NewNop()),
		)

		task := &models.TranscodingTask{
			ID:        "task-dbok",
			ContentID: "content-dbok",
			Profile:   "720p",
			InputURL:  "/tmp/test-input.mp4",
			Metadata:  make(map[string]interface{}),
		}
		svc.storeTask(task)

		svc.processTask(context.Background(), task, zap.NewNop())

		updated, _ := svc.getTask("task-dbok")
		assert.NotEqual(t, "pending", updated.Status, "task should have progressed past pending")
	})
}

func TestTranscodingService_downloadInputFile_ValidContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake video data"))
	}))
	defer srv.Close()

	svc := NewTranscodingService(nil, nil, WithLogger(zap.NewNop()))
	path, err := svc.downloadInputFile(context.Background(), srv.URL+"/video.mp4")
	require.NoError(t, err)
	defer os.Remove(path)

	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestTranscodingService_downloadInputFile_OctetStreamContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("binary data"))
	}))
	defer srv.Close()

	svc := NewTranscodingService(nil, nil, WithLogger(zap.NewNop()))
	path, err := svc.downloadInputFile(context.Background(), srv.URL+"/file.bin")
	require.NoError(t, err)
	defer os.Remove(path)
}

func TestTranscodingService_downloadInputFile_UnsupportedContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>"))
	}))
	defer srv.Close()

	svc := NewTranscodingService(nil, nil, WithLogger(zap.NewNop()))
	_, err := svc.downloadInputFile(context.Background(), srv.URL+"/page.html")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported content type")
}

func TestTranscodingService_downloadInputFile_ContentTypeWithCharset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("video data"))
	}))
	defer srv.Close()

	svc := NewTranscodingService(nil, nil, WithLogger(zap.NewNop()))
	path, err := svc.downloadInputFile(context.Background(), srv.URL+"/video.mp4")
	require.NoError(t, err)
	defer os.Remove(path)
}

func TestTranscodingService_downloadInputFile_NoExtension(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("video data"))
	}))
	defer srv.Close()

	svc := NewTranscodingService(nil, nil, WithLogger(zap.NewNop()))
	path, err := svc.downloadInputFile(context.Background(), srv.URL+"/video")
	require.NoError(t, err)
	defer os.Remove(path)
	assert.Contains(t, path, ".mp4")
}

func TestTranscodingService_downloadInputFile_DefaultContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("video data"))
	}))
	defer srv.Close()

	svc := NewTranscodingService(nil, nil, WithLogger(zap.NewNop()))
	_, err := svc.downloadInputFile(context.Background(), srv.URL+"/video.mp4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported content type")
}

func TestTranscodingService_AdjustWorkerCount_ScaleUp(t *testing.T) {
	q := NewMemoryTranscodingQueue()
	svc := NewTranscodingService(nil, q,
		WithTranscoder(&mockVideoTranscoder{}),
		WithLogger(zap.NewNop()),
		WithMinWorkers(1),
		WithMaxWorkers(4),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	atomic.StoreInt32(&svc.currentWorkers, 1)

	for i := 0; i < 5; i++ {
		_ = q.Enqueue(&models.TranscodingTask{
			ID:        fmt.Sprintf("scale-task-%d", i),
			ContentID: "content-scale",
			Profile:   "720p",
			Status:    "pending",
			Metadata:  make(map[string]interface{}),
		})
	}

	svc.adjustWorkerCount(ctx, zap.NewNop())

	current := atomic.LoadInt32(&svc.currentWorkers)
	assert.True(t, current > 1, "should have scaled up, got %d", current)

	svc.extraMu.Lock()
	for _, c := range svc.extraCancels {
		c()
	}
	svc.extraMu.Unlock()
}

func TestTranscodingService_AdjustWorkerCount_ScaleDown(t *testing.T) {
	q := NewMemoryTranscodingQueue()
	svc := NewTranscodingService(nil, q,
		WithTranscoder(&mockVideoTranscoder{}),
		WithLogger(zap.NewNop()),
		WithMinWorkers(1),
		WithMaxWorkers(4),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	atomic.StoreInt32(&svc.currentWorkers, 3)

	svc.adjustWorkerCount(ctx, zap.NewNop())

	current := atomic.LoadInt32(&svc.currentWorkers)
	assert.Equal(t, int32(1), current, "should have scaled down to minWorkers")

	svc.extraMu.Lock()
	for _, c := range svc.extraCancels {
		c()
	}
	svc.extraMu.Unlock()
}

func TestTranscodingService_AdjustWorkerCount_AtMax(t *testing.T) {
	q := NewMemoryTranscodingQueue()
	svc := NewTranscodingService(nil, q,
		WithTranscoder(&mockVideoTranscoder{}),
		WithLogger(zap.NewNop()),
		WithMinWorkers(1),
		WithMaxWorkers(2),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	atomic.StoreInt32(&svc.currentWorkers, 1)

	for i := 0; i < 20; i++ {
		_ = q.Enqueue(&models.TranscodingTask{
			ID:        fmt.Sprintf("max-task-%d", i),
			ContentID: "content-max",
			Profile:   "720p",
			Status:    "pending",
			Metadata:  make(map[string]interface{}),
		})
	}

	svc.adjustWorkerCount(ctx, zap.NewNop())

	current := atomic.LoadInt32(&svc.currentWorkers)
	assert.Equal(t, int32(2), current, "should not exceed maxWorkers")

	svc.extraMu.Lock()
	for _, c := range svc.extraCancels {
		c()
	}
	svc.extraMu.Unlock()
}

func TestTranscodingService_processTask_FailOnDownloadError(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(),
		WithTranscoder(&mockVideoTranscoder{}),
		WithLogger(zap.NewNop()),
	)

	task := &models.TranscodingTask{
		ID:        "task-dl-fail",
		ContentID: "content-dl",
		Profile:   "720p",
		InputURL:  "http://invalid-host-that-does-not-exist.example/video.mp4",
		Metadata:  make(map[string]interface{}),
	}
	svc.storeTask(task)

	svc.processTask(context.Background(), task, zap.NewNop())

	updated, _ := svc.getTask("task-dl-fail")
	assert.Equal(t, "failed", updated.Status)
	assert.Contains(t, updated.Error, "failed to download input")
}

func TestTranscodingService_processTask_FailOnTranscodeError(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(),
		WithTranscoder(&mockVideoTranscoder{transcodeErr: errors.New("ffmpeg crash")}),
		WithLogger(zap.NewNop()),
	)

	task := &models.TranscodingTask{
		ID:        "task-tc-fail",
		ContentID: "content-tc",
		Profile:   "720p",
		InputURL:  "/tmp/test-input.mp4",
		Metadata:  make(map[string]interface{}),
	}
	svc.storeTask(task)

	svc.processTask(context.Background(), task, zap.NewNop())

	updated, _ := svc.getTask("task-tc-fail")
	assert.Equal(t, "failed", updated.Status)
	assert.Contains(t, updated.Error, "ffmpeg crash")
}

type mockTranscoderWithFiles struct {
	transcodeErr error
	progressCalls []float64
}

func (m *mockTranscoderWithFiles) TranscodeHLS(_ context.Context, _, outputDir, _ string, progressFn func(progress float64)) error {
	if m.transcodeErr != nil {
		return m.transcodeErr
	}
	_ = os.WriteFile(filepath.Join(outputDir, "index.m3u8"), []byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=2500000\nseg000.ts\n"), 0644)
	_ = os.WriteFile(filepath.Join(outputDir, "seg000.ts"), []byte("ts-data"), 0644)
	for _, p := range m.progressCalls {
		progressFn(p)
	}
	return nil
}

func TestTranscodingService_processTask_FailOnUploadError(t *testing.T) {
	store := newMockSegmentStorage()
	store.uploadStreamErr = errors.New("storage down")
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(),
		WithTranscoder(&mockTranscoderWithFiles{}),
		WithStorage(store),
		WithLogger(zap.NewNop()),
	)

	task := &models.TranscodingTask{
		ID:        "task-up-fail",
		ContentID: "content-up",
		Profile:   "720p",
		InputURL:  "/tmp/test-input.mp4",
		Metadata:  make(map[string]interface{}),
	}
	svc.storeTask(task)

	svc.processTask(context.Background(), task, zap.NewNop())

	updated, _ := svc.getTask("task-up-fail")
	assert.Equal(t, "failed", updated.Status)
	assert.Contains(t, updated.Error, "failed to upload segments")
}

func TestTranscodingService_processTask_NoStorage(t *testing.T) {
	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(),
		WithTranscoder(&mockVideoTranscoder{progressCalls: []float64{100}}),
		WithLogger(zap.NewNop()),
	)

	task := &models.TranscodingTask{
		ID:        "task-no-storage",
		ContentID: "content-ns",
		Profile:   "720p",
		InputURL:  "/tmp/test-input.mp4",
		Metadata:  make(map[string]interface{}),
	}
	svc.storeTask(task)

	svc.processTask(context.Background(), task, zap.NewNop())

	updated, _ := svc.getTask("task-no-storage")
	assert.Equal(t, "completed", updated.Status)
}

func TestDB_CompleteTask_InTxError(t *testing.T) {
	db := &mockDB{
		inTxFn: func(_ context.Context, _ func(tx *sql.Tx) error) error {
			return errors.New("tx failed")
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))

	err := svc.CompleteTask(context.Background(), "task-1", "streams/out")
	require.Error(t, err)
}

func TestTranscodingService_ListTasks_WithOwnerFilter(t *testing.T) {
	queue := NewMemoryTranscodingQueue()
	svc := NewTranscodingService(nil, queue)

	svc.Transcode(context.Background(), "content-a", "720p", "https://example.com/a.mp4", 1, "0xOwner1")
	svc.Transcode(context.Background(), "content-a", "480p", "https://example.com/a2.mp4", 2, "0xOwner2")
	svc.Transcode(context.Background(), "content-b", "1080p", "https://example.com/b.mp4", 3, "0xOwner1")

	filtered, err := svc.ListTasks(context.Background(), "", "0xOwner1", 10, 0)
	require.NoError(t, err)
	assert.Len(t, filtered, 2)

	for _, task := range filtered {
		assert.Equal(t, "0xOwner1", task.OwnerWallet)
	}
}

func TestTranscodingService_CancelTask_Processing(t *testing.T) {
	queue := NewMemoryTranscodingQueue()
	svc := NewTranscodingService(nil, queue)

	taskID, _ := svc.Transcode(context.Background(), "content-cancel", "720p", "https://example.com/input.mp4", 1, "")
	require.NoError(t, svc.StartTask(context.Background(), taskID))

	err := svc.CancelTask(context.Background(), taskID)
	require.NoError(t, err)

	task, _ := svc.GetTranscodingStatus(context.Background(), taskID)
	assert.Equal(t, "cancelled", task.Status)
}

func TestTranscodingService_uploadSegments_WithConcurrency(t *testing.T) {
	store := newMockSegmentStorage()
	svc := NewTranscodingService(nil, nil,
		WithStorage(store),
		WithLogger(zap.NewNop()),
		WithUploadConcurrency(3),
	)
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		require.NoError(t, os.WriteFile(fmt.Sprintf("%s/seg%03d.ts", dir, i), []byte("ts-data"), 0644))
	}
	require.NoError(t, os.WriteFile(dir+"/index.m3u8", []byte("#EXTM3U"), 0644))

	err := svc.uploadSegments(context.Background(), dir, "content-conc", "720p")
	require.NoError(t, err)
}

func TestMemoryTranscodingQueue_Dequeue_WaitAndEnqueue(t *testing.T) {
	q := NewMemoryTranscodingQueue()

	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = q.Enqueue(&models.TranscodingTask{ID: "delayed-task", Status: "pending"})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	task, err := q.Dequeue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "delayed-task", task.ID)
}

func TestMemoryTranscodingQueue_GetStatus_NotFound(t *testing.T) {
	q := NewMemoryTranscodingQueue()
	_, err := q.GetStatus("nonexistent")
	require.Error(t, err)
}

func TestTranscodingService_saveTask_DBError(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
			return nil, errors.New("db insert error")
		},
	}
	svc := NewTranscodingService(db, NewMemoryTranscodingQueue())
	_, err := svc.Transcode(context.Background(), "content-1", "720p", "https://example.com/input.mp4", 5, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save task")
}

func TestDB_ListTasks_EmptyResult(t *testing.T) {
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return &mockRows{tasks: nil}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	tasks, err := svc.ListTasks(context.Background(), "nonexistent", "", 10, 0)
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestDB_ListTasks_RowsError(t *testing.T) {
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (stg.Rows, error) {
			return &mockRows{nextErr: errors.New("rows iteration error")}, nil
		},
	}
	svc := NewTranscodingService(db, nil, WithLogger(zap.NewNop()))
	tasks, err := svc.ListTasks(context.Background(), "content-1", "", 10, 0)
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestTranscodingService_processTask_WithHTTPInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake video data"))
	}))
	defer srv.Close()

	svc := NewTranscodingService(nil, NewMemoryTranscodingQueue(),
		WithTranscoder(&mockVideoTranscoder{progressCalls: []float64{100}}),
		WithStorage(newMockSegmentStorage()),
		WithLogger(zap.NewNop()),
	)

	task := &models.TranscodingTask{
		ID:        "task-http",
		ContentID: "content-http",
		Profile:   "720p",
		InputURL:  srv.URL + "/video.mp4",
		Metadata:  make(map[string]interface{}),
	}
	svc.storeTask(task)

	svc.processTask(context.Background(), task, zap.NewNop())

	updated, _ := svc.getTask("task-http")
	assert.Equal(t, "completed", updated.Status)
}
