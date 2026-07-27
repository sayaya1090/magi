package app

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// specMineExploreEnabled gates the plan-based repository exploration (MAGI_SPECMINE_EXPLORE, default
// ON). Off restores the prompt-analysis-only flow, with no read-only exploration subagent.
func specMineExploreEnabled() bool { return !envOff("MAGI_SPECMINE_EXPLORE") }

// specMineExploreSystem instructs the read-only exploration subagent: BEFORE any code is written,
// ground the plan in what the repository ACTUALLY contains.
const specMineExploreSystem = "You are a READ-ONLY repository explorer running before any code is " +
	"written. Given a task and its plan, use your read tools (read/grep/glob/list) to find the REAL, " +
	"EXISTING things the plan will build on or must match — exact file paths, function/type signatures, " +
	"existing interfaces and schemas (proto/message/struct/class definitions), config keys, and the " +
	"dependency versions already present. Report CONCRETE FACTS the later steps must honor verbatim, so " +
	"they use the real thing instead of guessing a name or shape. Be terse — a short list of " +
	"`path — fact` lines, nothing else. Do NOT propose changes, do NOT write anything, do NOT restate " +
	"the plan or the task. ACCURACY over volume — a WRONG fact is worse than a missing one, because the " +
	"executor is told to reuse these verbatim. So report ONLY what you actually READ: never guess or infer " +
	"a value you did not see (a byte offset, a record length, a field width, a file's contents). When you " +
	"state a size or layout, DERIVE it from the real bytes — e.g. read the file and use its actual length " +
	"and per-record split, do not do mental arithmetic that can be off; and if you break a record into " +
	"fields, the field sizes MUST sum to the record length you measured (if they don't, you miscounted — " +
	"re-read). Never reproduce a file's CONTENT you did not open (no invented sample rows / ids / values). " +
	"If you are unsure of a value, say it is unverified rather than assert it. If the repository has nothing " +
	"relevant (e.g. a greenfield task with an empty tree), say so in one line."

