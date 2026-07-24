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

	// Disagreement-triggered rebuttal (debate): the independent vote above kept the
	// members' errors uncorrelated; now let each see the others' reasoning once and
	// hold or revise. It fires ONLY when the split would otherwise FINISH the turn —
	// the independent tally is Done but a minority dissents. That is the case debate
	// exists for: a premature done where one member caught a defect the majority
	// missed (the cross-model report: a real bug flagged, then overruled 2-1). When
	// the tally is already Continue, debate is skipped: the dissent cannot change the
	// outcome (the turn continues regardless), and it must NOT be used to talk a
	// hesitant council INTO done — debate may only make finishing HARDER, never
	// easier, so it can't manufacture a coin-flip approval. Also skipped on unanimity
	// (no split) and the focused re-round (already a targeted re-poll).
	var debate *council.DebateOutcome
	if indepDec, _ := council.Tally(verdicts, rule); req.Debate && !req.DeltaRound &&
		splitVerdicts(verdicts) && indepDec == council.Done {
		before, _ := council.Tally(verdicts, rule)
		revised := c.rebut(ctx, req, members, verdicts)
		changed := 0
		prior := map[string]council.Decision{}
		for _, v := range verdicts {
			prior[v.Member] = v.Decision
		}
		for _, v := range revised {
			if p, ok := prior[v.Member]; ok && p != v.Decision {
				changed++
			}
		}
		after, _ := council.Tally(revised, rule)
		debate = &council.DebateOutcome{Before: before, After: after, Changed: changed}
		verdicts = revised
	}

	// Devil's advocate as a critically-reviewed INPUT, not a veto: the rebuttal above only fires
	// on a SPLIT, so a unanimous (no-split) DONE sails through unchallenged — the premature
	// consensus a devil exists to stress-test. Appoint one adversarial member to argue the
	// strongest case the turn is NOT done; if it raises a concrete concern, the council RE-JUDGES
	// that concern CRITICALLY (the devil deliberately hunts for problems and may overreach or
	// demand what the task never required) and their re-tally — not the devil — decides. A real
	// defect the members missed flips them to continue; a spurious devil concern is rejected and
	// the done stands. The devil never gets a binding vote. Skipped on the focused re-round and in
	// plan audit (no "done" to challenge yet).
	if req.Devil && !req.DeltaRound && req.Phase != "plan" && req.Phase != "contract" {
		if dec, _ := council.Tally(verdicts, rule); dec == council.Done && !splitVerdicts(verdicts) {
			if concern := c.devilConcern(ctx, req); concern != "" {
				verdicts = c.reviewDevil(ctx, req, members, verdicts, concern)
			}
		}
	}

	// Focused re-round: fold the prior round's done votes back in before the rule
	// runs, so the tally still spans the full council even though only the
	// dissenting members were re-polled.
	verdicts = append(verdicts, req.CarriedDone...)

	d := council.Deliberate(req.Round, verdicts, rule)
	d.Debate = debate
	if req.Phase == "plan" || req.Phase == "contract" {
		// Plan audit / contract gate: synthesize the members' proposed completion criteria
		// into the contract the turn will later be judged against, plus any executable
		// deliverable checks (settled by execution at plan time, not re-voted later). In the
		// contract phase this IS the primary artifact (authored before the plan); in the plan
		// phase it is derived alongside the plan review.
		d.Criteria = council.MergeCriteria(verdicts)
		d.Checks = council.MergeChecks(verdicts)
	}
	return d, nil
}

// splitVerdicts reports whether the members disagree — at least one done AND at
// least one continue among the non-abstaining votes. Only a real split triggers the
// rebuttal round; unanimity (or all-abstain) needs no debate.
func splitVerdicts(vs []council.Verdict) bool {
	var done, cont bool
	for _, v := range vs {
		switch v.Decision {
		case council.Done:
			done = true
		case council.Continue:
			cont = true
		}
	}
	return done && cont
}

// rebut runs one rebuttal round: each member sees a compact digest of the OTHER
// members' verdicts and rationales and may hold or change its vote. Members are
// re-polled in parallel, each keeping its own lens/prompt; a member whose re-poll
// fails or is unparseable keeps its round-1 verdict (fail-safe, never lost). The
// digest is built once from the independent verdicts so no member sees a peer's
// already-revised vote (simultaneous rebuttal, not sequential anchoring).
func (c *Council) rebut(ctx context.Context, req port.DeliberationRequest, members []council.Member, indep []council.Verdict) []council.Verdict {
	byName := map[string]council.Verdict{}
	for _, v := range indep {
		byName[v.Member] = v
	}
	out := make([]council.Verdict, len(members))
	var wg sync.WaitGroup
	for i, m := range members {
		wg.Add(1)
		go func(i int, m council.Member) {
			defer wg.Done()
			prior := byName[m.Name]
			peers := peerDigest(indep, m.Name)
			rv := c.pollRebut(ctx, req, m, prior, peers)
			out[i] = rv
		}(i, m)
	}
	wg.Wait()
	return out
}

