// Package llm implements the Council port by polling each member over an
// LLMProvider in parallel, parsing a JSON verdict from each, and tallying them
// with the pure core/council rules. It is the I/O half of the termination gate;
// the consensus half lives in core/council and is provider-agnostic.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/lang"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Council polls members over an LLM backend. Each member can run on its own
// backend (resolved by name) and model, so cheap and strong models can be mixed;
// a member with no provider/model falls back to the default backend and model.
type Council struct {
	resolve func(provider string) port.LLMProvider // name → backend ("" or unknown → default)
	model   string
}

// New builds an LLM-backed council. resolve maps a member's provider name to a
// backend (returning the default backend for "" or an unknown name); model is the
// fallback model when neither the member nor the request pins one.
func New(resolve func(provider string) port.LLMProvider, defaultModel string) *Council {
	return &Council{resolve: resolve, model: defaultModel}
}

// Deliberate polls every member concurrently and tallies the verdicts. A member
// that errors or returns an unparseable reply abstains (excluded from the
// denominator) rather than blocking the gate forever; if every member abstains,
// the pure tally resolves to Continue (the safe default).
func (c *Council) Deliberate(ctx context.Context, req port.DeliberationRequest) (council.Deliberation, error) {
	members := req.Members
	if len(members) == 0 {
		members = council.DefaultMembers()
	}
	rule := req.Rule
	if rule == "" {
		rule = council.DefaultRule
	}

	verdicts := make([]council.Verdict, len(members))
	var wg sync.WaitGroup
	for i, m := range members {
		wg.Add(1)
		go func(i int, m council.Member) {
			defer wg.Done()
			verdicts[i] = c.poll(ctx, req, m)
		}(i, m)
	}
	wg.Wait()

	// Focused re-round: fold the prior round's done votes back in before the rule
	// runs, so the tally still spans the full council even though only the
	// dissenting members were re-polled.
	verdicts = append(verdicts, req.CarriedDone...)

	d := council.Deliberate(req.Round, verdicts, rule)
	if req.Phase == "plan" {
		// Plan audit: synthesize the members' proposed completion criteria into the
		// contract the turn will later be judged against, plus any per-step executable
		// deliverable checks (settled by execution at plan time, not re-voted later).
		d.Criteria = council.MergeCriteria(verdicts)
		d.Checks = council.MergeChecks(verdicts)
	}
	return d, nil
}

// judgeReply is the JSON shape the revision judge is asked to return.
type judgeReply struct {
	Addressed bool   `json:"addressed"`
	Reason    string `json:"reason"`
}

// JudgeRevision asks a single model whether the revised procedure engages the council's
// concern. It fails OPEN (Addressed=true) on any backend or parse error so a flaky judge
// never falsely cuts a productive re-plan loop; the reason records why.
func (c *Council) JudgeRevision(ctx context.Context, req port.RevisionJudgeRequest) (port.RevisionVerdict, error) {
	model := req.DefaultModel
	if model == "" {
		model = c.model
	}
	provider := c.resolve("")
	if provider == nil { // defensive: no backend → don't block, assume engaged
		return port.RevisionVerdict{Addressed: true, Reason: "no council backend resolved"}, nil
	}
	system := "You check whether a REVISED plan actually engages a specific CONCERN raised about the PRIOR plan. " +
		"You are NOT judging whether the revised plan is perfect or complete — only whether it made a genuine, relevant " +
		"change directed at the concern (added/changed/reordered a step that targets it). A revision that ignores the " +
		"concern, or only rephrases the same steps, does NOT address it. " +
		"Reply with ONLY a JSON object: {\"addressed\": <true|false>, \"reason\": \"<one short sentence>\"}."
	user := "CONCERN raised about the prior plan:\n" + req.Critique +
		"\n\nPRIOR plan:\n" + req.PriorPlan +
		"\n\nREVISED plan:\n" + req.RevisedPlan +
		"\n\nDid the REVISED plan make a genuine change directed at the CONCERN?"

	stream, err := provider.StreamChat(ctx, port.ChatRequest{
		Model:    model,
		System:   system,
		Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: user}}}},
		Params:   map[string]any{"temperature": 0.0},
	})
	if err != nil {
		return port.RevisionVerdict{Addressed: true, Reason: "revision judge unavailable: " + err.Error()}, nil
	}
	var b strings.Builder
	for ev := range stream {
		if ev.Type == port.ProviderText {
			b.WriteString(ev.Text)
		}
	}
	js := firstJSONObject(b.String())
	if js == "" {
		return port.RevisionVerdict{Addressed: true, Reason: "unparseable revision-judge reply"}, nil
	}
	var r judgeReply
	if err := json.Unmarshal([]byte(js), &r); err != nil {
		return port.RevisionVerdict{Addressed: true, Reason: "unparseable revision-judge reply"}, nil
	}
	reason := strings.TrimSpace(r.Reason)
	if reason == "" {
		reason = "no reason given"
	}
	return port.RevisionVerdict{Addressed: r.Addressed, Reason: reason}, nil
}

