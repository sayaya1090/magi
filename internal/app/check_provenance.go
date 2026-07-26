package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The typed check closed the door on a check that fabricates its own evidence (check_assert.go): the
// runner owns the invocation, so nothing the model writes can redirect, wrap, or exit-code its way to
// a pass. One door stays open, and it is the honest cost of moving the running into the STEP — the
// step can record a result it never obtained:
//
//	echo 'All tests passed' > /tmp/test.log      the marker the check greps for, typed by hand
//	write {path: /tmp/test.log, content: ...}    the same thing through the write tool
//
// The check cannot tell those from a real recording, because by the time it reads the file the two
// are byte-identical. But magi is the one that executed the tool calls, and it kept the record: the
// worker's own tool-call log says whether that path last received a program's output or a string the
// model composed. That record is not something the model can edit — it is written by the runtime as
// a side effect of granting the call.
//
// So the audit reads it, and reports the one shape that is not arguable: THE ASSERTED PATTERN APPEARS
// VERBATIM IN WHAT THE WORKER TYPED. A worker that wrote `All tests passed` into the file whose check
// greps for `All tests passed` has proved nothing about the deliverable, whatever else it may have
// done.
//
// It reports rather than fails, deliberately. A file the worker authors is not always a fabrication —
// when the deliverable IS the file (a config, a generated source), authoring it is the work, and a
// check asserting its content is merely tautological rather than dishonest. Telling those two apart
// needs judgement this pass does not have, and a wrong hard-fail would break work that is correct.
// So the finding is attached to the check's own recorded output and announced on the progress
// stream, where the council and the post-hoc log both see it. Escalation to a verdict is a separate
// decision, to be made on the false-positive rate this produces, not ahead of it.

// provenanceEnabled gates the audit (default ON; MAGI_CHECK_PROVENANCE=0 disables).
func provenanceEnabled() bool { return !envOff("MAGI_CHECK_PROVENANCE") }

// provenanceScanCap bounds how many of a session's most recent tool calls are examined per audit.
// The writer of a check's source is a call from the step that just ran, so the recent window holds
// it; scanning an entire long run per check per gate cycle would not.
const provenanceScanCap = 400

// authoredContent is a tool call that put MODEL-COMPOSED text into a path: the content came out of
// the reply, not out of a program. tool names the call, and text is what was composed — the haystack
// the asserted pattern is looked for in.
type authoredContent struct {
	tool string
	text string
}

// auditSourceProvenance reports whether the pattern a typed check asserts was itself typed by the
// worker into the file the check reads. Returns "" when there is nothing to say — the audit is off,
// the assertion has no pattern, no tool call authored that path, or the pattern is absent from what
// was authored (the normal case: the file holds a program's real output).
//
// Only `matches` is auditable this way. `nonempty` has no pattern to look for, `absent` passes by the
// pattern NOT being there (typing it in could only make the check fail, which is not a cheat), and
// the liveness probes read the world rather than a file.
func (a *App) auditSourceProvenance(ctx context.Context, sid session.SessionID, src string, as assertion) string {
	if !provenanceEnabled() || as.verb != "matches" || strings.TrimSpace(src) == "" {
		return ""
	}
	pat := literalOf(as.arg)
	if pat == "" {
		return "" // a pattern with no literal core (`.*`, `^$`) cannot be looked for in typed text
	}
	if a.provAuditDone(sid, src, pat) {
		return "" // asked already this run; the scan reads every event of every session
	}
	return auditFinding(a.pathAuthors(ctx, sid, src), src, as)
}

// auditFinding is the decision itself, over the authors already gathered: the finding exists exactly
// when one of them composed the asserted pattern verbatim. Separated from the gathering because this
// is the part that must be right — the gathering can over-collect harmlessly, but a finding names a
// specific call as having faked a result.
func auditFinding(authors []authoredContent, src string, as assertion) string {
	if as.verb != "matches" {
		return ""
	}
	pat := literalOf(as.arg)
	if pat == "" {
		return ""
	}
	for _, w := range authors {
		if !strings.Contains(w.text, pat) {
			continue
		}
		return fmt.Sprintf("PROVENANCE: %s was written by the worker's own `%s` call, and the text it wrote "+
			"contains %q — the very string this check looks for. Nothing here shows the work was done: a recorded "+
			"result must be the REAL output of the command it summarizes, redirected into this path.",
			src, w.tool, clipLine(pat, 80))
	}
	return ""
}