// peerDigest renders the other members' verdicts+rationales for the rebuttal prompt.
func peerDigest(vs []council.Verdict, self string) string {
	var b strings.Builder
	for _, v := range vs {
		if v.Member == self || v.Decision == council.Abstain {
			continue
		}
		line := "- " + v.Member + " (" + v.Lens + ") voted " + string(v.Decision)
		if r := strings.TrimSpace(v.Rationale); r != "" {
			line += ": " + r
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimSpace(b.String())
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
	// ask streams one member turn for userMsg and parses its reply. Errors (backend down)
	// and parse outcome are surfaced separately so the caller can distinguish "unavailable"
	// (abstain) from "unparseable" (retry once with a JSON-only reminder).
	sys := memberSystem(m, req.Phase, req.Task, req.Keep)
	ask := func(userMsg string) (memberReply, bool, error) {
		stream, err := provider.StreamChat(ctx, port.ChatRequest{
			Model:    model,
			System:   sys,
			Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: userMsg}}}},
			Params:   map[string]any{"temperature": 0.0},
		})
		if err != nil {
			return memberReply{}, false, err
		}
		var b strings.Builder
		for ev := range stream {
			if ev.Type == port.ProviderText {
				b.WriteString(ev.Text)
			}
		}
		r, ok := parseReply(b.String())
		return r, ok, nil
	}

	r, ok, err := ask(user)
	if err != nil {
		v.Decision = council.Abstain
		v.Rationale = "council member unavailable: " + err.Error()
		return v
	}
	if !ok {
		// A verbose model can wrap the required JSON in prose and fail parsing, which would
		// silently drop this member's vote from quorum and skew the tally with the remaining
		// minority. Give it one focused retry demanding JSON only before abstaining — the same
		// remedy the planner already applies to its own JSON replies.
		r, ok, err = ask(user + councilJSONReminder)
		if err != nil {
			v.Decision = council.Abstain
			v.Rationale = "council member unavailable: " + err.Error()
			return v
		}
		if !ok {
			v.Decision = council.Abstain
			v.Rationale = "unparseable council reply"
			return v
		}
	}
	v.Decision = decisionOf(r.Decision)
	v.Confidence = r.Confidence
	v.Rationale = r.Rationale
	v.Feedback = r.Feedback
	v.Keep = r.Keep
	v.Severity = r.Severity // plan-audit phase only; gates blocking vs advisory
	v.Criteria = r.Criteria // plan-audit phase only; empty otherwise
	v.Checks = r.Checks     // plan-audit phase only; per-step executable deliverable checks
	return v
}

// pollRebut re-polls one member in the rebuttal round: same lens/prompt as poll, but
// the evidence carries the peer digest and an instruction to hold or revise. On any
// error or unparseable reply it returns the member's PRIOR verdict unchanged, so the
// rebuttal can only refine consensus, never lose a vote to a flaky re-poll.
func (c *Council) pollRebut(ctx context.Context, req port.DeliberationRequest, m council.Member, prior council.Verdict, peers string) council.Verdict {
	if strings.TrimSpace(peers) == "" {
		return prior // no dissent to consider (all others abstained)
	}
	model := m.Model
	if model == "" {
		model = req.DefaultModel
	}
	if model == "" {
		model = c.model
	}
	provider := c.resolve(m.Provider)
	if provider == nil {
		return prior
	}
	user := evidence(req) + "\n\n# Council disagreement — reconsider once\n" +
		"The council split. Your independent verdict was " + string(prior.Decision) + ". The other members:\n" +
		peers + "\n\nWeigh their reasoning against yours. If a peer identifies a real defect your lens should " +
		"honor, change your vote; if your verdict still holds under their reasoning, keep it and say why. Do " +
		"not defer just to agree, and do not raise brand-new unrelated objections — this is about the concerns " +
		"already on the table. Reply in the SAME JSON shape."
	stream, err := provider.StreamChat(ctx, port.ChatRequest{
		Model:    model,
		System:   memberSystem(m, req.Phase, req.Task, req.Keep),
		Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: user}}}},
		Params:   map[string]any{"temperature": 0.0},
	})
	if err != nil {
		return prior
	}
	var b strings.Builder
	for ev := range stream {
		if ev.Type == port.ProviderText {
			b.WriteString(ev.Text)
		}
	}
	r, ok := parseReply(b.String())
	if !ok {
		return prior
	}
	v := council.Verdict{Member: m.Name, Lens: m.Lens, Weight: m.Weight}
	v.Decision = decisionOf(r.Decision)
	v.Confidence = r.Confidence
	v.Rationale = r.Rationale
	v.Feedback = r.Feedback
	v.Keep = r.Keep
	v.Severity = r.Severity
	v.Criteria = r.Criteria
	v.Checks = r.Checks
	return v
}

