// Package llm implements the Council port by polling each member over an
// LLMProvider in parallel, parsing a JSON verdict from each, and tallying them
// with the pure core/council rules. It is the I/O half of the termination gate;
// the consensus half lives in core/council and is provider-agnostic.
package llm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/lang"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/jsonx"
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
	// the focused re-round (no fresh "done" to challenge there).
	if req.Devil && !req.DeltaRound {
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
	Addressed judgeBool `json:"addressed"`
	Reason    string    `json:"reason"`
}

// judgeBool reads the boolean shapes a model actually emits for the verdict field — bare
// true/false, the same word quoted, yes/no. A strict bool rejects the whole object over that
// one field, and since this call fails OPEN the reply then lands as the OPPOSITE of what the
// judge said, with its reason discarded too. Observed: a syntactically valid, fully delivered
// {"addressed": "false", "reason": "<names the step the revision omitted>"} recorded as
// "unparseable" and reported as addressed=true.
//
// `known` separates "the judge said no" from "no verdict arrived": an unreadable value must keep
// failing open (a flaky judge must never cut a productive re-plan loop), while a readable false
// is a real verdict and has to survive as one.
type judgeBool struct {
	val   bool
	known bool
}

func (v *judgeBool) UnmarshalJSON(b []byte) error {
	switch strings.ToLower(strings.TrimSpace(strings.Trim(string(b), `"`))) {
	case "true", "yes", "y", "1":
		*v = judgeBool{val: true, known: true}
	case "false", "no", "n", "0":
		*v = judgeBool{val: false, known: true}
	default:
		*v = judgeBool{val: true, known: false} // unreadable → fail open, same as no reply at all
	}
	return nil
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
	text, cut := drain(stream)
	b.WriteString(text)
	if cut != nil {
		fmt.Fprintf(os.Stderr, "magi: a council reply was cut off after %d chars: %v\n", len(text), cut)
	}
	var r judgeReply
	parsed := false
	for _, js := range jsonx.Objects(b.String()) {
		if jsonx.Unmarshal(js, &r) {
			parsed = true
			break
		}
	}
	if !parsed {
		noteUnparsed("a revision-judge reply", b.String())
		return port.RevisionVerdict{Addressed: true, Reason: "unparseable revision-judge reply"}, nil
	}
	if !r.Addressed.known {
		// The object parsed but its verdict field did not (or was absent). Fail open, and say
		// which of the two it was — "unparseable reply" would misdescribe a reply that arrived
		// whole and was merely mis-typed, and that wording sent an earlier hunt after the stream.
		return port.RevisionVerdict{Addressed: true, Reason: "the revision judge gave no readable verdict"}, nil
	}
	reason := strings.TrimSpace(r.Reason)
	if reason == "" {
		reason = "no reason given"
	}
	return port.RevisionVerdict{Addressed: r.Addressed.val, Reason: reason}, nil
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
	sys := memberSystem(m, req.Phase, req.Task, req.Keep, req.Constraints)
	// The raw reply travels back with the parse outcome: the retry has to be able to name WHICH way
	// the previous one failed, and that is only knowable from the text it failed on.
	ask := func(userMsg string) (memberReply, string, bool, error) {
		stream, err := provider.StreamChat(ctx, port.ChatRequest{
			Model:    model,
			System:   sys,
			Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: userMsg}}}},
			Params:   map[string]any{"temperature": 0.0},
		})
		if err != nil {
			return memberReply{}, "", false, err
		}
		var b strings.Builder
		text, cut := drain(stream)
		b.WriteString(text)
		if cut != nil {
			fmt.Fprintf(os.Stderr, "magi: a council reply was cut off after %d chars: %v\n", len(text), cut)
		}
		r, ok := parseReply(b.String())
		if !ok {
			noteUnparsed("a council member's verdict (recorded as an abstain)", b.String())
		}
		return r, b.String(), ok, nil
	}

	r, raw, ok, err := ask(user)
	if err != nil {
		v.Decision = council.Abstain
		v.Rationale = "council member unavailable: " + err.Error()
		return v
	}
	if !ok {
		// An unreadable reply would silently drop this member's vote from quorum and skew the tally
		// with the remaining minority. Give it one focused retry — naming the actual defect, since
		// "strip the prose" is useless advice to a model whose object was bare but malformed — before
		// abstaining. The same remedy the planner already applies to its own JSON replies.
		r, _, ok, err = ask(user + councilRetryReminder(raw))
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
	v.Decision = decisionOf(string(r.Decision))
	v.Confidence = float64(r.Confidence)
	v.Rationale = string(r.Rationale)
	v.Feedback = string(r.Feedback)
	v.Keep = string(r.Keep)
	v.Severity = string(r.Severity) // plan-audit phase only; gates blocking vs advisory
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
		System:   memberSystem(m, req.Phase, req.Task, req.Keep, req.Constraints),
		Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: user}}}},
		Params:   map[string]any{"temperature": 0.0},
	})
	if err != nil {
		return prior
	}
	var b strings.Builder
	text, cut := drain(stream)
	b.WriteString(text)
	if cut != nil {
		fmt.Fprintf(os.Stderr, "magi: a council reply was cut off after %d chars: %v\n", len(text), cut)
	}
	r, ok := parseReply(b.String())
	if !ok {
		noteUnparsed("a council member's re-round verdict (keeping the prior one)", b.String())
		return prior
	}
	v := council.Verdict{Member: m.Name, Lens: m.Lens, Weight: m.Weight}
	v.Decision = decisionOf(string(r.Decision))
	v.Confidence = float64(r.Confidence)
	v.Rationale = string(r.Rationale)
	v.Feedback = string(r.Feedback)
	v.Keep = string(r.Keep)
	v.Severity = string(r.Severity)
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
	text, cut := drain(stream)
	b.WriteString(text)
	if cut != nil {
		fmt.Fprintf(os.Stderr, "magi: a council reply was cut off after %d chars: %v\n", len(text), cut)
	}
	r, ok := parseReply(b.String())
	if !ok {
		return v
	}
	// The devil never carries a done: only a continue with a real, specific defect counts.
	if decisionOf(string(r.Decision)) != council.Continue || strings.TrimSpace(string(r.Feedback)) == "" {
		return v
	}
	v.Decision = council.Continue
	v.Confidence = float64(r.Confidence)
	v.Rationale = string(r.Rationale)
	v.Feedback = string(r.Feedback)
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
		"actually require, or demanding a stricter form than the task requires. Judge it on its merits through " +
		"YOUR lens: vote continue ONLY if it names a REAL, task-required defect that genuinely leaves the work " +
		"unfinished; if the work satisfies the task despite the concern, HOLD done. Do not defer to the devil, " +
		"and do not raise a brand-new objection of your own. Reply in the SAME JSON shape."
	stream, err := provider.StreamChat(ctx, port.ChatRequest{
		Model:    model,
		System:   memberSystem(m, req.Phase, req.Task, req.Keep, req.Constraints),
		Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: user}}}},
		Params:   map[string]any{"temperature": 0.0},
	})
	if err != nil {
		return prior
	}
	var b strings.Builder
	text, cut := drain(stream)
	b.WriteString(text)
	if cut != nil {
		fmt.Fprintf(os.Stderr, "magi: a council reply was cut off after %d chars: %v\n", len(text), cut)
	}
	r, ok := parseReply(b.String())
	if !ok {
		return prior
	}
	v := council.Verdict{Member: m.Name, Lens: m.Lens, Weight: m.Weight}
	v.Decision = decisionOf(string(r.Decision))
	v.Confidence = float64(r.Confidence)
	v.Rationale = string(r.Rationale)
	v.Feedback = string(r.Feedback)
	v.Keep = string(r.Keep)
	return v
}

