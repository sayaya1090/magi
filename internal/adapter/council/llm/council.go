// Package llm implements the Council port by polling each member over an
// LLMProvider in parallel, parsing a JSON verdict from each, and tallying them
// with the pure core/council rules. It is the I/O half of the termination gate;
// the consensus half lives in core/council and is provider-agnostic.
package llm

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

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
// memberDeadline bounds one member's vote.
//
// Generous, because a council member is a model reading a turn's whole evidence and the cost of
// cutting a slow one off is a worse verdict. It is here to stop a turn dying, not to hurry anybody.
// A var and not a const so a test can lower it: the thing being tested is that a silent member is
// survived, and waiting three real minutes to see that is not a test anybody runs.
//
// The prefill allowance rides on top, from the same knob the main loop's first-token bound reads
// (MAGI_FIRST_TOKEN; this package cannot import internal/app, so the env is read again here). A
// member's vote begins with prefilling the whole evidence prompt, and on the slow local backend
// that bound exists for — measured ~3min to first token — a flat 3-minute deadline was spent
// ENTIRELY in prefill: every member "did not answer", every vote became an abstention, and the
// finish gate degenerated into a rubber stamp on exactly the hardware that most needs it. Members
// on a fast backend still answer in seconds; the deadline only binds on the pathological case,
// where a wrong abstain is worse than a longer wait.
var memberDeadline = 3*time.Minute + firstTokenAllowance()