// exploreSpecMine spawns a SYNCHRONOUS read-only subagent that grounds the plan in the real repository —
// mining the actual signatures, paths, and interfaces the steps must match — and injects its findings as
// a note the plan-audit check-author, the executor, and the termination council all read. This is the
// plan-BASED half of spec mining (the prompt-analysis half runs earlier, on the request alone). It is
// handed the task + the plan + the repo map, NOT the raw session conversation: the plan is already the
// distilled intent, and raw history dilutes a weak model. Best-effort and top-level only (depth 0,
// non-workflow): a disabled flag, an empty plan, or a failed/empty spawn injects nothing.
func (a *App) exploreSpecMine(ctx context.Context, s session.Session, task string, steps []planStep, depth int) {
	if !specMineExploreEnabled() || depth != 0 || a.cfg.Workflow || len(steps) == 0 {
		return
	}
	// A greenfield task starts with an empty (or unreadable) workspace — there is nothing to explore, so
	// spawning a read-only subagent to discover "the repository is empty" is pure overhead (a full LLM
	// round-trip + a spawn). Skip it; the prompt-analysis half already grounded the request. Observed on
	// the empty-repo bench tasks (kv-store-grpc et al.), which are greenfield.
	repo := strings.TrimSpace(repoMap(s.Workdir))
	if repo == "" || repo == "(unavailable)" {
		return
	}
	base := a.agentFor(s)
	// A read-only spec built on the fly (no config dependency, like the recovery lifeline): the tool
	// allowlist alone makes it read-only — no write/edit/bash — so it cannot mutate the workspace.
	spec := AgentSpec{
		Name:     "specmine",
		System:   specMineExploreSystem,
		Tools:    []string{"read", "grep", "glob", "list", "findcontext"},
		Model:    base.Model,
		Provider: base.Provider,
	}
	brief := "── TASK\n" + strings.TrimSpace(task) +
		"\n\n── PLAN (do NOT carry it out — just find what it will build on)\n" + renderSteps(steps) +
		"\n\n# Repository (top level)\n" + repo +
		"\n\nExplore read-only and report the concrete EXISTING facts (paths, signatures, interfaces) the steps must honor."
	a.emitToolProgress(s.ID, plannerActor, "", "specmine", "exploring the repo for the plan's real signatures/paths…")
	r := a.spawnResolved(ctx, s, depth, spec, port.SpawnRequest{Agent: "specmine", Prompt: brief})
	findings := strings.TrimSpace(stripReportStatus(r.Text))
	if r.Err != "" || findings == "" {
		return
	}
	// An exploration a guard STOPPED did not finish mining: r.Text is then whatever the model happened
	// to be saying when it was cut off, mid-analysis. Promoting that to "Repository findings" is worse
	// than injecting nothing, because the note's own header tells every later reader to reuse what it
	// contains VERBATIM. Observed: a guard-stopped explorer's last words were a fix proposal for a file
	// it had already moved on from; the note carried no path at all, and the plan and all six of its
	// checks went to the wrong file — none of them could ever pass. Drop it and let the planner ground
	// itself with its own tools, which is strictly what it does when mining is off.
	//
	// What the model was SAYING is unusable; what it SEARCHED FOR is not. A grep that matched nothing
	// is a fact magi recorded itself, from the call's own arguments and result — no prose passes
	// through it, so a mid-analysis hallucination cannot ride along. And the direction is fail-safe:
	// a negative can only stop a later step from building on a name that is not there, never make it
	// adopt a wrong one. Observed in the run that motivated this: the explorer had already
	// established "<identifier> is not found with grep", the guard stop threw that away with
	// everything else, and the plan was then built on that identifier through three revisions —
	// every check with it, none of them able to pass.
	if why := a.spawnStoppedBy(ctx, r.SessionID); why != "" {
		neg := a.searchedNotFound(ctx, r.SessionID)
		a.emitToolProgress(s.ID, plannerActor, "", "specmine",
			fmt.Sprintf("spec-mine: exploration was stopped by the %s after %d chars — discarding the partial "+
				"findings rather than passing a mid-analysis fragment off as repository facts; keeping %d "+
				"searched-and-not-found fact(s) from magi's own record of its searches", why, len(findings), len(neg)))
		// The absence and the plan that uses the name anyway both end up in the planner's window, and
		// leaving them there is what produced the run this was written for. Settle them here instead
		// (specmine_confirm.go); a retracted absence is dropped BEFORE rendering, so the injection never
		// asserts and corrects the same name.
		conf, retracted := a.confirmContradictions(ctx, s, depth, spec, planText(steps), neg)
		if note := renderSearchMisses(dropRetracted(neg, retracted)); note != "" || conf != "" {
			a.injectSpecMineNote(ctx, s.ID, strings.TrimSpace(note+"\n\n"+conf))
		}
		return
	}
	// The explorer's whole contract is `path — fact` lines: findings that name no file are, for this
	// pass, empty. Re-ask ONCE naming the defect (the same shape as the plan re-ask and the coverage
	// re-ask) rather than letting the planner invent a path — which is exactly what it does, since the
	// note it is handed reads as authoritative. If the retry names none either, keep what we have: a
	// bounded miss beats an unbounded loop.
	exploreSID := r.SessionID // whose search record the contradiction check reads, below
	if !mentionsFilePath(findings) {
		a.emitToolProgress(s.ID, plannerActor, "", "specmine",
			fmt.Sprintf("spec-mine: the exploration named no file path in %d chars — re-asking once for "+
				"`path — fact` lines", len(findings)))
		r2 := a.spawnResolved(ctx, s, depth, spec, port.SpawnRequest{Agent: "specmine",
			Prompt: brief + "\n\n" + specMineNoPathReminder})
		if f2 := strings.TrimSpace(stripReportStatus(r2.Text)); r2.Err == "" && f2 != "" &&
			a.spawnStoppedBy(ctx, r2.SessionID) == "" && mentionsFilePath(f2) {
			findings, exploreSID = f2, r2.SessionID
		} else {
			a.emitToolProgress(s.ID, plannerActor, "", "specmine",
				"spec-mine: the re-ask named no path either — keeping the first findings as-is")
		}
	}
	a.injectSpecMineNote(ctx, s.ID, "# Repository findings (from a read-only exploration of the plan) — the existing signatures/paths/"+
		"interfaces the steps should match. Reuse a FIXED identifier or path from here verbatim (do not invent "+
		"an alternative name); but a DERIVED value below (a record/field byte size, a file's sample contents) "+
		"is a read-only pass's reading, not ground truth — if your own read of the file disagrees, TRUST THE "+
		"FILE, and confirm sizes/offsets against the real bytes before you depend on them:\n"+findings)
	// A finished exploration reports what it FOUND; it is under no obligation to mention that the plan
	// leans on something it searched for and did not find. That contradiction is the same one the
	// stopped path salvages, so it is settled here too — only the wholesale absence list stays
	// exclusive to the stopped path, where the prose it would have replaced is gone.
	if conf, _ := a.confirmContradictions(ctx, s, depth, spec, planText(steps),
		a.searchedNotFound(ctx, exploreSID)); conf != "" {
		a.injectSpecMineNote(ctx, s.ID, conf)
	}
}

