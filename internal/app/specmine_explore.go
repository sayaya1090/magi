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

// specMineExploreTools is the allowlist that makes the exploration read-only. Enforcement is the
// vocabulary itself: no write, edit, or bash is reachable, so mutation is not something the child
// can express rather than something it is asked not to do.
var specMineExploreTools = []string{"read", "grep", "glob", "list", "findcontext"}

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

// specMineBrief builds the exploration's prompt: whose request this is, whose plan, the repo map,
// and — last, where the instruction the model acts on belongs — this agent's own job.
//
// Whose task is it? The plan block always said "do NOT carry it out"; the request above it was
// headed "── TASK" and left to speak for itself. A request is written in imperatives ("make X pass",
// "implement Y"), so under that header it read as this agent's own assignment, and the system
// prompt's "do not propose changes" — seven lines into a paragraph — loses to a heading. So the
// request is labelled for what it is: someone else's, quoted only so this agent knows what to look
// FOR. The per-item explorers already draw that line ("Overall goal (context for your
// investigation)" then "INVESTIGATE (read-only) — …", explorerPrompt); this one did not, and it is
// the one handed the raw request. The job names its deliverable in terms the allowlist can reach,
// so "done" is something this agent can actually be.
func specMineBrief(task, plan, repo string) string {
	return "── THE REQUEST (context only — SOMEONE ELSE will carry this out, not you; it is quoted so you " +
		"know what to look FOR)\n" + strings.TrimSpace(task) +
		"\n\n── THEIR PLAN (do NOT carry it out — just find what it will build on)\n" + plan +
		"\n\n# Repository (top level)\n" + repo +
		"\n\n── YOUR JOB (the only thing you produce)\nExplore read-only and report the concrete EXISTING facts " +
		"(paths, signatures, interfaces) the steps must honor, as `path — fact` lines. Finding and naming those " +
		"facts IS your deliverable — you are done when you have them. Not a design, not a fix, not an opinion on " +
		"whether the plan is any good, and nothing about how the request should be carried out."
}

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
	// specMineExploreTools is a named set because a misspelling here is silent: an allowlist entry no
	// tool answers to grants nothing and reads exactly like one that grants something.
	spec := AgentSpec{
		Name:     "specmine",
		System:   specMineExploreSystem,
		Tools:    specMineExploreTools,
		Model:    base.Model,
		Provider: base.Provider,
	}
	brief := specMineBrief(task, renderSteps(steps), repo)
	a.emitToolProgress(s.ID, plannerActor, "", "specmine", "exploring the repo for the plan's real signatures/paths…")
	r := a.spawnResolved(ctx, s, depth, spec, port.SpawnRequest{Agent: "specmine", Prompt: brief})
	findings := strings.TrimSpace(stripReportStatus(r.Text))
	// An exploration that ERRORED — every attempt stalled, timed out, or had its lease judged KILL —
	// comes back with no findings, and that has always meant "inject nothing". But it is a STOPPED
	// exploration too, and the searches it ran are salvageable for the same reason the guard-stop
	// path below salvages them: they are magi's own record, from each call's arguments and result,
	// with no model prose in the path. The two kinds of stop got opposite treatment only because a
	// guard stop deliberately leaves Err empty while an expired lease sets it, so the salvage sat
	// behind a door this case never opens. Observed: a plan built on an invented identifier sent
	// three explorer attempts after it, each one establishing with its own greps that the name is
	// nowhere in the tree, all three killed on the lease — and the planner was handed a bare error.
	if r.Err != "" {
		a.salvageSearches(ctx, s, depth, spec, steps, r.SessionID,
			"the exploration never reported ("+clipLine(r.Err, 160)+")")
		return
	}
	if findings == "" {
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
		a.salvageSearches(ctx, s, depth, spec, steps, r.SessionID,
			fmt.Sprintf("exploration was stopped by the %s after %d chars — discarding the partial "+
				"findings rather than passing a mid-analysis fragment off as repository facts", why, len(findings)))
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

// salvageSearches injects what an exploration that never reported still established: the patterns
// it searched for and did not find, read out of magi's own record of the calls rather than out of
// the model's prose. lead names WHY there is no report, and is rendered ahead of the count.
//
// Shared by both ways an exploration can end without one — a guard force-stop and an errored spawn
// (a stall, a hard timeout, or a lease the judge ruled KILL). Keeping it in one place is the point:
// the salvage was written for the first and reachable only from it, which is how the second kind of
// stop came to throw away the identical evidence.
func (a *App) salvageSearches(ctx context.Context, s session.Session, depth int, spec AgentSpec,
	steps []planStep, sid session.SessionID, lead string) {
	neg := a.searchedNotFound(ctx, sid)
	a.emitToolProgress(s.ID, plannerActor, "", "specmine",
		fmt.Sprintf("spec-mine: %s; keeping %d searched-and-not-found fact(s) from magi's own record of its searches",
			lead, len(neg)))
	// The absence and the plan that uses the name anyway both end up in the planner's window, and
	// leaving them there is what produced the run this was written for. Settle them here instead
	// (specmine_confirm.go); a retracted absence is dropped BEFORE rendering, so the injection never
	// asserts and corrects the same name.
	conf, retracted := a.confirmContradictions(ctx, s, depth, spec, planText(steps), neg)
	if note := renderSearchMisses(dropRetracted(neg, retracted)); note != "" || conf != "" {
		a.injectSpecMineNote(ctx, s.ID, strings.TrimSpace(note+"\n\n"+conf))
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
