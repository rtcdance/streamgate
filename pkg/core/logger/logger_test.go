package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewLogger(t *testing.T) {
	t.Run("creates production logger", func(t *testing.T) {
		logger := NewLogger("test-service")

		assert.NotNil(t, logger)
	})

	t.Run("creates logger with service name", func(t *testing.T) {
		logger := NewLogger("my-service")

		assert.NotNil(t, logger)

		logger.Info("test message")
		logger.Error("error message")
	})

	t.Run("creates multiple loggers", func(t *testing.T) {
		logger1 := NewLogger("service1")
		logger2 := NewLogger("service2")

		assert.NotNil(t, logger1)
		assert.NotNil(t, logger2)
		assert.NotSame(t, logger1, logger2)
	})
}

func TestNewDevelopmentLogger(t *testing.T) {
	t.Run("creates development logger", func(t *testing.T) {
		logger := NewDevelopmentLogger("test-service")

		assert.NotNil(t, logger)
	})

	t.Run("creates logger with service name", func(t *testing.T) {
		logger := NewDevelopmentLogger("my-service")

		assert.NotNil(t, logger)

		logger.Debug("debug message")
		logger.Info("info message")
		logger.Warn("warn message")
		logger.Error("error message")
	})

	t.Run("creates multiple loggers", func(t *testing.T) {
		logger1 := NewDevelopmentLogger("service1")
		logger2 := NewDevelopmentLogger("service2")

		assert.NotNil(t, logger1)
		assert.NotNil(t, logger2)
		assert.NotSame(t, logger1, logger2)
	})
}

func TestLoggerOutput(t *testing.T) {
	t.Run("production logger can log", func(t *testing.T) {
		logger := NewLogger("test")

		assert.NotPanics(t, func() {
			logger.Info("test info message")
			logger.Warn("test warn message")
			logger.Error("test error message")
		})
	})

	t.Run("development logger can log", func(t *testing.T) {
		logger := NewDevelopmentLogger("test")

		assert.NotPanics(t, func() {
			logger.Debug("test debug message")
			logger.Info("test info message")
			logger.Warn("test warn message")
			logger.Error("test error message")
		})
	})
}
