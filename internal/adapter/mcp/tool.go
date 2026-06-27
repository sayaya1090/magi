package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// mcpTool adapts a remote MCP tool to port.Tool.
type mcpTool struct {
	client      *Client
	name        string
	description string
	schema      json.RawMessage
}

func (t *mcpTool) Name() string            { return t.name }
func (t *mcpTool) Description() string     { return t.description }
func (t *mcpTool) Schema() json.RawMessage { return t.schema }

// Execute forwards the call to the MCP server and flattens its content blocks
// into the tool result text.
func (t *mcpTool) Execute(ctx context.Context, args json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	res, err := t.client.CallTool(cctx, t.name, args)
	if err != nil {
		b, _ := json.Marshal(err.Error())
		return session.ToolResult{Content: b, IsError: true}, nil
	}

	var sb strings.Builder
	for i, c := range res.Content {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(c.Text)
	}
	b, _ := json.Marshal(sb.String())
	return session.ToolResult{Content: b, IsError: res.IsError}, nil
}
