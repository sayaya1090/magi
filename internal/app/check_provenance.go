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
// So the audit reads it. Where the assertion carries a pattern it reports the shape that is not
// arguable — THE ASSERTED PATTERN APPEARS VERBATIM IN WHAT THE WORKER TYPED. Where it carries none
// (`nonempty`, `absent`, `equals`) the authorship alone is the report, because a check that passes
// on a file whose bytes came out of the reply is evidence about the reply.
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

// auditSourceProvenance reports when a check's source file was composed by the worker rather than
// produced by a program. Returns "" when there is nothing to say — the audit is off, the assertion
// does not read a file, or no tool call authored that path (the normal case).
//
// The question has two sharpnesses. For `matches` the pattern itself can be looked for in what was
// typed, and finding it names the cheat exactly: the worker wrote the very string the check greps
// for. For the other file-reading verbs there is no pattern, and the authorship alone is the
// finding — which is enough, because a check that passes on a file whose bytes came out of the
// reply is evidence about the reply and not about the work.
//
// That second half was missing, and it is the half that mattered: `nonempty` is satisfied by ANY
// text, so it is the cheapest assertion to fake and the one an audit keyed on patterns can never
// see. Observed live, one second apart — `echo "Bootstrap completed successfully - no crash in
// build" > /app/crash.log`, then `crash.log nonempty` flipping to pass, on a step whose deliverable
// was "bootstrap crash reproduced" and whose real build was segfaulting.
//
// The liveness probes (port_open, process_alive) read the world rather than a file, so there is no
// authorship to ask about.
func (a *App) auditSourceProvenance(ctx context.Context, sid session.SessionID, src string, as assertion) string {
	if !provenanceEnabled() || strings.TrimSpace(src) == "" {
		return ""
	}
	switch as.verb {
	case "matches", "nonempty", "absent", "equals":
	default:
		return ""
	}
	pat := literalOf(as.arg) // "" for the verbs with no pattern — the authorship question stands
	if as.verb == "matches" && pat == "" {
		return "" // a pattern with no literal core (`.*`, `^$`) cannot be looked for in typed text
	}
	if f, asked := a.provAudit(sid, src, pat); asked {
		return f // asked already this run; the scan reads every event of every session
	}
	f := auditFinding(a.pathAuthors(ctx, sid, src), src, as)
	a.rememberProvAudit(sid, src, pat, f)
	return f
}

// auditFinding is the decision itself, over the authors already gathered. Separated from the
// gathering because this is the part that must be right — the gathering can over-collect harmlessly,
// but a finding names a specific call as having faked a result.
func auditFinding(authors []authoredContent, src string, as assertion) string {
	if len(authors) == 0 {
		return ""
	}
	switch as.verb {
	case "matches", "nonempty", "absent", "equals":
	default:
		return "" // reads the world, not a file — there is no authorship to report
	}
	if as.verb == "matches" {
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
	w := authors[0]
	return fmt.Sprintf("PROVENANCE: %s was written by the worker's own `%s` call — its contents came out of the "+
		"reply, not out of a program. `%s` passing on a file the worker composed is evidence about what was typed, "+
		"not about the work: a recorded result must be the REAL output of the command it summarizes, redirected "+
		"into this path.", src, w.tool, as.verb)
}

// provAudit returns what a previous ask about this (source, pattern) found, and whether it was ever
// asked. rememberProvAudit records the answer.
//
// The memo exists because the scan re-reads every event of the gating session and of every session
// beneath it, and a step's checks run at each gate cycle — the cost is paid over and over for an
// answer that does not move. What it must NOT do is hide that answer: the FINDING is cached, not
// merely the fact of having asked. A check is evaluated more than once by design (the delegate step
// gate runs it, then the incremental recorder runs it again), and each of those records its own
// event, so a memo that returned "" the second time would leave whichever record someone actually
// reads with no finding on it.
func (a *App) provAudit(sid session.SessionID, src, pat string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	if !ok {
		return "", false
	}
	f, asked := st.provAudited[src+"\x00"+pat]
	return f, asked
}

func (a *App) rememberProvAudit(sid session.SessionID, src, pat, finding string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	if st.provAudited == nil {
		st.provAudited = map[string]string{}
	}
	st.provAudited[src+"\x00"+pat] = finding
}

// pathAuthors returns every recent tool call, in the gating session and in every session BENEATH it
// at any depth, that put model-composed text into path. The descendants are included because a
// delegated step's tool calls happen there, and that is precisely where the recording was supposed
// to be made.
//
// At ANY depth, because a worker spawns workers: a step that decomposes, or a delegate that
// delegates, puts the write two levels down while the gate runs at the top. Observed live — a
// worker's own child wrote /app/bug_analysis.md, the file a step-4 check reads, while the check ran
// in the main session. A one-level walk sees the parent and not the writer, so a record composed
// only in a grandchild would have looked like a program's real output.
//
// Best-effort throughout: an unreadable session yields no authors rather than an error, because a
// missing record must never be reported as a fabrication.
func (a *App) pathAuthors(ctx context.Context, sid session.SessionID, p string) []authoredContent {
	out := authorsIn(a.readEventsBestEffort(ctx, sid), p)
	for _, k := range a.descendantsOf(sid) {
		out = append(out, authorsIn(a.readEventsBestEffort(ctx, k), p)...)
	}
	return out
}

// descendantsOf returns every session under sid, at any depth, in breadth-first order. The
// parent->children index is built once under the lock and walked outside it, so a deep tree does not
// hold the mutex for the length of the walk. The visited set is what keeps a corrupt parent chain (a
// cycle) from spinning here — nothing enforces acyclicity in the recorded metadata, and a hang
// inside a check audit would be a very expensive way to learn that.
func (a *App) descendantsOf(sid session.SessionID) []session.SessionID {
	a.mu.Lock()
	kids := make(map[session.SessionID][]session.SessionID, len(a.states))
	for _, st := range a.states {
		if p := st.meta.Parent; p != "" {
			kids[p] = append(kids[p], st.meta.ID)
		}
	}
	a.mu.Unlock()

	seen := map[session.SessionID]bool{sid: true}
	queue := append([]session.SessionID{}, kids[sid]...)
	var out []session.SessionID
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
		queue = append(queue, kids[s]...)
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
// quote-aware segmentation in check_shell.go, so a `>` inside a quoted argument is data.
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
