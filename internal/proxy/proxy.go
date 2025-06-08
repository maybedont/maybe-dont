package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sudermanjr/maybe-dont/internal/config"
	"go.uber.org/zap"
)

// Proxy represents an MCP security proxy instance
type Proxy struct {
	logger *zap.Logger
	config *config.Config
	server *server.MCPServer
	client *client.Client
	mu     sync.RWMutex
}

// New creates a new proxy instance
func New(cfg *config.Config, logger *zap.Logger) (*Proxy, error) {
	return &Proxy{
		logger: logger,
		config: cfg,
	}, nil
}

// Start initializes and starts the proxy
func (p *Proxy) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Debug print the loaded config
	if cfgBytes, err := json.MarshalIndent(p.config, "", "  "); err == nil {
		p.logger.Debug("Loaded proxy config", zap.String("config", string(cfgBytes)))
	} else {
		p.logger.Warn("Failed to marshal config for debug print", zap.Error(err))
	}

	// Initialize downstream client based on transport type
	if err := p.initDownstreamClient(ctx); err != nil {
		return fmt.Errorf("failed to initialize downstream client: %w", err)
	}

	// Initialize server based on server type
	if err := p.initServer(ctx); err != nil {
		return fmt.Errorf("failed to initialize server: %w", err)
	}

	return nil
}

// Stop gracefully shuts down the proxy
func (p *Proxy) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.server != nil {
		// The server will be cleaned up by the context cancellation
		p.server = nil
	}

	if p.client != nil {
		// The client will be cleaned up by the context cancellation
		p.client = nil
	}

	return nil
}

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
	return nil
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
	return nil
}

func (p *Proxy) initServer(ctx context.Context) error {
	switch p.config.Server.Type {
	case "sse":
		return p.initSSEServer(ctx)
	default:
		return fmt.Errorf("unsupported server type: %s", p.config.Server.Type)
	}
}

func (p *Proxy) initSSEServer(ctx context.Context) error {
	srv := server.NewMCPServer("maybe-dont", "1.0.0",
		server.WithLogging(),
		server.WithRecovery(),
	)

	// Create SSE server
	sseSrv := server.NewSSEServer(srv,
		server.WithSSEEndpoint("/events"),
		server.WithMessageEndpoint("/message"),
	)

	p.server = srv
	go func() {
		// Start the SSE server
		if err := sseSrv.Start(p.config.Server.ListenAddr); err != nil {
			p.logger.Error("SSE server error", zap.Error(err))
		}
		// The SSE server will be cleaned up when the context is cancelled
		<-ctx.Done()
	}()
	return nil
}

