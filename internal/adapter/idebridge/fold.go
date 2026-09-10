package idebridge

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
)

// Rows folds a conversation's log into the lines a screen shows.
//
// This is the rule itself — the third of the eight, and the one two clients wrote separately. It
// is ported from `clients/vscode/src/core/transcript.ts` deliberately and not merged with the
// Kotlin one: where they disagree the TypeScript reading was chosen (rows.go says why), and every
// place the two differed is a comment there rather than a silent pick here.
//
// Not every event becomes a row, and the ones that do not are as deliberate as the ones that do:
// `context.usage` and `todos.changed` are facts for other screens, and putting them in the
// transcript would make the conversation a log file.
//
// **Draw shallowly.** What to SAY is the daemon's decision. A client that parses payloads to
// compose sentences composes them once per client, and there are six clients.
func Rows(events []event.Event) []Row {
	// Pointers, not values. The rule reaches back and marks rows that are already out — a reply
	// clears the bar on the prompt above it, a tool result lands ON its call's row, a resurfaced
	// interjection MOVES its original. With values, `drafts` would hold copies and every one of
	// those marks would be written to something nobody draws.
	var out []*Row
	answered := map[int64]bool{}
	// Streaming chunks, keyed by the message and kind they belong to. A draft is REPLACED by the
	// fact when it arrives rather than added to — the appended part carries the whole text, so
	// keeping both would show the answer twice.
	drafts := map[string]*Row{}
	dropDraft := func(key string) {
		row, ok := drafts[key]
		if !ok {
			return
		}
		delete(drafts, key)
		for i, r := range out {
			if r == row {
				out = append(out[:i], out[i+1:]...)
				return
			}
		}
	}
	// Any assistant part answers the prompts above it. The mark travels on the ROW rather than
	// being recomputed at draw time: a screen that re-derived it would have to hold the whole log
	// to draw one row.
	answerPending := func() {
		for _, r := range out {
			if r.Pending && !answered[r.Seq] {
				r.Pending = false
				answered[r.Seq] = true
			}
		}
	}

	for _, e := range events {
		d := payload(e.Data)
		switch e.Type {
		case event.TypePartDelta:
			kind := str(d, "kind")
			if kind == "" {
				kind = "text"
			}
			text := str(d, "text")
			if text == "" || (kind != "text" && kind != "reasoning") {
				break
			}
			key := str(d, "messageId") + ":" + kind
			row, ok := drafts[key]
			if !ok {
				who, folded := WhoAgent, false
				if kind == "reasoning" {
					who, folded = WhoThinking, true
				}
				row = &Row{Seq: e.Seq, Who: who, Draft: true, Folded: folded}
				drafts[key] = row
				out = append(out, row)
				// A chunk is an answer to whatever is above it, the same as any assistant part.
				// Without this the person's own row keeps its bar for the whole of a streamed reply.
				answerPending()
			}
			row.Text += text

		case event.TypePromptSubmitted:
			text := strings.TrimSpace(partsText(d))
			if text == "" {
				break
			}
			// ⚠ Not every prompt is the person's. The core signs each one, and a client that reads
			// none of it shows anything the daemon submitted wearing the person's name — measured
			// off a live daemon: an `actor.kind: "system"` prompt reading "You stopped without
			// saying you are finished", drawn as something the person typed.
			switch e.Actor.Kind {
			case "agent":
				// A subagent's report, injected back. The body belongs to that child's own
				// transcript; repeating it here is noise.
				break
			case "system":
				// A planner or council note. Worth a line — without it this panel shows LESS than
				// the headless printer does. One line only: the whole of it is in the log, and a
				// note must not push the conversation out.
				who := e.Actor.ID
				if who == "" {
					who = "system"
				}
				out = append(out, &Row{Seq: e.Seq, Who: WhoSystem,
					Text: "⟳ " + who + " note: " + firstLine(text)})
			default:
				id := str(d, "messageId")
				// ⚠ A resurfaced interjection is the SAME question, not a second one. Something
				// typed while a turn was running is queued, and the drain re-runs it as a FRESH
				// prompt with a new id; `resurfacedFrom` carries the original's. Without reading
				// it the question stays stranded far up the transcript wearing a queued mark that
				// never clears, and a second copy appears at the bottom.
				//
				// MOVED, not deleted and re-pushed: the original row carries its own history and
				// re-creating it throws that away.
				if from := str(d, "resurfacedFrom"); from != "" {
					if i := lastUserRow(out, from); i >= 0 {
						moved := out[i]
						out = append(out[:i], out[i+1:]...)
						moved.Text, moved.Queued, moved.Pending, moved.Seq = text, false, true, e.Seq
						if id != "" {
							moved.MsgID = id
						} else {
							moved.MsgID = from
						}
						out = append(out, moved)
						break
					}
				}
				out = append(out, &Row{Seq: e.Seq, Who: WhoUser, Text: text, Pending: true, MsgID: id})
			}

		case event.TypePartAppended:
			p := obj(d, "part")
			role := str(d, "role")
			// The fact replaces its own draft. Keyed by message AND kind, because one message
			// streams reasoning and text as two drafts and only one is being written here.
			if k := str(p, "kind"); k == "text" || k == "reasoning" {
				dropDraft(str(d, "messageId") + ":" + k)
			}
			if role == "assistant" || role == "tool" {
				answerPending()
			}
			// ⚠ An inline answer pulls its question down to it. A queued or mid-turn message can be
			// answered INLINE — no fresh prompt is emitted, so `inReplyTo` is the only link a
			// display layer has to pair the answer with its question.
			if replyTo := str(d, "inReplyTo"); role == "assistant" && replyTo != "" {
				if q := lastUserRow(out, replyTo); q >= 0 {
					moved := out[q]
					out = append(out[:q], out[q+1:]...)
					moved.Queued = false
					out = append(out, moved)
				}
			}
			appendPart(&out, e.Seq, p)

		case event.TypeError:
			// A recovered error is not an ending, and the payload says which. Dropping that
			// distinction would make every retry look like a failure.
			if msg := strings.TrimSpace(str(d, "message")); msg != "" {
				if isTrue(d, "recovered") {
					msg += " (recovered)"
				}
				out = append(out, &Row{Seq: e.Seq, Who: WhoError, Text: msg})
			}

		case event.TypeCouncilDecided:
			out = append(out, decidedRow(e.Seq, d))

		case event.TypeInterjectionDeferred:
			// ⚠ One event type carries BOTH ends of this state: the core writes it with
			// `resolved:false` when a prompt is queued and `resolved:true` when it LEAVES the
			// queue. Reading the type and ignoring the field marks every un-parking PARKED — and
			// because it arrives after whatever cleared it, the wrong word is the last one.
			//
			// ⚠ `Resolved` is a Go bool with omitempty: FALSE NEVER GOES ON THE WIRE. The parked
			// case is the one with no field at all, so the test is "not true", never "is false".
			//
			// Only `queued` moves. Leaving the queue is not being answered.
			id := str(d, "messageId")
			parked := !isTrue(d, "resolved")
			for _, r := range out {
				if r.Who == WhoUser && r.MsgID == id {
					r.Queued = parked
				}
			}

		case event.TypeInterjectionAnswered:
			// The agent says its answer already covered the parked message. The bar comes down —
			// "in its place" and "still waiting" are not the same thing.
			id := str(d, "messageId")
			for _, r := range out {
				if r.Who == WhoUser && r.MsgID == id {
					r.Queued, r.Pending = false, false
				}
			}

		case event.TypeSessionMoved:
			// The core writes this fact INTO the conversation being left, and says why: what a
			// reader needs is the reason its transcript stops. Without the line the conversation
			// simply stops — indistinguishable from a daemon that died, which is the reading
			// somebody would act on.
			//
			// Said, not followed. Switching a panel out from under somebody mid-read is not this
			// row's business — the id is in the text so a resume can be deliberate.
			text := "⇢ the companion moved to another conversation — this one ends here"
			if to := strings.TrimSpace(str(d, "to")); to != "" {
				text = "⇢ the companion moved to " + to + " — this conversation ends here"
			}
			out = append(out, &Row{Seq: e.Seq, Who: WhoSystem, Text: text})

		case event.TypeCouncilConvened:
			out = append(out, convenedRow(e.Seq, d))

		case event.TypeCouncilVerdict:
			out = append(out, verdictRow(e.Seq, d))

		case event.TypeCompaction:
			// ⚠ Without this row the transcript just STOPS earlier than a person remembers, with
			// nothing saying why: the fold replaces everything up to a point with a summary, and a
			// reader scrolling back finds a gap and no explanation of it.
			before, after := num(d, "tokensBefore"), num(d, "tokensAfter")
			out = append(out, &Row{Seq: e.Seq, Who: WhoSystem,
				Text: "↯ folded the conversation: ~" + itoa(before) + "→" + itoa(after) +
					" tok (" + SizeNote(before, after) + ")"})

		case event.TypePromptAbandoned:
			// `msgId` here, `messageId` on the prompt. Same id, two spellings, no error either way.
			id := str(d, "msgId")
			if id == "" {
				break
			}
			for _, r := range out {
				if r.Who == WhoUser && r.MsgID == id {
					r.Pending, r.Queued, r.Abandoned = false, false, true
				}
			}

		case event.TypeTurnFinished:
			// ⚠ A turn that could not be verified is not a turn that finished. The core sets this
			// when the execution-evidence gate could not confirm the outcome, in its own words so
			// the turn is "labeled UNVERIFIED rather than laundered into a confident success".
			//
			// A row rather than a mark on the last row: the fact belongs to the TURN, and the last
			// row may be a tool call or a council seat that had nothing to do with the deliverable.
			if isTrue(d, "unverified") {
				text := "⚠ Unverified — nothing ran to confirm this"
				if why := strings.TrimSpace(str(d, "reason")); why != "" {
					text += ": " + why
				}
				out = append(out, &Row{Seq: e.Seq, Who: WhoSystem, Text: text})
			}
			// Not a row. It ends the turn, and the screen reads that from the pending marks.
			for _, r := range out {
				r.Pending = false
			}
			// ⚠ Sweep the orphan drafts. There are several paths where the core streams chunks and
			// never writes the fact — a reply the spin guard discarded, a tool call that arrived as
			// text, an interrupt, a provider error, a failed interjection mini-turn. Left standing,
			// a half-answer sits on the screen of the window that happened to be attached and
			// nowhere else, which is the very split this package exists to prevent.
			for key := range drafts {
				dropDraft(key)
			}
		}
	}

	// Values on the way out. The pointers are this fold's own machinery, and handing them to a
	// caller would let one screen's edit reach another's rows.
	flat := make([]Row, 0, len(out))
	for _, r := range out {
		flat = append(flat, *r)
	}
	return flat
}

