package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// oneError publishes a single error diagnostic for uri — the default fake behavior.
func oneError(send func(map[string]any), uri string) {
	send(map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics",
		"params": map[string]any{
			"uri": uri,
			"diagnostics": []map[string]any{{
				"range":    map[string]any{"start": map[string]any{"line": 0, "character": 0}},
				"severity": 1, "message": "boom",
			}},
		},
	})
}

// fakeLSPServer speaks just enough LSP over a pipe pair to exercise the warm pool:
// it answers initialize and, on each didOpen/didChange, runs onDoc to publish
// diagnostics for the opened URI. It closes its write side on client EOF so the
// client's reader goroutine unwinds cleanly.
func fakeLSPServer(t *testing.T) *lspClient { return fakeLSPServerWith(t, oneError) }

func fakeLSPServerWith(t *testing.T, onDoc func(send func(map[string]any), uri string)) *lspClient {
	t.Helper()
	caR, caW := io.Pipe() // client writes -> server reads
	cbR, cbW := io.Pipe() // server writes -> client reads

	send := func(m map[string]any) {
		b, _ := json.Marshal(m)
		writeFramed(cbW, string(b))
	}
	// EOF-safe framed read: returns nil when the client closes its write side
	// (the shared readFramed helper panics at EOF, which our forever-loop would hit).
	readOne := func(br *bufio.Reader) []byte {
		n := -1
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return nil
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
				fmt.Sscanf(strings.TrimSpace(v), "%d", &n)
			}
		}
		if n < 0 {
			return nil
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(br, buf); err != nil {
			return nil
		}
		return buf
	}
	go func() {
		sr := bufio.NewReader(caR)
		defer cbW.Close() // unblock the client's reader on our exit
		for {
			raw := readOne(sr)
			if len(raw) == 0 {
				return // client closed its write side (server killed)
			}
			var m map[string]json.RawMessage
			if json.Unmarshal(raw, &m) != nil {
				return
			}
			var method string
			_ = json.Unmarshal(m["method"], &method)
			if _, hasID := m["id"]; hasID && method == "initialize" {
				send(map[string]any{
					"jsonrpc": "2.0", "id": json.RawMessage(m["id"]),
					"result": map[string]any{"capabilities": map[string]any{}},
				})
				continue
			}
			if method == "textDocument/didOpen" || method == "textDocument/didChange" {
				var p struct {
					TextDocument struct {
						URI string `json:"uri"`
					} `json:"textDocument"`
				}
				_ = json.Unmarshal(m["params"], &p)
				onDoc(send, p.TextDocument.URI)
			}
		}
	}()

	return &lspClient{cmd: nil, in: caW, out: bufio.NewReader(cbR)}
}

// resetPool installs fakes and clears pool state; returns a cleanup and a spawn counter.
func resetPool(t *testing.T, spawn func(srv lspServer, workdir string) (*lspClient, error)) *int32 {
	t.Helper()
	var count int32
	origSpawn, origLook := lspSpawn, lspLookPath
	lspLookPath = func(string) (string, error) { return "/fake/bin", nil }
	lspSpawn = func(srv lspServer, workdir string) (*lspClient, error) {
		atomic.AddInt32(&count, 1)
		return spawn(srv, workdir)
	}
	lspPool.mu.Lock()
	lspPool.warm = map[string]*warmLSP{}
	lspPool.advised = map[string]bool{}
	lspPool.mu.Unlock()
	t.Cleanup(func() {
		CloseLSPPool()
		lspSpawn, lspLookPath = origSpawn, origLook
	})
	return &count
}

