package app

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// What magi's own record says happened — as opposed to what the model said, or what a check
// authored in advance was told to look for.
//
// Everything here was already being collected for one purpose or another: the exit code a bash
// result carries, the detector that knows when that exit belongs to a `| tail` rather than to the
// command, the tokenizer that separates running a program from printing a file, and the write paths
// read out of a command. What was missing is the one place that asks them together, so a supervisor
// can answer "did anything actually happen" without a contract to compare against.
//
// It is deliberately NOT a verdict. It reports; the reading is someone else's — a termination hook
// deciding whether to intervene, a council member judging against the record instead of the prose,
// or the agent itself. That separation is the point: a check authored before the work could be
// wrong about the work, and today's defects were all of that kind. An observation cannot be wrong
// about what it observed; it can only be incomplete, and it says so.

// observedCmd is one command magi ran and what it learned about how it ended.
type observedCmd struct {
	cmd     string
	exit    int
	unclear bool  // the reported exit belongs to a pipe/`;` tail, not to this command
	stages  []int // per-stage statuses when the pipeline reported them, left to right
}

// observedRun is the record over a session.
type observedRun struct {
	cmds    []observedCmd
	changed []string // paths a tool call in this run wrote to
	// looked counts, per path, how many times this run went and LOOKED at it: the read tool, or a
	// search rooted there. A shell command naming the path is NOT one of them — deciding that `od
	// f` looks at f while `cobc f` runs it takes a verb table, and the table is the guess. Those
	// repeats are already a fact under reran, by the command's own text. lookOrder keeps first-seen
	// order so the rendering is stable across steps rather than reshuffling with map iteration.
	looked    map[string]int
	lookOrder []string
	// reran counts bash COMMANDS by their own text — every command, with no judgement about
	// whether it looked or ran. The path counter above credits a shell command only against a path
	// the read tool already opened, so a run that never touches read gets nothing from it, and that
	// is the run most worth telling: observed live on sqlite-with-gcov, 52 bash calls, zero reads,
	// and one `ls … && nm … | grep __gcov | wc -l` issued five times byte-for-byte while the record
	// said nothing. Classifying first is what would make this wrong (`nm` is not in the inspect
	// verb list, so that very command reads as a program run); a command's own text is not a guess,
	// it is exactly what magi granted, and issuing the same one again is a fact either way.
	reran      map[string]int
	rerunOrder []string
	// wrote counts write/edit/multiedit calls per path. `changed` names each path once, which is
	// the right shape for "what exists now" and the wrong one for "how many times you rewrote it".
	// Observed live on large-scale-text-editing: one file authored 14 times, the same 152 bytes six
	// of those, and the record said only that the path had changed. The per-call self-edit note DID
	// say so — twenty times — but it lives in the tool result, which compaction takes away, and
	// this block is what survives.
	wrote map[string]int
}

// noteRerun records one bash command, keyed by its own whitespace-normalized text.
func (o *observedRun) noteRerun(cmd string) {
	key := strings.Join(strings.Fields(cmd), " ")
	if key == "" {
		return
	}
	if o.reran == nil {
		o.reran = map[string]int{}
	}
	if o.reran[key] == 0 {
		o.rerunOrder = append(o.rerunOrder, key)
	}
	o.reran[key]++
}

// noteLook records one look at a path.
func (o *observedRun) noteLook(p string) {
	p = strings.TrimSpace(p)
	if p == "" {
		return
	}
	if o.looked == nil {
		o.looked = map[string]int{}
	}
	if o.looked[p] == 0 {
		o.lookOrder = append(o.lookOrder, p)
	}
	o.looked[p]++
}

// observedScanCap bounds the walk: a session longer than this is read from its tail, where the
// current turn's work is. Scanning an entire long run on every step would cost more than the block
// is worth.
const observedScanCap = 400

// readEventsBestEffort reads a session's events, returning nil on any failure — a missing record
// must never be reported as an absence of work.
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

// observe reads what happened under sid. Best-effort: an unreadable session contributes nothing
// rather than a guess, because a missing record must never be reported as an absence of work.
func (a *App) observe(ctx context.Context, sid session.SessionID) observedRun {
	return observeEvents(a.readEventsBestEffort(ctx, sid), a.touchesFile)
}

// observedResult is what came back for one call, as the ledger needs it: the content, and the two
// flags that decide whether the call is evidence of anything having happened.
type observedResult struct {
	content  string
	isError  bool
	advisory bool
	present  bool
}

