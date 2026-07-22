package app

import (
	"fmt"
	"strings"
)

// Prompt/instruction builders for the planner: the planner contract and its optional
// rules, plus the per-step hand-off prompts (delegate/refine/redecompose/explorer). Pure
// string construction, split out of planner.go for cohesion (behavior unchanged).

// plannerContract instructs the planner to emit an ordered procedure with a
// per-step execution strategy, not a solo/parallel boolean.
const plannerContract = "Plan the PROCEDURE to handle the request: an ordered, minimal list of steps, each with how to execute it.\n" +
	"ORDER matters — lay the steps out logically: first locate/scout what is actually relevant, then investigate it, " +
	"then any step that builds on earlier findings. A simple request is a single step. Do NOT pad the plan with broad, " +
	"unrelated area-splits — every step must serve THIS request.\n\n" +
	"Each step has a \"strategy\":\n" +
	"- \"solo\": the main agent does it directly (no explorer). Use for anything that writes/edits, or that needs full " +
	"context — including any REASONING/ANALYSIS step (weighing trade-offs, diagnosing a root cause, synthesizing a " +
	"conclusion or decision). Analysis depends on maximum context, which only the main agent has; never hand it to an " +
	"explorer, whose view is limited to the focus/question you give it.\n" +
	"- \"parallel\": independent read-only investigations you ALREADY know are relevant — give \"groups\" (each {agent, focus, question}).\n" +
	"- \"scout\": you DON'T yet know the work-list — give \"discover\" (the list to produce, SCOPED TO WHAT THE TASK NEEDS — " +
	"e.g. for a bug hunt, the source files/packages in scope, NOT tangential files like docs) and \"each\" (what to find " +
	"out about every item); a read-only explorer lists them, then one explorer runs per item in parallel.\n" +
	"- \"delegate\": hand a LARGE, INDEPENDENT chunk of the WORK (that WRITES/BUILDS/RUNS/FIXES something) to a sub-agent " +
	"that plans and carries it out on its own — give \"task\" (the full self-contained instruction) and \"agent\" (the executor). " +
	"Use this ONLY when the task genuinely splits into big, mostly-independent build/fix units (e.g. two separate subcommands, " +
	"a component plus its tests). Decompose CONSERVATIVELY: a small, quick, or tightly-coupled piece of work is a \"solo\" step, " +
	"NOT a delegate — do NOT shatter one coherent change into many tiny delegates. Prefer the fewest units that are each " +
	"worth handing off whole. If no executor agent is available (see below), don't use delegate.\n" +
	"- \"refine\": a large sub-GOAL that is NOT independent — it depends on or builds on earlier steps, or is too big to " +
	"state as one concrete action yet. State it at a HIGH LEVEL and give \"task\" (what the sub-goal must achieve). It is " +
	"expanded into concrete sub-steps AT EXECUTION TIME, WITH the current context carried forward (unlike delegate, which " +
	"hands an independent chunk to a context-free sub-agent). Use refine to break a HARD problem into a few abstract phases " +
	"you will each work out in detail as you reach them; a small or already-concrete action stays \"solo\". An abstract " +
	"refine step is fine — you are NOT required to spell out its internal actions here. " +
	"When a task is genuinely HARD and its parts are SEQUENTIALLY DEPENDENT — each stage needs the result of the one " +
	"before (a storage layer, THEN an index built ON that storage, THEN operations built ON that index) — PREFER " +
	"opening the plan with a few \"refine\" phases over one long flat list of \"solo\" steps; you will expand each phase in " +
	"context when you reach it. Keep flat \"solo\" lists for tasks whose steps are short and mostly independent.\n\n" +
	"Explorers are READ-ONLY and LOCATE/GATHER only (agent ∈ explore|locator); never use them to write, and never to " +
	"REASON or ANALYZE — a step that draws a conclusion, weighs trade-offs, or diagnoses a cause is a \"solo\" step. " +
	"Explorers also have NO shell — their tools are LOCAL file reads (read/grep/glob/list) only. So an investigation is " +
	"an explorer step ONLY if it can be answered by reading local files. Anything that must RUN a command — ssh or reach " +
	"a REMOTE host, execute a program, probe a network/HTTP endpoint, query a database, or inspect a live " +
	"service/process/environment — needs bash, which an explorer lacks; route it to \"solo\" (the main agent has bash), " +
	"never parallel/scout. \"Explore the server\" is not automatically an explorer task: if it means ssh to a machine, it is solo. " +
	"Also give \"estimated_steps\": your honest guess at the TOTAL number of tool calls the whole task needs " +
	"(a one-file tweak ~5, a feature with tests ~30, a big build/debug ~100). It is pacing guidance only — never a limit.\n" +
	"Reply with ONLY a JSON object, no prose:\n" +
	`{"reason":"one sentence","estimated_steps":12,"steps":[{"title":"...","strategy":"solo|parallel|scout|delegate|refine","groups":[{"agent":"explore","focus":"...","question":"..."}],"agent":"explore","discover":"...","each":"...","task":"..."}]}` +
	"\n\nExample — a HARD, sequentially-dependent task (\"build a persistent key-value store backed by a " +
	"write-ahead log\") is opened as a few high-level \"refine\" PHASES, each worked out in context when reached, " +
	"NOT flattened into a long list of \"solo\" steps:\n" +
	`{"reason":"each layer builds on the one below, so open with phases and expand each in context","estimated_steps":40,"steps":[{"title":"On-disk write-ahead log","strategy":"refine","task":"design and implement the append-only log file format and an append operation"},{"title":"In-memory index from the log","strategy":"refine","task":"replay the log on startup to build a key to offset index"},{"title":"KV operations","strategy":"refine","task":"implement get/put/delete over the index and the log, keeping them consistent"},{"title":"Log compaction","strategy":"refine","task":"add a pass that rewrites the log to drop superseded records"}]}`

