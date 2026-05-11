package middleware

import (
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
					s.logger.Error("Panic recovered",
						zap.Any("error", err),
						zap.Stack("stack"),
						zap.String("path", c.Request.URL.Path),
						zap.String("method", c.Request.Method),
					)
				}
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "internal server error",
					"code":  "INTERNAL_ERROR",
				})
			}
		}()
		c.Next()
	}
}
