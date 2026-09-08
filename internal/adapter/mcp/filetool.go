package mcp

import (
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/port"
)

// A hand that edits a file, said in a way the core can read.
//
// # What was broken
//
// An editor plugin attaches its own edit tool — that is what `mcp-attach` is for, and the JetBrains
// client does exactly this with `apply_edit`, going through the IDE so undo, local history and
// inspections all see the change. It worked. And the core did not know it had happened: the machinery
// that answers "did this call touch a file, and which one?" recognised the builtins by NAME, and by
// name `mcp__jetbrains__apply_edit` is nothing.
//
// So a turn that did its work through an editor plugin read as a turn that changed nothing. The
// record said `changed: nothing`, the council judged on that record, the stall guard counted no
// progress, and a person's own `Edit(...)` allow rule never matched. Nothing errored anywhere —
// which is why it could sit there.
//
// `internal/app/filetools.go` opened the way in for exactly this case and named it in its own
// comment. It had no producer: `port.FileTool` was implemented by nothing outside tests.
//
// # Why the schema decides, and not the annotation alone
//
// `WritesFile` takes no arguments, so a tool cannot answer "this call names a file" per call — it
// answers once, for the tool. Declaring every non-read-only server tool a file writer would be
// wrong and expensive: a deck tool like `add_slide` names no path, and the core would then run
// post-edit diagnostics and PostToolUse hooks against the empty path on every one of them — the
// twelve-second round trip on a file that does not exist that filetools.go warns about.
//
// So the tool's OWN inputSchema decides. A tool that declares a string property called `path` is a
// tool that names a file; the rest are left exactly as they were, declaring nothing.
//
// # And why read-only still matters
//
// Given a path, whether the call CHANGES it comes from the server's `annotations.readOnlyHint`.
// `show` opens a file at a line and changes nothing; `apply_edit` rewrites it. A server that does
// not send the hint is taken at the protocol's default — not read-only — so its path-naming tools
// are treated as writers. That errs toward recording a change that did not happen rather than
// missing one that did, which is the right way round for a record the council reads: an extra path
// in "changed" is visible and checkable, and a missing one is invisible.

// fileArgName is the argument a call names a file with. One name, the same one the builtins read
// (`internal/app/diagnose.go`, pathArg) — a server that calls it something else is not recognised,
// and that is better than guessing at `file`, `filename`, `uri` and being wrong about which of them
// is a path on disk.
const fileArgName = "path"

// namesAFile reports whether this tool's own schema declares the file argument.
//
// The property must be a string, or absent a type. A `path` that the server declares as an integer
// or an array is not the thing this is looking for, and reading it as one would put a number in the
// list of what the turn changed.
func namesAFile(schema json.RawMessage) bool {
	var s struct {
		Properties map[string]struct {
			Type any `json:"type"`
		} `json:"properties"`
	}
	if json.Unmarshal(schema, &s) != nil {
		return false
	}
	p, ok := s.Properties[fileArgName]
	if !ok {
		return false
	}
	switch t := p.Type.(type) {
	case nil:
		return true
	case string:
		return t == "string"
	case []any:
		// A union. "string" being among them is enough — a nullable path is still a path.
		for _, x := range t {
			if s, _ := x.(string); s == "string" {
				return true
			}
		}
	}
	return false
}

// fileHand is an mcpTool that names a file. It exists only to carry the two declarations, and
// embeds the tool so every other interface it satisfies (port.Owned, port.ReadOnlyTool) is kept.
type fileHand struct{ *mcpTool }

// FileArg answers port.FileTool: the file this call names, from the call's own arguments.
func (t *fileHand) FileArg(args json.RawMessage) string {
	var a map[string]json.RawMessage
	if json.Unmarshal(args, &a) != nil {
		return ""
	}
	raw, ok := a[fileArgName]
	if !ok {
		return ""
	}
	var p string
	if json.Unmarshal(raw, &p) != nil {
		return ""
	}
	return strings.TrimSpace(p)
}

// WritesFile answers port.FileTool: whether the call changes the file or only reads it. The
// server's own word, from `annotations.readOnlyHint`.
func (t *fileHand) WritesFile() bool { return !t.ReadOnly() }

// declared returns what to put in the registry: the tool wrapped when it names a file, the tool
// itself when it does not.
//
// Wrapped at the registry boundary rather than made a field on mcpTool, because a type assertion
// cannot read a field: `t.(port.FileTool)` succeeds for every mcpTool the moment the methods exist
// on it, and then a path-less tool declares a path-shaped answer it does not have.
func declared(t *mcpTool) port.Tool {
	if namesAFile(t.Schema()) {
		return &fileHand{mcpTool: t}
	}
	return t
}