// poll asks one member and returns its verdict.
func (c *Council) poll(ctx context.Context, req port.DeliberationRequest, m council.Member) council.Verdict {
	v := council.Verdict{Member: m.Name, Lens: m.Lens, Weight: m.Weight}

	// Model: the member's pin, else the request's default (session model), else the
	// adapter's fallback. Provider: the member's named backend, else default.
	model := m.Model
	if model == "" {
		model = req.DefaultModel
	}
	if model == "" {
		model = c.model
	}
	provider := c.resolve(m.Provider)
	if provider == nil { // defensive: a resolver must yield a backend
		v.Decision = council.Abstain
		v.Rationale = "no council backend resolved for provider " + m.Provider
		return v
	}
	user := evidence(req)
	if req.DeltaRound {
		if concern := strings.TrimSpace(req.PriorConcern[m.Name]); concern != "" {
			// Focused re-round: the member re-judges ITS OWN standing objection
			// against the delta, instead of re-auditing the whole turn (which is
			// both slower and how fresh nitpicks appear round after round).
			user += "\n\n# Focused re-round\nYou previously voted continue with this concern:\n" + concern +
				"\n\nThe Actions section above contains ONLY what the agent did since that rejection (the delta). " +
				"Judge whether the delta resolves YOUR concern: vote done if it does; if not, state precisely " +
				"what is still missing — do not raise new, unrelated objections."
		}
	}
	stream, err := provider.StreamChat(ctx, port.ChatRequest{
		Model:    model,
		System:   memberSystem(m, req.Phase, req.Task),
		Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: user}}}},
		Params:   map[string]any{"temperature": 0.0},
	})
	if err != nil {
		v.Decision = council.Abstain
		v.Rationale = "council member unavailable: " + err.Error()
		return v
	}

	var b strings.Builder
	for ev := range stream {
		if ev.Type == port.ProviderText {
			b.WriteString(ev.Text)
		}
	}

	r, ok := parseReply(b.String())
	if !ok {
		v.Decision = council.Abstain
		v.Rationale = "unparseable council reply"
		return v
	}
	v.Decision = decisionOf(r.Decision)
	v.Confidence = r.Confidence
	v.Rationale = r.Rationale
	v.Feedback = r.Feedback
	v.Severity = r.Severity // plan-audit phase only; gates blocking vs advisory
	v.Criteria = r.Criteria // plan-audit phase only; empty otherwise
	v.Checks = r.Checks     // plan-audit phase only; per-step executable deliverable checks
	return v
}

// memberReply is the JSON shape each member is asked to return.
type memberReply struct {
	Decision   string   `json:"decision"`
	Confidence float64  `json:"confidence"`
	Rationale  string   `json:"rationale"`
	Feedback   string   `json:"feedback"`
	Severity   string   `json:"severity"` // plan-audit phase: critical|warn|info for a revise vote
	Criteria   []string `json:"criteria"` // plan-audit phase: proposed completion criteria
	// Checks are plan-audit per-step executable deliverable checks (empty otherwise).
	Checks []council.DeliverableCheck `json:"checks"`
}

