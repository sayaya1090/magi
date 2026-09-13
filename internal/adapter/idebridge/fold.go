package idebridge

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
)

// Rows folds a conversation's log into the lines a screen shows.
//
// This is the rule itself — the third of the eight, and the one two clients wrote separately. It
// was ported from `clients/vscode/src/core/transcript.ts` deliberately and not merged with the
// Kotlin one: where they had historically disagreed the TypeScript reading was chosen (rows.go
// explains the six-vs-eight vocabulary history and subsequent alignment), and every place the two
// differed is documented there rather than silently picked here.
//
// Not every event becomes a row, and the ones that do not are as deliberate as the ones that do:
// `context.usage` and `todos.changed` are facts for other screens, and putting them in the
// transcript would make the conversation a log file.
//
// **Draw shallowly.** What to SAY is the daemon's decision. A client that parses payloads to
// compose sentences composes them once per client, and there are six clients.
// name fills Row.ID — the one place the rule lives, so a client's name for a row is the fold's.
//
// A fact is named by the event that made it. A draft has no event to be named by (chunks are written
// with seq 0), so it is named by what it is a draft of — the same key the fold finds it with.
func name(rows []Row) {
	for i := range rows {
		r := &rows[i]
		if r.Draft {
			kind := "text"
			if r.Who == WhoThinking {
				kind = "reasoning"
			}
			r.ID = "d:" + r.MsgID + ":" + kind
			continue
		}
		r.ID = strconv.FormatInt(r.Seq, 10)
	}
}

