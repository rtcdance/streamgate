package gateway

import (
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func httptestNewRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}

func newTestContext(w *httptest.ResponseRecorder) *gin.Context {
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c
}