// neverRan reports whether a call was REFUSED before it executed — by the permission gate (the
// decided fact carries the denial), or by one of the other gates, whose refusals are magi's own
// fixed sentences at the head of the result.
//
// The check exists because the ledger's header says "the calls it granted, the paths they wrote" —
// and it was counting calls that were never granted. Measured live (the deny-mode wave test): a
// run whose every write/edit/bash was refused still read `changed: x.txt · authored more than
// once: x.txt ×2 · commands: echo "hello" > x.txt`, the model quoted that harness-authoritative
// line as proof it had done the work, and the council spent eighteen rounds rejecting a claim
// magi's own record kept feeding. An observation cannot be wrong about what it observed — so what
// was refused must not be observed as done.
func neverRan(res observedResult, denied bool) bool {
	if denied {
		return true
	}
	if !res.present || !res.isError {
		return false
	}
	s := strings.TrimSpace(decodeResultText(res.content))
	for _, p := range []string{
		"blocked by policy:", "blocked by hook:", "tool not permitted for agent",
		"unknown tool:", "not run — ", "denied by user", "magi crashed while running",
	} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	// The categorical headless denial names the tool first ("bash is unavailable in this headless
	// run: …"), so it is matched inside the head of the sentence rather than as a prefix.
	return strings.Contains(clipLine(s, 120), "is unavailable in this headless run")
}

// observeEvents is observe over events already in hand. The per-step state block has just read the
// session to build the request; reading it again there turns a cheap block into the loop's dominant
// cost, which is how a block meant to be free stops being worth having.
// touches answers what a call did to a file. Handed in for the same reason the guard's is: the
// evidence the council reads is "what changed", and a tool that edits under a name this switch
// never listed used to leave no trace in it. nil = the builtin names.
func observeEvents(all []event.Event, touches func(string, json.RawMessage) (fileTouch, bool)) observedRun {
	if touches == nil {
		touches = func(name string, args json.RawMessage) (fileTouch, bool) {
			return touchesFileIn(nil, name, args)
		}
	}
	var out observedRun
	seen := map[string]bool{}
	for _, evs := range [][]event.Event{all} {
		if len(evs) > observedScanCap {
			evs = evs[len(evs)-observedScanCap:]
		}
		results := map[string]observedResult{}
		denied := map[string]bool{}
		for _, e := range evs {
			if e.Type == event.TypePermissionDecided {
				var pd event.PermissionDecidedData
				if json.Unmarshal(e.Data, &pd) == nil && pd.Decision == "deny" {
					denied[pd.CallID] = true
				}
				continue
			}
			var d event.PartAppendedData
			if e.Type != event.TypePartAppended || json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			if d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil {
				r := d.Part.ToolResult
				results[r.CallID] = observedResult{
					content: string(r.Content), isError: r.IsError, advisory: r.Advisory, present: true,
				}
			}
		}
		for _, e := range evs {
			var d event.PartAppendedData
			if e.Type != event.TypePartAppended || json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			tc := d.Part.ToolCall
			if d.Part.Kind != session.PartToolCall || tc == nil {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(tc.Name))
			// What this call did to a file, asked of the tool itself. The switch below still names
			// the builtins — it also decides bash and the spawn/report kinds — but a tool that
			// edits under a name nobody listed used to leave no trace in the evidence the council
			// reads, so a turn that did its work through an editor plugin read as a turn that
			// changed nothing.
			if touch, isFile := touches(tc.Name, tc.Args); isFile && name != "bash" {
				path := strings.TrimSpace(touch.path)
				if !touch.writes {
					if neverRan(results[tc.CallID], denied[tc.CallID]) {
						continue // refused, so nothing was looked at
					}
					out.noteLook(path)
					continue
				}
				// Counted only when the write LANDED: an errored result that is not advisory means
				// nothing was written — a refusal, a jail denial, an edit whose anchor was not
				// found. (A landed write that then failed diagnostics is IsError AND Advisory, and
				// stays counted: the file on disk changed.) A call still in flight has no result
				// yet and keeps the old benefit of the doubt.
				if res := results[tc.CallID]; res.present && res.isError && !res.advisory {
					continue
				}
				if path != "" {
					if !seen[path] {
						seen[path] = true
						out.changed = append(out.changed, path)
					}
					if out.wrote == nil {
						out.wrote = map[string]int{}
					}
					out.wrote[path]++
				}
				continue
			}
			switch name {
			case "bash":
				res := results[tc.CallID]
				if neverRan(res, denied[tc.CallID]) {
					continue // refused before it ran: not a command, not a write, not a repeat
				}
				var args struct{ Command string }
				if json.Unmarshal(tc.Args, &args) != nil {
					continue
				}
				for _, p := range bashWritePaths(args.Command) {
					if !seen[p] {
						seen[p] = true
						out.changed = append(out.changed, p)
					}
				}
				out.noteRerun(args.Command)
				exit, known := exitOfBashResult(res.content)
				// A pipeline's exit is its last stage's, so `make … | tail` used to land in "status
				// unknown" — magi could see the shape and not the outcome. bash reports every stage
				// now, and when the note says the head of the pipe failed, that is not a shrug: the
				// record says FAILED.
				// The per-stage statuses, when the result carried them. They used to be read out of
				// the note's prose as one bit — "did the head fail" — and turned into a synthetic
				// exit 1. That bit was a reading, and it broke silently the moment the note stopped
				// making it. The numbers are the fact; the record carries them and adds nothing.
				stages := stagesOfBashResult(res.content)
				out.cmds = append(out.cmds, observedCmd{
					cmd:    args.Command,
					exit:   exit,
					stages: stages,
					// Unclear covers both shapes of "magi did not learn this command's status": a
					// tail that owns the reported exit, and a result carrying no exit at all (a
					// background start, a kill). Neither is a failure and neither is a success —
					// unless the stages told us, in which case it is no longer unknown.
					unclear: (!known || builtin.ExitCodeMasked(args.Command)) && len(stages) == 0,
				})
			}
		}
	}
	return out
}

