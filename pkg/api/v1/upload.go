package v1

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

// UploadHandler handles upload requests
type UploadHandler struct{}

// Upload handles file upload
func (h *UploadHandler) Upload(c *gin.Context) {
	c.JSON(http.StatusCreated, gin.H{"file_id": "file_123"})
}

// UploadChunk handles chunked upload
func (h *UploadHandler) UploadChunk(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"chunk_index": 0})
}

// GetStatus gets upload status
func (h *UploadHandler) GetStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "pending"})
}
