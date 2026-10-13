package gateway

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/util"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TranscodeInputPolicy is how the transcode API decides which input_url values
// it will accept. The zero value is the safe default: no allowlist, external
// input refused.
type TranscodeInputPolicy struct {
	// AllowedHosts is an exact host or host:port allowlist — the configured
	// object store. Matching it is the ONLY way an internal address is permitted,
	// so the store can be reached without opening every private address to a
	// caller-supplied URL.
	AllowedHosts []string
	// AllowExternal permits public internet URLs. Off by default: the API takes
	// uploaded objects and internal object-store URLs, not arbitrary external
	// input.
	AllowExternal bool
}

// RegisterTranscodingRoutes registers video transcoding management routes.
func RegisterTranscodingRoutes(router gin.IRouter, log *zap.Logger, svc *service.TranscodingService, policy ...TranscodeInputPolicy) {
	inputPolicy := TranscodeInputPolicy{}
	if len(policy) > 0 {
		inputPolicy = policy[0]
	}
	transcode := router.Group(APIPrefix + "/transcode")
	transcode.POST("/submit", handleTranscodeSubmit(svc, log, inputPolicy))
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

func handleTranscodeSubmit(svc *service.TranscodingService, log *zap.Logger, policy TranscodeInputPolicy) gin.HandlerFunc {
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
		// One guard decides scheme, host allowlist, external policy and the
		// resolved addresses. The message stays generic so the response does not
		// become a probe for which internal names exist.
		if err := util.ValidateFetchTarget(req.InputURL, util.FetchTargetOptions{
			AllowedHosts:  policy.AllowedHosts,
			AllowExternal: policy.AllowExternal,
		}); err != nil {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "input_url is not an acceptable source")
			return
		}
		wallet := middleware.GetWalletAddress(c)
		taskID, err := svc.Transcode(c.Request.Context(), req.ContentID, req.Profile, req.InputURL, req.Priority, wallet)
		if err != nil {
			// Saturation is retryable, so it gets an explicit status plus
			// Retry-After instead of a bare 500.
			switch {
			case errors.Is(err, service.ErrOwnerTaskLimit):
				c.Header("Retry-After", "30")
				abortWithError(c, http.StatusTooManyRequests, ErrRateLimited, "too many pending transcode tasks, retry later")
			case errors.Is(err, service.ErrQueueFull):
				c.Header("Retry-After", "5")
				abortWithError(c, http.StatusServiceUnavailable, ErrInternalError, "transcode queue is full, retry later")
			default:
				abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			}
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

// The FFmpeg -> service.VideoTranscoder adapter lives in
// pkg/plugins/transcoder (NewVideoTranscoder) so the gateway and the isolated
// transcoder microservice run the same profile mapping.
