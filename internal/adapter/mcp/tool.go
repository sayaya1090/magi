package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// mcpTool adapts a remote MCP tool to port.Tool. name is the NAMESPACED name the agent calls (so it
// cannot shadow a builtin or another server's tool — the registry replaces by name); remote is the
// server-side name the call is forwarded to.
type mcpTool struct {
	client      *Client
	name        string
	remote      string
	description string
	schema      json.RawMessage
	// imageDir is where a picture this tool returns is kept — the daemon's data directory. Empty
	// when the host gave none, and then images are reported as dropped rather than written
	// somewhere that disappears.
	imageDir string
}

func (t *mcpTool) Name() string            { return t.name }
func (t *mcpTool) Description() string     { return t.description }
func (t *mcpTool) Schema() json.RawMessage { return t.schema }

// Execute forwards the call to the MCP server and flattens its content blocks into the tool
// result: the text as text, and any images to disk with references beside it.
//
// The flattening used to be text-only, and an image block carries no text — so a server that
// answered with a rendered slide returned the empty string and the model was told the call had
// worked and produced nothing.
func (t *mcpTool) Execute(ctx context.Context, args json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	res, err := t.client.CallTool(cctx, t.remote, args)
	if err != nil {
		b, _ := json.Marshal(err.Error())
		return session.ToolResult{Content: b, IsError: true}, nil
	}

	kept, notes := keepImages(t.imageDir, string(env.SessionID), t.name, res.Content)
	var sb strings.Builder
	for _, c := range res.Content {
		if c.Type == "image" {
			continue // 그림은 글이 아니다 — 아래에서 한 줄로 말하고, 파일은 kept가 든다
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(c.Text)
	}
	// The text says a picture came back, because the text is what every reader gets: a model whose
	// backend cannot take images, a terminal, a log somebody greps a week later.
	for _, k := range kept {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("[image: " + k.MIME + " at " + k.Path + "]")
	}
	for _, n := range notes {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(n)
	}
	b, _ := json.Marshal(sb.String())
	return session.ToolResult{Content: b, IsError: res.IsError, Images: kept}, nil
}
