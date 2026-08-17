package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/core/text"
	"github.com/sayaya1090/magi/internal/port"
)

// gateAllowlist blocks a tool the agent isn't permitted to call. Returns true to stop.
//
// The refusal NAMES what the agent may call instead, from the same toolSpecs that built the
// offer it was given — so the list cannot drift from the one the model saw. A bare "not
// permitted" is a dead end: it says the call failed and nothing about what would not, so the
// model's only move is to try again. Observed repeatedly — a read-only explorer calls a tool
// outside its set, is refused with no alternative, retries the identical call four times, and
// the loop guard kills the turn. The last sentence exists for the case the list cannot help
// with: an agent that genuinely has no way to do the thing should SAY so, not keep asking.
func (a *App) gateAllowlist(ctx context.Context, s session.Session, agent AgentSpec, depth int, actor event.Actor, tc *session.ToolCall, toolMsgID string) bool {
	if agent.allows(tc.Name) {
		return false
	}
	msg := "tool not permitted for agent " + agent.Name + " — this call did nothing."
	var names []string
	for _, spec := range a.toolSpecs(agent) {
		names = append(names, spec.Name)
	}
	if len(names) > 0 {
		msg += " " + agent.Name + " may call: " + strings.Join(names, ", ") + "."
	}
	msg += " Retrying " + tc.Name + " will be refused the same way. Use one of the tools above, or " +
		"if none of them can do this, say so in your reply and continue with what you can."
	a.appendToolResult(ctx, s.ID, actor, toolMsgID, tc.CallID, msg, true)
	return true
}

// dangerGated reports whether a tool needs approval before it runs: prompted under ask/auto,
// refused under deny, waved through under allow.
//
// Two sources, because the set is not fixed at startup. The configured map covers the built-ins
// (and anything cmd wires in). An MCP tool is danger-gated BY CONSTRUCTION, read off the
// `mcp__<server>__<tool>` namespace the manager enforces: the set of MCP tools changes while the
// process runs (a server registered by a plugin, one that dies mid-session), so a map filled at
// startup cannot know them — and it didn't, which meant every MCP tool ran without a prompt in
// every mode, `deny` included, while EXTENDING promised the opposite. An external tool is a
// process this binary did not write doing whatever it does; "ask first" is the only default that
// matches what the mode names mean.
func (a *App) dangerGated(name string) bool {
	return a.cfg.DangerTools[name] || strings.HasPrefix(name, "mcp__")
}

// gatePermission applies the guardrail policy (a hard deny blocks regardless of mode) and
// prompts for dangerous or policy-forced tool calls, recording the PermissionDecided fact.
// Returns true to stop (policy deny, or the user denied the prompt).
func (a *App) gatePermission(ctx context.Context, sid session.SessionID, actor event.Actor, tc *session.ToolCall, toolMsgID string) bool {
	verdict, reason := a.policy.Decide(tc.Name, tc.Args)
	if verdict == "deny" {
		dd, _ := json.Marshal(event.PermissionDecidedData{CallID: tc.CallID, Decision: "deny"})
		a.appendFact(ctx, sid, event.TypePermissionDecided, actor, dd)
		a.appendToolResult(ctx, sid, actor, toolMsgID, tc.CallID, "blocked by policy: "+reason, true)
		return true
	}
	forcePrompt := verdict == "ask"
	if !forcePrompt {
		reason = "" // routine danger-tool confirmation, not a policy hit
	}
	// A scanner-forced "ask" (a destructive invocation, an egress command, a pipe-to-shell, a
	// secret-path reference) is NOT waived by an allow rule. An allow rule pre-approves a program
	// for ordinary use — `bash(git:*)` trusts git — but "git status && rm -rf build" both matches
	// that rule (it begins "git ") AND carries a destructive tail the scanner flagged, and a broad
	// prefix rule persisted from one benign approval must not become a standing waiver for the
	// dangerous invocation chained after it. So forcePrompt prompts regardless of the rule; the
	// rule only skips the prompt for a routine danger-tool call the scanner did not flag.
	if forcePrompt || (a.dangerGated(tc.Name) && !a.policy.AllowedByRule(tc.Name, tc.Args)) {
		allowed := a.requestPermission(ctx, sid, actor, tc, forcePrompt, reason)
		decision := "allow"
		if !allowed {
			decision = "deny"
		}
		dd, _ := json.Marshal(event.PermissionDecidedData{CallID: tc.CallID, Decision: decision})
		a.appendFact(ctx, sid, event.TypePermissionDecided, actor, dd)
		if !allowed {
			a.appendToolResult(ctx, sid, actor, toolMsgID, tc.CallID, a.denyReason(tc.Name), true)
			return true
		}
	}
	return false
}

