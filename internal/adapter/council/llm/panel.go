package llm

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/jsonx"
	"github.com/sayaya1090/magi/internal/port"
)

// The council asks ONE call for all three judgements instead of three calls for one each.
//
// The reason is measured, not aesthetic. Across four benchmark arms the council's cache ratio
// ranked exactly inversely with the number of member polls, with no exceptions: 24 polls gave
// 6.28, 30 gave 4.55, 39 gave 3.85, 42 gave 2.85. Each additional poll cost about one CLI scaffold
// (~46K written, ~48.5K measured) on assembled evidence and about a whole conversation (~79K) on
// context — because every member opens its own backend session and reads nothing from cache. The
// polls are the council's entire cost, and two of every three are re-sending the same evidence.
//
// What is given up is that the three judgements are no longer independent draws: one forward pass
// writes all of them, and a verdict written after another can lean on it. That is the property the
// council exists for, so the prompt spends its words defending it — and on the arm that decided
// this, the defence held: the members split within a single reply more often than they had in any
// split-call arm (four convenings of nine, against three, two, and none), one lens abstained rather
// than agree, and the round twice refused a done. The independence the split shape bought was
// largely notional anyway: at temperature 0 on the same evidence it had produced 18 identical
// verdicts out of 18.
//
// Everything downstream is unchanged — the same three council.Verdict values come out, with the
// same members, lenses and fields, so the tally, the rebuttal round, the events and the UI cannot
// tell the difference.

// panelSchema is one verdict per lens, in a named array. The per-verdict field order is the same
// as the single-member schema's and for the same reason: the walk is written before the decision
// it is supposed to support.
const panelSchema = `{"verdicts":[` +
	`{"member":"<name>","lens":"<lens>",` +
	`"checks":["<requirement> - SATISFIED|UNSATISFIED - <verbatim fragment, or NO-EVIDENCE>", "..."],` +
	`"decision":"done|continue|abstain","confidence":0.0-1.0,"rationale":"one sentence",` +
	`"feedback":"the specific gap (only if continue)",` +
	`"cite":"verbatim fragment of what you were shown, or NO-EVIDENCE"}` +
	`, ...one object for EACH lens listed above, in that order...]}`

const panelKeepField = `,"keep":"what's already correct through your lens — advisory, optional"`

// panelRoster names the lenses and their routes: the part that was per-request in the split shape
// and is now a list inside one request.
func panelRoster(members []council.Member) string {
	var b strings.Builder
	b.WriteString("THE COUNCIL — you are writing the verdict of EACH of these, one after another:\n")
	for _, m := range members {
		fmt.Fprintf(&b, "\n• %s — lens %q: %s\n  ROUTE: %s\n", m.Name, m.Lens, lensOf(m), council.RouteFor(m.Lens))
	}
	return b.String()
}

// panelIndependence is the whole cost of this shape, spent on defending against it.
//
// Three separate calls could not copy from each other because none of them could see the others.
// One call can, and the cheapest thing a model can do here is decide once and dress the decision
// three ways. So the instruction asks for the expensive thing explicitly and names the tell.
const panelIndependence = "These are THREE JUDGEMENTS, not one judgement written three times. Take them in " +
	"order, and finish each one — its own requirements walk, in its own route's order, its own citation — " +
	"before you begin the next. Do not let a verdict you have already written settle a later one: a lens " +
	"that walks the same requirements in the same order and cites the same fragment as an earlier lens has " +
	"not been applied, it has been echoed. Where two lenses do reach the same conclusion, they must reach it " +
	"from different evidence, and each must say which evidence. Disagreement between your own lenses is a " +
	"normal and useful outcome — record it rather than smoothing it away; a lens that cannot find grounds of " +
	"its own should abstain, not agree.\n\n"

// panelBody is the part of the panel prompt that is about the council rather than about the
// evidence: who is in it, that their judgements are three and not one, and that the round will be
// closed in a second message.
func panelBody(members []council.Member) string {
	return panelRoster(members) + "\n" + councilRouteCaveat + panelIndependence + panelSynthesis
}

// panelPromptFor renders the whole panel system prompt. Named so a test can read what is actually
// sent rather than re-assemble it and check its own assembly.
func panelPromptFor(members []council.Member, keep bool) string {
	return panelBody(members) + orientAssembled + fmt.Sprintf(councilCore, keepClauseFor(keep)) +
		fmt.Sprintf(councilGrounds, citeNoEvidence, panelSchemaFor(keep))
}

