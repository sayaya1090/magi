package mcp

import (
	"encoding/json"
	"testing"
)

// An rpcError is a Go error whose message is the server's own words.
func TestRPCErrorSpeaksTheServersWords(t *testing.T) {
	e := &rpcError{Code: -32601, Message: "method not found"}
	if e.Error() != "method not found" {
		t.Fatalf("got %q", e.Error())
	}
}

// The mcp tool's face is the server's declaration, verbatim — reshaping it here is how a console
// comes to show a tool the server never advertised.
func TestMCPToolFaceIsTheDeclaration(t *testing.T) {
	mt := &mcpTool{name: "mcp__ppt__render", description: "renders a slide", schema: json.RawMessage(`{"type":"object"}`)}
	if mt.Name() != "mcp__ppt__render" || mt.Description() != "renders a slide" {
		t.Fatal("the face must be the declaration")
	}
	var v any
	if err := json.Unmarshal(mt.Schema(), &v); err != nil {
		t.Fatalf("schema: %v", err)
	}
}
