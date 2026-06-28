package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// A minimal LSP JSON-RPC (stdio) client for languages other than Go (Go keeps the
// simpler gopls CLI path). It spawns a language server, initializes, opens the file,
// issues ONE navigation request, and shuts down — enough for definition / references
// / documentSymbol without a long-lived server. Degrades cleanly when the server
// binary is absent.

type lspServer struct {
	argv   []string
	langID string
}

// serverFor maps a file extension to its language server, or ok=false if unsupported.
func serverFor(path string) (lspServer, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".tsx", ".mts", ".cts":
		return lspServer{[]string{"typescript-language-server", "--stdio"}, "typescript"}, true
	case ".js", ".jsx", ".mjs", ".cjs":
		return lspServer{[]string{"typescript-language-server", "--stdio"}, "javascript"}, true
	case ".py":
		return lspServer{[]string{"pyright-langserver", "--stdio"}, "python"}, true
	case ".rs":
		return lspServer{[]string{"rust-analyzer"}, "rust"}, true
	case ".c", ".h":
		return lspServer{[]string{"clangd"}, "c"}, true
	case ".cc", ".cpp", ".cxx", ".hpp", ".hh":
		return lspServer{[]string{"clangd"}, "cpp"}, true
	}
	return lspServer{}, false
}

type lspClient struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
	id  int
}

func startLSP(ctx context.Context, srv lspServer, workdir string) (*lspClient, error) {
	if _, err := exec.LookPath(srv.argv[0]); err != nil {
		return nil, fmt.Errorf("%s is not installed (needed for LSP navigation of this language)", srv.argv[0])
	}
	cmd := exec.CommandContext(ctx, srv.argv[0], srv.argv[1:]...)
	cmd.Dir = workdir
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &lspClient{cmd: cmd, in: in, out: bufio.NewReader(out)}, nil
}

func (c *lspClient) close() {
	_ = c.in.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
}

func (c *lspClient) writeMsg(m map[string]any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n", len(b)); err != nil {
		return err
	}
	_, err = c.in.Write(b)
	return err
}

func (c *lspClient) readMsg() (map[string]json.RawMessage, error) {
	length := -1
	for {
		line, err := c.out.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			fmt.Sscanf(strings.TrimSpace(v), "%d", &length)
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(c.out, buf); err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *lspClient) notify(method string, params any) error {
	return c.writeMsg(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// call sends a request and returns its result, replying null to any server→client
// request encountered meanwhile (e.g. workspace/configuration) so the server doesn't
// block, and ignoring notifications.
func (c *lspClient) call(method string, params any) (json.RawMessage, error) {
	c.id++
	id := c.id
	if err := c.writeMsg(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		m, err := c.readMsg()
		if err != nil {
			return nil, err
		}
		idRaw, hasID := m["id"]
		_, hasMethod := m["method"]
		switch {
		case hasID && hasMethod: // server→client request — reply null to unblock it
			var raw json.RawMessage = idRaw
			_ = c.writeMsg(map[string]any{"jsonrpc": "2.0", "id": raw, "result": nil})
		case hasID: // a response
			var mid int
			if json.Unmarshal(idRaw, &mid) == nil && mid == id {
				if e, ok := m["error"]; ok {
					return nil, fmt.Errorf("lsp error: %s", string(e))
				}
				return m["result"], nil
			}
		}
		// notification (method, no id) → ignore
	}
}

// lspQuery runs one navigation request against the server for path. method is an LSP
// method like "textDocument/definition". For documentSymbol, line/char are ignored.
func lspQuery(ctx context.Context, workdir, absPath, method string, line, char int) (json.RawMessage, error) {
	srv, ok := serverFor(absPath)
	if !ok {
		return nil, fmt.Errorf("no LSP server configured for %s", filepath.Ext(absPath))
	}
	c, err := startLSP(ctx, srv, workdir)
	if err != nil {
		return nil, err
	}
	defer c.close()

	rootURI := "file://" + workdir
	if _, err := c.call("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      rootURI,
		"capabilities": map[string]any{},
	}); err != nil {
		return nil, err
	}
	_ = c.notify("initialized", map[string]any{})

	uri := "file://" + absPath
	data, _ := os.ReadFile(absPath)
	_ = c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": uri, "languageId": srv.langID, "version": 1, "text": string(data)},
	})

	var params map[string]any
	if method == "textDocument/documentSymbol" {
		params = map[string]any{"textDocument": map[string]any{"uri": uri}}
	} else {
		params = map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": line, "character": char},
		}
		if method == "textDocument/references" {
			params["context"] = map[string]any{"includeDeclaration": true}
		}
	}
	return c.call(method, params)
}

