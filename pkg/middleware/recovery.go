package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// RecoveryMiddleware returns a recovery middleware
func (s *Service) RecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				if s != nil && s.logger != nil {
					s.logger.Error("Panic recovered", zap.Any("error", err))
				}
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": fmt.Sprintf("Internal server error: %v", err),
				})
			}
		}()
		c.Next()
	}
}
