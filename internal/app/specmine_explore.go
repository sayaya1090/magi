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
	"the plan or the task. If the repository has nothing relevant (e.g. a greenfield task with an empty " +
	"tree), say so in one line."

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
		"\n\n# Repository (top level)\n" + repoMap(s.Workdir) +
		"\n\nExplore read-only and report the concrete EXISTING facts (paths, signatures, interfaces) the steps must honor."
	a.emitToolProgress(s.ID, plannerActor, "", "specmine", "exploring the repo for the plan's real signatures/paths…")
	r := a.spawnResolved(ctx, s, depth, spec, port.SpawnRequest{Agent: "specmine", Prompt: brief})
	findings := strings.TrimSpace(stripReportStatus(r.Text))
	if r.Err != "" || findings == "" {
		return
	}
	note := "# Repository findings (read-only exploration of the plan) — the REAL, existing signatures/paths/" +
		"interfaces the steps must match; use these verbatim, do not invent alternatives:\n" + findings
	// Fold into the mined contract the termination council reads (cachedSpecMine), and inject into the
	// session so the plan-audit check-author and the executor read it too.
	if prev := strings.TrimSpace(a.cachedSpecMine(s.ID)); prev != "" {
		a.storeSpecMine(s.ID, prev+"\n\n"+note)
	} else {
		a.storeSpecMine(s.ID, note)
	}
	_ = a.appendPromptText(ctx, s.ID, event.Actor{Kind: event.ActorSystem, ID: "specmine"}, note)
}
