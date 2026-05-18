package gateway

import (
	"fmt"
	"net/http"

	"streamgate/pkg/models"
	"streamgate/pkg/service"

	"github.com/gin-gonic/gin"
)

func RegisterPlaybackStatsRoutes(router *gin.RouterGroup, svc *service.PlaybackStatsService) {
	router.POST("/api/v1/stats/playback", recordPlaybackEvent(svc))
	router.GET("/api/v1/content/:id/stats", getContentStats(svc))
	router.GET("/api/v1/stats/top", listTopContent(svc))
}

func recordPlaybackEvent(svc *service.PlaybackStatsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		wallet := getWalletAddress(c)
		var req struct {
			ContentID       string `json:"content_id" binding:"required"`
			EventType       string `json:"event_type" binding:"required"`
			DurationSeconds int    `json:"duration_seconds"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			respond(c, http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
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
			respond(c, http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		respond(c, http.StatusOK, gin.H{"recorded": true})
	}
}

func getContentStats(svc *service.PlaybackStatsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		contentID := c.Param("id")
		stats, err := svc.GetContentStats(c.Request.Context(), contentID)
		if err != nil {
			respond(c, http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		respondOK(c, stats)
	}
}

func listTopContent(svc *service.PlaybackStatsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := 20
		if l := c.Query("limit"); l != "" {
			if parsed, err := parseInt(l); err == nil && parsed > 0 && parsed <= 100 {
				limit = parsed
			}
		}
		stats, err := svc.ListTopContent(c.Request.Context(), limit)
		if err != nil {
			respond(c, http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		respondOK(c, gin.H{"content": stats})
	}
}

func getWalletAddress(c *gin.Context) string {
	if v, exists := c.Get("wallet_address"); exists {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