// memberSystem builds the system prompt for one member: its identity (the theme
// label) and its judging lens (the attribute), plus the strict output contract.
// The phase selects whether the member judges a finished turn ("terminate") or a
// proposed procedure ("plan").
func memberSystem(m council.Member, phase, task string) string {
	lens := council.Lenses[m.Lens]
	if lens == "" {
		lens = "Judge whether the task is genuinely complete."
	}
	if phase == "plan" {
		return withLangNote(planMemberSystem(m, lens), task)
	}
	return withLangNote(fmt.Sprintf(
		"You are %s, a member of the council that decides whether an AI coding agent's turn is truly finished. "+
			"Your lens is %q: %s\n\n"+
			"Judge the agent's REPORT against the TASK and PLAN. Use the SIGNALS and DIFF as evidence WHEN PRESENT — "+
			"but the ABSENCE of a diff or signal is NEVER a reason to continue. Many turns (investigation, reading, "+
			"answering, analysis) legitimately produce no diff; demanding one is exactly the reflexive churn to avoid.\n"+
			"Choose exactly one vote:\n"+
			"- \"done\": through your lens, the report reasonably satisfies the task. Judge it on its merits — if there is "+
			"evidence it should back the claim; if there is none, the task simply didn't call for any.\n"+
			"First decide what the deliverable IS, from the USER'S TASK — not from the plan's or criteria's wording. If the "+
			"user asked to CREATE, SAVE, BUILD, RUN, IMPLEMENT, FIX, ADD, or otherwise modify something concrete (a named file, a building/running program, "+
			"a service, a specific output), then \"done\" requires evidence — in the REPORT, the DIFF, the SIGNALS, or WHAT THE "+
			"TURN PRODUCED (its tool results, e.g. a write that reported bytes written or a command that shows the file's "+
			"content) — that it actually exists; a confident claim or a description of it is NOT itself the artifact — if that "+
			"evidence is absent, vote continue and name the missing artifact in feedback. (A missing git DIFF alone is not "+
			"decisive: the workdir may not be a git repo — weigh the produced tool results instead.) A tool's [ok]/exit-0 "+
			"status is NOT itself proof — the result must SHOW the artifact (its bytes or content); an echo, ls, or command "+
			"that does not reveal the artifact proves nothing, and the agent's own narration is a claim, never evidence. "+
			"Otherwise — a read, "+
			"review, analyze, explain, or answer "+
			"task — the deliverable IS the answer or review in the REPORT itself: judge its substance, and never demand a "+
			"file, diff, or document. A plan step or criterion phrased as \"write/produce a summary\" for such a task is "+
			"satisfied by that content in the report, not by a separate file. Files the agent merely READ or cited are "+
			"INPUTS, never missing deliverables — never fault it for not \"creating\" one.\n"+
			"For such an analyze / review / survey task over a LARGE set (many files or items), \"done\" means the report "+
			"covers the task's scope REASONABLY and representatively — a well-organized answer hitting the important, "+
			"priority-ranked findings is COMPLETE. Do NOT demand EXHAUSTIVE enumeration of every item, or atom-level "+
			"precision (every file, every exact line number), unless the TASK EXPLICITLY asked for it: \"cover more items\" "+
			"or \"be more precise\" is a wish for more proof, not a concrete defect, and voting continue for it is exactly "+
			"the churn to avoid. Vote continue on such a report ONLY for a REAL defect — a whole REQUESTED part is missing, "+
			"or the report's own content is wrong or self-contradictory. This proportionality relaxes ONLY the BREADTH "+
			"expected of a genuine analysis/survey answer. It does NOT apply to any CREATE/BUILD/RUN/FIX PART of the task: "+
			"even when the task is MOSTLY analysis, if part of it is to write/build/run/fix something concrete, that part "+
			"still owes the existence, correctness, and run-the-check evidence below — never treat a never-written file, a "+
			"never-run check, or a wrong value as \"covered\" just because the analysis around it reads as representative.\n"+
			"Beyond existence, check CORRECTNESS against the LETTER of the task: when the task dictates the deliverable's "+
			"exact content, value, format, name, or location, compare what the turn ACTUALLY produced (its shown "+
			"content/bytes/tool results) against that literal requirement. A deliverable that exists but whose content "+
			"does not match what was asked — e.g. writing a file's NAME where the task asked for the word/content INSIDE "+
			"it, a placeholder, a wrong field, or the right shape with the wrong value — is a concrete defect: vote "+
			"continue and name the mismatch in feedback. Re-read the task wording literally; the agent's own paraphrase "+
			"of what it did is a claim, never proof the content is right.\n"+
			"Existence is not correctness: when the task implies a CHECKABLE behavior — a password that must unlock "+
			"something, a service that must respond, a command that must produce a required output, a build that must "+
			"compile — \"done\" requires that the turn actually RAN that check and its REAL output is visible in the "+
			"SIGNALS, tool results, or report. An artifact that was produced but never exercised is unverified: vote "+
			"continue and name the exact check to run. Unanimous confidence is not a substitute for one real run.\n"+
			"Correctness also covers PREMISES the deliverable rests on. When its correctness depends on an EXTERNAL "+
			"FACT the agent could not confirm — a `self-check/unverified-premise` signal is present (a knowledge lookup "+
			"failed and was never recovered), or the report shows a key value was assumed, recalled, or inferred rather "+
			"than looked up/tested — treat that fact as UNVERIFIED. A deliverable in exactly the right shape and format "+
			"built on an unconfirmed domain fact (an API detail, a constant, a sequence, a spec, an identifier) is NOT "+
			"done: you cannot certify such a fact from reasoning alone, so do NOT vote done on faith that it is probably "+
			"right — vote continue and tell the agent to verify the fact or state the uncertainty plainly instead of "+
			"asserting it as settled.\n"+
			"A report that RATIONALIZES incompletion is NOT done. If the report's own words say a required part was "+
			"impossible, skipped, never actually run, only inferred from documentation or reasoning, or \"needed no "+
			"work\" without shown verification, that is an ADMISSION the deliverable was not produced or confirmed — "+
			"no matter how reasonable the excuse or how confident the framing (\"this constitutes full completion\", "+
			"\"honest acknowledgment of limitations\", \"nothing to fix\"). An eloquent justification never converts "+
			"unfinished work into done: vote continue, and in feedback tell the agent to either actually perform and "+
			"verify that part, or finish honestly by reporting the task as failed/blocked instead of done.\n"+
			"- \"continue\": ONLY when you can name a SPECIFIC, REAL defect through your lens — a FAILING signal, a part of "+
			"the task/plan the report itself shows is unmet, or a concrete error in the work. Put the next step in "+
			"`feedback`. A missing diff or signal is NOT a defect.\n"+
			"- \"abstain\": your lens genuinely cannot judge from what is given. Excluded from the tally.\n\n"+
			"Never invent a defect, never demand evidence the task never required, and never continue out of mere "+
			"uncertainty or a wish for more proof. When the turn changed no files, judge the report's SUBSTANCE against "+
			"the task — the absence of a diff is not itself a defect, but a wrong or incomplete answer still is.\n\n"+
			"Respond with ONLY a JSON object, no prose, no code fence:\n"+
			`{"decision":"done|continue|abstain","confidence":0.0-1.0,"rationale":"one sentence","feedback":"the specific gap (only if continue)"}`,
		m.Name, m.Lens, lens), task)
}

