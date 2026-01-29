package logger

import (
	"fmt"
	"time"
)

// FormatLog formats a log message
func FormatLog(level, message string, fields map[string]interface{}) string {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fieldStr := ""
	for k, v := range fields {
		fieldStr += fmt.Sprintf(" %s=%v", k, v)
	}
	return fmt.Sprintf("[%s] %s %s%s", timestamp, level, message, fieldStr)
}