// devilAdvocate polls one adversarial member against a would-be DONE. It reads the same evidence
// but under an adversarial system prompt: find the single most likely REAL reason the turn is not
// finished. It returns Continue ONLY with a concrete defect, else Abstain — it never returns Done,
// so it can only make finishing harder. Any error or unparseable/soft reply yields Abstain, so a
// flaky devil call can never, by itself, block a done.
func (c *Council) devilAdvocate(ctx context.Context, req port.DeliberationRequest) council.Verdict {
	v := council.Verdict{Member: "Devil", Lens: "adversary", Decision: council.Abstain}
	model := req.DefaultModel
	if model == "" {
		model = c.model
	}
	provider := c.resolve("")
	if provider == nil {
		return v
	}
	user := evidence(req) + "\n\n# Your charge\nThe council is about to rule this turn DONE. Make the " +
		"strongest case it is NOT — find the single most likely REAL way the deliverable is unmet, unverified, " +
		"or wrong, grounded in the task, report, and signals above."
	stream, err := provider.StreamChat(ctx, port.ChatRequest{
		Model:    model,
		System:   withLangNote(devilSystem, req.Task),
		Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: user}}}},
		Params:   map[string]any{"temperature": 0.0},
	})
	if err != nil {
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
		return v
	}
	// The devil never carries a done: only a continue with a real, specific defect counts.
	if decisionOf(r.Decision) != council.Continue || strings.TrimSpace(r.Feedback) == "" {
		return v
	}
	v.Decision = council.Continue
	v.Confidence = r.Confidence
	v.Rationale = r.Rationale
	v.Feedback = r.Feedback
	return v
}

// devilConcern runs the devil and returns its single strongest concern, or "" when it found no
// real defect. The concern is fed BACK to the council as a critically-reviewed input (reviewDevil),
// never a vote — so a spurious devil argument cannot, by itself, block a done.
func (c *Council) devilConcern(ctx context.Context, req port.DeliberationRequest) string {
	dv := c.devilAdvocate(ctx, req)
	if dv.Decision != council.Continue {
		return ""
	}
	return strings.TrimSpace(dv.Feedback)
}

// reviewDevil re-polls each member with the devil's concern attached, asking them to judge it
// CRITICALLY — the devil deliberately hunts for problems, so a plausible concern the task does not
// actually require must be rejected, not deferred to. Members re-poll in parallel, each keeping its
// lens; a failed/unparseable re-poll keeps that member's prior verdict (fail-safe, never lost). The
// members' re-tally decides — the devil casts no binding vote.
func (c *Council) reviewDevil(ctx context.Context, req port.DeliberationRequest, members []council.Member, prior []council.Verdict, concern string) []council.Verdict {
	byName := map[string]council.Verdict{}
	for _, v := range prior {
		byName[v.Member] = v
	}
	out := make([]council.Verdict, len(members))
	var wg sync.WaitGroup
	for i, m := range members {
		wg.Add(1)
		go func(i int, m council.Member) {
			defer wg.Done()
			out[i] = c.pollDevilReview(ctx, req, m, byName[m.Name], concern)
		}(i, m)
	}
	wg.Wait()
	return out
}

// pollDevilReview re-polls one member to weigh the devil's concern critically. Same lens/prompt as
// poll; on any error or unparseable reply it returns the member's prior verdict unchanged.
func (c *Council) pollDevilReview(ctx context.Context, req port.DeliberationRequest, m council.Member, prior council.Verdict, concern string) council.Verdict {
	model := m.Model
	if model == "" {
		model = req.DefaultModel
	}
	if model == "" {
		model = c.model
	}
	provider := c.resolve(m.Provider)
	if provider == nil {
		return prior
	}
	user := evidence(req) + "\n\n# Devil's-advocate challenge — judge it CRITICALLY\n" +
		"A devil's advocate, tasked with finding ANY reason this turn is not done, argues:\n" + concern +
		"\n\nThe devil deliberately hunts for problems and may OVERREACH — raising a concern the task does not " +
		"actually require, or demanding a stricter form than the grader checks. Judge it on its merits through " +
		"YOUR lens: vote continue ONLY if it names a REAL, task-required defect that genuinely leaves the work " +
		"unfinished; if the work satisfies the task despite the concern, HOLD done. Do not defer to the devil, " +
		"and do not raise a brand-new objection of your own. Reply in the SAME JSON shape."
	stream, err := provider.StreamChat(ctx, port.ChatRequest{
		Model:    model,
		System:   memberSystem(m, req.Phase, req.Task, req.Keep),
		Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: user}}}},
		Params:   map[string]any{"temperature": 0.0},
	})
	if err != nil {
		return prior
	}
	var b strings.Builder
	for ev := range stream {
		if ev.Type == port.ProviderText {
			b.WriteString(ev.Text)
		}
	}
	r, ok := parseReply(b.String())
	if !ok {
		return prior
	}
	v := council.Verdict{Member: m.Name, Lens: m.Lens, Weight: m.Weight}
	v.Decision = decisionOf(r.Decision)
	v.Confidence = r.Confidence
	v.Rationale = r.Rationale
	v.Feedback = r.Feedback
	v.Keep = r.Keep
	return v
}

// councilJSONReminder is appended to a member's user message for a single re-poll when its first
// reply could not be parsed as the required JSON — a verbose model wrapping the object in prose is
// the common cause, and abstaining outright would drop the vote from quorum.
const councilJSONReminder = "\n\n# Reply format\nYour previous reply could not be parsed as the required " +
	"JSON. Reply with ONLY the JSON object — no prose, explanation, or markdown fences before or after it."

