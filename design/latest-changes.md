Summary

I've implemented per-session downstream client management using the MCP server SDK's session lifecycle hooks. Here are the key changes:

1. session.go - Complete Rewrite

- Created SessionClientInfo struct to hold downstream client info per session
- Created Session struct to represent an upstream session with its downstream clients
- Refactored SessionManager to manage sessions and their clients
- Added GetSessionIDFromContext() to extract session ID using server.ClientSessionFromContext()
- Added proper cleanup with DeleteSession() and CloseAllSessions()

2. client_manager.go - Major Refactor

- Removed ClientInfo struct (replaced by SessionClientInfo)
- Changed from storing active clients to storing client configurations (clientConfigs)
- Added CreateSessionClients() - creates downstream clients when an upstream session starts
- Added CloseSessionClients() - closes downstream clients when a session ends
- Added GetSessionClient() - retrieves a downstream client for a specific session
- Updated Close() to take context and close all sessions
- Renamed all retry/discovery functions to use Session prefix

3. server.go - Session Hooks

- Added onSessionRegister() hook - creates downstream clients when session starts
- Added onSessionUnregister() hook - closes downstream clients when session ends
- Updated initMCPServer() to register session lifecycle hooks instead of the old lazy-init approach
- Removed unused registerClient* functions

4. gateway.go - Session-Aware Handlers

- Updated HandleToolCall() to get session ID from context and route to session-specific client
- Updated HandlePromptCall() similarly
- Updated handleResourceRequest() similarly
- Updated Stop() to pass context to Close()

Architecture

┌──────────────────┐     ┌───────────────────────────────┐     ┌────────────────┐
│  Upstream Client │     │         Gateway               │     │ Downstream MCP │
│  (Session A)     │────▶│  Session A                    │     │    Servers     │
└──────────────────┘     │    ├── aws client (A)    ─────────▶│  AWS           │
│    └── github client (A) ─────────▶│  GitHub        │
┌──────────────────┐     │                                │     └────────────────┘
│  Upstream Client │────▶│  Session B                    │
│  (Session B)     │     │    ├── aws client (B)    ─────────▶│  (Separate     │
└──────────────────┘     │    └── github client (B) ─────────▶│   connections) │
└───────────────────────────────┘

Each upstream session gets its own dedicated downstream connections, with automatic cleanup when sessions end.