// literalRule is appended to the planner contract when specFidelityEnabled(): it forbids
// paraphrasing a literal contract, so the exact identifiers a grader checks survive into the
// step title/task (and from there into every downstream executor). See specFidelityEnabled.
const literalRule = "\n\nPRESERVE LITERALS: when the request specifies EXACT identifiers — a field/message/function " +
	"name, an output format, a numeric threshold, a path, or a literal string — reproduce it VERBATIM in the step " +
	"\"title\"/\"task\". Never paraphrase a literal contract (keep a field named `value` as `value`, not \"the value\"; " +
	"keep `YYYY-MM-DD` verbatim). The plan is a summary of the request, but its literals are NOT summaries."

// (specFidelityNote removed: literal fidelity is carried by literalRule in the planner contract
// above and the curated brief's verbatim `literals`, so the per-turn execution note was redundant.)

// (checkpointFirstNote removed: the discipline is now carried by checkpointFirstRule in the planner
// contract — which orders an EARLY checkpoint step — the plan-audit's executable deliverable checks,
// and a standing rule in the executor's system prompt. The per-turn note was redundant.)

// checkpointFirstRule is appended to the planner contract when checkpointFirstEnabled():
// it makes a multi-step plan ORDER the checkpoint early (a sequencing concern, not a new
// verification owner), so later steps implement against an artifact that already exists.
const checkpointFirstRule = "\n\nCHECKPOINT FIRST: if the request states HOW completion is checked or the output " +
	"applied (a snippet, command, function call, or I/O contract), make an EARLY step build a small runnable " +
	"checkpoint reproducing that check (inputs synthesized from the spec, including any named counter-example); " +
	"later steps implement until it passes. External events named by the request (a signal, a kill, a " +
	"disconnect) must be delivered for real — subprocess plus the actual signal — never simulated in-process. " +
	"Only add this when the check is actually executable — do not pad a " +
	"prose-only task with it."

