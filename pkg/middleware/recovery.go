package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RecoveryMiddleware returns a recovery middleware
func (s *Service) RecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				s.logger.Error("Panic recovered", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "Internal server error",
				})
			}
		}()
		c.Next()
	}
}
