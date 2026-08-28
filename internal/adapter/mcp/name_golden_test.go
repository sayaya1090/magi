package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A tool's registered name is a contract between two implementations, the same way the socket name
// is: this daemon builds mcp__<server>__<tool> here, and the JetBrains plugin builds it again in
// Kotlin to know what it may call. A server name that sanitises differently on the two sides names
// two different tools, and the symptom is a tool that "is not there".
//
// Its own file, and its own producer. The socket golden is written whole by a test in package
// daemon; a second producer writing into that same file would erase whatever the other one put
// there, and which half survived would depend on who ran last — a failure that reads as "I
// regenerated the golden and now it fails" with nothing in the file to explain it.
//
// Note these are two different functions with nearly the same name: the daemon's sanitize maps an
// unaccepted character to '-', this one maps it to '_' and answers "x" for a name that sanitises
// away to nothing. Both are in goldens now, in separate files, for that reason.
//
// Regenerate deliberately:
//
//	MAGI_GOLDEN_UPDATE=1 go test ./internal/adapter/mcp/ -run Golden
const nameGoldenFile = "../../../clients/jetbrains/plugin/core/src/test/resources/mcpname-golden.json"

func TestToolNameGoldens(t *testing.T) {
	type golden struct {
		Regenerate       string      `json:"regenerate"`
		SanitizeToolPart [][2]string `json:"sanitizeToolPart"`
		Namespaced       [][3]string `json:"namespacedToolName"` // server, tool, registered name
	}
	// No lone surrogate here on purpose. Go and the JVM disagree about one ("\uD800" is three
	// invalid runes to Go and one character to Kotlin), but the disagreement cannot be reached: a
	// JVM writing UTF-8 replaces an unpaired surrogate before it reaches the wire, so both sides
	// see the same bytes. A golden holding it would fail forever over a difference nothing can
	// produce.
	got := golden{
		Regenerate: "The MCP tool-name rule moved. Both sides build it — internal/adapter/mcp/manager.go " +
			"(namespacedToolName, sanitizeToolPart) and the plugin's McpName — and a difference names two " +
			"different tools, which reads as a tool that is not there. Fix both, then regenerate: " +
			"MAGI_GOLDEN_UPDATE=1 go test ./internal/adapter/mcp/ -run Golden",
	}
	for _, in := range []string{"a😀b", "", "...", "ppt.one", "한글", "😀", "jetbrains"} {
		got.SanitizeToolPart = append(got.SanitizeToolPart, [2]string{in, sanitizeToolPart(in)})
	}
	for _, pair := range [][2]string{{"ppt", "render"}, {"ppt.one", "open file"}, {"", "x"}} {
		got.Namespaced = append(got.Namespaced,
			[3]string{pair[0], pair[1], namespacedToolName(pair[0], pair[1])})
	}

	body, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')

	if os.Getenv("MAGI_GOLDEN_UPDATE") != "" {
		if err := os.MkdirAll(filepath.Dir(nameGoldenFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(nameGoldenFile, body, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("golden rewritten: %s", nameGoldenFile)
		return
	}
	want, err := os.ReadFile(nameGoldenFile)
	if err != nil {
		t.Fatalf("%v — write it with MAGI_GOLDEN_UPDATE=1 go test ./internal/adapter/mcp/ -run Golden", err)
	}
	if string(want) != string(body) {
		t.Errorf("%s\n\nwas:\n%s\n\nnow:\n%s", got.Regenerate, want, body)
	}
}
