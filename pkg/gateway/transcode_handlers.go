package gateway

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"streamgate/pkg/middleware"
	"streamgate/pkg/plugins/transcoder"
	"streamgate/pkg/service"
	"streamgate/pkg/util"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// RegisterTranscodingRoutes registers video transcoding management routes.
func RegisterTranscodingRoutes(router gin.IRouter, log *zap.Logger, svc *service.TranscodingService) {
	transcode := router.Group("/api/v1/transcode")
	transcode.POST("/submit", handleTranscodeSubmit(svc, log))
	transcode.GET("/status/:id", handleTranscodeStatus(svc, log))
	transcode.POST("/cancel/:id", handleTranscodeCancel(svc, log))
	transcode.GET("/tasks", handleTranscodeTasks(svc, log))
	transcode.GET("/profiles", handleTranscodeProfiles(svc, log))
	log.Info("Transcoding routes registered")
}

type transcodeSubmitRequest struct {
	ContentID string `json:"content_id"`
	Profile   string `json:"profile"`
	InputURL  string `json:"input_url"`
	Priority  int    `json:"priority"`
}

func handleTranscodeSubmit(svc *service.TranscodingService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrInternalError, "transcoding service unavailable")
			return
		}
		var req transcodeSubmitRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid request")
			return
		}
		req.ContentID = strings.TrimSpace(req.ContentID)
		req.Profile = strings.TrimSpace(req.Profile)
		req.InputURL = strings.TrimSpace(req.InputURL)
		if req.ContentID == "" || req.Profile == "" || req.InputURL == "" {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "content_id, profile and input_url are required")
			return
		}
		if req.Priority < 0 || req.Priority > 10 {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "priority must be between 0 and 10")
			return
		}
		if !util.IsValidURL(req.InputURL) {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "input_url must be a valid http/https URL")
			return
		}
		if err := util.IsSafeURL(req.InputURL); err != nil {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "input_url targets a private or internal network")
			return
		}
		wallet := middleware.GetWalletAddress(c)
		taskID, err := svc.Transcode(c.Request.Context(), req.ContentID, req.Profile, req.InputURL, req.Priority, wallet)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}
		respondAccepted(c, gin.H{
			"task_id": taskID,
			"status":  "pending",
		})
	}
}

func handleTranscodeStatus(svc *service.TranscodingService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrInternalError, "transcoding service unavailable")
			return
		}
		taskID := strings.TrimSpace(c.Param("id"))
		if taskID == "" {
			taskID = strings.TrimSpace(c.Query("task_id"))
		}
		if taskID == "" {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "task_id is required")
			return
		}
		task, err := svc.GetTranscodingStatus(c.Request.Context(), taskID)
		if err != nil {
			abortWithError(c, http.StatusNotFound, ErrNotFound, "transcode task not found")
			return
		}
		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "authentication required")
			return
		}
		if task.OwnerWallet != wallet {
			abortWithError(c, http.StatusForbidden, ErrForbidden, "not authorized to access this task")
			return
		}
		respondOK(c, task)
	}
}

func handleTranscodeCancel(svc *service.TranscodingService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrInternalError, "transcoding service unavailable")
			return
		}
		taskID := strings.TrimSpace(c.Param("id"))
		if taskID == "" {
			taskID = strings.TrimSpace(c.Query("task_id"))
		}
		if taskID == "" {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "task_id is required")
			return
		}
		task, err := svc.GetTranscodingStatus(c.Request.Context(), taskID)
		if err != nil {
			abortWithError(c, http.StatusNotFound, ErrNotFound, "transcode task not found")
			return
		}
		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "authentication required")
			return
		}
		if task.OwnerWallet != wallet {
			abortWithError(c, http.StatusForbidden, ErrForbidden, "not authorized to cancel this task")
			return
		}
		if err := svc.CancelTask(c.Request.Context(), taskID); err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}
		respondOK(c, gin.H{"task_id": taskID, "status": "cancelled"})
	}
}

func handleTranscodeTasks(svc *service.TranscodingService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrInternalError, "transcoding service unavailable")
			return
		}
		contentID := strings.TrimSpace(c.Query("content_id"))
		limit := 20
		offset := 0
		if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 100 {
				limit = parsed
			}
		}
		if raw := strings.TrimSpace(c.Query("offset")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
				offset = parsed
			}
		}
		wallet := middleware.GetWalletAddress(c)
		tasks, err := svc.ListTasks(c.Request.Context(), contentID, wallet, limit, offset)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}
		respondOK(c, gin.H{"tasks": tasks})
	}
}

func handleTranscodeProfiles(svc *service.TranscodingService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrInternalError, "transcoding service unavailable")
			return
		}
		respondOK(c, gin.H{"profiles": svc.ListProfiles()})
	}
}

// --- Internal adapters ---

type ffmpegRouterAdapter struct {
	ft  *transcoder.FFmpegTranscoder
	log *zap.Logger
}

func (a *ffmpegRouterAdapter) TranscodeHLS(ctx context.Context, inputPath, outputDir, profile string, progressFn func(progress float64)) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
	}

	profiles := []transcoder.TranscodeProfile{
		{Resolution: "1920x1080", Bitrate: "5000k", Format: "hls"},
		{Resolution: "1280x720", Bitrate: "2500k", Format: "hls"},
		{Resolution: "854x480", Bitrate: "1000k", Format: "hls"},
		{Resolution: "640x360", Bitrate: "500k", Format: "hls"},
	}

	switch profile {
	case "1080p":
		profiles = []transcoder.TranscodeProfile{{Resolution: "1920x1080", Bitrate: "5000k", Format: "hls"}}
	case "720p":
		profiles = []transcoder.TranscodeProfile{{Resolution: "1280x720", Bitrate: "2500k", Format: "hls"}}
	case "480p":
		profiles = []transcoder.TranscodeProfile{{Resolution: "854x480", Bitrate: "1000k", Format: "hls"}}
	case "360p":
		profiles = []transcoder.TranscodeProfile{{Resolution: "640x360", Bitrate: "500k", Format: "hls"}}
	case "abr", "":
	}

	callback := func(p *transcoder.TranscodeProgress) {
		if progressFn != nil && p != nil {
			progressFn(p.Progress)
		}
	}
	return a.ft.TranscodeToHLS(ctx, inputPath, outputDir, profiles, callback)
}

type zapRouterInfoLogger struct {
	*zap.Logger
}

func (l *zapRouterInfoLogger) Info(msg string, fields ...interface{}) {
	l.Logger.Info(msg, zap.Any("fields", fields))
}
