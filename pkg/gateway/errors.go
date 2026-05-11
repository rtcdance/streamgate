package gateway

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// APIError is the standard error response format for all StreamGate endpoints.
type APIError struct {
	Error     string `json:"error"`                // Human-readable message
	Code      string `json:"code"`                 // Machine-readable error code
	RequestID string `json:"request_id,omitempty"` // Request correlation ID
	Detail    string `json:"detail,omitempty"`      // Additional context
}

// Error code constants
const (
	ErrInvalidRequest     = "INVALID_REQUEST"
	ErrUnauthorized       = "UNAUTHORIZED"
	ErrTokenRevoked       = "TOKEN_REVOKED"
	ErrTokenExpired       = "TOKEN_EXPIRED"
	ErrForbidden          = "FORBIDDEN"
	ErrNFTRequired        = "NFT_REQUIRED"
	ErrNFTVerifyError     = "NFT_VERIFY_ERROR"
	ErrMissingContract    = "MISSING_CONTRACT"
	ErrContentNotFound    = "CONTENT_NOT_FOUND"
	ErrContentForbidden   = "CONTENT_FORBIDDEN"
	ErrContentUnavailable = "CONTENT_UNAVAILABLE"
	ErrUploadFailed       = "UPLOAD_FAILED"
	ErrNotFound           = "NOT_FOUND"
	ErrRateLimited        = "RATE_LIMITED"
	ErrPayloadTooLarge    = "PAYLOAD_TOO_LARGE"
	ErrHealthCheckFailed  = "HEALTH_CHECK_FAILED"
	ErrInternalError      = "INTERNAL_ERROR"
)

// WithDetail adds detail to the error.
func (e APIError) WithDetail(detail string) APIError {
	e.Detail = detail
	return e
}

// abortWithError sends a structured error response and aborts the request chain.
func abortWithError(c *gin.Context, status int, code, msg string) {
	reqID, _ := c.Get("request_id")
	apiErr := APIError{Error: msg, Code: code}
	if id, ok := reqID.(string); ok && id != "" {
		apiErr.RequestID = id
	}
	c.AbortWithStatusJSON(status, apiErr)
}

// abortWithErrorDetail sends a structured error response with detail and aborts.
// For 5xx errors, the detail is logged server-side only and replaced with a
// generic message in the response to prevent leaking internal state.
func abortWithErrorDetail(c *gin.Context, status int, code, msg, detail string) {
	reqID, _ := c.Get("request_id")
	reqIDStr, _ := reqID.(string)

	// For server errors, log the real detail but don't send it to the client
	if status >= 500 {
		if detail != "" {
			fmt.Printf("[ERROR] request_id=%s code=%s internal_detail=%s\n", reqIDStr, code, detail)
		}
		detail = "" // Never expose internal details for 5xx
	}

	apiErr := APIError{Error: msg, Code: code, Detail: detail}
	if reqIDStr != "" {
		apiErr.RequestID = reqIDStr
	}
	c.AbortWithStatusJSON(status, apiErr)
}

// RequestIDMiddleware is a gin middleware that generates a unique request ID
// for each request and stores it in the context and response headers.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = fmt.Sprintf("req-%s-%d", uuid.New().String()[:8], time.Now().UnixNano())
		}
		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

// internalErrMsg returns a safe user-facing message for 5xx errors.
// It replaces raw internal error strings with a generic message while
// preserving the original error in the detail field for debugging.
func internalErrMsg(err error) string {
	return "an internal error occurred"
}