// denyReason explains a permission denial to the agent as its tool result. In a
// headless run there is no human to say "no": requestPermission denied the call
// categorically because the permission mode (ask/auto/deny) cannot approve a
// dangerous/forced tool with no interactive prompt. Reporting that as "denied by
// user" is a lie that invites a pointless retry — the agent re-issues the same call,
// gets denied again, and flails. So name the real cause and tell it not to retry.
// With an interactive human present it genuinely WAS a user decision.
func (a *App) denyReason(tool string) string {
	if a.cfg.Interactive {
		return "denied by user"
	}
	return fmt.Sprintf(
		"%s is unavailable in this headless run: permission mode %q cannot approve it with no interactive prompt. "+
			"Do not retry this tool — continue without it, or stop and report that it could not be run and why. "+
			"(The operator can re-run with --permission allow to enable it.)",
		tool, a.Permission())
}

// readOnlyPaths is what stays read-only inside whatever the sandbox makes writable.
//
// The transcript, and only the transcript so far. Everything else magi owns is either outside the
// writable set already (the config dir) or is the workspace itself, which is the point.
func readOnlyPaths(cfg Config) []string {
	if cfg.StoreDir == "" {
		return nil
	}
	return []string{cfg.StoreDir}
}

// gatePreHooks runs PreToolUse hooks, which can block execution (e.g. protect paths).
// Returns true to stop.
func (a *App) gatePreHooks(ctx context.Context, s session.Session, actor event.Actor, tc *session.ToolCall, toolMsgID string) bool {
	if block := a.runPreToolHooks(ctx, s.Workdir, tc.Name, pathArg(tc.Args)); block != "" {
		a.appendToolResult(ctx, s.ID, actor, toolMsgID, tc.CallID, "blocked by hook: "+block, true)
		return true
	}
	return false
}

// gateStaleAnchor refuses an `at`/`to` edit whose anchored line no longer holds what the agent was
// shown. Returns true to stop.
//
// The anchored edit path is the one place a tool call names a location without naming its content:
// `old`/`new` is checked by construction (the string has to match), and `at`/`to` is checked only
// for being in range. So an edit that shifted the line count — the agent's own previous edit, a
// post-edit formatter, anything — leaves every later anchor pointing one place and meaning another,
// and the write lands silently on the wrong line.
//
// It fires only where magi has a record of that exact line (see runGuard.shownLines), so a file the
// agent never read is not refused; it is simply unguarded, as it was before.
func (a *App) gateStaleAnchor(ctx context.Context, s session.Session, actor event.Actor, tc *session.ToolCall, guard *runGuard, toolMsgID string) bool {
	if guard == nil || tc.Name != "edit" {
		return false
	}
	var ea struct {
		Path string          `json:"path"`
		At   builtin.FlexInt `json:"at"`
	}
	// Tolerant on purpose, like every other reader of a model's arguments here: "540.0" is as
	// common as 540, and a strict parse would drop the guard rather than the call.
	if json.Unmarshal(tc.Args, &ea) != nil || ea.Path == "" || int(ea.At) <= 0 {
		return false
	}
	abs := ea.Path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(s.Workdir, abs)
	}
	current, err := os.ReadFile(abs)
	if err != nil {
		return false // the tool reports a missing/unreadable file in its own words
	}
	if !guard.anchorDrifted(relForChange(s.Workdir, ea.Path), int(ea.At), string(current)) {
		return false
	}
	a.appendToolResult(ctx, s.ID, actor, toolMsgID, tc.CallID,
		fmt.Sprintf("line %d of %s is not the line you read — the file changed after that read "+
			"(an earlier edit, a formatter, or something outside this turn). Re-read the file and "+
			"anchor to the line numbers it returns; editing this anchor now would write to the "+
			"wrong line.", int(ea.At), ea.Path), true)
	return true
}