// reread lists the paths this run opened more than once, most-repeated first, as "path ×N".
//
// Everything magi grants is in the record, and until now the part it handed back was writes and
// command outcomes — nothing about what the agent had already looked at. Observed live: one run
// read the same eighty lines of shared_heap.c thirteen times through four different mechanisms (the
// read tool at two offsets, `sed -n`, `cat | sed`, `cat -n | sed`), each call carrying a slightly
// different window so no two shared a fingerprint and the repeat guard never fired. Every one of
// those calls is inspect-only, so the record filed it as neither ran-clean nor failed — the agent's
// picture of what it had already seen was whatever it remembered seeing.
//
// A screen-driven agent gets this for free: its scrollback shows the file it opened four screens
// ago. This is the same fact, from the store magi already keeps. It is stated, not enforced —
// re-reading is sometimes exactly right (a file changed since, a window that genuinely needed
// widening), so nothing here blocks or nudges. Only repeats are listed, because a path read once is
// not information.
func (o observedRun) reread() []string {
	var out []string
	for _, p := range o.lookOrder {
		if n := o.looked[p]; n > 1 {
			out = append(out, fmt.Sprintf("%s ×%d", clipLine(p, 70), n))
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return countOf(out[i]) > countOf(out[j]) })
	return out
}

// repeatedCommands lists the bash commands issued more than once this run, busiest first, as
// "`cmd` ×N". No classification: re-running a build with nothing changed in between and re-printing
// the same file are different acts, and both are worth knowing. The reader decides which it is.
func (o observedRun) repeatedCommands() []string {
	var out []string
	for _, c := range o.rerunOrder {
		if n := o.reran[c]; n > 1 {
			out = append(out, fmt.Sprintf("`%s` ×%d", clipLine(c, 70), n))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return countOf(out[i]) > countOf(out[j]) })
	return out
}

// rewritten lists the paths authored more than once this run, busiest first, as "path ×N". How many
// times a file was rewritten is a fact about the run that "changed: <path>" cannot carry.
func (o observedRun) rewritten() []string {
	var out []string
	for _, p := range o.changed {
		if n := o.wrote[p]; n > 1 {
			out = append(out, fmt.Sprintf("%s ×%d", clipLine(p, 70), n))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return countOf(out[i]) > countOf(out[j]) })
	return out
}

// countOf reads back the ×N rendered above, for ordering. An unparseable tail sorts last.
func countOf(s string) int {
	i := strings.LastIndex(s, " ×")
	if i < 0 {
		return 0
	}
	n, err := strconv.Atoi(s[i+len(" ×"):])
	if err != nil {
		return 0
	}
	return n
}

