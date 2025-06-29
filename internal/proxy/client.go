package proxy

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

func (p *Proxy) initDownstreamClient(ctx context.Context) error {
	switch p.config.Client.Type {
	case "stdio":
		return p.initStdioClient(ctx)
	case "sse":
		return p.initSSEClient(ctx)
	case "http":
		return p.initHTTPClient(ctx)
	default:
		return fmt.Errorf("unsupported transport type: %s", p.config.Client.Type)
	}
}

func (p *Proxy) initStdioClient(ctx context.Context) error {
	cl, err := client.NewStdioMCPClient(p.config.Client.Command, nil, p.config.Client.CommandArgs...)
	if err != nil {
		return fmt.Errorf("failed to create MCP client: %w", err)
	}

	p.client = cl
	return p.checkCapabilities(ctx)
}

func (p *Proxy) initSSEClient(ctx context.Context) error {
	cl, err := client.NewSSEMCPClient(
		p.config.Client.DownstreamURL,
		client.WithHeaders(p.config.Client.SSEConfig.Headers),
	)
	if err != nil {
		return fmt.Errorf("failed to create SSE client: %w", err)
	}

	p.client = cl
	return p.checkCapabilities(ctx)
}

func (p *Proxy) initHTTPClient(ctx context.Context) error {
	cl, err := client.NewStreamableHttpClient(
		p.config.Client.DownstreamURL,
		transport.WithHTTPHeaders(p.config.Client.HTTPConfig.Headers),
	)
	if err != nil {
		return fmt.Errorf("failed to create HTTP client: %w", err)
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
	p.logger.Debug("MCP server capabilities",
		zap.Any("capabilities", resp.Capabilities),
	)

	// Check and get details for each enabled capability
	if p.capabilities.Tools != nil && p.capabilities.Tools.ListChanged {
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

		p.capabilityDetails.Tools = toolsResp.Tools
		p.logger.Debug("Available tools from MCP server",
			zap.Any("tools", toolsResp.Tools),
		)
	}

	if p.capabilities.Prompts != nil && p.capabilities.Prompts.ListChanged {
		promptsReq := &mcp.ListPromptsRequest{
			PaginatedRequest: mcp.PaginatedRequest{
				Request: mcp.Request{
					Method: "prompts/list",
				},
				Params: mcp.PaginatedParams{},
			},
		}

		promptsResp, err := p.client.ListPrompts(ctx, *promptsReq)
		if err != nil {
			return fmt.Errorf("failed to list available prompts: %w", err)
		}

		p.capabilityDetails.Prompts = promptsResp.Prompts
		p.logger.Info("Available prompts from MCP server",
			zap.Any("prompts", promptsResp.Prompts),
		)
	}

	if p.capabilities.Resources != nil && p.capabilities.Resources.ListChanged {
		resourcesReq := &mcp.ListResourcesRequest{
			PaginatedRequest: mcp.PaginatedRequest{
				Request: mcp.Request{
					Method: "resources/list",
				},
				Params: mcp.PaginatedParams{},
			},
		}

		resourcesResp, err := p.client.ListResources(ctx, *resourcesReq)
		if err != nil {
			return fmt.Errorf("failed to list available resources: %w", err)
		}

		p.capabilityDetails.Resources = resourcesResp.Resources
		p.logger.Info("Available resources from MCP server",
			zap.Any("resources", resourcesResp.Resources),
		)

		// Also list resource templates if available
		templatesReq := &mcp.ListResourceTemplatesRequest{
			PaginatedRequest: mcp.PaginatedRequest{
				Request: mcp.Request{
					Method: "resources/templates/list",
				},
				Params: mcp.PaginatedParams{},
			},
		}

		templatesResp, err := p.client.ListResourceTemplates(ctx, *templatesReq)
		if err != nil {
			return fmt.Errorf("failed to list resource templates: %w", err)
		}

		p.capabilityDetails.Templates = templatesResp.ResourceTemplates
		p.logger.Info("Available resource templates from MCP server",
			zap.Any("templates", templatesResp.ResourceTemplates),
		)
	}

	return nil
}