// executeTool runs one tool call (with permission gating) and persists the result.
// turnTask is the user instruction this turn answers. It reaches here for one reason: what an agent
// learns is worth little without what it was doing when it learned it, and the experience store had
// no way to know. See the Propose closure below.
func (a *App) executeTool(ctx context.Context, s session.Session, agent AgentSpec, depth int, actor event.Actor, tc *session.ToolCall, guard *runGuard, turnTask string) {
	sid := s.ID
	workdir := s.Workdir
	toolMsgID := "m_" + newID()

	// A panic anywhere under one tool call used to end the whole run. Observed live: an off-by-one
	// in the result-truncation path panicked on a bare read, magi exited 2 eight calls into a
	// three-hour task, harbor recorded NonZeroAgentExitCodeError, and the task scored 0 with nothing
	// attempted. One call's bug cost every call that would have followed it.
	//
	// So a crash under a tool becomes that TOOL's failure, not the run's. Two things this must not
	// do. It must not leave the call unanswered — the loop waits on a result, and a call recorded
	// with no result is the same silent nothing that oversized results used to return — so the
	// deferred emit fires only when the normal path did not get that far (resultEmitted). And it
	// must not hide the crash: the agent is handed the panic value verbatim, the top frames go to
	// the run's error stream where the operator sees them, and neither is softened into "the tool
	// failed". A recovered panic is a magi bug that still needs fixing; this only stops it from
	// taking the run's remaining hours with it.
	resultEmitted := false
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		stack := string(debug.Stack())
		a.emitError(ctx, sid, actor, fmt.Sprintf("magi panicked while running tool %q: %v\n%s",
			tc.Name, r, firstFrames(stack, 8)))
		if !resultEmitted {
			a.appendToolResult(ctx, sid, actor, toolMsgID, tc.CallID, fmt.Sprintf(
				"magi crashed while running this call and could not complete it: %v\n\n"+
					"This is a defect in magi, not in what you asked for. The call did not run to "+
					"completion, so nothing here says whether its work happened — check the state "+
					"yourself before assuming either way. A different shape of the same request "+
					"(a narrower range, a smaller batch) may get through.", r), true)
		}
	}()

	// Another harness's name for a tool this one HAS: run it, and say which name is real.
	//
	// Measured across the recorded runs: 134 of 10,580 calls named a tool that does not exist, in
	// 21% of sessions, and the guesses are not confusion about what to do — 91 of them were
	// `todo_write`/`todo`/`todo write` carrying the one argument `todos`, which is `todowrite`
	// spelled the way every other harness spells it. Refusing those cost a round trip each and did
	// not teach: one run made the same `todo_write` call four times over four hours, each answered
	// with the exact name and each followed by the same guess.
	//
	// Resolved HERE, before the gates and the guard, because both key on the name: the allowlist
	// would refuse a name it does not know, and the repeat fingerprint would file two spellings of
	// one call as two different calls. Only nearestToolName's matches are taken — a spelling that
	// normalises to a registered name, or a prefix unique among them — so an ambiguous guess
	// (`recall`, `web`) and one that means nothing here (`run`, 33 calls with mixed arguments) are
	// still refused with the roster. And only to a tool this agent is ALLOWED, so the alias can
	// never route around the allowlist.
	//
	// The tool-call part is already in the log under the model's own spelling, so the record keeps
	// what it actually said; the result says what actually ran.
	aliasedFrom := ""
	if _, known := a.tools.Get(tc.Name); !known {
		var allowed []string
		for _, n := range a.ToolNames() {
			if agent.allows(n) {
				allowed = append(allowed, n)
			}
		}
		if near := nearestToolName(tc.Name, allowed); near != "" {
			if _, ok := a.tools.Get(near); ok {
				aliasedFrom, tc.Name = tc.Name, near
			}
		}
	}

	// The call is RECORDED, not judged. This used to refuse an identical call repeated past a
	// limit, on the theory that a repeat whose outcome cannot change is a loop worth breaking. The
	// theory did not survive being measured: across the recorded runs, the trials magi force-stopped
	// itself produced not one pass, while the trials that ran to the external deadline produced 76.
	// A repeat the model can see in its own context is the model's to break.
	var guardFP string
	var guardNovel bool // this call's first occurrence this epoch (check n==1) — exercise novelty
	if guard != nil {
		_, n, fp := guard.check(tc.Name, tc.Args)
		guardFP = fp
		guardNovel = n == 1
	}

	// Pre-execution gates, run in order — the first that blocks emits its own tool result
	// and stops the call (allowlist → guardrail policy/permission prompt → PreToolUse hooks).
	//
	// Staleness runs FIRST: it is a correctness check, and asking a person to approve an edit magi
	// is about to refuse spends their attention on nothing.
	if a.gateStaleAnchor(ctx, s, actor, tc, guard, toolMsgID) ||
		a.gateIrreversible(ctx, s, actor, tc, guard, toolMsgID) ||
		a.gateAllowlist(ctx, s, agent, depth, actor, tc, toolMsgID) ||
		a.gatePermission(ctx, sid, actor, tc, toolMsgID) ||
		a.gatePreHooks(ctx, s, actor, tc, toolMsgID) {
		return
	}

	// Tool-env callbacks. Route a mid-turn user interjection — top level only, since the user
	// steers the agent they are talking to. The tool has already validated
	// action ∈ {queue,redirect,append}; this records the signal for the loop to drain and apply
	// at its next step.
	spawnFn, childStepsFn, restoreChildFn, mergeChildFn := a.spawnFnFor(depth, s, actor, tc.CallID, tc.Name)
	var expectFn func(port.Elsewhere) error
	var routeInterjectionFn func(action, reason, requestID string) error
	if depth == 0 {
		expectFn = func(e port.Elsewhere) error { return a.Expect(sid, actor, e) }
		routeInterjectionFn = func(action, reason, requestID string) error {
			if !a.hasPendingInterject(sid) {
				return fmt.Errorf("there is no queued interjection to route right now")
			}
			a.signalTurnControl(sid, func(tc *turnControl) {
				tc.route = action
				tc.routeID = requestID
				if reason != "" {
					tc.reason = reason
				}
			})
			return nil
		}
	}
	tool, ok := a.tools.Get(tc.Name)
	if !ok {
		// A bare "unknown tool" reply gets retried verbatim: models carry other harnesses' tool
		// names as priors and, told only that the name is unknown, guess the same name again —
		// observed 4 identical todo_write calls in one run, each a full round trip on a slow model.
		// Name the roster, and when the guess differs from a REGISTERED tool only in separators,
		// say which one it meant: that converts the rejection into a correct next call.
		//
		// The message used to append "there is no todo/plan tool" unconditionally, which was false
		// whenever todowrite was registered — and it was, so the same reply both listed todowrite
		// and denied it existed. Derive everything from the roster instead of asserting it.
		names := a.ToolNames()
		msg := "unknown tool: " + tc.Name + "."
		if near := nearestToolName(tc.Name, names); near != "" {
			msg += " Did you mean `" + near + "`? (that is the exact registered name)"
		}
		msg += " Available tools: " + strings.Join(names, ", ")
		a.appendToolResult(ctx, sid, actor, toolMsgID, tc.CallID, msg, true)
		return
	}
	// An argument the tool does not declare is dropped by encoding/json without a word, so the
	// tool answers a DIFFERENT question than the one asked and returns it in the ordinary shape
	// of an answer — neither the model nor magi can tell. This is the same defect as the unknown
	// tool name handled above, one level down, and it is not hypothetical: across the recorded
	// runs 387 of 25291 calls carried a key the tool never declared, and almost all of them were
	// a real loss (`ignore_case` on a search, a todo list written EMPTY because the key arrived
	// as `.todos`, another harness's `file_path` on a write).
	//
	// The two shapes need different answers, so they are separated by the same normalization the
	// unknown-tool path uses. A key that is a DECLARED key up to case and separators is a
	// misspelling: the model meant to pass it, so running without it silently discards an
	// argument it is relying on — refuse and name the real key. A key that matches nothing is a
	// capability the tool does not have; refusing those would break calls that work today (the
	// single largest group is a harmless `description` on bash), so let the call run and say
	// plainly what was ignored.
	misspelled, ignored, declared := unknownToolArgs(tool.Schema(), tc.Args)
	if len(misspelled) > 0 {
		a.appendToolResult(ctx, sid, actor, toolMsgID, tc.CallID,
			"not run — "+tc.Name+" has no argument "+quoteJoin(argKeys(misspelled))+". "+
				renamesFor(misspelled)+" Nothing was dropped silently: re-issue the call with the "+
				"correct name(s). "+tc.Name+" accepts: "+strings.Join(declared, ", "), true)
		return
	}
	// For a file edit, snapshot the file's content BEFORE the tool runs so the council can
	// be shown the agent's actual before→after change (reconstructed from its own tools).
	var changeBefore, changePath string
	var changeReadable bool
	if guard != nil && fileModifiers[tc.Name] {
		changePath = pathArg(tc.Args)
		if changePath != "" {
			changeBefore, changeReadable = readForChange(workdir, changePath)
		}
		// Journal it for a child, before the call runs. This is the only moment the pre-touch state
		// exists, and a loop that wants to put a failed round back has nothing else to go on.
		if depth > 0 && changePath != "" {
			a.journalFor(sid, workdir).note(relForChange(workdir, changePath),
				changeBefore, changeReadable, pathExists(workdir, changePath))
		}
	}
	// The same snapshot for a bash mutation, whose destination has to be read out of the command
	// text (bashWritePaths) because bash carries no path argument. It gives the bash path the
	// content-level self-revert check that write/edit already have — see the retraction below.
	var bashChanges []bashChange
	if guard != nil && tc.Name == "bash" {
		var ba struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(tc.Args, &ba) == nil {
			// One entry per DESTINATION, not per redirect. A command that writes the same path
			// twice (`printf … > f && printf … >> f`, or a heredoc re-run in one line) produced two
			// entries, and the self-revert check below appended its note once per entry — observed
			// live: "this write left the file byte-for-byte as it already was" printed twice in a
			// single tool result. The second copy says nothing the first did not.
			seen := map[string]bool{}
			for _, p := range bashWritePaths(ba.Command) {
				if seen[p] {
					continue
				}
				seen[p] = true
				before, ok := readForChange(workdir, p)
				// Existence, not just content: an absent path and an empty one both read as "",
				// and the difference between them is the difference between a command that
				// created or deleted something and one that did nothing at all.
				existed := pathExists(workdir, p)
				bashChanges = append(bashChanges, bashChange{
					path: p, before: before, readable: ok, existedBefore: existed})
				// Same journal, for the destinations a bash command names. This is what covers the
				// mutations write/edit never sees — a redirect, an `mv`, an `rm`.
				if depth > 0 {
					a.journalFor(sid, workdir).note(relForChange(workdir, p), before, ok, existed)
				}
			}
		}
	}
	// A move or a delete takes a path AWAY, and bashWritePaths names destinations — so the file
	// that is now missing appears nowhere. Journal those too, or a restore puts back everything the
	// child wrote and none of what it removed.
	if depth > 0 && guard != nil && tc.Name == "bash" {
		var ba struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(tc.Args, &ba) == nil {
			for _, p := range bashMoveSources(ba.Command) {
				before, ok := readForChange(workdir, p)
				a.journalFor(sid, workdir).note(relForChange(workdir, p), before, ok, pathExists(workdir, p))
			}
		}
	}
	// Mark a tool as in flight so the stall watchdog does not kill a child that is
	// legitimately blocked in a long, silent tool (e.g. a multi-minute bash). Held
	// through the rest of executeTool (result append / council eval), which is short.
	a.enterTool(sid)
	defer a.leaveTool(sid)
	scratch := a.scratchFor(sid)
	// An isolated child's sandbox loses the SHARED temp trees and keeps only this scratch: /tmp is
	// world-shared and the per-user temp tree holds every sibling's clone — observed live, a
	// child's `echo … > /tmp/…` sailed through the seatbelt on the strength of the general temp
	// allowance. bash points TMPDIR here already, so tooling inside the child keeps working.
	isolatedTemp := ""
	if a.sandboxOverridden(sid) {
		isolatedTemp = scratch.tmpDir()
	}
	res, err := tool.Execute(ctx, tc.Args, port.ToolEnv{
		SessionID:    sid,
		Workdir:      workdir,
		ScratchLogs:  scratch.logsDir(),
		ScratchTmp:   scratch.tmpDir(),
		Platform:     a.plat,
		EmitProgress: func(text string) { a.emitToolProgress(sid, actor, tc.CallID, tc.Name, text) },
		// Only at depth 0. A child has no Spawn, so a child cannot spawn — recursion is impossible
		// by construction rather than bounded by a depth counter somebody has to remember to check.
		Spawn: spawnFn,
		// Paired with Spawn and scoped to the same tool call: it answers only for children this
		// call started, so a plugin cannot read a session it did not create.
		ChildSteps: childStepsFn,
		// Same scope again: a call can only put back a child it started.
		RestoreChild: restoreChildFn,
		MergeChild:   mergeChildFn,
		Council: func(cctx context.Context, question string, complete bool) (string, error) {
			return a.councilAdvice(cctx, s, guardChanges(guard), guardEpoch(guard), question, complete)
		},
		RouteInterjection: routeInterjectionFn,
		AskUser:           a.askUserFn(ctx, s, depth, tc),
		SetTodos:          func(td []session.Todo) { a.putTodos(ctx, sid, actor, td) },
		SetLabels:         func(ls []string) { a.putLabels(ctx, sid, actor, ls) },
		NoteForTurn:       func(t string) error { return a.noteForTurn(sid, t) },
		Propose: func(c port.Contribution) error {
			if a.cfg.Experience == nil {
				return fmt.Errorf("shared experience not configured")
			}
			// What it was doing when it learned this. The store writes Source as a line at the end
			// of the body and the console draws it, and until now every entry said the same word:
			// "agent". That answers who wrote it, which nobody was asking — the question a person
			// has in front of a rule they did not write is what it came out of.
			//
			// Clipped, because a task can be a paragraph and this is a provenance line, not a copy
			// of the request. Only added when the caller left Source at its default: a contribution
			// that names its own source knows better than this does.
			if c.Source == "agent" && strings.TrimSpace(turnTask) != "" {
				c.Source = "agent · " + text.Clip(strings.Join(strings.Fields(turnTask), " "), 140)
			}
			// A wiki edit that named no source gets this companion's declared name: on a page
			// every companion reads, the editor line answers WHO in the fleet wrote it.
			if c.Source == "" {
				if c.Source = a.cfg.Companion; c.Source == "" {
					c.Source = "agent"
				}
			}
			err := a.cfg.Experience.Propose(ctx, c)
			if err == nil {
				a.mu.Lock()
				st := a.stateLocked(sid)
				st.expPtrQ, st.expPtr = "", "" // a just-saved memory must show in the next pointer
				a.mu.Unlock()
			}
			return err
		},
		LoadSkill: func(name string) (string, bool) { return a.skillBody(s.Workdir, name) },
		Recall: func(query string) (string, error) {
			// Budget/dedupe is applied inside recallContext, keyed on the RESOLVED topic
			// (so two phrasings of one topic don't both spend it, and a miss is free).
			return a.recallContext(ctx, sid, query, guard)
		},
		RecallMemory: func(query string) (string, error) {
			if a.cfg.Experience == nil {
				return "", fmt.Errorf("shared experience not configured")
			}
			mems, skills, err := a.cfg.Experience.Retrieve(ctx, query, agent.Groups)
			if err != nil {
				return "", err
			}
			out := formatExperienceFull(mems, skills)
			// The wiki answers through the same door: one recall reaches both stores, so the
			// model needs no second habit — and a query that names a page title behaves as a
			// page fetch (the search boosts exact titles past everything else).
			if w, ok := a.cfg.Experience.(port.WikiStore); ok {
				if pages, err := w.WikiSearch(ctx, query, 3); err == nil && len(pages) > 0 {
					out = strings.TrimRight(formatWikiPages(pages)+"\n"+out, "\n")
					// A recall IS the usage the forgetting horizon measures: a page that was
					// actually handed to a model earned its place in the index a while longer.
					if t, ok := a.cfg.Experience.(interface{ WikiTouch([]string) }); ok {
						titles := make([]string, 0, len(pages))
						for _, p := range pages {
							titles = append(titles, p.Title)
						}
						t.WikiTouch(titles)
					}
				}
			}
			return out, nil
		},
		WikiAdvise: func(page string) string { return a.wikiNeighborNote(ctx, page) },
		Schedule: func(c port.ScheduleChange) (string, error) {
			return a.EditSchedule(s.Workdir, c)
		},
		// Only at depth 0, and for the same reason Spawn is. A child handing work to a companion
		// would put the answer into the CHILD's conversation, which ends when the child does — so
		// the reply would be written into a transcript nobody reads again.
		Expect: expectFn,
		SearchSessions: func(query, open string) (string, error) {
			if open != "" {
				return a.OpenTurn(ctx, open)
			}
			return a.SearchSessions(ctx, s.Workdir, query)
		},
		// AllowNet ties network egress to the APPROVAL axis: permission=allow means full trust
		// (the headless/bench posture), so the OS sandbox must not hard-block the network — a
		// confined mode still confines the FILESYSTEM, but ssh/curl/apt/pip work. Without this a
		// benchmark run silently loses network the moment a container has bwrap/sandbox-exec, and
		// subagents (same execute path) inherit it too.
		Sandbox: port.SandboxSpec{Mode: a.sandboxModeFor(sid), Workdir: workdir, AllowNet: a.cfg.Permission == "allow",
			ReadOnly: readOnlyPaths(a.cfg), TempDir: isolatedTemp},
	})
	if err != nil {
		resultEmitted = true
		a.appendToolResult(ctx, sid, actor, toolMsgID, tc.CallID, string(capToolResult([]byte(err.Error()))), true)
		return
	}
	res.CallID = tc.CallID
	// Cap a single tool result so one giant output (e.g. reading a 500KB file) can't blow
	// the model's context window past what compaction can recover (compaction can't summarize
	// a result that's still in the recent, un-compacted window). Truncate the raw output here,
	// before diagnostics are appended, so the agent is told to narrow its read/command.
	res.Content = capToolResult(res.Content)
	// Ran under a name the model does not use. Silence here would be a side effect it cannot see:
	// the call worked, so nothing in the result would say the name was wrong, and the next call
	// spells it wrong again. Naming it is the whole reason this is an alias and not a rename.
	if aliasedFrom != "" {
		res.Content = appendToContent(res.Content, "\n\n[note: there is no `"+aliasedFrom+
			"` tool here — this ran `"+tc.Name+"`, which is the exact registered name. Use that one.]")
	}
	// The tool's OWN success, before post-edit diagnostics/hooks below flip IsError: a write
	// that landed but fails gofmt/a hook still changed the file, and the council must see
	// that (broken) change — so change capture keys off this, not the post-diagnostics flag.
	toolOK := !res.IsError

	// An unrecognized argument that is not a misspelling ran anyway (see the dispatch check), so
	// the result answers a narrower question than the model asked. Say which part of the call was
	// not honored, on the same result, so the correction arrives with the evidence it explains.
	if len(ignored) > 0 {
		// Two different things happened and they need two different sentences. When the call
		// SUCCEEDED, the result is real and merely narrower than what was asked for. When it
		// FAILED, there is no narrower result to disclaim — and the old wording said there was.
		//
		// Measured live (extract-elf, 2026-08-01): `write{file_name, content}` came back as
		// "path is required" followed by "the call ran WITHOUT it, so this result does not
		// reflect it". The two halves contradict each other, and the second one reads as a write
		// that happened. Dropping the argument is often WHY the call failed — here file_name was
		// the misspelling that left path missing — so the honest sentence connects them.
		outcome := "the call ran WITHOUT it, so this result does not reflect it"
		if res.IsError {
			outcome = "it was dropped before the call, and what is above is that call FAILING without it"
		}
		res.Content = appendToContent(res.Content, "\n\n[ignored arguments] "+tc.Name+" does not take "+
			quoteJoin(ignored)+" — "+outcome+". "+
			tc.Name+" accepts: "+strings.Join(declared, ", "))
	}

	// Post-edit diagnostics + PostToolUse hooks: feed problems back so the agent
	// self-corrects (built-in autoformat runs here too).
	if !res.IsError && fileModifiers[tc.Name] {
		path := pathArg(tc.Args)
		// IsError, because that is what makes the agent stop and read it rather than move on. And
		// Advisory beside it, because the edit HAPPENED: without the second flag every screen drew
		// a file that was written and then linted as a write that failed — the file on disk, the
		// model carrying on, and both windows saying ✗.
		if h := a.runPostToolHooks(ctx, workdir, tc.Name, path); h != "" {
			res.Content = appendToContent(res.Content, "\n\n"+h)
			res.IsError, res.Advisory = true, true
		}
		diag, advice := a.diagnose(ctx, workdir, path)
		if diag != "" {
			res.Content = appendToContent(res.Content, "\n\n[diagnostics]\n"+diag)
			res.IsError, res.Advisory = true, true
		}
		// A missing language server is an environment note, not an edit failure: the
		// edit succeeded, so attach the install hint without flipping IsError.
		if advice != "" {
			res.Content = appendToContent(res.Content, "\n\n[lsp] "+advice)
		}
	}

	// Everything the loop guard has to record about this finished call — one concept, one
	// place (see noteToolOutcome).
	a.noteToolOutcome(sid, guard, toolOutcome{
		tc: tc, res: &res, workdir: workdir, fp: guardFP, novel: guardNovel, toolOK: toolOK,
		changePath: changePath, changeBefore: changeBefore, bashChanges: bashChanges,
	})

	// This call is no longer the thing being waited on, so its last progress note stops being true
	// here — before the result lands, so nobody reads a finished call's heartbeat off a
	// transcript that already shows its answer.
	a.clearDoing(sid, tc.CallID)

	resultEmitted = true
	a.appendPart(ctx, sid, actor, toolMsgID, session.RoleTool, session.Part{
		ID: "p_" + newID(), Kind: session.PartToolResult, ToolResult: &res,
	})
}