// panelSchemaFor is the reply shape, with the advisory keep field when it was asked for.
func panelSchemaFor(keep bool) string {
	if !keep {
		return panelSchema
	}
	return strings.Replace(panelSchema, `"cite":"verbatim fragment of what you were shown, or NO-EVIDENCE"}`,
		`"cite":"verbatim fragment of what you were shown, or NO-EVIDENCE"`+panelKeepField+`}`, 1)
}

// samePanelBackend reports whether one call can carry every member's judgement. It cannot when the
// members are pinned to different backends or models: that is a deliberate configuration — a weak
// model and a strong one judging the same work — and folding it into one request would silently
// answer with whichever backend the first member happened to name. Those councils keep the
// per-member shape and pay for it, which is the right trade when the whole point is that the
// members are not the same judge.
func samePanelBackend(members []council.Member) bool {
	for _, m := range members[1:] {
		if m.Provider != members[0].Provider || m.Model != members[0].Model {
			return false
		}
	}
	return true
}

// panelSynthesis warns that the round is not over when the verdicts are.
const panelSynthesis = "When all three verdicts are written you will be asked one more question about them.\n\n"

// panelCloseAsk is the second call, and it has to be a DIFFERENT question over DIFFERENT material
// or it will only ratify.
//
// It also has to be asked NEUTRALLY, and the first three versions were not. They opened "now close
// the round" — and close is a finishing word. The clamp above means this call can only ever change
// an outcome in one direction, toward continue; framed as a closing, the one thing it is able to do
// is the one thing it has been nudged away from doing. Both answers are named here, and neither is
// presented as the point of asking.
//
// Two versions established that by failing. The first was handed the same system prompt and the same
// evidence — the agent's report included — and asked to conclude again from what it had just
// concluded from: eleven convenings, not one disagreement. The second took the report off the table
// and gave it a mechanical job, naming the deciding walk item: four convenings, still not one
// disagreement. Reframing a reader does not give it anything new to read.
//
// So it is given the one VIEW nobody else has. The three walks are written in sequence and no author
// sees the others; this call sees all three at once, which makes three defects visible that are
// invisible from inside any one of them — a contradiction between two readings of the same output, a
// requirement no walk covered at all, and a value accepted for its provenance that is wrong on its
// face. The third is the one the measurements kept pointing at: across every arm, the walks checked
// where a number came from and nobody ever asked whether the number could be right.
const panelCloseAsk = "── ACROSS THE WALKS ──\n" +
	"The three walks are written. Each was written without sight of the other two; you are the first reader " +
	"to see them together, and that view is the whole reason there is a question left to ask.\n" +
	"The question is NOT whether to wrap this up. It is what the three walks, read against each other, " +
	"actually add up to — which may be that the task is finished, and may be that something in them is still " +
	"open. Both answers are ordinary. Neither is the outcome you are here to produce.\n" +
	"The agent's own report is above you and is NOT evidence here. It is the claim under examination; it had " +
	"whatever weight it deserves inside the walks. Do not re-read it to decide this — a summary that reads as " +
	"complete reads that way whether or not the work is. Judge on what the TOOLS RETURNED and on what the " +
	"three walks say.\n" +
	"Read the walks ACROSS each other and look for the three things only that view shows:\n" +
	"1. CONTRADICTION — two lenses reading the SAME evidence to opposite marks, one SATISFIED and one " +
	"UNSATISFIED. At least one of them is wrong, and a round cannot finish on a contradiction it has not " +
	"resolved: say which reading the tool output actually supports, and if you cannot, continue.\n" +
	"2. GAP — a requirement the TASK states that appears in NO walk. Three lenses can each cover a part and " +
	"together miss one, and no member can see this because no member sees the other walks. A requirement " +
	"nobody walked has not been judged, and it is not satisfied by their silence.\n" +
	"3. A VALUE THAT IS WRONG ON ITS FACE — an item marked SATISFIED because the right command ran and " +
	"produced a number, where the NUMBER ITSELF is not one the task's subject admits. The walks check " +
	"PROVENANCE, which is their job; nobody checks magnitude. A measured constant far outside its known " +
	"range, a count that cannot be that large, a duration that cannot be that short, a converged fit whose " +
	"parameters sit nowhere near the thing being fitted — a well-sourced wrong answer is still wrong. Two " +
	"values wrong by the SAME factor is a unit or column error and not a coincidence.\n" +
	"Then say what they add up to. Do not count votes:\n" +
	"- any UNSATISFIED item, or one settled only by NO-EVIDENCE where the task called for evidence, or a " +
	"contradiction, a gap, or an implausible value → continue, and name it;\n" +
	"- ONE lens finding such an item is enough even when the other two found nothing: two lenses not noticing " +
	"a defect is not evidence that there is none, and a requirement the task states is not carried or " +
	"cancelled by a show of hands;\n" +
	"- a lens asking for proof of something the TASK never required is not a defect and must not hold the " +
	"round — that is the churn the rules forbid;\n" +
	"- everything walked, satisfied by tool results, and nothing implausible → done.\n" +
	"If the walks disagree and what the tools returned cannot say which is right, CONTINUE: one more turn " +
	"costs a turn, a wrong done costs the task.\n" +
	"Reply with ONLY this JSON object, no prose, no code fence:\n" +
	`{"rationale":"what decides it: the walk item and whose lens, or the contradiction/gap/implausible value",` +
	`"decision":"done|continue","feedback":"what must happen next (only if continue)"}`