// lspLocation is one LSP Location (also covers LocationLink via targetUri/range).
type lspLocation struct {
	URI         string  `json:"uri"`
	TargetURI   string  `json:"targetUri"`
	Range       lspRng  `json:"range"`
	TargetRange *lspRng `json:"targetRange"`
}
type lspRng struct {
	Start lspPos `json:"start"`
}
type lspPos struct {
	Line int `json:"line"`
	Char int `json:"character"`
}

// formatLocations renders an LSP definition/references result as workspace-relative
// "file:line:col" lines (1-based).
func formatLocations(result json.RawMessage, workdir string) string {
	if len(result) == 0 || string(result) == "null" {
		return ""
	}
	var locs []lspLocation
	if json.Unmarshal(result, &locs) != nil {
		var one lspLocation // a single Location, not an array
		if json.Unmarshal(result, &one) != nil {
			return ""
		}
		locs = []lspLocation{one}
	}
	var lines []string
	for _, l := range locs {
		uri, rng := l.URI, l.Range
		if uri == "" {
			uri = l.TargetURI
		}
		if l.TargetRange != nil {
			rng = *l.TargetRange
		}
		path := strings.TrimPrefix(uri, "file://")
		if rel, err := filepath.Rel(workdir, path); err == nil && !strings.HasPrefix(rel, "..") {
			path = rel
		}
		lines = append(lines, fmt.Sprintf("%s:%d:%d", path, rng.Start.Line+1, rng.Start.Char+1))
	}
	return strings.Join(lines, "\n")
}

// formatSymbols renders an LSP documentSymbol result (hierarchical DocumentSymbol[]
// or flat SymbolInformation[]) as a simple "name (kind) :line" outline.
func formatSymbols(result json.RawMessage) string {
	if len(result) == 0 || string(result) == "null" {
		return ""
	}
	type docSym struct {
		Name     string          `json:"name"`
		Kind     int             `json:"kind"`
		Range    lspRng          `json:"range"`
		Location *lspLocation    `json:"location"` // SymbolInformation form
		Children json.RawMessage `json:"children"`
	}
	var syms []docSym
	if json.Unmarshal(result, &syms) != nil {
		return ""
	}
	var lines []string
	var walk func(s docSym, depth int)
	walk = func(s docSym, depth int) {
		line := s.Range.Start.Line + 1
		if s.Location != nil {
			line = s.Location.Range.Start.Line + 1
		}
		lines = append(lines, fmt.Sprintf("%s%s (%s) :%d", strings.Repeat("  ", depth), s.Name, symbolKind(s.Kind), line))
		if len(s.Children) > 0 {
			var kids []docSym
			if json.Unmarshal(s.Children, &kids) == nil {
				for _, k := range kids {
					walk(k, depth+1)
				}
			}
		}
	}
	for _, s := range syms {
		walk(s, 0)
	}
	return strings.Join(lines, "\n")
}

// symbolKind maps the LSP SymbolKind enum to a short label.
func symbolKind(k int) string {
	names := []string{"", "file", "module", "namespace", "package", "class", "method",
		"property", "field", "constructor", "enum", "interface", "function", "variable",
		"constant", "string", "number", "boolean", "array", "object", "key", "null",
		"enum-member", "struct", "event", "operator", "type-param"}
	if k >= 0 && k < len(names) && names[k] != "" {
		return names[k]
	}
	return "symbol"
}

// utf16Col converts a byte index within lineText to a 0-based UTF-16 column (the LSP
// position encoding).
func utf16Col(lineText string, byteIdx int) int {
	if byteIdx > len(lineText) {
		byteIdx = len(lineText)
	}
	n := 0
	for _, r := range lineText[:byteIdx] {
		if r > 0xffff {
			n += 2
		} else {
			n++
		}
	}
	return n
}