// nearestToolName returns the registered tool a called name differs from only in SEPARATORS or
// case — `todo_write` for `todowrite`, `bashOutput` for `bash_output`. Models carry other
// harnesses' spellings as priors, and a rejection that only says "unknown" gets the same spelling
// back; naming the exact registered form ends that loop in one round trip.
//
// Deliberately NOT fuzzy: only a separator/case difference counts. A guess like `run` for `bash`
// would put a tool the model never asked for into its mouth, and a wrong suggestion costs more
// than none — the full roster is always listed anyway.
// unknownToolArgs compares the argument keys a model actually sent against the ones the tool
// declares, splitting the strays in two: `misspelled` maps a sent key to the declared key it is a
// case/separator variant of (the model meant that argument), and `ignored` lists the rest (the
// tool has no such capability). `declared` is the accepted set, for the message.
//
// A schema without a usable `properties` object yields nothing: the check must never invent a
// complaint about a tool whose argument surface it could not read.
func unknownToolArgs(schema, args json.RawMessage) (misspelled map[string]string, ignored, declared []string) {
	var sch struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if json.Unmarshal(schema, &sch) != nil || len(sch.Properties) == 0 {
		return nil, nil, nil
	}
	var sent map[string]json.RawMessage
	if json.Unmarshal(args, &sent) != nil || len(sent) == 0 {
		return nil, nil, nil
	}
	byNorm := make(map[string]string, len(sch.Properties))
	for p := range sch.Properties {
		declared = append(declared, p)
		byNorm[normArgKey(p)] = p
	}
	sort.Strings(declared)
	// A REQUIRED key the call did not send is what makes an undeclared key readable as a rename
	// rather than as an extra: the tool cannot run without it, so the call is already lost.
	var missing []string
	for _, r := range sch.Required {
		if _, sentIt := sent[r]; !sentIt {
			missing = append(missing, r)
		}
	}
	for k := range sent {
		if _, ok := sch.Properties[k]; ok {
			continue
		}
		if real, ok := byNorm[normArgKey(k)]; ok {
			if misspelled == nil {
				misspelled = map[string]string{}
			}
			misspelled[k] = real
			continue
		}
		if real := qualifiedName(k, missing); real != "" {
			if misspelled == nil {
				misspelled = map[string]string{}
			}
			misspelled[k] = real
			continue
		}
		ignored = append(ignored, k)
	}
	sort.Strings(ignored)
	return misspelled, ignored, declared
}