// implicitAcceptRule is appended to the planner contract when implicitAcceptEnabled(): a task's
// real acceptance conditions are usually stricter than the instruction prose — the exact output
// it implies, the standard semantics it assumes, and the edge cases it never lists — so the planner
// is told to surface those and fold them into the steps' deliverables. See implicitAcceptEnabled.
const implicitAcceptRule = "\n\nEDGE-CASE RIGOR — plan for the real contract, not just the sentence: a correct solution " +
	"must survive careful scrutiny, not only the happy path the prose spells out. Before finalizing, ask what a careful " +
	"reviewer would ALSO require and make the relevant steps deliver it: (1) EXACT output — if the task shows or implies a " +
	"specific format, token, or message, produce it verbatim (a literal like `Cleaned up.` or `Results: X Y Z`, exact " +
	"counts/casing), not a paraphrase; (2) STANDARD SEMANTICS the prose assumes but does not spell out (a task whose jobs " +
	"must clean up on cancellation implies interrupt/cancellation actually runs their cleanup; a headless build implies no " +
	"display-library linkage); (3) EDGE CASES the task implies but never lists — malformed, empty, or boundary inputs, error " +
	"paths, and concurrency — handled rather than assumed away; (4) IDIOMATIC over hacky — use the mechanism the domain " +
	"expects. Do NOT invent requirements the task excludes; infer only what a competent implementation of THIS task would " +
	"obviously satisfy."

// planEnvelope gives the planner the two facts it otherwise plans blind to: the step
// BUDGET it is planning within, and its DEPTH relative to the recursion cap. Both let it
// right-size the procedure — a plan produced at the cap, or with little budget, should be
// small and concrete. The cap line is also a soft mirror of guardExpansion's hard rule:
// at the cap a "refine" step could never be expanded (planEligible needs depth < MaxPlanDepth,
// so a refine at depth d expands only while d+1 < MaxPlanDepth), so it must not be emitted.
func planEnvelope(depth, maxPlanDepth, maxSteps int) string {
	var b strings.Builder
	b.WriteString("\n\nBudget & depth (right-size the plan to these):\n")
	scope := "the whole task"
	if depth > 0 {
		scope = "this sub-task"
	}
	fmt.Fprintf(&b, "- About %d tool calls are available for %s — pacing guidance, not a hard limit.\n", maxSteps, scope)
	fmt.Fprintf(&b, "- Planning depth %d of max %d.\n", depth, maxPlanDepth)
	if depth+1 >= maxPlanDepth {
		b.WriteString("- You are AT the depth cap: every step MUST be concrete (solo/parallel/scout/delegate). " +
			"Do NOT use \"refine\" — an abstract step here can never be expanded, so it would dead-end; work it out inline instead.\n")
	} else {
		b.WriteString("- Below the cap: \"refine\" phases are allowed and are expanded in context when reached. " +
			"If this plan is itself an expansion of a refine step and uses \"refine\", it MUST also contain at least one " +
			"concrete work step — never a plan that only re-defers the work.\n")
	}
	return b.String()
}

// refinePrompt frames a refine step as an in-context sub-goal. On a local retry it leads
// with the prior failure so the next attempt changes approach (the failure is also in the
// cloned context, but stating it explicitly steadies a weak model).
func refinePrompt(st planStep, fail string) string {
	p := ""
	if f := strings.TrimSpace(fail); f != "" {
		p = "A previous attempt at this sub-goal did NOT succeed: " + f + "\nTake a DIFFERENT approach this time.\n\n"
	}
	return p + st.Task + "\n\n(You are working out ONE sub-goal of a larger plan, continuing from the conversation " +
		"so far. Break it into concrete steps as needed, complete it fully, then " + noFabricate + " If after real " +
		"effort this sub-goal genuinely cannot be done, report status \"failed\" and say plainly what blocked you — " +
		"do not report unfinished work as done.)"
}

// noFabricate is the anti-fabrication half of every child hand-off's self-verify contract:
// verify by real execution and cite it, and if you could NOT run/confirm something, admit it
// rather than manufacture a verified-looking result. The delegate and stuck-recovery hand-offs
// previously asked only to "report how you verified it" with no license for the honest negative
// — an asymmetry that pressures a weak model to fabricate (write a stand-in results file it never
// produced) just to answer the ask. Single-sourced so all hand-offs stay symmetric.
const noFabricate = "verify it yourself by actually running it, and report concretely how you verified it (the " +
	"command you ran and its real output). If you could NOT actually run or confirm something, say so plainly and " +
	"treat it as unverified — never invent or hand-write output, and never write a stand-in or placeholder file to " +
	"make it look done; hiding the gap is worse than admitting it."

