package app

import (
	"encoding/json"
	"strings"

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
func reconstruct(evs []event.Event) []session.Message {
	var entries []*entry
	index := map[string]*entry{} // messageID -> entry

	// Recall topics accumulate across every compaction so the surviving (latest)
	// summary advertises ALL recoverable topics — a later compaction drops the
	// earlier summary entry, so without this its topics would become undiscoverable
	// even though recall_context can still reach them.
	var topics []string
	topicSeen := map[string]bool{}

	addPart := func(seq int64, msgID string, role session.Role, part session.Part) {
		if e, ok := index[msgID]; ok {
			e.msg.Parts = append(e.msg.Parts, part)
			return
		}
		e := &entry{seq: seq, msg: session.Message{ID: msgID, Role: role, Parts: []session.Part{part}}}
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
			e := &entry{seq: ev.Seq, msg: session.Message{ID: d.MessageID, Role: session.RoleUser, Parts: d.Parts}}
			index[d.MessageID] = e
			entries = append(entries, e)

		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(ev.Data, &d) != nil {
				continue
			}
			addPart(ev.Seq, d.MessageID, d.Role, d.Part)
		}
	}

	out := make([]session.Message, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.msg)
	}
	return out
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