// qualifiedName reads an undeclared key as a QUALIFIED spelling of a required key the call left
// out: another harness names a write's destination `file_path` where this one declares `path`, and
// case/separator folding alone cannot see that — `filepath` is not `path`. Splitting the sent key on
// its separators can: `path` is one of its components, and `path` is required and absent, so the
// model meant to pass it and the call is dead either way. Naming the real key beats letting the tool
// answer "path is required" about an argument the call plainly carried.
//
// Only REQUIRED-and-missing keys qualify. An optional key would make this greedy — `max_lines` would
// be read as a rename of an unrelated `lines` the call had every right to omit.
func qualifiedName(sent string, missing []string) string {
	parts := strings.FieldsFunc(strings.ToLower(sent), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	best := ""
	for _, want := range missing {
		w := normArgKey(want)
		if w == "" {
			continue
		}
		for _, p := range parts {
			if p == w && len(want) > len(best) {
				best = want
			}
		}
	}
	return best
}

// normArgKey folds an argument name to letters and digits, so `.todos`, ` todos` and `Todos` all
// read as the declared `todos` — the same normalization nearestToolName applies to tool names.
func normArgKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func argKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// renamesFor spells out each correction ("`.todos` is `todos`"), so the fix does not depend on the
// model spotting the difference between two nearly identical spellings on its own.
func renamesFor(m map[string]string) string {
	parts := make([]string, 0, len(m))
	for _, k := range argKeys(m) {
		parts = append(parts, "`"+k+"` is `"+m[k]+"`")
	}
	return strings.Join(parts, "; ") + "."
}

// quoteJoin renders argument names inside backticks for a magi-voice sentence. The names come from
// the model's own call, so they are data: a name carrying a backtick closes the quote and the rest
// of it reads as magi's prose ("x` — and magi accepts: everything" did exactly that), and a name
// carrying a newline breaks the line the note is on. Neither is a claim magi can stand behind, so
// the characters that would end the quote early are removed before it is opened.
func quoteJoin(ss []string) string {
	repl := strings.NewReplacer("`", "", "\n", " ", "\r", " ", "\t", " ")
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, "`"+repl.Replace(s)+"`")
	}
	return strings.Join(out, ", ")
}