// withLangNote appends, when the task is in a non-English language, an instruction to
// write the human-facing rationale/feedback in that language (keys/decision stay
// English) — so the user reads the council's reasons in their own language.
func withLangNote(prompt, task string) string {
	l := lang.Detect(task)
	if l == "" {
		return prompt
	}
	return prompt + "\n\nWrite the \"rationale\" and \"feedback\" values in " + l +
		" (the user's language), not English. Keep the JSON keys and the \"decision\" value in English."
}

// planMemberSystem builds the prompt for the pre-flight plan audit: the member
// judges whether the PROPOSED PROCEDURE is a sound way to accomplish the task,
// through its lens — there is no report/diff/signals yet.
func planMemberSystem(m council.Member, lens string) string {
	return fmt.Sprintf(
		"You are %s, a member of a council that audits an AI coding agent's PROPOSED PROCEDURE before it runs. "+
			"Your lens is %q: %s\n\n"+
			"Judge ONLY whether the PROCEDURE (the ordered steps) is a sound, sufficient way to accomplish the TASK "+
			"through your lens. The DEFAULT is to APPROVE: a plan that would plausibly get the task done is sound, even "+
			"if terse. Choose exactly one vote:\n"+
			"- \"done\" (approve): the steps would accomplish the task. MOST plans are this — especially for simple "+
			"read / review / answer tasks.\n"+
			"- \"continue\" (revise): ONLY if you can name a CONCRETE flaw in the STEPS THEMSELVES — a step that is "+
			"wrong, a necessary ACTION that is missing, a redundant step, or a wrong order. Put the concrete fix in "+
			"`feedback`, and set `severity`:\n"+
			"    • \"critical\" — the plan as written will FAIL or produce a WRONG/unsafe result without this fix "+
			"(a necessary action is missing, a step is incorrect, destructive, or in an order that breaks the task). "+
			"ONLY critical blocks the agent from starting — reserve it for real defects.\n"+
			"    • \"warn\" — the plan would still accomplish the task, but this would meaningfully improve it.\n"+
			"    • \"info\" — a minor nit. When unsure, use \"warn\"; do NOT inflate to critical.\n"+
			"- \"abstain\": your lens cannot judge these steps. The VERIFICATION lens has nothing to verify before the "+
			"work exists — it MUST abstain, never revise. Abstaining is excluded from the tally.\n\n"+
			"Lens notes at PLAN time:\n"+
			"- COMPLETENESS judges ONLY whether the steps as a set COVER the task's scope — NOT whether every detail, "+
			"sub-check, or edge case is enumerated. A plan whose steps touch each part of the request IS complete → "+
			"approve. Revise ONLY if a whole REQUIRED part of the task has no step addressing it at all (name the missing "+
			"part). A broad step like \"review the docs\" already covers its details — do not revise to enumerate them.\n"+
			"- CORRECTNESS judges whether the APPROACH is sound and no necessary ACTION is missing — not whether the plan "+
			"adds checks/validation.\n\n"+
			"NEVER revise for any of the following — these are NOT flaws in a plan:\n"+
			"- the plan doesn't spell out verification criteria, acceptance criteria, success metrics, tests, or a "+
			"checklist (those belong to execution and to the `criteria` field below — NOT to the plan's steps);\n"+
			"- the plan has no explicit 'verify' / 'validate' step, or could be 'more thorough', 'more detailed', or "+
			"'more rigorous';\n"+
			"- you would simply have planned it differently, or you are merely uncertain;\n"+
			"- a step marked [refine] is INTENTIONALLY high-level: it is expanded into concrete sub-steps AT EXECUTION "+
			"TIME with the current context, so 'it doesn't spell out its internal actions', 'it is too abstract', or 'it "+
			"needs more concrete steps' is NOT a flaw in it. NEVER critical-revise a refine step for abstractness alone; if "+
			"you genuinely see a better decomposition, that is a \"warn\" at most (advisory, non-blocking). This is NOT a pass "+
			"for a bad plan, though: if a refine-bearing plan is genuinely UNSOUND — the approach is wrong, a REQUIRED part "+
			"of the task has no step at all, or the plan simply would not achieve the task — that is STILL critical, abstract "+
			"or not. Reject the absurd, approve the merely abstract.\n"+
			"A SIMPLE task needs only a SIMPLE plan. Never invent a flaw. If you cannot name a concrete defect in the "+
			"steps, you APPROVE (or abstain).\n\n"+
			"CALIBRATION — match this bar. Task: \"review the project's dev docs\"; Plan: \"1.[scout] find the docs  "+
			"2.[parallel] read each  3.[solo] summarize\". The CORRECT verdicts are: correctness → done (discover→read→"+
			"synthesize is a sound approach with no necessary action missing); completeness → done (the steps cover the "+
			"whole request); verification → abstain (nothing to verify yet). Revising this plan — to add acceptance "+
			"criteria, a verify step, or more detail — would be WRONG. Hold real plans to exactly this bar.\n\n"+
			"SEPARATELY, through your lens, propose this task's COMPLETION CRITERIA in `criteria`: a short list (1-3) of "+
			"concrete done-conditions used to judge the FINISHED work later (e.g. a file/output that must exist, a check "+
			"that must pass). Each criterion MUST be ACHIEVABLE and PROPORTIONATE to the task's size: for an analysis, "+
			"survey, or investigation over a LARGE set (many files/items), do NOT write a done-condition that demands "+
			"EXHAUSTIVE enumeration of every item or atom-level precision (\"list ALL N files with EXACT line numbers\") "+
			"— that is impractical and will be enforced as an impossible contract. Phrase such a criterion as the QUALITY "+
			"of a representative, priority-ranked coverage instead (e.g. \"the main refactoring candidates are identified "+
			"with their file and rough location\"), never \"every X\" or \"all N with exact lines\". For a "+
			"read / review / analyze / answer task, a criterion is a quality of the ANSWER, never a new file or document "+
			"to create. (This proportionality applies to analysis/survey scope ONLY — for a CREATE/BUILD/RUN/FIX task the "+
			"criterion still requires the concrete artifact to exist and its check to pass; do not soften that.) These are "+
			"NOT steps the plan must contain, and their absence from the plan is NEVER a reason to revise. Keep each item "+
			"one short line; omit if your lens adds nothing.\n\n"+
			"ALSO, where a step's deliverable is MACHINE-CHECKABLE, propose one or more executable `checks`. Each check "+
			"names the plan `step` it belongs to (its title or number), the expected `deliverable` in one short phrase, a "+
			"shell `command` that verifies it from the task's working directory, and an optional `expect` REGULAR "+
			"EXPRESSION the command's output must match (omit `expect` for an exit-code-only check). PREFER a check that "+
			"EXERCISES the deliverable's content over one that only asserts a file exists — run it, grep its contents, or "+
			"test that it is non-empty (`test -s out.txt`, not bare `test -f`) — so a stale or empty leftover cannot pass. "+
			"The deliverable may be a file (`test -s out.txt`), a build/test result (`go build ./...`), or PROGRAM OUTPUT "+
			"ON SCREEN — for output, run the program and match its stdout with `expect` (e.g. command `./run --demo`, "+
			"expect `^total: [0-9]+$`). When the task's acceptance involves an EXTERNAL event — a signal "+
			"(Ctrl-C/SIGINT), a kill, a disconnect — author a check that DELIVERS the event for real: launch the "+
			"artifact as a background process, send the actual signal, and match the required output (e.g. command "+
			"`python3 app.py & p=$!; sleep 1; kill -INT $p; wait $p 2>/dev/null; ...`, expect the cleanup marker). "+
			"An in-process simulation (raising the exception by hand) verifies the wrong delivery path. A "+
			"step may have SEVERAL checks (several deliverables). Propose checks ONLY when they are concrete and would "+
			"genuinely pass for correct work — commands must be non-destructive and deterministic. For a "+
			"read/review/analyze/answer step there is usually nothing to execute: emit NO check for it (the prose "+
			"`criteria` already cover it). Omit `checks` entirely if your lens has none.\n\n"+
			"Respond with ONLY a JSON object, no prose, no code fence:\n"+
			`{"decision":"done|continue|abstain","confidence":0.0-1.0,"rationale":"one sentence","feedback":"the specific fix (only if continue)","severity":"critical|warn|info (only if continue)","criteria":["..."],"checks":[{"step":"...","deliverable":"...","command":"...","expect":"..."}]}`,
		m.Name, m.Lens, lens)
}

