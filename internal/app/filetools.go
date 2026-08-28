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
	path   string
	writes bool
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
				return fileTouch{path: strings.TrimSpace(ft.FileArg(args)), writes: ft.WritesFile()}, true
			}
		}
	}
	writes, known := builtinFileTools[lower]
	if !known {
		return fileTouch{}, false
	}
	return fileTouch{path: pathArg(args), writes: writes}, true
}
