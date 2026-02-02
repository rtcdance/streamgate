package errors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// ErrorCode represents a standardized error code
type ErrorCode string

const (
	ErrCodeInternal       ErrorCode = "INTERNAL_ERROR"
	ErrCodeBadRequest     ErrorCode = "BAD_REQUEST"
	ErrCodeUnauthorized   ErrorCode = "UNAUTHORIZED"
	ErrCodeForbidden      ErrorCode = "FORBIDDEN"
	ErrCodeNotFound       ErrorCode = "NOT_FOUND"
	ErrCodeConflict       ErrorCode = "CONFLICT"
	ErrCodeRateLimit      ErrorCode = "RATE_LIMIT"
	ErrCodeServiceUnavail ErrorCode = "SERVICE_UNAVAILABLE"
	ErrCodeTimeout        ErrorCode = "TIMEOUT"
	ErrCodeValidation     ErrorCode = "VALIDATION_ERROR"
	ErrCodeDatabase       ErrorCode = "DATABASE_ERROR"
	ErrCodeExternal       ErrorCode = "EXTERNAL_SERVICE_ERROR"
	ErrCodeCircuitOpen    ErrorCode = "CIRCUIT_BREAKER_OPEN"
)

// AppError represents an application error
type AppError struct {
	Code       ErrorCode              `json:"code"`
	Message    string                 `json:"message"`
	Details    map[string]interface{} `json:"details,omitempty"`
	StatusCode int                    `json:"-"`
	Err        error                  `json:"-"`
	Retryable  bool                   `json:"-"`
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying error
func (e *AppError) Unwrap() error {
	return e.Err
}

// NewAppError creates a new application error
func NewAppError(code ErrorCode, message string) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		StatusCode: statusCodeForCode(code),
		Retryable:  isRetryable(code),
	}
}

// Wrap wraps an error with additional context
func Wrap(err error, code ErrorCode, message string) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		Err:        err,
		StatusCode: statusCodeForCode(code),
		Retryable:  isRetryable(code),
	}
}

// WithDetail adds a detail to the error
func (e *AppError) WithDetail(key string, value interface{}) *AppError {
	if e.Details == nil {
		e.Details = make(map[string]interface{})
	}
	e.Details[key] = value
	return e
}

// WithStatusCode sets the HTTP status code
func (e *AppError) WithStatusCode(code int) *AppError {
	e.StatusCode = code
	return e
}

// WithRetryable sets whether the error is retryable
func (e *AppError) WithRetryable(retryable bool) *AppError {
	e.Retryable = retryable
	return e
}

// ToJSON converts the error to JSON
func (e *AppError) ToJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"error": map[string]interface{}{
			"code":    e.Code,
			"message": e.Message,
			"details": e.Details,
		},
	})
}