// appendPart is the part-kind fold, kept apart because it is the one place a MISSING branch is
// invisible: a kind nobody names is not an empty row, it is a row that never existed with nothing
// anywhere saying so. The web console hit exactly that — an image and an error both reached the log
// and neither reached the page.
func appendPart(out *[]*Row, seq int64, p map[string]any) {
	switch str(p, "kind") {
	case "text":
		if t := strings.TrimSpace(str(p, "text")); t != "" {
			*out = append(*out, &Row{Seq: seq, Who: WhoAgent, Text: t})
		}
	case "reasoning":
		if t := strings.TrimSpace(str(p, "text")); t != "" {
			*out = append(*out, &Row{Seq: seq, Who: WhoThinking, Text: t, Folded: true})
		}
	case "tool-call":
		call := obj(p, "toolCall")
		if call == nil {
			return
		}
		name := str(call, "name")
		if name == "" {
			name = "tool"
		}
		*out = append(*out, &Row{Seq: seq, Who: WhoTool, Text: name,
			CallID: str(call, "callId"), Args: AskedFor(call["args"])})
	case "tool-result":
		res := obj(p, "toolResult")
		if res == nil {
			return
		}
		// The result lands ON the call's row rather than starting a new one — one call, one line.
		call := str(res, "callId")
		for i := len(*out) - 1; i >= 0; i-- {
			r := (*out)[i]
			if r.Who != WhoTool || r.CallID != call {
				continue
			}
			// Two questions, not one (see Row.Note). An advisory result DID the work: a post-edit
			// hook's output sets isError on purpose, and a screen reading only that drew a file
			// that was written and then linted as a write that FAILED.
			advisory := isTrue(res, "advisory")
			ok := !isTrue(res, "isError") || advisory
			r.Ok = &ok
			if advisory {
				r.Note = true
			}
			if isTrue(res, "isError") && !advisory {
				// The reason travels with the failure. Read the VALUE, not its rendering: content
				// is often a JSON string, and stringifying it again leaves the escapes on screen.
				r.Out = said(res["content"])
			}
			return
		}
	case "image":
		// The path, not the picture. A panel cannot read a file off disk without being handed a
		// handle for it, and drawing nothing while that is worked out is the defect. A path a
		// person can open is the honest minimum — and the glyph the Kotlin copy put in the text
		// belongs to whatever draws this, not to the fact.
		if img := obj(p, "image"); img != nil {
			if path := str(img, "path"); path != "" {
				*out = append(*out, &Row{Seq: seq, Who: WhoImage, Text: path})
			}
		}
	case "error":
		if t := strings.TrimSpace(str(p, "error")); t != "" {
			*out = append(*out, &Row{Seq: seq, Who: WhoError, Text: t})
		}
	}
}

