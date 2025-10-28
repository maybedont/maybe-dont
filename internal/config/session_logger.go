package config

import (
	"context"

	"go.uber.org/zap"
)

// contextKey is the type for context keys
type contextKey string

const (
	// SessionIDKey is the context key for session IDs
	sessionIDKey contextKey = "session_id"
)

// SessionLogger wraps a zap.Logger and automatically includes session_id from context in all log calls.
// If no session_id exists in the context, it will use "n/a".
type SessionLogger struct {
	logger *zap.Logger
}

// NewSessionLogger creates a new SessionLogger that wraps the given zap.Logger.
func NewSessionLogger(logger *zap.Logger) *SessionLogger {
	return &SessionLogger{logger: logger}
}

// extractSessionID extracts the session ID from the context, returning "n/a" if not present.
func (l *SessionLogger) extractSessionID(ctx context.Context) string {
	if ctx == nil {
		return "n/a"
	}
	if sessionID, ok := ctx.Value(sessionIDKey).(string); ok {
		return sessionID
	}
	return "n/a"
}

// Debug logs a debug message with session_id automatically included.
func (l *SessionLogger) Debug(ctx context.Context, msg string, fields ...zap.Field) {
	sessionID := l.extractSessionID(ctx)
	allFields := append([]zap.Field{zap.String("session_id", sessionID)}, fields...)
	l.logger.Debug(msg, allFields...)
}

// Info logs an info message with session_id automatically included.
func (l *SessionLogger) Info(ctx context.Context, msg string, fields ...zap.Field) {
	sessionID := l.extractSessionID(ctx)
	allFields := append([]zap.Field{zap.String("session_id", sessionID)}, fields...)
	l.logger.Info(msg, allFields...)
}

// Warn logs a warning message with session_id automatically included.
func (l *SessionLogger) Warn(ctx context.Context, msg string, fields ...zap.Field) {
	sessionID := l.extractSessionID(ctx)
	allFields := append([]zap.Field{zap.String("session_id", sessionID)}, fields...)
	l.logger.Warn(msg, allFields...)
}

// Error logs an error message with session_id automatically included.
func (l *SessionLogger) Error(ctx context.Context, msg string, fields ...zap.Field) {
	sessionID := l.extractSessionID(ctx)
	allFields := append([]zap.Field{zap.String("session_id", sessionID)}, fields...)
	l.logger.Error(msg, allFields...)
}

// Logger returns the underlying zap.Logger for cases where direct access is needed.
func (l *SessionLogger) Logger() *zap.Logger {
	return l.logger
}

// Sync flushes any buffered log entries.
func (l *SessionLogger) Sync() error {
	return l.logger.Sync()
}
