package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"streamgate/pkg/service"
)

type apiTranscodingQueue struct {
	lastTask *service.TranscodingTask
}

func (q *apiTranscodingQueue) Enqueue(task *service.TranscodingTask) error {
	q.lastTask = task
	return nil
}

func (q *apiTranscodingQueue) Dequeue() (*service.TranscodingTask, error) {
	return q.lastTask, nil
}

func (q *apiTranscodingQueue) GetStatus(taskID string) (string, error) {
	if q.lastTask != nil && q.lastTask.ID == taskID {
		return q.lastTask.Status, nil
	}
	return "", assert.AnError
}

func newTestTranscodingHandler(t *testing.T) *TranscodingHandler {
	t.Helper()
	return NewTranscodingHandler(service.NewTranscodingService(nil, &apiTranscodingQueue{}))
}

func TestTranscodingHandler_Submit(t *testing.T) {
	handler := newTestTranscodingHandler(t)

	router := gin.New()
	router.POST("/transcode/submit", handler.Submit)

	body, _ := json.Marshal(map[string]interface{}{
		"content_id": "content-1",
		"profile":    "720p",
		"input_url":  "https://example.com/input.mp4",
		"priority":   5,
	})
	req := httptest.NewRequest(http.MethodPost, "/transcode/submit", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), `"task_id":`)
	assert.Contains(t, rec.Body.String(), `"status":"pending"`)
}

func TestTranscodingHandler_GetStatusAndCancel(t *testing.T) {
	queue := &apiTranscodingQueue{}
	svc := service.NewTranscodingService(nil, queue)
	handler := NewTranscodingHandler(svc)

	taskID, err := svc.Transcode("content-1", "720p", "https://example.com/input.mp4", 5)
	require.NoError(t, err)

	router := gin.New()
	router.GET("/transcode/status/:id", handler.GetStatus)
	router.POST("/transcode/cancel/:id", handler.Cancel)

	statusReq := httptest.NewRequest(http.MethodGet, "/transcode/status/"+taskID, nil)
	statusRec := httptest.NewRecorder()
	router.ServeHTTP(statusRec, statusReq)

	require.Equal(t, http.StatusOK, statusRec.Code)
	assert.Contains(t, statusRec.Body.String(), taskID)
	assert.Contains(t, statusRec.Body.String(), `"status":"pending"`)

	cancelReq := httptest.NewRequest(http.MethodPost, "/transcode/cancel/"+taskID, nil)
	cancelRec := httptest.NewRecorder()
	router.ServeHTTP(cancelRec, cancelReq)

	require.Equal(t, http.StatusOK, cancelRec.Code)
	assert.Contains(t, cancelRec.Body.String(), `"status":"cancelled"`)
}

func TestTranscodingHandler_ListProfiles(t *testing.T) {
	handler := newTestTranscodingHandler(t)

	router := gin.New()
	router.GET("/transcode/profiles", handler.ListProfiles)

	req := httptest.NewRequest(http.MethodGet, "/transcode/profiles", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"profiles":`)
}

func TestTranscodingHandler_ListTasks(t *testing.T) {
	queue := &apiTranscodingQueue{}
	svc := service.NewTranscodingService(nil, queue)
	handler := NewTranscodingHandler(svc)

	taskID, err := svc.Transcode("content-list", "720p", "https://example.com/input.mp4", 1)
	require.NoError(t, err)
	require.NotEmpty(t, taskID)

	router := gin.New()
	router.GET("/transcode/tasks", handler.ListTasks)

	req := httptest.NewRequest(http.MethodGet, "/transcode/tasks?content_id=content-list", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"tasks":`)
	assert.Contains(t, rec.Body.String(), taskID)
	assert.Contains(t, rec.Body.String(), `"content_id":"content-list"`)
}

func TestTranscodingHandler_ListTasksPagination(t *testing.T) {
	queue := &apiTranscodingQueue{}
	svc := service.NewTranscodingService(nil, queue)
	handler := NewTranscodingHandler(svc)

	_, err := svc.Transcode("content-a", "720p", "https://example.com/a1.mp4", 1)
	require.NoError(t, err)
	id2, err := svc.Transcode("content-a", "480p", "https://example.com/a2.mp4", 1)
	require.NoError(t, err)
	_, err = svc.Transcode("content-b", "1080p", "https://example.com/b1.mp4", 1)
	require.NoError(t, err)

	router := gin.New()
	router.GET("/transcode/tasks", handler.ListTasks)

	req := httptest.NewRequest(http.MethodGet, "/transcode/tasks?content_id=content-a&limit=1&offset=1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"tasks":`)
	assert.Contains(t, rec.Body.String(), id2)
	assert.NotContains(t, rec.Body.String(), `"content_id":"content-b"`)
}