// convenedRow opens a round.
//
// What is dropped by skipping this event is the round's THRESHOLD: three verdicts arrive — two
// continue, one done — and without the rule a reader cannot tell whether that needed a majority or
// all three.
func convenedRow(seq int64, d map[string]any) *Row {
	text := strings.TrimSpace(str(d, "task"))
	if text == "" {
		var members []string
		for _, m := range arr(d, "members") {
			if s, ok := m.(string); ok {
				members = append(members, s)
			}
		}
		if len(members) > 0 {
			text = strings.Join(members, ", ")
		} else {
			text = "a round opened"
		}
	}
	// ⚠ An empty `changes` is two different things: either the turn edited nothing, or it edited
	// something the reconstruction lost. The core says the first out loud with `noChanges`, and the
	// fact belongs on the opening row because it says what KIND of turn was judged. Measured
	// 2026-09-10: 374 of 994 rounds are read-only turns — the common case, which is exactly why
	// leaving it unsaid makes the common case read like a failure.
	return &Row{Seq: seq, Who: WhoCouncil, Opened: true, Text: text,
		Round:    int(num(d, "round")),
		Rule:     strings.TrimSpace(str(d, "rule")),
		ReadOnly: isTrue(d, "noChanges")}
}

// verdictRow is one seat's vote.
//
// ⚠ A verdict ALWAYS makes a row. Pushing one only when there is prose makes a member who voted
// with nothing to add vanish, and a council of three draws as a council of two with nothing saying
// a seat was missing. The prose that DID arrive is never dropped either: a silent verdict has
// arrived carrying a full rationale, and a shaper that drew the fallback words instead lost them.
func verdictRow(seq int64, d map[string]any) *Row {
	text := strings.TrimSpace(str(d, "feedback"))
	if text == "" {
		text = strings.TrimSpace(str(d, "rationale"))
	}
	silent := isTrue(d, "silent")
	if text == "" && silent {
		text = "no answer came back"
	}
	row := &Row{Seq: seq, Who: WhoCouncil, Text: text, Member: str(d, "member"),
		Round:    int(num(d, "round")),
		Decision: strings.TrimSpace(str(d, "decision")),
		Silent:   silent,
		Lens:     strings.TrimSpace(str(d, "lens")),
		Cite:     strings.TrimSpace(str(d, "cite")),
		Keep:     strings.TrimSpace(str(d, "keep")),
		Thought:  strings.TrimSpace(str(d, "thought"))}
	if c := num(d, "confidence"); c > 0 {
		row.Confidence = &c
	}
	return row
}