// councilRetryReminder is appended to a member's user message for a single re-poll when its first
// reply could not be read, and it names WHICH failure occurred. The single reminder used to assume
// prose wrapping in every case, which is one of three unrelated defects and not the one seen most:
// a model that emitted a bare object with a mis-nested array was told to strip prose it had never
// written, produced the identical malformation on the retry, and lost its vote anyway. So the
// diagnosis magi already computes for the log is fed back to the model instead of being kept from
// the only party that can act on it.
func councilRetryReminder(text string) string {
	const head = "\n\n# Reply format\nYour previous reply could not be read. "
	d := jsonx.Diagnose(text)
	switch {
	case strings.HasPrefix(d, "syntax error"):
		return head + "It DID contain a JSON object, so the problem is not prose around it — the JSON " +
			"itself is malformed:\n" + d + "\nSend the SAME verdict again as one well-formed JSON object. " +
			"Every `[` must be closed by `]` BEFORE the next key begins, every `{` by `}`, and every string " +
			"by its closing quote."
	case strings.HasPrefix(d, "the JSON parses"):
		return head + "The JSON is well-formed but does not carry a verdict: " + d + "\nThe `decision` " +
			"field is required and must be exactly \"done\" or \"continue\". Send the object again with it."
	default:
		return head + "Reply with ONLY the JSON object — no prose, explanation, or markdown fences " +
			"before or after it."
	}
}

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
// EVERY field is read through a tolerant type, because Go aborts the whole document on the first
// type mismatch — and here that costs the member's VOTE, recorded as an abstain the tally cannot
// tell from "no opinion". A quoted "0.9", a single criterion given as a bare string, or a decision
// wrapped in a one-element list is not worth losing a verdict over. Decision and Severity are
// validated by VALUE further down (decisionOf / the severity tiers), so widening their accepted
// SHAPE cannot let a bad value through — it only stops a well-formed vote from vanishing.
type memberReply struct {
	Decision   jsonx.Text   `json:"decision"`
	Confidence jsonx.Number `json:"confidence"`
	Rationale  jsonx.Text   `json:"rationale"`
	Feedback   jsonx.Text   `json:"feedback"`
	Keep       jsonx.Text   `json:"keep"`     // advisory: what's already correct (only when asked; MAGI_COUNCIL_KEEP)
	Severity   jsonx.Text   `json:"severity"` // plan-audit phase: critical|warn|info for a revise vote
}

