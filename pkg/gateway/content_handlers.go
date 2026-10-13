package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/service/registry"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ContentAttester is the durable registry-attest surface (SG-13 A.2/A.7),
// implemented by *RegistryHub. nil (or a nil hub) means the on-chain registry
// is disabled and attest answers 503.
type ContentAttester interface {
	Attest(ctx context.Context, actor, contentID, idemKey string) (*registry.AttestResponse, bool, error)
}

// RegisterContentRoutes registers content CRUD routes.
func RegisterContentRoutes(router gin.IRouter, log *zap.Logger, contentSvc *service.ContentService, attester ContentAttester) {
	content := router.Group(APIPrefix + "/content")
	content.GET("", handleListContents(contentSvc, log))
	content.GET("/:id", handleGetContent(contentSvc, log))
	content.POST("", handleCreateContent(contentSvc, log))
	content.PUT("/:id", handleUpdateContent(contentSvc, log))
	content.DELETE("/:id", handleDeleteContent(contentSvc, log))
	// On-chain attestation of a verified original: JWT + content owner only.
	content.POST("/:id/attest", handleAttestContent(contentSvc, attester, log))
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
		if err := contentSvc.DeleteContent(c.Request.Context(), id, middleware.GetWalletAddress(c)); err != nil {
			abortWithError(c, http.StatusNotFound, ErrContentNotFound, "content not found")
			return
		}
		respondNoContent(c)
	}
}

// handleAttestContent creates or reuses the durable on-chain register intent
// for a content the caller owns and returns its CURRENT status (SG-13 A.7 —
// business success is the DB row; the chain registration is a separate,
// visible status, never a synchronous "success" claim).
//
// Idempotency (A.2): an optional Idempotency-Key header records the exchange
// under (actor, operation, chain/registry scope, key) with the canonical
// payload digest as the comparison column. Same key + same payload replays
// the stored response; same key + different payload answers 409. When the
// header is ABSENT no idempotency record is written and dedup relies on the
// unique (chain, registry, contentID) intent alone — a deterministic key is
// deliberately NOT synthesised. The request body is ignored: the digest comes
// server-side from contents.file_hash, never from the client.
func handleAttestContent(contentSvc *service.ContentService, attester ContentAttester, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if contentSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrContentUnavailable, "content service unavailable")
			return
		}
		if attester == nil {
			abortWithError(c, http.StatusServiceUnavailable, "REGISTRY_DISABLED", "on-chain content registry is disabled")
			return
		}
		if _, ok := requireContentOwner(c, contentSvc); !ok {
			return
		}
		resp, conflict, err := attester.Attest(c.Request.Context(), middleware.GetWalletAddress(c), c.Param("id"), c.GetHeader("Idempotency-Key"))
		if err != nil {
			switch {
			case errors.Is(err, registry.ErrContentNotFound):
				abortWithError(c, http.StatusNotFound, ErrContentNotFound, "content not found")
			case errors.Is(err, registry.ErrNotOwner):
				abortWithError(c, http.StatusForbidden, ErrContentForbidden, "not authorized to access this content")
			case errors.Is(err, registry.ErrOriginalNotVerified):
				abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "content has no verified original to attest")
			default:
				abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, "failed to record attestation intent", err.Error())
			}
			return
		}
		if conflict {
			abortWithError(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was reused with a different payload")
			return
		}
		respondOK(c, gin.H{"attestation": resp})
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
