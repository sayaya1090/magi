package builtin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// writeFramed/readFramed mirror the client's Content-Length framing for the fake server.
func writeFramed(w io.Writer, body string) {
	fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(body), body)
}

func readFramed(br *bufio.Reader) []byte {
	n := -1
	for {
		line, _ := br.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			fmt.Sscanf(strings.TrimSpace(v), "%d", &n)
		}
	}
	buf := make([]byte, n)
	io.ReadFull(br, buf)
	return buf
}

// writeMsg → readMsg roundtrip: the Content-Length framing encodes and decodes.
func TestLSPFraming(t *testing.T) {
	var buf bytes.Buffer
	w := &lspClient{in: nopWriteCloser{&buf}}
	if err := w.writeMsg(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}); err != nil {
		t.Fatal(err)
	}
	r := &lspClient{out: bufio.NewReader(&buf)}
	m, err := r.readMsg()
	if err != nil {
		t.Fatal(err)
	}
	if string(m["method"]) != `"ping"` || string(m["id"]) != "1" {
		t.Errorf("decoded %v", m)
	}
}

// A request gets its answer, and a server→client request seen on the way does not block it.
//
// This used to be asked of lspClient.call, a single-shot request/response that nothing calls any
// more: the pool runs one reader for a connection's life and hands answers to waiters by id
// (warmLSP.reader / warmLSP.request), which is the only version that reaches a real server now.
// The behaviour is the same and it had no test on the live side — the two below were the whole of
// it, and they were pointed at code no binary could run.
//
// workspace/configuration is the one that actually arrives: a server asks for settings mid-request
// and waits for the reply, so a client that ignored it would hang on its own answer.
func TestPoolRequestAnswersAServerRequestMeanwhile(t *testing.T) {
	cliInR, cliInW := io.Pipe()
	srvOutR, srvOutW := io.Pipe()
	w := &warmLSP{
		cli:     &lspClient{in: cliInW, out: bufio.NewReader(srvOutR)},
		waiters: map[int]chan rpcResult{},
		updated: make(chan struct{}, 1),
	}
	go w.reader()

	replied := make(chan []byte, 1)
	go func() {
		sr := bufio.NewReader(cliInR)
		_ = readFramed(sr) // our request, id 1
		writeFramed(srvOutW, `{"jsonrpc":"2.0","id":7,"method":"workspace/configuration"}`)
		replied <- readFramed(sr) // whatever the client says back to id 7
		writeFramed(srvOutW, `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := w.request(ctx, "textDocument/definition", map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if !strings.Contains(string(res), `"ok":true`) {
		t.Errorf("result = %s", res)
	}
	select {
	case got := <-replied:
		// null, and addressed to the id the server asked under — a reply to the wrong id leaves
		// the server waiting exactly as silence would.
		if !strings.Contains(string(got), `"id":7`) || !strings.Contains(string(got), `"result":null`) {
			t.Errorf("the client answered the server's request with %s", got)
		}
	default:
		t.Error("the client never answered the server's request")
	}
}

// A JSON-RPC error response comes back as a Go error rather than as an empty result.
func TestPoolRequestSurfacesAnErrorResponse(t *testing.T) {
	cliInR, cliInW := io.Pipe()
	srvOutR, srvOutW := io.Pipe()
	w := &warmLSP{
		cli:     &lspClient{in: cliInW, out: bufio.NewReader(srvOutR)},
		waiters: map[int]chan rpcResult{},
		updated: make(chan struct{}, 1),
	}
	go w.reader()
	go func() {
		sr := bufio.NewReader(cliInR)
		_ = readFramed(sr)
		writeFramed(srvOutW, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"no"}}`)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := w.request(ctx, "bad", nil); err == nil {
		t.Error("an error response should return a Go error")
	}
}

// notify writes a message with no id (a notification, not a request).
func TestLSPNotify(t *testing.T) {
	var buf bytes.Buffer
	c := &lspClient{in: nopWriteCloser{&buf}}
	_ = c.writeMsg(map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}})
	var m map[string]json.RawMessage
	_ = json.Unmarshal(readFramed(bufio.NewReader(&buf)), &m)
	if _, hasID := m["id"]; hasID {
		t.Error("a notification must not carry an id")
	}
	if string(m["method"]) != `"initialized"` {
		t.Errorf("method = %s", m["method"])
	}
}
