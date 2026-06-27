package app

import (
	"encoding/json"

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
			// Keep only entries newer than the compaction boundary.
			kept := entries[:0:0]
			index = map[string]*entry{}
			for _, e := range entries {
				if e.seq > d.ReplacesUpToSeq {
					kept = append(kept, e)
					index[e.msg.ID] = e
				}
			}
			summary := &entry{seq: ev.Seq, msg: session.Message{
				ID:    "compaction-" + itoa(ev.Seq),
				Role:  session.RoleSystem,
				Parts: []session.Part{{Kind: session.PartText, Text: d.Summary}},
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
