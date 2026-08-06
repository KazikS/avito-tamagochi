// Package logger содержит базовый логгер для использования в проектах
package logger

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type ctxKey string

const (
	loggerKey    ctxKey = "logger"
	requestIDKey ctxKey = "request_id"
	RequestIDKey ctxKey = "request_id"
)

type Logger struct {
	l *zap.Logger
}

func NewLoggerContext(ctx context.Context, dev bool) (context.Context, error) {
	var config zap.Config

	if dev {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		config = zap.NewProductionConfig()
	}

	logger, err := config.Build(zap.AddCallerSkip(1))
	if err != nil {
		return nil, fmt.Errorf("NewLogger: %w", err)
	}

	return context.WithValue(ctx, loggerKey, &Logger{logger}), nil
}

func GetLoggerFromCtx(ctx context.Context) (*Logger, bool) {
	log, ok := ctx.Value(loggerKey).(*Logger)
	return log, ok
}

func NewContextWithLogger(ctx context.Context, log *Logger) context.Context {
	return context.WithValue(ctx, loggerKey, log)
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

func getRequestID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDKey).(string)
	return id, ok
}

func RequestID(id string) zap.Field {
	return zap.String(string(requestIDKey), id)
}

func (l *Logger) withRequestID(ctx context.Context, fields []zap.Field) []zap.Field {
	if id, ok := getRequestID(ctx); ok {
		fields = append(fields, RequestID(id))
	}

	return fields
}

func (l *Logger) Info(ctx context.Context, msg string, fields ...zap.Field) {
	if l == nil || l.l == nil {
		return
	}

	fields = l.withRequestID(ctx, fields)
	l.l.Info(msg, fields...)
}

func (l *Logger) Warn(ctx context.Context, msg string, fields ...zap.Field) {
	if l == nil || l.l == nil {
		return
	}

	fields = l.withRequestID(ctx, fields)
	l.l.Warn(msg, fields...)
}

func (l *Logger) Error(ctx context.Context, err error, msg string, fields ...zap.Field) {
	if l == nil || l.l == nil {
		return
	}

	fields = l.withRequestID(ctx, fields)
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	l.l.Error(msg, fields...)
}

func (l *Logger) Debug(ctx context.Context, msg string, fields ...zap.Field) {
	if l == nil || l.l == nil {
		return
	}

	fields = l.withRequestID(ctx, fields)
	l.l.Debug(msg, fields...)
}
