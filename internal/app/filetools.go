package app

import (
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/port"
)

// This file is the one place that answers "does this call touch a file, and which one?".
//
// It used to be answered by name, in a dozen places, with two different vocabularies: a map of
// three names for the write side and a switch over "read", "grep", "glob", "list" for the read
// side. That was
// exact while every file tool was a builtin. An editor plugin that attaches its own edit tool —
// which is the case this exists for — is called mcp__jetbrains__edit, and by name it is nothing.
//
// A tool says so itself by implementing port.FileTool. The builtin names stay recognised, so this
// adds a way in rather than replacing one.

// fileTouch is what a call does to a file: the path it names, and whether it changes it.
type fileTouch struct {
	path string
	// guard is the loop guard's SLOT for this call — the path when there is one, and the tool's
	// own name when there is not. Only the two guard readers that key a per-file ledger use it
	// (runGuard.mutated and runGuard.repeatEpoch); everything else wants `path` and reads `path`.
	//
	// It is a second field rather than a fallback written into `path` because six other readers
	// take that field AS A PATH: the secret/guardrail deny rules, the pre-call snapshot, the
	// post-edit diagnostics, the created-set, the read-coverage drop, and the change record the
	// council is shown. Each is already correct against "" — they skip — and each would be wrong
	// against a tool NAME: a 12-second LSP round trip on a file that does not exist, a non-file
	// entry in the list of what this turn changed.
	//
	// Nor can the guard simply skip a path-less write. check() counts every call toward the
	// no-progress window and only mutated() clears it, so skipping would mean a turn that
	// successfully edits through a path-less tool never registers progress at all — and the
	// stalled nudge, which re-arms without a cap and has no force-stop behind it, would tell an
	// agent doing real work that it is stalled, every twelve calls, forever.
	guard  string
	writes bool
}

// guardKey is the guard slot for a call that names `path` through the tool called `name`.
//
// "" is a perfectly good map key, so without this every path-less writer shares ONE slot in
// runGuard.lastMut: two such tools alternating would each read the other's signature as the
// world having moved, so neither one's identical rewrite could ever be recognised as the
// idempotent no-op it is, and every swing would be credited as progress. The tool name is
// prefixed with a NUL so it can never collide with a real relative path — the same convention
// the bash mutation slot ("\x00bash") already uses.
func guardKey(path, name string) string {
	if path != "" {
		return path
	}
	return "\x00" + name
}

// builtinFileTools is the default vocabulary — what a tool that declares nothing is taken to be.
// Kept as a map rather than folded into the builtins themselves so that a registry with none of
// them (a test, a companion built from parts) behaves the way it always has.
var builtinFileTools = map[string]bool{ // name → writes
	"write": true, "edit": true, "multiedit": true,
	"read": false, "grep": false, "glob": false, "list": false,
}

// confinedEdit reports one of magi's OWN writing file tools — the ones resolvePath keeps inside the
// workspace. Deliberately not "anything that writes files": the difference between a tool magi
// confines and a tool that merely says it writes is the whole of what a permission mode is
// promising when it says edits may run unasked.
func confinedEdit(name string) bool {
	writes, ok := builtinFileTools[strings.ToLower(strings.TrimSpace(name))]
	return ok && writes
}

// touchesFile answers what a call does to a file. ok is false when this tool touches none, which is
// the common case and not a failure.
func (a *App) touchesFile(name string, args json.RawMessage) (fileTouch, bool) {
	return touchesFileIn(a.tools, name, args)
}

// changesFile is touchesFile for the callers that only ask "was that an edit?".
func (a *App) changesFile(name string) bool {
	t, ok := touchesFileIn(a.tools, name, nil)
	return ok && t.writes
}

// filePathOf is the path a call names, or "" — the generalisation of reading an argument called
// "path", which is still what a tool that declares nothing is asked for.
func (a *App) filePathOf(name string, args json.RawMessage) string {
	t, ok := a.touchesFile(name, args)
	if !ok {
		return ""
	}
	return t.path
}

func touchesFileIn(reg port.ToolRegistry, name string, args json.RawMessage) (fileTouch, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	if reg != nil {
		if t, ok := reg.Get(name); ok {
			if ft, declares := t.(port.FileTool); declares {
				p := strings.TrimSpace(ft.FileArg(args))
				return fileTouch{path: p, guard: guardKey(p, lower), writes: ft.WritesFile()}, true
			}
		}
	}
	writes, known := builtinFileTools[lower]
	if !known {
		return fileTouch{}, false
	}
	p := pathArg(args)
	return fileTouch{path: p, guard: guardKey(p, lower), writes: writes}, true
}
