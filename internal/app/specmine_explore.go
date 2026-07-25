package app

import (
	"context"
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
	note := "# Repository findings (from a read-only exploration of the plan) — the existing signatures/paths/" +
		"interfaces the steps should match. Reuse a FIXED identifier or path from here verbatim (do not invent " +
		"an alternative name); but a DERIVED value below (a record/field byte size, a file's sample contents) " +
		"is a read-only pass's reading, not ground truth — if your own read of the file disagrees, TRUST THE " +
		"FILE, and confirm sizes/offsets against the real bytes before you depend on them:\n" + findings
	// Fold into the mined contract the termination council reads (cachedSpecMine), and inject into the
	// session so the plan-audit check-author and the executor read it too.
	if prev := strings.TrimSpace(a.cachedSpecMine(s.ID)); prev != "" {
		a.storeSpecMine(s.ID, prev+"\n\n"+note)
	} else {
		a.storeSpecMine(s.ID, note)
	}
	_ = a.appendPromptText(ctx, s.ID, event.Actor{Kind: event.ActorSystem, ID: "specmine"}, note)
}