// panelClose is the round's own conclusion, separate from its three verdicts.
type panelClose struct {
	Decision  council.Decision
	Rationale string
	Feedback  string
}

// closeReply is the second call's answer. `rationale` leads and `decision` follows it, for the
// reason the walk leads the verdict: what is written first is what the rest is written to fit.
type closeReply struct {
	Rationale jsonx.Text `json:"rationale"`
	Decision  jsonx.Text `json:"decision"`
	Feedback  jsonx.Text `json:"feedback"`
}

// closeOf reads the round's conclusion. An absent or unreadable one is NOT a done: it is nothing at
// all, and the tally then decides alone — the conclusion may only ever ADD a reason to continue.
func closeOf(text string) panelClose {
	for _, js := range jsonx.Objects(text) {
		var r closeReply
		if jsonx.Unmarshal(js, &r) && strings.TrimSpace(string(r.Decision)) != "" {
			return panelClose{
				Decision:  decisionOf(strings.TrimSpace(string(r.Decision))),
				Rationale: strings.TrimSpace(string(r.Rationale)),
				Feedback:  strings.TrimSpace(string(r.Feedback)),
			}
		}
	}
	return panelClose{}
}

// panelVerdict is one entry of the reply. Every field is a tolerant type for the same reason the
// single-member reply's are: a verdict that was cast must not be lost to the shape of a field.
type panelVerdict struct {
	Member     jsonx.Text   `json:"member"`
	Lens       jsonx.Text   `json:"lens"`
	Checks     jsonx.Text   `json:"checks"`
	Decision   jsonx.Text   `json:"decision"`
	Confidence jsonx.Number `json:"confidence"`
	Rationale  jsonx.Text   `json:"rationale"`
	Feedback   jsonx.Text   `json:"feedback"`
	Keep       jsonx.Text   `json:"keep"`
	Cite       jsonx.Text   `json:"cite"`
}

type panelReply struct {
	Verdicts []panelVerdict `json:"verdicts"`
}

// parsePanel pulls the verdicts out of a reply, tolerating the two shapes a model sends when it
// ignores the wrapper: a bare array, and the array under some other key.
func parsePanel(text string) ([]panelVerdict, bool) {
	for _, js := range jsonx.Objects(text) {
		var r panelReply
		if jsonx.Unmarshal(js, &r) && len(r.Verdicts) > 0 {
			return r.Verdicts, true
		}
		var bare []panelVerdict
		if jsonx.Unmarshal(js, &bare) && len(bare) > 0 {
			return bare, true
		}
	}
	// Nothing whole. Keep whatever arrived before the defect — with three verdicts in one reply the
	// alternative is not a worse council, it is NO council: every member abstains at once and the
	// quorum rule resolves the round to continue on no information at all.
	for _, js := range jsonx.Objects(text) {
		cut, ok := jsonx.SalvagePrefix(js)
		if !ok {
			continue
		}
		var r panelReply
		if jsonx.Unmarshal(cut, &r) && len(r.Verdicts) > 0 {
			fmt.Fprintf(os.Stderr, "magi: a council panel reply was cut off; kept %d verdict(s) from its prefix\n",
				len(r.Verdicts))
			return r.Verdicts, true
		}
	}
	return nil, false
}

