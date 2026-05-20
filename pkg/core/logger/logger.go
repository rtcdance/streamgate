package logger

import (
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type LogMode string

const (
	LogModeDevelopment LogMode = "development"
	LogModeProduction  LogMode = "production"
)

func NewLogger(serviceName string, mode ...LogMode) *zap.Logger {
	m := LogModeProduction
	if len(mode) > 0 && mode[0] != "" {
		m = mode[0]
	}

	if strings.EqualFold(string(m), string(LogModeDevelopment)) {
		return NewDevelopmentLogger(serviceName)
	}

	config := zap.NewProductionConfig()
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.OutputPaths = []string{"stdout"}
	config.ErrorOutputPaths = []string{"stderr"}

	logger, err := config.Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build production logger: %v\n", err)
		return zap.NewNop()
	}
	return logger.With(zap.String("service", serviceName))
}

func NewDevelopmentLogger(serviceName string) *zap.Logger {
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	logger, err := config.Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build development logger: %v\n", err)
		return zap.NewNop()
	}
	return logger.With(zap.String("service", serviceName))
}