// devilSystem is the adversarial member's contract: argue against done, but only on a REAL defect.
const devilSystem = "You are the council's devil's advocate. The other members are ready to rule this AI " +
	"coding agent's turn DONE, and nobody has argued the other side. Your job is to stress-test that " +
	"consensus: assume it is premature and hunt for the single most likely REAL reason the turn is not " +
	"actually finished — a required deliverable that does not exist, exists but was never run/verified, or " +
	"whose content/value/name/format does not match what the TASK literally asked; a checkable behavior never " +
	"exercised; a premise assumed rather than confirmed; a stated part the report itself admits is skipped or " +
	"rationalized away. The agent's own narration (\"done\", \"verified\", \"all tests pass\") is a CLAIM, never " +
	"proof — judge only the shown tool results, signals, and diff.\n" +
	"Vote \"continue\" ONLY if you can name a SPECIFIC, concrete defect and put the exact next step in " +
	"`feedback`. That concern is not a verdict — the council will REVIEW it critically and decide — so raise it " +
	"only if it is real. When that defect is itself a SPECIFIC (an exact value, a numeric type or integer " +
	"width, a version pin, a field or identifier's exact spelling or capitalization, a format, or a threshold), " +
	"you must be able to point to where the TASK ITSELF states it: a specific the task never stated is not a " +
	"real defect but manufactured doubt, so abstain rather than demand it. If, after genuinely trying to break " +
	"it, you find no real defect and would only be " +
	"manufacturing doubt or demanding evidence the task never required, vote \"abstain\". You must NEVER vote " +
	"\"done\": finding nothing is an abstain, not an endorsement. Do not invent defects and do not nitpick breadth " +
	"on an analysis/review answer that already covers the task representatively.\n" +
	"Respond with ONLY a JSON object, no prose, no code fence:\n" +
	`{"decision":"continue|abstain","confidence":0.0-1.0,"rationale":"one sentence","feedback":"the specific defect and next step (required for continue)"}`

// memberReply is the JSON shape each member is asked to return.
type memberReply struct {
	Decision   string   `json:"decision"`
	Confidence float64  `json:"confidence"`
	Rationale  string   `json:"rationale"`
	Feedback   string   `json:"feedback"`
	Keep       string   `json:"keep"`     // advisory: what's already correct (only when asked; MAGI_COUNCIL_KEEP)
	Severity   string   `json:"severity"` // plan-audit phase: critical|warn|info for a revise vote
	Criteria   []string `json:"criteria"` // plan-audit phase: proposed completion criteria
	// Checks are plan-audit per-step executable deliverable checks (empty otherwise).
	Checks []council.DeliverableCheck `json:"checks"`
}

