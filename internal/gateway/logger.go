package gateway

import (
	"context"

	"go.uber.org/zap"
)

// ContextLogger is a logger wrapper that automatically extracts and includes
// session ID from context in all log entries.
type ContextLogger struct {
	logger *zap.Logger
}

// NewContextLogger creates a new context-aware logger wrapper.
func NewContextLogger(logger *zap.Logger) *ContextLogger {
	return &ContextLogger{
		logger: logger,
	}
}

// extractSessionID extracts the session ID from context, returning "unknown" if not present.
func extractSessionID(ctx context.Context) string {
	if sessionID, ok := GetSessionID(ctx); ok {
		return sessionID
	}
	return "unknown"
}

// Debug logs a debug message with session ID automatically extracted from context.
func (l *ContextLogger) Debug(ctx context.Context, msg string, fields ...zap.Field) {
	sessionID := extractSessionID(ctx)
	allFields := append([]zap.Field{zap.String("session_id", sessionID)}, fields...)
	l.logger.Debug(msg, allFields...)
}

// Info logs an info message with session ID automatically extracted from context.
func (l *ContextLogger) Info(ctx context.Context, msg string, fields ...zap.Field) {
	sessionID := extractSessionID(ctx)
	allFields := append([]zap.Field{zap.String("session_id", sessionID)}, fields...)
	l.logger.Info(msg, allFields...)
}

// Warn logs a warning message with session ID automatically extracted from context.
func (l *ContextLogger) Warn(ctx context.Context, msg string, fields ...zap.Field) {
	sessionID := extractSessionID(ctx)
	allFields := append([]zap.Field{zap.String("session_id", sessionID)}, fields...)
	l.logger.Warn(msg, allFields...)
}

// Error logs an error message with session ID automatically extracted from context.
func (l *ContextLogger) Error(ctx context.Context, msg string, fields ...zap.Field) {
	sessionID := extractSessionID(ctx)
	allFields := append([]zap.Field{zap.String("session_id", sessionID)}, fields...)
	l.logger.Error(msg, allFields...)
}
