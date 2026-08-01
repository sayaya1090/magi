package tui

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Sweep seven: sequences nobody wrote down.
//
// The scripted scenarios cover the orders someone thought of, and both bugs found on day one were
// orders nobody had. So this walks a long random sequence of the things a session really does —
// prompts, steers, deltas, tool calls and results, council rounds, resizes, scrolls, modals,
// errors, finishes — and after every single step checks the invariants the whole view rests on.
//
// Seeded, so a failure is reproducible: the seed and the step are printed with it.
//
// The seeds come from MAGI_FUZZ_SEEDS when it is set, and from the baseline below when it is not.
// Walking a FRESH set is how this keeps finding things — the same twenty orders, however long the
// sequence, are twenty orders — but rotating them by editing this line put the rotation in the
// commit history and made the list read as a record of what was last run, which it is not. The
// baseline stays fixed so CI and a bisect always walk the same ground.
func TestRandomSessionsKeepTheViewCoherent(t *testing.T) {
	for _, seed := range fuzzSeeds(t) {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			s := newScript(t)
			// Real sessions for the picker step, made once: their IDs have to resolve, because
			// the picker swallows a later enter and switches to the selected row.
			var pickerRows []session.SessionMeta
			for i := 0; i < 20; i++ {
				if _, err := s.m.app.CreateSession(context.Background(),
					command.CreateSession{Workdir: s.m.workdir}); err != nil {
					t.Fatal(err)
				}
			}
			if metas, err := s.m.app.ListSessions(context.Background(), s.m.workdir); err == nil {
				pickerRows = metas
			}
			ids, calls := 0, 0
			// Which tool each call id was, so a result can be the shape THAT tool's body parses.
			// renderToolBody dispatches on the call's name: a grep-shaped result delivered to a
			// list call renders nothing at all.
			callTool := map[string]string{}
			cacheChecks := 0
			var prevID string
			var prevOK bool

			steps := []struct {
				what string
				do   func()
			}{
				{"user prompt", func() {
					ids++
					id := fmt.Sprintf("r%d", ids)
					before := len(s.m.blocks)
					s.typeText(fmt.Sprintf("request %d", ids)).enter()
					// Emit the prompt the VIEW actually submitted, not the one the walk intended.
					// A modal, the search bar or the palette can swallow the leading keystrokes,
					// and then the box holds a fragment — in production the app receives exactly
					// what was sent, so an event carrying different text is a state that cannot
					// happen, and asserting on it would be testing the harness.
					var sent string
					var ok bool
					for i := len(s.m.blocks) - 1; i >= before; i-- {
						if s.m.blocks[i].kind == blockUser && s.m.blocks[i].reqID == "" {
							sent, ok = s.m.blocks[i].text, true
							break
						}
					}
					if !ok {
						return // the keys never became a prompt; nothing was submitted
					}
					s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
						event.PromptSubmittedData{MessageID: id, Parts: []session.Part{{Kind: session.PartText, Text: sent}}})
				}},
				{"system note", func() {
					s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"},
						event.PromptSubmittedData{MessageID: "n", Parts: []session.Part{{Kind: session.PartText, Text: "act now"}}})
				}},
				{"delta", func() {
					s.emit(event.TypePartDelta, event.PartDeltaData{MessageID: "m", PartID: "p", Kind: session.PartText, Text: "tok "})
				}},
				{"reasoning delta", func() {
					s.emit(event.TypePartDelta, event.PartDeltaData{MessageID: "m", PartID: "pr", Kind: session.PartReasoning, Text: "think "})
				}},
				{"assistant text", func() { s.assistantText("a settled answer") }},
				{"tool call", func() {
					// Twelve sweeps only ever called bash, so every other tool's line and body
					// renderer was walked zero times — including the edit diff, which is its own
					// parser (argPath, diffBaseLine, parseTrailingAt, rawPath, renderCodeDiff) and
					// the thing a user reads most of. An agent's transcript is not one tool.
					calls++
					id := fmt.Sprintf("c%d", calls)
					switch rng.Intn(7) {
					case 0:
						callTool[id] = "read"
						s.toolCallArgs("read", id, `{"path":"/app/main.go","offset":40,"limit":30}`)
					case 1:
						callTool[id] = "write"
						s.toolCallArgs("write", id, `{"path":"/app/main.go","content":"package main\n\nfunc main() {}\n"}`)
					case 2:
						callTool[id] = "edit"
						s.toolCallArgs("edit", id,
							`{"path":"/app/main.go","old":"func main() {}","new":"func main() {\n\tprintln(\"hi\")\n}"}`)
					case 3:
						callTool[id] = "grep"
						s.toolCallArgs("grep", id, `{"pattern":"func [a-z]+","path":"/app"}`)
					case 4:
						callTool[id] = "glob"
						s.toolCallArgs("glob", id, `{"pattern":"**/*.go"}`)
					case 5:
						callTool[id] = "list"
						s.toolCallArgs("list", id, `{"path":"/app"}`)
					default:
						callTool[id] = "bash"
						s.toolCall("bash", id)
					}
				}},
				{"tool result", func() {
					if calls == 0 {
						return
					}
					// The result has to be the shape its tool's body renderer PARSES. A plain
					// string leaves grepBody, globBody and listBody returning nil at their first
					// line — they were entered and did nothing, which reads as coverage and is
					// not. This is the third time in this walk that feeding a view something it
					// does not read looked like a covered step.
					//
					// Sometimes long enough to fold, because the collapsed cap and the "+N more"
					// footer are only reached by a body that overflows them.
					n := 1 + rng.Intn(3)
					if rng.Intn(3) == 0 {
						n = 10 + rng.Intn(40)
					}
					id := fmt.Sprintf("c%d", rng.Intn(calls)+1)
					var b strings.Builder
					switch callTool[id] {
					case "read": // numbered lines, the shape readBody splits on
						for i := 0; i < n; i++ {
							b.WriteString(fmt.Sprintf("%d\tfunc line%02d() {}\n", i+1, i))
						}
					case "grep": // "rel:line:text"
						var xs []string
						for i := 0; i < n; i++ {
							xs = append(xs, fmt.Sprintf("app/main.go:%d:  func line%02d() {}", i+1, i))
						}
						b.WriteString(mustJSONString(xs))
					case "glob": // a list of paths
						var xs []string
						for i := 0; i < n; i++ {
							xs = append(xs, fmt.Sprintf("app/pkg%02d/file.go", i))
						}
						b.WriteString(mustJSONString(xs))
					case "list": // name/isDir entries
						b.WriteString("[")
						for i := 0; i < n; i++ {
							if i > 0 {
								b.WriteString(",")
							}
							b.WriteString(fmt.Sprintf(`{"name":"entry%02d","isDir":%v}`, i, i%3 == 0))
						}
						b.WriteString("]")
					default:
						for i := 0; i < n; i++ {
							b.WriteString(fmt.Sprintf("line %02d of a tool body\n", i))
						}
					}
					s.toolResult(id, b.String())
				}},
				{"orphan result", func() { s.toolResult("", "output with no call") }},
				{"council round", func() {
					s.emit(event.TypeCouncilConvened, event.CouncilConvenedData{
						Round: 1, Members: []string{"Melchior"}, Rule: "majority", Task: "t", Actions: "- bash → ok"})
					s.emit(event.TypeCouncilVerdict, event.CouncilVerdictData{Round: 1, Member: "Melchior", Decision: "done"})
					s.emit(event.TypeCouncilDecided, event.CouncilDecidedData{Round: 1, Decision: "done"})
				}},
				{"todos", func() {
					// Through the APP, not the event. The overview panel reads m.app.Todos(); an
					// emitted TodosChanged leaves it empty, so ten sweeps of this walk drew the
					// plan panel's rows zero times (appendPlanSteps, todoLine and ctxBar all sat
					// at 0%) while the step read as covering them. The panel's own probe was
					// written the same wrong way once and its first "all widths pass" verified
					// nothing, which is how this shape is known.
					//
					// The count varies because the defect found in that panel was a LONG plan:
					// it had no vertical bound, and floatPanel then refused to draw a box taller
					// than the screen, so the panel a long task is watched through was the one a
					// long task removed.
					n := 1 + rng.Intn(30)
					td := make([]session.Todo, n)
					for i := range td {
						st := "pending"
						switch {
						case i == 0:
							st = "in_progress"
						case i < n/3:
							st = "completed"
						}
						td[i] = session.Todo{
							Content: strings.Repeat(fmt.Sprintf("step %02d ", i), 1+rng.Intn(3)),
							Status:  st,
						}
					}
					s.m.app.SetTodos(s.m.sid, td)
					s.emit(event.TypeTodosChanged, event.TodosChangedData{Todos: td})
				}},
				{"context usage", func() {
					// With Percent left at zero the panel's Context section never draws — the
					// producer (loop.go) always fills it, so a walk that omits it is asserting
					// about an event the app does not emit. ctxBar sat at 0% behind that.
					tok := rng.Intn(70000)
					s.emit(event.TypeContextUsage, event.ContextUsageData{
						Tokens: tok, Window: 65536, Percent: float64(tok) / 65536 * 100,
						OutTokens: rng.Intn(4000)})
				}},
				{"permission", func() {
					s.emit(event.TypePermissionRequested, event.PermissionRequestedData{
						CallID: "cx", Name: "bash", Args: []byte(`{"command":"x"}`), Reason: "why"})
				}},
				{"answer permission", func() { s.send(tea.KeyPressMsg{Code: tea.KeyEscape}) }},
				{"resize", func() {
					s.send(tea.WindowSizeMsg{Width: 30 + rng.Intn(90), Height: 12 + rng.Intn(30)})
				}},
				{"tiny terminal", func() {
					// A split pane, a phone-sized ssh window, a terminal being dragged smaller: the
					// header alone is two rows and the input box is three, so a very short screen
					// leaves the transcript a negative number of rows unless something clamps it.
					// Eight rows is the floor this layout honours with a modal open: header(2) +
					// input box(3) + a modal shrunk to its irreducible prompt. Below that the
					// frame would have to hide something the user needs, and the fuzz does not
					// pretend otherwise rather than asserting a bar nothing can clear.
					s.send(tea.WindowSizeMsg{Width: 20 + rng.Intn(25), Height: 8 + rng.Intn(8)})
				}},
				{"question modal", func() {
					// Two one-letter options can never overflow anything, so seven sweeps of this
					// walk asked the same unoverflowable question and the modal's trim was never
					// entered — the defect found there on 2026-08-01 (the trim kept the FIRST
					// options, so the one being answered went off screen) was found by hand
					// instead. A real ask_user names as many choices as it has, at whatever length
					// the choices are, so the walk does too.
					n := 2 + rng.Intn(10)
					opts := make([]string, n)
					for i := range opts {
						opts[i] = strings.Repeat(fmt.Sprintf("choice %d ", i+1), 1+rng.Intn(4))
					}
					s.emit(event.TypeQuestionRequested, event.QuestionRequestedData{
						CallID: "q1", Question: "which one?", Options: opts})
					// …and the pick moves, because the trim is centred on it.
					for k := rng.Intn(n); k > 0; k-- {
						s.send(tea.KeyPressMsg{Code: tea.KeyDown})
					}
				}},
				{"session picker", func() {
					// resumeView was drawn zero times in eight sweeps — the walk's own dump printed
					// `resuming=false` on every failure. It is a full-screen list in the same slot
					// as the modals, and it was the last surface there that reserved a constant
					// instead of measuring.
					//
					// The rows are REAL sessions. A fabricated meta has an empty ID, and the enter
					// that a later "user prompt" step sends is swallowed by the picker and switches
					// to whatever row is selected — so a made-up list makes the app switch to a
					// session that does not exist, which is a state no /resume can produce. The
					// list is built once per walk (see pickerRows) because creating two dozen
					// sessions per step would dominate the run.
					if len(pickerRows) == 0 {
						t.Skip("no sessions to pick from")
					}
					s.m.resumeList = pickerRows[:1+rng.Intn(len(pickerRows))]
					s.m.resuming = true
					s.m.resumeSel = rng.Intn(len(s.m.resumeList))
					s.m.refresh()
					// Usually dismissed, the way a user who opened it to look does. Left open, it
					// swallows the next step's enter and switches session — which wipes the
					// transcript, and a walk that switches every few steps spends most of its
					// length in an empty session. Measured: with it always left open, not one
					// tool block survived to the end of a 500-step walk, so every renderer that
					// needs a settled transcript was starved. A quarter of the time is enough to
					// keep the switch itself walked.
					if rng.Intn(4) != 0 {
						s.send(tea.KeyPressMsg{Code: tea.KeyEscape})
					}
				}},
				{"close the picker", func() {
					s.m.resuming = false
					s.m.refresh()
				}},
				{"open the palette", func() {
					// The walk never typed a slash, so the completion popup — the other list that
					// windows itself against modalRoom — was drawn zero times in seven sweeps.
					// It is covered directly elsewhere, but not while a job pane is up, a resize
					// lands, or a permission modal opens over it, which is what this walk is for.
					s.typeText("/")
					for k := rng.Intn(6); k > 0; k-- {
						s.send(tea.KeyPressMsg{Code: tea.KeyDown})
					}
				}},
				{"diagnostic", func() {
					s.emit(event.TypeDiagnostic, event.DiagnosticData{Source: "council", Detail: "unparsed reply"})
				}},
				{"scroll", func() {
					if rng.Intn(2) == 0 {
						s.send(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
					} else {
						s.send(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
					}
				}},
				{"interject answered", func() {
					s.emitAs(event.TypeInterjectionAnswered, event.Actor{Kind: event.ActorSystem, ID: "interject"},
						event.InterjectionAnsweredData{MessageID: fmt.Sprintf("r%d", 1+rng.Intn(max(ids, 1)))})
				}},
				{"open search", func() {
					// ctrl+f opens a search bar that captures keys like a modal. The walk had no
					// step that ever populated searchHits, which left the "a hit outside the
					// transcript panics the jump" invariant below asserting over an always-empty
					// list — present, and proving nothing.
					s.send(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
					for _, r := range []rune("re") { // matches "request N" and the tool lines
						s.send(tea.KeyPressMsg{Code: r, Text: string(r)})
					}
				}},
				{"search step", func() {
					s.send(tea.KeyPressMsg{Code: tea.KeyEnter})
				}},
				{"close search", func() {
					s.send(tea.KeyPressMsg{Code: tea.KeyEscape})
				}},
				{"compaction", func() {
					s.emit(event.TypeCompaction, event.CompactionData{Summary: "s", TokensBefore: 100, TokensAfter: 10})
				}},
				{"background job pane", func() {
					// The pane strip was outside this walk entirely, and it joins the same frame
					// the width invariant below measures. Built with the fields syncJobPanes
					// fills in, so this is the state a real `bash background=true` produces —
					// nothing here is reachable only from a test.
					if len(s.m.panes) >= 4 {
						return
					}
					s.m.subID++
					s.m.panes = append(s.m.panes, &agentPane{
						job:     fmt.Sprintf("bg_%d", s.m.subID),
						role:    fmt.Sprintf("bg_%d", s.m.subID),
						sub:     s.m.subID,
						started: time.Now().Add(-time.Duration(rng.Intn(600)) * time.Second),
						task:    "bash -c 'cd /app && make -j8 CFLAGS=-O2 all && ./run_integration_suite --verbose'",
						live:    "compiling module 42 of 97\n",
					})
					s.m.dirty = true
				}},
				{"job exits", func() {
					if len(s.m.panes) == 0 {
						return
					}
					p := s.m.panes[rng.Intn(len(s.m.panes))]
					if p.done {
						return
					}
					p.done, p.exited, p.doneAt = true, true, time.Now()
					p.exit = rng.Intn(2) // a clean exit and a failing one render differently
					s.m.dirty = true
				}},
				{"focus a pane", func() {
					if len(s.m.panes) == 0 {
						return
					}
					s.m.focusPane = rng.Intn(len(s.m.panes))
				}},
				{"click the transcript", func() {
					// A plain click is how a reasoning block or a long tool body is expanded, and
					// toggleThoughtAt is the ONLY partial cache truncation in the package — it
					// keeps the prefix up to the toggled block and drops the rest. Nine sweeps
					// never clicked, so the one invalidation that is not a full reset was walked
					// zero times while the per-block cache-vs-fresh check right below sat there
					// ready to catch it. Most clicks land on nothing, which is also what a user's
					// clicks do.
					if s.m.height <= 2 || s.m.width <= 2 {
						return
					}
					// Press AND release at the same cell. The press only starts a selection; the
					// toggle, the copy chip and the council detail all hang off the RELEASE, so a
					// click alone enters none of them — the first pass at this step sent one and
					// left toggleThoughtAt at 0% while the run went green.
					x, y := rng.Intn(s.m.width), rng.Intn(s.m.height)
					s.m.handleMouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
					s.m.handleMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: x, Y: y})
					s.m.refresh()
				}},
				{"drag the panel splitter", func() {
					// The splitter is the one control that changes transcriptWidth WITHOUT changing
					// the terminal width, and transcriptWidth is the render cache's key. That is
					// not a guess about what could go wrong — the key was m.width once, and
					// dragging the splitter left every cached block wrapped to the width it no
					// longer had. Eleven sweeps never grabbed it, so the per-block cache-vs-fresh
					// check below has never seen this path.
					if s.m.height <= 4 || s.m.width <= 8 {
						return
					}
					y := 1 + rng.Intn(s.m.height-2)
					for x := 0; x < s.m.width; x++ {
						if !s.m.onPanelSplitter(x, y) {
							continue
						}
						s.m.handleMouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
						for k := 1 + rng.Intn(3); k > 0; k-- {
							s.m.handleMouse(tea.MouseMotionMsg{
								Button: tea.MouseLeft, X: rng.Intn(s.m.width), Y: y})
						}
						s.m.handleMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: rng.Intn(s.m.width), Y: y})
						s.m.refresh()
						return
					}
				}},
				{"error", func() { s.emit(event.TypeError, event.ErrorData{Message: "the provider refused"}) }},
				{"finish", func() { s.emit(event.TypeTurnFinished, event.TurnFinishedData{}) }},
			}

			for step := 0; step < 500; step++ {
				pick := steps[rng.Intn(len(steps))]
				pick.do()
				raw := s.rawView()
				where := fmt.Sprintf("seed %d, step %d (%s)", seed, step, pick.what)

				// One recorded start line per block, ascending — this is what turns a click into
				// a block, and it is the invariant the queued-tail rendering nearly broke.
				if len(s.m.blockLineStart) != len(s.m.blocks) {
					t.Fatalf("%s: %d start lines for %d blocks", where, len(s.m.blockLineStart), len(s.m.blocks))
				}
				for i := 1; i < len(s.m.blockLineStart); i++ {
					if s.m.blockLineStart[i] < s.m.blockLineStart[i-1] {
						t.Fatalf("%s: block %d starts at %d, before block %d at %d",
							where, i, s.m.blockLineStart[i], i-1, s.m.blockLineStart[i-1])
					}
				}
				// Nothing may be wider than the terminal it is drawn in. Report the WIDEST row, not
				// the first over-wide one: the frame is joined vertically, so one long row pads
				// every other to match and the first offender is almost never the culprit.
				// Measured on the row's own CONTENT: the vertical join pads every row to the widest
				// one, so raw widths are all equal and the first row always looks like the culprit.
				// Trailing blanks are padding; what is left is what the row actually drew.
				lines := strings.Split(raw, "\n")
				widest, at := 0, -1
				for i, line := range lines {
					trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(line, ""), " ")
					if w := lipgloss.Width(trimmed); w > widest {
						widest, at = w, i
					}
				}
				if widest > s.m.width {
					t.Fatalf("%s: line %d draws %d cells in a %d-column terminal:\n%q",
						where, at, widest, s.m.width, lines[at])
				}
				// A session with content never renders an empty screen.
				if len(s.m.blocks) > 0 && strings.TrimSpace(ansiSeq.ReplaceAllString(raw, "")) == "" {
					t.Fatalf("%s: %d blocks and a blank frame", where, len(s.m.blocks))
				}
				// The spinner is attached to a request id; a dangling one animates beside nothing.
				if s.m.turnReqID != "" && prevOK && s.m.turnReqID == prevID {
					gone := true
					for _, b := range s.m.blocks {
						if b.reqID == s.m.turnReqID {
							gone = false
						}
					}
					if gone {
						t.Fatalf("%s: the block carrying turnReqID %q vanished during this action", where, s.m.turnReqID)
					}
				}
				prevID, prevOK = s.m.turnReqID, false
				for _, b := range s.m.blocks {
					if b.reqID == prevID {
						prevOK = true
					}
				}
				if s.m.turnReqID != "" {
					found := false
					for _, b := range s.m.blocks {
						if b.reqID == s.m.turnReqID {
							found = true
						}
					}
					if !found {
						var sample []string
						for _, b := range s.m.blocks {
							if b.kind == blockUser {
								sample = append(sample, fmt.Sprintf("%q/%q", b.reqID, b.text))
							}
						}
						if len(sample) > 8 {
							sample = sample[len(sample)-8:]
						}
						t.Fatalf("%s: turnReqID %q matches no block; last user bubbles: %v", where, s.m.turnReqID, sample)
					}
				}
				// A request id identifies ONE bubble. Two would mean a prompt was echoed twice,
				// and the pairing helpers (which find "the" block by id) would pick either.
				seenIDs := map[string]int{}
				for _, b := range s.m.blocks {
					if b.kind == blockUser && b.reqID != "" {
						seenIDs[b.reqID]++
						if seenIDs[b.reqID] > 1 {
							t.Fatalf("%s: request %q has %d bubbles", where, b.reqID, seenIDs[b.reqID])
						}
					}
				}
				// A verdict row with no verdicts renders an empty council line.
				for i, b := range s.m.blocks {
					if b.kind == blockCouncilVerdict && len(b.councilVerdicts) == 0 {
						t.Fatalf("%s: block %d is a verdict row with no verdicts", where, i)
					}
				}
				// Not asserted: that a settled block never changes what it says. It is a property
				// worth having, and it is not expressible here — a block is identified by its
				// POSITION, and positions legitimately move (the queued-tail hoist, the two
				// question/answer pairings) while a tool call gains its result in place. Keying on
				// the index therefore reports every deliberate reorder as a rewrite. Giving blocks
				// a stable id would fix that, but changing the data model to suit a test is the
				// wrong direction, so this is left unclaimed rather than half-claimed.
				// The render cache is a prefix of the block list; an entry past the end would be
				// served for a block that no longer exists.
				if len(s.m.cache) > len(s.m.blocks) {
					t.Fatalf("%s: %d cached renders for %d blocks", where, len(s.m.cache), len(s.m.blocks))
				}
				// The viewport never scrolls past its own content.
				if off, h := s.m.vp.YOffset(), s.m.vp.TotalLineCount(); off < 0 || (h > 0 && off > h) {
					t.Fatalf("%s: viewport offset %d in %d lines of content", where, off, h)
				}
				// Search hits index the plain transcript; a stale hit past its end panics the jump.
				for _, h := range s.m.searchHits {
					if h < 0 || h >= len(s.m.contentPlain) {
						t.Fatalf("%s: search hit %d is outside the %d-line transcript", where, h, len(s.m.contentPlain))
					}
				}
				// Every cached render must still be what the block renders NOW. The cache is a
				// prefix keyed by index, so a block that changes in place — a call gaining its
				// result, a queued bubble losing its glyph, a bubble being reordered — is only
				// correct if whoever mutated it also truncated the cache. Nothing checked that:
				// the length invariant above passes just as happily over a stale entry, and a
				// stale entry is a frame showing something that is no longer true.
				//
				// The tail is checked EVERY step and the whole history on a stride. Both are
				// needed: re-rendering everything each step is too slow to run 500 of, and a
				// stride alone proves very little, because a resize, a finish or a queued hoist
				// drops the entire cache — so staleness introduced at one step is routinely
				// wiped before the next stride sees it. Every in-place mutation there is
				// (folding a result, clearing a queued glyph, reordering an answered bubble)
				// lands within a few blocks of the end, which is what the tail window covers.
				{
					from := 0
					if step%25 != 0 {
						from = max(0, len(s.m.cache)-40)
					}
					for i := from; i < len(s.m.blocks); i++ {
						if i >= len(s.m.cache) {
							break
						}
						// The one entry allowed to differ: the in-flight bubble carries a spinner
						// frame, and transcript() deliberately renders it fresh past the cache.
						if s.m.running && s.m.turnReqID != "" &&
							s.m.blocks[i].kind == blockUser && s.m.blocks[i].reqID == s.m.turnReqID {
							continue
						}
						cacheChecks++
						if fresh := s.m.renderBlock(s.m.blocks[i]); fresh != s.m.cache[i] {
							t.Fatalf("%s: block %d (%v) is cached stale — the screen shows what it no longer says\ncached: %q\nfresh:  %q",
								where, i, s.m.blocks[i].kind,
								ansiSeq.ReplaceAllString(s.m.cache[i], ""), ansiSeq.ReplaceAllString(fresh, ""))
						}
					}
				}
				// The frame must fit the terminal vertically too. One row too many scrolls the
				// screen, which on an alt-screen UI means the top of the frame is simply gone.
				if rows := len(lines); s.m.height > 0 && rows > s.m.height {
					t.Fatalf("%s: the frame is %d rows in a %d-row terminal (chrome=%d modalRoom=%d perm=%d quest=%d vp=%d palette=%d resuming=%v):\n%s",
						where, rows, s.m.height, s.m.chromeHeight(), s.m.modalRoom(),
						lipglossHeightOrZero(s.m.perm != nil, s.m.permView), lipglossHeightOrZero(s.m.quest != nil, s.m.questView),
						s.m.vp.Height(), len(s.m.paletteMatches()), s.m.resuming,
						ansiSeq.ReplaceAllString(raw, ""))
				}
			}
			// A green cache check that never compared anything is a green that means nothing —
			// every entry could have been exempt, or the cache empty at every stride.
			if cacheChecks == 0 {
				t.Error("the cache-coherence check never compared a single entry")
			}
		})
	}
}

