package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const defaultMaxBodySize int64 = 10 << 20 // 10MB for non-upload routes

// RequestSizeLimitMiddleware rejects requests with bodies exceeding maxBodySize.
// Upload routes (prefixed with /api/v1/upload) are exempted since they have
// their own size limit.
func (s *Service) RequestSizeLimitMiddleware(maxBodySize int64) gin.HandlerFunc {
	if maxBodySize <= 0 {
		maxBodySize = defaultMaxBodySize
	}
	return func(c *gin.Context) {
		// Skip for upload routes — they enforce their own limit
		if strings.HasPrefix(c.Request.URL.Path, "/api/v1/upload") {
			c.Next()
			return
		}
		if c.Request.ContentLength > maxBodySize {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": "Request body too large",
				"code":  "PAYLOAD_TOO_LARGE",
			})
			c.Abort()
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodySize)
		c.Next()
	}
}

// SecurityHeadersMiddleware adds standard security headers to every response.
func (s *Service) SecurityHeadersMiddleware() gin.HandlerFunc {
	headers := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"X-XSS-Protection":        "1; mode=block",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Content-Security-Policy": "default-src 'self'",
	}
	return func(c *gin.Context) {
		for k, v := range headers {
			c.Writer.Header().Set(k, v)
		}
		c.Next()
	}
}
