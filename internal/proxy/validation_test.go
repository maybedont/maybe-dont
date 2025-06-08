package proxy

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/sudermanjr/maybe-dont/internal/config"
	"go.uber.org/zap/zaptest"
)

func TestValidationChain(t *testing.T) {
	// Create test logger
	logger := zaptest.NewLogger(t)

	// Create test config
	cfg := &config.Config{
		Policy: struct {
			RulesFile string `mapstructure:"rules_file"`
			Default   string `mapstructure:"default"`
		}{
			Default: "deny",
		},
	}

	// Create validation chain
	chain := NewValidationChain(logger, cfg)

	tests := []struct {
		name    string
		req     *mcp.Request
		wantErr bool
	}{
		{
			name: "ping request",
			req: &mcp.Request{
				Method: "ping",
			},
			wantErr: false,
		},
		{
			name: "tool call request",
			req: &mcp.Request{
				Method: "tools/call",
				Params: mcp.RequestParams{
					Meta: &mcp.Meta{},
				},
			},
			wantErr: false, // Currently CEL validation is a TODO
		},
		{
			name: "resource read request",
			req: &mcp.Request{
				Method: "resources/read",
				Params: mcp.RequestParams{
					Meta: &mcp.Meta{},
				},
			},
			wantErr: false, // Currently CEL validation is a TODO
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := chain.Validate(context.Background(), tt.req)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLoggingHandler(t *testing.T) {
	// Create test logger
	logger := zaptest.NewLogger(t)

	// Create handler
	handler := NewLoggingHandler(logger)

	// Test request
	req := &mcp.Request{
		Method: "test",
		Params: mcp.RequestParams{
			Meta: &mcp.Meta{},
		},
	}

	// Test handler
	err := handler.Handle(context.Background(), req)
	assert.NoError(t, err)
}
func TestCELValidationHandler(t *testing.T) {
	// Create test logger
	logger := zaptest.NewLogger(t)

	// Create test config
	cfg := &config.Config{
		Policy: struct {
			RulesFile string `mapstructure:"rules_file"`
			Default   string `mapstructure:"default"`
		}{
			Default: "deny",
		},
	}

	// Create handler
	handler := NewCELValidationHandler(logger, cfg)

	// Test request
	req := &mcp.Request{
		Method: "test",
		Params: mcp.RequestParams{
			Meta: &mcp.Meta{},
		},
	}

	// Test handler
	err := handler.Handle(context.Background(), req)
	assert.NoError(t, err) // Currently CEL validation is a TODO
}