// decidedRow is the gate's own answer — what the council DECIDED, not what one member said.
//
// ⚠ The note alone cannot tell a rejection from a council nobody reached. It is one of two fixed
// sentences the core picks by whether the finish was accepted, and it never mentions who voted — so
// a round where three members read the work and objected and a round where all three were
// unreachable arrive as the same sentence with the same `continue`. The core's own words for what
// collapsing them costs: "a round nobody voted in is NOT a rejection … the reader's next move is
// different: fix the backend, not the work."
func decidedRow(seq int64, d map[string]any) *Row {
	t := obj(d, "tally")
	n := func(k string) int { return int(num(t, k)) }
	var counts string
	if t != nil {
		// `silent` is the abstentions that were FAILURES rather than choices, so the said-abstain
		// count is abstain − silent: printing both raw counts a dead member twice.
		said := n("abstain") - n("silent")
		parts := []string{itoa(float64(n("done"))) + " done", itoa(float64(n("continue"))) + " continue"}
		if said > 0 {
			parts = append(parts, itoa(float64(said))+" abstain")
		}
		if n("silent") > 0 {
			parts = append(parts, itoa(float64(n("silent")))+" no answer")
		}
		counts = strings.Join(parts, " / ")
	}
	// ⚠ The tally beside a rebuttal is the one taken AFTER it. A rebuttal round only runs when the
	// independent vote split, so a 3-0 that started 2-1 reads as agreement that was never there.
	// Absent on the common case, so nothing is said then rather than "not debated" — a line on
	// every round would drown the one that matters.
	var debated string
	if db := obj(d, "debate"); db != nil {
		changed := int(num(db, "changed"))
		moved := itoa(float64(changed)) + " members"
		if changed == 1 {
			moved = "1 member"
		}
		before := strings.TrimSpace(str(db, "before"))
		after := strings.TrimSpace(str(db, "after"))
		switch {
		case changed == 0:
			debated = "debated, no one moved"
		case before != after:
			debated = "debated: " + before + "→" + after + ", " + moved + " moved"
		default:
			debated = "debated: " + after + " held, " + moved + " moved"
		}
	}
	note := strings.TrimSpace(str(d, "note"))
	if note == "" {
		note = strings.TrimSpace(str(d, "feedback"))
	}
	var text []string
	for _, s := range []string{note, counts, debated} {
		if s != "" {
			text = append(text, s)
		}
	}
	return &Row{Seq: seq, Who: WhoCouncil, Text: strings.Join(text, " — "),
		Round:    int(num(d, "round")),
		Decision: strings.TrimSpace(str(d, "decision")),
		// Nobody weighed it. Same word the individual verdicts use for the same fact, so one thing
		// is not spelled two ways across one screen.
		Silent: t != nil && n("voters") == 0 && n("silent") > 0}
}