// memberSystem builds the system prompt for one member: its identity (the theme
// label) and its judging lens (the attribute), plus the strict output contract.
// The phase selects whether the member judges a finished turn ("terminate") or a
// proposed procedure ("plan").
func memberSystem(m council.Member, phase, task string, keep bool) string {
	lens := council.Lenses[m.Lens]
	if lens == "" {
		lens = "Judge whether the task is genuinely complete."
	}
	if phase == "contract" {
		return withLangNote(contractMemberSystem(m, lens), task)
	}
	if phase == "plan" {
		return withLangNote(planMemberSystem(m, lens, keep), task)
	}
	// Optional advisory (MAGI_COUNCIL_KEEP): each member also names what the report already
	// gets right through ITS lens, so the agent doesn't revert a correct part or re-verify a
	// settled one. It never changes the vote — feedback still drives continue.
	keepClause, schema := "", `{"decision":"done|continue|abstain","confidence":0.0-1.0,"rationale":"one sentence","feedback":"the specific gap (only if continue)"}`
	if keep {
		keepClause = "Also, through YOUR lens ONLY, note in `keep` what the report ALREADY gets right that the agent " +
			"must NOT redo or revert — a brief phrase (e.g. \"the parser change is correct and tested\"). This is purely " +
			"advisory: it NEVER changes your decision, and you still name any real defect in `feedback`. Leave `keep` " +
			"empty if nothing is clearly settled through your lens; never affirm something you cannot verify.\n"
		schema = `{"decision":"done|continue|abstain","confidence":0.0-1.0,"rationale":"one sentence","feedback":"the specific gap (only if continue)","keep":"what's already correct through your lens — advisory, optional"}`
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
			"In particular a statement that the task is \"done\", \"complete\", \"verified\", or that \"all tests passed\" is "+
			"the agent's OWN CLAIM — do NOT accept it as proof; judge only the executed check results, tool outputs, and diff. "+
			"When a `deliverable-check` signal is present and FAILED, the plan's own executable contract is objectively unmet: "+
			"vote continue no matter how confidently the report asserts success. "+
			"But passing checks are NECESSARY, not sufficient: those checks are the council's own approximation of a HIDDEN "+
			"grader that may be stricter, and a self-authored 'checkpoint' test the agent wrote is weaker still. Where the "+
			"TASK states an exact contract — a literal command line, a concrete input→output example, a numeric threshold — "+
			"do not vote done unless the evidence SHOWS that EXACT command was run and its output matched the stated value; a "+
			"substitute that merely looks equivalent, or the agent's own test standing in for the task's stated one, is a "+
			"common false done (Council 3-0 done, grader 0). "+
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
			"continue and name the OBJECTIVE still to be shown true — WHAT must hold — and leave HOW to the agent. Do "+
			"NOT prescribe a specific inspection command (ps/netstat/lsof/curl/pgrep): the tool may be absent in this "+
			"environment, and then a met objective reads as forever-unmet. If the turn ALREADY shows the behavior "+
			"working end-to-end — a functional exercise that succeeded, e.g. a client call that got the correct "+
			"response, a build that ran, a query that returned the required row — that IS the run: accept it as "+
			"satisfying the must-respond/must-run bar. Demanding an additional process listing or port scan on top of a "+
			"passing end-to-end exercise is exactly the ritual churn to avoid; a working round-trip is STRONGER evidence "+
			"than a process listing. (This does not relax a literal contract the TASK itself stated — that still owes "+
			"the exact command and value above.) Unanimous confidence is not a substitute for one real run.\n"+
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
			"The REPORT leads with a `STATUS:` line and may carry labeled sections. When an `EVIDENCE:` section is "+
			"present it is where the run-the-check proof should be — but it is still the agent's transcription: accept "+
			"it only when the SIGNALS or tool results corroborate that run; an EVIDENCE line that merely restates the "+
			"claimed output with no tool result showing it is still a claim, not proof. When a `DEVIATIONS:` section is "+
			"present on a done report, the agent has ITSELF flagged assumptions, workarounds, or boundaries it could "+
			"not hold — read each one: if any names an unmet requirement, an unverified premise, or a task constraint "+
			"that was crossed, vote continue and cite it; a confident overall framing never neutralizes an exception "+
			"the agent itself recorded.\n"+
			"When the report CONTESTS a prior council demand — a `CONTEST:` line, or the report otherwise arguing a "+
			"specific earlier requirement is already met or impossible exactly as stated — do not ignore it: judge the "+
			"evidence it cites. If a tool result or SIGNAL already shown establishes that requirement, or shows the "+
			"demanded METHOD is unavailable in this environment (a named command absent) while the requirement's "+
			"objective is demonstrably met end-to-end, then that point is settled: do NOT reissue it — either vote done "+
			"or name a DIFFERENT, real defect. Reissuing verbatim a demand the agent has shown is already met or "+
			"impossible-as-stated is exactly the churn to avoid. But a contest only REMOVES that one point; it is NEVER "+
			"itself evidence the whole task is done — judge every other requirement on its merits, and if the contest "+
			"cites no concrete tool output (it merely re-asserts completion), disregard it and keep the demand.\n"+
			"- \"continue\": ONLY when you can name a SPECIFIC, REAL defect through your lens — a FAILING signal, a part of "+
			"the task/plan the report itself shows is unmet, or a concrete error in the work. Put the next step in "+
			"`feedback`. A missing diff or signal is NOT a defect.\n"+
			"- \"abstain\": your lens genuinely cannot judge from what is given. Excluded from the tally.\n\n"+
			"Never invent a defect, never demand evidence the task never required, and never continue out of mere "+
			"uncertainty or a wish for more proof. GROUND every continue demand in the TASK: when the defect you name "+
			"is a SPECIFIC — an exact value, a numeric type or integer width, a version pin, a field or identifier's "+
			"exact spelling or capitalization, a format, or a threshold — you must be able to point to where the TASK "+
			"ITSELF states it, and say which task words require it in `feedback`. If the task does not state that "+
			"specific, it is NOT a defect: demanding it sends the agent churning on a phantom requirement until the "+
			"wall clock, on work that may already be correct. A plan or criteria phrasing that introduced a specific "+
			"the task never stated does NOT license the demand — defer to the task's literal wording. When the turn "+
			"changed no files, judge the report's SUBSTANCE against the task — the absence of a diff is not itself a "+
			"defect, but a wrong or incomplete answer still is.\n\n"+
			"PER-ITEM ACCEPTANCE: when the acceptance criteria are given as a NUMBERED checklist, judge EACH numbered "+
			"item individually against the evidence — for every item decide SATISFIED or UNSATISFIED — and vote "+
			"\"done\" ONLY when EVERY item you can judge is satisfied. If any item is UNSATISFIED, vote continue and "+
			"name the item number(s) and exactly what is missing in `feedback`; never wave a partly-met checklist to "+
			"done as a whole. (When the criteria are not enumerated, judge them as usual.)\n\n"+
			"%s"+
			"Respond with ONLY a JSON object, no prose, no code fence:\n"+
			"%s",
		m.Name, m.Lens, lens, keepClause, schema), task)
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
func planMemberSystem(m council.Member, lens string, keep bool) string {
	keepClause, keepField := "", ""
	if keep {
		keepClause = "Also, through YOUR lens, note in `keep` the plan steps that are ALREADY sound and must survive " +
			"if the plan is revised — do this EVEN WHEN YOU APPROVE, because another member's flaw sends the WHOLE plan " +
			"back to be re-planned, and without this the revision can drop the correct parts your lens already blessed. " +
			"Advisory: it never changes your vote; omit if nothing through your lens is clearly settled.\n\n"
		keepField = `,"keep":"plan steps already sound through your lens that a revision must preserve — advisory, optional"`
	}
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
			"CONTEST: when a prior concern is shown as CONTESTED — the plan author argues the TASK does not "+
			"require what it demanded, or the plan already satisfies it — re-judge that concern against the "+
			"TASK's literal words. If the task genuinely does not demand it, the concern was an OVER-DEMAND: "+
			"DROP it (approve, or name a DIFFERENT, real flaw) and do NOT re-raise it. Uphold it ONLY if you "+
			"can point to the task words that require it. A contested concern you cannot ground in the task is "+
			"not a valid reason to revise — re-issuing it is exactly the churn to avoid.\n\n"+
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
			"names the plan `step` it belongs to as its INTEGER STEP NUMBER (\"3\", not the step's title — the gate matches "+
			"by number, so a title label matches NO step and gets flattened onto the wrong worker), the expected "+
			"`deliverable` in one short phrase, a "+
			"shell `command` that verifies it from the task's working directory, and an optional `expect` REGULAR "+
			"EXPRESSION the command's output must match (omit `expect` for an exit-code-only check). A check that only "+
			"confirms the deliverable can be REACHED — a file exists or is non-empty, a port accepts a connection, a "+
			"module imports, a build succeeds, a process is alive — is a PRECONDITION, not proof, and is INSUFFICIENT "+
			"whenever the deliverable must BEHAVE or produce a correct result, because a non-functional stub passes every "+
			"one of those. You MUST author a check that INVOKES the behavior through the same interface its consumer uses "+
			"and asserts the OUTCOME, choosing the weakest input that still forces the real code path so a stub that "+
			"merely exists or opens the port FAILS. Asserting the artifact is "+
			"present while never exercising it is the single biggest reason a broken solution gets approved: a program that "+
			"builds but computes the wrong answer, a `gates.txt` that exists but simulates to the wrong value, a server "+
			"file that never actually listens, a cleanup handler that never fires. So beyond any existence check, run it, "+
			"grep its contents, or exercise its behavior (`test -s out.txt`, not bare `test -f`) so a stale, empty, or "+
			"WRONG leftover cannot pass. The deliverable may be a file (`test -s out.txt`), a build/test result "+
			"(`go build ./...`), or PROGRAM OUTPUT ON SCREEN — for output, run the program and match its stdout with "+
			"`expect` (e.g. command `./run --demo`, expect `^total: [0-9]+$`). HIGHEST-PRIORITY CHECK: if the task shows "+
			"ANY concrete example — a literal command line, an input→output mapping, or a numeric threshold — your FIRST "+
			"check MUST run that EXACT input and assert that EXACT output, verbatim, never a paraphrase or a value you "+
			"invented; this one check catches a wrong-but-present artifact that every file-existence check misses. The hidden grader enforces the TASK'S own "+
			"contract, so a self-substituted command or expected output passes your check but fails the grader: if the "+
			"task quotes an exact command line, run THAT line unchanged; if it gives an example input→output mapping, "+
			"assert exactly that mapping — never a value you invented. CONVERSELY do NOT demand MORE than the task states: "+
			"never pin a version, build id, exact path, or incidental attribute the task did not itself specify — "+
			"over-specification false-fails correct work and never converges. Assert the minimal condition that proves the "+
			"stated objective. When the task's acceptance involves an EXTERNAL event — a signal "+
			"(Ctrl-C/SIGINT), a kill, a disconnect — author a check that DELIVERS the event for real: launch the "+
			"artifact as a background process, send the actual signal, and match the required output (e.g. command "+
			"`python3 app.py & p=$!; sleep 1; kill -INT $p; wait $p 2>/dev/null; ...`, expect the cleanup marker). "+
			"An in-process simulation (raising the exception by hand) verifies the wrong delivery path. A "+
			"step may have SEVERAL checks (several deliverables). PORTABLE PROBES: the `command` may use ONLY tools "+
			"guaranteed to exist in the task's runtime — coreutils, `grep`/`test`, `python3`, and the task's own "+
			"toolchain. A check that fails merely because its TOOL is absent is a false negative that can NEVER pass, "+
			"trapping the run in a re-verify loop until timeout. In particular, to test that a server LISTENS on a port, "+
			"do NOT use `ss`/`netstat`/`lsof` (routinely missing in minimal images) — use a dependency-free connect, e.g. "+
			"command `python3 -c \"import socket,sys; sys.exit(0 if socket.socket().connect_ex(('localhost',PORT))==0 else 1)\"` "+
			"(or `curl -sf http://localhost:PORT/...` for HTTP). WORK AND CHECK ARE SEPARATE — the verifying command "+
			"must be IDEMPOTENT and cause NO state change: it may be run any number of times with the same result and "+
			"no side effects, because it VERIFIES the deliverable, it never PERFORMS, re-performs, or substitutes for the "+
			"step's own work. A command that PRODUCES or MUTATES the artifact — compress, download, build, generate, move, "+
			"delete (`tar -czf`, `scp`/`rsync`, `rm`, `mv`, a `>` redirect that writes the deliverable, `git commit`) — IS "+
			"the work, NOT a check: running it as the check re-does the step every gate cycle and false-fails on any "+
			"transient error, trapping the run in a redo loop (observed: a download step whose 'check' was "+
			"`ssh host 'tar -czf /tmp/b.tgz DIR'` re-compressed the remote tree, exited non-zero, and re-ran the download "+
			"forever). Instead probe the ALREADY-produced artifact at its FINAL location read-only: to verify a downloaded "+
			"archive use `test -s ./out.tgz` or `tar -tzf ./out.tgz >/dev/null` (LIST, never CREATE); to verify a build, "+
			"RUN the built binary — never re-invoke the build as the check. Verify the step's OWN stated deliverable, not "+
			"an intermediate. Propose checks ONLY when they are concrete and would "+
			"genuinely pass for correct work — commands must be non-destructive and deterministic. SMOKE CHECK when "+
			"full correctness is UNVERIFIABLE: a step that PRODUCES a runnable artifact (a program, script, generated "+
			"file) must ALWAYS get at least a SMOKE check even when you cannot check the exact answer because the "+
			"reference/expected output is hidden — assert the artifact RUNS without error and emits output in the "+
			"expected SHAPE (exists, non-empty, valid JSON/CSV, right column/field count, plausible row count). Skipping "+
			"such a step leaves the run with NO done-signal, so the agent second-guesses a working solution and refines "+
			"it until the wall clock. Example, for a step that writes a script emitting JSON: run the script on its input "+
			"and assert the output PARSES and is non-empty — command `<script> <input> > /tmp/o.json && python3 -c "+
			"\"import json,sys; sys.exit(0 if json.load(open('/tmp/o.json')) else 1)\"` (runs + non-empty) — shape only, "+
			"never the hidden exact values. For a "+
			"read/review/analyze/answer step there is usually nothing to execute: emit NO check for it (the prose "+
			"`criteria` already cover it). CHECKLIST-DRIVEN, JOINTLY SATISFIABLE: author each step together with its "+
			"own done-condition — a step that produces a runnable/inspectable artifact should carry at least one check "+
			"that DEFINES 'done' for it, so the checklist drives the step, not the reverse. A check belongs ONLY to the "+
			"step whose work creates or changes that state and is verified AT that step (label it with that step's "+
			"number); never re-assert it under a later step. The checks, read in plan order, must be JOINTLY "+
			"SATISFIABLE: do NOT require the SAME artifact PRESENT under one step and ABSENT under another as if both "+
			"held at once — a teardown/cleanup step's absence-check (`test ! -f a.tgz`) belongs to THAT step alone and is "+
			"verified right after it, while an earlier step's existence-check (`test -s a.tgz`) already passed at its own "+
			"earlier step; putting both into one step's list makes a checklist no state can satisfy. "+
			"Omit `checks` entirely if your lens has none.\n\n"+
			"%s"+
			"Respond with ONLY a JSON object, no prose, no code fence:\n"+
			`{"decision":"done|continue|abstain","confidence":0.0-1.0,"rationale":"one sentence","feedback":"the specific fix (only if continue)","severity":"critical|warn|info (only if continue)","criteria":["..."],"checks":[{"step":"...","deliverable":"...","command":"...","expect":"..."}]`+keepField+`}`,
		m.Name, m.Lens, lens, keepClause)
}

