package v1

import (
"net/http"
"github.com/gin-gonic/gin"
)

// ContentHandler handles content requests
type ContentHandler struct{}

// List lists all content
func (h *ContentHandler) List(c *gin.Context) {
c.JSON(http.StatusOK, gin.H{"content": []interface{}{}})
}

// Get gets a specific content
func (h *ContentHandler) Get(c *gin.Context) {
c.JSON(http.StatusOK, gin.H{"content": nil})
}

// Create creates new content
func (h *ContentHandler) Create(c *gin.Context) {
c.JSON(http.StatusCreated, gin.H{"message": "created"})
}

// Update updates content
func (h *ContentHandler) Update(c *gin.Context) {
c.JSON(http.StatusOK, gin.H{"message": "updated"})
}

// Delete deletes content
func (h *ContentHandler) Delete(c *gin.Context) {
c.JSON(http.StatusNoContent, gin.H{})
}
