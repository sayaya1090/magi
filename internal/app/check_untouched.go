package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// `absent` asks that a pattern NOT be in a file, and a file nobody touched satisfies that by having
// been left alone. So the pass is about the file's history, not about the step: it would have read
// the same before the run started, and it reads the same for a step that did nothing at all.
//
// Observed live — `absent /next\s*=\s*NULL/` on `ocaml/runtime/major_gc.c`, passing while the work
// went into `runtime/shared_heap.c` and that file was never opened. It became visible only once the
// path repair made the check runnable at all; before that it was a permanent false failure, which
// is a different problem with the same root: the assertion carries no evidence about the work.
//
// magi can tell the two apart from its own record of what ran. A mutating call naming the path —
// write/edit/multiedit, or a bash command that redirects into it — is the step touching the file. No
// such call anywhere under the gate means the pass proves nothing either way, which is the
// documented meaning of 126: no verdict, the step lands ungated rather than failed.
//
// Deliberately only `absent`. A `matches` pass says the pattern IS there, which is positive evidence
// about content whoever wrote it; `absent` is satisfied by an empty file, an unrelated file, and a
// file that was never in the run's way.

// untouchedGateEnabled gates the rule (default ON; MAGI_CHECK_UNTOUCHED=0 disables).
func untouchedGateEnabled() bool { return !envOff("MAGI_CHECK_UNTOUCHED") }

// pathTouched reports whether any tool call, in the gating session or in any session beneath it,
// mutated this path. It walks the same tree the provenance audit does and asks a broader question:
// not "who composed these bytes" but "did anything change this file at all".
func (a *App) pathTouched(ctx context.Context, sid session.SessionID, p string) bool {
	if mutatorIn(a.readEventsBestEffort(ctx, sid), p) {
		return true
	}
	for _, k := range a.descendantsOf(sid) {
		if mutatorIn(a.readEventsBestEffort(ctx, k), p) {
			return true
		}
	}
	return false
}

// mutatorIn scans one session's tool CALLS for one that changed p. The file-writing tools name their
// target directly; a bash command's targets are read out of the command text by the same helper the
// self-revert check uses, so the two agree on what counts as a bash write.
func mutatorIn(evs []event.Event, p string) bool {
	if len(evs) > provenanceScanCap {
		evs = evs[len(evs)-provenanceScanCap:]
	}
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil || d.Part.Kind != session.PartToolCall || d.Part.ToolCall == nil {
			continue
		}
		if mutatedBy(d.Part.ToolCall.Name, d.Part.ToolCall.Args, p) {
			return true
		}
	}
	return false
}

// mutatedBy decides whether one tool call changed p.
func mutatedBy(name string, args json.RawMessage, p string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "write", "edit", "multiedit":
		var a struct{ Path string }
		return json.Unmarshal(args, &a) == nil && samePath(a.Path, p)
	case "bash":
		var a struct{ Command string }
		if json.Unmarshal(args, &a) != nil {
			return false
		}
		for _, w := range bashWritePaths(a.Command) {
			if samePath(w, p) {
				return true
			}
		}
	}
	return false
}
