package v1

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestAuthHandler_Login(t *testing.T) {
	handler := &AuthHandler{}
	
	router := gin.New()
	router.POST("/login", handler.Login)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/login", nil)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "login")
}

func TestAuthHandler_Logout(t *testing.T) {
	handler := &AuthHandler{}
	
	router := gin.New()
	router.POST("/logout", handler.Logout)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/logout", nil)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "logout")
}

func TestAuthHandler_Verify(t *testing.T) {
	handler := &AuthHandler{}
	
	router := gin.New()
	router.POST("/verify", handler.Verify)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/verify", nil)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "verify")
}
