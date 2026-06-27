package mcp

import (
	"bufio"
	"encoding/json"
	"io"
)

// runFakeServer implements a tiny MCP server (initialize / tools/list /
// tools/call) over newline-delimited JSON-RPC, for tests. It serves one tool,
// "echo", which returns its `text` argument. It runs until r reaches EOF.
func runFakeServer(r io.Reader, w io.Writer) {
	br := bufio.NewReaderSize(r, 1<<20)
	enc := func(v any) {
		b, _ := json.Marshal(v)
		w.Write(append(b, '\n'))
	}
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			var m struct {
				ID     *int64          `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if json.Unmarshal(line, &m) == nil {
				switch m.Method {
				case "initialize":
					enc(map[string]any{
						"jsonrpc": "2.0", "id": m.ID,
						"result": map[string]any{
							"protocolVersion": protocolVersion,
							"capabilities":    map[string]any{"tools": map[string]any{}},
							"serverInfo":      map[string]any{"name": "fake", "version": "0.0.1"},
						},
					})
				case "notifications/initialized":
					// no response
				case "tools/list":
					enc(map[string]any{
						"jsonrpc": "2.0", "id": m.ID,
						"result": map[string]any{
							"tools": []map[string]any{{
								"name":        "echo",
								"description": "Echo back the text argument.",
								"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}},
							}},
						},
					})
				case "tools/call":
					var p struct {
						Name      string         `json:"name"`
						Arguments map[string]any `json:"arguments"`
					}
					_ = json.Unmarshal(m.Params, &p)
					text, _ := p.Arguments["text"].(string)
					enc(map[string]any{
						"jsonrpc": "2.0", "id": m.ID,
						"result": map[string]any{
							"content": []map[string]any{{"type": "text", "text": "echo: " + text}},
							"isError": false,
						},
					})
				default:
					if m.ID != nil {
						enc(map[string]any{"jsonrpc": "2.0", "id": m.ID,
							"error": map[string]any{"code": -32601, "message": "method not found"}})
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}
