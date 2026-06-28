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

	d := council.Deliberate(req.Round, verdicts, rule)
	if req.Phase == "plan" {
		// Plan audit: synthesize the members' proposed completion criteria into the
		// contract the turn will later be judged against.
		d.Criteria = council.MergeCriteria(verdicts)
	}
	return d, nil
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
	stream, err := provider.StreamChat(ctx, port.ChatRequest{
		Model:    model,
		System:   memberSystem(m, req.Phase, req.Task),
		Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: evidence(req)}}}},
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
	v.Criteria = r.Criteria // plan-audit phase only; empty otherwise
	return v
}

// memberReply is the JSON shape each member is asked to return.
type memberReply struct {
	Decision   string   `json:"decision"`
	Confidence float64  `json:"confidence"`
	Rationale  string   `json:"rationale"`
	Feedback   string   `json:"feedback"`
	Criteria   []string `json:"criteria"` // plan-audit phase: proposed completion criteria
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
			"`feedback`.\n"+
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
			"- you would simply have planned it differently, or you are merely uncertain.\n"+
			"A SIMPLE task needs only a SIMPLE plan. Never invent a flaw. If you cannot name a concrete defect in the "+
			"steps, you APPROVE (or abstain).\n\n"+
			"CALIBRATION — match this bar. Task: \"review the project's dev docs\"; Plan: \"1.[scout] find the docs  "+
			"2.[parallel] read each  3.[solo] summarize\". The CORRECT verdicts are: correctness → done (discover→read→"+
			"synthesize is a sound approach with no necessary action missing); completeness → done (the steps cover the "+
			"whole request); verification → abstain (nothing to verify yet). Revising this plan — to add acceptance "+
			"criteria, a verify step, or more detail — would be WRONG. Hold real plans to exactly this bar.\n\n"+
			"SEPARATELY, through your lens, propose this task's COMPLETION CRITERIA in `criteria`: a short list (1-3) of "+
			"concrete done-conditions used to judge the FINISHED work later (e.g. a file/output that must exist, a check "+
			"that must pass). These are NOT steps the plan must contain, and their absence from the plan is NEVER a "+
			"reason to revise. Keep each item one short line; omit if your lens adds nothing.\n\n"+
			"Respond with ONLY a JSON object, no prose, no code fence:\n"+
			`{"decision":"done|continue|abstain","confidence":0.0-1.0,"rationale":"one sentence","feedback":"the specific fix (only if continue)","criteria":["..."]}`,
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
	section("Diff", req.Diff)
	if req.NoChanges {
		b.WriteString("# Changes\n(none recorded — a read-only / investigation / answer turn: no diff or signals to inspect. " +
			"Judge the report's substance against the task; do not treat the absence of a diff as a defect.)\n\n")
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