// firstTokenAllowance mirrors internal/app's firstTokenTimeout default and env override — see
// memberDeadline for why it is read here rather than imported.
func firstTokenAllowance() time.Duration {
	if v := strings.TrimSpace(os.Getenv("MAGI_FIRST_TOKEN")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 300 * time.Second
}

func (c *Council) Deliberate(ctx context.Context, req port.DeliberationRequest) (council.Deliberation, error) {
	members := req.Members
	if len(members) == 0 {
		members = council.DefaultMembers()
	}
	rule := req.Rule
	if rule == "" {
		rule = council.DefaultRule
	}

	// A member that never answers is an abstention, not a hang.
	//
	// This waited on wg with no bound anywhere: one member's request going quiet — a backend that
	// accepted the connection and then said nothing — held Deliberate open forever. The tool call
	// that convened the council sat in the transcript with no result and the turn stopped, which is
	// exactly what was reported. The main stream got a stall guard for this shape; the council's
	// side calls never did.
	//
	// The council already knows what to do with a member that did not vote: abstain is a decision,
	// the breakdown counts it, and the rule reaches a verdict on the rest. So the bound turns a
	// stopped turn into a recorded abstention with a reason on it.
	pollCtx, cancelPoll := context.WithTimeout(ctx, memberDeadline)
	defer cancelPoll()

	// One call for the whole council. See panel.go: the polls ARE the council's cost, and two of
	// every three re-sent the same evidence to say the same thing about it. A council of one has
	// nothing to batch and keeps the single-member path.
	if len(members) > 1 && samePanelBackend(members) {
		verdicts, cl := c.pollPanel(pollCtx, req, members)
		// Overwritten, not filled in. A council cut off by the deadline comes back through the
		// ordinary failure path with "the panel did not return a verdict" on it — which is a claim
		// about what it said, and it never said anything. "Did not answer" and "could not be
		// understood" are different facts about a round, and a tally read back later has to be able
		// to tell them apart. One call means one deadline for all three, where three calls could
		// lose one member and keep two: that is the price of the shape, and it is recorded rather
		// than hidden behind a wording that suggests a reply arrived.
		if pollCtx.Err() != nil {
			for i := range verdicts {
				// Only a slot nobody answered. The deadline covers the panel call AND the closing
				// call, so a round whose verdicts arrived whole and whose CLOSE ran long used to
				// have its genuine abstentions rewritten as "the council did not answer" and
				// marked Silent — a member who deliberated and declined, recorded as a backend
				// that was unreachable. Silent is still set on the placeholders, which is what
				// the sweep is for.
				if verdicts[i].Decision == council.Abstain && verdicts[i].Silent {
					verdicts[i].Rationale = "the council did not answer within " + memberDeadline.String()
				}
			}
		}
		if req.OnVerdict != nil {
			for _, v := range verdicts {
				req.OnVerdict(v)
			}
		}
		d, err := c.finish(ctx, req, members, verdicts, rule)
		// Log what the closing call said EVERY time, not only when it changed something: an arm that
		// sees no "held back" line cannot tell a conclusion that agreed from one that never ran.
		if cl.Decision != "" {
			verb := "agreed with"
			if cl.Decision != d.Decision {
				verb = "DISAGREED with"
			}
			fmt.Fprintf(os.Stderr, "magi: the council's closing call %s the tally (%s): %s\n",
				verb, cl.Decision, clipWalk(cl.Rationale))
		}
		// The conclusion is clamped to one direction. It may turn a done into a continue — that is
		// the case it exists for, a lone dissent grounded in a requirement the task states, which
		// majority rule deletes by arithmetic. It may never do the reverse: this council's measured
		// failure is over-approval, and a conclusion free to overrule a blocking tally would be a
		// second road to done rather than a check on the first.
		if err == nil && cl.Decision == council.Continue && d.Decision == council.Done {
			d.Decision = council.Continue
			if d.Feedback == "" {
				d.Feedback = cl.Feedback
			}
			if d.Feedback == "" {
				d.Feedback = cl.Rationale
			}
		}
		// What it said travels with the round whether it agreed or not, and whether or not it
		// changed the decision. Agreement is worth reading too: it is the only line in the record
		// that says the three walks were read against each other and held up.
		if err == nil {
			d.Close = closeSaid(cl)
		}
		return d, err
	}

	verdicts := make([]council.Verdict, len(members))
	var wg sync.WaitGroup
	var notify sync.Mutex // OnVerdict does I/O; serialize it rather than require callers to
	for i, m := range members {
		wg.Add(1)
		go func(i int, m council.Member) {
			defer wg.Done()
			v := c.poll(pollCtx, req, m)
			// Overwritten, not filled in. A member cut off by the deadline came back through the
			// ordinary failure path with "unparseable council reply" on it — which is a claim
			// about what it said, and it never said anything. "Did not answer" and "could not be
			// understood" are different facts about a round, and a tally read back later has to be
			// able to tell them apart.
			if pollCtx.Err() != nil {
				v.Decision = "abstain"
				v.Rationale = "did not answer within " + memberDeadline.String()
			}
			verdicts[i] = v
			if req.OnVerdict != nil {
				notify.Lock()
				req.OnVerdict(v)
				notify.Unlock()
			}
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
	// easier, so it can't manufacture a coin-flip approval. Also skipped on unanimity (no split).
	return c.finish(ctx, req, members, verdicts, rule)
}

// finish runs the rebuttal round when the split warrants it and assembles the deliberation. Both
// polling shapes end here, so a panel round is debated, tallied and reported exactly as a split
// round is — the shape of the ASKING is the only thing that differs.
func (c *Council) finish(ctx context.Context, req port.DeliberationRequest, members []council.Member,
	verdicts []council.Verdict, rule council.Rule) (council.Deliberation, error) {
	var debate *council.DebateOutcome
	if indepDec, _ := council.Tally(verdicts, rule); req.Debate &&
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
	// Bounded exactly like the independent round: a member whose backend accepts the rebuttal request
	// and then goes silent must not wedge Deliberate to the turn's wall clock. Passing the raw turn
	// ctx here (as it once did) reopened the very hang the round-1 deadline was written to close — the
	// turn ctx does not fire until the wall clock even for a well-behaved, cancellation-honouring
	// provider. A member cut off keeps the vote it already cast: it revised nothing.
	rctx, cancel := context.WithTimeout(ctx, memberDeadline)
	defer cancel()
	out := make([]council.Verdict, len(members))
	var wg sync.WaitGroup
	for i, m := range members {
		wg.Add(1)
		go func(i int, m council.Member) {
			defer wg.Done()
			prior := byName[m.Name]
			peers := peerDigest(indep, m.Name)
			rv := c.pollRebut(rctx, req, m, prior, peers)
			if rctx.Err() != nil {
				rv = prior
			}
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
		v.Decision, v.Silent = council.Abstain, true
		v.Rationale = "no council backend resolved for provider " + m.Provider
		return v
	}
	user := evidence(req)
	// ask streams one member turn for userMsg and parses its reply. Errors (backend down)
	// and parse outcome are surfaced separately so the caller can distinguish "unavailable"
	// (abstain) from "unparseable" (retry once with a JSON-only reminder).
	sys := memberSystem(m, req.Task, req.Keep)
	// The raw reply travels back with the parse outcome: the retry has to be able to name WHICH way
	// the previous one failed, and that is only knowable from the text it failed on.
	ask := func(userMsg string) (memberReply, string, bool, error) {
		stream, err := provider.StreamChat(ctx, port.ChatRequest{
			Model:  model,
			System: sys,
			Messages: []session.Message{{Role: session.RoleUser,
				Parts: []session.Part{{Kind: session.PartText, Text: userMsg}}}},
			Params: map[string]any{"temperature": 0.0},
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
		v.Decision, v.Silent = council.Abstain, true
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
			v.Decision, v.Silent = council.Abstain, true
			v.Rationale = "council member unavailable: " + err.Error()
			return v
		}
		if !ok {
			v.Decision, v.Silent = council.Abstain, true
			v.Rationale = "unparseable council reply"
			return v
		}
	}
	fill := func(r memberReply) {
		v.Decision = decisionOf(string(r.Decision))
		v.Confidence = float64(r.Confidence)
		v.Rationale = string(r.Rationale)
		v.Feedback = string(r.Feedback)
		v.Keep = string(r.Keep)
		v.Cite = strings.TrimSpace(string(r.Cite))
	}
	fill(r)
	// The walk is the reason this council was changed, so it has to be legible after the fact: a
	// verdict whose reasoning exists only inside the model is a verdict nobody can audit. It goes to
	// stderr rather than into the agent-facing block, where it would crowd out the feedback the agent
	// actually acts on.
	if w := strings.TrimSpace(string(r.Checks)); w != "" {
		fmt.Fprintf(os.Stderr, "magi: council %s (%s) walked: %s\n", m.Name, m.Lens, clipWalk(w))
	} else {
		fmt.Fprintf(os.Stderr, "magi: council %s (%s) sent no requirements walk\n", m.Name, m.Lens)
	}
	// `cite` is recorded and shown, not checked. magi used to look each fragment up in the record
	// and downgrade a member to abstain when it could not find it — see the note above
	// citeNoEvidence for what that cost. A member's grounds are still worth having in front of a
	// reader, so the field stays; nothing is decided from it.
	return v
}

// pollRebut re-polls one member in the rebuttal round: same lens/prompt as poll, but
// the evidence carries the peer digest and an instruction to hold or revise. On any
// error or unparseable reply it returns the member's PRIOR verdict unchanged, so the
// rebuttal can only refine consensus, never lose a vote to a flaky re-poll.
// clipWalk bounds the walk written to the log. The walk is an audit record, not the verdict, and a
// member that enumerates thirty requirements should not push the rest of the run out of the log.
func clipWalk(s string) string {
	const n = 1200
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

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
		System:   memberSystem(m, req.Task, req.Keep),
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
	// Cite travels too. Assigned everywhere else a verdict is built (poll), and forgotten here, so
	// EVERY member's grounds vanished after a debate round — and an empty cite is itself worth
	// seeing (council_view), which made "no grounds given" indistinguishable from "grounds
	// discarded in transit".
	v.Cite = strings.TrimSpace(string(r.Cite))
	return v
}

// councilRetryReminder is appended to a member's user message for a single re-poll when its first
// reply could not be read, and it names WHICH failure occurred. The single reminder used to assume
// prose wrapping in every case, which is one of three unrelated defects and not the one seen most:
// a model that emitted a bare object with a mis-nested array was told to strip prose it had never
// written, produced the identical malformation on the retry, and lost its vote anyway. So the
// diagnosis magi already computes for the log is fed back to the model instead of being kept from
// the only party that can act on it.
// panelShapeAsk and memberShapeAsk are what the retry must ask FOR — the reminder was written
// for the single-member shape and reused verbatim by the panel, where obeying it ("send the
// object again with `decision`") produces a single verdict the panel parser cannot read: the
// recovery instructed the shape that guarantees a second failure and lost all three votes.
const (
	memberShapeAsk = "The `decision` field is required and must be exactly \"done\" or \"continue\". " +
		"Send the object again with it."
	panelShapeAsk = "Every lens must appear as its own entry in a top-level `verdicts` ARRAY, and each " +
		"entry needs its own `decision`, exactly \"done\" or \"continue\". One object with a single " +
		"decision is not a panel reply. Send the whole panel again in that shape."
)

func councilRetryReminder(text string) string { return councilRetryReminderFor(text, memberShapeAsk) }

func councilRetryReminderFor(text, shapeAsk string) string {
	const head = "\n\n# Reply format\nYour previous reply could not be read. "
	d := jsonx.Diagnose(text)
	switch {
	case strings.HasPrefix(d, "syntax error"):
		return head + "It DID contain a JSON object, so the problem is not prose around it — the JSON " +
			"itself is malformed:\n" + d + "\nSend the SAME verdict again as one well-formed JSON object. " +
			"Every `[` must be closed by `]` BEFORE the next key begins, every `{` by `}`, and every string " +
			"by its closing quote."
	case strings.HasPrefix(d, "the JSON parses"):
		return head + "The JSON is well-formed but does not carry a verdict: " + d + "\n" + shapeAsk
	default:
		return head + "Reply with ONLY the JSON object — no prose, explanation, or markdown fences " +
			"before or after it."
	}
}

// memberReply is the JSON shape each member is asked to return.
// EVERY field is read through a tolerant type, because Go aborts the whole document on the first
// type mismatch — and here that costs the member's VOTE, recorded as an abstain the tally cannot
// tell from "no opinion". A quoted "0.9", a single criterion given as a bare string, or a decision
// wrapped in a one-element list is not worth losing a verdict over. Decision is validated by
// VALUE further down (decisionOf), so widening the accepted SHAPE cannot let a bad value
// through — it only stops a well-formed vote from vanishing.
type memberReply struct {
	Decision   jsonx.Text   `json:"decision"`
	Confidence jsonx.Number `json:"confidence"`
	Rationale  jsonx.Text   `json:"rationale"`
	Feedback   jsonx.Text   `json:"feedback"`
	Keep       jsonx.Text   `json:"keep"` // advisory: what's already correct (only when asked; MAGI_COUNCIL_KEEP)
	Cite       jsonx.Text   `json:"cite"` // verbatim fragment of the record the verdict rests on, or NO-EVIDENCE
	// Checks is the requirements walk the member writes BEFORE its decision. jsonx.Text is the
	// right type precisely because it never fails: a member that sends the walk as one string, as
	// a list of strings, or not at all must not lose its vote over the shape of a field that
	// decides nothing by itself. A list arrives joined with "; ".
	Checks jsonx.Text `json:"checks"`
}

// The judging instruction is ONE text, held in pieces so that every shape that asks for a verdict
// asks for the SAME judgement.
//
// It briefly existed twice — one caller carried it in full, another carried a shortened retelling
// — and the two were then compared on a bench as though only their evidence differed. They did not:
// one had been given a paraphrase of the other's rules, so nothing the comparison found could be
// attributed to the thing it was comparing. Shapes may differ in what a member is SHOWN and in how
// many calls it takes to ask. They may not differ in what is asked.

// councilIdentity is the only member-VARYING part: who is speaking and which way it walks.
// Verbs: name, lens, lens description, route.
const councilIdentity = "You are %s, a member of the council that decides whether an AI coding agent's turn is truly finished. " +
	"Your lens is %q: %s\n" +
	"YOUR ROUTE: %s\n"

// councilRouteCaveat keeps a route from hardening into a jurisdiction. Measured: two members once
// deflected a real defect a third had named as "belonging to the correctness lens", and the lens
// that raised it dropped it the next round — the defect fell through the gap between them.
const councilRouteCaveat = "A route is where you look FIRST and hardest — it is NOT the limit of what you may judge. A real defect " +
	"outside your route is still yours to name, and a requirement your route does not touch still gets walked " +
	"below. The three of you read the SAME evidence by DIFFERENT paths so that what one path walks past, " +
	"another meets head-on; a member that judges the work as a single undifferentiated impression has " +
	"contributed the same vote twice.\n\n"

// councilCore is the judging body and the requirements walk. Carries one verb: the optional keep
// clause that sits at its end.
const councilCore = "Judge the agent's REPORT against the TASK and PLAN. Use the SIGNALS and DIFF as evidence WHEN PRESENT — " +
	"but the ABSENCE of a diff or signal is NEVER a reason to continue. Many turns (investigation, reading, " +
	"answering, analysis) legitimately produce no diff; demanding one is exactly the reflexive churn to avoid.\n" +
	"Choose exactly one vote:\n" +
	"- \"done\": through your lens, the report reasonably satisfies the task. Judge it on its merits — if there is " +
	"evidence it should back the claim; if there is none, the task simply didn't call for any.\n" +
	"First decide what the deliverable IS, from the USER'S TASK — not from the plan's or criteria's wording. If the " +
	"user asked to CREATE, SAVE, BUILD, RUN, IMPLEMENT, FIX, ADD, or otherwise modify something concrete (a named file, a building/running program, " +
	"a service, a specific output), then \"done\" requires evidence — in the REPORT, the DIFF, the SIGNALS, or WHAT THE " +
	"TURN PRODUCED (its tool results, e.g. a write that reported bytes written or a command that shows the file's " +
	"content) — that it actually exists; a confident claim or a description of it is NOT itself the artifact — if that " +
	"evidence is absent, vote continue and name the missing artifact in feedback. (A missing git DIFF alone is not " +
	"decisive: the workdir may not be a git repo — weigh the produced tool results instead.) A tool's [ok]/exit-0 " +
	"status is NOT itself proof — the result must SHOW the artifact (its bytes or content); an echo, ls, or command " +
	"that does not reveal the artifact proves nothing, and the agent's own narration is a claim, never evidence. " +
	"In particular a statement that the task is \"done\", \"complete\", \"verified\", or that \"all tests passed\" is " +
	"the agent's OWN CLAIM — do NOT accept it as proof; judge only the executed check results, tool outputs, and diff. " +
	"A test the agent wrote for itself is the weakest evidence there is: it shows the code does what its author " +
	"expected, which is the thing in doubt. Where the " +
	"TASK states an exact contract — a literal command line, a concrete input→output example, a numeric threshold — " +
	"do not vote done unless the evidence SHOWS that EXACT command was run and its output matched the stated value; a " +
	"substitute that merely looks equivalent, or the agent's own test standing in for the task's stated one, is a " +
	"common false done: unanimous here, rejected where it counts. " +
	"Otherwise — a read, " +
	"review, analyze, explain, or answer " +
	"task — the deliverable IS the answer or review in the REPORT itself: judge its substance, and never demand a " +
	"file, diff, or document. A plan step or criterion phrased as \"write/produce a summary\" for such a task is " +
	"satisfied by that content in the report, not by a separate file. Files the agent merely READ or cited are " +
	"INPUTS, never missing deliverables — never fault it for not \"creating\" one.\n" +
	"For such an analyze / review / survey task over a LARGE set (many files or items), \"done\" means the report " +
	"covers the task's scope REASONABLY and representatively — a well-organized answer hitting the important, " +
	"priority-ranked findings is COMPLETE. Do NOT demand EXHAUSTIVE enumeration of every item, or atom-level " +
	"precision (every file, every exact line number), unless the TASK EXPLICITLY asked for it: \"cover more items\" " +
	"or \"be more precise\" is a wish for more proof, not a concrete defect, and voting continue for it is exactly " +
	"the churn to avoid. Vote continue on such a report ONLY for a REAL defect — a whole REQUESTED part is missing, " +
	"or the report's own content is wrong or self-contradictory. This proportionality relaxes ONLY the BREADTH " +
	"expected of a genuine analysis/survey answer. It does NOT apply to any CREATE/BUILD/RUN/FIX PART of the task: " +
	"even when the task is MOSTLY analysis, if part of it is to write/build/run/fix something concrete, that part " +
	"still owes the existence, correctness, and run-the-check evidence below — never treat a never-written file, a " +
	"never-run check, or a wrong value as \"covered\" just because the analysis around it reads as representative.\n" +
	"Beyond existence, check CORRECTNESS against the LETTER of the task: when the task dictates the deliverable's " +
	"exact content, value, format, name, or location, compare what the turn ACTUALLY produced (its shown " +
	"content/bytes/tool results) against that literal requirement. A deliverable that exists but whose content " +
	"does not match what was asked — e.g. writing a file's NAME where the task asked for the word/content INSIDE " +
	"it, a placeholder, a wrong field, or the right shape with the wrong value — is a concrete defect: vote " +
	"continue and name the mismatch in feedback. Re-read the task wording literally; the agent's own paraphrase " +
	"of what it did is a claim, never proof the content is right.\n" +
	"Existence is not correctness: when the task implies a CHECKABLE behavior — a password that must unlock " +
	"something, a service that must respond, a command that must produce a required output, a build that must " +
	"compile — \"done\" requires that the turn actually RAN that check and its REAL output is visible in the " +
	"SIGNALS, tool results, or report. An artifact that was produced but never exercised is unverified: vote " +
	"continue and name the OBJECTIVE still to be shown true — WHAT must hold — and leave HOW to the agent. Do " +
	"NOT prescribe a specific inspection command (ps/netstat/lsof/curl/pgrep): the tool may be absent in this " +
	"environment, and then a met objective reads as forever-unmet. If the turn ALREADY shows the behavior " +
	"working end-to-end — a functional exercise that succeeded, e.g. a client call that got the correct " +
	"response, a build that ran, a query that returned the required row — that IS the run: accept it as " +
	"satisfying the must-respond/must-run bar. Demanding an additional process listing or port scan on top of a " +
	"passing end-to-end exercise is exactly the ritual churn to avoid; a working round-trip is STRONGER evidence " +
	"than a process listing. A PASSING functional test — an in-process or automated exercise that drove the " +
	"deliverable through the same interface its consumer uses and showed the behavior holding — likewise counts " +
	"as that run: do NOT dismiss it as \"mere simulation\" in order to demand a HARDER real-world reproduction the " +
	"TASK never asked for (a real external signal, live hardware, a network peer, a production deployment, manual " +
	"operation). Acceptance may be stricter about whether the CONTRACTED behavior actually works — that is " +
	"NOT licence to invent a stricter ACCEPTANCE METHOD than the task implies. (This never rescues a behavior only " +
	"CLAIMED, faked, or never actually run — that stays continue; it forbids only piling a heavier proof ceremony " +
	"onto a behavior already shown working.) (This does not relax a literal contract the TASK itself stated — that " +
	"still owes the exact command and value above.) Unanimous confidence is not a substitute for one real run.\n" +
	"Correctness also covers PREMISES the deliverable rests on. When its correctness depends on an EXTERNAL " +
	"FACT the agent could not confirm — a `self-check/unverified-premise` signal is present (a knowledge lookup " +
	"failed and was never recovered), or the report shows a key value was assumed, recalled, or inferred rather " +
	"than looked up/tested — treat that fact as UNVERIFIED. A deliverable in exactly the right shape and format " +
	"built on an unconfirmed domain fact (an API detail, a constant, a sequence, a spec, an identifier) is NOT " +
	"done: you cannot certify such a fact from reasoning alone, so do NOT vote done on faith that it is probably " +
	"right — vote continue and tell the agent to verify the fact or state the uncertainty plainly instead of " +
	"asserting it as settled.\n" +
	"A report that RATIONALIZES incompletion is NOT done. If the report's own words say a required part was " +
	"impossible, skipped, never actually run, only inferred from documentation or reasoning, or \"needed no " +
	"work\" without shown verification, that is an ADMISSION the deliverable was not produced or confirmed — " +
	"no matter how reasonable the excuse or how confident the framing (\"this constitutes full completion\", " +
	"\"honest acknowledgment of limitations\", \"nothing to fix\"). An eloquent justification never converts " +
	"unfinished work into done: vote continue, and in feedback tell the agent to either actually perform and " +
	"verify that part, or finish honestly by reporting the task as failed/blocked instead of done.\n" +
	"The REPORT leads with a `STATUS:` line and may carry labeled sections. When an `EVIDENCE:` section is " +
	"present it is where the run-the-check proof should be — but it is still the agent's transcription: accept " +
	"it only when the SIGNALS or tool results corroborate that run; an EVIDENCE line that merely restates the " +
	"claimed output with no tool result showing it is still a claim, not proof. When a `DEVIATIONS:` section is " +
	"present on a done report, the agent has ITSELF flagged assumptions, workarounds, or boundaries it could " +
	"not hold — read each one: if any names an unmet requirement, an unverified premise, or a task constraint " +
	"that was crossed, vote continue and cite it; a confident overall framing never neutralizes an exception " +
	"the agent itself recorded.\n" +
	"When the report CONTESTS a prior council demand — a `CONTEST:` line, or the report otherwise arguing a " +
	"specific earlier requirement is already met or impossible exactly as stated — do not ignore it: judge the " +
	"evidence it cites. If a tool result or SIGNAL already shown establishes that requirement, or shows the " +
	"demanded METHOD is unavailable in this environment (a named command absent) while the requirement's " +
	"objective is demonstrably met end-to-end, then that point is settled: do NOT reissue it — either vote done " +
	"or name a DIFFERENT, real defect. Reissuing verbatim a demand the agent has shown is already met or " +
	"impossible-as-stated is exactly the churn to avoid. But a contest only REMOVES that one point; it is NEVER " +
	"itself evidence the whole task is done — judge every other requirement on its merits, and if the contest " +
	"cites no concrete tool output (it merely re-asserts completion), disregard it and keep the demand.\n" +
	"- \"continue\": ONLY when you can name a SPECIFIC, REAL defect through your lens — a FAILING signal, a part of " +
	"the task/plan the report itself shows is unmet, or a concrete error in the work. Put the next step in " +
	"`feedback`. A missing diff or signal is NOT a defect.\n" +
	"- \"abstain\": your lens genuinely cannot judge from what is given. Excluded from the tally.\n\n" +
	"Never invent a defect, never demand evidence the task never required, and never continue out of mere " +
	"uncertainty or a wish for more proof. GROUND every continue demand in the TASK: when the defect you name " +
	"is a SPECIFIC — an exact value, a numeric type or integer width, a version pin, a field or identifier's " +
	"exact spelling or capitalization, a format, or a threshold — you must be able to point to where the TASK " +
	"ITSELF states it, and say which task words require it in `feedback`. If the task does not state that " +
	"specific, it is NOT a defect: demanding it sends the agent churning on a phantom requirement until the " +
	"wall clock, on work that may already be correct. A plan or criteria phrasing that introduced a specific " +
	"the task never stated does NOT license the demand — defer to the task's literal wording. When the turn " +
	"changed no files, judge the report's SUBSTANCE against the task — the absence of a diff is not itself a " +
	"defect, but a wrong or incomplete answer still is.\n\n" +
	// The walk used to fire only when the acceptance criteria arrived as a NUMBERED checklist. The
	// tasks this council judges do not number theirs, so the one rule that forced item-by-item
	// judgement never ran and each member voted against the work as an undivided impression. That
	// history belongs in this comment and not in the prompt: the member is being asked to judge a
	// turn, not to read magi's changelog.
	"REQUIREMENTS WALK — write it BEFORE you decide, as the FIRST field of your reply.\n" +
	"Enumerate the requirements the TASK ITSELF states — one line each, in the task's own words, walking them " +
	"in the order YOUR ROUTE gives. For each write three things: the requirement, then SATISFIED or " +
	"UNSATISFIED, then the VERBATIM fragment of what you were shown that settles it — or NO-EVIDENCE when " +
	"nothing you were shown speaks to it either way. List only what the TASK states: a requirement it never " +
	"stated is a phantom, and the grounding rule above governs it. Keep the walk to the requirements that " +
	"decide the outcome — a walk too long to read is a walk nobody uses.\n" +
	"WHAT MAY SETTLE AN ITEM: the fragment you write for a requirement must be something a TOOL " +
	"RETURNED — its output, the bytes it printed, the content it revealed. A line the AGENT wrote " +
	"about its own work never settles anything, and the most misleading form of it is a success " +
	"banner printed by a script the agent wrote for the occasion: a short verdict word standing in " +
	"for the comparison it claims to have made. That word is the agent saying so, one step removed. " +
	"Cite the output the comparison produced, or mark the item NO-EVIDENCE. Measured: three members " +
	"once settled a whole task on one such banner, and both of that task's tests then failed.\n" +
	"THEN decide, and the decision must FOLLOW from the walk you just wrote:\n" +
	"- any requirement UNSATISFIED → continue, and name that requirement in `feedback`;\n" +
	"- a requirement supported only by NO-EVIDENCE is NOT satisfied — treat it as UNSATISFIED — unless the " +
	"task never called for evidence at all (a read / answer / analyze / review task, where the report's " +
	"substance IS the deliverable and the proportionality rule above applies);\n" +
	"- every requirement SATISFIED → done.\n" +
	"Write the walk first and the decision after it. A decision written before the walk is a walk written to " +
	"fit the decision. (When the criteria ARE an enumerated checklist, the walk is that checklist, item by " +
	"item, by number — never wave a partly-met checklist to done as a whole.)\n\n" +
	"%s"

// councilGrounds asks for the citation and fixes the reply shape. Verbs: the no-evidence token,
// the schema.
const councilGrounds = "GROUNDS: put in `cite` the fragment your verdict rests on, COPIED VERBATIM from what you were " +
	"given above — a line of tool output, a line of the report, a line of the task. A command's TEXT " +
	"is not its output: the script in a heredoc, and the strings it would print, are what was SENT — what " +
	"came back is the output line beneath it. If your verdict rests on the report's substance rather than " +
	"on anything you observed, put %s in `cite`; that is a normal answer and costs you nothing. " +
	"(`cite` and the walk use that token for different things: in `cite` it means your verdict rests on " +
	"substance rather than on an observation, which is fine; inside the walk it means one requirement has " +
	"nothing showing it was met, which is not.)\n" +
	"Respond with ONLY a JSON object, no prose, no code fence:\n" +
	"%s"

// The core speaks of "the REPORT", "the SIGNALS" and "the DIFF"; this says what they are. It is a
// separate piece from the core because the core is about JUDGING and this is about what the member
// is looking at — the one thing a future evidence shape would legitimately need to change.
const orientAssembled = "You are shown an assembled block below: the TASK, the PLAN, the agent's " +
	"REPORT, the SIGNALS from its turn, and a DIFF when one exists.\n\n"

// verdictSchemaFor is the reply shape, and it is one function so that the two modes cannot drift
// into asking for different fields. `checks` leads and `decision` follows it: a schema that asks
// for the verdict first gets the verdict first, written before the model has read a single
// requirement back to itself.
func verdictSchemaFor(keep bool) string {
	s := `{"checks":["<requirement> - SATISFIED|UNSATISFIED - <verbatim fragment, or NO-EVIDENCE>", "..."],` +
		`"decision":"done|continue|abstain","confidence":0.0-1.0,"rationale":"one sentence",` +
		`"feedback":"the specific gap (only if continue)",` +
		`"cite":"verbatim fragment of what you were shown, or NO-EVIDENCE"`
	if keep {
		s += `,"keep":"what's already correct through your lens — name it, don't restate it; advisory, optional"`
	}
	return s + "}"
}

// memberSystem is the member's system prompt in ASSEMBLED-EVIDENCE mode: who it is, what it is
// looking at, and the shared judging instruction.
//
// There used to be four of these, one per phase — a plan audit, a contract gate, a substitution
// review, and this. The other three judged something magi authored BEFORE the work existed, and
// went with it. What a member judges now is a finished turn against the task, which is the only
// phase that was ever asked about the work itself.
func memberSystem(m council.Member, task string, keep bool) string {
	return withLangNote(fmt.Sprintf(
		councilIdentity+councilRouteCaveat+orientAssembled+councilCore+councilGrounds,
		m.Name, m.Lens, lensOf(m), council.RouteFor(m.Lens), keepClauseFor(keep), citeNoEvidence,
		verdictSchemaFor(keep)), task)
}

func lensOf(m council.Member) string {
	if l := council.Lenses[m.Lens]; l != "" {
		return l
	}
	return "Judge whether the task is genuinely complete."
}

// keepClauseFor is the optional advisory (MAGI_COUNCIL_KEEP): a member also names what the report
// ALREADY gets right through its lens, so the agent doesn't revert a correct part or re-verify a
// settled one. It never changes the vote — feedback still drives continue.
//
// No worked example here on purpose: a concrete-looking one ("the parser change in lexer.c…") was
// copied VERBATIM into verdicts by a weaker model, in runs with no such file — observed twice in
// one wave. The grounding clause replaces it: a keep that names nothing shown is a keep to leave
// empty.
func keepClauseFor(keep bool) string {
	if !keep {
		return ""
	}
	return "Also, through YOUR lens ONLY, note in `keep` what the report ALREADY gets right that the agent " +
		"must NOT redo or revert. NAME it in one short line — a file, function or behavior exactly as it appears " +
		"in what you were shown; if the name you are about to write does not appear there, leave `keep` empty. " +
		"The agent is holding the work, so do not restate it. This is purely advisory: it NEVER changes your " +
		"decision, and you still name any real defect in `feedback`. Leave `keep` empty if nothing is clearly " +
		"settled through your lens; never affirm something you cannot verify.\n"
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
	section("The agent’s own plan (its todo list, not a contract anyone else authored)", req.Plan)
	section("Agent's report (the claim)", req.Report)
	// The identifiers the task NAMES, and whether each is in what the turn wrote. A comparison of
	// two things the members already have, made for them because it is the one a reader makes only
	// if they think to make it — see literals.go for the false done that showed they do not.
	if literalsEnabled() {
		section("Identifiers the task names that the work does not contain",
			literalsSection(req.Task, req.Changes+"\n"+req.Actions))
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
	if b.Len() == 0 {
		return "No task, report, or evidence was provided. With nothing to judge through your lens, abstain."
	}
	return strings.TrimSpace(b.String())
}

// decisionOf maps a member's free-form decision string to a Decision. An
// unrecognized but parsed value resolves to Continue (the gate never finishes on
// an ambiguous vote).
// decisionWord is decisionOf plus whether the word was RECOGNISED. Callers where an unknown word
// must not become a vote (the panel's close, a verdict with no decision) ask this one; callers
// where the safe default is `continue` keep asking decisionOf.
func decisionWord(s string) (council.Decision, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "done":
		return council.Done, true
	case "continue":
		return council.Continue, true
	case "abstain":
		return council.Abstain, true
	}
	return council.Continue, false
}

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
