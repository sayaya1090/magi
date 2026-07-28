package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/artifact"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// rawTruthy reads a boolean tool argument straight off the wire, tolerating the shapes weak
// models actually send for one (`true`, `"true"`, `1`) instead of failing the whole decode on
// a quoted bool — the same tolerance flexBool gives the tool itself (see [[tool-arg-tolerance]]).
// Absent or unrecognized is false.
func rawTruthy(raw json.RawMessage) bool {
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	switch strings.ToLower(s) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

// exerciseConverged reports whether a bash call that came back exit 0 is real evidence that the
// build/test it ran converged — i.e. whether that exit code is the command's OWN. It is false when
// the command text proves the exit belongs to something else (a `|| true` mask, a `| tail` pipe, a
// `;`-sequenced `echo`/`tail` tail). Only a true here clears the command's check-churn count; a
// masked exit leaves the count alone rather than climbing it, because the command text proves the
// exit says nothing — not that it failed.
func exerciseConverged(command string) bool {
	return !builtin.ExitCodeMasked(command)
}

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
	if (a.cfg.DangerTools[tc.Name] || forcePrompt) && !a.policy.AllowedByRule(tc.Name, tc.Args) {
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

// gatePreHooks runs PreToolUse hooks, which can block execution (e.g. protect paths).
// Returns true to stop.
func (a *App) gatePreHooks(ctx context.Context, s session.Session, actor event.Actor, tc *session.ToolCall, toolMsgID string) bool {
	if block := a.runPreToolHooks(ctx, s.Workdir, tc.Name, pathArg(tc.Args)); block != "" {
		a.appendToolResult(ctx, s.ID, actor, toolMsgID, tc.CallID, "blocked by hook: "+block, true)
		return true
	}
	return false
}

// executeTool runs one tool call (with permission gating) and persists the result.
func (a *App) executeTool(ctx context.Context, s session.Session, agent AgentSpec, depth int, actor event.Actor, tc *session.ToolCall, guard *runGuard) {
	sid := s.ID
	workdir := s.Workdir
	toolMsgID := "m_" + newID()

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
	if a.gateAllowlist(ctx, s, agent, depth, actor, tc, toolMsgID) ||
		a.gatePermission(ctx, sid, actor, tc, toolMsgID) ||
		a.gatePreHooks(ctx, s, actor, tc, toolMsgID) {
		return
	}

	// Tool-env callbacks. A concern may be retired only by the agent that holds the whole-task
	// view, and only advisorily: a still-true concern is re-raised next turn (self-healing), so
	// this cannot launder a fact away, only clear stale advisory memory.
	var resolveConcernFn func(key, reason string) error
	if depth == 0 {
		resolveConcernFn = func(key, reason string) error {
			return a.appendConcernResolved(context.WithoutCancel(ctx), sid,
				event.Actor{Kind: event.ActorAgent, ID: "orchestrator"}, key, "orchestrator", reason)
		}
	}
	// Route a mid-turn user interjection (top-level only — subagents aren't steered by
	// the user). The tool has already validated action ∈ {queue,redirect,append}; we
	// record the signal for the loop to drain and apply at its next step.
	var routeInterjectionFn func(action, reason, requestID string) error
	if depth == 0 {
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
	// Any agent may declare its current approach unworkable. It used to be offered only to a
	// "plan-eligible" agent, because honoring it re-ran the pre-flight planner; now it resets the
	// turn's no-progress accounting and tells the agent to start over, which is something any
	// agent that can act needs — and the sterile-replan counter still watches for a run that keeps
	// declaring itself stuck without ever finishing a step.
	replanFn := func(reason string) error {
		a.signalTurnControl(sid, func(tc *turnControl) {
			tc.replan = true
			if reason != "" {
				tc.reason = reason
			}
		})
		return nil
	}
	st, _ := json.Marshal(event.ToolStartedData{CallID: tc.CallID, Name: tc.Name})
	a.publishTransient(sid, event.TypeToolStarted, actor, st)

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
	if guard != nil && fileModifiers[tc.Name] {
		changePath = pathArg(tc.Args)
		if changePath != "" {
			changeBefore = readForChange(workdir, changePath)
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
			for _, p := range bashWritePaths(ba.Command) {
				bashChanges = append(bashChanges, bashChange{p, readForChange(workdir, p)})
			}
		}
	}
	// Mark a tool as in flight so the stall watchdog does not kill a child that is
	// legitimately blocked in a long, silent tool (e.g. a multi-minute bash). Held
	// through the rest of executeTool (result append / council eval), which is short.
	a.enterTool(sid)
	defer a.leaveTool(sid)
	scratch := a.scratchFor(sid)
	res, err := tool.Execute(ctx, tc.Args, port.ToolEnv{
		SessionID:    sid,
		Workdir:      workdir,
		ScratchLogs:  scratch.logsDir(),
		ScratchTmp:   scratch.tmpDir(),
		Platform:     a.plat,
		EmitArtifact: func(art artifact.Artifact) { a.emitArtifact(ctx, sid, actor, art) },
		EmitProgress: func(text string) { a.emitToolProgress(sid, actor, tc.CallID, tc.Name, text) },
		Council: func(cctx context.Context, question string, complete bool) (string, error) {
			return a.councilAdvice(cctx, s, guardChanges(guard), question, complete)
		},
		ResolveConcern:    resolveConcernFn,
		RouteInterjection: routeInterjectionFn,
		Replan:            replanFn,
		AskUser:           a.askUserFn(ctx, s, depth, tc),
		SetTodos:          func(td []session.Todo) { a.putTodos(ctx, sid, actor, td) },
		Propose: func(c port.Contribution) error {
			if a.cfg.Experience == nil {
				return fmt.Errorf("shared experience not configured")
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
			mems, skills, err := a.cfg.Experience.Retrieve(ctx, query)
			if err != nil {
				return "", err
			}
			return formatExperienceFull(mems, skills), nil
		},
		// AllowNet ties network egress to the APPROVAL axis: permission=allow means full trust
		// (the headless/bench posture), so the OS sandbox must not hard-block the network — a
		// confined mode still confines the FILESYSTEM, but ssh/curl/apt/pip work. Without this a
		// benchmark run silently loses network the moment a container has bwrap/sandbox-exec, and
		// subagents (same execute path) inherit it too.
		Sandbox: port.SandboxSpec{Mode: a.cfg.Sandbox, Workdir: workdir, AllowNet: a.cfg.Permission == "allow"},
	})
	if err != nil {
		a.appendToolResult(ctx, sid, actor, toolMsgID, tc.CallID, string(capToolResult([]byte(err.Error()))), true)
		return
	}
	res.CallID = tc.CallID
	// Cap a single tool result so one giant output (e.g. reading a 500KB file) can't blow
	// the model's context window past what compaction can recover (compaction can't summarize
	// a result that's still in the recent, un-compacted window). Truncate the raw output here,
	// before diagnostics are appended, so the agent is told to narrow its read/command.
	res.Content = capToolResult(res.Content)
	// The tool's OWN success, before post-edit diagnostics/hooks below flip IsError: a write
	// that landed but fails gofmt/a hook still changed the file, and the council must see
	// that (broken) change — so change capture keys off this, not the post-diagnostics flag.
	toolOK := !res.IsError

	// An unrecognized argument that is not a misspelling ran anyway (see the dispatch check), so
	// the result answers a narrower question than the model asked. Say which part of the call was
	// not honored, on the same result, so the correction arrives with the evidence it explains.
	if len(ignored) > 0 {
		res.Content = appendToContent(res.Content, "\n\n[ignored arguments] "+tc.Name+" does not take "+
			quoteJoin(ignored)+" — the call ran WITHOUT it, so this result does not reflect it. "+
			tc.Name+" accepts: "+strings.Join(declared, ", "))
	}

	// Post-edit diagnostics + PostToolUse hooks: feed problems back so the agent
	// self-corrects (built-in autoformat runs here too).
	if !res.IsError && fileModifiers[tc.Name] {
		path := pathArg(tc.Args)
		if h := a.runPostToolHooks(ctx, workdir, tc.Name, path); h != "" {
			res.Content = appendToContent(res.Content, "\n\n"+h)
			res.IsError = true
		}
		diag, advice := a.diagnose(ctx, workdir, path)
		if diag != "" {
			res.Content = appendToContent(res.Content, "\n\n[diagnostics]\n"+diag)
			res.IsError = true
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

func quoteJoin(ss []string) string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, "`"+s+"`")
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
	return ""
}