// fuzzBaseline is the fixed set. It is not special — any twenty seeds are — and it is written
// down so an unconfigured run, a CI run and a bisect all walk the same sequences.
var fuzzBaseline = []int64{7717, 7723, 7727, 7741, 7753, 7757, 7759, 7789, 7793, 7817,
	7823, 7829, 7841, 7853, 7867, 7873, 7877, 7879, 7883, 7901}

// fuzzSeeds reads MAGI_FUZZ_SEEDS — comma or space separated — falling back to the baseline.
//
// A malformed entry FAILS rather than being skipped. A sweep that quietly walked nineteen seeds
// because one had a typo would report the same green as a sweep that walked twenty, and the whole
// value of rotating them is knowing which ground was covered.
func fuzzSeeds(t *testing.T) []int64 {
	raw := strings.TrimSpace(os.Getenv("MAGI_FUZZ_SEEDS"))
	if raw == "" {
		return fuzzBaseline
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
	var out []int64
	for _, f := range fields {
		n, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			t.Fatalf("MAGI_FUZZ_SEEDS has a seed that is not a number: %q (%v)", f, err)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		t.Fatal("MAGI_FUZZ_SEEDS is set but holds no seeds")
	}
	return out
}

// lipglossHeightOrZero measures a modal only when it is up.
func lipglossHeightOrZero(up bool, render func() string) int {
	if !up {
		return 0
	}
	return lipgloss.Height(render())
}