// divergeClause (MAGI_DIVERGE, default ON) teaches the diverge→triage→commit shape for
// problems whose CAUSE or APPROACH is genuinely uncertain: enumerate a few DISTINCT
// candidate explanations first, kill the wrong ones with cheap decisive probes, then
// commit the budget to the survivor. Counterweight to the observed local-refinement
// lock: three bench post-mortems in a row spent the whole budget drilling variations
// of the FIRST hypothesis while the winning fix lay on a neighboring axis nobody
// re-examined. Appended to the planner contract at build time (plan_flags gate).
const divergeClause = "\nWhen the CAUSE of a problem (or the right approach) is genuinely UNCERTAIN — a bug hunt, a " +
	"root-cause diagnosis, a reverse-engineering question — do NOT commit the whole plan to your first hypothesis. " +
	"Open with ONE step that lists 2-3 DISTINCT candidate explanations (different mechanisms, not variations of one " +
	"idea) each with the cheapest observation that would CONFIRM or KILL it; run those probes (parallel/scout if " +
	"read-only, solo otherwise); then commit the remaining steps to the surviving candidate. If work on the survivor " +
	"later stalls, revisit the list and switch to the next candidate rather than iterating variations of a dead one.\n"

// stuckRedecomposePrompt frames the solo-stuck recovery: the decompose instruction, the task,
// and the specific wall the previous attempt hit (a stall reason or the council's last unmet
// concern) so the child knows what to break through, then the delegate contract's self-verify
// framing. Reuses decomposePrefix so its "BREAK IT DOWN" wording stays single-sourced.
func stuckRedecomposePrompt(task, blockReason string) string {
	p := decomposePrefix + strings.TrimSpace(task)
	if r := strings.TrimSpace(blockReason); r != "" {
		p += "\n\nWhat blocked the previous attempt (address this directly): " + r
	}
	return p + "\n\n(You are taking over a task a previous attempt got stuck on; partial work may " +
		"already be on disk. Complete it fully, then " + noFabricate + ")"
}

// stuckUnitPrompt frames ONE work unit of a stuck task's decomposition for a child that was
// seeded with the FULL parent context (CloneContext). Because the whole conversation — every file
// already read, every partial change already on disk — is carried forward, the child must NOT
// re-read to reconstruct context; it just carries out this single scoped unit within that context.
// This is the anti-fixation lever: the previous attempt looped re-reading the same file instead of
// acting, so the unit prompt hands the model the context it kept looping to rebuild and a small,
// concrete next action. blockReason (the wall the previous attempt hit) rides on EVERY unit — the
// unit that actually touches the fixation point may be any of them, and one warning line is cheap.
func stuckUnitPrompt(st planStep, blockReason string) string {
	unit := strings.TrimSpace(st.Task)
	if unit == "" {
		unit = strings.TrimSpace(st.Title)
	}
	p := "You already have the full conversation and all work so far in context — do NOT re-read " +
		"files or re-derive what you already know. A previous attempt on the larger task got stuck. " +
		"It has been broken into small units; carry out ONLY THIS ONE unit now, then stop:\n\n" + unit
	if r := strings.TrimSpace(blockReason); r != "" {
		p += "\n\nWhat blocked the previous attempt (do not repeat it): " + r
	}
	return p + "\n\n(Complete just this unit fully — take the real action, don't re-inspect what you " +
		"already have. Then " + noFabricate + ")"
}

// delegatePrompt frames a delegate step as a self-contained sub-task instruction, optionally
// prefixed with a compact brief (see delegateBrief). The brief is orientation only — the task
// itself stays self-contained, so an empty brief leaves the original context-free hand-off.
func delegatePrompt(st planStep, brief string) string {
	p := ""
	if b := strings.TrimSpace(brief); b != "" {
		p = b + "\n\n"
	}
	return p + st.Task + "\n\n(You are handling ONE independent part of a larger plan. Complete this part fully, " +
		"then " + noFabricate + ")"
}

