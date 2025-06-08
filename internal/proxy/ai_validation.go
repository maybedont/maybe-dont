package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sudermanjr/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// AIValidationHandler validates tool calls using an AI model
type AIValidationHandler struct {
	logger *zap.Logger
	config *config.AIValidation
	client *http.Client
}

// NewAIValidationHandler creates a new AI validation handler
func NewAIValidationHandler(logger *zap.Logger, config *config.AIValidation) *AIValidationHandler {
	return &AIValidationHandler{
		logger: logger,
		config: config,
		client: &http.Client{
			Timeout: time.Duration(config.Timeout) * time.Second,
		},
	}
}

// HandleToolCall implements ToolValidationHandler
func (h *AIValidationHandler) HandleToolCall(ctx context.Context, req mcp.CallToolRequest) error {
	if !h.config.Enabled {
		return nil
	}

	// Prepare the prompt for the AI model
	prompt := fmt.Sprintf(`Evaluate if this tool call should be allowed:
Tool: %s
Arguments: %v
Context: %v

Return a JSON response with:
{
  "allowed": boolean,
  "reason": "string explaining the decision"
}`, req.Params.Name, req.Params.Arguments, req.Request.Params.Meta)

	// Prepare the request to the AI API
	aiReq := struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		MaxTokens int `json:"max_tokens"`
	}{
		Model: h.config.Model,
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			{
				Role:    "system",
				Content: "You are a security validation AI that evaluates if tool calls should be allowed based on security best practices.",
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
		MaxTokens: h.config.MaxTokens,
	}

	// Send request to AI API
	reqBody, err := json.Marshal(aiReq)
	if err != nil {
		return fmt.Errorf("failed to marshal AI request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", h.config.Endpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+os.Getenv("MCP_PROXY_OPENAI_API_KEY"))

	resp, err := h.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request to AI API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("AI API returned non-200 status code: %d", resp.StatusCode)
	}

	// Parse the AI response
	var aiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&aiResp); err != nil {
		return fmt.Errorf("failed to decode AI response: %w", err)
	}

	if len(aiResp.Choices) == 0 {
		return fmt.Errorf("AI API returned no choices")
	}

	// Parse the JSON content from the AI response
	var decision struct {
		Allowed bool   `json:"allowed"`
		Reason  string `json:"reason"`
	}

	if err := json.Unmarshal([]byte(aiResp.Choices[0].Message.Content), &decision); err != nil {
		return fmt.Errorf("failed to parse AI decision: %w", err)
	}

	if !decision.Allowed {
		return fmt.Errorf("AI validation denied request: %s", decision.Reason)
	}

	h.logger.Info("AI validation allowed request",
		zap.String("tool", req.Params.Name),
		zap.String("reason", decision.Reason),
	)

	return nil
}
