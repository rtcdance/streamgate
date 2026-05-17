package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRespondOK_AddsRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("request_id", "req-test-123")
	c.Request = httptest.NewRequest("GET", "/test", http.NoBody)

	respondOK(c, gin.H{"key": "value"})

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "value", resp["key"])
	assert.Equal(t, "req-test-123", resp["request_id"])
}

func TestRespondOK_NoRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", http.NoBody)

	respondOK(c, gin.H{"key": "value"})

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "value", resp["key"])
	_, hasRequestID := resp["request_id"]
	assert.False(t, hasRequestID)
}

func TestRespondOK_NonMapData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("request_id", "req-test-456")
	c.Request = httptest.NewRequest("GET", "/test", http.NoBody)

	respondOK(c, "plain string")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `"plain string"`, w.Body.String())
}

func TestRespondCreated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("request_id", "req-created")
	c.Request = httptest.NewRequest("POST", "/test", http.NoBody)

	respondCreated(c, gin.H{"id": "123"})

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestRespondAccepted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("request_id", "req-accepted")
	c.Request = httptest.NewRequest("POST", "/test", http.NoBody)

	respondAccepted(c, gin.H{"task_id": "t1"})

	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestRespondNoContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("DELETE", "/test", http.NoBody)

	respondNoContent(c)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.String())
}