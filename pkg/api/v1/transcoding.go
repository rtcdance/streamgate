package v1

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"streamgate/pkg/service"
)

// TranscodingHandler handles transcoding requests.
type TranscodingHandler struct {
	service *service.TranscodingService
}

// NewTranscodingHandler creates a new transcoding handler.
func NewTranscodingHandler(svc *service.TranscodingService) *TranscodingHandler {
	return &TranscodingHandler{service: svc}
}

type transcodeSubmitRequest struct {
	ContentID string `json:"content_id"`
	Profile   string `json:"profile"`
	InputURL  string `json:"input_url"`
	Priority  int    `json:"priority"`
}

// Submit submits a transcoding task.
func (h *TranscodingHandler) Submit(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "transcoding service unavailable"})
		return
	}

	var req transcodeSubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	req.ContentID = strings.TrimSpace(req.ContentID)
	req.Profile = strings.TrimSpace(req.Profile)
	req.InputURL = strings.TrimSpace(req.InputURL)
	if req.ContentID == "" || req.Profile == "" || req.InputURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content_id, profile and input_url are required"})
		return
	}

	taskID, err := h.service.Transcode(req.ContentID, req.Profile, req.InputURL, req.Priority)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"task_id": taskID,
		"status":  "pending",
	})
}

// GetStatus gets transcoding task status.
func (h *TranscodingHandler) GetStatus(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "transcoding service unavailable"})
		return
	}

	taskID := strings.TrimSpace(c.Param("id"))
	if taskID == "" {
		taskID = strings.TrimSpace(c.Query("task_id"))
	}
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id is required"})
		return
	}

	task, err := h.service.GetTranscodingStatus(taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, task)
}

// Cancel cancels a transcoding task.
func (h *TranscodingHandler) Cancel(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "transcoding service unavailable"})
		return
	}

	taskID := strings.TrimSpace(c.Param("id"))
	if taskID == "" {
		taskID = strings.TrimSpace(c.Query("task_id"))
	}
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id is required"})
		return
	}

	if err := h.service.CancelTask(taskID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"task_id": taskID, "status": "cancelled"})
}

// ListProfiles returns all supported transcoding profiles.
func (h *TranscodingHandler) ListProfiles(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "transcoding service unavailable"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"profiles": h.service.ListProfiles()})
}

// ListTasks returns transcoding tasks, optionally filtered by content_id.
func (h *TranscodingHandler) ListTasks(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "transcoding service unavailable"})
		return
	}

	contentID := strings.TrimSpace(c.Query("content_id"))
	limit := 20
	offset := 0
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if raw := strings.TrimSpace(c.Query("offset")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	tasks, err := h.service.ListTasks(contentID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}