// provAuditDone marks a (source, pattern) pair audited and reports whether it already was. The audit
// re-reads every event of the session and of every worker under it, and a step's checks run at each
// gate cycle — without this the cost is paid over and over for an answer that does not move.
func (a *App) provAuditDone(sid session.SessionID, src, pat string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	if !ok {
		return false
	}
	if st.provAudited == nil {
		st.provAudited = map[string]bool{}
	}
	k := src + "\x00" + pat
	if st.provAudited[k] {
		return true
	}
	st.provAudited[k] = true
	return false
}

// pathAuthors returns every recent tool call, in the gating session AND in the workers it spawned,
// that put model-composed text into path. Worker sessions are included because a delegated step's
// tool calls happen there, and that is precisely where the recording was supposed to be made.
// Best-effort throughout: an unreadable session yields no authors rather than an error, because a
// missing record must never be reported as a fabrication.
func (a *App) pathAuthors(ctx context.Context, sid session.SessionID, p string) []authoredContent {
	out := authorsIn(a.readEventsBestEffort(ctx, sid), p)
	a.mu.Lock()
	var kids []session.SessionID
	for _, st := range a.states {
		if st.meta.Parent == sid {
			kids = append(kids, st.meta.ID)
		}
	}
	a.mu.Unlock()
	for _, k := range kids {
		out = append(out, authorsIn(a.readEventsBestEffort(ctx, k), p)...)
	}
	return out
}

// readEventsBestEffort reads a session's events, returning nil on any failure.
func (a *App) readEventsBestEffort(ctx context.Context, sid session.SessionID) []event.Event {
	if a.store == nil {
		return nil
	}
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return nil
	}
	return evs
}

// authorsIn scans one session's tool CALLS (the arguments, not the results — the arguments are what
// the model composed) for those that authored content into p.
func authorsIn(evs []event.Event, p string) []authoredContent {
	if len(evs) > provenanceScanCap {
		evs = evs[len(evs)-provenanceScanCap:]
	}
	var out []authoredContent
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil || d.Part.Kind != session.PartToolCall || d.Part.ToolCall == nil {
			continue
		}
		if w, ok := authoredBy(d.Part.ToolCall.Name, d.Part.ToolCall.Args, p); ok {
			out = append(out, w)
		}
	}
	return out
}

// authoredBy decides whether one tool call authored content into p, and returns the text it composed.
//
// The file-writing tools are unambiguous: their whole purpose is to put the reply's text on disk, so
// a call naming p authored it. bash is the judgement call, and it turns on WHO PRODUCED the bytes —
// `make test > log` records a program's real output and is exactly the shape a check is supposed to
// read, while `echo ok > log` records the model's own claim. So a bash call counts only when p is the
// target of a redirect whose producer is a text-emitting builtin or a heredoc.
func authoredBy(name string, args json.RawMessage, p string) (authoredContent, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "write":
		var a struct {
			Path, Content string
		}
		if json.Unmarshal(args, &a) == nil && samePath(a.Path, p) {
			return authoredContent{tool: "write", text: a.Content}, true
		}
	case "edit":
		var a struct{ Path, New string }
		if json.Unmarshal(args, &a) == nil && samePath(a.Path, p) {
			return authoredContent{tool: "edit", text: a.New}, true
		}
	case "multiedit":
		var a struct {
			Path  string
			Edits []struct{ New string }
		}
		if json.Unmarshal(args, &a) == nil && samePath(a.Path, p) {
			var b strings.Builder
			for _, h := range a.Edits {
				b.WriteString(h.New)
				b.WriteByte('\n')
			}
			return authoredContent{tool: "multiedit", text: b.String()}, true
		}
	case "bash":
		var a struct{ Command string }
		if json.Unmarshal(args, &a) == nil && composedRedirectTo(a.Command, p) {
			// The haystack is the WHOLE command, not the redirect's own words: a heredoc body sits on
			// the lines after the `cat >` that opens it, and splitting them out would lose exactly the
			// text that matters.
			return authoredContent{tool: "bash", text: a.Command}, true
		}
	}
	return authoredContent{}, false
}

