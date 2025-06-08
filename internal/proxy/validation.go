package proxy

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sudermanjr/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// ValidationHandler defines the interface for request validation handlers
type ValidationHandler interface {
	Handle(ctx context.Context, req *mcp.Request) error
	SetNext(handler ValidationHandler)
}

// BaseHandler provides common functionality for all handlers
type BaseHandler struct {
	next ValidationHandler
}

// SetNext sets the next handler in the chain
func (h *BaseHandler) SetNext(handler ValidationHandler) {
	h.next = handler
}

// LoggingHandler logs the request details
type LoggingHandler struct {
	BaseHandler
	logger *zap.Logger
}

// NewLoggingHandler creates a new logging handler
func NewLoggingHandler(logger *zap.Logger) *LoggingHandler {
	return &LoggingHandler{
		logger: logger,
	}
}

// Handle implements the ValidationHandler interface for logging
func (h *LoggingHandler) Handle(ctx context.Context, req *mcp.Request) error {
	// Log request details
	h.logger.Info("Processing request",
		zap.Any("request", req),
	)

	// Continue to next handler if exists
	if h.next != nil {
		return h.next.Handle(ctx, req)
	}
	return nil
}

// CELValidationHandler validates requests using CEL policies
type CELValidationHandler struct {
	BaseHandler
	logger *zap.Logger
	config *config.Config
}

// NewCELValidationHandler creates a new CEL validation handler
func NewCELValidationHandler(logger *zap.Logger, cfg *config.Config) *CELValidationHandler {
	return &CELValidationHandler{
		logger: logger,
		config: cfg,
	}
}

// Handle implements the ValidationHandler interface for CEL validation
func (h *CELValidationHandler) Handle(ctx context.Context, req *mcp.Request) error {
	// TODO: Implement CEL policy evaluation
	// For now, just log that we would evaluate policies
	h.logger.Debug("Would evaluate CEL policies for request",
		zap.String("method", req.Method),
	)

	// Continue to next handler if exists
	if h.next != nil {
		return h.next.Handle(ctx, req)
	}
	return nil
}

// ValidationChain represents the complete validation chain
type ValidationChain struct {
	firstHandler ValidationHandler
}

// NewValidationChain creates a new validation chain with all handlers
func NewValidationChain(logger *zap.Logger, cfg *config.Config) *ValidationChain {
	// Create handlers
	loggingHandler := NewLoggingHandler(logger)
	celHandler := NewCELValidationHandler(logger, cfg)

	// Set up the chain
	loggingHandler.SetNext(celHandler)

	return &ValidationChain{
		firstHandler: loggingHandler,
	}
}

// Validate processes a request through the validation chain
func (c *ValidationChain) Validate(ctx context.Context, req *mcp.Request) error {
	return c.firstHandler.Handle(ctx, req)
}