// render describes the record in the words a reader needs, or "" when there is nothing to say.
// Ordered facts first: what changed, what ran, and last what magi could NOT determine — a reader
// that stops early still has the part that is settled.
func (o observedRun) render() string {
	if len(o.cmds) == 0 && len(o.changed) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("── WHAT MAGI OBSERVED (its own record of this run: the calls it granted, the paths " +
		"they wrote, and which of them it saw end badly — not a report anyone wrote) ──")
	if len(o.changed) > 0 {
		b.WriteString("\nchanged: " + strings.Join(clipEach(o.changed, 8), ", "))
	} else {
		b.WriteString("\nchanged: nothing")
	}
	if again := o.reread(); len(again) > 0 {
		b.WriteString("\nalready looked at more than once: " + strings.Join(clipEach(again, 6), " · "))
	}
	if again := o.repeatedCommands(); len(again) > 0 {
		b.WriteString("\nissued more than once, exactly as written: " + strings.Join(clipEach(again, 6), " · "))
	}
	if again := o.rewritten(); len(again) > 0 {
		b.WriteString("\nauthored more than once: " + strings.Join(clipEach(again, 6), " · "))
	}
	// Every command, with the exit magi actually learned. It used to sort them first — inspections
	// dropped entirely, the rest split into "ran clean" and "ran and FAILED" — and that sorting was
	// a reading, in the one place whose whole point is not to do the reading. It was also wrong
	// repeatedly: `sed -n` and `grep` read as program runs until the verb list was fixed, a quoted
	// `|` split a grep into a fragment no verb matched, and `find` and `git log` still read as runs.
	// An exit is a fact and a command's text is a fact; which of them exercised anything is for
	// whoever reads this. What the sorting existed to catch — the same check re-run forever — the
	// two repeat lines above now state outright.
	// One entry per DISTINCT command, in the order first issued. The repeat line above already
	// carries the counts, so printing the same string three times here would be the noise this
	// block exists to cut through. When a command ran more than once its LAST status is the one
	// shown — that is the current state of the thing it did.
	//
	// Only a non-zero exit magi actually learned is appended. A zero is what the reader assumes,
	// and "unknown" on a `cat … | head` says nothing about what to do next; both are already in the
	// tool result sitting beside this block. What survives compaction, and so has to live here, is
	// WHICH commands ran and which of them ended badly.
	var ran []string
	at := map[string]int{}
	for _, c := range o.cmds {
		line := clipLine(c.cmd, 70)
		switch {
		case len(c.stages) > 1:
			// The pipeline's own statuses, which the reported exit cannot show. Stated, not read:
			// which of them matters is the reader's call.
			parts := make([]string, len(c.stages))
			for i, st := range c.stages {
				parts[i] = strconv.Itoa(st)
			}
			line += " → stages " + strings.Join(parts, " ")
		case !c.unclear && c.exit != 0:
			line += fmt.Sprintf(" → exit %d", c.exit)
		}
		key := strings.Join(strings.Fields(c.cmd), " ")
		if i, seen := at[key]; seen {
			ran[i] = line
			continue
		}
		at[key] = len(ran)
		ran = append(ran, line)
	}
	if len(ran) > 0 {
		b.WriteString("\ncommands: " + strings.Join(clipEach(ran, 12), " · "))
	}
	return b.String()
}

// exitOfBashResult reads the `exit N` the bash tool puts at the head of its result. Returns
// (0, false) when the result carries none — a background start, or a shape this does not know —
// so an unreadable result is never mistaken for a clean exit.
func exitOfBashResult(res string) (int, bool) {
	s := strings.TrimSpace(decodeResultText(res))
	if !strings.HasPrefix(s, "exit ") {
		return 0, false
	}
	f := strings.Fields(s)
	if len(f) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(f[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// decodeResultText unwraps a tool result's stored form. Content is a json.RawMessage, so a bash
// result arrives as a JSON STRING — quoted, with its newlines escaped — and reading it as raw text
// leaves `exit 0\noutput: …` glued into one token. Anything that is not a JSON string (a structured
// result from another tool) is returned unchanged.
func decodeResultText(res string) string {
	var s string
	if json.Unmarshal([]byte(res), &s) == nil {
		return s
	}
	return res
}

// stagesOfBashResult pulls the per-stage statuses out of a bash result's stage note. The note
// states them as "Its stages exited 2 → 0 (left to right)"; nothing else in a result looks like that,
// and a result without the note yields nothing.
//
// It replaced a scan for the sentence "the work at the head of the pipe FAILED" — a verdict the
// note no longer makes. Reading prose for a bit is how that broke without a test noticing; reading
// it for numbers at least fails loudly, and the numbers are what the record wanted.
func stagesOfBashResult(res string) []int {
	m := stagesNoteRe.FindStringSubmatch(res)
	if m == nil {
		return nil
	}
	var out []int
	for _, f := range strings.Split(m[1], "→") {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			return nil // a shape magi does not recognize is one it must not report numbers from
		}
		out = append(out, n)
	}
	return out
}

var stagesNoteRe = regexp.MustCompile(`Its stages exited ([0-9\s\x{2192}]+) \(left to right\)`)