// SizeNote says how much a fold shed, in the core's own words.
//
// The larger-than-before case is stated rather than clamped: a summary that came out bigger than
// what it replaced is the one outcome a person should see, and rendering it as "−0, −0%" would
// hide it.
func SizeNote(before, after float64) string {
	if after > before {
		return "+" + itoa(after-before) + ", the summary is LARGER than what it replaced"
	}
	freed := math.Max(0, before-after)
	pct := 0.0
	if before > 0 {
		pct = math.Round(freed * 100 / before)
	}
	return "−" + itoa(freed) + ", −" + itoa(pct) + "%"
}

// AskedFor is a tool call's arguments as one line, for the row that names the call.
//
// Nothing is invented. An empty object summarises to nothing, and the row is then the bare name
// again — which is the truth about a call that was given no arguments.
func AskedFor(args any) string {
	if args == nil {
		return ""
	}
	var o map[string]any
	switch v := args.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &o); err != nil {
			return clip(v)
		}
	case map[string]any:
		o = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return clip(string(b))
	}
	for _, k := range []string{"path", "command", "pattern", "query", "id", "name"} {
		if s, ok := o[k].(string); ok && strings.TrimSpace(s) != "" {
			return clip(strings.TrimSpace(s))
		}
	}
	b, err := json.Marshal(o)
	if err != nil || len(b) == 0 || string(b) == "{}" {
		return ""
	}
	return clip(string(b))
}

// said is a tool result's content as words. A JSON string is its own text; anything else is its JSON.
func said(content any) string {
	if content == nil {
		return ""
	}
	t, ok := content.(string)
	if !ok {
		b, err := json.Marshal(content)
		if err != nil {
			return ""
		}
		t = string(b)
	}
	if strings.TrimSpace(t) == "" {
		return ""
	}
	return clip(t)
}

// clip is one line, bounded. A row is a line — a summary that wraps is not a summary.
//
// The bound is counted in UTF-16 code units rather than runes, which is the one place this port
// had to choose deliberately: the TypeScript copy slices a JS string, so an emoji costs it two.
// Counting runes here would clip a line of emoji at a different place than the other client does,
// and "the rule is written once" is exactly what that would break.
func clip(s string) string {
	line := strings.TrimSpace(firstLine(s))
	units, cut := 0, -1
	for i, r := range line {
		if units >= 100 {
			cut = i
			break
		}
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
	}
	if cut < 0 {
		return line
	}
	return line[:cut] + "…"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func lastUserRow(out []*Row, msgID string) int {
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Who == WhoUser && out[i].MsgID == msgID {
			return i
		}
	}
	return -1
}

// partsText joins a submitted prompt's text parts, which is where the person's words are.
func partsText(d map[string]any) string {
	var b strings.Builder
	for _, p := range arr(d, "parts") {
		if m, ok := p.(map[string]any); ok {
			b.WriteString(str(m, "text"))
		}
	}
	return b.String()
}

// payload decodes an event's data once. A payload that will not parse is not half a row — it is
// no row, and inventing fields from a broken one is worse than drawing nothing.
func payload(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil || d == nil {
		return map[string]any{}
	}
	return d
}

func str(d map[string]any, k string) string {
	s, _ := d[k].(string)
	return s
}

func num(d map[string]any, k string) float64 {
	switch v := d[k].(type) {
	case float64:
		return v
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return 0
}

func isTrue(d map[string]any, k string) bool {
	b, _ := d[k].(bool)
	return b
}

func obj(d map[string]any, k string) map[string]any {
	m, _ := d[k].(map[string]any)
	return m
}

func arr(d map[string]any, k string) []any {
	a, _ := d[k].([]any)
	return a
}

// itoa writes a whole number the way the other copies write it — no decimal point on a count.
func itoa(f float64) string {
	n := int64(f)
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
