package gateway

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

// ResponseValidationResult represents the result of a single response validation check
type ResponseValidationResult struct {
	PolicyName      string `json:"policy_name"`
	PolicyType      string `json:"policy_type"` // "cel" or "ai"
	Allowed         bool   `json:"allowed"`
	Message         string `json:"message,omitempty"`
	Error           string `json:"error,omitempty"`
	RedactedContent string `json:"redacted_content,omitempty"` // For redaction actions
}

// ResponseValidationResults represents all validation results for a response
type ResponseValidationResults struct {
	Results         []ResponseValidationResult `json:"results"`
	Allowed         bool                       `json:"allowed"`
	Message         string                     `json:"message,omitempty"`
	Error           string                     `json:"error,omitempty"`
	AllowCount      int                        `json:"allow_count"`
	DenyCount       int                        `json:"deny_count"`
	RedactCount     int                        `json:"redact_count"`
	RedactedContent *string                    `json:"redacted_content,omitempty"` // Final redacted content if any redaction occurred
}

// ResponseValidationHandler defines the interface for response validation handlers
type ResponseValidationHandler interface {
	HandleResponse(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error)
}

// ResponseValidationChain implements a chain of response validation handlers
type ResponseValidationChain struct {
	handlers []ResponseValidationHandler
	logger   *zap.Logger
}

// NewResponseValidationChain creates a new response validation chain
func NewResponseValidationChain(logger *zap.Logger, handlers ...ResponseValidationHandler) *ResponseValidationChain {
	return &ResponseValidationChain{
		handlers: handlers,
		logger:   logger,
	}
}

// Handle processes a response through the validation chain
func (c *ResponseValidationChain) Handle(ctx context.Context, req mcp.CallToolRequest, result *mcp.CallToolResult) (ResponseValidationResults, error) {
	var finalResults ResponseValidationResults
	finalResults.Results = make([]ResponseValidationResult, 0)
	finalResults.Allowed = true // Default to allowed

	var currentResult = result

	for _, handler := range c.handlers {
		results, err := handler.HandleResponse(ctx, req, currentResult)
		if err != nil {
			c.logger.Error("Response validation handler error",
				zap.Error(err),
			)
			// Continue processing other handlers
			continue
		}

		finalResults.Results = append(finalResults.Results, results.Results...)
		finalResults.AllowCount += results.AllowCount
		finalResults.DenyCount += results.DenyCount
		finalResults.RedactCount += results.RedactCount

		// If a handler denied the response, mark overall as denied
		if !results.Allowed {
			finalResults.Allowed = false
			if finalResults.Message == "" {
				finalResults.Message = results.Message
			}
		}

		// If a handler redacted content, update the current result
		if results.RedactedContent != nil {
			// Apply redaction to the current result
			if len(currentResult.Content) > 0 {
				// Update the first text content item
				for i := range currentResult.Content {
					if textContent, ok := currentResult.Content[i].(mcp.TextContent); ok {
						textContent.Text = *results.RedactedContent
						currentResult.Content[i] = textContent
						break
					}
				}
			}
			finalResults.RedactedContent = results.RedactedContent
		}
	}

	// Set final message if not already set
	if finalResults.Message == "" {
		if finalResults.DenyCount > 0 {
			finalResults.Message = "Response denied by policy"
		} else if finalResults.RedactCount > 0 {
			finalResults.Message = "Response content redacted"
		} else {
			finalResults.Message = "Response validation passed"
		}
	}

	return finalResults, nil
}