// evidence renders the deliberation request into the user message the members see.
func evidence(req port.DeliberationRequest) string {
	var b strings.Builder
	section := func(title, body string) {
		if strings.TrimSpace(body) == "" {
			return
		}
		b.WriteString("# " + title + "\n")
		b.WriteString(strings.TrimSpace(body))
		b.WriteString("\n\n")
	}
	if req.Phase == "plan" {
		// Plan audit: only the task and the proposed procedure exist yet.
		section("Task (the goal)", req.Task)
		section("Proposed procedure (the plan to audit)", req.Plan)
		if b.Len() == 0 {
			return "No task or procedure was provided; with nothing to judge, abstain."
		}
		return strings.TrimSpace(b.String())
	}
	section("Task (the goal)", req.Task)
	section("Plan / acceptance criteria (the contract)", req.Plan)
	section("Agent's report (the claim)", req.Report)
	if len(req.Signals) > 0 {
		b.WriteString("# Signals (the evidence)\n")
		for _, s := range req.Signals {
			line := "- "
			switch {
			case s.Source != "" && s.Kind != "":
				line += "[" + s.Source + "/" + s.Kind + "] "
			case s.Source != "":
				line += "[" + s.Source + "] "
			case s.Kind != "":
				line += "[" + s.Kind + "] "
			}
			line += s.Status
			if d := strings.TrimSpace(s.Detail); d != "" {
				line += ":\n" + d
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}
	section("What the turn's tools produced (verified tool outputs — real evidence, independent of git: e.g. a write's byte count, a cat showing the file's contents)", req.Actions)
	section("Changes the agent made this turn (per-file before→after, reconstructed from its own edit tools)", req.Changes)
	if req.NoChanges {
		b.WriteString("# Changes\n(none recorded — a read-only / investigation / answer turn: no file edits or signals to inspect. " +
			"Judge the report's substance against the task; do not treat the absence of edits as a defect.)\n\n")
	}
	// Budget awareness (terminate gate only): when the agent is almost out of its step budget,
	// a "continue" verdict it cannot act on just burns the remaining steps and ends the turn
	// with nothing landed. Tell members to prefer accepting a reasonable result over demanding
	// work that won't fit — without lowering the bar for a genuine, fixable defect.
	if req.Phase != "plan" && req.StepsLeft > 0 && req.StepsLeft <= 5 {
		b.WriteString(fmt.Sprintf("# Budget\nThe agent has only about %d step(s) left before it is force-stopped. "+
			"If the report is a reasonable, working result, prefer DONE — a continue verdict it has no budget to act "+
			"on wastes the remaining steps and ends the turn with nothing landed. Ask for another round only for a "+
			"real, fixable defect that fits in the remaining budget.\n\n", req.StepsLeft))
	}
	if b.Len() == 0 {
		return "No task, report, or evidence was provided. With nothing to judge through your lens, abstain."
	}
	return strings.TrimSpace(b.String())
}

// decisionOf maps a member's free-form decision string to a Decision. An
// unrecognized but parsed value resolves to Continue (the gate never finishes on
// an ambiguous vote).
func decisionOf(s string) council.Decision {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "done":
		return council.Done
	case "abstain":
		return council.Abstain
	default:
		return council.Continue
	}
}

// parseReply extracts the first balanced JSON object from the text (tolerating
// surrounding prose or code fences that weak models emit) and unmarshals it.
func parseReply(text string) (memberReply, bool) {
	js := firstJSONObject(text)
	if js == "" {
		return memberReply{}, false
	}
	var r memberReply
	if err := json.Unmarshal([]byte(js), &r); err != nil {
		return memberReply{}, false
	}
	if r.Decision == "" {
		return memberReply{}, false
	}
	return r, true
}

// firstJSONObject returns the first balanced {...} object in s, respecting
// strings and escapes, or "" if none.
func firstJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		switch {
		case esc:
			esc = false
		case ch == '\\' && inStr:
			esc = true
		case ch == '"':
			inStr = !inStr
		case inStr:
			// skip
		case ch == '{':
			depth++
		case ch == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
