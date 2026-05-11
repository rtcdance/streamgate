package gateway

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"streamgate/pkg/middleware"
	"streamgate/pkg/service"
)

// sanitizeObjectKey rejects values containing path traversal sequences.
func sanitizeObjectKey(val string) (string, bool) {
	if strings.Contains(val, "..") || strings.ContainsAny(val, "/\\") {
		return "", false
	}
	return val, true
}

const maxUploadSize int64 = 500 * 1024 * 1024 // 500MB

// RegisterUploadRoutes registers file upload routes.
func RegisterUploadRoutes(router gin.IRouter, log *zap.Logger, objStorage service.SegmentStorage) {
	upload := router.Group("/api/v1/upload")
	upload.POST("", func(c *gin.Context) {
		wallet := middleware.GetWalletAddress(c)
		file, err := c.FormFile("file")
		if err != nil {
			abortWithErrorDetail(c, http.StatusBadRequest, ErrInvalidRequest, "no file provided", err.Error())
			return
		}
		// Validate file size
		if file.Size > maxUploadSize {
			abortWithError(c, http.StatusRequestEntityTooLarge, ErrPayloadTooLarge, fmt.Sprintf("file size %d exceeds maximum allowed size %d", file.Size, maxUploadSize))
			return
		}
		contentID := c.PostForm("content_id")
		if contentID == "" {
			contentID = fmt.Sprintf("upload-%d", time.Now().UnixNano())
		}
		if _, ok := sanitizeObjectKey(contentID); !ok {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "content_id contains invalid characters")
			return
		}
		src, err := file.Open()
		if err != nil {
			abortWithError(c, http.StatusInternalServerError, ErrUploadFailed, "failed to read file")
			return
		}
		defer func() { _ = src.Close() }()
		objectKey := fmt.Sprintf("uploads/%s/%s/original%s", wallet, contentID, filepath.Ext(file.Filename))
		if objStorage != nil {
			if err := objStorage.UploadStream(c.Request.Context(), "streamgate", objectKey, src, file.Size); err != nil {
				abortWithErrorDetail(c, http.StatusInternalServerError, ErrUploadFailed, "upload failed", err.Error())
				return
			}
		}
		c.JSON(http.StatusCreated, gin.H{"content_id": contentID, "object_key": objectKey, "filename": file.Filename, "size": file.Size, "upload_url": objectKey})
	})
	upload.POST("/chunk", func(c *gin.Context) {
		wallet := middleware.GetWalletAddress(c)
		uploadID := c.PostForm("upload_id")
		chunkIndexStr := c.PostForm("chunk_index")
		if uploadID == "" || chunkIndexStr == "" {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "upload_id and chunk_index are required")
			return
		}
		if _, ok := sanitizeObjectKey(uploadID); !ok {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "upload_id contains invalid characters")
			return
		}
		chunkIndex, err := strconv.Atoi(chunkIndexStr)
		if err != nil {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid chunk_index")
			return
		}
		file, err := c.FormFile("chunk")
		if err != nil {
			abortWithErrorDetail(c, http.StatusBadRequest, ErrInvalidRequest, "no chunk file provided", err.Error())
			return
		}
		if file.Size > maxUploadSize {
			abortWithError(c, http.StatusRequestEntityTooLarge, ErrPayloadTooLarge, "chunk exceeds maximum allowed size")
			return
		}
		src, err := file.Open()
		if err != nil {
			abortWithError(c, http.StatusInternalServerError, ErrUploadFailed, "failed to read chunk")
			return
		}
		defer func() { _ = src.Close() }()
		objectKey := fmt.Sprintf("uploads/chunks/%s/%s/%d", wallet, uploadID, chunkIndex)
		if objStorage != nil {
			if err := objStorage.UploadStream(c.Request.Context(), "streamgate", objectKey, src, file.Size); err != nil {
				abortWithErrorDetail(c, http.StatusInternalServerError, ErrUploadFailed, "chunk upload failed", err.Error())
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"upload_id": uploadID, "chunk_index": chunkIndex, "object_key": objectKey, "size": file.Size})
	})
	upload.GET("/:id/status", func(c *gin.Context) {
		wallet := middleware.GetWalletAddress(c)
		contentID := c.Param("id")
		if objStorage != nil {
			prefix := fmt.Sprintf("uploads/%s/%s/", wallet, contentID)
			if objects, err := objStorage.ListObjects(c.Request.Context(), "streamgate", prefix); err == nil && len(objects) > 0 {
				c.JSON(http.StatusOK, gin.H{"content_id": contentID, "status": "uploaded", "objects": objects})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"content_id": contentID, "status": "not_found"})
	})
	log.Info("Upload routes registered")
}