func TestPoolWarmReuse(t *testing.T) {
	count := resetPool(t, func(lspServer, string) (*lspClient, error) { return fakeLSPServer(t), nil })
	wd := t.TempDir()
	f := filepath.Join(wd, "a.ts")
	if err := os.WriteFile(f, []byte("let x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		diags, missing, err := lspPool.Diagnose(ctx, wd, f)
		if err != nil || missing != "" {
			t.Fatalf("call %d: err=%v missing=%q", i, err, missing)
		}
		if !strings.Contains(diags, "error: boom") {
			t.Fatalf("call %d: diags = %q, want boom", i, diags)
		}
	}
	if got := atomic.LoadInt32(count); got != 1 {
		t.Errorf("spawned %d servers across 2 calls, want 1 (warm reuse)", got)
	}
}

func TestPoolSelfHeal(t *testing.T) {
	count := resetPool(t, func(lspServer, string) (*lspClient, error) { return fakeLSPServer(t), nil })
	wd := t.TempDir()
	f := filepath.Join(wd, "a.ts")
	os.WriteFile(f, []byte("x\n"), 0o644)
	ctx := context.Background()

	if _, _, err := lspPool.Diagnose(ctx, wd, f); err != nil {
		t.Fatal(err)
	}
	// Kill the warm server behind the pool's back.
	srv, _ := serverFor(f)
	key := poolKey(wd, srv.argv[0])
	lspPool.mu.Lock()
	w := lspPool.warm[key]
	lspPool.mu.Unlock()
	if w == nil {
		t.Fatal("no warm client after first call")
	}
	w.close()
	// Give the reader goroutine a moment to observe the dead pipe.
	time.Sleep(50 * time.Millisecond)

	diags, _, err := lspPool.Diagnose(ctx, wd, f)
	if err != nil {
		t.Fatalf("self-heal call: %v", err)
	}
	if !strings.Contains(diags, "boom") {
		t.Errorf("self-heal diags = %q", diags)
	}
	if got := atomic.LoadInt32(count); got != 2 {
		t.Errorf("spawned %d, want 2 (restart after death)", got)
	}
}

func TestPoolMissingServerAdviceOnce(t *testing.T) {
	origLook := lspLookPath
	lspLookPath = func(string) (string, error) { return "", fmt.Errorf("not found") }
	lspPool.mu.Lock()
	lspPool.advised = map[string]bool{}
	lspPool.mu.Unlock()
	t.Cleanup(func() { lspLookPath = origLook })

	wd := t.TempDir()
	f := filepath.Join(wd, "a.rs") // rust-analyzer, no prereq bootstrap
	os.WriteFile(f, []byte("fn main(){}\n"), 0o644)

	_, missing, err := lspPool.Diagnose(context.Background(), wd, f)
	if err != nil || missing != "rust-analyzer" {
		t.Fatalf("missing=%q err=%v, want rust-analyzer", missing, err)
	}
	// First AutoDiagnose advises, second is silent (session-once).
	first := AutoDiagnose(context.Background(), wd, f, "darwin")
	if !strings.Contains(first, "rustup component add rust-analyzer") {
		t.Errorf("first advice = %q", first)
	}
	if second := AutoDiagnose(context.Background(), wd, f, "darwin"); second != "" {
		t.Errorf("second advice = %q, want empty (advised once)", second)
	}
}

// The pool must skip an initial empty publish (server not-yet-analyzed) and return
// the populated set that follows within the quiet window — the freshness behavior
// that pull() implements on top of diagSeq.
func TestPoolSkipsInitialEmptyPublish(t *testing.T) {
	emptyThenErr := func(send func(map[string]any), uri string) {
		send(map[string]any{
			"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics",
			"params": map[string]any{"uri": uri, "diagnostics": []map[string]any{}},
		})
		time.Sleep(20 * time.Millisecond)
		oneError(send, uri)
	}
	count := resetPool(t, func(lspServer, string) (*lspClient, error) {
		return fakeLSPServerWith(t, emptyThenErr), nil
	})
	_ = count
	wd := t.TempDir()
	f := filepath.Join(wd, "a.ts")
	os.WriteFile(f, []byte("x\n"), 0o644)

	diags, _, err := lspPool.Diagnose(context.Background(), wd, f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diags, "boom") {
		t.Errorf("want populated set after empty publish, got %q", diags)
	}
}

func TestNormURI(t *testing.T) {
	for _, c := range []struct{ a, b string }{
		{"file:///a/b.py", "file:///a/b.py"},
		{"file:///a/b.py", "file://a/b.py"},
		{"file:///a/b.py", "/a/b.py"},
	} {
		if normURI(c.a) != normURI(c.b) {
			t.Errorf("normURI(%q)=%q != normURI(%q)=%q", c.a, normURI(c.a), c.b, normURI(c.b))
		}
	}
	if normURI("file:///a/b.py") == normURI("file:///a/c.py") {
		t.Error("distinct files must not normalize equal")
	}
}

func TestPoolCloseEmptiesWarm(t *testing.T) {
	resetPool(t, func(lspServer, string) (*lspClient, error) { return fakeLSPServer(t), nil })
	wd := t.TempDir()
	f := filepath.Join(wd, "a.ts")
	os.WriteFile(f, []byte("x\n"), 0o644)
	if _, _, err := lspPool.Diagnose(context.Background(), wd, f); err != nil {
		t.Fatal(err)
	}
	CloseLSPPool()
	lspPool.mu.Lock()
	n := len(lspPool.warm)
	lspPool.mu.Unlock()
	if n != 0 {
		t.Errorf("warm map has %d after CloseLSPPool, want 0", n)
	}
}
