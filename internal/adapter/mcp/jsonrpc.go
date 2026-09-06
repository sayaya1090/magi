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
	// Annotations are the server's hints about the tool. Only readOnlyHint is read: it says the
	// tool changes nothing, which makes its result re-derivable — what the context folder gives
	// up first. Absent means "not declared", which is treated as not read-only.
	Annotations *toolAnnotations `json:"annotations,omitempty"`
}

type toolAnnotations struct {
	ReadOnlyHint bool `json:"readOnlyHint"`
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

// contentBlock is one piece of a tool's answer. MCP defines several kinds and this daemon reads
// two of them: text, and images.
//
// It read only text until 2026-08-28, and the shape said so — {Type, Text}. An image block carries
// no text field at all, so a server that answered with a rendered slide produced the empty string:
// the tool "succeeded" and returned nothing, which is the worst of the three possible failures
// (the other two being an error and a refusal, both of which say something).
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	// Data is base64 as the protocol sends it, and MimeType names what it decodes to. Kept as the
	// wire spells them: the decode belongs where the bytes are given somewhere to live, not here.
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}
