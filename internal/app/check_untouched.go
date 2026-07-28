package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

// writtenPaths lists the files a session and everything under it actually wrote, newest last, in the
// spelling the tool call used. It reads the same record pathTouched does and answers the other half
// of the question: not "was this one file changed" but "what did this worker produce".
func (a *App) writtenPaths(ctx context.Context, sid session.SessionID, cap int) []string {
	seen := map[string]bool{}
	var out []string
	add := func(evs []event.Event) {
		for _, p := range writesIn(evs) {
			if seen[p] || len(out) >= cap {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	add(a.readEventsBestEffort(ctx, sid))
	for _, k := range a.descendantsOf(sid) {
		add(a.readEventsBestEffort(ctx, k))
	}
	return out
}

// writesIn pulls every path one session's tool calls wrote to, in call order.
func writesIn(evs []event.Event) []string {
	if len(evs) > provenanceScanCap {
		evs = evs[len(evs)-provenanceScanCap:]
	}
	var out []string
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil || d.Part.Kind != session.PartToolCall || d.Part.ToolCall == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(d.Part.ToolCall.Name)) {
		case "write", "edit", "multiedit":
			var a struct{ Path string }
			if json.Unmarshal(d.Part.ToolCall.Args, &a) == nil && strings.TrimSpace(a.Path) != "" {
				out = append(out, strings.TrimSpace(a.Path))
			}
		case "bash":
			var a struct{ Command string }
			if json.Unmarshal(d.Part.ToolCall.Args, &a) == nil {
				out = append(out, bashWritePaths(a.Command)...)
			}
		}
	}
	return out
}

// `nonempty` and `equals` needed a different question than `absent` did, and the first attempt at
// them got it wrong in a way worth recording.
//
// `absent` is satisfied by leaving a file alone, so "no tool call touched it" answers it exactly.
// `nonempty` is not: a build produces its artifact as a SIDE EFFECT, with no redirect and no write
// call naming the path, so pathTouched reports untouched for a file the run genuinely just made.
// Gating on that alone refused to credit real deliverables — caught by the existing tests, not by
// reasoning.
//
// The question these two verbs actually ask is whether the assertion was ALREADY TRUE before the
// step ran, and the filesystem answers it: a file whose mtime predates the turn is one the turn did
// not produce. Both signals are used, and only their conjunction is meaningless — no tool named it
// AND it is older than the run. Either one alone leaves the check standing.

// predatesRun reports whether p existed before this turn started. Unknown (no recorded start, an
// unreadable path, a clock that cannot answer) reads as "not older", so a check is only ever
// DOWNGRADED on positive evidence that it predates the work.
func (a *App) predatesRun(sid session.SessionID, workdir, p string) bool {
	a.mu.Lock()
	start := a.stateLocked(sid).turnStart
	a.mu.Unlock()
	if start.IsZero() {
		return false
	}
	fp := filepath.FromSlash(p)
	if !filepath.IsAbs(fp) {
		fp = filepath.Join(workdir, fp)
	}
	fi, err := os.Stat(fp)
	if err != nil {
		return false
	}
	return fi.ModTime().Before(start)
}

// staleEvidence is the conjunction the two verbs gate on: nothing in this run named the path, and
// the file is older than the run. Then the assertion was true before the step and says nothing
// about it.
func (a *App) staleEvidence(ctx context.Context, sid session.SessionID, workdir, p string) bool {
	return untouchedGateEnabled() && !a.pathTouched(ctx, sid, p) && a.predatesRun(sid, workdir, p)
}
