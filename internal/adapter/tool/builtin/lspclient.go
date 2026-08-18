package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
// Each entry names the server binary invoked over stdio; when it isn't on PATH the
// query degrades gracefully (startLSP returns an error the tool reports). Go is not
// here — it takes the dedicated gopls CLI path in lspnav/lspdiag.
func serverFor(path string) (lspServer, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	// TypeScript / JavaScript
	case ".ts", ".mts", ".cts":
		return lspServer{[]string{"typescript-language-server", "--stdio"}, "typescript"}, true
	case ".tsx":
		return lspServer{[]string{"typescript-language-server", "--stdio"}, "typescriptreact"}, true
	case ".js", ".mjs", ".cjs":
		return lspServer{[]string{"typescript-language-server", "--stdio"}, "javascript"}, true
	case ".jsx":
		return lspServer{[]string{"typescript-language-server", "--stdio"}, "javascriptreact"}, true
	// Python
	case ".py", ".pyi":
		return lspServer{[]string{"pyright-langserver", "--stdio"}, "python"}, true
	// Rust
	case ".rs":
		return lspServer{[]string{"rust-analyzer"}, "rust"}, true
	// C / C++ / Objective-C (all via clangd)
	case ".c", ".h":
		return lspServer{[]string{"clangd"}, "c"}, true
	case ".cc", ".cpp", ".cxx", ".c++", ".hpp", ".hh", ".hxx", ".h++":
		return lspServer{[]string{"clangd"}, "cpp"}, true
	case ".m":
		return lspServer{[]string{"clangd"}, "objective-c"}, true
	case ".mm":
		return lspServer{[]string{"clangd"}, "objective-cpp"}, true
	// JVM languages
	case ".java":
		return lspServer{[]string{"jdtls"}, "java"}, true
	case ".kt", ".kts":
		return lspServer{[]string{"kotlin-language-server"}, "kotlin"}, true
	case ".scala", ".sc":
		return lspServer{[]string{"metals"}, "scala"}, true
	case ".groovy", ".gradle":
		return lspServer{[]string{"groovy-language-server"}, "groovy"}, true
	// .NET
	case ".cs":
		return lspServer{[]string{"csharp-ls"}, "csharp"}, true
	case ".fs", ".fsx":
		return lspServer{[]string{"fsautocomplete", "--adaptive-lsp-server-enabled"}, "fsharp"}, true
	// Scripting / dynamic
	case ".rb":
		return lspServer{[]string{"ruby-lsp"}, "ruby"}, true
	case ".php":
		return lspServer{[]string{"intelephense", "--stdio"}, "php"}, true
	case ".lua":
		return lspServer{[]string{"lua-language-server"}, "lua"}, true
	case ".sh", ".bash":
		return lspServer{[]string{"bash-language-server", "start"}, "shellscript"}, true
	case ".pl", ".pm":
		return lspServer{[]string{"perl", "-MPerl::LanguageServer", "-e", "Perl::LanguageServer::run"}, "perl"}, true
	// Systems / compiled
	case ".swift":
		return lspServer{[]string{"sourcekit-lsp"}, "swift"}, true
	case ".zig":
		return lspServer{[]string{"zls"}, "zig"}, true
	case ".hs":
		return lspServer{[]string{"haskell-language-server-wrapper", "--lsp"}, "haskell"}, true
	case ".ml", ".mli":
		return lspServer{[]string{"ocamllsp"}, "ocaml"}, true
	case ".dart":
		return lspServer{[]string{"dart", "language-server"}, "dart"}, true
	case ".ex", ".exs":
		return lspServer{[]string{"elixir-ls"}, "elixir"}, true
	case ".jl":
		return lspServer{[]string{"julia", "--startup-file=no", "-e", "using LanguageServer; runserver()"}, "julia"}, true
	// Web / markup / config
	case ".vue":
		return lspServer{[]string{"vue-language-server", "--stdio"}, "vue"}, true
	case ".svelte":
		return lspServer{[]string{"svelteserver", "--stdio"}, "svelte"}, true
	case ".html", ".htm":
		return lspServer{[]string{"vscode-html-language-server", "--stdio"}, "html"}, true
	case ".css":
		return lspServer{[]string{"vscode-css-language-server", "--stdio"}, "css"}, true
	case ".scss":
		return lspServer{[]string{"vscode-css-language-server", "--stdio"}, "scss"}, true
	case ".less":
		return lspServer{[]string{"vscode-css-language-server", "--stdio"}, "less"}, true
	case ".json", ".jsonc":
		return lspServer{[]string{"vscode-json-language-server", "--stdio"}, "json"}, true
	case ".yaml", ".yml":
		return lspServer{[]string{"yaml-language-server", "--stdio"}, "yaml"}, true
	case ".toml":
		return lspServer{[]string{"taplo", "lsp", "stdio"}, "toml"}, true
	case ".tf", ".tfvars":
		return lspServer{[]string{"terraform-ls", "serve"}, "terraform"}, true
	case ".md", ".markdown":
		return lspServer{[]string{"marksman", "server"}, "markdown"}, true
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
		_ = in.Close()
		_ = out.Close()
		return nil, err
	}
	return &lspClient{cmd: cmd, in: in, out: bufio.NewReader(out)}, nil
}

func (c *lspClient) close() {
	_ = c.in.Close()
	if c.cmd == nil { // in-process fake (tests): no OS process to reap
		return
	}
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

// lspRng / lspPos are the position shapes the diagnostics decoder reads out of a
// publishDiagnostics notification.
type lspRng struct {
	Start lspPos `json:"start"`
}
type lspPos struct {
	Line int `json:"line"`
	Char int `json:"character"`
}
