package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// entry is a message together with the seq of the event that created it, so
// compaction can drop only the events it replaces (F-COMPACT, F-EVENT-RECON).
type entry struct {
	seq int64
	msg session.Message
}

// reconstruct rebuilds the conversation from a session's event log. A compaction
// event replaces all messages with seq <= ReplacesUpToSeq by a single system
// summary, while messages newer than that boundary are preserved.
func reconstruct(evs []event.Event) []session.Message { return rebuild(evs, false) }

// reconstructWhole is the same conversation with NOTHING dropped: every message that was ever
// said, and a note where the model's memory of them folded.
//
// What the model is sent and what a person reads are two different things, and the log holds
// both — compaction never deletes an event, it only changes which ones go into the next request.
// Reading the display off reconstruct made the browser inherit the model's amnesia: mid-read, a
// fold landed and the scrollback the person was following vanished, replaced by a summary of
// itself. Reported from a live console, and the reporter's diagnosis was exactly right — the
// compacted messages drop out and only the tail remains, so the window reads as though it
// collapsed.
//
// The note stays because the fold is a real event a reader wants to see: it is the moment the
// agent stopped being able to remember what is still on the screen above it.
func reconstructWhole(evs []event.Event) []session.Message { return rebuild(evs, true) }

func rebuild(evs []event.Event, whole bool) []session.Message {
	var entries []*entry
	index := map[string]*entry{} // messageID -> entry

	// Elided tool results, collected up front because the elide event always lands after the part
	// it names. The MODEL's view substitutes the stub; the person's view (whole) keeps the bytes —
	// what the model can no longer afford to re-read is still what actually happened.
	elided := map[string]int{}
	if !whole {
		for _, ev := range evs {
			if ev.Type != event.TypeResultElided {
				continue
			}
			var d event.ResultElidedData
			if json.Unmarshal(ev.Data, &d) == nil && d.CallID != "" {
				elided[d.CallID] = d.Bytes
			}
		}
	}

	// Recall topics accumulate across every compaction so the surviving (latest)
	// summary advertises ALL recoverable topics — a later compaction drops the
	// earlier summary entry, so without this its topics would become undiscoverable
	// even though recall_context can still reach them.
	var topics []string
	topicSeen := map[string]bool{}

	// at is the time of the event that OPENED the message: a message that grew over four minutes
	// of streaming is stamped with when it started, which is what a transcript reads as and what
	// the terminal has always stamped its blocks with.
	addPart := func(seq int64, at time.Time, msgID string, role session.Role, part session.Part) {
		if e, ok := index[msgID]; ok {
			e.msg.Parts = append(e.msg.Parts, part)
			return
		}
		e := &entry{seq: seq, msg: session.Message{ID: msgID, Role: role, Parts: []session.Part{part}, At: at}}
		index[msgID] = e
		entries = append(entries, e)
	}

	for _, ev := range evs {
		switch ev.Type {
		case event.TypeCompaction:
			var d event.CompactionData
			_ = json.Unmarshal(ev.Data, &d)
			for _, sh := range d.Shards {
				if !topicSeen[sh.Topic] {
					topicSeen[sh.Topic] = true
					label := `"` + sh.Topic + `"`
					if sh.Brief != "" {
						label += " — " + sh.Brief
					}
					topics = append(topics, label)
				}
			}
			if whole {
				// The display keeps everything and marks the seam. No entry is dropped and the
				// index is left intact, so a message that goes on growing after the fold still
				// finds its parts.
				entries = append(entries, &entry{seq: ev.Seq, msg: session.Message{
					ID:    "compaction-" + itoa(ev.Seq),
					Role:  session.RoleSystem,
					Parts: []session.Part{{Kind: session.PartText, Text: foldNote(d, topics)}},
				}})
				continue
			}
			// Keep only entries newer than the compaction boundary.
			kept := entries[:0:0]
			index = map[string]*entry{}
			for _, e := range entries {
				if e.seq > d.ReplacesUpToSeq {
					kept = append(kept, e)
					index[e.msg.ID] = e
				}
			}
			text := d.Summary
			if h := recallHint(topics); h != "" {
				text += "\n\n" + h
			}
			summary := &entry{seq: ev.Seq, msg: session.Message{
				ID:    "compaction-" + itoa(ev.Seq),
				Role:  session.RoleSystem,
				Parts: []session.Part{{Kind: session.PartText, Text: text}},
			}}
			entries = append([]*entry{summary}, kept...)

		case event.TypePromptSubmitted:
			var d event.PromptSubmittedData
			if json.Unmarshal(ev.Data, &d) != nil {
				continue
			}
			// Who wrote it survives into the message. It used to be dropped here — every prompt
			// became a user message whatever had written it — and the consequence was that magi's
			// own words reached the model as the person's: "You stopped without saying you are
			// finished" arrived indistinguishable from somebody typing it. The actor was in the
			// event the whole time and was read only for display.
			//
			// A system role is not a second channel: normalizeSystemPlacement demotes any
			// mid-conversation system message to a user one prefixed "[system note]", which is
			// exactly the attribution that was missing and is portable to backends that reject a
			// system message anywhere but the head. Compaction summaries have travelled that road
			// since they existed.
			//
			// ActorAgent stays a user message: a subagent's report IS handed to the agent as work
			// that arrived, and it names itself in its own text.
			role := session.RoleUser
			author := ""
			if ev.Actor.Kind == event.ActorSystem {
				role = session.RoleSystem
				// And WHICH part of magi wrote it. The orchestrator's nudge, the planner's note
				// and a mined spec all arrive here as "system", and a reader who cannot tell them
				// apart cannot tell whether the agent was corrected or informed.
				author = ev.Actor.ID
			}
			e := &entry{seq: ev.Seq, msg: session.Message{
				ID: d.MessageID, Role: role, Parts: d.Parts, At: ev.TS, Author: author}}
			index[d.MessageID] = e
			entries = append(entries, e)

		case event.TypePromptAbandoned:
			// The prompt is still part of the conversation — it was said — so it is marked rather
			// than removed. Removing it would leave the answer that never came looking like an
			// answer to whatever came before.
			var d event.PromptAbandonedData
			if json.Unmarshal(ev.Data, &d) == nil {
				if e, ok := index[d.MsgID]; ok {
					e.msg.Abandoned = true
				}
			}

		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(ev.Data, &d) != nil {
				continue
			}
			if d.Part.ToolResult != nil {
				if n, ok := elided[d.Part.ToolResult.CallID]; ok {
					r := *d.Part.ToolResult
					r.Content = elideStub(n)
					d.Part.ToolResult = &r
				}
			}
			addPart(ev.Seq, ev.TS, d.MessageID, d.Role, d.Part)
		}
	}

	out := make([]session.Message, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.msg)
	}
	return out
}

