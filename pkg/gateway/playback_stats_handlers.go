package gateway

import (
	"net/http"
	"strconv"

	"streamgate/pkg/middleware"
	"streamgate/pkg/models"
	"streamgate/pkg/service"

	"github.com/gin-gonic/gin"
)

func RegisterPlaybackStatsRoutes(router *gin.RouterGroup, svc *service.PlaybackStatsService) {
	router.POST(APIPrefix+"/stats/playback", recordPlaybackEvent(svc))
	router.GET(APIPrefix+"/content/:id/stats", getContentStats(svc))
	router.GET(APIPrefix+"/stats/top", listTopContent(svc))
}

func recordPlaybackEvent(svc *service.PlaybackStatsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		wallet := middleware.GetWalletAddress(c)
		var req struct {
			ContentID       string `json:"content_id" binding:"required"`
			EventType       string `json:"event_type" binding:"required"`
			DurationSeconds int    `json:"duration_seconds"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			abortWithErrorDetail(c, http.StatusBadRequest, ErrInvalidRequest, "invalid request body", err.Error())
			return
		}
		event := &models.PlaybackEvent{
			ContentID:       req.ContentID,
			WalletAddress:   wallet,
			EventType:       req.EventType,
			DurationSeconds: req.DurationSeconds,
			UserAgent:       c.Request.UserAgent(),
			IPAddress:       c.ClientIP(),
		}
		if err := svc.RecordEvent(c.Request.Context(), event); err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}
		respondOK(c, gin.H{"recorded": true})
	}
}

func getContentStats(svc *service.PlaybackStatsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		contentID := c.Param("id")
		stats, err := svc.GetContentStats(c.Request.Context(), contentID)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}
		respondOK(c, stats)
	}
}

func listTopContent(svc *service.PlaybackStatsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := 20
		if l := c.Query("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
				limit = parsed
			}
		}
		stats, err := svc.ListTopContent(c.Request.Context(), limit)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}
		respondOK(c, gin.H{"content": stats})
	}
}