// stampOf is an event's time as a row carries it, or "" when the log did not say.
//
// RFC3339 with nanoseconds, which is what the log writes — the same string the other copies read off
// the wire, so a row folded here and a row folded there say the same instant the same way. A zero time
// is no time: absent and "the epoch" are different facts, and a screen drawing 1970 beside a message
// from today is the kind of wrong that looks like a bug in the clock.
func stampOf(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

// body is a whole text, or "" when there is nothing but whitespace in it.
//
// ⚠ **Trimming decides whether there is a body; it does not produce one.** These bodies used to be
// stored trimmed, and for a tool's output that is a real loss: `"    return x\n"` became `"return x"`,
// so the indentation — which in code output IS the content — was gone by the time any screen saw it.
//
// ⚠ **And it is only for the places where the body IS the row.** A prompt, an assistant part, a
// member's rationale: there the text is the row's whole reason to exist, so whitespace-only means
// nothing was said and the row is not made (or the fallback words are). Where the row exists for
// another reason — a tool result lands on its CALL's row, a verdict's reasoning rides beside a vote
// that was cast — the body is kept exactly as it came, whitespace-only included: "this file holds
// three spaces" and "the tool returned nothing" are different answers, and collapsing them is the
// same loss as the trim, one step smaller (the review named it, 2026-09-13).
//
// What stays trimmed on purpose, because it is not a body:
//
//   - **tokens** — a decision, a lens, a rule, a session id, a vote tally. A provider that writes
//     `" done "` means the same word as `done`, and comparing those as different states is a bug.
//   - **pieces of a sentence this fold composes** — an error's message (it gets " (recovered)"
//     appended), a council note joined with " — ". Those are renderings, and a rendering that keeps a
//     trailing newline in the middle of a sentence is just broken.
func body(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return s
}

// summarise fills Row.Summary for every row — one line, bounded, from whatever body the row carries.
//
// Done in one pass at the end rather than at each of the twelve places a row is built: a summary
// derived per-site is a rule with twelve copies, and this tree has paid for that shape often enough
// to know how it ends. The bodies are the fact; this is a rendering of them.
func summarise(rows []Row) {
	for i := range rows {
		r := &rows[i]
		// The order is what a person reads first: what was said, then what was asked, then what came
		// back. A tool row's Text is the tool's NAME, so the arguments join it rather than replace it.
		line := clip(r.Text)
		if r.Who == WhoTool {
			if a := askedLine(r.Args); a != "" {
				line = strings.TrimSpace(line + " " + a)
			} else if o := clip(r.Out); o != "" {
				line = strings.TrimSpace(line + " " + o)
			}
			line = clip(line)
		}
		if line == "" {
			line = clip(r.Out)
		}
		r.Summary = line
	}
}

func Rows(events []event.Event) []Row {
	rows, _ := fold(events)
	return rows
}

// foldStats is what the fold DID, for the tests that ask whether it did too much.
//
// ⚠ A seam, not a knob, and it exists because the alternative was a clock. The property worth holding
// is "clearing the waiting marks costs the number of MARKS, not the length of the conversation", and a
// timing test cannot hold it: measured 2026-09-13, the quadratic shape and the linear one were 36×
// and 24× on the same 16×-longer log — 1.5 apart, with allocator and GC effects that size. Counting
// the work is exact, needs no clock, and fails the same way on a busy machine as on an idle one.
type foldStats struct {
	// pendingVisits is how many rows the two mark-clearing loops looked at, in total.
	pendingVisits int
}

func fold(events []event.Event) ([]Row, foldStats) {
	var stats foldStats
	// Pointers, not values. The rule reaches back and marks rows that are already out — a reply
	// clears the bar on the prompt above it, a tool result lands ON its call's row, a resurfaced
	// interjection MOVES its original. With values, `drafts` would hold copies and every one of
	// those marks would be written to something nobody draws.
	var out []*Row
	answered := map[int64]bool{}
	// pending is the rows that carry a waiting mark. Held apart from `out` because clearing the mark
	// is the one thing this fold does OFTEN and to FEW rows — see answerPending.
	var pending []*Row
	// Streaming chunks, keyed by the message and kind they belong to. A draft is REPLACED by the
	// fact when it arrives rather than added to — the appended part carries the whole text, so
	// keeping both would show the answer twice.
	drafts := map[string]*Row{}
	// How many councils this stream has opened, and where each member's verdict sits in `out` — so
	// the fact can land on its preview's place rather than beside it. See the note at the verdict
	// case on why the round number cannot be the key.
	convene := 0
	verdictAt := map[string]int{}
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
	//
	// ⚠ **Only the rows that carry the mark, not every row so far.** This used to walk `out`, and
	// `out` grows: an answer arrives, the walk is the length of the conversation, and there is an
	// answer per assistant part. Measured 2026-09-13 on synthetic logs — 200 events 1.5ms, 20000
	// events 786ms, and 53% of that in this one closure. Quadratic, and the `rows` door pays it on
	// every call: three quarters of a second to answer one question about a long conversation, and a
	// live surface that re-folds per event could not keep up at all. Nothing here is a cache — the
	// list IS the set this loop was looking for, and the predicate below is unchanged.
	answerPending := func() {
		kept := pending[:0]
		stats.pendingVisits += len(pending)
		for _, r := range pending {
			switch {
			case !r.Pending:
				// Cleared by another path (abandoned, answered in place). It is no longer waiting,
				// so it is no longer a candidate.
			case answered[r.Seq]:
				// ⚠ Already answered under this seq, so the mark STAYS — the old walk did the same
				// thing by skipping it. A resurfaced prompt is the case that matters: it moves with
				// a new seq, and then it is a candidate again.
				kept = append(kept, r)
			default:
				r.Pending = false
				answered[r.Seq] = true
			}
		}
		pending = kept
	}

	for _, e := range events {
		// New rows get this event's time, stamped after the switch. Done in ONE place rather than at
		// each of the twelve sites a row is built: a field filled per-site is a rule with twelve
		// copies, and the site somebody forgets is the one nobody notices — which is how this door
		// came to carry no time at all while the Kotlin copy put one on every row (2026-09-14).
		before := len(out)
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
				// MsgID travels on the draft too. Without it the row cannot be NAMED: a draft is named
				// by the message and kind it is a draft of, and two messages streaming text at once
				// would otherwise share one name and one screen row.
				row = &Row{Seq: e.Seq, Who: who, Draft: true, Folded: folded, MsgID: str(d, "messageId")}
				drafts[key] = row
				out = append(out, row)
				// A chunk is an answer to whatever is above it, the same as any assistant part.
				// Without this the person's own row keeps its bar for the whole of a streamed reply.
				answerPending()
			}
			row.Text += text

		case event.TypePromptSubmitted:
			// Whole: a pasted snippet's indentation is part of what the person said.
			text := body(partsText(d))
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
						// 다시 기다리는 중이 됐으니 다시 후보다 — 새 seq 로는 아직 답이 없다.
						pending = append(pending, moved)
						if id != "" {
							moved.MsgID = id
						} else {
							moved.MsgID = from
						}
						out = append(out, moved)
						break
					}
				}
				row := &Row{Seq: e.Seq, Who: WhoUser, Text: text, Pending: true, MsgID: id}
				out = append(out, row)
				pending = append(pending, row)
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
			// A new convene. Every council opens at round 1, so the round number alone cannot tell
			// two of them apart — this is what keys a verdict to the round it belongs to.
			convene++

		case event.TypeCouncilVerdict:
			row := verdictRow(e.Seq, d)
			// ⚠ **A preview and the fact it becomes are ONE row, not two.**
			//
			// The core shows a round as it lands — `publishTransient` puts each verdict on the bus
			// the moment it arrives, so nobody stares at "3 of 3 answered" for ninety seconds — and
			// then writes the same verdicts as facts. Transient events carry no seq, so appending
			// both drew a council of three as **six rows**, every member twice. Measured 2026-09-11
			// through this shaper; the two editor clients had it too and were fixed in the same
			// shape (docs/CLIENT_LIFECYCLE §6).
			//
			// The fact lands on the preview's PLACE, decision included: a rebuttal round can move a
			// vote, and a preview left standing beside it shows the council disagreeing with itself.
			//
			// A preview arriving after the fact (seq 0 with one already recorded) is dropped — it
			// cannot be newer than what the log holds.
			key := fmt.Sprintf("%d/%d/%s", convene, row.Round, row.Member)
			if at, seen := verdictAt[key]; seen {
				if e.Seq > 0 {
					*out[at] = *row
				}
			} else {
				verdictAt[key] = len(out)
				out = append(out, row)
			}

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
			//
			// Same reason as answerPending: only the rows that carry the mark can lose it, and
			// walking the whole conversation once per TURN is the other half of the quadratic.
			stats.pendingVisits += len(pending)
			for _, r := range pending {
				r.Pending = false
			}
			pending = pending[:0]
			// ⚠ Sweep the orphan drafts. There are several paths where the core streams chunks and
			// never writes the fact — a reply the spin guard discarded, a tool call that arrived as
			// text, an interrupt, a provider error, a failed interjection mini-turn. Left standing,
			// a half-answer sits on the screen of the window that happened to be attached and
			// nowhere else, which is the very split this package exists to prevent.
			for key := range drafts {
				dropDraft(key)
			}
		}
		// ⚠ First stamp wins, so a row MOVED by a later event keeps the time it was made — a
		// resurfaced question still says when it was asked, which is what a person scrolling back to
		// it is reading. `before` can exceed len(out) when an event removed rows, hence the guard.
		if at := stampOf(e.TS); at != "" && before <= len(out) {
			for _, r := range out[before:] {
				if r.At == "" {
					r.At = at
				}
			}
		}
	}

	// Values on the way out. The pointers are this fold's own machinery, and handing them to a
	// caller would let one screen's edit reach another's rows.
	flat := make([]Row, 0, len(out))
	for _, r := range out {
		flat = append(flat, *r)
	}
	name(flat)
	summarise(flat)
	return flat, stats
}

