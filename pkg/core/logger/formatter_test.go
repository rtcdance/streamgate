package logger

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatLog(t *testing.T) {
	t.Run("simple level and message no fields", func(t *testing.T) {
		result := FormatLog("INFO", "hello world", nil)

		assert.Contains(t, result, "INFO")
		assert.Contains(t, result, "hello world")
	})

	t.Run("level and message with fields", func(t *testing.T) {
		fields := map[string]interface{}{
			"user": "alice",
		}
		result := FormatLog("ERROR", "something failed", fields)

		assert.Contains(t, result, "ERROR")
		assert.Contains(t, result, "something failed")
		assert.Contains(t, result, "user=alice")
	})

	t.Run("empty fields map", func(t *testing.T) {
		fields := map[string]interface{}{}
		result := FormatLog("WARN", "no details", fields)

		assert.Contains(t, result, "WARN")
		assert.Contains(t, result, "no details")
	})

	t.Run("multiple fields with different types", func(t *testing.T) {
		fields := map[string]interface{}{
			"count":    42,
			"active":   true,
			"ratio":    3.14,
			"name":     "bob",
		}
		result := FormatLog("DEBUG", "multi field log", fields)

		assert.Contains(t, result, "DEBUG")
		assert.Contains(t, result, "multi field log")
		assert.Contains(t, result, "count=42")
		assert.Contains(t, result, "active=true")
		assert.Contains(t, result, "ratio=3.14")
		assert.Contains(t, result, "name=bob")
	})

	t.Run("nil fields", func(t *testing.T) {
		result := FormatLog("INFO", "nil fields test", nil)

		assert.Contains(t, result, "INFO")
		assert.Contains(t, result, "nil fields test")
	})

	t.Run("output format structure", func(t *testing.T) {
		fields := map[string]interface{}{
			"req_id": "abc123",
		}
		result := FormatLog("INFO", "request started", fields)

		assert.True(t, strings.HasPrefix(result, "["))
		assert.Contains(t, result, "] INFO request started req_id=abc123")
	})
}