// contractMemberSystem builds the system prompt for the CONTRACT gate (Phase=="contract"),
// which runs BEFORE the planner. The members author and agree on the turn's acceptance
// contract — completion `criteria` (prose done-conditions the finished work is judged against)
// and executable `checks` (deterministic verifications) — derived from the TASK itself, not
// from any plan. The contract is bounded on BOTH sides: an UPPER bound (necessity — assert only
// what the task states, never an invented version/path/value) and a LOWER bound (sufficiency —
// exercise the contracted behavior, never accept mere existence/reachability of a stub). This
// is the same criteria/checks calibration the plan-audit member applies, applied here first and
// on its own so the plan is later built to satisfy a reviewed contract.
func contractMemberSystem(m council.Member, lens string) string {
	return fmt.Sprintf(
		"You are %s, a member of a council that defines an AI coding agent's ACCEPTANCE CONTRACT for a TASK, "+
			"BEFORE any plan exists. Your lens is %q: %s\n\n"+
			"The contract has two parts, and you propose BOTH through your lens:\n"+
			"- `criteria`: a short list of concrete DONE-CONDITIONS in prose — what must be TRUE for the task to "+
			"count as finished. These are judged by reading the finished work; they cover conditions that cannot be "+
			"reduced to a command (an answer's substance, a behavior's correctness).\n"+
			"- `checks`: executable verifications — each a shell `command` from the task's working directory with an "+
			"optional `expect` REGULAR EXPRESSION its output must match (omit `expect` for exit-code-only). A check is "+
			"the machine-runnable evidence for a criterion, when one can be written.\n\n"+
			"Judge and refine the DRAFT contract (if one is shown) through your lens, then vote:\n"+
			"- \"done\": the contract, through your lens, faithfully and proportionately captures the task — nothing "+
			"required is missing and nothing is over-demanded.\n"+
			"- \"continue\": name a CONCRETE flaw and set `severity` — \"critical\" only when the contract as written "+
			"would accept a WRONG result or reject a CORRECT one (a required condition missing, or an over-demand that a "+
			"correct solution cannot satisfy); \"warn\"/\"info\" for improvements. Put the fix in `feedback` AND emit your "+
			"corrected `criteria`/`checks`.\n"+
			"- \"abstain\": your lens adds nothing to the contract.\n\n"+
			"BOUND THE CONTRACT ON BOTH SIDES:\n"+
			"- LOWER BOUND (sufficiency — do not under-specify): a criterion or check that only confirms the deliverable "+
			"can be REACHED — a file exists, a module imports, a port accepts a connection, a build succeeds, a process is "+
			"alive — is a PRECONDITION, not proof, because a non-functional STUB passes every one. When the task says the "+
			"deliverable must DO something (answer a request, return a value, transform an input, persist state, produce "+
			"an output), the contract MUST invoke that behavior through the same interface its consumer uses and assert "+
			"the OUTCOME, choosing the weakest input that still forces the real code path so a stub FAILS. If the task "+
			"shows a concrete example — a literal command line, an input→output mapping, a numeric threshold — the "+
			"HIGHEST-PRIORITY check runs that EXACT input and asserts that EXACT output, verbatim (never a paraphrase or "+
			"an invented value). When acceptance involves an EXTERNAL event (a signal/Ctrl-C, a kill, a disconnect), the "+
			"check DELIVERS it for real (launch the artifact, send the actual signal, match the required output) — never "+
			"an in-process simulation. When the task gives a stateful interface with a sequence of calls, the check "+
			"REPLAYS that sequence and asserts the stated side effect (a file written, state persisted across calls, an "+
			"endpoint reachable) — not merely that the class imports or is an instance of its base.\n"+
			"- UPPER BOUND (necessity — do not over-specify): assert ONLY what the task itself states. NEVER pin a "+
			"version, build id, exact path, timestamp, or incidental attribute the task did not require — over-"+
			"specification false-fails correct work and never converges. For an analysis/survey/answer task over a LARGE "+
			"set, phrase criteria as the QUALITY of a representative, priority-ranked coverage (\"the main candidates are "+
			"identified with file and rough location\"), never \"every X\" or \"all N with exact line numbers\"; for such "+
			"a task a criterion is a quality of the ANSWER, not a new file to create, and it needs no check.\n\n"+
			"PORTABLE PROBES: a `command` may use ONLY tools guaranteed present — coreutils, `grep`/`test`, `python3`, and "+
			"the task's own toolchain. NEVER use `ss`/`netstat`/`lsof`/`pgrep`/`ps`/`fuser`/`jq` (routinely absent → exit "+
			"127 → false-fails forever); test a listening port with a dependency-free `python3 -c \"import socket,sys; "+
			"sys.exit(0 if socket.socket().connect_ex(('localhost',PORT))==0 else 1)\"` or `curl -sf`. IDEMPOTENT, NO "+
			"STATE CHANGE: a check VERIFIES read-only; it never PERFORMS the work (no build/download/generate/`>`-write/"+
			"`rm`/`mv` as a check). Keep each criterion one short line; emit checks only where machine-checkable and they "+
			"would genuinely pass for correct work; omit a part your lens cannot add to.\n\n"+
			"GOAL, NOT METHOD — and keep it SMALL: a criterion states WHAT must be true, never HOW to build it (leave the "+
			"implementation method to the worker); a check verifies the OUTCOME and must not prescribe a specific tool or "+
			"build step. A SMALL contract of a few essential, high-value conditions is BETTER than an exhaustive one — do "+
			"NOT pad it; if the draft is already sufficient, approve rather than add more.\n\n"+
			"RE-JUDGE CARRIED CONCERNS: when the draft carries a prior round's feedback, do not blindly perpetuate it. "+
			"Check each carried concern against the TASK: if it demands something the task does not require (an over-"+
			"demand), DROP it from the contract rather than encode it — an unjustified concern must not become a "+
			"criterion or check. Keep only what the task's own words support.\n\n"+
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
	if req.Phase == "contract" {
		// Contract gate: only the task exists yet — no plan, no report. Members author and
		// review the acceptance contract (criteria + checks) for the task itself. A draft
		// contract from a prior round, when present, is carried in Plan for refinement.
		section("Task (the goal)", req.Task)
		section("Draft contract so far (refine it)", req.Plan)
		section("A prior concern was CONTESTED as unjustified — re-judge whether the TASK requires it; drop it if not", req.Contest)
		if b.Len() == 0 {
			return "No task was provided; with nothing to contract for, abstain."
		}
		return strings.TrimSpace(b.String())
	}
	if req.Phase == "plan" {
		// Plan audit: only the task and the proposed procedure exist yet.
		section("Task (the goal)", req.Task)
		section("The plan author CONTESTED your prior concern as unjustified — re-judge it against the TASK; if the task truly does not require it, do NOT re-raise it", req.Contest)
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
	if req.Phase != "plan" && req.Phase != "contract" && req.StepsLeft > 0 && req.StepsLeft <= 5 {
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
