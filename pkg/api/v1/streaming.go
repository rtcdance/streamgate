package v1

import (
"net/http"
"github.com/gin-gonic/gin"
)

// StreamingHandler handles streaming requests
type StreamingHandler struct{}

// GetHLS gets HLS playlist
func (h *StreamingHandler) GetHLS(c *gin.Context) {
c.Header("Content-Type", "application/vnd.apple.mpegurl")
c.String(http.StatusOK, "#EXTM3U\n#EXT-X-VERSION:3\n")
}

// GetDASH gets DASH manifest
func (h *StreamingHandler) GetDASH(c *gin.Context) {
c.Header("Content-Type", "application/dash+xml")
c.String(http.StatusOK, "<?xml version=\"1.0\"?><MPD></MPD>")
}

// GetSegment gets a video segment
func (h *StreamingHandler) GetSegment(c *gin.Context) {
c.Header("Content-Type", "video/mp2t")
c.String(http.StatusOK, "")
}