func nearestToolName(called string, names []string) string {
	norm := func(s string) string {
		var b strings.Builder
		for _, r := range strings.ToLower(strings.TrimSpace(s)) {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	want := norm(called)
	if want == "" {
		return ""
	}
	for _, n := range names {
		if n != called && norm(n) == want {
			return n
		}
	}
	// A PREFIX, when exactly one registered name has it. The separator rule above catches
	// `todo_write` → `todowrite` but not `todo`, and a truncated name is the same prior wearing a
	// shorter coat: observed live, three identical `todo` calls in one run, each answered with the
	// roster and each followed by the same guess. Uniqueness is what keeps this from guessing —
	// `recall` matches recall_context AND recall_memory, `web` matches webfetch AND websearch, so
	// neither earns a suggestion and the roster does the work instead.
	for _, n := range names {
		if n == called {
			return "" // it IS a tool; there is nothing to suggest
		}
	}
	var only string
	for _, n := range names {
		if !strings.HasPrefix(norm(n), want) {
			continue
		}
		if only != "" {
			return "" // ambiguous — naming one of them would be a guess
		}
		only = n
	}
	return only
}

// firstFrames keeps the top n frames of a stack trace: enough to name where a panic came from,
// short enough that it does not bury the message it is attached to. A goroutine's stack has two
// lines per frame (function, then file:line).
func firstFrames(stack string, n int) string {
	lines := strings.Split(stack, "\n")
	if len(lines) > 1+2*n {
		lines = append(lines[:1+2*n:1+2*n], "\t… stack truncated")
	}
	return strings.Join(lines, "\n")
}
