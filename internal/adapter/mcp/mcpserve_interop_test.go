package mcp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/mcpserve"
)

// magi's own MCP client, talking to magi's own MCP server.
//
// Both halves are in this tree and both have tests of their own, and that is exactly the situation
// where two implementations of one protocol agree with themselves and with nobody else. The server
// is tested by feeding it lines and reading lines back; the client is tested against a fake server
// written to match it. Neither says the two will connect.
//
// This wires them together over a pipe, which is the same shape as the subprocess a real caller
// starts (`magi --mcp --to design`), minus the process.
func TestMagisClientTalksToMagisServer(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills", "invoice-retry.md"), []byte(`---
description: the invoice job is not idempotent on retry
observed: 4
---
The idempotency key has to come from the request, not from a timestamp.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Two pipes: what the client writes is what the server reads, and back.
	toServer, clientWrites := io.Pipe()
	toClient, serverWrites := io.Pipe()
	srv := &mcpserve.Server{Name: "api", Role: "the billing API", Dir: dir}
	served := make(chan error, 1)
	go func() {
		served <- srv.Serve(context.Background(), toServer, serverWrites)
		serverWrites.Close()
	}()
	c := newClient(toClient, clientWrites, clientWrites)
	t.Cleanup(func() {
		c.Close()
		clientWrites.Close()
		select {
		case <-served:
		case <-time.After(2 * time.Second):
			t.Error("the server did not stop when its input closed")
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("the handshake failed: %v", err)
	}
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("listing tools failed: %v", err)
	}
	names := make([]string, len(tools))
	for i, tl := range tools {
		names[i] = tl.Name
		// The client hands this schema to a model. Empty, the model has nothing to fill in.
		if len(tl.InputSchema) == 0 {
			t.Errorf("%s arrived with no input schema", tl.Name)
		}
	}
	if got := strings.Join(names, ","); got != "about,knows,detail" {
		t.Fatalf("the client sees %q", got)
	}

	res, err := c.CallTool(ctx, "knows", json.RawMessage(`{"about":"idempotency on retry"}`))
	if err != nil {
		t.Fatalf("calling knows failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("knows refused: %+v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("knows answered with no content")
	}
	if !strings.Contains(res.Content[0].Text, "invoice-retry") {
		t.Errorf("the answer did not carry the entry:\n%s", res.Content[0].Text)
	}

	// And a refusal crosses as a refusal rather than as a transport error, which is the difference
	// between "try a different argument" and "this connection is broken".
	bad, err := c.CallTool(ctx, "knows", json.RawMessage(`{"about":""}`))
	if err != nil {
		t.Fatalf("an empty topic came back as a transport failure: %v", err)
	}
	if !bad.IsError {
		t.Error("an empty topic was answered rather than refused")
	}
}
