package gateway

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func respond(c *gin.Context, status int, data interface{}) {
	reqID, _ := c.Get("request_id")
	if id, ok := reqID.(string); ok && id != "" {
		if m, ok := data.(gin.H); ok {
			cp := make(gin.H, len(m)+1)
			for k, v := range m {
				cp[k] = v
			}
			cp["request_id"] = id
			data = cp
		}
	}
	c.JSON(status, data)
}

func respondOK(c *gin.Context, data interface{}) {
	respond(c, http.StatusOK, data)
}

func respondCreated(c *gin.Context, data interface{}) {
	respond(c, http.StatusCreated, data)
}

func respondAccepted(c *gin.Context, data interface{}) {
	respond(c, http.StatusAccepted, data)
}

func respondNoContent(c *gin.Context) {
	c.AbortWithStatus(http.StatusNoContent)
}