// elideStub is the fixed text an elided result shows the model: what happened, how big it was,
// and how to get it back. Deterministic per byte count, so every rebuild of the same log renders
// the same bytes and the prefix cache holds across steps.
func elideStub(n int) json.RawMessage {
	b, _ := json.Marshal("[tool result elided to keep the conversation inside the context window (" +
		itoa(int64(n)) + " bytes). It is re-derivable: re-read the file or re-run the command if it is needed again.]")
	return b
}

// filterDeferredEvents removes user prompt events whose MessageID is currently deferred
// (a mid-turn interjection queued to run as its own later turn). Applied to the LIVE
// views only — the running turn's model context and the council's per-turn evidence scan
// — so a still-queued interjection can neither merge into the current turn nor reset the
// council's PromptSubmitted turn-boundary window. Order and seqs of the remaining events
// are preserved, so reconstruct's compaction boundaries are unaffected.
func filterDeferredEvents(evs []event.Event, deferred map[string]bool) []event.Event {
	if len(deferred) == 0 {
		return evs
	}
	out := make([]event.Event, 0, len(evs))
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted {
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil && deferred[d.MessageID] {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

// abandonedDeferrals reconstructs, from the deferral ledger (F5), the set of interjection
// origin MessageIDs that were queued but never resolved. An interjection is RESOLVED when
// it leaves the queue: absorbed inline / by a route (an InterjectionDeferred entry with
// Resolved:true) or drained to its own turn (a PromptSubmitted whose ResurfacedFrom points
// back to it). Everything queued (Resolved:false) and not so resolved is abandoned — the
// in-memory queue was lost to a process kill before it could drain. Callers keep these
// masked from the live turn context so a stranded interjection is not mixed into the next
// request. Returns nil when nothing is abandoned.
func abandonedDeferrals(evs []event.Event) map[string]bool {
	deferred := map[string]bool{}
	resolved := map[string]bool{}
	for _, e := range evs {
		switch e.Type {
		case event.TypeInterjectionDeferred:
			var d event.InterjectionDeferredData
			if json.Unmarshal(e.Data, &d) != nil || d.MessageID == "" {
				continue
			}
			if d.Resolved {
				resolved[d.MessageID] = true
			} else {
				deferred[d.MessageID] = true
			}
		case event.TypePromptSubmitted:
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil && d.ResurfacedFrom != "" {
				resolved[d.ResurfacedFrom] = true
			}
		case event.TypeInterjectionAnswered:
			// The model answered it inline. The ledger's Resolved:true is written later, at the
			// finish boundary (settleAnsweredClaims), so a reload in the window between the answered
			// claim and its settlement used to see a deferred entry with no resolution and mask the
			// interjection as abandoned — silently dropping a request the model had addressed. The
			// display layer (answeredInline) already treats this event as resolution; reconstruct
			// now agrees.
			var d event.InterjectionAnsweredData
			if json.Unmarshal(e.Data, &d) == nil && d.MessageID != "" {
				resolved[d.MessageID] = true
			}
		}
	}
	for id := range resolved {
		delete(deferred, id)
	}
	if len(deferred) == 0 {
		return nil
	}
	return deferred
}

// dropResurfacedOrigins removes the ORIGINAL prompt event of any queued interjection
// that was later re-emitted (linked via ResurfacedFrom). Applied to display/resume
// views only (SessionState): the re-emitted copy sits next to its answer at the back
// of the stream, so dropping the stranded original leaves a single query paired with
// its answer instead of a duplicate. Order and seqs of the remaining events are
// preserved. Turn logic (which uses reconstruct directly) is unaffected.
func dropResurfacedOrigins(evs []event.Event) []event.Event {
	var origins map[string]bool
	for _, e := range evs {
		if e.Type != event.TypePromptSubmitted {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) == nil && d.ResurfacedFrom != "" {
			if origins == nil {
				origins = map[string]bool{}
			}
			origins[d.ResurfacedFrom] = true
		}
	}
	if len(origins) == 0 {
		return evs
	}
	out := make([]event.Event, 0, len(evs))
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted {
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil && origins[d.MessageID] {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

// recallHint is the line appended to a compaction summary telling the model the shed
// detail is recoverable and which topics to ask for. Empty when nothing was sharded.
func recallHint(topics []string) string {
	if len(topics) == 0 {
		return ""
	}
	return `[Earlier context was compacted but its full detail is preserved. Call recall_context with the quoted topic to pull any of these back verbatim — ` + strings.Join(topics, "; ") + "]"
}

// rebuildMessages reconstructs only the messages with the given IDs from the raw event
// log, IGNORING compaction boundaries — the originals always persist, so this recovers
// shed detail for recall_context. It shares the part-grouping rule with reconstruct so
// recalled context and live context never drift.
func rebuildMessages(evs []event.Event, ids []string) []session.Message {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var entries []*entry
	index := map[string]*entry{}
	for _, ev := range evs {
		switch ev.Type {
		case event.TypePromptSubmitted:
			var d event.PromptSubmittedData
			if json.Unmarshal(ev.Data, &d) != nil || !want[d.MessageID] {
				continue
			}
			if _, ok := index[d.MessageID]; ok {
				continue
			}
			e := &entry{seq: ev.Seq, msg: session.Message{ID: d.MessageID, Role: session.RoleUser, Parts: d.Parts}}
			index[d.MessageID] = e
			entries = append(entries, e)
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(ev.Data, &d) != nil || !want[d.MessageID] {
				continue
			}
			if e, ok := index[d.MessageID]; ok {
				e.msg.Parts = append(e.msg.Parts, d.Part)
				continue
			}
			e := &entry{seq: ev.Seq, msg: session.Message{ID: d.MessageID, Role: d.Role, Parts: []session.Part{d.Part}}}
			index[d.MessageID] = e
			entries = append(entries, e)
		}
	}
	out := make([]session.Message, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.msg)
	}
	return out
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
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

// foldNote is what a READER is told where a compaction happened. It says what the agent lost, not
// what the reader lost — the reader lost nothing, every message is still above this line.
func foldNote(d event.CompactionData, topics []string) string {
	note := "⋯ the agent's memory was folded here"
	if d.TokensBefore > 0 && d.TokensAfter > 0 {
		note += fmt.Sprintf(" — %d → %d tokens", d.TokensBefore, d.TokensAfter)
	}
	note += ". Everything above stays on this screen; from here the agent has a summary of it"
	if h := recallHint(topics); h != "" {
		note += ", and can pull the detail back:\n\n" + h
	} else {
		note += "."
	}
	return note
}
