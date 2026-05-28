package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const defaultMaxBodySize int64 = 10 << 20 // 10MB for non-upload routes

// ContentTypeMiddleware validates request Content-Type for API routes.
// JSON routes must use application/json. Upload routes are exempted.
func (s *Service) ContentTypeMiddleware() gin.HandlerFunc {
	jsonContentTypes := map[string]bool{
		"application/json":                true,
		"application/json; charset=utf-8": true,
		"application/json;charset=utf-8":  true,
	}

	return func(c *gin.Context) {
		path := c.Request.URL.Path

		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead ||
			c.Request.Method == http.MethodDelete || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		if strings.HasPrefix(path, "/api/v1/upload") {
			c.Next()
			return
		}

		if strings.HasPrefix(path, "/api/") && c.Request.ContentLength > 0 {
			if !jsonContentTypes[c.GetHeader("Content-Type")] {
				c.JSON(http.StatusUnsupportedMediaType, gin.H{
					"error": "Content-Type must be application/json",
					"code":  "UNSUPPORTED_MEDIA_TYPE",
				})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

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
// Demo pages (/demo/*) get a relaxed CSP to allow CDN scripts, inline styles,
// and inline scripts required by the acceptance test pages.
func (s *Service) SecurityHeadersMiddleware() gin.HandlerFunc {
	const (
		strictCSP = "default-src 'self'"
		demoCSP   = "default-src 'self'; script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' http://localhost:* ws://localhost:* https://cdn.jsdelivr.net"
	)

	headers := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Strict-Transport-Security": "max-age=63072000; includeSubDomains; preload",
	}
	return func(c *gin.Context) {
		for k, v := range headers {
			c.Writer.Header().Set(k, v)
		}
		// Relax CSP for demo pages — they need CDN scripts, inline styles, inline JS
		if strings.HasPrefix(c.Request.URL.Path, "/demo/") {
			c.Writer.Header().Set("Content-Security-Policy", demoCSP)
		} else {
			c.Writer.Header().Set("Content-Security-Policy", strictCSP)
		}
		c.Next()
	}
}