// statusCodeForCode returns the HTTP status code for an error code
func statusCodeForCode(code ErrorCode) int {
	switch code {
	case ErrCodeBadRequest:
		return http.StatusBadRequest
	case ErrCodeUnauthorized:
		return http.StatusUnauthorized
	case ErrCodeForbidden:
		return http.StatusForbidden
	case ErrCodeNotFound:
		return http.StatusNotFound
	case ErrCodeConflict:
		return http.StatusConflict
	case ErrCodeRateLimit:
		return http.StatusTooManyRequests
	case ErrCodeServiceUnavail:
		return http.StatusServiceUnavailable
	case ErrCodeTimeout:
		return http.StatusGatewayTimeout
	case ErrCodeValidation:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

// isRetryable checks if an error code is retryable
func isRetryable(code ErrorCode) bool {
	switch code {
	case ErrCodeInternal, ErrCodeServiceUnavail, ErrCodeTimeout, ErrCodeExternal:
		return true
	default:
		return false
	}
}

// Common error constructors
func BadRequest(message string) *AppError {
	return NewAppError(ErrCodeBadRequest, message)
}

func Unauthorized(message string) *AppError {
	return NewAppError(ErrCodeUnauthorized, message)
}

func Forbidden(message string) *AppError {
	return NewAppError(ErrCodeForbidden, message)
}

func NotFound(message string) *AppError {
	return NewAppError(ErrCodeNotFound, message)
}

func Conflict(message string) *AppError {
	return NewAppError(ErrCodeConflict, message)
}

func RateLimit(message string) *AppError {
	return NewAppError(ErrCodeRateLimit, message)
}

func ServiceUnavailable(message string) *AppError {
	return NewAppError(ErrCodeServiceUnavail, message)
}

func Timeout(message string) *AppError {
	return NewAppError(ErrCodeTimeout, message)
}

func ValidationError(message string) *AppError {
	return NewAppError(ErrCodeValidation, message)
}

func DatabaseError(err error, message string) *AppError {
	return Wrap(err, ErrCodeDatabase, message)
}

func ExternalServiceError(err error, message string) *AppError {
	return Wrap(err, ErrCodeExternal, message)
}

func CircuitBreakerOpen(service string) *AppError {
	return NewAppError(ErrCodeCircuitOpen, fmt.Sprintf("Circuit breaker is open for service '%s'", service))
}

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxRetries      int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	BackoffFactor   float64
	RetryableErrors map[ErrorCode]bool
}

// DefaultRetryConfig returns default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:    3,
		InitialDelay:  1 * time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		RetryableErrors: map[ErrorCode]bool{
			ErrCodeInternal:       true,
			ErrCodeServiceUnavail: true,
			ErrCodeTimeout:        true,
			ErrCodeExternal:       true,
		},
	}
}

// Retry executes a function with retry logic
func Retry(ctx context.Context, config RetryConfig, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := calculateBackoff(attempt, config)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		var appErr *AppError
		if errors.As(err, &appErr) {
			if !appErr.Retryable {
				return err
			}
			if !config.RetryableErrors[appErr.Code] {
				return err
			}
		}

		if attempt == config.MaxRetries {
			break
		}
	}

	return fmt.Errorf("max retries (%d) exceeded: %w", config.MaxRetries, lastErr)
}

// calculateBackoff calculates exponential backoff delay
func calculateBackoff(attempt int, config RetryConfig) time.Duration {
	delay := config.InitialDelay

	for i := 1; i < attempt; i++ {
		delay = time.Duration(float64(delay) * config.BackoffFactor)
		if delay > config.MaxDelay {
			delay = config.MaxDelay
			break
		}
	}

	return delay
}

// DegradationStrategy defines how to handle degraded state
type DegradationStrategy int

const (
	DegradationFailFast DegradationStrategy = iota
	DegradationFallback
	DegradationCached
	DegradationPartial
)

// DegradationHandler handles service degradation
type DegradationHandler struct {
	strategy DegradationStrategy
	logger   *zap.Logger
	fallback map[string]interface{}
	cache    map[string]interface{}
}

// NewDegradationHandler creates a new degradation handler
func NewDegradationHandler(strategy DegradationStrategy, logger *zap.Logger) *DegradationHandler {
	return &DegradationHandler{
		strategy: strategy,
		logger:   logger,
		fallback: make(map[string]interface{}),
		cache:    make(map[string]interface{}),
	}
}

// Handle executes a function with degradation handling
func (dh *DegradationHandler) Handle(ctx context.Context, fn func() (interface{}, error), serviceName string) (interface{}, error) {
	result, err := fn()

	if err == nil {
		return result, nil
	}

	var appErr *AppError
	if !errors.As(err, &appErr) {
		return nil, err
	}

	dh.logger.Warn("Service degraded",
		zap.String("service", serviceName),
		zap.String("error", appErr.Error()),
		zap.String("strategy", dh.strategyName()))

	switch dh.strategy {
	case DegradationFailFast:
		return nil, appErr
	case DegradationFallback:
		if fallback, ok := dh.fallback[serviceName]; ok {
			dh.logger.Info("Using fallback response", zap.String("service", serviceName))
			return fallback, nil
		}
		return nil, appErr
	case DegradationCached:
		if cached, ok := dh.cache[serviceName]; ok {
			dh.logger.Info("Using cached response", zap.String("service", serviceName))
			return cached, nil
		}
		return nil, appErr
	case DegradationPartial:
		if partialResult, ok := dh.fallback[serviceName]; ok {
			dh.logger.Info("Returning partial result", zap.String("service", serviceName))
			return partialResult, nil
		}
		return nil, appErr
	default:
		return nil, appErr
	}
}

