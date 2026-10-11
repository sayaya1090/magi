package figma

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/clients/appbridge"
)

func TestBridgeRegistersDesignToolsAndBoundChat(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "figma-bridge-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var mu sync.Mutex
	var received map[string]any
	go func() {
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			go func() {
				defer c.Close()
				scan := bufio.NewScanner(c)
				for scan.Scan() {
					var p map[string]any
					json.Unmarshal(scan.Bytes(), &p)
					out := map[string]any{"ok": true}
					switch p["method"] {
					case "sessions":
						out["sessions"] = []any{}
					case "session-new":
						out["session"] = "figma-session"
					case "submit":
						mu.Lock()
						received = p
						mu.Unlock()
					}
					json.NewEncoder(c).Encode(out)
				}
			}()
		}
	}()
	bridge, err := appbridge.New(appbridge.Config{App: "figma", Token: "test-only-token", Tools: Tools(), Instructions: Instructions, Socket: func(context.Context, string) (string, error) { return socket, nil }})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := bridge.Connect(context.Background(), "figma:test-file")
	if err != nil {
		t.Fatal(err)
	}
	_, err = bridge.Request(context.Background(), binding.ID, appbridge.Request{Method: "say", Text: "Align the title"})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if received["session"] != "figma-session" || received["text"] != "Align the title" {
		t.Fatalf("wrong bound chat: %#v", received)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+binding.ID+"/"+binding.Session, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer test-only-token")
	w := httptest.NewRecorder()
	bridge.Mux.ServeHTTP(w, req)
	var result struct {
		Result struct {
			Tools []appbridge.Tool `json:"tools"`
		} `json:"result"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Result.Tools) != 4 {
		t.Fatalf("catalog: %s", w.Body.String())
	}
	for _, tool := range result.Result.Tools {
		if !strings.HasPrefix(tool.Name, "figma_") {
			t.Fatal(tool.Name)
		}
	}
}

func TestReadToolSchemasOmitNullRequired(t *testing.T) {
	for _, tool := range Tools() {
		schema := tool.InputSchema.(map[string]any)
		if value, ok := schema["required"]; ok && value == nil {
			t.Fatalf("%s has null required", tool.Name)
		}
		if tool.ReadOnly {
			if _, ok := schema["required"]; ok {
				t.Fatalf("%s needs no arguments", tool.Name)
			}
		}
	}
}
