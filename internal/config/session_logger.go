package config

import (
	"context"

	"go.uber.org/zap"
)

// ContextKey is the type for context keys
type ContextKey string

const (
	// RequestIDKey is the context key for request IDs
	RequestIDKey ContextKey = "request_id"
)

// SessionLogger wraps a zap.Logger and automatically includes request_id from context in all log calls.
// If no request_id exists in the context, it will use "n/a".
type SessionLogger struct {
	logger *zap.Logger
}

// NewSessionLogger creates a new SessionLogger that wraps the given zap.Logger.
func NewSessionLogger(logger *zap.Logger) *SessionLogger {
	return &SessionLogger{logger: logger}
}

// extractRequestID extracts the request ID from the context, returning "n/a" if not present.
func (l *SessionLogger) extractRequestID(ctx context.Context) string {
	if ctx == nil {
		return "n/a"
	}
	if requestID, ok := ctx.Value(RequestIDKey).(string); ok {
		return requestID
	}
	return "n/a"
}

// Debug logs a debug message with request_id automatically included.
func (l *SessionLogger) Debug(ctx context.Context, msg string, fields ...zap.Field) {
	requestID := l.extractRequestID(ctx)
	allFields := append([]zap.Field{zap.String("request_id", requestID)}, fields...)
	l.logger.Debug(msg, allFields...)
}

// Info logs an info message with request_id automatically included.
func (l *SessionLogger) Info(ctx context.Context, msg string, fields ...zap.Field) {
	requestID := l.extractRequestID(ctx)
	allFields := append([]zap.Field{zap.String("request_id", requestID)}, fields...)
	l.logger.Info(msg, allFields...)
}

// Warn logs a warning message with request_id automatically included.
func (l *SessionLogger) Warn(ctx context.Context, msg string, fields ...zap.Field) {
	requestID := l.extractRequestID(ctx)
	allFields := append([]zap.Field{zap.String("request_id", requestID)}, fields...)
	l.logger.Warn(msg, allFields...)
}

// Error logs an error message with request_id automatically included.
func (l *SessionLogger) Error(ctx context.Context, msg string, fields ...zap.Field) {
	requestID := l.extractRequestID(ctx)
	allFields := append([]zap.Field{zap.String("request_id", requestID)}, fields...)
	l.logger.Error(msg, allFields...)
}

// Logger returns the underlying zap.Logger for cases where direct access is needed.
func (l *SessionLogger) Logger() *zap.Logger {
	return l.logger
}

// GetZapLogger returns the underlying zap.Logger for cases where direct access is needed.
// This is an alias for Logger() to provide more explicit naming.
func (l *SessionLogger) GetZapLogger() *zap.Logger {
	return l.logger
}

// Sync flushes any buffered log entries.
func (l *SessionLogger) Sync() error {
	return l.logger.Sync()
}