func (p *Proxy) handleRequest(ctx context.Context, req *mcp.Request) (any, error) {
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}

	// Validate request parameters
	if err := p.validateRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Forward the request to the downstream client based on the request type
	switch req.Method {
	case "initialize":
		params := req.Params.Meta.AdditionalFields
		if params == nil {
			return nil, fmt.Errorf("missing parameters for initialize request")
		}
		initReq := &mcp.InitializeRequest{
			Params: mcp.InitializeParams{
				ProtocolVersion: mcp.ExtractString(params, "protocolVersion"),
				Capabilities:    mcp.ClientCapabilities{},
				ClientInfo: mcp.Implementation{
					Name:    mcp.ExtractString(params, "clientInfo.name"),
					Version: mcp.ExtractString(params, "clientInfo.version"),
				},
			},
		}
		return p.client.Initialize(ctx, *initReq)

	case "tools/call":
		params := req.Params.Meta.AdditionalFields
		if params == nil {
			return nil, fmt.Errorf("missing parameters for tools/call request")
		}
		name := mcp.ExtractString(params, "name")
		if name == "" {
			return nil, fmt.Errorf("missing tool name in tools/call request")
		}
		callReq := &mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      name,
				Arguments: mcp.ExtractMap(params, "arguments"),
			},
		}
		return p.client.CallTool(ctx, *callReq)

	case "prompts/get":
		params := req.Params.Meta.AdditionalFields
		if params == nil {
			return nil, fmt.Errorf("missing parameters for prompts/get request")
		}
		name := mcp.ExtractString(params, "name")
		if name == "" {
			return nil, fmt.Errorf("missing prompt name in prompts/get request")
		}
		getReq := &mcp.GetPromptRequest{
			Params: mcp.GetPromptParams{
				Name: name,
			},
		}
		return p.client.GetPrompt(ctx, *getReq)

	case "prompts/list":
		params := req.Params.Meta.AdditionalFields
		if params == nil {
			return nil, fmt.Errorf("missing parameters for prompts/list request")
		}
		listReq := &mcp.ListPromptsRequest{
			PaginatedRequest: mcp.PaginatedRequest{
				Request: mcp.Request{
					Method: "prompts/list",
				},
				Params: mcp.PaginatedParams{
					Cursor: mcp.Cursor(mcp.ExtractString(params, "cursor")),
				},
			},
		}
		return p.client.ListPrompts(ctx, *listReq)

	case "resources/templates/list":
		params := req.Params.Meta.AdditionalFields
		if params == nil {
			return nil, fmt.Errorf("missing parameters for resources/templates/list request")
		}
		listReq := &mcp.ListResourceTemplatesRequest{
			PaginatedRequest: mcp.PaginatedRequest{
				Request: mcp.Request{
					Method: "resources/templates/list",
				},
				Params: mcp.PaginatedParams{
					Cursor: mcp.Cursor(mcp.ExtractString(params, "cursor")),
				},
			},
		}
		return p.client.ListResourceTemplates(ctx, *listReq)

	case "resources/list":
		params := req.Params.Meta.AdditionalFields
		if params == nil {
			return nil, fmt.Errorf("missing parameters for resources/list request")
		}
		listReq := &mcp.ListResourcesRequest{
			PaginatedRequest: mcp.PaginatedRequest{
				Request: mcp.Request{
					Method: "resources/list",
				},
				Params: mcp.PaginatedParams{
					Cursor: mcp.Cursor(mcp.ExtractString(params, "cursor")),
				},
			},
		}
		return p.client.ListResources(ctx, *listReq)

	case "tools/list":
		params := req.Params.Meta.AdditionalFields
		if params == nil {
			return nil, fmt.Errorf("missing parameters for tools/list request")
		}
		listReq := &mcp.ListToolsRequest{
			PaginatedRequest: mcp.PaginatedRequest{
				Request: mcp.Request{
					Method: "tools/list",
				},
				Params: mcp.PaginatedParams{
					Cursor: mcp.Cursor(mcp.ExtractString(params, "cursor")),
				},
			},
		}
		return p.client.ListTools(ctx, *listReq)

	case "resources/read":
		params := req.Params.Meta.AdditionalFields
		if params == nil {
			return nil, fmt.Errorf("missing parameters for resources/read request")
		}
		uri := mcp.ExtractString(params, "uri")
		if uri == "" {
			return nil, fmt.Errorf("missing URI in resources/read request")
		}
		readReq := &mcp.ReadResourceRequest{
			Params: mcp.ReadResourceParams{
				URI: uri,
			},
		}
		return p.client.ReadResource(ctx, *readReq)

	case "level/set":
		params := req.Params.Meta.AdditionalFields
		if params == nil {
			return nil, fmt.Errorf("missing parameters for level/set request")
		}
		level := mcp.ExtractString(params, "level")
		if level == "" {
			return nil, fmt.Errorf("missing level in level/set request")
		}
		setReq := &mcp.SetLevelRequest{
			Params: mcp.SetLevelParams{
				Level: mcp.LoggingLevel(level),
			},
		}
		return nil, p.client.SetLevel(ctx, *setReq)

	case "ping":
		return nil, p.client.Ping(ctx)

	case "subscribe":
		params := req.Params.Meta.AdditionalFields
		if params == nil {
			return nil, fmt.Errorf("missing parameters for subscribe request")
		}
		uri := mcp.ExtractString(params, "uri")
		if uri == "" {
			return nil, fmt.Errorf("missing URI in subscribe request")
		}
		subReq := &mcp.SubscribeRequest{
			Params: mcp.SubscribeParams{
				URI: uri,
			},
		}
		return nil, p.client.Subscribe(ctx, *subReq)

	case "unsubscribe":
		params := req.Params.Meta.AdditionalFields
		if params == nil {
			return nil, fmt.Errorf("missing parameters for unsubscribe request")
		}
		uri := mcp.ExtractString(params, "uri")
		if uri == "" {
			return nil, fmt.Errorf("missing URI in unsubscribe request")
		}
		unsubReq := &mcp.UnsubscribeRequest{
			Params: mcp.UnsubscribeParams{
				URI: uri,
			},
		}
		return nil, p.client.Unsubscribe(ctx, *unsubReq)

	default:
		return nil, fmt.Errorf("unsupported method: %s", req.Method)
	}
}

// validateRequest validates the request structure and required fields
func (p *Proxy) validateRequest(req *mcp.Request) error {
	if req.Method == "" {
		return fmt.Errorf("missing method")
	}

	if req.Params.Meta == nil {
		return fmt.Errorf("missing metadata")
	}

	return nil
}
