// Package mcp implements a minimal Model Context Protocol client over the stdio
// transport (newline-delimited JSON-RPC 2.0). It spawns an MCP server process,
// discovers its tools, and exposes them as port.Tool so the agent can call them
// alongside built-in and Lua-plugin tools. (D10, SPEC F-MCP)
package mcp

import "encoding/json"

const (
	jsonRPCVersion  = "2.0"
	protocolVersion = "2025-06-18" // MCP protocol revision the client speaks
)

// request is an outgoing JSON-RPC request.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// notification is an outgoing JSON-RPC notification (no id, no response).
type notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// message is a permissive view of any incoming line: it may be a response (has
// id + result/error) or a server-initiated request/notification (has method).
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a JSON-RPC error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

// ---- MCP payloads ----

type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      clientInfo     `json:"clientInfo"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type listToolsResult struct {
	Tools []toolDef `json:"tools"`
}

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// callToolResult is the result of tools/call. Content blocks are flattened to
// text for the agent's tool-result payload.
type callToolResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