// appendPart is the part-kind fold, kept apart because it is the one place a MISSING branch is
// invisible: a kind nobody names is not an empty row, it is a row that never existed with nothing
// anywhere saying so. The web console hit exactly that — an image and an error both reached the log
// and neither reached the page.
func appendPart(out *[]*Row, seq int64, p map[string]any) {
	switch str(p, "kind") {
	case "text":
		if t := body(str(p, "text")); t != "" {
			*out = append(*out, &Row{Seq: seq, Who: WhoAgent, Text: t})
		}
	case "reasoning":
		if t := body(str(p, "text")); t != "" {
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
			// ⚠ **The body is kept for every result, not only for failures.** It used to be filled
			// only when `isError && !advisory`, which folded two different decisions into one: whether
			// the body is PRESERVED and whether a screen shows it by default. A client moved onto this
			// row could then never expand a successful call — the output existed in the log and not on
			// the row, so the screen would have to go and parse the log again, which is the drift this
			// package exists to end.
			//
			// What a screen draws by default is said elsewhere and stays said: Ok tells failure from
			// success, Note marks an advisory result, Folded marks bodies shut until asked for.
			//
			// Read the VALUE, not its rendering: content is often a JSON string, and stringifying it
			// again leaves the escapes on screen.
			r.Out = said(res["content"])
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
	// ⚠ **The evidence, whole and in the core's own order.** Picking which of these to carry would
	// turn "what the members saw" into "what we decided to show", and a verdict is checkable only
	// against the first. This row used to keep the task as its text and drop the other four, so the one
	// thing that makes a council answer auditable never reached a screen (2026-09-14).
	var seen []string
	for _, k := range []string{"task", "plan", "report", "actions", "changes"} {
		if v := str(d, k); strings.TrimSpace(v) != "" {
			seen = append(seen, k+": "+v)
		}
	}
	return &Row{Seq: seq, Who: WhoCouncil, Opened: true, Text: text,
		Round:    int(num(d, "round")),
		Rule:     strings.TrimSpace(str(d, "rule")),
		Evidence: strings.Join(seen, "\n\n"),
		ReadOnly: isTrue(d, "noChanges")}
}

// verdictRow is one seat's vote.
//
// ⚠ A verdict ALWAYS makes a row. Pushing one only when there is prose makes a member who voted
// with nothing to add vanish, and a council of three draws as a council of two with nothing saying
// a seat was missing. The prose that DID arrive is never dropped either: a silent verdict has
// arrived carrying a full rationale, and a shaper that drew the fallback words instead lost them.
func verdictRow(seq int64, d map[string]any) *Row {
	// A rationale is a body: kept whole, because a member quoting three indented lines of a diff is
	// saying something with that indentation. Blank still means "no prose", which is what the
	// fallback below answers.
	text := body(str(d, "feedback"))
	if text == "" {
		text = body(str(d, "rationale"))
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
		// Bodies, not tokens — and they ride on a row the VOTE made, so they are kept as they came.
		// A cite is a fragment of the record and meant to be checkable against it; a trimmed quote no
		// longer matches what it quotes. Thought is the provider's reasoning stream, the same fact as
		// a reasoning part one layer up, which is kept whole there.
		Cite:    str(d, "cite"),
		Keep:    str(d, "keep"),
		Thought: str(d, "thought")}
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

// AskedFor is a tool call's arguments, WHOLE.
//
// Nothing is invented. An empty object summarises to nothing, and the row is then the bare name
// again — which is the truth about a call that was given no arguments.
//
// ⚠ **It used to clip to one line and 100 units, and that is a loss no reader can undo.** The fold
// is becoming the one place rows are built, and a client handed a clipped argument cannot get the
// command back — the same reason the row vocabulary is eight words and not six ("richer is
// recoverable, collapsed is not", rows.go). A `bash` call whose command spans three lines is exactly
// the call somebody is trying to read.
//
// The one-line form did not disappear: Row.Summary carries it, built by Summarise, so a screen that
// draws a list still has one line to draw and a screen that shows the call has the call.
func AskedFor(args any) string {
	if args == nil {
		return ""
	}
	var o map[string]any
	switch v := args.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &o); err != nil {
			return strings.TrimSpace(v)
		}
	case map[string]any:
		o = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
	// ⚠ **No picking here.** This used to return the FIRST of path/command/pattern/… that was present,
	// so `{path, old_string, new_string}` arrived as the path alone — the two strings that say what the
	// edit actually was were dropped. One representative field is a summary, not the arguments, and a
	// client handed the summary cannot get the call back. The pick lives in askedLine, for Row.Summary.
	b, err := json.Marshal(o)
	if err != nil || len(b) == 0 || string(b) == "{}" {
		return ""
	}
	return string(b)
}

// askedLine is a tool call's arguments as ONE line — the representative field if the call has one,
// the whole thing clipped otherwise.
//
// This is the rule AskedFor used to be. It reads the rendered arguments back rather than taking the
// original value, so there is one representation of a call's arguments on the row and one place that
// summarises it.
func askedLine(args string) string {
	if strings.TrimSpace(args) == "" {
		return ""
	}
	var o map[string]any
	if err := json.Unmarshal([]byte(args), &o); err != nil {
		return clip(args)
	}
	// The order is what a person scanning a list reads first: where, then what was run, then what was
	// looked for.
	for _, k := range []string{"path", "command", "pattern", "query", "id", "name"} {
		if s, ok := o[k].(string); ok && strings.TrimSpace(s) != "" {
			return clip(strings.TrimSpace(s))
		}
	}
	return clip(args)
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
	// ⚠ **Whole, not trimmed, and not blanked.** This is what a tool said, and this row exists because
	// a CALL was made — so nothing here has to earn the row by being non-empty. It used to arrive
	// clipped to one line (a stack trace reached a screen as its first line), then trimmed
	// (`"    return x\n"` became `"return x"`, and in code output the indentation IS the content), and
	// then blanked when it was whitespace ALONE — which made "this file holds three spaces"
	// indistinguishable from "the tool returned nothing". Preserving the body and deciding what a
	// screen shows are two decisions; Ok, Note and Folded make the second one, and Row.Summary carries
	// the one line a list draws — empty, for a body with no words in it.
	return t
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
