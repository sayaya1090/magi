package builtin

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
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

// call() sends a request, replies null to a server→client request it sees meanwhile
// (so the server can't block it), and returns the matching response.
func TestLSPCall(t *testing.T) {
	cliInR, cliInW := io.Pipe()   // client writes requests here; server reads
	srvOutR, srvOutW := io.Pipe() // server writes here; client reads
	c := &lspClient{in: cliInW, out: bufio.NewReader(srvOutR)}

	go func() {
		sr := bufio.NewReader(cliInR)
		_ = readFramed(sr) // the client's request (id 1)
		// Send a server→client request first; the client must reply null to it.
		writeFramed(srvOutW, `{"jsonrpc":"2.0","id":7,"method":"workspace/configuration"}`)
		_ = readFramed(sr) // the client's null reply to id 7
		// Then the real response to the original request.
		writeFramed(srvOutW, `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
	}()

	res, err := c.call("textDocument/definition", map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !strings.Contains(string(res), `"ok":true`) {
		t.Errorf("result = %s", res)
	}
}

// call() surfaces a JSON-RPC error response as a Go error.
func TestLSPCallError(t *testing.T) {
	cliInR, cliInW := io.Pipe()
	srvOutR, srvOutW := io.Pipe()
	c := &lspClient{in: cliInW, out: bufio.NewReader(srvOutR)}
	go func() {
		sr := bufio.NewReader(cliInR)
		_ = readFramed(sr)
		writeFramed(srvOutW, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"no"}}`)
	}()
	if _, err := c.call("bad", nil); err == nil {
		t.Error("an error response should return a Go error")
	}
}

// notify writes a message with no id (a notification, not a request).
func TestLSPNotify(t *testing.T) {
	var buf bytes.Buffer
	c := &lspClient{in: nopWriteCloser{&buf}}
	_ = c.notify("initialized", map[string]any{})
	var m map[string]json.RawMessage
	_ = json.Unmarshal(readFramed(bufio.NewReader(&buf)), &m)
	if _, hasID := m["id"]; hasID {
		t.Error("a notification must not carry an id")
	}
	if string(m["method"]) != `"initialized"` {
		t.Errorf("method = %s", m["method"])
	}
}
