package middleware

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

// AuthMiddleware returns an auth middleware
func (s *Service) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			c.Abort()
			return
		}
		c.Next()
	}
}