// memberSystem builds the system prompt for one member: its identity (the theme label) and its
// judging lens (the attribute), plus the strict output contract.
//
// There used to be four of these, one per phase — a plan audit, a contract gate, a substitution
// review, and this. The other three judged something magi authored BEFORE the work existed, and
// went with it. What a member judges now is a finished turn against the task, which is the only
// phase that was ever asked about the work itself.
func memberSystem(m council.Member, phase, task string, keep, constraints bool) string {
	lens := council.Lenses[m.Lens]
	if lens == "" {
		lens = "Judge whether the task is genuinely complete."
	}
	// Optional advisory (MAGI_COUNCIL_KEEP): each member also names what the report already
	// gets right through ITS lens, so the agent doesn't revert a correct part or re-verify a
	// settled one. It never changes the vote — feedback still drives continue.
	keepClause, schema := "", `{"decision":"done|continue|abstain","confidence":0.0-1.0,"rationale":"one sentence","feedback":"the specific gap (only if continue)"}`
	if keep {
		keepClause = "Also, through YOUR lens ONLY, note in `keep` what the report ALREADY gets right that the agent " +
			"must NOT redo or revert. NAME it in one short line — the file, function or behavior (e.g. \"the parser " +
			"change in lexer.c, already tested\"); the agent is holding the work, so do not restate it. This is purely " +
			"advisory: it NEVER changes your decision, and you still name any real defect in `feedback`. Leave `keep` " +
			"empty if nothing is clearly settled through your lens; never affirm something you cannot verify.\n"
		schema = `{"decision":"done|continue|abstain","confidence":0.0-1.0,"rationale":"one sentence","feedback":"the specific gap (only if continue)","keep":"what's already correct through your lens — name it, don't restate it; advisory, optional"}`
	}
	// Scope/boundary verification (MAGI_CONSTRAINT_GATE, default off): OPT-IN because it adds a
	// rejection criterion to a council that already tends to over-reject correct work — measured on an
	// A/B arm before it becomes default. Empty when off (byte-identical baseline prompt).
	scopeClause := ""
	if constraints {
		scopeClause = "SCOPE and BOUNDARY constraints are part of the contract, verified against the DIFF and the produced " +
			"artifact — not just the report's prose. When the task fixes WHAT may change (\"only modify FILE\", \"do " +
			"not touch AREA\", \"leave X unchanged\"), a diff that edits anything the task placed OFF-LIMITS is a " +
			"defect EVEN IF the functional change is correct: vote continue and name the out-of-scope edit. The " +
			"agent's own note that it \"had to\" cross the boundary — or a diff that quietly changes a protected file — " +
			"does NOT license it (this is a common self-acknowledged violation: the reasoning admits \"violating " +
			"constraint\" while the edit stands). Likewise, when the task requires the OUTPUT to contain a specific " +
			"element or end a certain way (a required directive/marker/terminator the deliverable MUST include), or " +
			"forbids a specific action, check the ACTUAL artifact for it — a required structural element the output " +
			"LACKS, or a forbidden action taken, is a concrete defect. Do NOT invent a limit the task never stated — " +
			"assert only a boundary the task itself set.\n"
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
			"But passing checks are NECESSARY, not sufficient: those checks are the council's own approximation of an acceptance "+
			"that may be stricter, and a self-authored 'checkpoint' test the agent wrote is weaker still. Where the "+
			"TASK states an exact contract — a literal command line, a concrete input→output example, a numeric threshold — "+
			"do not vote done unless the evidence SHOWS that EXACT command was run and its output matched the stated value; a "+
			"substitute that merely looks equivalent, or the agent's own test standing in for the task's stated one, is a "+
			"common false done: unanimous here, rejected where it counts. "+
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
			"%s"+
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
			"than a process listing. A PASSING functional test — an in-process or automated exercise that drove the "+
			"deliverable through the same interface its consumer uses and showed the behavior holding — likewise counts "+
			"as that run: do NOT dismiss it as \"mere simulation\" in order to demand a HARDER real-world reproduction the "+
			"TASK never asked for (a real external signal, live hardware, a network peer, a production deployment, manual "+
			"operation). Acceptance may be stricter about whether the CONTRACTED behavior actually works — that is "+
			"NOT licence to invent a stricter ACCEPTANCE METHOD than the task implies. (This never rescues a behavior only "+
			"CLAIMED, faked, or never actually run — that stays continue; it forbids only piling a heavier proof ceremony "+
			"onto a behavior already shown working.) (This does not relax a literal contract the TASK itself stated — that "+
			"still owes the exact command and value above.) Unanimous confidence is not a substitute for one real run.\n"+
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
		m.Name, m.Lens, lens, scopeClause, keepClause, schema), task)
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
	if req.StepsLeft > 0 && req.StepsLeft <= 5 {
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
// noteUnparsed reports a model reply this package could not read. Every such failure here is
// SILENT by construction — a member becomes an abstain the tally cannot tell from "no opinion", and
// the revision judge fails OPEN and waves a rewrite through — so without a line on stderr there is
// no way to tell a model that answered in prose from one whose JSON we mishandled.

// drain accumulates a member/judge reply and reports whether the stream was CUT OFF midway. Every
// reader here used to drop the error event, so a truncated reply arrived as a complete one: the
// member became an abstain the tally cannot tell from "no opinion", and the revision judge — which
// fails OPEN — waved a rewrite through. The partial text is still returned so a lenient parse can
// still succeed on it; the cut is reported separately.
func drain(stream <-chan port.ProviderEvent) (string, error) {
	var b strings.Builder
	var cut error
	for ev := range stream {
		switch ev.Type {
		case port.ProviderText:
			b.WriteString(ev.Text)
		case port.ProviderError:
			cut = ev.Err
		}
	}
	return b.String(), cut
}

func noteUnparsed(what, text string) {
	fmt.Fprintf(os.Stderr, "magi: %s could not be parsed (%d bytes): %s\n", what, len(text), jsonx.Report(text))
}

func parseReply(text string) (memberReply, bool) {
	// Try EVERY balanced object and repair the weak-model defects, because an unparsed verdict is
	// not a neutral outcome here — the member is recorded as ABSTAINING, which is indistinguishable
	// in the tally from "my lens has nothing to add". A stray brace in the reasoning ahead of the
	// real object, or a raw newline inside the multi-line `rationale`/`feedback` prose, silently
	// removed a vote that had in fact been cast.
	for _, js := range jsonx.Objects(text) {
		var r memberReply
		if !jsonx.Unmarshal(js, &r) || strings.TrimSpace(string(r.Decision)) == "" {
			continue
		}
		return r, true
	}
	// Nothing parsed whole. Before giving up, keep whatever arrived BEFORE the defect — because the
	// alternative here is not "a slightly worse verdict", it is NO verdict, and the fields are ordered
	// with the decision first. Observed repeatedly from one model: a criteria array left unclosed
	// before the `checks` key, at byte 563 of 567, discarding a `continue` (once a `critical` one)
	// that had been stated in full at byte 12. The loss is real and one-directional, so it is logged
	// with the defect that forced it rather than folded into a silent success.
	for _, js := range jsonx.Objects(text) {
		cut, ok := jsonx.SalvagePrefix(js)
		if !ok {
			continue
		}
		var r memberReply
		if !jsonx.Unmarshal(cut, &r) || strings.TrimSpace(string(r.Decision)) == "" {
			continue // the decision itself was behind the defect: there is no vote to keep
		}
		noteSalvaged(js, cut)
		return r, true
	}
	return memberReply{}, false
}

// noteSalvaged reports a verdict that was read only by discarding the tail of its own reply. It is a
// SUCCESS with a cost — later fields (criteria, checks) may be missing — so it is reported even
// though nothing failed, and it names the syntax defect so a recurring model-side shape is visible
// as such rather than as an unexplained gap in a member's criteria.
func noteSalvaged(orig, kept string) {
	fmt.Fprintf(os.Stderr, "magi: a council reply was malformed and only its prefix could be read "+
		"(%d of %d bytes kept; fields after the defect are missing): %s\n", len(kept), len(orig), jsonx.Diagnose(orig))
}