// pollPanel runs the whole council in one call and returns one verdict per member, in member
// order. A member the reply did not speak for comes back as an abstain that says so, which the
// tally already knows how to handle — the round is never silently decided by a short reply.
func (c *Council) pollPanel(ctx context.Context, req port.DeliberationRequest, members []council.Member) ([]council.Verdict, panelClose) {
	out := make([]council.Verdict, len(members))
	for i, m := range members {
		out[i] = council.Verdict{Member: m.Name, Lens: m.Lens, Weight: m.Weight,
			Decision: council.Abstain, Rationale: "the council panel did not return a verdict for this lens"}
	}
	model := req.DefaultModel
	if model == "" {
		model = c.model
	}
	provider := c.resolve("")
	if provider == nil {
		for i := range out {
			out[i].Rationale = "no council backend resolved"
		}
		return out, panelClose{}
	}

	schema := panelSchemaFor(req.Keep)
	// The judging instruction is the same text every other shape uses; only the roster and the
	// independence clause are added, and the identity line is replaced by the roster.
	body := panelBody(members)
	sys := body + orientAssembled + fmt.Sprintf(councilCore, keepClauseFor(req.Keep)) +
		fmt.Sprintf(councilGrounds, citeNoEvidence, schema)
	user := evidence(req)
	sys = withLangNote(sys, req.Task)

	send := func(msgs []session.Message) (string, error) {
		stream, err := provider.StreamChat(ctx, port.ChatRequest{
			Model: model, System: sys,
			Messages: msgs,
			Params:   map[string]any{"temperature": 0.0},
		})
		if err != nil {
			return "", err
		}
		text, cut := drain(stream)
		if cut != nil {
			fmt.Fprintf(os.Stderr, "magi: a council panel reply was cut off after %d chars: %v\n", len(text), cut)
		}
		return text, nil
	}
	msg := func(role session.Role, text string) session.Message {
		return session.Message{Role: role, Parts: []session.Part{{Kind: session.PartText, Text: text}}}
	}
	ask := func(u string) (string, error) { return send([]session.Message{msg(session.RoleUser, u)}) }

	raw, err := ask(user)
	if err != nil {
		for i := range out {
			out[i].Rationale = "council unavailable: " + err.Error()
		}
		return out, panelClose{}
	}
	vs, ok := parsePanel(raw)
	if !ok {
		noteUnparsed("the council panel's verdicts (every lens recorded as an abstain)", raw)
		if raw, err = ask(user + councilRetryReminder(raw)); err == nil {
			vs, ok = parsePanel(raw)
		}
		if !ok {
			return out, panelClose{}
		}
	}
	// The round's own conclusion, asked as a second turn of the SAME exchange so the backend reads
	// the whole prefix from cache. A failure here is not an outcome: the tally still decides.
	var cl panelClose
	if closeRaw, cerr := send([]session.Message{
		msg(session.RoleUser, user), msg(session.RoleAssistant, raw), msg(session.RoleUser, panelCloseAsk),
	}); cerr == nil {
		if cl = closeOf(closeRaw); cl.Decision == "" {
			noteUnparsed("the council's closing decision (the tally decides alone)", closeRaw)
		}
	} else {
		fmt.Fprintf(os.Stderr, "magi: the council's closing call failed (%v); the tally decides alone\n", cerr)
	}
	// Match by name, then by lens, then by position: a model that renames a member has still cast
	// its votes, and losing the whole round over a label is the failure this is guarding against.
	used := make([]bool, len(vs))
	take := func(i int, j int) {
		v := vs[j]
		used[j] = true
		out[i].Decision = decisionOf(strings.TrimSpace(string(v.Decision)))
		out[i].Confidence = float64(v.Confidence)
		out[i].Rationale = string(v.Rationale)
		out[i].Feedback = string(v.Feedback)
		out[i].Keep = string(v.Keep)
		out[i].Cite = strings.TrimSpace(string(v.Cite))
		if w := strings.TrimSpace(string(v.Checks)); w != "" {
			fmt.Fprintf(os.Stderr, "magi: council %s (%s) walked: %s\n", out[i].Member, out[i].Lens, clipWalk(w))
		} else {
			fmt.Fprintf(os.Stderr, "magi: council %s (%s) sent no requirements walk\n", out[i].Member, out[i].Lens)
		}
	}
	for i, m := range members {
		for j, v := range vs {
			if !used[j] && strings.EqualFold(strings.TrimSpace(string(v.Member)), m.Name) {
				take(i, j)
				break
			}
		}
	}
	for i, m := range members {
		if out[i].Decision != council.Abstain || out[i].Rationale == "" {
			continue
		}
		for j, v := range vs {
			if !used[j] && strings.EqualFold(strings.TrimSpace(string(v.Lens)), m.Lens) {
				take(i, j)
				break
			}
		}
	}
	for i := range members {
		if !strings.HasPrefix(out[i].Rationale, "the council panel did not return") {
			continue
		}
		for j := range vs {
			if !used[j] {
				take(i, j)
				break
			}
		}
	}
	return out, cl
}
