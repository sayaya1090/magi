package mcp

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func tool(schema string, readOnly bool) *mcpTool {
	return &mcpTool{name: "mcp__ide__t", schema: json.RawMessage(schema), readOnly: readOnly}
}

// An editor plugin's edit has to be visible to the core AS an edit. It was not: the core recognised
// file tools by name, and `mcp__jetbrains__apply_edit` is nothing by name — so a turn that did its
// work through the IDE read as a turn that changed nothing, with no error anywhere.
func TestAPathNamingWriterDeclaresTheFileItChanges(t *testing.T) {
	got := declared(tool(`{"type":"object","properties":{"path":{"type":"string"},"old":{"type":"string"}}}`, false))
	ft, ok := got.(port.FileTool)
	if !ok {
		t.Fatal("a tool whose schema names a path does not declare port.FileTool")
	}
	if !ft.WritesFile() {
		t.Error("a tool that did not say it is read-only is not treated as a writer")
	}
	if p := ft.FileArg(json.RawMessage(`{"path":" src/a.ts ","old":"x"}`)); p != "src/a.ts" {
		t.Errorf("FileArg = %q, want src/a.ts (trimmed)", p)
	}
}

// `show` opens a file and changes nothing. Reading it as a write would put the path in the record's
// list of what this turn changed — a change that is not on disk.
func TestAReadOnlyToolLooksRatherThanWrites(t *testing.T) {
	got := declared(tool(`{"type":"object","properties":{"path":{"type":"string"},"line":{"type":"integer"}}}`, true))
	ft, ok := got.(port.FileTool)
	if !ok {
		t.Fatal("a read-only path tool should still declare — the core needs to know it LOOKED")
	}
	if ft.WritesFile() {
		t.Error("a tool that declared readOnlyHint is treated as a writer")
	}
}

// The expensive half. A tool that names no path must keep declaring nothing: the core runs
// post-edit diagnostics and PostToolUse hooks on whatever a declared writer names, and against ""
// that is a language-server round trip on a file that does not exist — on every deck call.
func TestAToolThatNamesNoFileDeclaresNothing(t *testing.T) {
	for _, schema := range []string{
		`{"type":"object","properties":{"title":{"type":"string"}}}`,
		`{"type":"object"}`,
		`{"type":"object","properties":{}}`,
		`not json at all`,
	} {
		if _, ok := declared(tool(schema, false)).(port.FileTool); ok {
			t.Errorf("schema %s declared port.FileTool with no path to name", schema)
		}
	}
}

// A `path` the server declares as something other than a string is not the thing this looks for.
// Reading it as one would put a number in the list of what the turn changed.
func TestAPathThatIsNotAStringIsNotAPath(t *testing.T) {
	for _, schema := range []string{
		`{"properties":{"path":{"type":"integer"}}}`,
		`{"properties":{"path":{"type":"array"}}}`,
	} {
		if _, ok := declared(tool(schema, false)).(port.FileTool); ok {
			t.Errorf("schema %s declared a path that is not a string", schema)
		}
	}
	// Absent and union types are paths: a schema that says nothing, and a nullable one.
	for _, schema := range []string{
		`{"properties":{"path":{}}}`,
		`{"properties":{"path":{"type":["string","null"]}}}`,
	} {
		if _, ok := declared(tool(schema, false)).(port.FileTool); !ok {
			t.Errorf("schema %s should be a path", schema)
		}
	}
}

// Wrapping must not hide what the tool already answered. Both of these are read by the core: the
// conversation a hand belongs to, and whether its result can be had again by asking again.
func TestWrappingKeepsWhatTheToolAlreadyDeclared(t *testing.T) {
	base := tool(`{"properties":{"path":{"type":"string"}}}`, true)
	base.byOwner = map[string]*Client{"": nil}
	got := declared(base)
	if _, ok := got.(port.Owned); !ok {
		t.Error("the wrapper hid port.Owned — a per-conversation hand would show to everyone")
	}
	ro, ok := got.(port.ReadOnlyTool)
	if !ok || !ro.ReadOnly() {
		t.Error("the wrapper hid port.ReadOnlyTool — compaction sheds re-readable results with it")
	}
	if got.Name() != base.Name() || got.Description() != base.Description() {
		t.Error("the wrapper changed the tool's identity")
	}
}

// A call that names no path answers "": there is nothing to record, which is not an error.
func TestFileArgIsEmptyWhenTheCallNamesNoPath(t *testing.T) {
	ft := declared(tool(`{"properties":{"path":{"type":"string"}}}`, false)).(port.FileTool)
	for _, args := range []string{`{}`, `{"path":""}`, `{"path":"   "}`, `{"path":5}`, `bad`} {
		if p := ft.FileArg(json.RawMessage(args)); p != "" {
			t.Errorf("FileArg(%s) = %q, want empty", args, p)
		}
	}
}