// composedProducers emit text the caller supplied rather than the result of doing anything. `cat` is
// here for its heredoc form only, which composedRedirectTo checks for separately — `cat real.log >
// copy.log` moves output that some other command produced and is not an authorship.
var composedProducers = map[string]bool{"echo": true, "printf": true, "cat": true, "true": true, ":": true}

// composedRedirectTo reports whether cmd redirects composed text into p. It walks the same
// quote-aware segmentation the read-only guard uses, so a `>` inside a quoted argument is data.
func composedRedirectTo(cmd, p string) bool {
	for _, seg := range shellCommandSegments(cmd) {
		fields := strings.Fields(seg)
		producer, heredoc, target := "", false, ""
		for i := 0; i < len(fields); i++ {
			f := fields[i]
			switch {
			case strings.HasPrefix(f, "<<"):
				heredoc = true
			case f == ">" || f == ">>":
				if i+1 < len(fields) {
					target = shellWord(fields[i+1])
					i++
				}
			case strings.HasPrefix(f, ">>"):
				target = shellWord(strings.TrimPrefix(f, ">>"))
			case strings.HasPrefix(f, ">"):
				target = shellWord(strings.TrimPrefix(f, ">"))
			case producer == "" && !isEnvAssignment(f):
				producer = path.Base(shellWord(f))
			}
		}
		if target == "" || !samePath(target, p) {
			continue
		}
		// `cat` alone is a copy; `cat <<EOF` is the model dictating the file's contents.
		if producer == "cat" && !heredoc {
			continue
		}
		if composedProducers[producer] || (heredoc && producer != "") {
			return true
		}
	}
	return false
}

// samePath reports whether two path spellings name the same file for this purpose. Exact after
// cleaning, or one is a path-boundary suffix of the other — a check says `/app/out.log` where the
// tool call said `out.log` from that same directory, and treating those as different paths would
// make the audit silently find nothing. The suffix rule can over-match two same-named files in
// different trees; that is the right direction for a reporting pass, and the pattern test that
// follows is what actually decides.
func samePath(a, b string) bool {
	x, y := path.Clean(strings.TrimSpace(strings.ReplaceAll(a, "\\", "/"))), path.Clean(strings.TrimSpace(strings.ReplaceAll(b, "\\", "/")))
	if x == "." || y == "." || x == "" || y == "" {
		return false
	}
	if x == y {
		return true
	}
	return strings.HasSuffix(x, "/"+y) || strings.HasSuffix(y, "/"+x)
}

// literalOf extracts the longest run of ordinary characters from a regular expression — the part that
// must appear verbatim in the subject for the pattern to match at all. `All tests passed` out of
// `^All tests passed$`, `error` out of `(error|fail)`. Returns "" when the pattern has no such run
// (`.*`, `\d+`), which means it cannot be looked for in composed text and the audit stays silent.
func literalOf(pat string) string {
	best, cur := "", strings.Builder{}
	flush := func() {
		if cur.Len() > len(best) {
			best = cur.String()
		}
		cur.Reset()
	}
	for i := 0; i < len(pat); i++ {
		c := pat[i]
		if c == '\\' { // an escaped metacharacter is a literal, but skip the pair rather than guess
			i++
			flush()
			continue
		}
		if strings.IndexByte(`^$.*+?()[]{}|`, c) >= 0 {
			flush()
			continue
		}
		cur.WriteByte(c)
	}
	flush()
	// A one- or two-character run is noise (a stray `a` between metacharacters) and would match text
	// everywhere; require enough to be about this contract.
	if best = strings.TrimSpace(best); len(best) < 4 {
		return ""
	}
	return best
}
