package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORSMiddleware returns a CORS middleware.
// If allowedOrigins is non-empty, the Origin header is echoed back when it
// matches an entry in the list (credentials-safe). When the list is empty the
// wildcard "*" is used (not credentials-safe per CORS spec).
func (s *Service) CORSMiddleware(allowedOrigins ...string) gin.HandlerFunc {
	originSet := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[o] = struct{}{}
	}
	wildcard := len(originSet) == 0

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		if wildcard {
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		} else if _, ok := originSet[origin]; ok {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Vary", "Origin")
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			if c.Request.Method == http.MethodOptions {
				c.Writer.Header().Set("Access-Control-Allow-Origin", "")
				c.Writer.Header().Set("Vary", "Origin")
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			c.Next()
			return
		}

		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Authorization, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
