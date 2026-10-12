package gateway

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// A playlist response carries a playback capability in its BODY, so a shared
// cache must never store it, and the URL (which contains the capability) must
// not be forwarded to another origin through a Referer header.
func TestSetCapabilityResponseHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	setCapabilityResponseHeaders(c)

	assert.Equal(t, "private, no-store", c.Writer.Header().Get("Cache-Control"))
	assert.Equal(t, "no-referrer", c.Writer.Header().Get("Referrer-Policy"))
	assert.Equal(t, "nosniff", c.Writer.Header().Get("X-Content-Type-Options"))
}
