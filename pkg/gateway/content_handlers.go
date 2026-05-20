package gateway

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"streamgate/pkg/middleware"
	"streamgate/pkg/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// RegisterContentRoutes registers content CRUD routes.
func RegisterContentRoutes(router gin.IRouter, log *zap.Logger, contentSvc *service.ContentService) {
	content := router.Group(APIPrefix + "/content")
	content.GET("", handleListContents(contentSvc, log))
	content.GET("/:id", handleGetContent(contentSvc, log))
	content.POST("", handleCreateContent(contentSvc, log))
	content.PUT("/:id", handleUpdateContent(contentSvc, log))
	content.DELETE("/:id", handleDeleteContent(contentSvc, log))
	log.Info("Content routes registered")
}

func handleListContents(contentSvc *service.ContentService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if contentSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrContentUnavailable, "content service unavailable")
			return
		}
		wallet := middleware.GetWalletAddress(c)
		limit := 20
		offset := 0
		if v := c.Query("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}
		if v := c.Query("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}
		ownerID := wallet
		items, totalCount, err := contentSvc.ListContentsWithCount(c.Request.Context(), ownerID, limit, offset)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, "failed to list content", err.Error())
			return
		}
		respondOK(c, gin.H{"items": items, "total_count": totalCount, "limit": limit, "offset": offset})
	}
}

func handleGetContent(contentSvc *service.ContentService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if contentSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrContentUnavailable, "content service unavailable")
			return
		}
		_, ok := requireContentOwner(c, contentSvc)
		if !ok {
			return
		}
		id := c.Param("id")
		content, err := contentSvc.GetContent(c.Request.Context(), id)
		if err != nil {
			abortWithError(c, http.StatusNotFound, ErrContentNotFound, "content not found")
			return
		}
		respondOK(c, gin.H{"content": content})
	}
}

func handleCreateContent(contentSvc *service.ContentService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if contentSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrContentUnavailable, "content service unavailable")
			return
		}
		var req struct {
			Title        string                 `json:"title"`
			Description  string                 `json:"description"`
			Type         string                 `json:"type"`
			URL          string                 `json:"url"`
			ThumbnailURL string                 `json:"thumbnail_url"`
			Duration     int                    `json:"duration"`
			Size         int64                  `json:"size"`
			Metadata     map[string]interface{} `json:"metadata"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid request body")
			return
		}
		if req.Title == "" {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "title is required")
			return
		}
		if len(req.Title) > 255 {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "title must be at most 255 characters")
			return
		}
		validTypes := map[string]bool{"video": true, "audio": true, "image": true, "document": true, "livestream": true}
		if req.Type == "" {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "type is required")
			return
		}
		if !validTypes[req.Type] {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid content type")
			return
		}
		if req.Duration < 0 {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "duration must be non-negative")
			return
		}
		if req.Size < 0 {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "size must be non-negative")
			return
		}
		wallet := middleware.GetWalletAddress(c)
		content := &service.Content{
			Title:        req.Title,
			Description:  req.Description,
			Type:         req.Type,
			URL:          req.URL,
			ThumbnailURL: req.ThumbnailURL,
			Duration:     req.Duration,
			Size:         req.Size,
			OwnerID:      wallet,
			Metadata:     req.Metadata,
		}
		if content.Metadata == nil {
			content.Metadata = make(map[string]interface{})
		}
		id, err := contentSvc.CreateContent(c.Request.Context(), content)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, "failed to create content", err.Error())
			return
		}
		respondCreated(c, gin.H{"id": id, "content": content})
	}
}

func handleUpdateContent(contentSvc *service.ContentService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if contentSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrContentUnavailable, "content service unavailable")
			return
		}
		existing, ok := requireContentOwner(c, contentSvc)
		if !ok {
			return
		}
		var req struct {
			Title        *string                `json:"title"`
			Description  *string                `json:"description"`
			Type         *string                `json:"type"`
			URL          *string                `json:"url"`
			ThumbnailURL *string                `json:"thumbnail_url"`
			Duration     *int                   `json:"duration"`
			Size         *int64                 `json:"size"`
			Metadata     map[string]interface{} `json:"metadata"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid request body")
			return
		}
		if req.Metadata != nil {
			if serialized, err := json.Marshal(req.Metadata); err != nil || len(serialized) > 65536 {
				abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "metadata must be valid JSON under 64KB")
				return
			}
		}
		if req.Title != nil {
			existing.Title = *req.Title
		}
		if req.Description != nil {
			existing.Description = *req.Description
		}
		if req.Type != nil {
			existing.Type = *req.Type
		}
		if req.URL != nil {
			existing.URL = *req.URL
		}
		if req.ThumbnailURL != nil {
			existing.ThumbnailURL = *req.ThumbnailURL
		}
		if req.Duration != nil {
			existing.Duration = *req.Duration
		}
		if req.Size != nil {
			existing.Size = *req.Size
		}
		if req.Metadata != nil {
			existing.Metadata = req.Metadata
		}
		if err := contentSvc.UpdateContent(c.Request.Context(), existing); err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, "failed to update content", err.Error())
			return
		}
		respondOK(c, gin.H{"content": existing})
	}
}

func handleDeleteContent(contentSvc *service.ContentService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if contentSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrContentUnavailable, "content service unavailable")
			return
		}
		_, ok := requireContentOwner(c, contentSvc)
		if !ok {
			return
		}
		id := c.Param("id")
		if err := contentSvc.DeleteContent(c.Request.Context(), id); err != nil {
			abortWithError(c, http.StatusNotFound, ErrContentNotFound, "content not found")
			return
		}
		respondNoContent(c)
	}
}

// requireContentOwner verifies the current user owns the content.
func requireContentOwner(c *gin.Context, contentSvc *service.ContentService) (*service.Content, bool) {
	id := c.Param("id")
	content, err := contentSvc.GetContent(c.Request.Context(), id)
	if err != nil {
		abortWithError(c, http.StatusNotFound, ErrContentNotFound, "content not found")
		return nil, false
	}
	wallet := middleware.GetWalletAddress(c)
	if !strings.EqualFold(content.OwnerID, wallet) {
		abortWithError(c, http.StatusForbidden, ErrContentForbidden, "not authorized to access this content")
		return nil, false
	}
	// Return a copy to prevent callers from mutating cached data
	cp := *content
	if content.Metadata != nil {
		cp.Metadata = make(map[string]interface{}, len(content.Metadata))
		for k, v := range content.Metadata {
			cp.Metadata[k] = v
		}
	}
	return &cp, true
}
