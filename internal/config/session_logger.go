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
	// SessionIDKey is the context key for session IDs
	SessionIDKey ContextKey = "session_id"
)

// SessionLogger wraps a zap.Logger and automatically includes request_id and session_id from context in all log calls.
// If no request_id or session_id exists in the context, it will use "-".
type SessionLogger struct {
	logger *zap.Logger
}

// NewSessionLogger creates a new SessionLogger that wraps the given zap.Logger.
func NewSessionLogger(logger *zap.Logger) *SessionLogger {
	return &SessionLogger{logger: logger}
}

// extractRequestID extracts the request ID from the context, returning "-" if not present.
func (l *SessionLogger) extractRequestID(ctx context.Context) string {
	if ctx == nil {
		return "-"
	}
	if requestID, ok := ctx.Value(RequestIDKey).(string); ok {
		return requestID
	}
	return "-"
}

// extractSessionID extracts the session ID from the context, returning "-" if not present.
func (l *SessionLogger) extractSessionID(ctx context.Context) string {
	if ctx == nil {
		return "-"
	}
	if sessionID, ok := ctx.Value(SessionIDKey).(string); ok {
		return sessionID
	}
	return "-"
}

// buildFields constructs the log fields, auto-injecting request_id and session_id
// from context only when the caller hasn't already provided them explicitly.
// This prevents duplicate JSON keys in structured log output.
func (l *SessionLogger) buildFields(ctx context.Context, fields []zap.Field) []zap.Field {
	hasRequestID := false
	hasSessionID := false
	for _, f := range fields {
		switch f.Key {
		case "request_id":
			hasRequestID = true
		case "session_id":
			hasSessionID = true
		}
	}

	var prefix []zap.Field
	if !hasRequestID {
		prefix = append(prefix, zap.String("request_id", l.extractRequestID(ctx)))
	}
	if !hasSessionID {
		prefix = append(prefix, zap.String("session_id", l.extractSessionID(ctx)))
	}
	return append(prefix, fields...)
}

// Debug logs a debug message with request_id and session_id automatically included.
func (l *SessionLogger) Debug(ctx context.Context, msg string, fields ...zap.Field) {
	l.logger.Debug(msg, l.buildFields(ctx, fields)...)
}

// Info logs an info message with request_id and session_id automatically included.
func (l *SessionLogger) Info(ctx context.Context, msg string, fields ...zap.Field) {
	l.logger.Info(msg, l.buildFields(ctx, fields)...)
}

// Warn logs a warning message with request_id and session_id automatically included.
func (l *SessionLogger) Warn(ctx context.Context, msg string, fields ...zap.Field) {
	l.logger.Warn(msg, l.buildFields(ctx, fields)...)
}

// Error logs an error message with request_id and session_id automatically included.
func (l *SessionLogger) Error(ctx context.Context, msg string, fields ...zap.Field) {
	l.logger.Error(msg, l.buildFields(ctx, fields)...)
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
