package proxy

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

func (p *Proxy) initDownstreamClient(ctx context.Context) error {
	switch p.config.Transport.Type {
	case "stdio":
		return p.initStdioClient(ctx)
	case "sse":
		return p.initSSEClient(ctx)
	default:
		return fmt.Errorf("unsupported transport type: %s", p.config.Transport.Type)
	}
}

func (p *Proxy) initStdioClient(ctx context.Context) error {
	cl, err := client.NewStdioMCPClient(p.config.Transport.Command, nil, p.config.Transport.CommandArgs...)
	if err != nil {
		return fmt.Errorf("failed to create MCP client: %w", err)
	}

	p.client = cl
	return p.checkCapabilities(ctx)
}

func (p *Proxy) initSSEClient(ctx context.Context) error {
	cl, err := client.NewSSEMCPClient(
		p.config.Transport.DownstreamURL,
		client.WithHeaders(p.config.Transport.SSEConfig.Headers),
	)
	if err != nil {
		return fmt.Errorf("failed to create SSE client: %w", err)
	}

	p.client = cl
	return p.checkCapabilities(ctx)
}

func (p *Proxy) checkCapabilities(ctx context.Context) error {
	req := &mcp.InitializeRequest{
		Request: mcp.Request{
			Method: "initialize",
		},
		Params: mcp.InitializeParams{
			ProtocolVersion: "1.0",
			Capabilities:    mcp.ClientCapabilities{},
			ClientInfo: mcp.Implementation{
				Name:    "maybe-dont",
				Version: "0.0.1",
			},
		},
	}

	resp, err := p.client.Initialize(ctx, *req)
	if err != nil {
		return fmt.Errorf("failed to check MCP server capabilities: %w", err)
	}

	p.capabilities = &resp.Capabilities
	p.logger.Info("MCP server capabilities",
		zap.Any("capabilities", resp.Capabilities),
	)

	if p.capabilities.Tools != nil {
		toolsReq := &mcp.ListToolsRequest{
			PaginatedRequest: mcp.PaginatedRequest{
				Request: mcp.Request{
					Method: "tools/list",
				},
				Params: mcp.PaginatedParams{},
			},
		}

		toolsResp, err := p.client.ListTools(ctx, *toolsReq)
		if err != nil {
			return fmt.Errorf("failed to list available tools: %w", err)
		}

		p.logger.Info("Available tools from MCP server",
			zap.Any("tools", toolsResp.Tools),
		)
	}

	return nil
}