// injectSpecMineNote folds a note into the mined contract the termination council reads
// (cachedSpecMine) and appends it to the session, so the plan-audit check-author and the executor
// read it too.
func (a *App) injectSpecMineNote(ctx context.Context, sid session.SessionID, note string) {
	if strings.TrimSpace(note) == "" {
		return
	}
	if prev := strings.TrimSpace(a.cachedSpecMine(sid)); prev != "" {
		a.storeSpecMine(sid, prev+"\n\n"+note)
	} else {
		a.storeSpecMine(sid, note)
	}
	_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "specmine"}, note)
}

// specMineNoPathReminder is appended to the explorer's brief after a reply that named no file. It
// states the consequence, which the model cannot know: without a real path the planner writes one
// from memory, and every check built on it asserts against a file that is not there.
const specMineNoPathReminder = "YOUR PREVIOUS REPLY NAMED NO FILE. Findings without a path cannot be reused: the " +
	"steps that read this note will write a plausible-looking path from memory, and every check built on it then " +
	"asserts against a file that does not exist and can never pass. Reply with `path — fact` lines ONLY, each path " +
	"one you actually opened, written the way it appears in the repository (the directories included, not the bare " +
	"file name). Report where things ARE; do not propose, draft, or explain a change."

// spawnStoppedBy names the guard that force-stopped a child session ("loop guard", "stall guard",
// "spin guard"), or "" when the child ended on its own. A guard stop leaves an error event in the
// child's log but does NOT set SpawnResult.Err — the partial text is returned like any other result —
// so a caller that treats the reply as a finished answer cannot otherwise tell the two apart.
// Best-effort: an unreadable session reads as a normal ending, keeping today's behavior.
func (a *App) spawnStoppedBy(ctx context.Context, sid session.SessionID) string {
	if sid == "" {
		return ""
	}
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return ""
	}
	for _, e := range evs {
		if e.Type != event.TypeError {
			continue
		}
		var d event.ErrorData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		switch d.Code {
		case "loop_guard":
			return "loop guard"
		case "stall_guard":
			return "stall guard"
		case "spin_guard":
			return "spin guard"
		}
	}
	return ""
}

// filePathRe matches a token that reads as a file: an optional directory prefix, then a name with a
// letter-initial extension. Requiring the extension keeps prose out — "e.g." and a version like
// "1.73.0" both fail it, the first on length and the second because a digit cannot open an extension.
// It is a trigger for ONE re-ask, not a gate, so a miss costs one generation and a false hit costs
// nothing.
var filePathRe = regexp.MustCompile(`[A-Za-z0-9_./-]*[A-Za-z0-9_-]\.[A-Za-z][A-Za-z0-9]{0,7}\b`)

// mentionsFilePath reports whether text names at least one file. Used to tell a repository
// exploration that reported facts from one that reported none.
func mentionsFilePath(text string) bool {
	for _, m := range filePathRe.FindAllString(text, -1) {
		if len(m) >= 5 {
			return true
		}
	}
	return false
}