// delegateBrief builds the compact context a delegate child gets IN ADDITION to its
// self-contained task: the overall goal (so its choices align with the whole task, not just
// its slice), the OTHER step titles (boundaries — what it must NOT redo), and what earlier
// steps ALREADY produced (interfaces/outputs to build on rather than reinvent). It carries
// NO parent conversation or reasoning — that is refine's job (a full context clone); a
// delegate child stays a clean-room worker that just knows where its slice fits. Every part
// is clipped so the brief can't blow up as siblings accumulate. Returns "" when there is
// nothing worth briefing (e.g. a lone first step with no goal text).
func delegateBrief(goal string, steps []planStep, i int, prior []string) string {
	var b strings.Builder
	if g := strings.TrimSpace(goal); g != "" {
		// Part C: the delegate child is context-free — it never sees the raw request, so a
		// paraphrased goal is its ONLY spec. When spec fidelity is on, carry the goal verbatim
		// (generously clipped) and label it authoritative, so the child copies exact identifiers
		// from it rather than normalizing them. Off → the compact 400-char orientation line.
		// The cap is generous (8000B covers virtually every real request) and, crucially, uses
		// clipSpec — a bare "…" here made the child reproduce a truncated block into an edit
		// old-string that then matched nothing (the exact defect the VERBATIM label promises against).
		if specFidelityEnabled() {
			b.WriteString("SPEC (authoritative — for any exact name, field, format, or value, follow this VERBATIM): " + clipSpec(g, 8000) + "\n")
		} else {
			b.WriteString("Overall goal (the whole task your part serves): " + clipLine(g, 400) + "\n")
		}
	}
	var others []string
	for j, st := range steps {
		if j != i {
			if t := strings.TrimSpace(st.Title); t != "" {
				others = append(others, t)
			}
		}
	}
	if len(others) > 0 {
		b.WriteString("Other steps handled separately (do NOT redo these): " + clipLine(strings.Join(others, "; "), 400) + "\n")
	}
	if p := strings.TrimSpace(strings.Join(prior, "\n")); p != "" {
		b.WriteString("Already produced by earlier steps — build on these, don't rebuild:\n" + clipLine(p, 800) + "\n")
	}
	return strings.TrimSpace(b.String())
}

// decomposePrefix leads the ADaPT failure-branch retry: the direct attempt did not
// complete, so tell the executor to break the sub-task down and do the pieces one at a
// time. The executor re-plans at depth+1, so this decomposition instruction lands in its
// own pre-flight planner.
const decomposePrefix = "A direct attempt at the task below did NOT complete. Approach it differently now: BREAK IT DOWN " +
	"into smaller, independent steps and carry them out ONE AT A TIME, verifying each before moving on.\n\n"

// redecomposePrompt frames the ADaPT failure-branch retry as the delegate instruction with
// the decompose prefix — it reuses delegatePrompt's self-contained framing, so the two
// share their trailing contract instead of duplicating it.
func redecomposePrompt(st planStep, brief string) string {
	return decomposePrefix + delegatePrompt(st, brief)
}

// runExplorers dispatches the groups as read-only subagents concurrently and
// returns their findings concatenated in a stable order.
// explorerPrompt builds the read-only investigation prompt for one explorer group, optionally
// prefixed with the overall goal for orientation. Shared by the synchronous (runExplorers) and
// background (dispatchExplorerSteps) fan-out paths so both send an identical prompt.
func explorerPrompt(goal string, g planGroup) string {
	prompt := fmt.Sprintf("Investigate (read-only): %s\n\n%s", g.Focus, g.Question)
	if og := strings.TrimSpace(goal); og != "" {
		prompt = "Overall goal (context for your investigation): " + clipLine(og, 400) + "\n\n" + prompt
	}
	return prompt
}