// strategyName returns the strategy name
func (dh *DegradationHandler) strategyName() string {
	switch dh.strategy {
	case DegradationFailFast:
		return "fail_fast"
	case DegradationFallback:
		return "fallback"
	case DegradationCached:
		return "cached"
	case DegradationPartial:
		return "partial"
	default:
		return "unknown"
	}
}

// SetFallback sets a fallback value for a service
func (dh *DegradationHandler) SetFallback(serviceName string, value interface{}) {
	dh.fallback[serviceName] = value
}

// SetCache sets a cached value for a service
func (dh *DegradationHandler) SetCache(serviceName string, value interface{}) {
	dh.cache[serviceName] = value
}

// ErrorHandler provides centralized error handling
type ErrorHandler struct {
	logger             *zap.Logger
	retryConfig        RetryConfig
	degradationHandler *DegradationHandler
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(logger *zap.Logger) *ErrorHandler {
	return &ErrorHandler{
		logger:             logger,
		retryConfig:        DefaultRetryConfig(),
		degradationHandler: NewDegradationHandler(DegradationFallback, logger),
	}
}

// Handle handles an error with appropriate response
func (eh *ErrorHandler) Handle(err error) (int, []byte) {
	if err == nil {
		return http.StatusOK, []byte(`{"status":"ok"}`)
	}

	var appErr *AppError
	if errors.As(err, &appErr) {
		eh.logger.Error("Application error",
			zap.String("code", string(appErr.Code)),
			zap.String("message", appErr.Message),
			zap.Error(appErr.Err))

		response, _ := appErr.ToJSON()
		return appErr.StatusCode, response
	}

	eh.logger.Error("Unexpected error", zap.Error(err))

	response, _ := NewAppError(ErrCodeInternal, "An unexpected error occurred").ToJSON()
	return http.StatusInternalServerError, response
}

// HandleWithRetry handles a function with retry and degradation
func (eh *ErrorHandler) HandleWithRetry(ctx context.Context, fn func() error, serviceName string) error {
	return Retry(ctx, eh.retryConfig, func() error {
		_, err := eh.degradationHandler.Handle(ctx, func() (interface{}, error) {
			return nil, fn()
		}, serviceName)
		return err
	})
}

// HandleWithDegradation handles a function with degradation
func (eh *ErrorHandler) HandleWithDegradation(ctx context.Context, fn func() (interface{}, error), serviceName string) (interface{}, error) {
	return eh.degradationHandler.Handle(ctx, fn, serviceName)
}

// SetRetryConfig sets the retry configuration
func (eh *ErrorHandler) SetRetryConfig(config RetryConfig) {
	eh.retryConfig = config
}

// SetDegradationStrategy sets the degradation strategy
func (eh *ErrorHandler) SetDegradationStrategy(strategy DegradationStrategy) {
	eh.degradationHandler.strategy = strategy
}

// SetFallback sets a fallback value
func (eh *ErrorHandler) SetFallback(serviceName string, value interface{}) {
	eh.degradationHandler.SetFallback(serviceName, value)
}

// SetCache sets a cached value
func (eh *ErrorHandler) SetCache(serviceName string, value interface{}) {
	eh.degradationHandler.SetCache(serviceName, value)
}

// Recover recovers from panics and returns an error
func Recover() error {
	if r := recover(); r != nil {
		return NewAppError(ErrCodeInternal, fmt.Sprintf("Panic recovered: %v", r))
	}
	return nil
}

// RecoverWithLogger recovers from panics and logs them
func RecoverWithLogger(logger *zap.Logger) error {
	if r := recover(); r != nil {
		logger.Error("Panic recovered",
			zap.Any("panic", r),
			zap.Stack("stack"))
		return NewAppError(ErrCodeInternal, "An unexpected error occurred")
	}
	return nil
}
